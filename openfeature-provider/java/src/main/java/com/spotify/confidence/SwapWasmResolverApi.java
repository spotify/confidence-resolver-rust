package com.spotify.confidence;

import com.spotify.confidence.flags.resolver.v1.*;
import com.spotify.confidence.flags.resolver.v1.ResolveWithStickyResponse.MissingMaterializations;
import java.util.List;
import java.util.Set;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.CompletionStage;
import java.util.concurrent.atomic.AtomicReference;
import java.util.stream.Collectors;

class SwapWasmResolverApi implements ResolverApi {
  private static final int MAX_CLOSED_RETRIES = 10;
  private static final int MAX_MATERIALIZATION_RETRIES = 3;

  private final AtomicReference<WasmResolveApi> wasmResolverApiRef = new AtomicReference<>();
  private final MaterializationStore materializationStore;
  private final WasmFlagLogger flagLogger;

  public SwapWasmResolverApi(
      WasmFlagLogger flagLogger,
      byte[] initialState,
      String accountId,
      MaterializationStore materializationStore) {
    this.materializationStore = materializationStore;
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
    return resolveWithStickyInternal(request, 0, 0);
  }

  private CompletionStage<ResolveFlagsResponse> resolveWithStickyInternal(
      ResolveWithStickyRequest request, int closedRetries, int materializationRetries) {
    final var instance = wasmResolverApiRef.get();
    final ResolveWithStickyResponse response;
    try {
      response = instance.resolveWithSticky(request);
    } catch (IsClosedException e) {
      if (closedRetries >= MAX_CLOSED_RETRIES) {
        throw new RuntimeException(
            "Max retries exceeded for IsClosedException: " + MAX_CLOSED_RETRIES, e);
      }
      return resolveWithStickyInternal(request, closedRetries + 1, materializationRetries);
    }

    switch (response.getResolveResultCase()) {
      case SUCCESS -> {
        final var success = response.getSuccess();
        if (!success.getUpdatesList().isEmpty()) {
          storeUpdates(success.getUpdatesList());
        }
        return CompletableFuture.completedFuture(success.getResponse());
      }
      case MISSING_MATERIALIZATIONS -> {
        if (materializationRetries >= MAX_MATERIALIZATION_RETRIES) {
          throw new RuntimeException(
              "Max retries exceeded for missing materializations: " + MAX_MATERIALIZATION_RETRIES);
        }
        final var missingMaterializations = response.getMissingMaterializations();
        return handleMissingMaterializations(request, missingMaterializations)
            .thenCompose(
                req -> resolveWithStickyInternal(req, closedRetries, materializationRetries + 1));
      }
      case RESOLVERESULT_NOT_SET ->
          throw new RuntimeException("Invalid response: resolve result not set");
      default ->
          throw new RuntimeException("Unhandled response case: " + response.getResolveResultCase());
    }
  }

  private CompletionStage<Void> storeUpdates(
      List<ResolveWithStickyResponse.MaterializationUpdate> updates) {
    final Set<MaterializationStore.WriteOp> writeOps =
        updates.stream()
            .map(
                u ->
                    new MaterializationStore.WriteOp.Variant(
                        u.getWriteMaterialization(), u.getUnit(), u.getRule(), u.getVariant()))
            .collect(Collectors.toSet());

    return materializationStore.write(writeOps);
  }

  private CompletionStage<ResolveWithStickyRequest> handleMissingMaterializations(
      ResolveWithStickyRequest request, MissingMaterializations missingMaterializations) {

    final List<? extends MaterializationStore.ReadOp> readOps =
        missingMaterializations.getItemsList().stream()
            .map(
                mm ->
                    new MaterializationStore.ReadOp.Variant(
                        mm.getReadMaterialization(), mm.getUnit(), mm.getRule()))
            .toList();

    return materializationStore
        .read(readOps)
        .thenApply(
            results -> {
              final ResolveWithStickyRequest.Builder builder = request.toBuilder();

              results.stream()
                  .map(MaterializationStore.ReadResult.Variant.class::cast)
                  .forEach(
                      vr -> {
                        MaterializationInfo.Builder matBuilder =
                            builder
                                .putMaterializationsPerUnitBuilderIfAbsent(vr.unit())
                                .putInfoMapBuilderIfAbsent(vr.materialization());
                        vr.variant()
                            .ifPresent(
                                variant -> {
                                  matBuilder
                                      .putRuleToVariant(vr.rule(), variant)
                                      .setUnitInInfo(true);
                                });
                      });
              return builder.build();
            });
  }
}
