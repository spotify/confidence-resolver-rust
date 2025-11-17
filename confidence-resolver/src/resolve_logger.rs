use std::sync::{
    atomic::{AtomicU32, Ordering},
    Arc, RwLock,
};

use crate::{
    schema_util::{DerivedClientSchema, SchemaFromEvaluationContext},
    FlagToApply,
};
use arc_swap::ArcSwap;
use papaya::{HashMap, HashSet};

mod pb {
    pub use crate::proto::confidence::flags::admin::v1::{
        client_resolve_info, flag_resolve_info, ClientResolveInfo, FlagResolveInfo,
    };
    pub use crate::proto::confidence::flags::resolver::v1::{events::FlagAssigned, ResolveReason};
    pub use crate::proto::{
        confidence::flags::resolver::v1::{
            events::{
                flag_assigned::{
                    self, applied_flag::Assignment, AppliedFlag, AssignmentInfo, DefaultAssignment,
                },
                ClientInfo,
            },
            WriteFlagLogsRequest,
        },
        google::Struct,
    };
    pub use flag_assigned::default_assignment::DefaultAssignmentReason;
}

#[derive(Debug)]
pub struct ResolveLogger {
    state: ArcSwap<RwLock<Option<ResolveInfoState>>>,
}

impl Default for ResolveLogger {
    fn default() -> Self {
        Self::new()
    }
}

impl ResolveLogger {
    pub fn new() -> ResolveLogger {
        ResolveLogger {
            state: ArcSwap::new(Arc::new(RwLock::new(Some(ResolveInfoState::default())))),
        }
    }

    fn with_state<F: FnOnce(&ResolveInfoState)>(&self, f: F) {
        loop {
            let lock = self.state.load_full();
            let Ok(rg) = lock.try_read() else {
                // this is lock free. If we didn't get the read lock it means checkpoint has
                // swapped and acquired the write lock so we can just retry and get the next state
                continue;
            };
            // In an earlier version we failed on this Option being None, leading to flakey tests.
            // The Option can be none if thread T1 has a reference to the lock, but parks before try_lock.
            // In the meantime a checkpoint thread T2, swaps out the lock, takes a write lock, takes the option
            // (replacing it with None) and releases the lock. Now T1 wakes up and tries and succeeds the read
            // lock. This scenario is rare and as above it's sound to retry,
            if let Some(state) = rg.as_ref() {
                f(state);
                break;
            };
        }
    }

