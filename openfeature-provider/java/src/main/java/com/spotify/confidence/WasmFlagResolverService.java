package com.spotify.confidence;

import com.spotify.confidence.flags.resolver.v1.ResolveWithStickyRequest;
import com.spotify.confidence.flags.resolver.v1.ResolveFlagsRequest;
import com.spotify.confidence.flags.resolver.v1.ResolveFlagsResponse;
import java.util.concurrent.CompletableFuture;

record WasmFlagResolverService(
    ResolverApi wasmResolveApi, StickyResolveStrategy stickyResolveStrategy)
    implements FlagResolverService {
  @Override
  public CompletableFuture<ResolveFlagsResponse> resolveFlags(ResolveFlagsRequest request) {
    return wasmResolveApi.resolveWithSticky(
        ResolveWithStickyRequest.newBuilder()
            .setResolveRequest(request)
            .setFailFastOnSticky(getFailFast(stickyResolveStrategy))
            .build());
  }

  private static boolean getFailFast(StickyResolveStrategy stickyResolveStrategy) {
    return stickyResolveStrategy instanceof ResolverFallback;
  }

  public void close() {
    wasmResolveApi.close();
  }
}
