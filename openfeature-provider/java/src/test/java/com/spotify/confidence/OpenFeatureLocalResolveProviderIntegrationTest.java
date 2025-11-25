package com.spotify.confidence;

import static org.assertj.core.api.AssertionsForInterfaceTypes.assertThat;
import static org.junit.jupiter.api.Assertions.*;

import com.auth0.jwt.JWT;
import com.auth0.jwt.algorithms.Algorithm;
import com.google.protobuf.Timestamp;
import com.spotify.confidence.flags.admin.v1.ResolverStateServiceGrpc;
import com.spotify.confidence.flags.admin.v1.ResolverStateUriRequest;
import com.spotify.confidence.flags.admin.v1.ResolverStateUriResponse;
import com.spotify.confidence.flags.resolver.v1.InternalFlagLoggerServiceGrpc;
import com.spotify.confidence.flags.resolver.v1.WriteFlagAssignedRequest;
import com.spotify.confidence.flags.resolver.v1.WriteFlagAssignedResponse;
import com.spotify.confidence.flags.resolver.v1.WriteFlagLogsRequest;
import com.spotify.confidence.flags.resolver.v1.WriteFlagLogsResponse;
import com.spotify.confidence.iam.v1.AccessToken;
import com.spotify.confidence.iam.v1.AuthServiceGrpc;
import com.spotify.confidence.iam.v1.RequestAccessTokenRequest;
import com.sun.net.httpserver.HttpServer;
import dev.openfeature.sdk.*;
import dev.openfeature.sdk.exceptions.FlagNotFoundError;
import io.grpc.ClientInterceptor;
import io.grpc.ManagedChannel;
import io.grpc.inprocess.InProcessChannelBuilder;
import io.grpc.inprocess.InProcessServerBuilder;
import io.grpc.stub.StreamObserver;
import io.grpc.testing.GrpcCleanupRule;
import java.io.File;
import java.io.OutputStream;
import java.net.InetSocketAddress;
import java.nio.file.Files;
import java.time.Instant;
import java.time.temporal.ChronoUnit;
import java.util.ArrayList;
import java.util.Date;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

/**
 * Unit tests for OpenFeatureLocalResolveProvider using mocked gRPC services.
 *
 * <p>This test uses in-process gRPC server to test the provider without requiring external
 * services.
 */
class OpenFeatureLocalResolveProviderIntegrationTest {
  private static final String TEST_CLIENT_ID = "test-client-id";
  private static final String TEST_CLIENT_SECRET = "test-client-secret";
  private static final String FLAG_CLIENT_SECRET = "mkjJruAATQWjeY7foFIWfVAcBWnci2YF";
  private static final String ACCOUNT_NAME = "accounts/test-account";

  private final GrpcCleanupRule grpcCleanup = new GrpcCleanupRule();
  private String serverName;
  private OpenFeatureLocalResolveProvider provider;
  private MockAuthService mockAuthService;
  private MockResolverStateService mockResolverStateService;
  private MockFlagLoggerService mockFlagLoggerService;
  private HttpServer httpServer;

  @BeforeEach
  void setUp() throws Exception {
    serverName = InProcessServerBuilder.generateName();

    // Start HTTP server to serve resolver state file
    httpServer = HttpServer.create(new InetSocketAddress(0), 0);
    httpServer.createContext(
        "/resolver_state.pb",
        exchange -> {
          try {
            final byte[] stateBytes =
                Files.readAllBytes(
                    new File(getClass().getResource("/resolver_state_current.pb").getPath())
                        .toPath());

            exchange.getResponseHeaders().set("Content-Type", "application/octet-stream");
            exchange.getResponseHeaders().set("ETag", "\"test-etag\"");

            // Handle conditional GET
            final String ifNoneMatch = exchange.getRequestHeaders().getFirst("if-none-match");
            if ("\"test-etag\"".equals(ifNoneMatch)) {
              exchange.sendResponseHeaders(304, -1);
            } else {
              exchange.sendResponseHeaders(200, stateBytes.length);
              try (OutputStream os = exchange.getResponseBody()) {
                os.write(stateBytes);
              }
            }
          } finally {
            exchange.close();
          }
        });
    httpServer.start();

    mockAuthService = new MockAuthService();
    mockResolverStateService = new MockResolverStateService(httpServer.getAddress().getPort());
    mockFlagLoggerService = new MockFlagLoggerService();

    // Start in-process server with mock services
    grpcCleanup.register(
        InProcessServerBuilder.forName(serverName)
            .directExecutor()
            .addService(mockAuthService)
            .addService(mockResolverStateService)
            .addService(mockFlagLoggerService)
            .build()
            .start());

    // Create custom channel factory that connects to in-process server
    final ChannelFactory testChannelFactory =
        new ChannelFactory() {
          private final List<ManagedChannel> channels = new ArrayList<>();

          @Override
          public ManagedChannel create(String target, List<ClientInterceptor> interceptors) {
            InProcessChannelBuilder builder = InProcessChannelBuilder.forName(serverName);
            if (!interceptors.isEmpty()) {
              builder.intercept(interceptors.toArray(new ClientInterceptor[0]));
            }
            ManagedChannel channel = builder.build();
            this.channels.add(channel);
            return channel;
          }

          @Override
          public void shutdown() {
            channels.stream().forEach(ManagedChannel::shutdown);
          }
        };

    // Create provider with test configuration
    final ApiSecret apiSecret = new ApiSecret(TEST_CLIENT_ID, TEST_CLIENT_SECRET);
    final LocalProviderConfig config = new LocalProviderConfig(apiSecret, testChannelFactory);
    provider = new OpenFeatureLocalResolveProvider(config, FLAG_CLIENT_SECRET);
  }

