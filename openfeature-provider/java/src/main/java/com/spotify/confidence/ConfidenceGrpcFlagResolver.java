package com.spotify.confidence;

import com.google.protobuf.Struct;
import com.spotify.confidence.flags.resolver.v1.FlagResolverServiceGrpc;
import com.spotify.confidence.flags.resolver.v1.ResolveFlagsRequest;
import com.spotify.confidence.flags.resolver.v1.ResolveFlagsResponse;
import com.spotify.confidence.flags.resolver.v1.Sdk;
import com.spotify.confidence.flags.resolver.v1.Sdk.Builder;
import com.spotify.confidence.flags.resolver.v1.SdkId;
import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;
import java.time.Duration;
import java.util.List;
import java.util.Optional;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.TimeUnit;

/**
 * A simplified gRPC-based flag resolver for fallback scenarios in the local provider. This is a
 * copy of the core functionality from GrpcFlagResolver adapted for the local provider's needs.
 */
public class ConfidenceGrpcFlagResolver {
  private final ManagedChannel channel;
  private final Builder sdkBuilder =
      Sdk.newBuilder().setVersion("0.2.8"); // Using static version for local provider

  private final FlagResolverServiceGrpc.FlagResolverServiceFutureStub stub;

  public ConfidenceGrpcFlagResolver() {
    final String confidenceDomain =
        Optional.ofNullable(System.getenv("CONFIDENCE_DOMAIN")).orElse("edge-grpc.spotify.com");
    final boolean useGrpcPlaintext =
        Optional.ofNullable(System.getenv("CONFIDENCE_GRPC_PLAINTEXT"))
            .map(Boolean::parseBoolean)
            .orElse(false);

    ManagedChannelBuilder<?> builder = ManagedChannelBuilder.forTarget(confidenceDomain);
    if (useGrpcPlaintext) {
      builder = builder.usePlaintext();
    }

    final ManagedChannel channel =
        builder.intercept(new DefaultDeadlineClientInterceptor(Duration.ofMinutes(1))).build();

    this.channel = channel;
    this.stub = FlagResolverServiceGrpc.newFutureStub(channel);
  }

  public CompletableFuture<ResolveFlagsResponse> resolve(
      List<String> flags, String clientSecret, Struct context) {
    return GrpcUtil.toCompletableFuture(
        stub.withDeadlineAfter(10_000, TimeUnit.MILLISECONDS)
            .resolveFlags(
                ResolveFlagsRequest.newBuilder()
                    .setClientSecret(clientSecret)
                    .addAllFlags(flags)
                    .setEvaluationContext(context)
                    .setSdk(sdkBuilder.setId(SdkId.SDK_ID_JAVA_PROVIDER).build())
                    .setApply(true)
                    .build()));
  }

  public void close() {
    channel.shutdownNow();
  }
}
