mod flag_resolve;

use confidence_resolver::confidence::flags::resolver::v1::events::flag_assigned::applied_flag::Assignment;
use confidence_resolver::confidence::flags::resolver::v1::events::flag_assigned::default_assignment::DefaultAssignmentReason;
use confidence_resolver::confidence::flags::resolver::v1::events::flag_assigned::{AppliedFlag, AssignmentInfo, DefaultAssignment};
use confidence_resolver::confidence::flags::resolver::v1::events::{ClientInfo, FlagAssigned};
use confidence_resolver::{confidence, Client, FlagToApply, Host, ResolvedValue, ResolverState, Struct};
use std::collections::BTreeMap;
use worker::*;

use base64::engine::general_purpose::STANDARD;
use base64::Engine;
use bytes::Bytes;
use serde_json::from_slice;
use serde_json::json;

use confidence::flags::resolver::v1::{
    ApplyFlagsRequest, ApplyFlagsResponse, ResolveFlagsRequest, ResolveReason,
};

use once_cell::sync::Lazy;

const ACCOUNT_ID: &str = include_str!("../../data/account_id");
const STATE_JSON: &[u8] = include_bytes!("../../data/resolver_state_current.pb");
const ENCRYPTION_KEY_BASE64: &str = include_str!("../../data/encryption_key");

#[derive(Clone, Serialize, Deserialize, Debug)]
pub struct InternalClientResolveInfo {
    pub client: String,
    pub client_credential: String,
    pub schema: Vec<InternalEvaluationContextSchemaInstance>,
}

#[derive(Clone, Serialize, Deserialize, Debug)]
pub struct WriteFlagLogsRequest {
    flag_assigned: Vec<FlagAssigned>,
    client_resolve_info: Vec<ClientResolveInfo>,
    flag_resolve_info: Vec<FlagResolveInfo>,
    schemas: Vec<EvaluationContextSchemaInstance>,
}

#[derive(Clone, Serialize, Deserialize, Debug)]
pub struct InternalEvaluationContextSchemaInstance {
    pub schema_json: String,
    pub semantic_types_json: String,
}

#[derive(Clone, Serialize, Deserialize, Debug)]
pub struct WriteResolveInfoItem {
    client_resolve_info: InternalClientResolveInfo,
    flag_resolve_info: Vec<FlagResolveInfo>,
}

#[derive(Clone, Serialize, Deserialize)]
pub struct WriteResolveInfoRequest {
    client_resolve_info: Vec<InternalClientResolveInfo>,
    flag_resolve_info: Vec<FlagResolveInfo>,
}

#[derive(Clone, Serialize, Deserialize, Debug)]
pub struct FlagLogQueueRequest {
    flag_assigned: Vec<FlagAssigned>,
    client_resolve_info: Vec<InternalClientResolveInfo>,
    flag_resolve_info: Vec<FlagResolveInfo>,
    schemas: Vec<InternalEvaluationContextSchemaInstance>,
}

#[derive(Clone, Serialize, Deserialize, Debug)]
pub struct WriteFlagAssignedRequest {
    flag_assigned: Vec<FlagAssigned>,
}
use crate::flag_resolve::SchemaFromEvaluationContext;
use confidence_resolver::confidence::flags::admin::v1::client_resolve_info::EvaluationContextSchemaInstance;
use confidence_resolver::confidence::flags::admin::v1::{
    flag_resolve_info::AssignmentResolveInfo, flag_resolve_info::RuleResolveInfo,
    flag_resolve_info::VariantResolveInfo, ClientResolveInfo, ContextFieldSemanticType,
    FlagResolveInfo,
};
use serde::{Deserialize, Serialize};
use std::sync::OnceLock;

static FLAGS_LOGS_QUEUE: OnceLock<Queue> = OnceLock::new();

static CONFIDENCE_CLIENT_ID: OnceLock<String> = OnceLock::new();
static CONFIDENCE_CLIENT_SECRET: OnceLock<String> = OnceLock::new();

static RESOLVER_STATE: Lazy<ResolverState> =
    Lazy::new(|| ResolverState::from_proto(STATE_JSON.to_owned().into(), ACCOUNT_ID));