    pub fn log_resolve(
        &self,
        _resolve_id: &str,
        resolve_context: &pb::Struct,
        client_credential: &str,
        values: &[crate::ResolvedValue<'_>],
    ) {
        self.with_state(|state: &ResolveInfoState| {
            // Increment the resolve counter
            state.resolve_count.fetch_add(1, Ordering::Relaxed);

            state
                .client_resolve_info
                .with_default(client_credential, |client_resolve_info| {
                    let schema = SchemaFromEvaluationContext::get_schema(resolve_context);
                    client_resolve_info.schemas.pin().insert(schema);
                });

            for value in values {
                state
                    .flag_resolve_info
                    .with_default(&value.flag.name, |flag_state| {
                        for fallthrough in &value.fallthrough_rules {
                            flag_state.rule_resolve_info.with_default(
                                &fallthrough.rule.name,
                                |rule_state| {
                                    rule_state.count.fetch_add(1, Ordering::Relaxed);
                                    rule_state
                                        .assignment_counts
                                        .increment(&fallthrough.assignment_id);
                                },
                            );
                        }

                        match &value.assignment_match {
                            Some(assignment) => {
                                let variant_key: &str = match assignment.variant {
                                    Some(variant) => &variant.name,
                                    None => "",
                                };
                                flag_state.variant_resolve_info.increment(variant_key);
                                flag_state.rule_resolve_info.with_default(
                                    &assignment.rule.name,
                                    |rule_state| {
                                        rule_state.count.fetch_add(1, Ordering::Relaxed);
                                        rule_state
                                            .assignment_counts
                                            .increment(&assignment.assignment_id);
                                    },
                                );
                            }
                            None => {
                                flag_state.variant_resolve_info.increment("");
                            }
                        }
                    });
            }
        })
    }

    pub fn log_assigns(
        &self,
        resolve_id: &str,
        _evaluation_context: &pb::Struct,
        assigned_flags: &[crate::FlagToApply],
        client: &crate::Client,
        sdk: &Option<crate::flags_resolver::Sdk>,
    ) {
        self.with_state(|state: &ResolveInfoState| {
            let client_info = Some(pb::ClientInfo {
                client: client.client_name.to_string(),
                client_credential: client.client_credential_name.to_string(),
                sdk: sdk.clone(),
            });
            let flags = assigned_flags
                .iter()
                .map(
                    |FlagToApply {
                         assigned_flag: f,
                         skew_adjusted_applied_time,
                     }| {
                        let assignment = if !f.variant.is_empty() {
                            let assignment_info = pb::AssignmentInfo {
                                segment: f.segment.clone(),
                                variant: f.variant.clone(),
                            };
                            Some(pb::Assignment::AssignmentInfo(assignment_info))
                        } else {
                            let default_reason: pb::DefaultAssignmentReason =
                                match pb::ResolveReason::try_from(f.reason) {
                                    Ok(pb::ResolveReason::NoSegmentMatch) => {
                                        pb::DefaultAssignmentReason::NoSegmentMatch
                                    }
                                    Ok(pb::ResolveReason::NoTreatmentMatch) => {
                                        pb::DefaultAssignmentReason::NoTreatmentMatch
                                    }
                                    Ok(pb::ResolveReason::FlagArchived) => {
                                        pb::DefaultAssignmentReason::FlagArchived
                                    }
                                    _ => pb::DefaultAssignmentReason::Unspecified,
                                };
                            Some(pb::Assignment::DefaultAssignment(pb::DefaultAssignment {
                                reason: default_reason.into(),
                            }))
                        };
                        pb::AppliedFlag {
                            flag: f.flag.clone(),
                            targeting_key: f.targeting_key.clone(),
                            targeting_key_selector: f.targeting_key_selector.clone(),
                            assignment_id: f.assignment_id.clone(),
                            rule: f.rule.clone(),
                            fallthrough_assignments: f.fallthrough_assignments.clone(),
                            apply_time: Some(skew_adjusted_applied_time.clone()),
                            assignment,
                        }
                    },
                )
                .collect();

            state.flag_assigned.push(pb::FlagAssigned {
                resolve_id: resolve_id.to_string(),
                client_info,
                flags,
            });
        })
    }

    pub fn checkpoint(&self) -> pb::WriteFlagLogsRequest {
        let lock = self
            .state
            .swap(Arc::new(RwLock::new(Some(ResolveInfoState::default()))));
        // the only operation we do under write-lock is take the option, and that can't panic, so lock shouldn't be poisoned,
        // even so, if it some how was it's safe to still use the value.
        let mut wg = lock
            .write()
            .unwrap_or_else(|poisoned| poisoned.into_inner());
        // also shouldn't be possible for this Option to be None as we never insert None and only one thread can swap the value out
        // if this assertion somehow is faulty, returning an empty WriteFlagLogsRequest is sound.
        wg.take()
            .map(|state| {
                let client_resolve_info = build_client_resolve_info(&state);
                let flag_resolve_info = build_flag_resolve_info(&state);
                let resolve_count = state.resolve_count.load(Ordering::Relaxed) as i64;
                pb::WriteFlagLogsRequest {
                    flag_resolve_info,
                    client_resolve_info,
                    flag_assigned: state.flag_assigned.into_iter().collect(),
                    telemetry_data: None,
                    resolve_count,
                }
            })
            .unwrap_or_default()
    }
}

#[derive(Debug, Default)]
struct RuleResolveInfo {
    count: AtomicU32,
    assignment_counts: HashMap<String, AtomicU32>,
}

#[derive(Debug, Default)]
struct FlagResolveInfo {
    variant_resolve_info: HashMap<String, AtomicU32>,
    rule_resolve_info: HashMap<String, RuleResolveInfo>,
}

#[derive(Debug, Default)]
struct ClientResolveInfo {
    schemas: HashSet<DerivedClientSchema>,
}
#[derive(Debug, Default)]
struct ResolveInfoState {
    flag_resolve_info: HashMap<String, FlagResolveInfo>,
    client_resolve_info: HashMap<String, ClientResolveInfo>,
    flag_assigned: crossbeam_queue::SegQueue<pb::FlagAssigned>,
    resolve_count: AtomicU32,
}

fn extract_client(credential: &str) -> String {
    // split on '/', take first two segments
    let mut it = credential.split('/');
    match (it.next(), it.next()) {
        (Some(a), Some(b)) => format!("{}/{}", a, b),
        _ => credential.to_string(),
    }
}

fn to_pb_schema_instance(
    schema: &DerivedClientSchema,
) -> pb::client_resolve_info::EvaluationContextSchemaInstance {
    pb::client_resolve_info::EvaluationContextSchemaInstance {
        schema: schema
            .fields
            .iter()
            .map(|(k, v)| (k.clone(), *v as i32))
            .collect(),
        semantic_types: schema.semantic_types.clone(),
    }
}

fn build_client_resolve_info(state: &ResolveInfoState) -> Vec<pb::ClientResolveInfo> {
    let mp = state.client_resolve_info.pin();
    mp.iter()
        .map(|(credential, info)| {
            let client = extract_client(credential);
            let sp = info.schemas.pin();
            let schemas = sp.iter().map(to_pb_schema_instance).collect();
            pb::ClientResolveInfo {
                client,
                client_credential: credential.clone(),
                schema: schemas,
            }
        })
        .collect()
}

fn to_pb_variant(
    (variant_key, counter): (&String, &AtomicU32),
) -> pb::flag_resolve_info::VariantResolveInfo {
    pb::flag_resolve_info::VariantResolveInfo {
        variant: variant_key.clone(),
        count: counter.load(Ordering::Relaxed) as i64,
    }
}

fn to_pb_assignment(
    (assignment_id, cnt): (&String, &AtomicU32),
) -> pb::flag_resolve_info::AssignmentResolveInfo {
    pb::flag_resolve_info::AssignmentResolveInfo {
        assignment_id: assignment_id.clone(),
        count: cnt.load(Ordering::Relaxed) as i64,
    }
}

fn to_pb_rule(
    (rule_name, rinfo): (&String, &RuleResolveInfo),
) -> pb::flag_resolve_info::RuleResolveInfo {
    let ap = rinfo.assignment_counts.pin();
    let assignments = ap.iter().map(to_pb_assignment).collect();
    pb::flag_resolve_info::RuleResolveInfo {
        rule: rule_name.clone(),
        count: rinfo.count.load(Ordering::Relaxed) as i64,
        assignment_resolve_info: assignments,
    }
}

fn build_flag_resolve_info(state: &ResolveInfoState) -> Vec<pb::FlagResolveInfo> {
    let mp = state.flag_resolve_info.pin();
    mp.iter()
        .map(|(flag_name, info)| {
            let vp = info.variant_resolve_info.pin();
            let variants = vp.iter().map(to_pb_variant).collect();

            let rp = info.rule_resolve_info.pin();
            let rules = rp.iter().map(to_pb_rule).collect();

            pb::FlagResolveInfo {
                flag: flag_name.clone(),
                variant_resolve_info: variants,
                rule_resolve_info: rules,
            }
        })
        .collect()
}

trait PapayaMapExt<V> {
    fn with_default<F>(&self, key: &str, f: F)
    where
        V: Default,
        F: FnOnce(&V);
}

impl<V> PapayaMapExt<V> for HashMap<String, V> {
    fn with_default<F>(&self, key: &str, f: F)
    where
        V: Default,
        F: FnOnce(&V),
    {
        let g = self.pin();
        if let Some(v) = g.get(key) {
            // fast path with no allocation if entry exists
            f(v);
        } else {
            let v = g.get_or_insert_with(key.to_owned(), V::default);
            f(v);
        }
    }
}

trait PapayaCounterMapExt {
    fn increment(&self, key: &str);
}

impl PapayaCounterMapExt for HashMap<String, AtomicU32> {
    fn increment(&self, key: &str) {
        self.with_default(key, |counter| {
            counter.fetch_add(1, Ordering::Relaxed);
        });
    }
}

#[cfg(test)]
mod tests {
    use crate::{
      proto::{confidence::flags::admin::v1::{
        context_field_semantic_type::{CountrySemanticType, DateSemanticType, TimestampSemanticType, VersionSemanticType}, evaluation_context_schema_field, ContextFieldSemanticType
      }, google::Struct}, resolve_logger::{pb::WriteFlagLogsRequest, ResolveLogger}
    };
    use crate::proto::confidence::flags::admin::v1::context_field_semantic_type::country_semantic_type::CountryFormat;
    use std::{collections::BTreeMap};
    use serde_json::json;

