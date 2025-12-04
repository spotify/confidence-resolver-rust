package com.spotify.confidence;

import java.util.List;
import java.util.Set;
import java.util.concurrent.CompletionStage;

/**
 * A store that doesn't support the feature and where the provider resorts to use gRPC to resolve
 * flags when the WASM resolver encounters missing materializations. This provides a fallback to the
 * remote Confidence service.
 */
final class UnsupportedMaterializationStore implements MaterializationStore {

  @Override
  public CompletionStage<List<ReadResult>> read(List<? extends ReadOp> ops) {
    throw new MaterializationNotSupportedException();
  }

  @Override
  public CompletionStage<Void> write(Set<? extends WriteOp> ops) {
    throw new MaterializationNotSupportedException();
  }
}
