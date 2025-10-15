package com.spotify.confidence;

import java.util.Map;
import java.util.concurrent.CompletableFuture;

public non-sealed interface MaterializationRepository extends StickyResolveStrategy {
  CompletableFuture<Map<String, MaterializationInfo>> loadMaterializedAssignmentsForUnit(
      String unit, String materialization);

  CompletableFuture<Void> storeAssignment(
      String unit, Map<String, MaterializationInfo> assignments);
}