    #[test]
    fn decorates_with_context_schema() {
        let logger = ResolveLogger::new();
        let ctx: Struct = serde_json::from_value(json!({
          "country": "SE",
          "not_a_country": "abc",
          "vi_pratar_svenska_så_detta_är_tiden": "2025-04-01T12:34:56Z",
          "version": "1.2.3",
          "siffra": 3,
          "today": "2025-04-01"
        }))
        .unwrap();

        let cred = "clients/test/clientCredentials/test";
        let rv = [];
        logger.log_resolve("id", &ctx, cred, &rv);
        let req = logger.checkpoint();
        // find the client entry in the built request
        let crec = req
            .client_resolve_info
            .iter()
            .find(|c| c.client_credential == cred)
            .unwrap();
        let schema = &crec.schema[0];

        // Expected fields kinds
        let mut expected_fields = BTreeMap::new();
        expected_fields.insert(
            "country".to_string(),
            evaluation_context_schema_field::Kind::StringKind as i32,
        );
        expected_fields.insert(
            "not_a_country".to_string(),
            evaluation_context_schema_field::Kind::StringKind as i32,
        );
        expected_fields.insert(
            "vi_pratar_svenska_så_detta_är_tiden".to_string(),
            evaluation_context_schema_field::Kind::StringKind as i32,
        );
        expected_fields.insert(
            "version".to_string(),
            evaluation_context_schema_field::Kind::StringKind as i32,
        );
        expected_fields.insert(
            "siffra".to_string(),
            evaluation_context_schema_field::Kind::NumberKind as i32,
        );
        expected_fields.insert(
            "today".to_string(),
            evaluation_context_schema_field::Kind::StringKind as i32,
        );
        assert_eq!(schema.schema, expected_fields);

        // Expected semantic types
        let mut expected_sem = BTreeMap::new();
        expected_sem.insert(
      "country".to_string(),
      ContextFieldSemanticType { r#type: Some(
        crate::proto::confidence::flags::admin::v1::context_field_semantic_type::Type::Country(
          CountrySemanticType { format: CountryFormat::TwoLetterIsoCode.into() }
        )
      )}
    );

