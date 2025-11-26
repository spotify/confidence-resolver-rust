package com.spotify.confidence.materialization;

import java.util.Map;
import java.util.Optional;
import java.util.Set;
import java.util.concurrent.CompletionStage;

public interface MaterializationReader {

  /**
   * Get variants for the given keys.
   *
   * @param keys the keys to look up
   * @return a map from key to variant (empty Optional if not found)
   */
  CompletionStage<Map<MaterializationKey, Optional<String>>> getVariants(
      Set<MaterializationKey> keys);

  /**
   * Check which (materialization, unit) pairs are included.
   *
   * @param keys set of (materialization, unit) pairs to check (rule field is ignored)
   * @return set of (materialization, unit) pairs that exist
   */
  CompletionStage<Set<MaterializationKey>> checkInclusions(Set<MaterializationKey> keys);
}
