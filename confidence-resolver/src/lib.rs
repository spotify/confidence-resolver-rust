#![cfg_attr(
    not(test),
    deny(
        clippy::panic,
        clippy::unwrap_used,
        clippy::expect_used,
        clippy::indexing_slicing,
        clippy::arithmetic_side_effects
    )
)]

use bitvec::prelude as bv;
use core::marker::PhantomData;
use fastmurmur3::murmur3_x64_128;
use std::collections::{BTreeMap, HashMap, HashSet};

use bytes::Bytes;

use chrono::{DateTime, Utc};

const BUCKETS: u64 = 1_000_000;
const TARGETING_KEY: &str = "targeting_key";
const NULL: Value = Value { kind: None };

const MAX_NO_OF_FLAGS_TO_BATCH_RESOLVE: usize = 210;

use err::Fallible;

mod err;
pub mod flag_logger;
mod gzip;
pub mod proto;
pub mod resolve_logger;
mod schema_util;
mod value;

use proto::confidence::flags::admin::v1 as flags_admin;
use proto::confidence::flags::resolver::v1 as flags_resolver;
use proto::confidence::flags::resolver::v1::resolve_token_v1::AssignedFlag;
use proto::confidence::flags::types::v1 as flags_types;
use proto::confidence::iam::v1 as iam;
use proto::google::{value::Kind, Struct, Timestamp, Value};
use proto::Message;

use flags_admin::flag::rule;
use flags_admin::flag::{Rule, Variant};
use flags_admin::Flag;
use flags_admin::ResolverState as ResolverStatePb;
use flags_admin::Segment;
use flags_types::expression;
use flags_types::targeting;
use flags_types::targeting::criterion;
use flags_types::targeting::Criterion;
use flags_types::Expression;
use gzip::decompress_gz;

use crate::err::{ErrorCode, OrFailExt};
use crate::proto::confidence::flags::resolver::v1::resolve_with_sticky_response::{
    MaterializationUpdate, ResolveResult,
};
use crate::proto::confidence::flags::resolver::v1::{
    resolve_with_sticky_response, MaterializationMap, ResolveFlagsRequest, ResolveFlagsResponse,
    ResolveWithStickyRequest, ResolveWithStickyResponse,
};

impl TryFrom<Vec<u8>> for ResolverStatePb {
    type Error = ErrorCode;
    fn try_from(s: Vec<u8>) -> Fallible<Self> {
        ResolverStatePb::decode(&s[..]).or_fail()
    }
}

fn timestamp_to_datetime(ts: &Timestamp) -> Fallible<DateTime<Utc>> {
    DateTime::from_timestamp(ts.seconds, ts.nanos as u32).or_fail()
}
fn datetime_to_timestamp(dt: &DateTime<Utc>) -> Timestamp {
    Timestamp {
        seconds: dt.timestamp(),
        nanos: dt.timestamp_subsec_nanos() as i32,
    }
}

#[derive(Debug)]
pub struct Account {
    pub name: String,
}
impl Account {
    fn new(name: &str) -> Account {
        Account {
            name: name.to_string(),
        }
    }

    fn salt(&self) -> Fallible<String> {
        let id = self.name.split("/").nth(1).or_fail()?;
        Ok(format!("MegaSalt-{}", id))
    }

    fn salt_unit(&self, unit: &str) -> Fallible<String> {
        let salt = self.salt()?;
        Ok(format!("{}|{}", salt, unit))
    }
}

#[derive(Debug)]
pub struct Client {
    pub account: Account,
    pub client_name: String,
    pub client_credential_name: String,
}

#[derive(Debug)]
pub struct ResolverState {
    pub secrets: HashMap<String, Client>,
    pub flags: HashMap<String, Flag>,
    pub segments: HashMap<String, Segment>,
    pub bitsets: HashMap<String, bv::BitVec<u8, bv::Lsb0>>,
}
impl ResolverState {
    pub fn from_proto(state_pb: ResolverStatePb, account_id: &str) -> Fallible<Self> {
        let mut secrets = HashMap::new();
        let mut flags = HashMap::new();
        let mut segments = HashMap::new();
        let mut bitsets = HashMap::new();

        for flag in state_pb.flags {
            flags.insert(flag.name.clone(), flag);
        }
        for segment in state_pb.segments_no_bitsets {
            segments.insert(segment.name.clone(), segment);
        }
        for bitset in state_pb.bitsets {
            let Some(b) = bitset.bitset else { continue };
            match b {
                flags_admin::resolver_state::packed_bitset::Bitset::GzippedBitset(zipped_bytes) => {
                    // unzip bytes
                    let buffer = decompress_gz(&zipped_bytes[..])?;
                    let bitvec = bv::BitVec::from_slice(&buffer);
                    bitsets.insert(bitset.segment.clone(), bitvec);
                }
                // missing bitset treated as full
                flags_admin::resolver_state::packed_bitset::Bitset::FullBitset(true) => (),
                _ => fail!(),
            }
        }
        for client in state_pb.clients {
            for credential in &state_pb.client_credentials {
                if !credential.name.starts_with(client.name.as_str()) {
                    continue;
                }
                let Some(iam::client_credential::Credential::ClientSecret(client_secret)) =
                    &credential.credential
                else {
                    continue;
                };

                secrets.insert(
                    client_secret.secret.clone(),
                    Client {
                        account: Account::new(&format!("accounts/{}", account_id)),
                        client_name: client.name.clone(),
                        client_credential_name: credential.name.clone(),
                    },
                );
            }
        }

        Ok(ResolverState {
            secrets,
            flags,
            segments,
            bitsets,
        })
    }

    #[cfg(feature = "json")]
    pub fn get_resolver_with_json_context<'a, H: Host>(
        &'a self,
        client_secret: &str,
        evaluation_context: &str,
        encryption_key: &Bytes,
    ) -> Result<AccountResolver<'a, H>, String> {
        self.get_resolver(
            client_secret,
            // allow this unwrap cause it only happens in std
            #[allow(clippy::unwrap_used)]
            serde_json::from_str(evaluation_context)
                .map_err(|_| "failed to parse evaluation context".to_string())?,
            encryption_key,
        )
    }

    pub fn get_resolver<'a, H: Host>(
        &'a self,
        client_secret: &str,
        evaluation_context: Struct,
        encryption_key: &Bytes,
    ) -> Result<AccountResolver<'a, H>, String> {
        self.secrets
            .get(client_secret)
            .ok_or("client secret not found".to_string())
            .map(|client| {
                AccountResolver::new(
                    client,
                    self,
                    EvaluationContext {
                        context: evaluation_context,
                    },
                    encryption_key,
                )
            })
    }
}

pub struct EvaluationContext {
    pub context: Struct,
}
pub struct FlagToApply {
    pub assigned_flag: AssignedFlag,
    pub skew_adjusted_applied_time: Timestamp,
}

pub trait Host {
    #[cfg(not(feature = "std"))]
    fn random_alphanumeric(len: usize) -> String;
    #[cfg(feature = "std")]
    fn random_alphanumeric(len: usize) -> String {
        use rand::distr::{Alphanumeric, SampleString};
        Alphanumeric.sample_string(&mut rand::rng(), len)
    }

    fn log(_: &str) {
        // noop
    }

    #[cfg(not(feature = "std"))]
    fn current_time() -> Timestamp;
    #[cfg(feature = "std")]
    fn current_time() -> Timestamp {
        let now = chrono::Utc::now();
        Timestamp {
            seconds: now.timestamp(),
            nanos: now.timestamp_subsec_nanos() as i32,
        }
    }

    fn log_resolve(
        resolve_id: &str,
        evaluation_context: &Struct,
        values: &[ResolvedValue<'_>],
        client: &Client,
        sdk: &Option<flags_resolver::Sdk>,
    );

    fn log_assign(
        resolve_id: &str,
        evaluation_context: &Struct,
        assigned_flags: &[FlagToApply],
        client: &Client,
        sdk: &Option<flags_resolver::Sdk>,
    );

    fn encrypt_resolve_token(token_data: &[u8], encryption_key: &[u8]) -> Result<Vec<u8>, String> {
        #[cfg(feature = "std")]
        {
            const ENCRYPTION_WRITE_BUFFER_SIZE: usize = 4096;

            use std::io::Write;

            use crypto::{aes, blockmodes, buffer};
            use rand::RngCore;

            let mut iv = [0u8; 16];
            rand::rng().fill_bytes(&mut iv);

            let mut final_encrypted_token = Vec::<u8>::new();
            final_encrypted_token
                .write(&iv)
                .map_err(|_| "Failed to write iv to encrypted resolve token buffer".to_string())?;

            let mut encryptor = aes::cbc_encryptor(
                aes::KeySize::KeySize128,
                &iv,
                encryption_key,
                blockmodes::PkcsPadding,
            );

            let token_read_buffer = &mut buffer::RefReadBuffer::new(token_data);
            let mut write_buffer = [0; ENCRYPTION_WRITE_BUFFER_SIZE];
            let token_write_buffer = &mut buffer::RefWriteBuffer::new(&mut write_buffer);

            loop {
                use crypto::buffer::{BufferResult, ReadBuffer, WriteBuffer};

                let result = encryptor
                    .encrypt(token_read_buffer, token_write_buffer, true)
                    .map_err(|_| "Failed to encrypt resolve token".to_string())?;

                final_encrypted_token.extend(
                    token_write_buffer
                        .take_read_buffer()
                        .take_remaining()
                        .iter()
                        .copied(),
                );

                match result {
                    BufferResult::BufferUnderflow => break,
                    BufferResult::BufferOverflow => {}
                }
            }

            Ok(final_encrypted_token)
        }

        #[cfg(not(feature = "std"))]
        {
            // Null encryption for no_std when key is all zeros
            if encryption_key.iter().all(|&b| b == 0) {
                Ok(token_data.to_vec())
            } else {
                Err("Encryption not available in no_std mode".to_string())
            }
        }
    }

    fn decrypt_resolve_token(
        encrypted_data: &[u8],
        encryption_key: &[u8],
    ) -> Result<Vec<u8>, String> {
        #[cfg(feature = "std")]
        {
            {
                const ENCRYPTION_WRITE_BUFFER_SIZE: usize = 4096;

                use crypto::{aes, blockmodes, buffer};

                let mut iv = [0u8; 16];
                iv.copy_from_slice(encrypted_data.get(0..16).or_fail()?);

                let mut decryptor = aes::cbc_decryptor(
                    aes::KeySize::KeySize128,
                    &iv,
                    encryption_key,
                    blockmodes::PkcsPadding,
                );

                let encrypted_token_read_buffer =
                    &mut buffer::RefReadBuffer::new(encrypted_data.get(16..).or_fail()?);
                let mut write_buffer = [0; ENCRYPTION_WRITE_BUFFER_SIZE];
                let encrypted_token_write_buffer =
                    &mut buffer::RefWriteBuffer::new(&mut write_buffer);

                let mut final_decrypted_token = Vec::<u8>::new();
                loop {
                    use crypto::buffer::{BufferResult, ReadBuffer, WriteBuffer};

                    let result = decryptor
                        .decrypt(
                            encrypted_token_read_buffer,
                            encrypted_token_write_buffer,
                            true,
                        )
                        .or_fail()?;

                    final_decrypted_token.extend(
                        encrypted_token_write_buffer
                            .take_read_buffer()
                            .take_remaining()
                            .iter()
                            .copied(),
                    );

                    match result {
                        BufferResult::BufferUnderflow => break,
                        BufferResult::BufferOverflow => {}
                    }
                }

                Ok(final_decrypted_token)
            }
            .map_err(|e: ErrorCode| format!("failed to decrypt resolve token [{}]", e.b64_str()))
        }

        #[cfg(not(feature = "std"))]
        {
            // Null decryption for no_std when key is all zeros
            if encryption_key.iter().all(|&b| b == 0) {
                Ok(encrypted_data.to_vec())
            } else {
                Err("decryption not available in no_std mode".into())
            }
        }
    }
}

pub struct AccountResolver<'a, H: Host> {
    pub client: &'a Client,
    pub state: &'a ResolverState,
    pub evaluation_context: EvaluationContext,
    pub encryption_key: Bytes,
    host: PhantomData<H>,
}

