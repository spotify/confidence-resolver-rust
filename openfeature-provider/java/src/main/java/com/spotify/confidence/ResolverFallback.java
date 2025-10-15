package com.spotify.confidence;

import com.spotify.confidence.flags.resolver.v1.ResolveFlagsRequest;
import com.spotify.confidence.flags.resolver.v1.ResolveFlagsResponse;
import java.util.concurrent.CompletableFuture;

public non-sealed interface ResolverFallback extends StickyResolveStrategy {
  CompletableFuture<ResolveFlagsResponse> resolve(ResolveFlagsRequest request);
}
