use crate::confidence::flags::admin::v1::client_resolve_info::EvaluationContextSchemaInstance;
use crate::confidence::flags::admin::v1::flag_resolve_info::{
    AssignmentResolveInfo, RuleResolveInfo, VariantResolveInfo,
};
use crate::confidence::flags::admin::v1::{
    ClientResolveInfo, ContextFieldSemanticType, FlagResolveInfo,
};
use crate::confidence::flags::resolver::v1::events::flag_assigned::applied_flag::Assignment;
use crate::confidence::flags::resolver::v1::events::flag_assigned::default_assignment::DefaultAssignmentReason;
use crate::confidence::flags::resolver::v1::events::flag_assigned::{
    AppliedFlag, AssignmentInfo, DefaultAssignment,
};
use crate::confidence::flags::resolver::v1::events::{ClientInfo, FlagAssigned};
use crate::confidence::flags::resolver::v1::Sdk;
use crate::schema_util::SchemaFromEvaluationContext;
use crate::Client;
use crate::Struct;
use crate::{FlagToApply, ResolvedValue};
use alloc::collections::{BTreeMap, BTreeSet};
use alloc::string::{String, ToString};
use alloc::vec;
use alloc::vec::Vec;
#[cfg(feature = "std")]
use serde::{Deserialize, Serialize};
use spin::Mutex;

#[cfg(not(feature = "std"))]
#[derive(Clone, Debug)]
pub struct FlagLogQueueRequest {
    pub flag_assigned: Vec<FlagAssigned>,
    pub client_resolve_info: Vec<ClientResolveInfo>,
    pub flag_resolve_info: Vec<FlagResolveInfo>,
}

pub struct Logger {
    flag_log_requests: Mutex<Vec<FlagLogQueueRequest>>,
}

impl Logger {
    pub fn new() -> Logger {
        Logger {
            flag_log_requests: Mutex::new(Vec::new()),
        }
    }
}

