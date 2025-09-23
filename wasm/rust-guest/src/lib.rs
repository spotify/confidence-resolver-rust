use std::cell::RefCell;
use std::sync::Arc;
use std::sync::LazyLock;

use arc_swap::ArcSwapOption;
use bytes::Bytes;
use prost::Message;

#[global_allocator]
static ALLOC: wee_alloc::WeeAlloc = wee_alloc::WeeAlloc::INIT;

use confidence_resolver::proto::confidence::flags::resolver::v1::{
    ResolveWithStickyRequest, WriteFlagLogsRequest,
};
use confidence_resolver::resolve_logger::ResolveLogger;
use rand::distr::Alphanumeric;
use rand::distr::SampleString;
use rand::rngs::SmallRng;
use rand::SeedableRng;
use wasm_msg;
use wasm_msg::wasm_msg_guest;
use wasm_msg::wasm_msg_host;
use wasm_msg::WasmResult;

// Include the generated protobuf code
pub mod proto {
    include!(concat!(env!("OUT_DIR"), "/rust_guest.rs"));
}
use crate::proto::SetResolverStateRequest;
use confidence_resolver::{
    proto::{
        confidence::flags::admin::v1::ResolverState as ResolverStatePb,
        confidence::flags::resolver::v1::{
            ResolveFlagResponseResult, ResolveFlagsRequest, ResolveFlagsResponse, ResolvedFlag, Sdk,
        },
        google::{Struct, Timestamp},
    },
    Client, FlagToApply, Host, ResolveReason, ResolvedValue, ResolverState,
};
use proto::{ResolveSimpleRequest, Void};

impl Into<proto::FallthroughAssignment>
    for confidence_resolver::proto::confidence::flags::resolver::v1::events::FallthroughAssignment
{
    fn into(self) -> proto::FallthroughAssignment {
        proto::FallthroughAssignment {
            rule: self.rule,
            assignment_id: self.assignment_id,
            targeting_key: self.targeting_key,
            targeting_key_selector: self.targeting_key_selector,
        }
    }
}

const VOID: Void = Void {};
const ENCRYPTION_KEY: Bytes = Bytes::from_static(&[0; 16]);

// TODO simplify by assuming single threaded?
static RESOLVER_STATE: ArcSwapOption<ResolverState> = ArcSwapOption::const_empty();
static LOGGER: LazyLock<ResolveLogger> = LazyLock::new(ResolveLogger::new);

thread_local! {
    static RNG: RefCell<SmallRng> = RefCell::new({
        let t = WasmHost::current_time();
        SmallRng::seed_from_u64((t.seconds as u64) ^ (t.nanos as u64))
    });
}

impl<'a> Into<proto::ResolvedValue> for &ResolvedValue<'a> {
    fn into(self) -> proto::ResolvedValue {
        proto::ResolvedValue {
            flag: Some(proto::Flag {
                name: self.flag.name.clone(),
            }),
            reason: convert_reason(self.reason.clone()),
            assignment_match: match (&self.assignment_match) {
                None => None,
                Some(am) => Some(proto::AssignmentMatch {
                    matched_rule: Some(proto::MatchedRule {
                        name: am.rule.clone().name,
                    }),
                    targeting_key: am.targeting_key.clone(),
                    segment: am.segment.name.clone(),
                    variant: am.variant.map(|v| proto::Variant {
                        name: v.clone().name,
                        value: v.value.clone(),
                    }),
                    assignment_id: am.assignment_id.to_string(),
                }),
            },
            fallthrough_rules: self
                .fallthrough_rules
                .iter()
                .map(|fr| proto::FallthroughRule {
                    name: fr.rule.clone().name,
                    assignment_id: fr.clone().assignment_id,
                    targeting_key: fr.clone().targeting_key,
                    targeting_key_selector: fr.rule.clone().targeting_key_selector,
                })
                .collect(),
        }
    }
}

fn convert_reason(reason: ResolveReason) -> i32 {
    match reason {
        ResolveReason::Match => i32::from(proto::ResolveReason::Match),
        ResolveReason::NoSegmentMatch => i32::from(proto::ResolveReason::NoSegmentMatch),
        ResolveReason::FlagArchived => i32::from(proto::ResolveReason::FlagArchived),
        ResolveReason::TargetingKeyError => i32::from(proto::ResolveReason::TargetingKeyError),
    }
}