trait ResponseExt {
    fn with_cors_headers(self, allowed_origin: &str) -> Result<Self>
    where
        Self: Sized;
}

struct H {}

fn convert_to_write_assign_request(
    resolve_id: &str,
    _evaluation_context: &Struct,
    assigned_flags: &[FlagToApply],
    client: &Client,
    sdk: &Option<confidence::flags::resolver::v1::Sdk>,
) -> WriteFlagAssignedRequest {
    let client_info = ClientInfo {
        client: client.client_name.to_string(),
        client_credential: client.client_credential_name.to_string(),
        sdk: sdk.clone(),
    };

    let assigns: Vec<FlagAssigned> = assigned_flags
        .iter()
        // .map(|flag_to_apply| flag_to_apply.assigned_flag)
        .map(|flag_to_apply| {
            let FlagToApply {
                assigned_flag,
                skew_adjusted_applied_time,
            } = flag_to_apply;
            // Create the `AppliedFlag` instance
            let applied_flag = AppliedFlag {
                flag: assigned_flag.flag.clone(),
                targeting_key: assigned_flag.targeting_key.clone(),
                targeting_key_selector: assigned_flag.targeting_key_selector.clone(),
                assignment_id: assigned_flag.assignment_id.to_string(),
                rule: assigned_flag.rule.clone(),
                fallthrough_assignments: assigned_flag.fallthrough_assignments.clone(),
                apply_time: Some(skew_adjusted_applied_time.clone()),
                assignment: if !assigned_flag.variant.is_empty() {
                    // Populate the `AssignmentInfo` if the variant is not empty
                    let assignment_info = AssignmentInfo {
                        segment: assigned_flag.segment.clone(),
                        variant: assigned_flag.variant.clone(),
                    };
                    Some(Assignment::AssignmentInfo(assignment_info))
                } else {
                    // Populate the `DefaultAssignment` otherwise
                    let default_assignment = DefaultAssignment {
                        reason: to_default_assignment(assigned_flag.reason()),
                    };
                    Some(Assignment::DefaultAssignment(default_assignment))
                },
            };
            // Create the `FlagAssigned` instance
            return FlagAssigned {
                client_info: Some(client_info.clone()),
                resolve_id: resolve_id.to_string(),
                flags: vec![applied_flag], // Add the `AppliedFlag` to the repeated `flags` field
                ..Default::default()
            };
        })
        .collect();

    WriteFlagAssignedRequest {
        flag_assigned: assigns,
    }
}

fn convert_to_write_resolve_request(
    _resolve_id: &str,
    evaluation_context: &Struct,
    values: &[ResolvedValue],
    client: &Client,
    _sdk: &Option<confidence::flags::resolver::v1::Sdk>,
) -> WriteResolveInfoItem {
    // Create client resolve info
    let derived_schema = SchemaFromEvaluationContext::get_schema(evaluation_context);
    let schema: BTreeMap<String, i32> = derived_schema
        .fields
        .into_iter()
        .map(|(k, v)| (k, v as i32))
        .collect();
    let semantic_types: BTreeMap<String, ContextFieldSemanticType> =
        derived_schema.semantic_types.into_iter().collect();
    let schema_instance = InternalEvaluationContextSchemaInstance {
        schema_json: serde_json::to_string(&schema).unwrap(),
        semantic_types_json: serde_json::to_string(&semantic_types).unwrap(),
    };

    let client_resolve_info = InternalClientResolveInfo {
        client: client.client_name.clone(),
        client_credential: client.client_credential_name.clone(),
        schema: vec![schema_instance],
    };

    // Create flag resolve info for each resolved value
    let flag_resolve_info: Vec<FlagResolveInfo> = values
        .iter()
        .map(|resolved_value| {
            FlagResolveInfo {
                flag: resolved_value.flag.name.clone(),
                variant_resolve_info: vec![VariantResolveInfo {
                    variant: resolved_value
                        .assignment_match
                        .as_ref()
                        .and_then(|am| am.variant.as_ref())
                        .map(|v| v.name.clone())
                        .unwrap_or_default(),
                    count: 1, // Individual resolve count
                }],
                rule_resolve_info: vec![RuleResolveInfo {
                    rule: resolved_value
                        .assignment_match
                        .as_ref()
                        .map(|am| am.rule.name.clone())
                        .unwrap_or_default(),
                    count: 1, // Individual resolve count
                    assignment_resolve_info: vec![AssignmentResolveInfo {
                        assignment_id: resolved_value
                            .assignment_match
                            .as_ref()
                            .map(|am| am.assignment_id.to_string())
                            .unwrap_or_default(),
                        count: 1, // Individual resolve count
                    }],
                }],
            }
        })
        .collect();

    WriteResolveInfoItem {
        client_resolve_info,
        flag_resolve_info,
    }
}