  @AfterEach
  void tearDown() {
    if (provider != null) {
      provider.shutdown();
    }
    if (httpServer != null) {
      httpServer.stop(0);
    }
  }

  @Test
  void testProviderInitialization() throws Exception {
    assertEquals(ProviderState.NOT_READY, provider.getState());

    provider.initialize(new ImmutableContext());

    assertEquals(ProviderState.READY, provider.getState());
  }

  @Test
  void testMetadata() {
    assertEquals("confidence-sdk-java-local", provider.getMetadata().getName());
  }

  @Test
  void testResolveTutorialFeatureFlag() throws Exception {
    provider.initialize(new ImmutableContext());

    final ImmutableContext context =
        new ImmutableContext(
            "tutorial_visitor", Map.of("visitor_id", new Value("tutorial_visitor")));

    final ProviderEvaluation<Value> evaluation =
        provider.getObjectEvaluation("tutorial-feature", new Value("default"), context);

    assertThat(evaluation.getReason()).isEqualTo("RESOLVE_REASON_MATCH");
    assertThat(evaluation.getVariant()).isNotNull();
    assertThat(evaluation.getVariant())
        .isEqualTo("flags/tutorial-feature/variants/exciting-welcome");
    assertThat(evaluation.getValue().asStructure().asMap())
        .containsExactlyInAnyOrderEntriesOf(
            Map.of(
                "title",
                new Value("Welcome to Confidence!"),
                "message",
                new Value(
                    "We are very excited to welcome you to Confidence! This is a message from the tutorial flag.")));

    ProviderEvaluation<String> stringEvaluation =
        provider.getStringEvaluation("tutorial-feature.message", "meh", context);
    assertThat(stringEvaluation.getValue())
        .isEqualTo(
            "We are very excited to welcome you to Confidence! This is a message from the tutorial flag.");
  }

  @Test
  void testResolveTutorialFeatureFlagWithoutContext() throws Exception {
    provider.initialize(new ImmutableContext());

    final ImmutableContext context = new ImmutableContext();
    Value defaultValue = new Value(new ImmutableStructure(Map.of("test", new Value("best"))));
    ProviderEvaluation<Value> aDefault =
        provider.getObjectEvaluation("tutorial-feature", defaultValue, context);
    assertThat(aDefault.getValue().asStructure().asMap())
        .containsExactlyInAnyOrderEntriesOf(defaultValue.asStructure().asMap());
    assertThat(aDefault.getVariant()).isNull();
    assertThat(aDefault.getErrorCode()).isNull();
    assertThat(aDefault.getReason())
        .isEqualTo(
            "The server returned no assignment for the flag. Typically, this happens if no configured rules matches the given evaluation context.");
  }

  @Test
  void testResolveNonExistingFeatureFlag() throws Exception {
    provider.initialize(new ImmutableContext());

    final ImmutableContext context =
        new ImmutableContext(
            "tutorial_visitor", Map.of("visitor_id", new Value("tutorial_visitor")));

    assertThrows(
        FlagNotFoundError.class,
        () -> provider.getObjectEvaluation("non-existing-feature", new Value("default"), context));
  }

  @Test
  void testAuthServiceCalledDuringInit() throws Exception {
    provider.initialize(new ImmutableContext());

    assertThat(mockAuthService.getRequestCount()).isGreaterThan(0);
  }

  @Test
  void testResolverStateServiceCalled() throws Exception {
    provider.initialize(new ImmutableContext());

    assertThat(mockResolverStateService.getRequestCount()).isGreaterThan(0);
  }

