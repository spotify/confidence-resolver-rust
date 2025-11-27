package com.spotify.confidence.materialization;

/** Key for reading or writing a variant assignment within a materialization. */
public record VariantKey(String materialization, String unit, String rule) {

  /** Convert to an inclusion key (drops the rule). */
  public InclusionKey toInclusionKey() {
    return new InclusionKey(materialization, unit);
  }
}

