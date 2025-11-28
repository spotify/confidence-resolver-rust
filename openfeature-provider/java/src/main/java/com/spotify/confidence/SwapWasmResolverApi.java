package com.spotify.confidence;

import com.spotify.confidence.flags.resolver.v1.ResolveFlagsRequest;
import com.spotify.confidence.flags.resolver.v1.ResolveFlagsResponse;
import com.spotify.confidence.flags.resolver.v1.ResolveWithStickyRequest;
import com.spotify.confidence.flags.resolver.v1.ResolveWithStickyResponse;
import com.spotify.confidence.flags.resolver.v1.ResolveWithStickyResponse.MissingMaterializations;
import com.spotify.confidence.materialization.MaterializationStore;
import com.spotify.confidence.materialization.ReadOp;
import com.spotify.confidence.materialization.ReadResult;
import com.spotify.confidence.materialization.WriteOp;
import java.util.List;
import java.util.Set;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.CompletionStage;
import java.util.concurrent.atomic.AtomicReference;
import java.util.stream.Collectors;

class SwapWasmResolverApi implements ResolverApi {
  private final AtomicReference<WasmResolveApi> wasmResolverApiRef = new AtomicReference<>();
  private final MaterializationStore materializationStore;
  private final WasmFlagLogger flagLogger;

  public SwapWasmResolverApi(
      WasmFlagLogger flagLogger,
      byte[] initialState,
      String accountId,
      MaterializationStore stickyResolveStrategy) {
    this.materializationStore = stickyResolveStrategy;
    this.flagLogger = flagLogger;

    // Create initial instance
    final WasmResolveApi initialInstance = new WasmResolveApi(flagLogger);
    initialInstance.setResolverState(initialState, accountId);
    this.wasmResolverApiRef.set(initialInstance);
  }

  @Override
  public void init(byte[] state, String accountId) {
    updateStateAndFlushLogs(state, accountId);
  }

  @Override
  public void updateStateAndFlushLogs(byte[] state, String accountId) {
    // Create new instance with updated state
    final WasmResolveApi newInstance = new WasmResolveApi(flagLogger);
    newInstance.setResolverState(state, accountId);

    // Get current instance before switching
    final WasmResolveApi oldInstance = wasmResolverApiRef.getAndSet(newInstance);
    if (oldInstance != null) {
      oldInstance.close();
    }
  }

  /**
   * Closes the current WasmResolveApi instance, flushing any pending logs. This ensures logs are
   * not lost on shutdown.
   */
  @Override
  public void close() {
    final WasmResolveApi currentInstance = wasmResolverApiRef.getAndSet(null);
    if (currentInstance != null) {
      currentInstance.close();
    }
  }

  @Override
  public CompletionStage<ResolveFlagsResponse> resolveWithSticky(ResolveWithStickyRequest request) {
    final var instance = wasmResolverApiRef.get();
    final ResolveWithStickyResponse response;
    try {
      response = instance.resolveWithSticky(request);
    } catch (IsClosedException e) {
      return resolveWithSticky(request);
    }

    switch (response.getResolveResultCase()) {
      case SUCCESS -> {
        final var success = response.getSuccess();
        // Store updates if present
        if (!success.getUpdatesList().isEmpty()) {
          storeUpdates(success.getUpdatesList());
        }
        return CompletableFuture.completedFuture(success.getResponse());
      }
      case MISSING_MATERIALIZATIONS -> {
        final var missingMaterializations = response.getMissingMaterializations();

        // Check for ResolverFallback first - return early if so
        // TODO this needs to be handled differently...
        if (materializationStore instanceof ResolverFallback fallback) {
          return fallback.resolve(request.getResolveRequest());
        }

        // Handle MaterializationRepository case
        return handleMissingMaterializations(request, missingMaterializations)
            .thenCompose(this::resolveWithSticky);
      }
      case RESOLVERESULT_NOT_SET ->
          throw new RuntimeException("Invalid response: resolve result not set");
      default ->
          throw new RuntimeException("Unhandled response case: " + response.getResolveResultCase());
    }
  }

  private CompletionStage<Void> storeUpdates(
      List<ResolveWithStickyResponse.MaterializationUpdate> updates) {
    final Set<WriteOp> writeOps =
        updates.stream()
            .map(
                u ->
                    new WriteOp.Variant(
                        u.getWriteMaterialization(), u.getUnit(), u.getRule(), u.getVariant()))
            .collect(Collectors.toSet());

    return materializationStore.write(writeOps);
  }

  private CompletionStage<ResolveWithStickyRequest> handleMissingMaterializations(
      ResolveWithStickyRequest request, MissingMaterializations missingMaterializations) {

    final List<? extends ReadOp> readOps =
        missingMaterializations.getItemsList().stream()
            .map(mm -> new ReadOp.Variant(mm.getReadMaterialization(), mm.getUnit(), mm.getRule()))
            .toList();

    return materializationStore
        .read(readOps)
        .thenApply(
            results -> {
              final ResolveWithStickyRequest.Builder builder = request.toBuilder();

              results.stream()
                  .map(ReadResult.Variant.class::cast)
                  .filter(res -> res.variant().isPresent())
                  .forEach(
                      vr -> {
                        builder
                            .putMaterializationsPerUnitBuilderIfAbsent(vr.unit())
                            .putInfoMapBuilderIfAbsent(vr.materialization())
                            .putRuleToVariant(vr.rule(), vr.variant().get());
                      });
              return builder.build();
            });
  }

  @Override
  public ResolveFlagsResponse resolve(ResolveFlagsRequest request) {
    final var instance = wasmResolverApiRef.get();
    try {
      return instance.resolve(request);
    } catch (IsClosedException e) {
      return resolve(request);
    }
  }
}