impl FlagLogger for Logger {
    fn log_resolve(
        &self,
        resolve_id: &str,
        evaluation_context: &Struct,
        values: &[ResolvedValue<'_>],
        client: &Client,
        sdk: &Option<Sdk>,
    ) {
        let req =
            convert_to_write_resolve_request(resolve_id, evaluation_context, values, client, sdk);
        let mut vec = self.flag_log_requests.lock();
        vec.push(req);
    }

    fn log_assign(
        &self,
        resolve_id: &str,
        evaluation_context: &Struct,
        assigned_flags: &[FlagToApply],
        client: &Client,
        sdk: &Option<Sdk>,
    ) {
        let req = convert_to_write_assign_request(
            resolve_id,
            evaluation_context,
            assigned_flags,
            client,
            sdk,
        );
        let mut vec = self.flag_log_requests.lock();
        vec.push(req);
    }

    fn pop_flag_log_batch(&self) -> FlagLogQueueRequest {
        let mut vec = self.flag_log_requests.lock();
        Self::aggregate_batch(vec.split_off(0))
    }
    fn aggregate_batch(message_batch: Vec<FlagLogQueueRequest>) -> FlagLogQueueRequest {
        // map of client credential to derived schema
        let mut schema_map: BTreeMap<String, SchemaItem> = BTreeMap::new();
        // map of flag to flag resolve info
        let mut flag_resolve_map: BTreeMap<String, VariantRuleResolveInfo> = BTreeMap::new();
        let mut flag_assigned: Vec<FlagAssigned> = vec![];

        for flag_logs_message in message_batch {
            for c in &flag_logs_message.client_resolve_info {
                if let Some(set) = schema_map.get_mut(&c.client_credential) {
                    for schema in &c.schema {
                        set.schemas.insert(schema.clone());
                    }
                } else {
                    let mut set = BTreeSet::new();
                    for schema in &c.schema {
                        set.insert(schema.clone());
                    }
                    schema_map.insert(
                        c.client_credential.clone(),
                        SchemaItem {
                            client: c.client.clone(),
                            schemas: set.clone(),
                        },
                    );
                }
            }

            for f in &flag_logs_message.flag_resolve_info {
                let flag_info = flag_resolve_map
                    .entry(f.flag.clone())
                    .or_insert_with(VariantRuleResolveInfo::new);
                update_rule_variant_info(flag_info, f);
            }
            for fa in &flag_logs_message.flag_assigned {
                flag_assigned.push(fa.clone());
            }
        }

        let mut client_resolve_info: Vec<ClientResolveInfo> = vec![];
        for (client_credentials, schema_item) in schema_map {
            client_resolve_info.push(ClientResolveInfo {
                client_credential: client_credentials,
                client: schema_item.client,
                schema: schema_item.schemas.into_iter().collect(),
            })
        }

        let mut flag_resolve_info: Vec<FlagResolveInfo> = vec![];

        for (flag, resolve_info) in flag_resolve_map {
            let variant_resolve_info = resolve_info
                .variant_resolve_info
                .iter()
                .map(|r| VariantResolveInfo {
                    variant: r.0.clone(),
                    count: *r.1,
                })
                .collect();

            let mut rule_resolve_info: Vec<RuleResolveInfo> = vec![];

            for (rule, info) in resolve_info.rule_resolve_info {
                rule_resolve_info.push(RuleResolveInfo {
                    rule,
                    count: info.count,
                    assignment_resolve_info: info
                        .assignment_count
                        .iter()
                        .map(|(assignment_id, count)| AssignmentResolveInfo {
                            count: *count,
                            assignment_id: assignment_id.clone(),
                        })
                        .collect(),
                });
            }

            flag_resolve_info.push(FlagResolveInfo {
                flag,
                variant_resolve_info,
                rule_resolve_info,
            })
        }

        FlagLogQueueRequest {
            flag_assigned,
            flag_resolve_info,
            client_resolve_info,
        }
    }
}

pub trait FlagLogger {
    fn log_resolve(
        &self,
        resolve_id: &str,
        evaluation_context: &Struct,
        values: &[ResolvedValue<'_>],
        client: &Client,
        sdk: &Option<Sdk>,
    );

    fn log_assign(
        &self,
        resolve_id: &str,
        evaluation_context: &Struct,
        assigned_flags: &[FlagToApply],
        client: &Client,
        sdk: &Option<Sdk>,
    );

    fn pop_flag_log_batch(&self) -> FlagLogQueueRequest;
    fn aggregate_batch(message_batch: Vec<FlagLogQueueRequest>) -> FlagLogQueueRequest;
}

#[cfg(feature = "std")]
#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct FlagLogQueueRequest {
    pub flag_assigned: Vec<FlagAssigned>,
    pub client_resolve_info: Vec<ClientResolveInfo>,
    pub flag_resolve_info: Vec<FlagResolveInfo>,
}

fn convert_to_write_assign_request(
    resolve_id: &str,
    _evaluation_context: &Struct,
    assigned_flags: &[FlagToApply],
    client: &Client,
    sdk: &Option<Sdk>,
) -> FlagLogQueueRequest {
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
            FlagAssigned {
                client_info: Some(client_info.clone()),
                resolve_id: ToString::to_string(resolve_id),
                flags: vec![applied_flag], // Add the `AppliedFlag` to the repeated `flags` field
            }
        })
        .collect();

    FlagLogQueueRequest {
        flag_assigned: assigns,
        client_resolve_info: vec![],
        flag_resolve_info: vec![],
    }
}

fn to_default_assignment(reason: crate::confidence::flags::resolver::v1::ResolveReason) -> i32 {
    #[allow(clippy::needless_return)]
    return match reason {
        crate::confidence::flags::resolver::v1::ResolveReason::NoSegmentMatch => {
            DefaultAssignmentReason::NoSegmentMatch
        }
        crate::confidence::flags::resolver::v1::ResolveReason::NoTreatmentMatch => {
            DefaultAssignmentReason::NoTreatmentMatch
        }
        crate::confidence::flags::resolver::v1::ResolveReason::FlagArchived => {
            DefaultAssignmentReason::FlagArchived
        }
        _ => DefaultAssignmentReason::Unspecified,
    } as i32;
}

