package com.spotify.confidence;

import com.spotify.confidence.iam.v1.ClientCredential;

interface ResolverServiceFactory {
  FlagResolverService create(ClientCredential.ClientSecret clientSecret);

  default FlagResolverService create(String clientSecret) {
    return create(ClientCredential.ClientSecret.newBuilder().setSecret(clientSecret).build());
  }
}
