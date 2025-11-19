package com.spotify.confidence;

import static com.spotify.confidence.GrpcUtil.createConfidenceChannel;

import com.google.common.annotations.VisibleForTesting;
import com.google.common.util.concurrent.ThreadFactoryBuilder;
import com.spotify.confidence.TokenHolder.Token;
import com.spotify.confidence.flags.admin.v1.ResolverStateServiceGrpc;
import com.spotify.confidence.flags.admin.v1.ResolverStateServiceGrpc.ResolverStateServiceBlockingStub;
import com.spotify.confidence.iam.v1.AuthServiceGrpc;
import com.spotify.confidence.iam.v1.AuthServiceGrpc.AuthServiceBlockingStub;
import com.spotify.confidence.iam.v1.ClientCredential.ClientSecret;
import io.grpc.Channel;
import io.grpc.ClientInterceptors;
import io.grpc.protobuf.services.HealthStatusManager;
import java.time.Duration;
import java.util.Optional;
import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;

class LocalResolverServiceFactory implements ResolverServiceFactory {
  private final ResolverApi wasmResolveApi;
  private static final Duration POLL_LOG_INTERVAL = Duration.ofSeconds(10);
  private static final Duration DEFAULT_POLL_INTERVAL = Duration.ofMinutes(5);
  private static final ScheduledExecutorService flagsFetcherExecutor =
      Executors.newScheduledThreadPool(1, new ThreadFactoryBuilder().setDaemon(true).build());
  private final StickyResolveStrategy stickyResolveStrategy;
  private static final ScheduledExecutorService logPollExecutor =
      Executors.newScheduledThreadPool(1, new ThreadFactoryBuilder().setDaemon(true).build());

  private static long getPollIntervalSeconds() {
    return Optional.ofNullable(System.getenv("CONFIDENCE_RESOLVER_POLL_INTERVAL_SECONDS"))
        .map(Long::parseLong)
        .orElse(DEFAULT_POLL_INTERVAL.toSeconds());
  }

  static FlagResolverService from(
      ApiSecret apiSecret, StickyResolveStrategy stickyResolveStrategy) {
    return createFlagResolverService(apiSecret, stickyResolveStrategy);
  }

  static FlagResolverService from(
      AccountStateProvider accountStateProvider,
      String accountId,
      StickyResolveStrategy stickyResolveStrategy) {
    return createFlagResolverService(accountStateProvider, accountId, stickyResolveStrategy);
  }

  private static FlagResolverService createFlagResolverService(
      ApiSecret apiSecret, StickyResolveStrategy stickyResolveStrategy) {
    final var channel = createConfidenceChannel();
    final AuthServiceBlockingStub authService = AuthServiceGrpc.newBlockingStub(channel);
    final TokenHolder tokenHolder =
        new TokenHolder(apiSecret.clientId(), apiSecret.clientSecret(), authService);
    final Token token = tokenHolder.getToken();
    final Channel authenticatedChannel =
        ClientInterceptors.intercept(channel, new JwtAuthClientInterceptor(tokenHolder));
    final ResolverStateServiceBlockingStub resolverStateService =
        ResolverStateServiceGrpc.newBlockingStub(authenticatedChannel);
    final HealthStatusManager healthStatusManager = new HealthStatusManager();
    final HealthStatus healthStatus = new HealthStatus(healthStatusManager);
    final FlagsAdminStateFetcher sidecarFlagsAdminFetcher =
        new FlagsAdminStateFetcher(resolverStateService, healthStatus, token.account());
    // Perform initial reload to fetch state and set accountId before creating resolver
    sidecarFlagsAdminFetcher.reload();
    final long pollIntervalSeconds = getPollIntervalSeconds();
    final var wasmFlagLogger = new GrpcWasmFlagLogger(apiSecret);
    final ResolverApi wasmResolverApi =
        new ThreadLocalSwapWasmResolverApi(
            wasmFlagLogger,
            sidecarFlagsAdminFetcher.rawStateHolder().get(),
            sidecarFlagsAdminFetcher.accountId,
            stickyResolveStrategy);
    flagsFetcherExecutor.scheduleAtFixedRate(
        sidecarFlagsAdminFetcher::reload,
        pollIntervalSeconds,
        pollIntervalSeconds,
        TimeUnit.SECONDS);

    logPollExecutor.scheduleAtFixedRate(
        () -> {
          try {
            wasmResolverApi.updateStateAndFlushLogs(
                sidecarFlagsAdminFetcher.rawStateHolder().get(),
                sidecarFlagsAdminFetcher.accountId);
          } catch (Exception e) {
            System.err.println("Error in log poll executor: " + e.getMessage());
            e.printStackTrace();
          }
        },
        POLL_LOG_INTERVAL.getSeconds(),
        POLL_LOG_INTERVAL.getSeconds(),
        TimeUnit.SECONDS);

    return new WasmFlagResolverService(wasmResolverApi, stickyResolveStrategy);
  }

  private static FlagResolverService createFlagResolverService(
      AccountStateProvider accountStateProvider,
      String accountId,
      StickyResolveStrategy stickyResolveStrategy) {
    final var mode = System.getenv("LOCAL_RESOLVE_MODE");
    if (!(mode == null || mode.equals("WASM"))) {
      throw new RuntimeException("Only WASM mode supported with AccountStateProvider");
    }
    final long pollIntervalSeconds = getPollIntervalSeconds();
    final AtomicReference<byte[]> resolverStateProtobuf =
        new AtomicReference<>(accountStateProvider.provide());
    // No-op logger for wasm mode with AccountStateProvider
    final WasmFlagLogger flagLogger = new NoOpWasmFlagLogger();
    final ResolverApi wasmResolverApi =
        new ThreadLocalSwapWasmResolverApi(
            flagLogger, resolverStateProtobuf.get(), accountId, stickyResolveStrategy);
    flagsFetcherExecutor.scheduleAtFixedRate(
        () -> resolverStateProtobuf.set(accountStateProvider.provide()),
        pollIntervalSeconds,
        pollIntervalSeconds,
        TimeUnit.SECONDS);
    logPollExecutor.scheduleAtFixedRate(
        () -> wasmResolverApi.updateStateAndFlushLogs(resolverStateProtobuf.get(), accountId),
        POLL_LOG_INTERVAL.getSeconds(),
        POLL_LOG_INTERVAL.getSeconds(),
        TimeUnit.SECONDS);
    return new WasmFlagResolverService(wasmResolverApi, stickyResolveStrategy);
  }

  LocalResolverServiceFactory(
      ResolverApi wasmResolveApi, StickyResolveStrategy stickyResolveStrategy) {
    this.wasmResolveApi = wasmResolveApi;
    this.stickyResolveStrategy = stickyResolveStrategy;
  }

  @VisibleForTesting
  public void setState(byte[] state, String accountId) {
    if (this.wasmResolveApi != null) {
      wasmResolveApi.updateStateAndFlushLogs(state, accountId);
    }
  }

  @Override
  public FlagResolverService create(ClientSecret clientSecret) {
    return new WasmFlagResolverService(wasmResolveApi, stickyResolveStrategy);
  }
}
