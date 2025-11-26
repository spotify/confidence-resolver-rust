package com.spotify.confidence.materialization;

import java.util.Set;
import java.util.concurrent.CompletionStage;

public interface MaterializationWriter {
  
  sealed interface WriteOp permits WriteOp.SetVariant {
    String materialization();
    String unit();

    // Upsert a variant
    record SetVariant(String materialization, String unit, String rule, String variant) implements WriteOp {}

  }

  CompletionStage<Void> write(Set<WriteOp> ops);

}
