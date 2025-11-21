package com.spotify.confidence;

import io.grpc.ClientInterceptor;
import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;
import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.Optional;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.TimeUnit;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

/**
 * Default implementation of ChannelFactory that creates standard gRPC channels
 * with security settings based on environment variables.
 *
 * <p>This factory:
 * <ul>
 *   <li>Uses TLS by default, unless CONFIDENCE_GRPC_PLAINTEXT=true</li>
 *   <li>Adds a default deadline interceptor (1 minute timeout)</li>
 *   <li>Applies any additional interceptors passed via defaultInterceptors</li>
 *   <li>Tracks all created channels and shuts them down when {@link #shutdown()} is called</li>
 * </ul>
 */
public class DefaultChannelFactory implements ChannelFactory {
  private static final Logger logger = LoggerFactory.getLogger(DefaultChannelFactory.class);
  private final List<ManagedChannel> channels = new CopyOnWriteArrayList<>();

  @Override
  public ManagedChannel create(String target, List<ClientInterceptor> defaultInterceptors) {
    final boolean useGrpcPlaintext =
        Optional.ofNullable(System.getenv("CONFIDENCE_GRPC_PLAINTEXT"))
            .map(Boolean::parseBoolean)
            .orElse(false);

    ManagedChannelBuilder<?> builder = ManagedChannelBuilder.forTarget(target);
    if (useGrpcPlaintext) {
      builder = builder.usePlaintext();
    }

    // Combine default interceptors with the deadline interceptor
    List<ClientInterceptor> allInterceptors = new ArrayList<>(defaultInterceptors);
    allInterceptors.add(new DefaultDeadlineClientInterceptor(Duration.ofMinutes(1)));

    ManagedChannel channel = builder.intercept(allInterceptors.toArray(new ClientInterceptor[0])).build();
    channels.add(channel);
    return channel;
  }

  @Override
  public void shutdown() {
    logger.debug("Shutting down {} channels created by DefaultChannelFactory", channels.size());
    for (ManagedChannel channel : channels) {
      try {
        channel.shutdown();
        if (!channel.awaitTermination(5, TimeUnit.SECONDS)) {
          logger.warn("Channel did not terminate within 5 seconds, forcing shutdown");
          channel.shutdownNow();
        }
      } catch (InterruptedException e) {
        logger.warn("Interrupted while shutting down channel", e);
        channel.shutdownNow();
        Thread.currentThread().interrupt();
      }
    }
    channels.clear();
  }
}
