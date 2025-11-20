package com.spotify.confidence;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertTrue;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import com.google.protobuf.util.Structs;
import com.google.protobuf.util.Values;
import com.spotify.confidence.flags.resolver.v1.ResolveFlagsRequest;
import com.spotify.confidence.flags.resolver.v1.ResolveFlagsResponse;
import com.spotify.confidence.flags.resolver.v1.ResolveReason;
import com.spotify.confidence.flags.resolver.v1.ResolvedFlag;
import com.spotify.confidence.flags.types.v1.FlagSchema;
import dev.openfeature.sdk.ImmutableContext;
import dev.openfeature.sdk.ProviderEvaluation;
import dev.openfeature.sdk.Value;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.Test;

public class WasmResolveTest extends ResolveTest {
  // Use materialization-enabled state bytes for StickyResolveStrategy tests
  protected final byte[] desiredStateBytes = ResolveTest.exampleStateWithMaterializationBytes;

  public WasmResolveTest() {
    super(true);
  }

  @Test
  public void testAccountStateProviderInterface() {
    final OpenFeatureLocalResolveProvider provider =
        new OpenFeatureLocalResolveProvider(
            new TestAccountStateProvider(ResolveTest.exampleStateBytes, "account"),
            TestBase.secret.getSecret(),
            new ResolverFallback() {
              @Override
              public CompletableFuture<ResolveFlagsResponse> resolve(ResolveFlagsRequest request) {
                return CompletableFuture.completedFuture(null);
              }

              @Override
              public void close() {}
            });
    try {
      provider.initialize(new ImmutableContext());
    } catch (Exception e) {
      throw new RuntimeException(e);
    }
    final ProviderEvaluation<Value> objectEvaluation =
        provider.getObjectEvaluation("flag-1", new Value("error"), new ImmutableContext("user1"));

    assertEquals("flags/flag-1/variants/onnn", objectEvaluation.getVariant());
    assertEquals(ResolveReason.RESOLVE_REASON_MATCH.toString(), objectEvaluation.getReason());
    assertNull(objectEvaluation.getErrorCode());
    assertNull(objectEvaluation.getErrorMessage());
    assertTrue(objectEvaluation.getValue().isStructure());
    final var structure = objectEvaluation.getValue().asStructure();
    assertEquals("on", structure.getValue("data").asString());
  }

  @Test
  public void testResolverFallbackWhenMaterializationsMissing() {
    // Create a mock ResolverFallback
    // Create expected response from fallback
    final ResolveFlagsResponse expectedFallbackResponse =
        ResolveFlagsResponse.newBuilder()
            .addResolvedFlags(
                ResolvedFlag.newBuilder()
                    .setFlag("flags/flag-1")
                    .setFlagSchema(
                        FlagSchema.StructFlagSchema.newBuilder()
                            .putSchema(
                                "data",
                                FlagSchema.newBuilder()
                                    .setStringSchema(
                                        FlagSchema.StringFlagSchema.newBuilder().build())
                                    .build())
                            .build())
                    .setVariant("flags/flag-1/variants/onnn")
                    .setValue(Structs.of("data", Values.of("on")))
                    .setReason(ResolveReason.RESOLVE_REASON_MATCH)
                    .build())
            .setResolveId("fallback-resolve-id")
            .build();

    // Mock the fallback to return the expected response
    when(mockFallback.resolve(any(ResolveFlagsRequest.class)))
        .thenReturn(CompletableFuture.completedFuture(expectedFallbackResponse));

    // Create the provider with mocked fallback strategy
    final OpenFeatureLocalResolveProvider provider =
        new OpenFeatureLocalResolveProvider(
            new TestAccountStateProvider(ResolveTest.exampleStateBytes, "account"),
            secret.getSecret(),
            mockFallback);
    try {
      provider.initialize(new ImmutableContext());
    } catch (Exception e) {
      throw new RuntimeException(e);
    }
    // Make the resolve request using OpenFeature API
    final ProviderEvaluation<Value> evaluation =
        provider.getObjectEvaluation(
            "flag-1", new Value("default"), new ImmutableContext("test-user"));

    // Assert that we get the expected result
    assertEquals("flags/flag-1/variants/onnn", evaluation.getVariant());
    assertEquals(ResolveReason.RESOLVE_REASON_MATCH.toString(), evaluation.getReason());
    assertTrue(evaluation.getValue().isStructure());
    final var structure = evaluation.getValue().asStructure();
    assertEquals("on", structure.getValue("data").asString());
  }

