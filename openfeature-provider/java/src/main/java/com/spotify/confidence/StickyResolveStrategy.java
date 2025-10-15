package com.spotify.confidence;

public sealed interface StickyResolveStrategy
    permits MaterializationRepository, ResolverFallback, RemoteResolverFallback {
  void close();
}
