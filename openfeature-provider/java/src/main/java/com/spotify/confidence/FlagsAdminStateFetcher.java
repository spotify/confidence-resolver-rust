package com.spotify.confidence;

import com.google.protobuf.Timestamp;
import com.spotify.confidence.flags.admin.v1.ResolverStateServiceGrpc;
import com.spotify.confidence.flags.admin.v1.ResolverStateUriRequest;
import com.spotify.confidence.flags.admin.v1.ResolverStateUriResponse;
import io.grpc.health.v1.HealthCheckResponse;

import java.io.IOException;
import java.io.InputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.time.Duration;
import java.time.Instant;
import java.util.concurrent.atomic.AtomicReference;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

/**
 * Fetches and caches account state from the Confidence resolver state service.
 *
 * <p>This implementation polls the resolver state service for flag configurations,
 * using ETags for conditional GETs to minimize bandwidth. The state is fetched
 * via signed URIs with automatic expiration and refresh handling.
 *
 * <p>Thread-safe implementation using atomic references for concurrent access.
 */
class FlagsAdminStateFetcher implements AccountStateProvider {

    private static final Logger logger = LoggerFactory.getLogger(FlagsAdminStateFetcher.class);

    private final ResolverStateServiceGrpc.ResolverStateServiceBlockingStub resolverStateService;
    // ETag for conditional GETs of resolver state
    private final AtomicReference<String> etagHolder = new AtomicReference<>();
    private final AtomicReference<byte[]> rawResolverStateHolder =
            new AtomicReference<>(
                    com.spotify.confidence.flags.admin.v1.ResolverState.newBuilder()
                            .build()
                            .toByteArray());
    private final AtomicReference<ResolverStateUriResponse> resolverStateUriResponse =
            new AtomicReference<>();
    private final AtomicReference<Instant> refreshTimeHolder = new AtomicReference<>();
    private String accountId = "";

    public FlagsAdminStateFetcher(
            ResolverStateServiceGrpc.ResolverStateServiceBlockingStub resolverStateService) {
        this.resolverStateService = resolverStateService;
    }

    @Override
    public byte[] provide() {
        return rawResolverStateHolder.get();
    }

    @Override
    public String accountId() {
        return accountId;
    }

    @Override
    public void reload() {
        try {
            fetchAndUpdateStateIfChanged();
        } catch (Exception e) {
            logger.warn("Failed to reload, ignoring reload", e);
        }
    }

    private ResolverStateUriResponse getResolverFileUri() {
        final Instant now = Instant.now();
        if (resolverStateUriResponse.get() == null
                || (refreshTimeHolder.get() == null || refreshTimeHolder.get().isBefore(now))) {
            resolverStateUriResponse.set(
                    resolverStateService.resolverStateUri(ResolverStateUriRequest.getDefaultInstance()));
            final var ttl =
                    Duration.between(now, toInstant(resolverStateUriResponse.get().getExpireTime()));
            refreshTimeHolder.set(now.plusMillis(ttl.toMillis() / 2));
        }
        return resolverStateUriResponse.get();
    }

    private Instant toInstant(Timestamp time) {
        return Instant.ofEpochSecond(time.getSeconds(), time.getNanos());
    }

    private void fetchAndUpdateStateIfChanged() {
        final var response = getResolverFileUri();
        this.accountId = response.getAccount();
        final var uri = response.getSignedUri();
        try {
            final HttpURLConnection conn = (HttpURLConnection) new URL(uri).openConnection();
            final String previousEtag = etagHolder.get();
            if (previousEtag != null) {
                conn.setRequestProperty("if-none-match", previousEtag);
            }
            if (conn.getResponseCode() == 304) {
                // Not modified
                return;
            }
            final String etag = conn.getHeaderField("etag");
            try (final InputStream stream = conn.getInputStream()) {
                final byte[] bytes = stream.readAllBytes();
                rawResolverStateHolder.set(bytes);
                etagHolder.set(etag);
            }
            logger.info("Loaded resolver state for {}, etag={}", accountId, etag);
        } catch (IOException e) {
            throw new RuntimeException(e);
        }
    }
}
