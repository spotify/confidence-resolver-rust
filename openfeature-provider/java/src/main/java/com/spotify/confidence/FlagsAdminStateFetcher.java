package com.spotify.confidence;

import com.google.protobuf.InvalidProtocolBufferException;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.io.IOException;
import java.io.InputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.util.concurrent.atomic.AtomicReference;

/**
 * Fetches and caches account state from the Confidence CDN.
 *
 * <p>This implementation fetches state directly from the CDN using the client secret,
 * using ETags for conditional GETs to minimize bandwidth.
 *
 * <p>Thread-safe implementation using atomic references for concurrent access.
 */
class FlagsAdminStateFetcher implements AccountStateProvider {

    private static final Logger logger = LoggerFactory.getLogger(FlagsAdminStateFetcher.class);
    private static final String CDN_BASE_URL = "https://confidence-resolver-state-cdn.spotifycdn.com/";

    private final String clientSecret;
    // ETag for conditional GETs of resolver state
    private final AtomicReference<String> etagHolder = new AtomicReference<>();
    private final AtomicReference<byte[]> rawResolverStateHolder =
            new AtomicReference<>(
                    com.spotify.confidence.flags.admin.v1.ResolverState.newBuilder()
                            .build()
                            .toByteArray());
    private String accountId = "";

    public FlagsAdminStateFetcher(String clientSecret) {
        this.clientSecret = clientSecret;
    }

    public AtomicReference<byte[]> rawStateHolder() {
        return rawResolverStateHolder;
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

    private void fetchAndUpdateStateIfChanged() {
        // Build CDN URL directly from client secret
        final var cdnUrl = CDN_BASE_URL + clientSecret;
        try {
            final HttpURLConnection conn = (HttpURLConnection) new URL(cdnUrl).openConnection();
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

                // Parse ClientResolverState from CDN response
                final var clientState = com.spotify.confidence.flags.admin.v1.ClientResolverState.parseFrom(bytes);
                this.accountId = clientState.getAccount();

                // Store the nested ResolverState
                rawResolverStateHolder.set(clientState.getState().toByteArray());
                etagHolder.set(etag);
            }
            logger.info("Loaded resolver state for account={}, etag={}", accountId, etag);
        } catch (IOException e) {
            throw new RuntimeException(e);
        }
    }
}