#[derive(Debug)]
pub enum ResolveFlagError {
    Message(String),
    MissingMaterializations(),
}

impl ResolveFlagError {
    fn message(&self) -> String {
        match self {
            ResolveFlagError::Message(msg) => msg.clone(),
            ResolveFlagError::MissingMaterializations() => "Missing materializations".to_string(),
        }
    }

    pub fn err(message: &str) -> ResolveFlagError {
        ResolveFlagError::Message(message.to_string())
    }

    pub fn missing_materializations() -> ResolveFlagError {
        ResolveFlagError::MissingMaterializations()
    }
}

impl From<ResolveFlagError> for String {
    fn from(value: ResolveFlagError) -> Self {
        value.message().to_string()
    }
}

impl From<ErrorCode> for ResolveFlagError {
    fn from(value: ErrorCode) -> Self {
        ResolveFlagError::err(format!("error code {}", &value.to_string()).as_str())
    }
}

impl ResolveWithStickyResponse {
    fn with_success(response: ResolveFlagsResponse, updates: Vec<MaterializationUpdate>) -> Self {
        ResolveWithStickyResponse {
            resolve_result: Some(ResolveResult::Success(
                resolve_with_sticky_response::Success {
                    response: Some(response),
                    updates,
                },
            )),
        }
    }

    fn with_missing_materializations(
        items: Vec<resolve_with_sticky_response::MissingMaterializationItem>,
    ) -> Self {
        ResolveWithStickyResponse {
            resolve_result: Some(ResolveResult::MissingMaterializations(
                resolve_with_sticky_response::MissingMaterializations { items },
            )),
        }
    }
}

impl ResolveWithStickyRequest {
    fn without_sticky(resolve_request: ResolveFlagsRequest) -> ResolveWithStickyRequest {
        ResolveWithStickyRequest {
            resolve_request: Some(resolve_request),
            fail_fast_on_sticky: false,
            not_process_sticky: true,
            materializations_per_unit: BTreeMap::new(),
        }
    }
}

