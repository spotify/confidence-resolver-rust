package com.spotify.confidence;

/**
 * API credentials for authenticating with the Confidence service.
 *
 * <p>This record holds the client ID and client secret used to authenticate with the Confidence API
 * for administrative operations like fetching flag configurations and logging exposure events.
 *
 * @param clientId the client ID for your Confidence application
 * @param clientSecret the client secret for your Confidence application
 * @since 0.2.4
 */
public record ApiSecret(String clientId, String clientSecret) {}
