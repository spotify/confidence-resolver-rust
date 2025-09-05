use confidence_resolver::confidence::flags::resolver::v1::events::FlagAssigned;
use confidence_resolver::{confidence, FlagToApply, Host, ResolvedValue, ResolverState, Struct};
use worker::*;

use base64::engine::general_purpose::STANDARD;
use base64::Engine;
use bytes::Bytes;
use serde_json::from_slice;
use serde_json::json;

use confidence::flags::resolver::v1::{ApplyFlagsRequest, ApplyFlagsResponse, ResolveFlagsRequest};

use confidence_resolver::Client;
use once_cell::sync::Lazy;

use confidence_resolver::flag_logger::{FlagLogQueueRequest, FlagLogger, Logger};

const ACCOUNT_ID: &str = include_str!("../../data/account_id");
const STATE_JSON: &[u8] = include_bytes!("../../data/resolver_state_current.pb");
const ENCRYPTION_KEY_BASE64: &str = include_str!("../../data/encryption_key");

use confidence_resolver::confidence::flags::admin::v1::{ClientResolveInfo, FlagResolveInfo};
use confidence_resolver::confidence::flags::resolver::v1::Sdk;
use serde::{Deserialize, Serialize};
use std::sync::OnceLock;

static FLAGS_LOGS_QUEUE: OnceLock<Queue> = OnceLock::new();

static CONFIDENCE_CLIENT_ID: OnceLock<String> = OnceLock::new();
static CONFIDENCE_CLIENT_SECRET: OnceLock<String> = OnceLock::new();

static FLAG_LOGGER: Lazy<Logger> = Lazy::new(|| Logger::new());

static RESOLVER_STATE: Lazy<ResolverState> =
    Lazy::new(|| ResolverState::from_proto(STATE_JSON.to_owned().into(), ACCOUNT_ID));

trait ResponseExt {
    fn with_cors_headers(self, allowed_origin: &str) -> Result<Self>
    where
        Self: Sized;
}

struct H {}

impl Host for H {
    fn log_resolve(
        resolve_id: &str,
        evaluation_context: &Struct,
        values: &[ResolvedValue<'_>],
        client: &Client,
        sdk: &Option<Sdk>,
    ) {
        FLAG_LOGGER.log_resolve(resolve_id, evaluation_context, values, client, sdk);
    }

    fn log_assign(
        resolve_id: &str,
        evaluation_context: &Struct,
        assigned_flags: &[FlagToApply],
        client: &Client,
        sdk: &Option<Sdk>,
    ) {
        FLAG_LOGGER.log_assign(resolve_id, evaluation_context, assigned_flags, client, sdk);
    }
}

fn set_client_creds(env: &Env) {
    if let Ok(var) = env.var("CONFIDENCE_CLIENT_ID") {
        let _ = CONFIDENCE_CLIENT_ID.set(var.to_string());
    } else {
        console_log!("no confidence client id provided");
    }
    if let Ok(var) = env.var("CONFIDENCE_CLIENT_SECRET") {
        let _ = CONFIDENCE_CLIENT_SECRET.set(var.to_string());
    } else {
        console_log!("no confidence client secret provided");
    }
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

    set_client_creds(&env);

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
                            Some(resolver) => match resolver.resolve_flags(&resolver_request) {
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
                            Some(resolver) => match resolver.apply_flags(&apply_flag_req) {
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

    wasm_bindgen_futures::spawn_local(async move {
        let aggregated = FLAG_LOGGER.pop_flag_log_batch();
        if !(aggregated.flag_assigned.is_empty()
            && aggregated.flag_resolve_info.is_empty()
            && aggregated.client_resolve_info.is_empty())
        {
            if let Ok(converted) = serde_json::to_string(&aggregated) {
                FLAGS_LOGS_QUEUE
                    .get()
                    .unwrap()
                    .send(converted)
                    .await
                    .unwrap();
            }
        }
    });

    response
}

#[event(queue)]
pub async fn consume_flag_logs_queue(
    message_batch: MessageBatch<String>,
    env: Env,
    _ctx: Context,
) -> Result<()> {
    set_client_creds(&env);

    if let Ok(messages) = message_batch.messages() {
        let logs: Vec<FlagLogQueueRequest> = messages
            .iter()
            .map(|m| m.body().clone())
            .map(|s| serde_json::from_str::<FlagLogQueueRequest>(s.as_str()).unwrap())
            .collect();
        let req = Logger::aggregate_batch(logs);
        send_flags_logs(
            CONFIDENCE_CLIENT_ID.get().unwrap().as_str(),
            CONFIDENCE_CLIENT_SECRET.get().unwrap().as_str(),
            WriteFlagLogsRequest {
                client_resolve_info: req.client_resolve_info,
                flag_assigned: req.flag_assigned,
                flag_resolve_info: req.flag_resolve_info,
            },
        )
        .await?;
    }

    Ok(())
}
fn get_token(client_id: &str, client_secret: &str) -> String {
    let combined = format!("{}:{}", client_id, client_secret);
    let encoded = STANDARD.encode(combined.as_bytes());
    format!("Basic {}", encoded)
}

#[derive(Clone, Serialize, Deserialize, Debug)]
pub struct WriteFlagLogsRequest {
    flag_assigned: Vec<FlagAssigned>,
    client_resolve_info: Vec<ClientResolveInfo>,
    flag_resolve_info: Vec<FlagResolveInfo>,
}

async fn send_flags_logs(
    client_id: &str,
    client_secret: &str,
    message: WriteFlagLogsRequest,
) -> Result<Response> {
    let resolve_url = "https://resolver.confidence.dev/v1/flagLogs:write";
    let mut init = RequestInit::new();
    let headers = Headers::new();
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
