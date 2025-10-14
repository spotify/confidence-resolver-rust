package com.spotify.confidence;

import com.spotify.confidence.flags.resolver.v1.ResolveFlagsRequest;
import com.spotify.confidence.flags.resolver.v1.ResolveFlagsResponse;
import java.util.concurrent.CompletableFuture;

interface FlagResolverService {
  CompletableFuture<ResolveFlagsResponse> resolveFlags(ResolveFlagsRequest request);

  void close();
}
