package com.spotify.confidence.materialization;

import java.util.List;
import java.util.Set;
import java.util.concurrent.CompletionStage;

public interface MaterializationStore {

  CompletionStage<List<ReadResult>> read(List<? extends ReadOp> ops);

  default CompletionStage<Void> write(Set<? extends WriteOp> ops) {
    throw new UnsupportedOperationException("Unimplemented method 'write'");
  }
}
