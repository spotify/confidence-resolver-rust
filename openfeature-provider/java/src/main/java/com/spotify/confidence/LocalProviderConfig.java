package com.spotify.confidence;

public class LocalProviderConfig {
  private final ChannelFactory channelFactory;
  private final HttpClientFactory httpClientFactory;

  public LocalProviderConfig() {
    this(null, null);
  }

  public LocalProviderConfig(ChannelFactory channelFactory) {
    this(channelFactory, null);
  }

  public LocalProviderConfig(ChannelFactory channelFactory, HttpClientFactory httpClientFactory) {
    this.channelFactory = channelFactory != null ? channelFactory : new DefaultChannelFactory();
    this.httpClientFactory =
        httpClientFactory != null ? httpClientFactory : new DefaultHttpClientFactory();
  }

  public ChannelFactory getChannelFactory() {
    return channelFactory;
  }

  public HttpClientFactory getHttpClientFactory() {
    return httpClientFactory;
  }
}
