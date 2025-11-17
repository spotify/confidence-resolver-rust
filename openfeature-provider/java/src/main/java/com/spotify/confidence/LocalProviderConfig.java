package com.spotify.confidence;

/**
 * Configuration for the local resolve OpenFeature provider.
 *
 * <p>This class combines API credentials with optional advanced configuration like custom
 * channel factories for gRPC connection creation.
 *
 * <p>Example usage with default channel factory:
 * <pre>{@code
 * ApiSecret apiSecret = new ApiSecret("client-id", "client-secret");
 * LocalProviderConfig config = new LocalProviderConfig(apiSecret);
 * OpenFeatureLocalResolveProvider provider = new OpenFeatureLocalResolveProvider(config, "client-secret");
 * }</pre>
 *
 * <p>Example usage with custom channel factory for testing:
 * <pre>{@code
 * ChannelFactory mockFactory = (target, interceptors) ->
 *     InProcessChannelBuilder.forName("test-server")
 *         .usePlaintext()
 *         .intercept(interceptors.toArray(new ClientInterceptor[0]))
 *         .build();
 *
 * ApiSecret apiSecret = new ApiSecret("client-id", "client-secret");
 * LocalProviderConfig config = new LocalProviderConfig(apiSecret, mockFactory);
 * OpenFeatureLocalResolveProvider provider = new OpenFeatureLocalResolveProvider(config, "client-secret");
 * }</pre>
 */
public class LocalProviderConfig {
  private final ApiSecret apiSecret;
  private final ChannelFactory channelFactory;

  /**
   * Creates a configuration with the default channel factory.
   *
   * @param apiSecret the API credentials for Confidence service authentication
   */
  public LocalProviderConfig(ApiSecret apiSecret) {
    this(apiSecret, null);
  }

  /**
   * Creates a configuration with a custom channel factory.
   *
   * @param apiSecret the API credentials for Confidence service authentication
   * @param channelFactory optional custom factory for creating gRPC channels; if null, uses
   *     {@link DefaultChannelFactory}
   */
  public LocalProviderConfig(ApiSecret apiSecret, ChannelFactory channelFactory) {
    this.apiSecret = apiSecret;
    this.channelFactory = channelFactory != null ? channelFactory : new DefaultChannelFactory();
  }

  /**
   * Returns the API credentials.
   *
   * @return the API secret containing client ID and client secret
   */
  public ApiSecret getApiSecret() {
    return apiSecret;
  }

  /**
   * Returns the channel factory for creating gRPC channels.
   *
   * @return the channel factory (never null; defaults to {@link DefaultChannelFactory})
   */
  public ChannelFactory getChannelFactory() {
    return channelFactory;
  }
}
