package com.spotify.confidence;

import com.spotify.confidence.flags.resolver.v1.ResolveFlagsRequest;
import com.spotify.confidence.flags.resolver.v1.ResolveFlagsResponse;
import io.grpc.ClientInterceptor;
import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.atomic.AtomicInteger;

import org.junit.jupiter.api.Test;

import static org.junit.jupiter.api.Assertions.*;

/**
 * Tests to verify that the ChannelFactory pattern works correctly.
 */
public class ChannelFactoryTest {

    private static final ApiSecret apiSecret = new ApiSecret("test-client-id", "test-client-secret");
    private final ResolverFallback noOpResolverFallback = new ResolverFallback() {
        @Override
        public CompletableFuture<
                ResolveFlagsResponse>
        resolve(ResolveFlagsRequest request) {
            return CompletableFuture.completedFuture(null);
        }

        @Override
        public void close() {
        }
    };

    @Test
    public void verifyCustomChannelFactoryIsCalledByProvider() {
        final AtomicInteger factoryCallCount = new AtomicInteger(0);
        final List<String> targetsReceived = new ArrayList<>();
        final List<Integer> interceptorCounts = new ArrayList<>();

        final ChannelFactory customFactory = new ChannelFactory() {
            @Override
            public ManagedChannel create(String target, List<ClientInterceptor> interceptors) {
                factoryCallCount.incrementAndGet();
                targetsReceived.add(target);
                interceptorCounts.add(interceptors.size());
                ManagedChannelBuilder<?> builder = ManagedChannelBuilder.forTarget("localhost")
                        .usePlaintext();

                if (!interceptors.isEmpty()) {
                    builder.intercept(interceptors.toArray(new ClientInterceptor[0]));
                }
                return builder.build();
            }

            @Override
            public void shutdown() {
                // Test implementation - no-op
            }
        };


        // Call the constructor that uses ChannelFactory
        new OpenFeatureLocalResolveProvider(new LocalProviderConfig(apiSecret, customFactory), "clientsecret", noOpResolverFallback);

        // called by tokenservice, state service and flag logger
        assertEquals(3, factoryCallCount.get(), "ChannelFactory should have been called twice, but was called "
                + factoryCallCount.get()
                + " times");

        assertFalse(targetsReceived.isEmpty(), "Factory should have received target addresses");

        // Verify it received the confidence domain as target
        assertTrue(
                targetsReceived.get(0).contains("grpc") || targetsReceived.get(0).contains("edge"),
                "Target should be a gRPC endpoint, got: " + targetsReceived.get(0));
        assertEquals(3, interceptorCounts.size(), "Interceptors should have been called");
    }

    @Test
    public void verifyDefaultChannelFactoryIsUsedWhenNoneProvided() {
        final LocalProviderConfig config = new LocalProviderConfig(apiSecret);
        assertInstanceOf(DefaultChannelFactory.class, config.getChannelFactory(), "LocalProviderConfig should use DefaultChannelFactory when none is provided");
    }
}
