package com.spotify.confidence;

import java.util.Map;

public record MaterializationInfo(
    boolean isUnitInMaterialization, Map<String, String> ruleToVariant) {

  public com.spotify.confidence.flags.resolver.v1.MaterializationInfo toProto() {
    return com.spotify.confidence.flags.resolver.v1.MaterializationInfo.newBuilder()
        .setUnitInInfo(this.isUnitInMaterialization)
        .putAllRuleToVariant(this.ruleToVariant)
        .build();
  }
}
