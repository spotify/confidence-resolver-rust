package com.spotify.confidence.materialization;

import java.util.Optional;

public sealed interface ReadOp permits ReadOp.Inclusion, ReadOp.Variant {
  String materialization();

  String unit();

  // Query: does the materialization include the key?
  record Inclusion(String materialization, String unit) implements ReadOp {
    ReadResult.Inclusion toResult(boolean included) {
      return new ReadResult.Inclusion(materialization, unit, included);
    }
  }

  // Query: get variants for specific rules for this key
  record Variant(String materialization, String unit, String rule) implements ReadOp {
    ReadResult.Variant toResult(Optional<String> variant) {
      return new ReadResult.Variant(materialization, unit, rule, variant);
    }
  }
}