impl<'a, H: Host> AccountResolver<'a, H> {
    pub fn new(
        client: &'a Client,
        state: &'a ResolverState,
        evaluation_context: EvaluationContext,
        encryption_key: &Bytes,
    ) -> AccountResolver<'a, H> {
        AccountResolver {
            client,
            state,
            evaluation_context,
            encryption_key: encryption_key.clone(),
            host: PhantomData,
        }
    }

    pub fn resolve_flags_sticky(
        &self,
        request: &flags_resolver::ResolveWithStickyRequest,
    ) -> Result<ResolveWithStickyResponse, String> {
        let timestamp = H::current_time();

        let resolve_request = &request.resolve_request.clone().or_fail()?;
        let flag_names = resolve_request.flags.clone();
        let flags_to_resolve = self
            .state
            .flags
            .values()
            .filter(|flag| flag.state() == flags_admin::flag::State::Active)
            .filter(|flag| flag.clients.contains(&self.client.client_name))
            .filter(|flag| flag_names.is_empty() || flag_names.contains(&flag.name))
            .collect::<Vec<&Flag>>();

        if flags_to_resolve.len() > MAX_NO_OF_FLAGS_TO_BATCH_RESOLVE {
            return Err(format!(
                "max {} flags allowed in a single resolve request, this request would return {} flags.",
                MAX_NO_OF_FLAGS_TO_BATCH_RESOLVE,
                flags_to_resolve.len()));
        }

        if let Ok(Some(unit)) = self.get_targeting_key(TARGETING_KEY) {
            if unit.len() > 100 {
                return Err("Targeting key is too larger, max 100 characters.".to_string());
            }
        }

        let mut resolve_results = Vec::with_capacity(flags_to_resolve.len());

        let mut has_missing_materializations = false;

        for flag in flags_to_resolve.clone() {
            let resolve_result = self.resolve_flag(flag, request.materializations_per_unit.clone());
            match resolve_result {
                Ok(resolve_result) => resolve_results.push(resolve_result),
                Err(err) => {
                    return match err {
                        ResolveFlagError::Message(msg) => Err(msg.to_string()),
                        ResolveFlagError::MissingMaterializations() => {
                            if request.not_process_sticky {
                                continue;
                            }
                            // we want to fallback on online resolver, return early
                            if request.fail_fast_on_sticky {
                                Ok(ResolveWithStickyResponse::with_missing_materializations(
                                    vec![],
                                ))
                            } else {
                                has_missing_materializations = true;
                                break;
                            }
                        }
                    };
                }
            }
        }

        if has_missing_materializations {
            let result = self.collect_missing_materializations(flags_to_resolve);
            if let Ok(missing) = result {
                return Ok(ResolveWithStickyResponse::with_missing_materializations(
                    missing,
                ));
            } else {
                return Err("Could not collect missing materializations".to_string());
            }
        }

        let resolved_values: Vec<ResolvedValue> = resolve_results
            .iter()
            .map(|r| r.resolved_value.clone())
            .collect();

        let resolve_id = H::random_alphanumeric(32);
        let mut response = flags_resolver::ResolveFlagsResponse {
            resolve_id: resolve_id.clone(),
            ..Default::default()
        };
        let mut updates: Vec<MaterializationUpdate> = vec![];
        for resolved_value in &resolved_values {
            response.resolved_flags.push(resolved_value.into());
        }

        // Collect all materialization updates from all resolve results
        for resolve_result in &resolve_results {
            updates.extend(resolve_result.updates.clone());
        }

        if resolve_request.apply {
            let flags_to_apply: Vec<FlagToApply> = resolved_values
                .iter()
                .filter(|v| v.should_apply)
                .map(|v| FlagToApply {
                    assigned_flag: v.into(),
                    skew_adjusted_applied_time: timestamp.clone(),
                })
                .collect();

            H::log_assign(
                &resolve_id,
                &self.evaluation_context.context,
                flags_to_apply.as_slice(),
                self.client,
                &resolve_request.sdk.clone(),
            );
        } else {
            // create resolve token
            let mut resolve_token_v1 = flags_resolver::ResolveTokenV1 {
                resolve_id: resolve_id.clone(),
                evaluation_context: Some(self.evaluation_context.context.clone()),
                ..Default::default()
            };
            for resolved_value in &resolved_values {
                let assigned_flag: AssignedFlag = resolved_value.into();
                resolve_token_v1
                    .assignments
                    .insert(assigned_flag.flag.clone(), assigned_flag);
            }

            let resolve_token = flags_resolver::ResolveToken {
                resolve_token: Some(flags_resolver::resolve_token::ResolveToken::TokenV1(
                    resolve_token_v1,
                )),
            };

            let encrypted_token = self
                .encrypt_resolve_token(&resolve_token)
                .map_err(|_| "Failed to encrypt resolve token".to_string())
                .or_fail()?;

            response.resolve_token = encrypted_token;
        }

        H::log_resolve(
            &resolve_id,
            &self.evaluation_context.context,
            &resolved_values,
            self.client,
            &resolve_request.sdk.clone(),
        );

        Ok(ResolveWithStickyResponse::with_success(response, updates))
    }

    pub fn resolve_flags(
        &self,
        request: &flags_resolver::ResolveFlagsRequest,
    ) -> Result<flags_resolver::ResolveFlagsResponse, String> {
        let response = self.resolve_flags_sticky(&ResolveWithStickyRequest::without_sticky(
            flags_resolver::ResolveFlagsRequest {
                flags: request.flags.clone(),
                sdk: request.sdk.clone(),
                evaluation_context: request.evaluation_context.clone(),
                client_secret: request.client_secret.clone(),
                apply: request.apply,
            },
        ));

        match response {
            Ok(v) => match v.resolve_result {
                None => Err("failed to resolve flags".to_string()),
                Some(r) => match r {
                    ResolveResult::Success(flags_response) => match flags_response.response {
                        Some(flags_response) => Ok(flags_response),
                        None => Err("failed to resolve flags".to_string()),
                    },
                    ResolveResult::MissingMaterializations(_) => {
                        Err("sticky assignments is not supported".to_string())
                    }
                },
            },
            Err(e) => Err(e),
        }
    }

    pub fn apply_flags(&self, request: &flags_resolver::ApplyFlagsRequest) -> Result<(), String> {
        let send_time_ts = request.send_time.as_ref().ok_or("send_time is required")?;
        let send_time = to_date_time_utc(send_time_ts).ok_or("invalid send_time")?;
        let receive_time: DateTime<Utc> = timestamp_to_datetime(&H::current_time())?;

        let resolve_token_outer = self.decrypt_resolve_token(&request.resolve_token)?;
        let Some(flags_resolver::resolve_token::ResolveToken::TokenV1(resolve_token)) =
            resolve_token_outer.resolve_token
        else {
            return Err("resolve token is not a V1 token".to_string());
        };

        let assignments = resolve_token.assignments;
        let evaluation_context = resolve_token
            .evaluation_context
            .as_ref()
            .ok_or("missing evaluation context")?;

        // ensure that all flags are present before we start sending events
        let mut assigned_flags: Vec<FlagToApply> = Vec::with_capacity(request.flags.len());
        for applied_flag in &request.flags {
            let Some(assigned_flag) = assignments.get(&applied_flag.flag) else {
                return Err("Flag in resolve token does not match flag in request".to_string());
            };
            let Some(apply_time) = applied_flag.apply_time.as_ref() else {
                return Err(format!("Missing apply time for flag {}", applied_flag.flag));
            };
            let apply_time = to_date_time_utc(apply_time).or_fail()?;
            let skew = send_time.signed_duration_since(apply_time);
            let adjusted_time = receive_time.checked_sub_signed(skew).or_fail()?;
            let skew_adjusted_applied_time = datetime_to_timestamp(&adjusted_time);
            assigned_flags.push(FlagToApply {
                assigned_flag: assigned_flag.clone(),
                skew_adjusted_applied_time,
            });
        }

        H::log_assign(
            &resolve_token.resolve_id,
            evaluation_context,
            assigned_flags.as_slice(),
            self.client,
            &request.sdk,
        );

        Ok(())
    }

    fn get_targeting_key(&self, targeting_key: &str) -> Result<Option<String>, String> {
        let unit_value = self.get_attribute_value(targeting_key);
        match &unit_value.kind {
            None => Ok(None),
            Some(Kind::NullValue(_)) => Ok(None),
            Some(Kind::StringValue(string_unit)) => Ok(Some(string_unit.clone())),
            Some(Kind::NumberValue(num_value)) => {
                if num_value.is_finite() && num_value.fract() == 0.0 {
                    Ok(Some(format!("{:.0}", num_value)))
                } else {
                    Err("TargetingKeyError".to_string())
                }
            }
            _ => Err("TargetingKeyError".to_string()),
        }
    }
    pub fn resolve_flag_name(
        &'a self,
        flag_name: &str,
    ) -> Result<FlagResolveResult<'a>, ResolveFlagError> {
        self.state
            .flags
            .get(flag_name)
            .ok_or(ResolveFlagError::err("flag not found"))
            .and_then(|flag| self.resolve_flag(flag, BTreeMap::new()))
    }

    pub fn collect_missing_materializations(
        &'a self,
        flags: Vec<&'a Flag>,
    ) -> Result<Vec<resolve_with_sticky_response::MissingMaterializationItem>, String> {
        let mut missing_materializations: Vec<
            resolve_with_sticky_response::MissingMaterializationItem,
        > = Vec::new();
        for flag in flags {
            let result = self.collect_missing_materializations_for_flag(flag);
            if let Ok(items) = result {
                missing_materializations.extend(items);
            } else {
                return Err(format!(
                    "Could not collect missing materializations for flag {}",
                    flag.name
                ));
            }
        }
        Ok(missing_materializations)
    }

    fn collect_missing_materializations_for_flag(
        &'a self,
        flag: &'a Flag,
    ) -> Result<Vec<resolve_with_sticky_response::MissingMaterializationItem>, String> {
        let mut missing_materializations: Vec<
            resolve_with_sticky_response::MissingMaterializationItem,
        > = Vec::new();

        if flag.state == flags_admin::flag::State::Archived as i32 {
            return Ok(vec![]);
        }

        for rule in &flag.rules {
            if !rule.enabled {
                continue;
            }

            if let Some(materialization_spec) = &rule.materialization_spec {
                let rule_name = &rule.name.as_str();
                let read_materialization = materialization_spec.read_materialization.as_str();
                if !read_materialization.is_empty() {
                    let targeting_key = if !rule.targeting_key_selector.is_empty() {
                        rule.targeting_key_selector.as_str()
                    } else {
                        TARGETING_KEY
                    };
                    let unit: String = match self.get_targeting_key(targeting_key) {
                        Ok(Some(u)) => u,
                        Ok(None) => continue,
                        Err(_) => return Err("Targeting key error".to_string()),
                    };
                    missing_materializations.push(
                        resolve_with_sticky_response::MissingMaterializationItem {
                            unit,
                            rule: rule_name.to_string(),
                            read_materialization: read_materialization.to_string(),
                        },
                    );
                    continue;
                }
            }
        }
        Ok(missing_materializations)
    }

    pub fn resolve_flag(
        &'a self,
        flag: &'a Flag,
        sticky_context: BTreeMap<String, MaterializationMap>,
    ) -> Result<FlagResolveResult<'a>, ResolveFlagError> {
        let mut updates: Vec<MaterializationUpdate> = Vec::new();
        let mut resolved_value = ResolvedValue::new(flag);

        if flag.state == flags_admin::flag::State::Archived as i32 {
            return Ok(FlagResolveResult {
                resolved_value: resolved_value.error(ResolveReason::FlagArchived),
                updates: vec![],
            });
        }

        for rule in &flag.rules {
            if !rule.enabled {
                continue;
            }

            let segment_name = &rule.segment;
            if !self.state.segments.contains_key(segment_name) {
                // log something? ResolveReason::SEGMENT_NOT_FOUND
                continue;
            }
            let segment = self.state.segments.get(segment_name).or_fail()?;

            let targeting_key = if !rule.targeting_key_selector.is_empty() {
                rule.targeting_key_selector.as_str()
            } else {
                TARGETING_KEY
            };
            let unit: String = match self.get_targeting_key(targeting_key) {
                Ok(Some(u)) => u,
                Ok(None) => continue,
                Err(_) => {
                    return Ok(FlagResolveResult {
                        resolved_value: resolved_value.error(ResolveReason::TargetingKeyError),
                        updates: vec![],
                    })
                }
            };

            let Some(spec) = &rule.assignment_spec else {
                continue;
            };

            let mut materialization_matched = false;
            if let Some(materialization_spec) = &rule.materialization_spec {
                let read_materialization = &materialization_spec.read_materialization;
                if !read_materialization.is_empty() {
                    if let Some(info) = sticky_context.get(&unit) {
                        let info_from_context = info.info_map.get(read_materialization);

                        if let Some(info_data) = info_from_context {
                            if !info_data.unit_in_info {
                                if materialization_spec
                                    .mode
                                    .as_ref()
                                    .map(|mode| mode.materialization_must_match)
                                    .unwrap_or(false)
                                {
                                    // Materialization must match but unit is not in materialization
                                    continue;
                                }
                                materialization_matched = false;
                            } else if materialization_spec
                                .mode
                                .as_ref()
                                .map(|mode| mode.segment_targeting_can_be_ignored)
                                .unwrap_or(false)
                            {
                                materialization_matched = true;
                            } else {
                                materialization_matched = self.segment_match(segment, &unit)?;
                            }
                        } else {
                            return Err(ResolveFlagError::missing_materializations());
                        }

                        if materialization_matched {
                            if let Some(variant_name) = info_from_context
                                .as_ref()
                                .and_then(|info| info.rule_to_variant.get(&rule.name))
                            {
                                if let Some(assignment) =
                                    spec.assignments.iter().find(|assignment| {
                                        if let Some(rule::assignment::Assignment::Variant(
                                            ref variant_assignment,
                                        )) = &assignment.assignment
                                        {
                                            variant_assignment.variant == *variant_name
                                        } else {
                                            false
                                        }
                                    })
                                {
                                    let variant = flag
                                        .variants
                                        .iter()
                                        .find(|v| v.name == *variant_name)
                                        .or_fail()?;
                                    return Ok(FlagResolveResult {
                                        resolved_value: resolved_value.with_variant_match(
                                            rule,
                                            segment,
                                            variant,
                                            &assignment.assignment_id,
                                            &unit,
                                        ),
                                        updates: vec![],
                                    });
                                }
                            }
                        }
                    } else {
                        return Err(ResolveFlagError::missing_materializations());
                    };
                }
            }

            if !materialization_matched && !self.segment_match(segment, &unit)? {
                // ResolveReason::SEGMENT_NOT_MATCH
                continue;
            }
            let bucket_count = spec.bucket_count;
            let variant_salt = segment_name.split("/").nth(1).or_fail()?;
            let key = format!("{}|{}", variant_salt, unit);
            let bucket = bucket(hash(&key), bucket_count as u64)? as i32;

            let matched_assignment = spec.assignments.iter().find(|assignment| {
                assignment
                    .bucket_ranges
                    .iter()
                    .any(|range| range.lower <= bucket && bucket < range.upper)
            });

            let has_write_spec = rule
                .materialization_spec
                .as_ref()
                .map(|materialization_spec| &materialization_spec.write_materialization);

            if let Some(assignment) = matched_assignment {
                let Some(a) = &assignment.assignment else {
                    continue;
                };

                // Extract variant name from assignment if it's a variant assignment
                let variant_name = match a {
                    rule::assignment::Assignment::Variant(ref variant_assignment) => {
                        variant_assignment.variant.clone()
                    }
                    _ => "".to_string(),
                };

                // write the materialization info if write spec exists
                if let Some(write_spec) = has_write_spec {
                    updates.push(MaterializationUpdate {
                        write_materialization: write_spec.to_string(),
                        unit: unit.to_string(),
                        rule: rule.clone().name,
                        variant: variant_name,
                    })
                }

                match a {
                    rule::assignment::Assignment::Fallthrough(_) => {
                        resolved_value.attribute_fallthrough_rule(
                            rule,
                            &assignment.assignment_id,
                            &unit,
                        );
                        continue;
                    }
                    rule::assignment::Assignment::ClientDefault(_) => {
                        return Ok(FlagResolveResult {
                            resolved_value: resolved_value.with_client_default_match(
                                rule,
                                segment,
                                &assignment.assignment_id,
                                &unit,
                            ),
                            updates,
                        })
                    }
                    rule::assignment::Assignment::Variant(
                        rule::assignment::VariantAssignment {
                            variant: variant_name,
                        },
                    ) => {
                        let variant = flag
                            .variants
                            .iter()
                            .find(|variant| variant.name == *variant_name)
                            .or_fail()?;

                        return Ok(FlagResolveResult {
                            resolved_value: resolved_value.with_variant_match(
                                rule,
                                segment,
                                variant,
                                &assignment.assignment_id,
                                &unit,
                            ),
                            updates,
                        });
                    }
                };
            }
        }

        if resolved_value.reason == ResolveReason::Match {
            resolved_value.should_apply = true;
        } else {
            resolved_value.should_apply = !resolved_value.fallthrough_rules.is_empty();
        }

        Ok(FlagResolveResult {
            resolved_value,
            updates,
        })
    }

    /// Get an attribute value from the [EvaluationContext] struct, addressed by a path specification.
    /// If the struct is `{user:{name:"roug",id:42}}`, then getting the `"user.name"` field will return
    /// the value `"roug"`.
    pub fn get_attribute_value(&self, field_path: &str) -> &Value {
        let mut path_parts = field_path.split('.').peekable();
        let mut s = &self.evaluation_context.context;

        while let Some(field) = path_parts.next() {
            match s.fields.get(field) {
                Some(value) => {
                    if path_parts.peek().is_none() {
                        // we are at the end of the path, return the value
                        return value;
                    } else if let Some(Kind::StructValue(struct_value)) = &value.kind {
                        // if we are not at the end of the path, and the value is a struct, continue
                        s = struct_value;
                    } else {
                        // if we are not at the end of the path, but the value is not a struct, return null
                        return &NULL;
                    }
                }
                None => {
                    // non-struct value addressed with .-operator
                    return &NULL;
                }
            }
        }

        &NULL
    }

    pub fn segment_match(&self, segment: &Segment, unit: &str) -> Fallible<bool> {
        self.segment_match_internal(segment, unit, &mut HashSet::new())
    }

    fn segment_match_internal(
        &self,
        segment: &Segment,
        unit: &str,
        visited: &mut HashSet<String>,
    ) -> Fallible<bool> {
        if visited.contains(&segment.name) {
            fail!("circular segment dependency found");
        }
        visited.insert(segment.name.clone());

        if !self.targeting_match(segment, unit, visited)? {
            return Ok(false);
        }

        // check bitset
        let Some(bitset) = self.state.bitsets.get(&segment.name) else {
            return Ok(true);
        }; // todo: would this match or not?
        let salted_unit = self.client.account.salt_unit(unit)?;
        let unit_hash = bucket(hash(&salted_unit), BUCKETS)?;
        if unit_hash >= bitset.len() {
            return Ok(false);
        }
        Ok(bitset[unit_hash])
    }

    fn targeting_match(
        &self,
        segment: &Segment,
        unit: &str,
        visited: &mut HashSet<String>,
    ) -> Fallible<bool> {
        let Some(targeting) = &segment.targeting else {
            return Ok(true);
        };
        let mut criterion_evaluator = |id: &String| {
            let Some(Criterion {
                criterion: Some(criterion),
            }) = targeting.criteria.get(id)
            else {
                return Ok(false);
            };
            match &criterion {
                criterion::Criterion::Attribute(attribute_criterion) => {
                    let expected_value_type = value::expected_value_type(attribute_criterion);
                    let attribute_value =
                        self.get_attribute_value(&attribute_criterion.attribute_name);
                    let converted =
                        value::convert_to_targeting_value(attribute_value, expected_value_type)?;
                    let wrapped = list_wrapper(&converted);

                    Ok(value::evaluate_criterion(attribute_criterion, &wrapped))
                }
                criterion::Criterion::Segment(segment_criterion) => {
                    let Some(ref_segment) = self.state.segments.get(&segment_criterion.segment)
                    else {
                        return Ok(false);
                    };

                    self.segment_match_internal(ref_segment, unit, visited)
                }
            }
        };

        let Some(expression) = &targeting.expression else {
            return Ok(true);
        };
        evaluate_expression(expression, &mut criterion_evaluator)
    }

    fn encrypt_resolve_token(
        &self,
        resolve_token: &flags_resolver::ResolveToken,
    ) -> Result<Vec<u8>, String> {
        let mut token_buf = Vec::with_capacity(resolve_token.encoded_len());
        resolve_token.encode(&mut token_buf).or_fail()?;

        H::encrypt_resolve_token(&token_buf, &self.encryption_key)
    }

    fn decrypt_resolve_token(
        &self,
        encrypted_token: &[u8],
    ) -> Result<flags_resolver::ResolveToken, String> {
        let decrypted_data = H::decrypt_resolve_token(encrypted_token, &self.encryption_key)?;

        let t = flags_resolver::ResolveToken::decode(&decrypted_data[..]).or_fail()?;
        Ok(t)
    }
}

