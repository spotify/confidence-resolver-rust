package com.spotify.confidence;

import java.util.List;
import java.util.Optional;
import java.util.Set;
import java.util.concurrent.CompletionStage;

public interface MaterializationStore {

  CompletionStage<List<ReadResult>> read(List<? extends ReadOp> ops);

  default CompletionStage<Void> write(Set<? extends WriteOp> ops) {
    throw new UnsupportedOperationException("Unimplemented method 'write'");
  }

  sealed interface WriteOp permits WriteOp.Variant {
    String materialization();

    String unit();

    // Upsert a variant
    record Variant(String materialization, String unit, String rule, String variant)
        implements WriteOp {}
  }

  sealed interface ReadResult permits ReadResult.Inclusion, ReadResult.Variant {
    String materialization();

    String unit();

    // Result for Inclusion
    record Inclusion(String materialization, String unit, boolean included) implements ReadResult {}

    // Result for GetRules
    record Variant(String materialization, String unit, String rule, Optional<String> variant)
        implements ReadResult {}
  }

  sealed interface ReadOp permits ReadOp.Inclusion, ReadOp.Variant {
    String materialization();

    String unit();

    // Query: does the materialization include the key?
    record Inclusion(String materialization, String unit) implements ReadOp {
      public ReadResult.Inclusion toResult(boolean included) {
        return new ReadResult.Inclusion(materialization, unit, included);
      }
    }

    // Query: get variants for specific rules for this key
    record Variant(String materialization, String unit, String rule) implements ReadOp {
      public ReadResult.Variant toResult(Optional<String> variant) {
        return new ReadResult.Variant(materialization, unit, rule, variant);
      }
    }
  }
}