        expected_sem.insert(
      "vi_pratar_svenska_så_detta_är_tiden".to_string(),
      ContextFieldSemanticType { r#type: Some(
        crate::proto::confidence::flags::admin::v1::context_field_semantic_type::Type::Timestamp(
          TimestampSemanticType::default()
        )
      )}
    );

        expected_sem.insert(
      "version".to_string(),
      ContextFieldSemanticType { r#type: Some(
        crate::proto::confidence::flags::admin::v1::context_field_semantic_type::Type::Version(
          VersionSemanticType::default()
        )
      )}
    );

        expected_sem.insert(
      "today".to_string(),
      ContextFieldSemanticType { r#type: Some(
        crate::proto::confidence::flags::admin::v1::context_field_semantic_type::Type::Date(
          DateSemanticType::default()
        )
      )}
    );

        assert_eq!(schema.semantic_types, expected_sem);
    }

    #[test]
    fn decorates_with_list_schema() {
        let logger = ResolveLogger::new();
        let ctx: Struct = serde_json::from_value(json!({
          "country": ["SE","DK","NO"],
          "random_stuff": ["SE","abc",3]
        }))
        .unwrap();

        let cred = "clients/test/clientCredentials/test";
        let rv = [];
        logger.log_resolve("id", &ctx, cred, &rv);
        let req = logger.checkpoint();
        let crec = req
            .client_resolve_info
            .iter()
            .find(|c| c.client_credential == cred)
            .unwrap();
        let schema = &crec.schema[0];

        let mut expected_fields = BTreeMap::new();
        expected_fields.insert(
            "country".to_string(),
            evaluation_context_schema_field::Kind::StringKind as i32,
        );
        assert_eq!(schema.schema, expected_fields);

        let mut expected_sem = BTreeMap::new();
        expected_sem.insert(
      "country".to_string(),
      ContextFieldSemanticType { r#type: Some(
        crate::proto::confidence::flags::admin::v1::context_field_semantic_type::Type::Country(
          CountrySemanticType { format: CountryFormat::TwoLetterIsoCode.into() }
        )
      )}
    );
        assert_eq!(schema.semantic_types, expected_sem);
    }

    #[test]
    fn simple_resolve_stats() {
        use crate::proto::confidence::flags::admin::v1::{
            flag::{Rule, Variant},
            Flag, Segment,
        };

        let logger = ResolveLogger::new();

        let flag = Flag {
            name: "flags/test".into(),
            ..Default::default()
        };
        let rule = Rule {
            name: "flags/test/rules/r1".into(),
            ..Default::default()
        };
        let variant = Variant {
            name: "flags/test/variants/control".into(),
            value: Some(Struct::default()),
            ..Default::default()
        };
        let segment = Segment {
            name: "segments/test".into(),
            ..Default::default()
        };

        let rv = [crate::ResolvedValue::new(&flag)
            .with_variant_match(&rule, &segment, &variant, "control", "user123")];

        let cred = "clients/test/clientCredentials/test";
        logger.log_resolve("id", &Struct::default(), cred, &rv);
        let req = logger.checkpoint();

        let flag_info = req
            .flag_resolve_info
            .iter()
            .find(|f| f.flag == flag.name)
            .unwrap();
        // variant keyed by variant name
        assert_eq!(
            flag_info
                .variant_resolve_info
                .iter()
                .find(|v| v.variant == variant.name)
                .unwrap()
                .count,
            1
        );
        // rule
        let ri = flag_info
            .rule_resolve_info
            .iter()
            .find(|r| r.rule == rule.name)
            .unwrap();
        assert_eq!(ri.count, 1);
        assert_eq!(
            ri.assignment_resolve_info
                .iter()
                .find(|a| a.assignment_id == "control")
                .unwrap()
                .count,
            1
        );
    }

    #[test]
    fn fallthrough_resolve_stats() {
        use crate::proto::confidence::flags::admin::v1::{
            flag::{Rule, Variant},
            Flag, Segment,
        };

        let logger = ResolveLogger::new();

        let flag = Flag {
            name: "flags/test-fallthrough".into(),
            ..Default::default()
        };
        let fallthrough_rule = Rule {
            name: "flags/test-fallthrough/rules/fall".into(),
            ..Default::default()
        };
        let match_rule = Rule {
            name: "flags/test-fallthrough/rules/final".into(),
            ..Default::default()
        };
        let match_variant = Variant {
            name: "flags/test-fallthrough/variants/final".into(),
            value: Some(Struct::default()),
            ..Default::default()
        };
        let segment = Segment {
            name: "segments/test".into(),
            ..Default::default()
        };

        let mut rv = crate::ResolvedValue::new(&flag);
        rv.attribute_fallthrough_rule(&fallthrough_rule, "control", "user123");
        let rv = [rv.with_variant_match(&match_rule, &segment, &match_variant, "final", "user123")];

        let cred = "clients/test/clientCredentials/test";
        logger.log_resolve("id", &Struct::default(), cred, &rv);
        let req = logger.checkpoint();

        let flag_info = req
            .flag_resolve_info
            .iter()
            .find(|f| f.flag == flag.name)
            .unwrap();
        // variant keyed by variant name
        assert_eq!(
            flag_info
                .variant_resolve_info
                .iter()
                .find(|v| v.variant == match_variant.name)
                .unwrap()
                .count,
            1
        );
        // match rule
        let mr = flag_info
            .rule_resolve_info
            .iter()
            .find(|r| r.rule == match_rule.name)
            .unwrap();
        assert_eq!(mr.count, 1);
        assert_eq!(
            mr.assignment_resolve_info
                .iter()
                .find(|a| a.assignment_id == "final")
                .unwrap()
                .count,
            1
        );
        // fallthrough rule: count also increments
        let fr = flag_info
            .rule_resolve_info
            .iter()
            .find(|r| r.rule == fallthrough_rule.name)
            .unwrap();
        assert_eq!(fr.count, 1);
        assert_eq!(
            fr.assignment_resolve_info
                .iter()
                .find(|a| a.assignment_id == "control")
                .unwrap()
                .count,
            1
        );
    }

    #[test]
    fn test_resolve_counter() {
        let logger = ResolveLogger::new();
        let cred = "clients/test/clientCredentials/test";

        // Log 5 resolves
        for _ in 0..5 {
            logger.log_resolve("id", &Struct::default(), cred, &[]);
        }

        // Checkpoint and verify the counter
        let req = logger.checkpoint();
        assert_eq!(req.resolve_count, 5);

        // Log 3 more resolves
        for _ in 0..3 {
            logger.log_resolve("id", &Struct::default(), cred, &[]);
        }

        // Checkpoint again and verify the counter was reset and is now 3
        let req2 = logger.checkpoint();
        assert_eq!(req2.resolve_count, 3);
    }

    #[test]
    fn concurrent_logging_and_checkpointing() {
        use crate::proto::confidence::flags::admin::v1::{
            flag::{Rule, Variant},
            Flag, Segment,
        };
        use std::sync::{
            atomic::{AtomicBool, Ordering},
            Arc,
        };
        use std::thread;
        use std::time::Duration;

        let logger = Arc::new(ResolveLogger::new());
        let flag = Flag {
            name: "flags/concurrent".into(),
            ..Default::default()
        };
        let rule = Rule {
            name: "flags/concurrent/rules/r1".into(),
            ..Default::default()
        };
        let variant = Variant {
            name: "flags/concurrent/variants/v1".into(),
            value: Some(Struct::default()),
            ..Default::default()
        };
        let segment = Segment {
            name: "segments/test".into(),
            ..Default::default()
        };

        let cred = "clients/test/clientCredentials/test";
        let threads = 3usize;

        let done = Arc::new(AtomicBool::new(false));
        // Spawn 3 logging threads
        let mut handles = Vec::new();
        for _ in 0..threads {
            let lg = logger.clone();
            let f = flag.clone();
            let r = rule.clone();
            let v = variant.clone();
            let s = segment.clone();
            let cred_s = cred.to_string();
            let done_cl = done.clone();
            handles.push(thread::spawn(move || {
                let mut count = 0i64;
                while !done_cl.load(Ordering::Relaxed) {
                    let rv = [crate::ResolvedValue::new(&f)
                        .with_variant_match(&r, &s, &v, "assign", "user")];
                    lg.log_resolve("id", &Struct::default(), &cred_s, &rv);
                    count += 1;
                }
                count
            }));
        }

        // Spawn one checkpointing thread that checkpoints periodically and sends results
        use std::sync::mpsc::channel;
        let (tx, rx) = channel::<WriteFlagLogsRequest>();
        let lg = logger.clone();
        let tx_thread = tx.clone();
        let chk_handle = thread::spawn(move || {
            for _ in 0..10 {
                thread::sleep(Duration::from_millis(10));
                tx_thread.send(lg.checkpoint()).unwrap();
            }
        });

        chk_handle.join().unwrap();
        done.store(true, Ordering::Relaxed);
        let total_expected = handles.into_iter().map(|h| h.join().unwrap()).sum::<i64>();
        // logger.checkpoint().iter().
        tx.send(logger.checkpoint()).unwrap();

        // Aggregate all checkpoint outputs
        let mut sum_variants: i64 = 0;
        let mut sum_rules: i64 = 0;
        let mut sum_assign: i64 = 0;
        for req in rx.try_iter() {
            if let Some(flag_info) = req.flag_resolve_info.iter().find(|f| f.flag == flag.name) {
                sum_variants += flag_info
                    .variant_resolve_info
                    .iter()
                    .map(|v| v.count)
                    .sum::<i64>();
                sum_rules += flag_info
                    .rule_resolve_info
                    .iter()
                    .map(|r| r.count)
                    .sum::<i64>();
                sum_assign += flag_info
                    .rule_resolve_info
                    .iter()
                    .flat_map(|r| r.assignment_resolve_info.iter())
                    .map(|a| a.count)
                    .sum::<i64>();
            }
        }

        // Validate all produced data is accounted for across all checkpoints
        assert_eq!(sum_variants, total_expected);
        assert_eq!(sum_rules, total_expected);
        assert_eq!(sum_assign, total_expected);
    }
}