fn converted_client(client: &Client) -> crate::proto::Client {
    proto::Client {
        client_name: client.client_name.clone(),
        account: Some(proto::Account {
            name: client.account.name.clone(),
        }),
        client_credential_name: client.client_credential_name.clone(),
    }
}

struct WasmHost;

impl Host for WasmHost {
    fn current_time() -> Timestamp {
        current_time(Void {}).unwrap()
    }

    fn log_resolve(
        resolve_id: &str,
        evaluation_context: &Struct,
        values: &[ResolvedValue<'_>],
        client: &Client,
        sdk: &Option<Sdk>,
    ) {
        let _ = LOGGER.log_resolve(
            resolve_id,
            evaluation_context,
            &client.client_credential_name,
            values,
        );
    }

    fn log_assign(
        resolve_id: &str,
        evaluation_context: &Struct,
        assigned_flags: &[FlagToApply],
        client: &Client,
        sdk: &Option<Sdk>,
    ) {
        let _ = LOGGER.log_assigns(resolve_id, evaluation_context, assigned_flags, client, sdk);
    }

    fn encrypt_resolve_token(token_data: &[u8], _encryption_key: &[u8]) -> Result<Vec<u8>, String> {
        Ok(token_data.to_vec())
    }

    fn decrypt_resolve_token(token_data: &[u8], _encryption_key: &[u8]) -> Result<Vec<u8>, String> {
        Ok(token_data.to_vec())
    }

    fn random_alphanumeric(len: usize) -> String {
        RNG.with_borrow_mut(|rng| Alphanumeric.sample_string(rng, len))
    }
}

/// Safely gets an owned handle to the current resolver state.
fn get_resolver_state() -> Result<Arc<ResolverState>, String> {
    let guard = RESOLVER_STATE.load();
    // Dereference the guard to get at the Option, then clone the Arc inside.
    // .cloned() on an Option<&Arc<T>> gives an Option<Arc<T>>.
    guard
        .as_ref()
        .cloned()
        .ok_or_else(|| "Resolver state not set".to_string())
}

wasm_msg_guest! {
    fn set_resolver_state(request: SetResolverStateRequest) -> WasmResult<Void> {
        let state_pb = ResolverStatePb::decode(request.state.as_slice())
            .map_err(|e| format!("Failed to decode resolver state: {}", e))?;
        let new_state = ResolverState::from_proto(state_pb, request.account_id.as_str())?;
        RESOLVER_STATE.store(Some(Arc::new(new_state)));
        Ok(VOID)
    }

    fn resolve_with_sticky(request: ResolveWithStickyRequest) -> WasmResult<ResolveFlagResponseResult> {
        let resolver_state = get_resolver_state()?;
        let resolve_request = &request.resolve_request.clone().unwrap();
        let evaluation_context = resolve_request.evaluation_context.clone().unwrap();
        let resolver = resolver_state.get_resolver::<WasmHost>(resolve_request.client_secret.as_str(), evaluation_context, &ENCRYPTION_KEY)?;
        resolver.resolve_flags_sticky(&request).into()
    }

    fn resolve(request: ResolveFlagsRequest) -> WasmResult<ResolveFlagsResponse> {
        let resolver_state = get_resolver_state()?;
        let evaluation_context = request.evaluation_context.as_ref().cloned().unwrap_or_default();
        let resolver = resolver_state.get_resolver::<WasmHost>(&request.client_secret, evaluation_context, &ENCRYPTION_KEY)?;
        resolver.resolve_flags(&request).into()
    }
    fn resolve_simple(request: ResolveSimpleRequest) -> WasmResult<ResolvedFlag> {
        let resolver_state = get_resolver_state()?;
        let evaluation_context = request.evaluation_context.as_ref().cloned().unwrap_or_default();
        let resolver = resolver_state.get_resolver::<WasmHost>(&request.client_secret, evaluation_context, &ENCRYPTION_KEY).unwrap();
        let resolve_result = resolver.resolve_flag_name(&request.name)?;
        Ok((&resolve_result.resolved_value).into())
    }
    fn flush_logs(_request:Void) -> WasmResult<WriteFlagLogsRequest> {
        LOGGER.checkpoint().map_err(|e| e.into())
    }

}

// Declare the add function as a host function
wasm_msg_host! {
    fn current_time(request: Void) -> WasmResult<Timestamp>;
}