  @Test
  public void testMaterializationRepositoryWhenMaterializationsMissing() {
    // Create a mock MaterializationRepository
    final MaterializationRepository mockRepository = mock(MaterializationRepository.class);

    // Create materialization info that the repository should return
    final MaterializationInfo materializationInfo =
        new MaterializationInfo(true, Map.of("MyRule", "flags/flag-1/variants/onnn"));

    final Map<String, MaterializationInfo> loadedAssignments =
        Map.of("read-mat", materializationInfo);

    // Mock the repository to return materialization info
    when(mockRepository.loadMaterializedAssignmentsForUnit(any(String.class), any()))
        .thenReturn(CompletableFuture.completedFuture(loadedAssignments));

    // Create the provider with mocked repository strategy
    final OpenFeatureLocalResolveProvider provider =
        new OpenFeatureLocalResolveProvider(
            new TestAccountStateProvider(desiredStateBytes, "account"),
            secret.getSecret(),
            mockRepository);
    try {
      provider.initialize(new ImmutableContext());
    } catch (Exception e) {
      throw new RuntimeException(e);
    }
    // Make the resolve request using OpenFeature API
    final ProviderEvaluation<Value> evaluation =
        provider.getObjectEvaluation(
            "flag-1", new Value("default"), new ImmutableContext("test-user"));

    // Assert that we get the expected result after materialization loading
    assertEquals("flags/flag-1/variants/onnn", evaluation.getVariant());
    assertEquals(ResolveReason.RESOLVE_REASON_MATCH.toString(), evaluation.getReason());
    assertTrue(evaluation.getValue().isStructure());
    final var structure = evaluation.getValue().asStructure();
    assertEquals("on", structure.getValue("data").asString());

    // Assert that the materialization repository was called with correct input
    verify(mockRepository).loadMaterializedAssignmentsForUnit("test-user", "read-mat");
  }

  // add @Test and run it locally for load testing
  public void testConcurrentResolveLoadTest() throws InterruptedException {
    // Test configuration
    final int totalResolves = 1000_000;
    final int numThreads = 10;
    final int resolvesPerThread = totalResolves / numThreads;

    // Create the provider using normal exampleState (not with materialization)
    final OpenFeatureLocalResolveProvider provider =
        new OpenFeatureLocalResolveProvider(
            new TestAccountStateProvider(ResolveTest.exampleStateBytes, "account"),
            TestBase.secret.getSecret(),
            new ResolverFallback() {
              @Override
              public CompletableFuture<ResolveFlagsResponse> resolve(ResolveFlagsRequest request) {
                return CompletableFuture.completedFuture(null);
              }

              @Override
              public void close() {}
            });

    // Thread pool for executing concurrent resolves
    final ExecutorService executorService = Executors.newFixedThreadPool(numThreads);
    final CountDownLatch startLatch = new CountDownLatch(1);
    final CountDownLatch completionLatch = new CountDownLatch(numThreads);

    // Track success and errors
    final AtomicInteger successCount = new AtomicInteger(0);
    final AtomicInteger errorCount = new AtomicInteger(0);
    final List<Exception> exceptions = new ArrayList<>();

    // Submit tasks to thread pool
    for (int i = 0; i < numThreads; i++) {
      final int threadId = i;
      executorService.submit(
          () -> {
            try {
              // Wait for all threads to be ready
              startLatch.await();

              // Perform resolves
              for (int j = 0; j < resolvesPerThread; j++) {
                try {
                  final ProviderEvaluation<Value> evaluation =
                      provider.getObjectEvaluation(
                          "flag-1",
                          new Value("default"),
                          new ImmutableContext("user-" + threadId + "-" + j));

                  // Verify the resolve was successful (accept either variant)
                  final String variant = evaluation.getVariant();
                  if (variant != null
                      && (variant.equals("flags/flag-1/variants/onnn")
                          || variant.equals("flags/flag-1/variants/offf"))) {
                    successCount.incrementAndGet();
                  } else {
                    errorCount.incrementAndGet();
                  }
                } catch (Exception e) {
                  errorCount.incrementAndGet();
                  synchronized (exceptions) {
                    exceptions.add(e);
                  }
                }
              }
            } catch (InterruptedException e) {
              Thread.currentThread().interrupt();
            } finally {
              completionLatch.countDown();
            }
          });
    }

    // Start all threads at once
    final long startTime = System.currentTimeMillis();
    startLatch.countDown();

    // Wait for all threads to complete (with timeout)
    final boolean completed = completionLatch.await(300, TimeUnit.SECONDS);
    final long endTime = System.currentTimeMillis();
    final long durationMs = endTime - startTime;

    // Shutdown executor
    executorService.shutdown();
    executorService.awaitTermination(5, TimeUnit.SECONDS);

    // Print test results
    System.out.println("=== Load Test Results ===");
    System.out.println("Total resolves: " + totalResolves);
    System.out.println("Threads: " + numThreads);
    System.out.println("Duration: " + durationMs + "ms");
    System.out.println(
        "Throughput: " + String.format("%.2f", (totalResolves * 1000.0 / durationMs)) + " req/s");
    System.out.println("Successful: " + successCount.get());
    System.out.println("Errors: " + errorCount.get());

    // Assert test success
    assertTrue(completed, "Load test did not complete within timeout");
    assertEquals(totalResolves, successCount.get(), "Not all resolves succeeded");
    assertEquals(0, errorCount.get(), "Unexpected errors occurred");
    assertTrue(exceptions.isEmpty(), "Exceptions occurred during load test: " + exceptions);
  }
}