fn to_date_time_utc(timestamp: &Timestamp) -> Option<chrono::DateTime<chrono::Utc>> {
    chrono::DateTime::from_timestamp(timestamp.seconds, timestamp.nanos as u32)
}

fn evaluate_expression(
    expression: &Expression,
    criterion_evaluator: &mut dyn FnMut(&String) -> Fallible<bool>,
) -> Fallible<bool> {
    let Some(expression) = &expression.expression else {
        return Ok(false);
    };
    match expression {
        expression::Expression::Ref(ref_) => criterion_evaluator(ref_),
        expression::Expression::Not(not) => Ok(!evaluate_expression(not, criterion_evaluator)?),
        expression::Expression::And(and) => {
            for op in &and.operands {
                if !evaluate_expression(op, criterion_evaluator)? {
                    return Ok(false);
                }
            }
            Ok(true)
        }
        expression::Expression::Or(or) => {
            for op in &or.operands {
                if evaluate_expression(op, criterion_evaluator)? {
                    return Ok(true);
                }
            }
            Ok(false)
        }
    }
}

fn list_wrapper(value: &targeting::value::Value) -> targeting::ListValue {
    match value {
        targeting::value::Value::ListValue(list_value) => list_value.clone(),
        _ => targeting::ListValue {
            values: vec![targeting::Value {
                value: Some(value.clone()),
            }],
        },
    }
}

#[derive(Debug, Clone)]
pub struct ResolvedValue<'a> {
    pub flag: &'a Flag,
    pub reason: ResolveReason,
    pub assignment_match: Option<AssignmentMatch<'a>>,
    pub fallthrough_rules: Vec<FallthroughRule<'a>>,
    pub should_apply: bool,
}

#[derive(Debug)]
pub struct FlagResolveResult<'a> {
    pub resolved_value: ResolvedValue<'a>,
    pub updates: Vec<MaterializationUpdate>,
}

impl<'a> ResolvedValue<'a> {
    fn new(flag: &'a Flag) -> Self {
        ResolvedValue {
            flag,
            reason: ResolveReason::NoSegmentMatch,
            assignment_match: Option::None,
            fallthrough_rules: vec![],
            should_apply: false,
        }
    }

    fn error(&self, reason: ResolveReason) -> Self {
        ResolvedValue {
            flag: self.flag,
            reason,
            assignment_match: Option::None,
            fallthrough_rules: self.fallthrough_rules.clone(),
            should_apply: false,
        }
    }

    fn attribute_fallthrough_rule(&mut self, rule: &'a Rule, assignment_id: &str, unit: &str) {
        self.fallthrough_rules.push(FallthroughRule {
            rule,
            assignment_id: assignment_id.to_string(),
            targeting_key: unit.to_string(),
        });
    }

    fn with_client_default_match(
        &self,
        rule: &'a Rule,
        segment: &'a Segment,
        assignment_id: &str,
        unit: &str,
    ) -> Self {
        ResolvedValue {
            flag: self.flag,
            reason: ResolveReason::Match,
            assignment_match: Option::Some(AssignmentMatch {
                rule,
                segment,
                assignment_id: assignment_id.to_string(),
                targeting_key: unit.to_string(),
                variant: Option::None,
            }),
            fallthrough_rules: self.fallthrough_rules.clone(),
            should_apply: true,
        }
    }

    fn with_variant_match(
        &self,
        rule: &'a Rule,
        segment: &'a Segment,
        variant: &'a Variant,
        assignment_id: &str,
        unit: &str,
    ) -> Self {
        ResolvedValue {
            flag: self.flag,
            reason: ResolveReason::Match,
            assignment_match: Option::Some(AssignmentMatch {
                rule,
                segment,
                assignment_id: assignment_id.to_string(),
                targeting_key: unit.to_string(),
                variant: Option::Some(variant),
            }),
            fallthrough_rules: self.fallthrough_rules.clone(),
            should_apply: true,
        }
    }
}

impl<'a> From<&ResolvedValue<'a>> for flags_resolver::ResolvedFlag {
    fn from(value: &ResolvedValue<'a>) -> Self {
        let mut resolved_flag = flags_resolver::ResolvedFlag {
            flag: value.flag.name.clone(),
            reason: value.reason as i32,
            should_apply: value.should_apply,
            ..Default::default()
        };

        if let Some(assignment_match) = &value.assignment_match {
            match assignment_match.variant {
                Some(variant) => {
                    resolved_flag.variant = variant.name.clone();
                    resolved_flag.value = variant.value.clone(); // todo: expand to schema
                    resolved_flag.flag_schema = value.flag.schema.clone();
                }
                None => {
                    resolved_flag.variant = "".to_string();
                    resolved_flag.value = Some(Struct::default());
                    resolved_flag.flag_schema =
                        Some(flags_types::flag_schema::StructFlagSchema::default())
                }
            }
        }

        resolved_flag
    }
}

impl<'a> From<&ResolvedValue<'a>> for flags_resolver::resolve_token_v1::AssignedFlag {
    fn from(value: &ResolvedValue<'a>) -> Self {
        let mut assigned_flag = flags_resolver::resolve_token_v1::AssignedFlag {
            flag: value.flag.name.clone(),
            reason: value.reason as i32,
            fallthrough_assignments: value
                .fallthrough_rules
                .iter()
                .map(
                    |fallthrough_rule| flags_resolver::events::FallthroughAssignment {
                        assignment_id: fallthrough_rule.assignment_id.clone(),
                        rule: fallthrough_rule.rule.name.clone(),
                        targeting_key: fallthrough_rule.targeting_key.clone(),
                        targeting_key_selector: fallthrough_rule
                            .rule
                            .targeting_key_selector
                            .clone(),
                    },
                )
                .collect(),
            ..Default::default()
        };

        if let Some(assignment_match) = &value.assignment_match {
            assigned_flag.assignment_id = assignment_match.assignment_id.clone();
            assigned_flag.rule = assignment_match.rule.name.clone();
            assigned_flag.segment = assignment_match.segment.name.clone();
            assigned_flag.targeting_key = assignment_match.targeting_key.clone();
            assigned_flag.targeting_key_selector =
                assignment_match.rule.targeting_key_selector.clone();
            if let Some(variant) = assignment_match.variant {
                assigned_flag.variant = variant.name.clone();
            }
        }

        assigned_flag
    }
}

#[derive(Debug, Clone)]
pub struct AssignmentMatch<'a> {
    pub rule: &'a Rule,
    pub segment: &'a Segment,
    pub assignment_id: String,
    pub targeting_key: String,
    pub variant: Option<&'a Variant>,
}

#[derive(Debug, Clone)]
pub struct FallthroughRule<'a> {
    pub rule: &'a Rule,
    pub assignment_id: String,
    pub targeting_key: String,
}

// note that the ordinal values are set to match the corresponding protobuf enum
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ResolveReason {
    // The flag was successfully resolved because one rule matched.
    Match = 1,
    // The flag could not be resolved because no rule matched.
    NoSegmentMatch = 2,
    // The flag could not be resolved because it was archived.
    FlagArchived = 4,
    // The flag could not be resolved because the targeting key field was invalid
    TargetingKeyError = 5,
}

pub fn hash(key: &str) -> u128 {
    murmur3_x64_128(key.as_bytes(), 0)
}

