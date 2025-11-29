package com.spotify.confidence.materialization;

import java.util.Map;
import java.util.Optional;
import java.util.Set;
import java.util.concurrent.CompletionStage;

public interface MaterializationReader {

  /**
   * Get variants for the given keys.
   *
   * @param keys the variant keys to look up
   * @return a map from key to variant (empty Optional if not found)
   */
  CompletionStage<Map<VariantKey, Optional<String>>> getVariants(Set<VariantKey> keys);

  /**
   * Check which (materialization, unit) pairs are included.
   *
   * @param keys set of inclusion keys to check
   * @return set of keys that exist
   */
  CompletionStage<Set<InclusionKey>> checkInclusions(Set<InclusionKey> keys);
}
