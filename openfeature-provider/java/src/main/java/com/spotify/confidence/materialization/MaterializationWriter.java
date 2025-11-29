package com.spotify.confidence.materialization;

import java.util.Map;
import java.util.concurrent.CompletionStage;

public interface MaterializationWriter {

  /**
   * Set variants for the given keys. This also marks the (materialization, unit) as included.
   *
   * @param variants map from variant key to variant value
   * @return completion stage that completes when the write is done
   */
  CompletionStage<Void> setVariants(Map<VariantKey, String> variants);
}
