use crate::proto::confidence::flags::resolver::v1::WriteFlagLogsRequest;
use crate::FlagToApply;

mod pb {
    pub use crate::proto::confidence::flags::resolver::v1::events::{
        flag_assigned::{
            self, applied_flag::Assignment, AppliedFlag, AssignmentInfo, DefaultAssignment,
        },
        ClientInfo,
    };
    pub use crate::proto::confidence::flags::resolver::v1::{events::FlagAssigned, ResolveReason};
    pub use flag_assigned::default_assignment::DefaultAssignmentReason;
}

#[derive(Debug, Default)]
pub struct AssignLogger {
    assigned: crossbeam_queue::SegQueue<pb::FlagAssigned>,
}

impl AssignLogger {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn log_assigns(
        &self,
        resolve_id: &str,
        _evaluation_context: &crate::proto::google::Struct,
        assigned_flags: &[FlagToApply],
        client: &crate::Client,
        sdk: &Option<crate::flags_resolver::Sdk>,
    ) {
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

        self.assigned.push(pb::FlagAssigned {
            resolve_id: resolve_id.to_string(),
            client_info,
            flags,
        });
    }

    pub fn checkpoint(&self) -> WriteFlagLogsRequest {
        let mut assigned = Vec::new();
        while let Some(ev) = self.assigned.pop() {
            assigned.push(ev);
        }
        WriteFlagLogsRequest {
            flag_resolve_info: Vec::new(),
            client_resolve_info: Vec::new(),
            flag_assigned: assigned,
            telemetry_data: None,
        }
    }
}