  @Test
  void testShutdownSendsAllLogData() throws Exception {
    provider.initialize(new ImmutableContext());

    // Wait for initialization to complete
    assertEquals(ProviderState.READY, provider.getState());

    final ImmutableContext context =
        new ImmutableContext(
            "tutorial_visitor", Map.of("visitor_id", new Value("tutorial_visitor")));

    // Perform multiple flag resolutions across multiple threads to ensure all WASM instances
    // have log data
    final int numThreads = Runtime.getRuntime().availableProcessors();
    final int resolutionsPerThread = 5;
    final ExecutorService executor = Executors.newFixedThreadPool(numThreads);
    final CountDownLatch latch = new CountDownLatch(numThreads * resolutionsPerThread);

    for (int i = 0; i < numThreads; i++) {
      for (int j = 0; j < resolutionsPerThread; j++) {
        executor.submit(
            () -> {
              try {
                provider.getObjectEvaluation("tutorial-feature", new Value("default"), context);
              } catch (Exception e) {
                // Ignore resolution errors for this test
              } finally {
                latch.countDown();
              }
            });
      }
    }

    // Wait for all resolutions to complete
    assertTrue(latch.await(10, TimeUnit.SECONDS), "All resolutions should complete");
    executor.shutdown();

    // Record the number of log requests before shutdown
    final int logRequestsBeforeShutdown = mockFlagLoggerService.getRequestCount();

    // Shutdown the provider - this should flush all pending logs
    provider.shutdown();

    // Verify that log requests were made during shutdown
    // Note: The exact number depends on batching, but there should be at least some logs
    final int logRequestsAfterShutdown = mockFlagLoggerService.getRequestCount();
    assertThat(logRequestsAfterShutdown).isGreaterThanOrEqualTo(logRequestsBeforeShutdown);

    // Verify that all flag assignments were logged
    // We expect at least numThreads * resolutionsPerThread flag assignments
    final int totalFlagAssignments = mockFlagLoggerService.getTotalFlagAssignments();
    assertThat(totalFlagAssignments)
        .withFailMessage(
            "Expected at least %d flag assignments but got %d",
            numThreads * resolutionsPerThread, totalFlagAssignments)
        .isGreaterThanOrEqualTo(numThreads * resolutionsPerThread);
  }

  /** Mock AuthService that returns a JWT token with required claims */
  private static class MockAuthService extends AuthServiceGrpc.AuthServiceImplBase {
    private int requestCount = 0;

    @Override
    public void requestAccessToken(
        RequestAccessTokenRequest request, StreamObserver<AccessToken> responseObserver) {
      requestCount++;

      // Create a JWT token with account claim
      final Instant now = Instant.now();
      final String token =
          JWT.create()
              .withClaim(JwtUtils.ACCOUNT_NAME_CLAIM, ACCOUNT_NAME)
              .withExpiresAt(Date.from(now.plus(1, ChronoUnit.HOURS)))
              .sign(Algorithm.none());

      final AccessToken response =
          AccessToken.newBuilder().setAccessToken(token).setExpiresIn(3600).build();

      responseObserver.onNext(response);
      responseObserver.onCompleted();
    }

    public int getRequestCount() {
      return requestCount;
    }
  }

  /** Mock ResolverStateService that returns test flag configuration from resources */
  private static class MockResolverStateService
      extends ResolverStateServiceGrpc.ResolverStateServiceImplBase {
    private final int httpPort;
    private int requestCount = 0;

    public MockResolverStateService(int httpPort) {
      this.httpPort = httpPort;
    }

    @Override
    public void resolverStateUri(
        ResolverStateUriRequest request,
        StreamObserver<ResolverStateUriResponse> responseObserver) {
      requestCount++;

      try {
        final Instant expireTime = Instant.now().plus(1, ChronoUnit.HOURS);
        final ResolverStateUriResponse response =
            ResolverStateUriResponse.newBuilder()
                .setAccount(ACCOUNT_NAME)
                .setSignedUri("http://localhost:" + httpPort + "/resolver_state.pb")
                .setExpireTime(
                    Timestamp.newBuilder()
                        .setSeconds(expireTime.getEpochSecond())
                        .setNanos(expireTime.getNano())
                        .build())
                .build();

        responseObserver.onNext(response);
        responseObserver.onCompleted();
      } catch (Exception e) {
        responseObserver.onError(e);
      }
    }

    public int getRequestCount() {
      return requestCount;
    }
  }

  /** Mock FlagLoggerService that accepts flag logs and tracks received data */
  private static class MockFlagLoggerService
      extends InternalFlagLoggerServiceGrpc.InternalFlagLoggerServiceImplBase {
    private final AtomicInteger requestCount = new AtomicInteger(0);
    private final List<WriteFlagLogsRequest> receivedRequests = new CopyOnWriteArrayList<>();

    @Override
    public void writeFlagAssigned(
        WriteFlagAssignedRequest request,
        StreamObserver<WriteFlagAssignedResponse> responseObserver) {
      responseObserver.onNext(WriteFlagAssignedResponse.getDefaultInstance());
      responseObserver.onCompleted();
    }

    @Override
    public void writeFlagLogs(
        WriteFlagLogsRequest request, StreamObserver<WriteFlagLogsResponse> responseObserver) {
      requestCount.incrementAndGet();
      receivedRequests.add(request);
      responseObserver.onNext(WriteFlagLogsResponse.getDefaultInstance());
      responseObserver.onCompleted();
    }

    public int getRequestCount() {
      return requestCount.get();
    }

    public int getTotalFlagAssignments() {
      return receivedRequests.stream().mapToInt(WriteFlagLogsRequest::getFlagAssignedCount).sum();
    }
  }
}
