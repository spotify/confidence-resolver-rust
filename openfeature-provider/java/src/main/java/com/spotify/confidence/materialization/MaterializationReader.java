package com.spotify.confidence.materialization;

import java.util.List;
import java.util.Optional;
import java.util.concurrent.CompletionStage;

public interface MaterializationReader {

  sealed interface ReadOp permits ReadOp.Inclusion, ReadOp.GetVariant {
    String materialization();
    String unit();

    // Query: does the materialization include the key?
    record Inclusion(String materialization, String unit) implements ReadOp {
      ReadResult.InclusionResult toResult(boolean included) {
        return new ReadResult.InclusionResult(materialization, unit, included);
      }
    }

    // Query: get variants for specific rules for this key
    record GetVariant(String materialization, String unit, String rule) implements ReadOp {
      ReadResult.VariantResult toResult(Optional<String> variant) {
        return new ReadResult.VariantResult(materialization, unit, rule, variant);
      }
    }
  }

  sealed interface ReadResult permits ReadResult.InclusionResult, ReadResult.VariantResult {
    String materialization();
    String unit();

    // Result for Inclusion
    record InclusionResult(String materialization, String unit, boolean included) implements ReadResult {}

    // Result for GetRules
    record VariantResult(String materialization, String unit, String rule, Optional<String> variant) implements ReadResult {}
  }

  CompletionStage<List<ReadResult>> read(List<ReadOp> ops);

}