#[allow(clippy::arithmetic_side_effects)] // buckets != 0 checked above
pub fn bucket(hash: u128, buckets: u64) -> Fallible<usize> {
    if buckets == 0 {
        fail!(":bucket.zero_buckets");
    }
    // convert u128 to u64 to match what we do in the java resolver
    let hash_long: u64 = hash as u64;

    // don't ask me why
    Ok(((hash_long >> 4) % buckets) as usize)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::proto::confidence::flags::resolver::v1::{ResolveFlagsResponse, Sdk};

    const EXAMPLE_STATE: &[u8] = include_bytes!("../test-payloads/resolver_state.pb");
    const SECRET: &str = "mkjJruAATQWjeY7foFIWfVAcBWnci2YF";

    const ENCRYPTION_KEY: Bytes = Bytes::from_static(&[0; 16]);

    struct L;

    impl Host for L {
        fn log_resolve(
            _resolve_id: &str,
            _evaluation_context: &Struct,
            _values: &[ResolvedValue<'_>],
            _client: &Client,
            _sdk: &Option<Sdk>,
        ) {
            // In tests, we don't need to print anything
        }

        fn log_assign(
            _resolve_id: &str,
            _evaluation_context: &Struct,
            _assigned_flag: &[FlagToApply],
            _client: &Client,
            _sdk: &Option<Sdk>,
        ) {
            // In tests, we don't need to print anything
        }
    }

    #[test]
    fn test_random_alphanumeric() {
        let rnd = L::random_alphanumeric(32);
        let re = regex::Regex::new(r"^[a-zA-Z0-9]{32}$").unwrap();
        assert!(re.is_match(&rnd));
    }

    #[test]
    fn test_parse_state_bitsets() {
        let state = ResolverState::from_proto(
            EXAMPLE_STATE.to_owned().try_into().unwrap(),
            "confidence-demo-june",
        )
        .unwrap();

        let bitvec = state.bitsets.get("segments/qnbpewfufewyn5rpsylm").unwrap();
        let bitvec2 = state.bitsets.get("segments/h2f3kemn2nqbnc7k5lk2").unwrap();

        assert_eq!(bitvec.count_ones(), 555600);
        assert_eq!(bitvec2.count_ones(), 555600);

        // assert that we read the bytes in LSB order
        let first_bits: Vec<bool> = (0..16).map(|i| bitvec[i]).collect();
        let expected_first_bits = vec![
            false, false, false, true, false, false, true, false, true, true, false, true, true,
            true, true, true,
        ];
        assert_eq!(first_bits, expected_first_bits);
    }

    #[test]
    fn test_parse_state_secrets() {
        let state = ResolverState::from_proto(
            EXAMPLE_STATE.to_owned().try_into().unwrap(),
            "confidence-demo-june",
        )
        .unwrap();

        let account_client = state
            .secrets
            .get("mkjJruAATQWjeY7foFIWfVAcBWnci2YF")
            .unwrap();
        assert_eq!(account_client.client_name, "clients/cqzy4juldrvnz0z1uedj");
        assert_eq!(
            account_client.client_credential_name,
            "clients/cqzy4juldrvnz0z1uedj/clientCredentials/yejholwrnjfewftakun8"
        );
    }

    #[test]
    fn test_hash() {
        let account = Account {
            name: "accounts/confidence-test".to_string(),
        };
        let bucket = bucket(hash(&account.salt_unit("roug").unwrap()), BUCKETS).unwrap();
        assert_eq!(bucket, 567493); // test matching bucketing result from the java randomizer
    }

    #[test]
    fn test_bucket_zero() {
        let account = Account {
            name: "accounts/confidence-test".to_string(),
        };
        let result = bucket(hash(&account.salt_unit("roug").unwrap()), 0);
        assert!(result.is_err()); // bucket count of 0 should return error
    }

    #[test]
    fn test_account_salt() {
        let account = Account {
            name: "accounts/test".to_string(),
        };

        assert_eq!(account.salt(), Ok("MegaSalt-test".into()));
    }

    #[test]
    fn test_resolve_flag() {
        let state = ResolverState::from_proto(
            EXAMPLE_STATE.to_owned().try_into().unwrap(),
            "confidence-demo-june",
        )
        .unwrap();

        {
            let context_json = r#"{"visitor_id": "tutorial_visitor"}"#;
            let resolver: AccountResolver<'_, L> = state
                .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
                .unwrap();
            let flag = resolver.state.flags.get("flags/tutorial-feature").unwrap();
            let resolve_result = resolver.resolve_flag(flag, BTreeMap::new()).unwrap();
            let resolved_value = &resolve_result.resolved_value;
            let assignment_match = resolved_value.assignment_match.as_ref().unwrap();

            assert_eq!(
                assignment_match.rule.name,
                "flags/tutorial-feature/rules/tutorial-visitor-override"
            );
            assert_eq!(
                assignment_match.variant.unwrap().name,
                "flags/tutorial-feature/variants/exciting-welcome"
            );
            assert_eq!(resolved_value.should_apply, true);
        }

        {
            let context_json = r#"{"visitor_id": "tutorial_visitor"}"#;
            let resolver: AccountResolver<'_, L> = state
                .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
                .unwrap();
            let flag = resolver.state.flags.get("flags/tutorial-feature").unwrap();
            let assignment_match = resolver
                .resolve_flag(flag, BTreeMap::new())
                .unwrap()
                .resolved_value
                .assignment_match
                .unwrap();

            assert_eq!(
                assignment_match.rule.name,
                "flags/tutorial-feature/rules/tutorial-visitor-override"
            );
            assert_eq!(
                assignment_match.variant.unwrap().name,
                "flags/tutorial-feature/variants/exciting-welcome"
            );
        }
    }
    #[test]
    fn test_resolve_flags() {
        let state = ResolverState::from_proto(
            EXAMPLE_STATE.to_owned().try_into().unwrap(),
            "confidence-demo-june",
        )
        .unwrap();

        {
            let context_json = r#"{"visitor_id": "tutorial_visitor"}"#;
            let resolver: AccountResolver<'_, L> = state
                .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
                .unwrap();

            let resolve_flag_req = flags_resolver::ResolveFlagsRequest {
                evaluation_context: Some(Struct::default()),
                client_secret: SECRET.to_string(),
                flags: vec!["flags/tutorial-feature".to_string()],
                apply: false,
                sdk: Some(Sdk {
                    sdk: None,
                    version: "0.1.0".to_string(),
                }),
            };

            let response: ResolveFlagsResponse = resolver.resolve_flags(&resolve_flag_req).unwrap();
            assert_eq!(response.resolved_flags.len(), 1);
            let flag = response.resolved_flags.get(0).unwrap();

            let decrypted_token = resolver
                .decrypt_resolve_token(&response.resolve_token)
                .unwrap();
            match decrypted_token.resolve_token {
                Some(flags_resolver::resolve_token::ResolveToken::TokenV1(token)) => {
                    assert_eq!(token.resolve_id, response.resolve_id);
                    assert_eq!(token.assignments.len(), response.resolved_flags.len());

                    let assignment = token.assignments.get("flags/tutorial-feature").unwrap();

                    assert_eq!(assignment.flag, "flags/tutorial-feature");
                    assert_eq!(
                        assignment.assignment_id,
                        "flags/tutorial-feature/variants/exciting-welcome"
                    );
                    assert_eq!(
                        assignment.variant,
                        "flags/tutorial-feature/variants/exciting-welcome"
                    );
                    assert_eq!(
                        assignment.rule,
                        "flags/tutorial-feature/rules/tutorial-visitor-override"
                    );

                    assert_eq!(assignment.flag, flag.flag);
                    assert_eq!(assignment.variant, flag.variant);
                }
                _ => panic!("Unexpected resolve token type"),
            }

            assert!(resolver.state.flags.contains_key("flags/tutorial-feature"));
            assert_eq!(true, flag.should_apply);
        }
    }

    #[test]
    fn test_resolve_flags_fallthrough() {
        let state = ResolverState::from_proto(
            EXAMPLE_STATE.to_owned().try_into().unwrap(),
            "confidence-demo-june",
        )
        .unwrap();

        // Single rule
        {
            let context_json = r#"{"visitor_id": "57"}"#;
            let resolver: AccountResolver<'_, L> = state
                .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
                .unwrap();

            let resolve_flag_req = flags_resolver::ResolveFlagsRequest {
                evaluation_context: Some(Struct::default()),
                client_secret: SECRET.to_string(),
                flags: vec!["flags/fallthrough-test-1".to_string()],
                apply: false,
                sdk: Some(Sdk {
                    sdk: None,
                    version: "0.1.0".to_string(),
                }),
            };

            let response: ResolveFlagsResponse = resolver.resolve_flags(&resolve_flag_req).unwrap();
            assert_eq!(response.resolved_flags.len(), 1);
            let flag = response.resolved_flags.get(0).unwrap();

            let decrypted_token = resolver
                .decrypt_resolve_token(&response.resolve_token)
                .unwrap();
            match decrypted_token.resolve_token {
                Some(flags_resolver::resolve_token::ResolveToken::TokenV1(token)) => {
                    assert_eq!(token.resolve_id, response.resolve_id);
                    assert_eq!(token.assignments.len(), response.resolved_flags.len());

                    let assignment = token.assignments.get("flags/fallthrough-test-1").unwrap();
                    assert_eq!(assignment.flag, "flags/fallthrough-test-1");
                    assert_eq!(assignment.targeting_key, "");
                    assert_eq!(assignment.targeting_key_selector, "");
                    assert_eq!(assignment.segment, "");
                    assert_eq!(assignment.variant, "");
                    assert_eq!(assignment.rule, "");
                    assert_eq!(ResolveReason::NoSegmentMatch as i32, flag.reason);
                    assert_eq!(assignment.assignment_id, "");

                    let expected_fallthrough = flags_resolver::events::FallthroughAssignment {
                        rule: "flags/fallthrough-test-1/rules/gdbiknjycxvmc6wu7zzz".to_string(),
                        assignment_id: "control".to_string(),
                        targeting_key: "57".to_string(),
                        targeting_key_selector: "visitor_id".to_string(),
                    };

                    assert_eq!(assignment.fallthrough_assignments.len(), 1);
                    assert_eq!(assignment.fallthrough_assignments[0], expected_fallthrough);
                }
                _ => panic!("Unexpected resolve token type"),
            }

            assert_eq!(true, flag.should_apply);
        }

        // Fallthrough to second rule
        {
            let context_json = r#"{"visitor_id": "26"}"#;
            let resolver: AccountResolver<'_, L> = state
                .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
                .unwrap();

            let resolve_flag_req = flags_resolver::ResolveFlagsRequest {
                evaluation_context: Some(Struct::default()),
                client_secret: SECRET.to_string(),
                flags: vec!["flags/fallthrough-test-2".to_string()],
                apply: false,
                sdk: Some(Sdk {
                    sdk: None,
                    version: "0.1.0".to_string(),
                }),
            };

            let response: ResolveFlagsResponse = resolver.resolve_flags(&resolve_flag_req).unwrap();
            assert_eq!(response.resolved_flags.len(), 1);
            let flag = response.resolved_flags.get(0).unwrap();

            let decrypted_token = resolver
                .decrypt_resolve_token(&response.resolve_token)
                .unwrap();
            match decrypted_token.resolve_token {
                Some(flags_resolver::resolve_token::ResolveToken::TokenV1(token)) => {
                    assert_eq!(token.resolve_id, response.resolve_id);
                    assert_eq!(token.assignments.len(), response.resolved_flags.len());

                    let assignment = token.assignments.get("flags/fallthrough-test-2").unwrap();
                    assert_eq!(assignment.flag, "flags/fallthrough-test-2");
                    assert_eq!(assignment.targeting_key, "26");
                    assert_eq!(assignment.targeting_key_selector, "visitor_id");
                    assert_eq!(assignment.segment, "segments/dvlllobhnpxcojqn6vfa");
                    assert_eq!(
                        assignment.variant,
                        "flags/fallthrough-test-2/variants/enabled"
                    );
                    assert_eq!(
                        assignment.rule,
                        "flags/fallthrough-test-2/rules/oxl1yqqjj1aqyiuvf9al"
                    );
                    assert_eq!(ResolveReason::Match as i32, flag.reason);
                    assert_eq!(assignment.assignment_id, "");

                    let expected_fallthrough = flags_resolver::events::FallthroughAssignment {
                        rule: "flags/fallthrough-test-2/rules/wwzea3vq89gwtcufe9ou".to_string(),
                        assignment_id: "control".to_string(),
                        targeting_key: "26".to_string(),
                        targeting_key_selector: "visitor_id".to_string(),
                    };

                    assert_eq!(assignment.fallthrough_assignments.len(), 1);
                    assert_eq!(assignment.fallthrough_assignments[0], expected_fallthrough);
                }
                _ => panic!("Unexpected resolve token type"),
            }

            assert_eq!(true, flag.should_apply);
        }
    }

    #[test]
    fn test_resolve_flags_no_match() {
        let state = ResolverState::from_proto(
            EXAMPLE_STATE.to_owned().try_into().unwrap(),
            "confidence-demo-june",
        )
        .unwrap();

        {
            let context_json = r#"{}"#; // NO CONTEXT
            let resolver: AccountResolver<'_, L> = state
                .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
                .unwrap();

            let resolve_flag_req = flags_resolver::ResolveFlagsRequest {
                evaluation_context: Some(Struct::default()),
                client_secret: SECRET.to_string(),
                flags: vec!["flags/tutorial-feature".to_string()],
                apply: false,
                sdk: Some(Sdk {
                    sdk: None,
                    version: "0.1.0".to_string(),
                }),
            };

            let response: ResolveFlagsResponse = resolver.resolve_flags(&resolve_flag_req).unwrap();
            assert_eq!(response.resolved_flags.len(), 1);
            assert!(resolver.state.flags.contains_key("flags/tutorial-feature"));

            let flag = response.resolved_flags.get(0).unwrap();
            assert_eq!(false, flag.should_apply);
            assert_eq!(ResolveReason::NoSegmentMatch as i32, flag.reason);
        }
    }

    #[test]
    fn test_resolve_flags_apply_logging() {
        let state = ResolverState::from_proto(
            EXAMPLE_STATE.to_owned().try_into().unwrap(),
            "confidence-demo-june",
        )
        .unwrap();

        // Custom logger that tracks what gets logged
        struct TestLogger {
            assign_logs: std::sync::Mutex<Vec<String>>,
        }

        impl Host for TestLogger {
            fn log_resolve(
                _resolve_id: &str,
                _evaluation_context: &Struct,
                _values: &[ResolvedValue<'_>],
                _client: &Client,
                _sdk: &Option<Sdk>,
            ) {
                // Do nothing for resolve logs
            }

            fn log_assign(
                resolve_id: &str,
                _evaluation_context: &Struct,
                assigned_flag: &[FlagToApply],
                _client: &Client,
                _sdk: &Option<Sdk>,
            ) {
                let mut logs = TestLogger::get_instance()
                    .assign_logs
                    .try_lock()
                    .expect("mutex is locked or poisoned");
                assigned_flag.iter().for_each(|f| {
                    let log_entry = format!("{}:{}", resolve_id, f.assigned_flag.flag);
                    logs.push(log_entry);
                });
            }
        }

        impl TestLogger {
            fn get_instance() -> &'static TestLogger {
                static INSTANCE: std::sync::OnceLock<TestLogger> = std::sync::OnceLock::new();
                INSTANCE.get_or_init(|| TestLogger {
                    assign_logs: std::sync::Mutex::new(Vec::new()),
                })
            }

            fn clear_logs() {
                if let Ok(mut logs) = TestLogger::get_instance().assign_logs.lock() {
                    logs.clear();
                }
            }

            fn get_logs() -> Vec<String> {
                TestLogger::get_instance()
                    .assign_logs
                    .lock()
                    .unwrap()
                    .clone()
            }
        }

        // Test 1: NO_MATCH case with apply=true should NOT log assignments
        {
            TestLogger::clear_logs();
            let context_json = r#"{}"#; // NO CONTEXT
            let resolver: AccountResolver<'_, TestLogger> = state
                .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
                .unwrap();

            let resolve_flag_req = flags_resolver::ResolveFlagsRequest {
                evaluation_context: Some(Struct::default()),
                client_secret: SECRET.to_string(),
                flags: vec!["flags/tutorial-feature".to_string()],
                apply: true,
                sdk: Some(Sdk {
                    sdk: None,
                    version: "0.1.0".to_string(),
                }),
            };

            let response: ResolveFlagsResponse = resolver.resolve_flags(&resolve_flag_req).unwrap();
            let flag = response.resolved_flags.get(0).unwrap();
            assert_eq!(false, flag.should_apply);
            assert_eq!(ResolveReason::NoSegmentMatch as i32, flag.reason);

            // Verify that no assignment was logged
            let logs = TestLogger::get_logs();
            assert_eq!(
                logs.len(),
                0,
                "NO_MATCH flags should not be logged when apply=true"
            );
        }

        // Test 2: MATCH case with apply=true SHOULD log assignments
        {
            TestLogger::clear_logs();
            let context_json = r#"{"visitor_id": "tutorial_visitor"}"#; // This should match
            let resolver: AccountResolver<'_, TestLogger> = state
                .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
                .unwrap();

            let resolve_flag_req = flags_resolver::ResolveFlagsRequest {
                evaluation_context: Some(Struct::default()),
                client_secret: SECRET.to_string(),
                flags: vec!["flags/tutorial-feature".to_string()],
                apply: true,
                sdk: Some(Sdk {
                    sdk: None,
                    version: "0.1.0".to_string(),
                }),
            };

            let response: ResolveFlagsResponse = resolver.resolve_flags(&resolve_flag_req).unwrap();
            let flag = response.resolved_flags.get(0).unwrap();
            assert_eq!(true, flag.should_apply);
            assert_eq!(ResolveReason::Match as i32, flag.reason);

            // Verify that assignment was logged
            let logs = TestLogger::get_logs();
            assert_eq!(
                logs.len(),
                1,
                "MATCH flags should be logged when apply=true"
            );
            assert!(
                logs[0].contains("flags/tutorial-feature"),
                "Log should contain the flag name"
            );
        }
    }

    #[test]
    fn test_targeting_key_integer_supported() {
        let state = ResolverState::from_proto(
            EXAMPLE_STATE.to_owned().try_into().unwrap(),
            "confidence-demo-june",
        )
        .unwrap();

        // Using integer for visitor_id should be treated as string and work
        let context_json = r#"{"visitor_id": 26}"#;
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        let flag = resolver
            .state
            .flags
            .get("flags/fallthrough-test-2")
            .unwrap();
        let resolve_result = resolver.resolve_flag(flag, BTreeMap::new()).unwrap();
        let resolved_value = &resolve_result.resolved_value;

        assert_eq!(resolved_value.reason as i32, ResolveReason::Match as i32);
        let assignment_match = resolved_value.assignment_match.as_ref().unwrap();
        assert_eq!(assignment_match.targeting_key, "26");
    }

    #[test]
    fn test_targeting_key_fractional_rejected() {
        let state = ResolverState::from_proto(
            EXAMPLE_STATE.to_owned().try_into().unwrap(),
            "confidence-demo-june",
        )
        .unwrap();

        // Fractional number for visitor_id should be rejected
        let context_json = r#"{"visitor_id": 26.5}"#;
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        let flag = resolver
            .state
            .flags
            .get("flags/fallthrough-test-2")
            .unwrap();
        let resolve_result = resolver.resolve_flag(flag, BTreeMap::new()).unwrap();
        let resolved_value = &resolve_result.resolved_value;

        assert_eq!(
            resolved_value.reason as i32,
            ResolveReason::TargetingKeyError as i32
        );
        assert!(resolved_value.assignment_match.is_none());
    }

    // eq rules

    #[test]
    fn test_segment_match_eq_bool_t() {
        let rule_json = r#"{
            "attributeName": "client.mobile",
            "eqRule": {
                "value": { "boolValue": true }
            }
        }"#;
        let context_json = r#"{
            "user_id": "test",
            "client": {
                "mobile": true
            }
        }"#;
        let (segment, state) = parse_segment(rule_json);
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        assert!(resolver.segment_match(&segment, "test").unwrap());
    }

    #[test]
    fn test_segment_match_eq_bool_f() {
        let rule_json = r#"{
            "attributeName": "client.mobile",
            "eqRule": {
                "value": { "boolValue": true }
            }
        }"#;
        let context_json = r#"{
            "user_id": "test",
            "client": {
                "mobile": false
            }
        }"#;
        let (segment, state) = parse_segment(rule_json);
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        assert!(!resolver.segment_match(&segment, "test").unwrap());
    }

    #[test]
    fn test_segment_match_eq_bool_l() {
        let rule_json = r#"{
            "attributeName": "client.mobile",
            "eqRule": {
                "value": { "boolValue": true }
            }
        }"#;
        let context_json = r#"{
            "user_id": "test",
            "client": {
                "mobile": [true, false]
            }
        }"#;
        let (segment, state) = parse_segment(rule_json);
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        assert!(resolver.segment_match(&segment, "test").unwrap());
    }

    #[test]
    fn test_segment_match_eq_bool_from_string_l() {
        let rule_json = r#"{
            "attributeName": "client.mobile",
            "eqRule": {
                "value": { "boolValue": true }
            }
        }"#;
        let context_json = r#"{
            "user_id": "test",
            "client": {
                "mobile": ["true", "false"]
            }
        }"#;
        let (segment, state) = parse_segment(rule_json);
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        assert!(resolver.segment_match(&segment, "test").unwrap());
    }

    #[test]
    fn test_segment_match_eq_number_t() {
        let rule_json = r#"{
            "attributeName": "client.score",
            "eqRule": {
                "value": { "numberValue": 42.1 }
            }
        }"#;
        let context_json = r#"{
            "user_id": "test",
            "client": {
                "score": 42.1
            }
        }"#;
        let (segment, state) = parse_segment(rule_json);
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        assert!(resolver.segment_match(&segment, "test").unwrap());
    }

    #[test]
    fn test_segment_match_eq_number_f() {
        let rule_json = r#"{
            "attributeName": "client.score",
            "eqRule": {
                "value": { "numberValue": 42.1 }
            }
        }"#;
        let context_json = r#"{
            "user_id": "test",
            "client": {
                "score": 41.0
            }
        }"#;
        let (segment, state) = parse_segment(rule_json);
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        assert!(!resolver.segment_match(&segment, "test").unwrap());
    }

    #[test]
    fn test_segment_match_eq_number_l() {
        let rule_json = r#"{
            "attributeName": "client.score",
            "eqRule": {
                "value": { "numberValue": 42.1 }
            }
        }"#;
        let context_json = r#"{
            "user_id": "test",
            "client": {
                "score": [41.0, 42.1]
            }
        }"#;
        let (segment, state) = parse_segment(rule_json);
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        assert!(resolver.segment_match(&segment, "test").unwrap());
    }

    #[test]
    fn test_segment_match_eq_string_t() {
        let rule_json = r#"{
            "attributeName": "client.name",
            "eqRule": {
                "value": { "stringValue": "Bob" }
            }
        }"#;
        let context_json = r#"{
            "user_id": "test",
            "client": {
                "name": "Bob"
            }
        }"#;
        let (segment, state) = parse_segment(rule_json);
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        assert!(resolver.segment_match(&segment, "test").unwrap());
    }

    #[test]
    fn test_segment_match_eq_string_f() {
        let rule_json = r#"{
            "attributeName": "client.name",
            "eqRule": {
                "value": { "stringValue": "Bob" }
            }
        }"#;
        let context_json = r#"{
            "user_id": "test",
            "client": {
                "name": "Alice"
            }
        }"#;
        let (segment, state) = parse_segment(rule_json);
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        assert!(!resolver.segment_match(&segment, "test").unwrap());
    }

    #[test]
    fn test_segment_match_eq_string_l() {
        let rule_json = r#"{
            "attributeName": "client.name",
            "eqRule": {
                "value": { "stringValue": "Bob" }
            }
        }"#;
        let context_json = r#"{
            "user_id": "test",
            "client": {
                "name": ["Alice", "Bob"]
            }
        }"#;
        let (segment, state) = parse_segment(rule_json);
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        assert!(resolver.segment_match(&segment, "test").unwrap());
    }

    #[test]
    fn test_segment_match_eq_timestamp_t() {
        let rule_json = r#"{
            "attributeName": "client.buildDate",
            "eqRule": {
                "value": { "timestampValue": "2022-11-17T15:16:17.118Z" }
            }
        }"#;
        let context_json = r#"{
            "user_id": "test",
            "client": {
                "buildDate": "2022-11-17T15:16:17.118Z"
            }
        }"#;
        let (segment, state) = parse_segment(rule_json);
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        assert!(resolver.segment_match(&segment, "test").unwrap());
    }

    #[test]
    fn test_segment_match_eq_timestamp_f() {
        let rule_json = r#"{
            "attributeName": "client.buildDate",
            "eqRule": {
                "value": { "timestampValue": "2022-11-17T15:16:17.118Z" }
            }
        }"#;
        let context_json = r#"{
            "user_id": "test",
            "client": {
                "buildDate": "2022-11-17T00:00:00Z"
            }
        }"#;
        let (segment, state) = parse_segment(rule_json);
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        assert!(!resolver.segment_match(&segment, "test").unwrap());
    }

    #[test]
    fn test_segment_match_eq_timestamp_l() {
        let rule_json = r#"{
            "attributeName": "client.buildDate",
            "eqRule": {
                "value": { "timestampValue": "2022-11-17T15:16:17.118Z" }
            }
        }"#;
        let context_json = r#"{
            "user_id": "test",
            "client": {
                "buildDate": ["2022-11-17T00:00:00Z", "2022-11-17T15:16:17.118Z"]
            }
        }"#;
        let (segment, state) = parse_segment(rule_json);
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        assert!(resolver.segment_match(&segment, "test").unwrap());
    }

    #[test]
    fn test_segment_match_eq_version_t() {
        let rule_json = r#"{
            "attributeName": "client.version",
            "eqRule": {
                "value": { "versionValue": { "version": "1.4.2" } }
            }
        }"#;
        let context_json = r#"{
            "user_id": "test",
            "client": {
                "version": "1.4.2"
            }
        }"#;
        let (segment, state) = parse_segment(rule_json);
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        assert!(resolver.segment_match(&segment, "test").unwrap());
    }

    #[test]
    fn test_segment_match_eq_version_f() {
        let rule_json = r#"{
            "attributeName": "client.version",
            "eqRule": {
                "value": { "versionValue": { "version": "1.4.2" } }
            }
        }"#;
        let context_json = r#"{
            "user_id": "test",
            "client": {
                "version": "1.4.1"
            }
        }"#;
        let (segment, state) = parse_segment(rule_json);
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        assert!(!resolver.segment_match(&segment, "test").unwrap());
    }

    #[test]
    fn test_segment_match_eq_version_l() {
        let rule_json = r#"{
            "attributeName": "client.version",
            "eqRule": {
                "value": { "versionValue": { "version": "1.4.2" } }
            }
        }"#;
        let context_json = r#"{
            "user_id": "test",
            "client": {
                "version": ["1.4.3", "1.4.2"]
            }
        }"#;
        let (segment, state) = parse_segment(rule_json);
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        assert!(resolver.segment_match(&segment, "test").unwrap());
    }

    // set rules

    #[test]
    fn test_segment_match_set_bool_t() {
        let rule_json = r#"{
            "attributeName": "client.mobile",
            "setRule": {
                "values": [{ "boolValue": true }, { "boolValue": false }]
            }
        }"#;
        let context_json = r#"{
            "user_id": "test",
            "client": {
                "mobile": true
            }
        }"#;
        let (segment, state) = parse_segment(rule_json);
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        assert!(resolver.segment_match(&segment, "test").unwrap());
    }

    #[test]
    fn test_segment_match_set_bool_f() {
        let rule_json = r#"{
            "attributeName": "client.mobile",
            "setRule": {
                "values": [{ "boolValue": true }, { "boolValue": false }]
            }
        }"#;
        let context_json = r#"{
            "user_id": "test",
            "client": {
                "not": "the field you are looking for"
            }
        }"#;
        let (segment, state) = parse_segment(rule_json);
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        assert!(!resolver.segment_match(&segment, "test").unwrap());
    }

    #[test]
    fn test_segment_match_set_bool_l() {
        let rule_json = r#"{
            "attributeName": "client.mobile",
            "setRule": {
                "values": [{ "boolValue": true }, { "boolValue": false }]
            }
        }"#;
        let context_json = r#"{
            "user_id": "test",
            "client": {
                "mobile": [true, false]
            }
        }"#;
        let (segment, state) = parse_segment(rule_json);
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        assert!(resolver.segment_match(&segment, "test").unwrap());
    }

    #[test]
    fn test_segment_match_set_number_t() {
        let rule_json = r#"{
            "attributeName": "client.score",
            "setRule": {
                "values": [{ "numberValue": 42.1 }, { "numberValue": 41.0 }]
            }
        }"#;
        let context_json = r#"{
            "user_id": "test",
            "client": {
                "score": 41.0
            }
        }"#;
        let (segment, state) = parse_segment(rule_json);
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        assert!(resolver.segment_match(&segment, "test").unwrap());
    }

    #[test]
    fn test_segment_match_set_number_f() {
        let rule_json = r#"{
            "attributeName": "client.score",
            "setRule": {
                "values": [{ "numberValue": 42.1 }, { "numberValue": 41.0 }]
            }
        }"#;
        let context_json = r#"{
            "user_id": "test",
            "client": {
                "score": 40.0
            }
        }"#;
        let (segment, state) = parse_segment(rule_json);
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        assert!(!resolver.segment_match(&segment, "test").unwrap());
    }

    #[test]
    fn test_segment_match_set_number_l() {
        let rule_json = r#"{
            "attributeName": "client.score",
            "setRule": {
                "values": [{ "numberValue": 42.1 }, { "numberValue": 41.0 }]
            }
        }"#;
        let context_json = r#"{
            "user_id": "test",
            "client": {
                "score": [40.0, 42.1]
            }
        }"#;
        let (segment, state) = parse_segment(rule_json);
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        assert!(resolver.segment_match(&segment, "test").unwrap());
    }

    #[test]
    fn test_segment_match_set_string_t() {
        let rule_json = r#"{
            "attributeName": "client.name",
            "setRule": {
                "values": [{ "stringValue": "Alice" }, { "stringValue": "Bob" }]
            }
        }"#;
        let context_json = r#"{
            "user_id": "test",
            "client": {
                "name": "Bob"
            }
        }"#;
        let (segment, state) = parse_segment(rule_json);
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        assert!(resolver.segment_match(&segment, "test").unwrap());
    }

    #[test]
    fn test_segment_match_set_string_f() {
        let rule_json = r#"{
            "attributeName": "client.name",
            "setRule": {
                "values": [{ "stringValue": "Alice" }, { "stringValue": "Bob" }]
            }
        }"#;
        let context_json = r#"{
            "user_id": "test",
            "client": {
                "name": "Joe"
            }
        }"#;
        let (segment, state) = parse_segment(rule_json);
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        assert!(!resolver.segment_match(&segment, "test").unwrap());
    }

    #[test]
    fn test_segment_match_set_string_l() {
        let rule_json = r#"{
            "attributeName": "client.name",
            "setRule": {
                "values": [{ "stringValue": "Alice" }, { "stringValue": "Bob" }]
            }
        }"#;
        let context_json = r#"{
            "user_id": "test",
            "client": {
                "name": ["Bob", "Joe"]
            }
        }"#;
        let (segment, state) = parse_segment(rule_json);
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        assert!(resolver.segment_match(&segment, "test").unwrap());
    }

    #[test]
    fn test_segment_match_set_timestamp_t() {
        let rule_json = r#"{
            "attributeName": "client.buildDate",
            "setRule": {
                "values": [
                    { "timestampValue": "2022-11-17T15:16:17.118Z" },
                    { "timestampValue": "2022-11-17T00:00:00Z" }
                ]
            }
        }"#;
        let context_json = r#"{
            "user_id": "test",
            "client": {
                "buildDate": "2022-11-17T15:16:17.118Z"
            }
        }"#;
        let (segment, state) = parse_segment(rule_json);
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        assert!(resolver.segment_match(&segment, "test").unwrap());
    }

    #[test]
    fn test_segment_match_set_timestamp_f() {
        let rule_json = r#"{
            "attributeName": "client.buildDate",
            "setRule": {
                "values": [
                    { "timestampValue": "2022-11-17T15:16:17.118Z" },
                    { "timestampValue": "2022-11-17T00:00:00Z" }
                ]
            }
        }"#;
        let context_json = r#"{
            "user_id": "test",
            "client": {
                "buildDate": "2022-11-17T01:00:00Z"
            }
        }"#;
        let (segment, state) = parse_segment(rule_json);
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        assert!(!resolver.segment_match(&segment, "test").unwrap());
    }

    #[test]
    fn test_segment_match_set_timestamp_l() {
        let rule_json = r#"{
            "attributeName": "client.buildDate",
            "setRule": {
                "values": [
                    { "timestampValue": "2022-11-17T15:16:17.118Z" },
                    { "timestampValue": "2022-11-17T00:00:00Z" }
                ]
            }
        }"#;
        let context_json = r#"{
            "user_id": "test",
            "client": {
                "buildDate": ["2022-11-17T00:00:00Z", "2022-11-17T01:00:00Z"]
            }
        }"#;
        let (segment, state) = parse_segment(rule_json);
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        assert!(resolver.segment_match(&segment, "test").unwrap());
    }

    #[test]
    fn test_segment_match_set_version_t() {
        let rule_json = r#"{
            "attributeName": "client.version",
            "setRule": {
                "values": [
                    { "versionValue": { "version": "1.4.2" } },
                    { "versionValue": { "version": "1.4.3" } }
                ]
            }
        }"#;
        let context_json = r#"{
            "user_id": "test",
            "client": {
                "version": "1.4.2"
            }
        }"#;
        let (segment, state) = parse_segment(rule_json);
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        assert!(resolver.segment_match(&segment, "test").unwrap());
    }

    #[test]
    fn test_segment_match_set_version_f() {
        let rule_json = r#"{
            "attributeName": "client.version",
            "setRule": {
                "values": [
                    { "versionValue": { "version": "1.4.2" } },
                    { "versionValue": { "version": "1.4.3" } }
                ]
            }
        }"#;
        let context_json = r#"{
            "user_id": "test",
            "client": {
                "version": "1.4.1"
            }
        }"#;
        let (segment, state) = parse_segment(rule_json);
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        assert!(!resolver.segment_match(&segment, "test").unwrap());
    }

    #[test]
    fn test_segment_match_set_version_l() {
        let rule_json = r#"{
            "attributeName": "client.version",
            "setRule": {
                "values": [
                    { "versionValue": { "version": "1.4.2" } },
                    { "versionValue": { "version": "1.4.3" } }
                ]
            }
        }"#;
        let context_json = r#"{
            "user_id": "test",
            "client": {
                "version": ["1.4.3", "1.4.7"]
            }
        }"#;
        let (segment, state) = parse_segment(rule_json);
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        assert!(resolver.segment_match(&segment, "test").unwrap());
    }

    // range rules

    #[test]
    fn test_segment_match_range_number_si_ei() {
        let rule_json = r#"{
            "attributeName": "client.score",
            "rangeRule": {
                "startInclusive": { "numberValue": 42.1 },
                "endInclusive": { "numberValue": 43.0 }
            }
        }"#;

        let assert_case = |context_json: &str, expected: bool| {
            let (segment, state) = parse_segment(rule_json);
            let resolver: AccountResolver<'_, L> = state
                .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
                .unwrap();
            assert_eq!(resolver.segment_match(&segment, "test"), Ok(expected));
        };

        assert_case(r#"{"client": { "score": 42.0 }, "user_id": "test"}"#, false);
        assert_case(r#"{"client": { "score": 42.1 }, "user_id": "test"}"#, true);
        assert_case(r#"{"client": { "score": 42.5 }, "user_id": "test"}"#, true);
        assert_case(r#"{"client": { "score": 43.0 }, "user_id": "test"}"#, true);
        assert_case(r#"{"client": { "score": 43.1 }, "user_id": "test"}"#, false);
    }

    #[test]
    fn test_segment_match_range_number_si_ee() {
        let rule_json = r#"{
            "attributeName": "client.score",
            "rangeRule": {
                "startInclusive": { "numberValue": 42.1 },
                "endExclusive": { "numberValue": 43.0 }
            }
        }"#;
        let assert_case = |context_json: &str, expected: bool| {
            let (segment, state) = parse_segment(rule_json);
            let resolver: AccountResolver<'_, L> = state
                .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
                .unwrap();
            assert_eq!(resolver.segment_match(&segment, "test"), Ok(expected));
        };

        assert_case(r#"{"client": { "score": 42.0 }, "user_id": "test"}"#, false);
        assert_case(r#"{"client": { "score": 42.1 }, "user_id": "test"}"#, true);
        assert_case(r#"{"client": { "score": 42.5 }, "user_id": "test"}"#, true);
        assert_case(r#"{"client": { "score": 43.0 }, "user_id": "test"}"#, false);
        assert_case(r#"{"client": { "score": 43.1 }, "user_id": "test"}"#, false);
    }

    #[test]
    fn test_segment_match_range_number_se_ei() {
        let rule_json = r#"{
            "attributeName": "client.score",
            "rangeRule": {
                "startExclusive": { "numberValue": 42.1 },
                "endInclusive": { "numberValue": 43.0 }
            }
        }"#;
        let assert_case = |context_json: &str, expected: bool| {
            let (segment, state) = parse_segment(rule_json);
            let resolver: AccountResolver<'_, L> = state
                .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
                .unwrap();
            assert_eq!(resolver.segment_match(&segment, "test"), Ok(expected));
        };

        assert_case(r#"{"client": { "score": 42.0 }, "user_id": "test"}"#, false);
        assert_case(r#"{"client": { "score": 42.1 }, "user_id": "test"}"#, false);
        assert_case(r#"{"client": { "score": 42.5 }, "user_id": "test"}"#, true);
        assert_case(r#"{"client": { "score": 43.0 }, "user_id": "test"}"#, true);
        assert_case(r#"{"client": { "score": 43.1 }, "user_id": "test"}"#, false);
    }

    #[test]
    fn test_segment_match_range_number_se_ee() {
        let rule_json = r#"{
            "attributeName": "client.score",
            "rangeRule": {
                "startExclusive": { "numberValue": 42.1 },
                "endExclusive": { "numberValue": 43.0 }
            }
        }"#;
        let assert_case = |context_json: &str, expected: bool| {
            let (segment, state) = parse_segment(rule_json);
            let resolver: AccountResolver<'_, L> = state
                .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
                .unwrap();
            assert_eq!(resolver.segment_match(&segment, "test"), Ok(expected));
        };

        assert_case(r#"{"client": { "score": 42.0 }, "user_id": "test"}"#, false);
        assert_case(r#"{"client": { "score": 42.1 }, "user_id": "test"}"#, false);
        assert_case(r#"{"client": { "score": 42.5 }, "user_id": "test"}"#, true);
        assert_case(r#"{"client": { "score": 43.0 }, "user_id": "test"}"#, false);
        assert_case(r#"{"client": { "score": 43.1 }, "user_id": "test"}"#, false);
    }

    #[test]
    fn test_segment_match_range_number_l() {
        let rule_json = r#"{
            "attributeName": "client.score",
            "rangeRule": {
                "startInclusive": { "numberValue": 42.1 },
                "endInclusive": { "numberValue": 43.0 }
            }
        }"#;
        let context_json = r#"{
            "user_id": "test",
            "client": {
                "score": [40.1, 42.5, 44.1]
            }
        }"#;
        let (segment, state) = parse_segment(rule_json);
        let resolver: AccountResolver<'_, L> = state
            .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
            .unwrap();

        assert!(resolver.segment_match(&segment, "test").unwrap());
    }

    #[test]
    fn test_segment_match_range_timestamp_si_ei() {
        let rule_json = r#"{
            "attributeName": "client.buildDate",
            "rangeRule": {
                "startInclusive": { "timestampValue": "2022-11-17T15:16:17.118Z" },
                "endInclusive": { "timestampValue": "2022-11-18T00:00:00Z" }
            }
        }"#;
        let assert_case = |context_json: &str, expected: bool| {
            let (segment, state) = parse_segment(rule_json);
            let resolver: AccountResolver<'_, L> = state
                .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
                .unwrap();
            assert_eq!(resolver.segment_match(&segment, "test"), Ok(expected));
        };

        assert_case(
            r#"{"client": { "buildDate": "2022-11-17T15:16:00.000Z" }, "user_id": "test"}"#,
            false,
        );
        assert_case(
            r#"{"client": { "buildDate": "2022-11-17T15:16:17.118Z" }, "user_id": "test"}"#,
            true,
        );
        assert_case(
            r#"{"client": { "buildDate": "2022-11-17T15:16:30.000Z" }, "user_id": "test"}"#,
            true,
        );
        assert_case(
            r#"{"client": { "buildDate": "2022-11-18T00:00:00.000Z" }, "user_id": "test"}"#,
            true,
        );
        assert_case(
            r#"{"client": { "buildDate": "2022-11-18T15:16:17.118Z" }, "user_id": "test"}"#,
            false,
        );
    }

    #[test]
    fn test_segment_match_range_timestamp_si_ee() {
        let rule_json = r#"{
            "attributeName": "client.buildDate",
            "rangeRule": {
                "startInclusive": { "timestampValue": "2022-11-17T15:16:17.118Z" },
                "endExclusive": { "timestampValue": "2022-11-18T00:00:00Z" }
            }
        }"#;
        let assert_case = |context_json: &str, expected: bool| {
            let (segment, state) = parse_segment(rule_json);
            let resolver: AccountResolver<'_, L> = state
                .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
                .unwrap();
            assert_eq!(resolver.segment_match(&segment, "test"), Ok(expected));
        };

        assert_case(
            r#"{"client": { "buildDate": "2022-11-17T15:16:00.000Z" }, "user_id": "test"}"#,
            false,
        );
        assert_case(
            r#"{"client": { "buildDate": "2022-11-17T15:16:17.118Z" }, "user_id": "test"}"#,
            true,
        );
        assert_case(
            r#"{"client": { "buildDate": "2022-11-17T15:16:30.000Z" }, "user_id": "test"}"#,
            true,
        );
        assert_case(
            r#"{"client": { "buildDate": "2022-11-18T00:00:00.000Z" }, "user_id": "test"}"#,
            false,
        );
        assert_case(
            r#"{"client": { "buildDate": "2022-11-18T15:16:17.118Z" }, "user_id": "test"}"#,
            false,
        );
    }

    #[test]
    fn test_segment_match_range_timestamp_se_ei() {
        let rule_json = r#"{
            "attributeName": "client.buildDate",
            "rangeRule": {
                "startExclusive": { "timestampValue": "2022-11-17T15:16:17.118Z" },
                "endInclusive": { "timestampValue": "2022-11-18T00:00:00Z" }
            }
        }"#;
        let assert_case = |context_json: &str, expected: bool| {
            let (segment, state) = parse_segment(rule_json);
            let resolver: AccountResolver<'_, L> = state
                .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
                .unwrap();
            assert_eq!(resolver.segment_match(&segment, "test"), Ok(expected));
        };

        assert_case(
            r#"{"client": { "buildDate": "2022-11-17T15:16:00.000Z" }, "user_id": "test"}"#,
            false,
        );
        assert_case(
            r#"{"client": { "buildDate": "2022-11-17T15:16:17.118Z" }, "user_id": "test"}"#,
            false,
        );
        assert_case(
            r#"{"client": { "buildDate": "2022-11-17T15:16:30.000Z" }, "user_id": "test"}"#,
            true,
        );
        assert_case(
            r#"{"client": { "buildDate": "2022-11-18T00:00:00.000Z" }, "user_id": "test"}"#,
            true,
        );
        assert_case(
            r#"{"client": { "buildDate": "2022-11-18T15:16:17.118Z" }, "user_id": "test"}"#,
            false,
        );
    }

    #[test]
    fn test_segment_match_range_timestamp_se_ee() {
        let rule_json = r#"{
            "attributeName": "client.buildDate",
            "rangeRule": {
                "startExclusive": { "timestampValue": "2022-11-17T15:16:17.118Z" },
                "endExclusive": { "timestampValue": "2022-11-18T00:00:00Z" }
            }
        }"#;
        let assert_case = |context_json: &str, expected: bool| {
            let (segment, state) = parse_segment(rule_json);
            let resolver: AccountResolver<'_, L> = state
                .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
                .unwrap();
            assert_eq!(resolver.segment_match(&segment, "test"), Ok(expected));
        };

        assert_case(
            r#"{"client": { "buildDate": "2022-11-17T15:16:00.000Z" }, "user_id": "test"}"#,
            false,
        );
        assert_case(
            r#"{"client": { "buildDate": "2022-11-17T15:16:17.118Z" }, "user_id": "test"}"#,
            false,
        );
        assert_case(
            r#"{"client": { "buildDate": "2022-11-17T15:16:30.000Z" }, "user_id": "test"}"#,
            true,
        );
        assert_case(
            r#"{"client": { "buildDate": "2022-11-18T00:00:00.000Z" }, "user_id": "test"}"#,
            false,
        );
        assert_case(
            r#"{"client": { "buildDate": "2022-11-18T15:16:17.118Z" }, "user_id": "test"}"#,
            false,
        );
    }

    #[test]
    fn test_segment_match_range_version_si_ei() {
        let rule_json = r#"{
            "attributeName": "client.version",
            "rangeRule": {
                "startInclusive": { "versionValue": { "version": "1.4.0" } },
                "endInclusive": { "versionValue": { "version": "1.4.5" } }
            }
        }"#;
        let assert_case = |context_json: &str, expected: bool| {
            let (segment, state) = parse_segment(rule_json);
            let resolver: AccountResolver<'_, L> = state
                .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
                .unwrap();
            assert_eq!(resolver.segment_match(&segment, "test"), Ok(expected));
        };

        assert_case(
            r#"{"client": { "version": "1.3.0" }, "user_id": "test"}"#,
            false,
        );
        assert_case(
            r#"{"client": { "version": "1.4.0" }, "user_id": "test"}"#,
            true,
        );
        assert_case(
            r#"{"client": { "version": "1.4.2" }, "user_id": "test"}"#,
            true,
        );
        assert_case(
            r#"{"client": { "version": "1.4.5" }, "user_id": "test"}"#,
            true,
        );
        assert_case(
            r#"{"client": { "version": "1.5.1" }, "user_id": "test"}"#,
            false,
        );
    }

    #[test]
    fn test_segment_match_range_version_si_ee() {
        let rule_json = r#"{
            "attributeName": "client.version",
            "rangeRule": {
                "startInclusive": { "versionValue": { "version": "1.4.0" } },
                "endExclusive": { "versionValue": { "version": "1.4.5" } }
            }
        }"#;
        let assert_case = |context_json: &str, expected: bool| {
            let (segment, state) = parse_segment(rule_json);
            let resolver: AccountResolver<'_, L> = state
                .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
                .unwrap();
            assert_eq!(resolver.segment_match(&segment, "test"), Ok(expected));
        };

        assert_case(
            r#"{"client": { "version": "1.3.0" }, "user_id": "test"}"#,
            false,
        );
        assert_case(
            r#"{"client": { "version": "1.4.0" }, "user_id": "test"}"#,
            true,
        );
        assert_case(
            r#"{"client": { "version": "1.4.2" }, "user_id": "test"}"#,
            true,
        );
        assert_case(
            r#"{"client": { "version": "1.4.5" }, "user_id": "test"}"#,
            false,
        );
        assert_case(
            r#"{"client": { "version": "1.5.1" }, "user_id": "test"}"#,
            false,
        );
    }

    #[test]
    fn test_segment_match_range_version_se_ei() {
        let rule_json = r#"{
            "attributeName": "client.version",
            "rangeRule": {
                "startExclusive": { "versionValue": { "version": "1.4.0" } },
                "endInclusive": { "versionValue": { "version": "1.4.5" } }
            }
        }"#;
        let assert_case = |context_json: &str, expected: bool| {
            let (segment, state) = parse_segment(rule_json);
            let resolver: AccountResolver<'_, L> = state
                .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
                .unwrap();
            assert_eq!(resolver.segment_match(&segment, "test"), Ok(expected));
        };

        assert_case(
            r#"{"client": { "version": "1.3.0" }, "user_id": "test"}"#,
            false,
        );
        assert_case(
            r#"{"client": { "version": "1.4.0" }, "user_id": "test"}"#,
            false,
        );
        assert_case(
            r#"{"client": { "version": "1.4.2" }, "user_id": "test"}"#,
            true,
        );
        assert_case(
            r#"{"client": { "version": "1.4.5" }, "user_id": "test"}"#,
            true,
        );
        assert_case(
            r#"{"client": { "version": "1.5.1" }, "user_id": "test"}"#,
            false,
        );
    }

    #[test]
    fn test_segment_match_range_version_se_ee() {
        let rule_json = r#"{
            "attributeName": "client.version",
            "rangeRule": {
                "startExclusive": { "versionValue": { "version": "1.4.0" } },
                "endExclusive": { "versionValue": { "version": "1.4.5" } }
            }
        }"#;
        let assert_case = |context_json: &str, expected: bool| {
            let (segment, state) = parse_segment(rule_json);
            let resolver: AccountResolver<'_, L> = state
                .get_resolver_with_json_context(SECRET, context_json, &ENCRYPTION_KEY)
                .unwrap();
            assert_eq!(resolver.segment_match(&segment, "test"), Ok(expected));
        };

        assert_case(
            r#"{"client": { "version": "1.3.0" }, "user_id": "test"}"#,
            false,
        );
        assert_case(
            r#"{"client": { "version": "1.4.0" }, "user_id": "test"}"#,
            false,
        );
        assert_case(
            r#"{"client": { "version": "1.4.2" }, "user_id": "test"}"#,
            true,
        );
        assert_case(
            r#"{"client": { "version": "1.4.5" }, "user_id": "test"}"#,
            false,
        );
        assert_case(
            r#"{"client": { "version": "1.5.1" }, "user_id": "test"}"#,
            false,
        );
    }

    fn parse_segment(rule_json: &str) -> (Segment, ResolverState) {
        let segment_json = format!(
            r#"{{
            "targeting": {{
                "criteria": {{
                    "c": {{
                        "attribute": {rule}
                    }}
                }},
                "expression": {{
                    "ref": "c"
                }}
            }},
            "allocation": {{
                "proportion": {{
                    "value": "1.0"
                }},
                "exclusivityTags": [],
                "exclusiveTo": []
            }}
        }}"#,
            rule = rule_json
        );
        let segment: Segment = serde_json::from_str(segment_json.as_str()).unwrap();

        let mut segments = HashMap::new();
        segments.insert(segment.name.clone(), segment.clone());

        let mut secrets = HashMap::new();
        secrets.insert(
            SECRET.to_string(),
            Client {
                account: Account::new("accounts/test"),
                client_name: "clients/test".to_string(),
                client_credential_name: "clients/test/clientCredentials/abcdef".to_string(),
            },
        );

        let state = ResolverState {
            secrets,
            flags: HashMap::new(),
            segments,
            bitsets: HashMap::new(),
        };

        (segment, state)
    }
}
