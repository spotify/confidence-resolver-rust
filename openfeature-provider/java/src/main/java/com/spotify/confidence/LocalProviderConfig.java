package com.spotify.confidence;

public class LocalProviderConfig {
  private final ChannelFactory channelFactory;

  public LocalProviderConfig() {
    this(null);
  }

  public LocalProviderConfig(ChannelFactory channelFactory) {
    this.channelFactory = channelFactory != null ? channelFactory : new DefaultChannelFactory();
  }

  public ChannelFactory getChannelFactory() {
    return channelFactory;
  }
}
