package com.spotify.confidence;

import com.spotify.confidence.flags.resolver.v1.FlagResolverServiceGrpc;
import com.spotify.confidence.flags.resolver.v1.ResolveFlagsRequest;
import com.spotify.confidence.flags.resolver.v1.ResolveFlagsResponse;
import io.grpc.ManagedChannel;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.TimeUnit;

/**
 * A simplified gRPC-based flag resolver for fallback scenarios in the local provider. This is a
 * copy of the core functionality from GrpcFlagResolver adapted for the local provider's needs.
 */
public class ConfidenceGrpcFlagResolver implements RemoteResolver {
  private final ManagedChannel channel;

  private final FlagResolverServiceGrpc.FlagResolverServiceFutureStub stub;

  public ConfidenceGrpcFlagResolver(ChannelFactory channelFactory) {
    this.channel = GrpcUtil.createConfidenceChannel(channelFactory);
    this.stub = FlagResolverServiceGrpc.newFutureStub(channel);
  }

  @Override
  public CompletableFuture<ResolveFlagsResponse> resolve(ResolveFlagsRequest request) {
    return GrpcUtil.toCompletableFuture(
        stub.withDeadlineAfter(10_000, TimeUnit.MILLISECONDS).resolveFlags(request));
  }

  @Override
  public void close() {
    channel.shutdownNow();
  }
}