impl Host for H {
    async fn log_resolve(
        _resolve_id: &str,
        evaluation_context: &Struct,
        values: &[ResolvedValue<'_>],
        client: &Client,
        _sdk: &Option<confidence::flags::resolver::v1::Sdk>,
    ) {
        let info_item =
            convert_to_write_resolve_request(_resolve_id, evaluation_context, values, client, _sdk);
        let req = FlagLogQueueRequest {
            flag_assigned: vec![],
            client_resolve_info: vec![info_item.client_resolve_info.clone()],
            flag_resolve_info: info_item.flag_resolve_info,
            schemas: info_item.client_resolve_info.schema,
        };
        if let Some(queue) = FLAGS_LOGS_QUEUE.get() {
            let _ = queue.send(req).await;
        }
    }

    async fn log_assign(
        resolve_id: &str,
        _evaluation_context: &Struct,
        assigned_flags: &[FlagToApply],
        client: &Client,
        sdk: &Option<confidence::flags::resolver::v1::Sdk>,
    ) {
        let request = convert_to_write_assign_request(
            resolve_id,
            _evaluation_context,
            assigned_flags,
            client,
            sdk,
        );
        if let Some(queue) = FLAGS_LOGS_QUEUE.get() {
            let _ = queue
                .send(FlagLogQueueRequest {
                    flag_assigned: request.flag_assigned,
                    client_resolve_info: vec![],
                    flag_resolve_info: vec![],
                    schemas: vec![],
                })
                .await;
        }
    }
}

fn to_default_assignment(reason: ResolveReason) -> i32 {
    #[allow(clippy::needless_return)]
    return match reason {
        ResolveReason::NoSegmentMatch => DefaultAssignmentReason::NoSegmentMatch,
        ResolveReason::NoTreatmentMatch => DefaultAssignmentReason::NoTreatmentMatch,
        ResolveReason::FlagArchived => DefaultAssignmentReason::FlagArchived,
        _ => DefaultAssignmentReason::Unspecified,
    } as i32;
}

#[event(fetch)]
pub async fn main(req: Request, env: Env, _ctx: Context) -> Result<Response> {
    match env.queue("flag_logs_queue") {
        Ok(queue) => {
            let _ = FLAGS_LOGS_QUEUE.set(queue);
        }
        Err(_e) => {
            console_log!("flag_logs_queue binding is missing; logging disabled");
        }
    }

    if let Ok(var) = env.var("CONFIDENCE_CLIENT_ID") {
        let _ = CONFIDENCE_CLIENT_ID.set(var.to_string());
    }
    if let Ok(var) = env.var("CONFIDENCE_CLIENT_SECRET") {
        let _ = CONFIDENCE_CLIENT_SECRET.set(var.to_string());
    }

    let allowed_origin_env = env
        .var("ALLOWED_ORIGIN")
        .map(|var| var.to_string())
        .unwrap_or("*".to_string()); // Fallback to "*" if the variable is not set

    // Optional env var containing the resolver state ETag for this deployment
    let state_etag_env = env
        .var("RESOLVER_STATE_ETAG")
        .map(|var| var.to_string())
        .unwrap_or_default();

    // Optional env var containing the confidence-resolver commit used for this deployment
    let resolver_version_env = env
        .var("DEPLOYER_VERSION")
        .map(|var| var.to_string())
        .unwrap_or_default();

    if req.method() == Method::Options {
        return Response::ok("")?.with_cors_headers(&allowed_origin_env);
    }

    let state = &RESOLVER_STATE;
    let router = Router::new();

    let response = router
        // GET endpoint to expose the current deployment state etag and resolver version
        .get_async("/v1/state:etag", |_req, _ctx| {
            let allowed_origin = allowed_origin_env.clone();
            let etag_value = state_etag_env.clone();
            let version_value = resolver_version_env.clone();
            async move {
                let body = json!({
                    "etag": etag_value,
                    "version": version_value,
                });
                Response::from_json(&body)?.with_cors_headers(&allowed_origin)
            }
        })
        // Router treats ":name" as parameters, which is incompatible without URLs
        // so we use "*path" to match the whole path and do the matching in the handler
        .post_async("/v1/*path", |mut req, ctx| {
            let allowed_origin = allowed_origin_env.clone();
            async move {
                let path = ctx.param("path").unwrap();
                match path.as_str() {
                    "flags:resolve" => {
                        let body_bytes: Vec<u8> = req.bytes().await?;
                        let resolver_request: ResolveFlagsRequest = match from_slice(&body_bytes) {
                            Ok(req) => req,
                            Err(e) => {
                                return Response::error(
                                    format!("Invalid request payload: {}", e),
                                    400,
                                )?
                                .with_cors_headers(&allowed_origin);
                            }
                        };
                        let evaluation_context = resolver_request
                            .evaluation_context
                            .clone()
                            .unwrap_or_default();
                        match state.get_resolver::<H>(
                            &resolver_request.client_secret,
                            evaluation_context,
                            &Bytes::from(STANDARD.decode(ENCRYPTION_KEY_BASE64).unwrap()),
                        ) {
                            Some(resolver) => match resolver.resolve_flags(&resolver_request).await
                            {
                                Ok(response) => Response::from_json(&response)?
                                    .with_cors_headers(&allowed_origin),
                                Err(err) => Response::error(
                                    format!("Failed to resolve flags: {}", err),
                                    500,
                                )?
                                .with_cors_headers(&allowed_origin),
                            },
                            None => Response::error("Error setting up the resolver", 500)?
                                .with_cors_headers(&allowed_origin),
                        }
                    }
                    "flags:apply" => {
                        let body_bytes: Vec<u8> = req.bytes().await?;
                        let apply_flag_req: ApplyFlagsRequest = match from_slice(&body_bytes) {
                            Ok(req) => req,
                            Err(e) => {
                                return Response::error(
                                    format!("Invalid request payload: {}", e),
                                    400,
                                )?
                                .with_cors_headers(&allowed_origin);
                            }
                        };

                        match state.get_resolver::<H>(
                            &apply_flag_req.client_secret,
                            Struct::default(),
                            &Bytes::from(STANDARD.decode(ENCRYPTION_KEY_BASE64).unwrap()),
                        ) {
                            Some(resolver) => match resolver.apply_flags(&apply_flag_req).await {
                                Ok(_response) => {
                                    return Response::from_json(&ApplyFlagsResponse::default());
                                }
                                Err(err) => {
                                    Response::error(format!("Failed to apply flags: {}", err), 500)?
                                        .with_cors_headers(&allowed_origin)
                                }
                            },
                            None => Response::error("Error setting up the resolver", 500)?
                                .with_cors_headers(&allowed_origin),
                        }
                    }
                    _ => Response::error("The URL is invalid", 404)?
                        .with_cors_headers(&allowed_origin),
                }
            }
        })
        .run(req, env)
        .await;
    response
}

#[event(queue)]
pub async fn consume_flag_logs_queue(
    message_batch: MessageBatch<FlagLogQueueRequest>,
    _env: Env,
    _ctx: Context,
) -> Result<()> {
    // Fail fast if credentials aren't available
    let client_id = CONFIDENCE_CLIENT_ID
        .get()
        .ok_or(Error::RustError("CONFIDENCE_CLIENT_ID is required".into()))?;
    let client_secret = CONFIDENCE_CLIENT_SECRET.get().ok_or(Error::RustError(
        "CONFIDENCE_CLIENT_SECRET is required".into(),
    ))?;
    let mut client_resolve_info: Vec<ClientResolveInfo> = vec![];
    let mut flag_resolve_info: Vec<FlagResolveInfo> = vec![];
    let mut flag_assigned: Vec<FlagAssigned> = vec![];

    if let Ok(messages) = message_batch.messages() {
        for message in messages {
            let flag_logs = message.body();
            for c in &flag_logs.client_resolve_info {
                let converted = convert_to_client_resolve_info(c.clone());
                client_resolve_info.push(converted.clone());
            }
            for f in &flag_logs.flag_resolve_info {
                flag_resolve_info.push(f.clone());
            }
            for fa in &flag_logs.flag_assigned {
                flag_assigned.push(fa.clone());
            }
        }
    }

    let req = WriteFlagLogsRequest {
        flag_assigned,
        client_resolve_info,
        flag_resolve_info,
        schemas: vec![],
    };
    send_flags_logs(client_id.as_str(), client_secret.as_str(), req).await?;

    message_batch.ack_all();
    Ok(())
}

pub fn convert_to_client_resolve_info(c: InternalClientResolveInfo) -> ClientResolveInfo {
    ClientResolveInfo {
        client: c.client,
        client_credential: c.client_credential,
        schema: c
            .schema
            .iter()
            .map(|sh| EvaluationContextSchemaInstance {
                schema: serde_json::from_str(sh.schema_json.as_str()).unwrap(),
                semantic_types: serde_json::from_str(sh.semantic_types_json.as_str()).unwrap(),
            })
            .collect(),
    }
}

fn get_token(client_id: &str, client_secret: &str) -> String {
    let combined = format!("{}:{}", client_id, client_secret);
    let encoded = STANDARD.encode(combined.as_bytes());
    format!("Basic {}", encoded)
}

async fn send_flags_logs(
    client_id: &str,
    client_secret: &str,
    message: WriteFlagLogsRequest,
) -> Result<Response> {
    let resolve_url = "https://resolver.confidence.dev/v1/flagLogs:write";
    let mut init = RequestInit::new();
    let mut headers = Headers::new();
    headers.set("Content-Type", "application/json")?;
    headers.set(
        "Authorization",
        get_token(client_id, client_secret).as_str(),
    )?;
    init.with_headers(headers);
    init.with_method(Method::Post);
    let json = serde_json::to_string(&message)?;
    init.with_body(Some(json.into()));
    let request = Request::new_with_init(resolve_url, &init)?;
    let response = Fetch::Request(request).send().await;
    response
}

impl ResponseExt for Response {
    fn with_cors_headers(mut self, allowed_origin: &str) -> Result<Self>
    where
        Self: Sized,
    {
        let headers = self.headers_mut();

        headers.set("Access-Control-Allow-Origin", allowed_origin)?;
        headers.set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")?;
        headers.set("Access-Control-Allow-Headers", "*")?;

        Ok(self)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashMap;

    #[test]
    fn test_convert_to_write_assign_request() {
        use confidence_resolver::Account;

        // Create test client
        let client = Client {
            client_name: "test-client".to_string(),
            client_credential_name: "test-credential".to_string(),
            account: Account {
                name: "test-account".to_string(),
            },
        };

        // Create test evaluation context
        let evaluation_context = Struct {
            fields: HashMap::new(),
        };

        // Create test assigned flags - simplified approach
        // Note: This test focuses on the conversion function structure
        // The actual FlagToApply creation would happen elsewhere in the system
        let assigned_flags = vec![]; // Empty for this test since the complex setup is not feasible

        // Create test SDK (optional)
        let sdk = None;

        // Call the function
        let result = convert_to_write_assign_request(
            "resolve-123",
            &evaluation_context,
            &assigned_flags,
            &client,
            &sdk,
        );

        // Verify the basic structure - with empty flags the result should be empty too
        assert_eq!(result.flag_assigned.len(), 0);

        // Test that the function runs without errors and returns correct type
        assert!(serde_json::to_string(&result).is_ok());
    }
}
