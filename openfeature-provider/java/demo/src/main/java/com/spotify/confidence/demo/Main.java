package com.spotify.confidence.demo;

import com.spotify.confidence.ApiSecret;
import com.spotify.confidence.OpenFeatureLocalResolveProvider;
import dev.openfeature.sdk.Client;
import dev.openfeature.sdk.EvaluationContext;
import dev.openfeature.sdk.ImmutableContext;
import dev.openfeature.sdk.MutableContext;
import dev.openfeature.sdk.OpenFeatureAPI;
import dev.openfeature.sdk.Value;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.util.HashMap;
import java.util.Map;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicLong;

public class Main {
    private static final Logger log = LoggerFactory.getLogger(Main.class);

    public static void main(String[] args) throws InterruptedException {
        String apiClientId = getEnvOrDefault("CONFIDENCE_API_CLIENT_ID", "API_ID");
        String apiClientSecret = getEnvOrDefault("CONFIDENCE_API_CLIENT_SECRET", "API_SECRET");
        String clientSecret = getEnvOrDefault("CONFIDENCE_CLIENT_SECRET", "CLIENT_SECRET");

        if (apiClientId.equals("API_ID") || apiClientSecret.equals("API_SECRET") || clientSecret.equals("CLIENT_SECRET")) {
            System.err.println("ERROR: Placeholder credentials detected. Please set environment variables:");
            System.err.println("  - CONFIDENCE_API_CLIENT_ID");
            System.err.println("  - CONFIDENCE_API_CLIENT_SECRET");
            System.err.println("  - CONFIDENCE_CLIENT_SECRET");
            System.exit(1);
        }

        log.info("Starting Confidence OpenFeature Local Provider Demo");

        try {
            ApiSecret apiSecret = new ApiSecret(apiClientId, apiClientSecret);
            OpenFeatureLocalResolveProvider provider = new OpenFeatureLocalResolveProvider(apiSecret, clientSecret);

            OpenFeatureAPI.getInstance().setProviderAndWait(provider);
            log.info("OpenFeature provider registered");

            Client client = OpenFeatureAPI.getInstance().getClient("demo-app");

            // Demo: Evaluate flags with multiple concurrent threads
            log.info("=== Flag Evaluation Demo with 10 Concurrent Threads ===");
            int threads = 10;
            long durationSeconds = 10;
            ExecutorService executor = Executors.newFixedThreadPool(threads);
            AtomicLong totalResolves = new AtomicLong(0);
            AtomicLong errorCount = new AtomicLong(0);

            long startTime = System.currentTimeMillis();
            long endTime = startTime + (durationSeconds * 1000);

            for (int i = 0; i < threads; i++) {
                executor.submit(() -> {
                    while (System.currentTimeMillis() < endTime) {
                        try {
                            // Simulate user context
                            Map<String, Value> attributes = new HashMap<>();
                            attributes.put("user_id", new Value("vahid"));
                            attributes.put("visitor_id", new Value("vahid"));
                            
                            EvaluationContext ctx = new ImmutableContext("user-123", attributes);
                            
                            // Flag key from Go demo
                            String flagKey = "mattias-boolean-flag"; 

                            // Just resolve, we primarily want to generate telemetry
                            Value value = client.getObjectValue(flagKey, new Value("default"), ctx);
                            
                            if (!value.isNull()) {
                                totalResolves.incrementAndGet();
                            } else {
                                errorCount.incrementAndGet();
                            }
                            
                            // Sleep briefly to not hammer it too hard in this loop
                            Thread.sleep(10);
                        } catch (Exception e) {
                            errorCount.incrementAndGet();
                            log.error("Evaluation error", e);
                        }
                    }
                });
            }

            executor.shutdown();
            executor.awaitTermination(durationSeconds + 5, TimeUnit.SECONDS);

            log.info("Demo completed:");
            log.info("Total resolves: {}", totalResolves.get());
            log.info("Errors: {}", errorCount.get());
            log.info("Approx RPS: {}", totalResolves.get() / durationSeconds);
            
            // Keep alive for a bit to allow async logs to flush
            log.info("Waiting 5 seconds for logs to flush...");
            Thread.sleep(5000);
            
            // Shutdown provider
            provider.shutdown();

        } catch (Exception e) {
            log.error("Fatal error", e);
            System.exit(1);
        }
    }

    private static String getEnvOrDefault(String key, String defaultValue) {
        String value = System.getenv(key);
        return (value != null && !value.isEmpty()) ? value : defaultValue;
    }
}

