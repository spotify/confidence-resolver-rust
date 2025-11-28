package com.spotify.confidence.materialization;

public sealed interface WriteOp permits WriteOp.Variant {
  String materialization();

  String unit();

  // Upsert a variant
  record Variant(String materialization, String unit, String rule, String variant)
      implements WriteOp {}
}
