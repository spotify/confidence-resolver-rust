package com.spotify.confidence;

import com.spotify.confidence.flags.resolver.v1.ResolveFlagsRequest;
import com.spotify.confidence.flags.resolver.v1.ResolveFlagsResponse;
import java.util.concurrent.CompletableFuture;

public interface RemoteResolver {
  CompletableFuture<ResolveFlagsResponse> resolve(ResolveFlagsRequest request);

  void close();
}
