package com.spotify.confidence;

import com.spotify.confidence.flags.resolver.v1.ResolveFlagsRequest;
import com.spotify.confidence.flags.resolver.v1.ResolveFlagsResponse;
import com.spotify.confidence.flags.resolver.v1.ResolveWithStickyRequest;
import java.util.concurrent.CompletableFuture;

/** Common interface for WASM-based flag resolver implementations. */
interface ResolverApi {

  /**
   * Resolves flags with sticky assignment support.
   *
   * @param request The resolve request with sticky context
   * @return A future containing the resolve response
   */
  CompletableFuture<ResolveFlagsResponse> resolveWithSticky(ResolveWithStickyRequest request);

  /**
   * Resolves flags without sticky assignment support.
   *
   * @param request The resolve request
   * @return The resolve response
   */
  ResolveFlagsResponse resolve(ResolveFlagsRequest request);

  void init(byte[] state, String accountId);

  /**
   * Updates the resolver state and flushes any pending logs.
   *
   * @param state The new resolver state
   * @param accountId The account ID
   */
  void updateStateAndFlushLogs(byte[] state, String accountId);

  /** Closes the resolver and releases any resources. */
  void close();
}
