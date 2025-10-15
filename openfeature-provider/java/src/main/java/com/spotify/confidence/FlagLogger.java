package com.spotify.confidence;

import com.google.protobuf.Timestamp;
import com.spotify.confidence.flags.resolver.v1.ResolveReason;
import com.spotify.confidence.flags.resolver.v1.Sdk;
import com.spotify.confidence.flags.resolver.v1.events.ClientInfo;
import com.spotify.confidence.flags.resolver.v1.events.FlagAssigned;
import com.spotify.confidence.flags.resolver.v1.events.FlagAssigned.DefaultAssignment.DefaultAssignmentReason;
import java.util.List;

interface FlagLogger {

  static FlagAssigned createFlagAssigned(
      String resolveId, Sdk sdk, List<FlagToApply> flagsToApply, AccountClient accountClient) {
    final var clientInfo =
        ClientInfo.newBuilder()
            .setClient(accountClient.client().getName())
            .setClientCredential(accountClient.clientCredential().getName())
            .setSdk(sdk)
            .build();

    final var builder = FlagAssigned.newBuilder().setResolveId(resolveId).setClientInfo(clientInfo);
    for (var flag : flagsToApply) {
      final var assignedFlag = flag.assignment();
      final FlagAssigned.AppliedFlag.Builder assignedBuilder =
          FlagAssigned.AppliedFlag.newBuilder()
              .setAssignmentId(assignedFlag.getAssignmentId())
              .setFlag(assignedFlag.getFlag())
              .setApplyTime(
                  Timestamp.newBuilder()
                      .setSeconds(flag.skewAdjustedAppliedTime().getEpochSecond())
                      .setNanos(flag.skewAdjustedAppliedTime().getNano())
                      .build())
              .setTargetingKey(assignedFlag.getTargetingKey())
              .setTargetingKeySelector(assignedFlag.getTargetingKeySelector())
              .setRule(assignedFlag.getRule())
              .addAllFallthroughAssignments(assignedFlag.getFallthroughAssignmentsList());

      if (!assignedFlag.getVariant().isBlank()) {
        assignedBuilder.setAssignmentInfo(
            FlagAssigned.AssignmentInfo.newBuilder()
                .setSegment(assignedFlag.getSegment())
                .setVariant(assignedFlag.getVariant())
                .build());
      } else {
        assignedBuilder.setDefaultAssignment(
            FlagAssigned.DefaultAssignment.newBuilder()
                .setReason(resolveToAssignmentReason(assignedFlag.getReason()))
                .build());
      }
      builder.addFlags(assignedBuilder);
    }

    return builder.build();
  }

  @SuppressWarnings("deprecation")
  private static DefaultAssignmentReason resolveToAssignmentReason(ResolveReason reason) {
    return switch (reason) {
      case RESOLVE_REASON_NO_SEGMENT_MATCH -> DefaultAssignmentReason.NO_SEGMENT_MATCH;
      case RESOLVE_REASON_NO_TREATMENT_MATCH -> DefaultAssignmentReason.NO_TREATMENT_MATCH;
      case RESOLVE_REASON_FLAG_ARCHIVED -> DefaultAssignmentReason.FLAG_ARCHIVED;
      default -> DefaultAssignmentReason.DEFAULT_ASSIGNMENT_REASON_UNSPECIFIED;
    };
  }
}
