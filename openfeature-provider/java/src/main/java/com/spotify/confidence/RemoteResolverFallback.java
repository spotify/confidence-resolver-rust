package com.spotify.confidence;

import com.google.protobuf.Struct;
import com.spotify.confidence.flags.resolver.v1.ResolveFlagsRequest;
import com.spotify.confidence.flags.resolver.v1.ResolveFlagsResponse;
import java.util.concurrent.CompletableFuture;

/**
 * A fallback resolver strategy that uses gRPC to resolve flags when the WASM resolver encounters
 * missing materializations. This provides a fallback to the remote Confidence service.
 */
final class RemoteResolverFallback implements ResolverFallback, StickyResolveStrategy {
  private final ConfidenceGrpcFlagResolver grpcFlagResolver;

  RemoteResolverFallback() {
    this.grpcFlagResolver = new ConfidenceGrpcFlagResolver();
  }

  @Override
  public CompletableFuture<ResolveFlagsResponse> resolve(ResolveFlagsRequest request) {
    if (request.getFlagsList().isEmpty()) {
      return CompletableFuture.completedFuture(ResolveFlagsResponse.newBuilder().build());
    }

    final Struct context = request.getEvaluationContext();

    return grpcFlagResolver.resolve(request.getFlagsList(), request.getClientSecret(), context);
  }

  /** Closes the underlying gRPC resources. */
  public void close() {
    grpcFlagResolver.close();
  }
}