fn convert_to_write_resolve_request(
    _resolve_id: &str,
    evaluation_context: &Struct,
    values: &[ResolvedValue],
    client: &Client,
    _sdk: &Option<Sdk>,
) -> FlagLogQueueRequest {
    // Create client resolve info
    let derived_schema = SchemaFromEvaluationContext::get_schema(evaluation_context);
    let schema: BTreeMap<String, i32> = derived_schema
        .fields
        .into_iter()
        .map(|(k, v)| (k, v as i32))
        .collect();
    let semantic_types: BTreeMap<String, ContextFieldSemanticType> =
        derived_schema.semantic_types.into_iter().collect();
    let schema_instance = EvaluationContextSchemaInstance {
        schema: schema.clone(),
        semantic_types: semantic_types.clone(),
    };

    let client_resolve_info = ClientResolveInfo {
        client: client.client_name.clone(),
        client_credential: client.client_credential_name.clone(),
        schema: vec![schema_instance],
    };

    // Create flag resolve info for each resolved value
    let flag_resolve_info: Vec<FlagResolveInfo> = values
        .iter()
        .map(|resolved_value| {
            let mut rules: Vec<RuleResolveInfo> = vec![];
            for fr in &resolved_value.fallthrough_rules {
                rules.push(RuleResolveInfo {
                    rule: fr.rule.name.clone(),
                    count: 1,
                    assignment_resolve_info: vec![AssignmentResolveInfo {
                        count: 1,
                        assignment_id: fr.assignment_id.clone(),
                    }],
                })
            }
            rules.push(RuleResolveInfo {
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
            });
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
                rule_resolve_info: rules,
            }
        })
        .collect();

    FlagLogQueueRequest {
        flag_assigned: vec![],
        client_resolve_info: vec![client_resolve_info],
        flag_resolve_info,
    }
}

struct SchemaItem {
    pub client: String,
    pub schemas: BTreeSet<EvaluationContextSchemaInstance>,
}

#[derive(Debug, Clone)]
struct RuleResolveInfoCount {
    pub count: i64,
    // assignment id to count
    pub assignment_count: BTreeMap<String, i64>,
}

#[derive(Debug, Clone)]
struct VariantRuleResolveInfo {
    // rule to count
    rule_resolve_info: BTreeMap<String, RuleResolveInfoCount>,
    // variant to count
    variant_resolve_info: BTreeMap<String, i64>,
}

impl VariantRuleResolveInfo {
    fn new() -> VariantRuleResolveInfo {
        VariantRuleResolveInfo {
            rule_resolve_info: BTreeMap::new(),
            variant_resolve_info: BTreeMap::new(),
        }
    }
}

fn update_rule_variant_info(
    flag_info: &mut VariantRuleResolveInfo,
    rule_resolve_info: &FlagResolveInfo,
) {
    for rule_info in &rule_resolve_info.rule_resolve_info {
        let resolve_count = match flag_info.rule_resolve_info.get(&rule_info.rule) {
            Some(i) => i.count,
            None => 0,
        } + rule_info.count;

        // assignment id to count
        let current_assignments: &BTreeMap<String, i64> =
            match flag_info.rule_resolve_info.get(&rule_info.rule) {
                Some(i) => &i.assignment_count,
                None => &BTreeMap::new(),
            };

        // assignment id to count
        let mut new_assignment_count: BTreeMap<String, i64> = BTreeMap::new();
        for aa in &rule_info.assignment_resolve_info {
            let count = match current_assignments.get(&aa.assignment_id) {
                None => 0,
                Some(a) => *a,
            } + aa.count;
            new_assignment_count.insert(aa.clone().assignment_id, count);
        }
        flag_info.rule_resolve_info.insert(
            rule_info.rule.clone(),
            RuleResolveInfoCount {
                count: resolve_count,
                assignment_count: new_assignment_count,
            },
        );
    }

    for variant_info in &rule_resolve_info.variant_resolve_info {
        let count = match flag_info.variant_resolve_info.get(&variant_info.variant) {
            None => 0,
            Some(v) => *v,
        } + variant_info.count;
        flag_info
            .variant_resolve_info
            .insert(variant_info.variant.clone(), count);
    }
}
