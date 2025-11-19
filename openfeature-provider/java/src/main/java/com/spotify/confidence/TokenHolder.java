package com.spotify.confidence;

import static com.spotify.confidence.JwtUtils.ACCOUNT_NAME_CLAIM;
import static com.spotify.confidence.JwtUtils.getClaimOrThrow;
import static java.time.Instant.now;

import com.auth0.jwt.JWT;
import com.auth0.jwt.interfaces.DecodedJWT;
import com.github.benmanes.caffeine.cache.Caffeine;
import com.github.benmanes.caffeine.cache.Expiry;
import com.github.benmanes.caffeine.cache.LoadingCache;
import com.spotify.confidence.iam.v1.AuthServiceGrpc;
import com.spotify.confidence.iam.v1.RequestAccessTokenRequest;
import java.time.Duration;
import java.time.Instant;

class TokenHolder {

  private final String apiClientId;
  private final String apiClientSecret;

  private LoadingCache<CacheKey, Token> tokenCache;
  private final AuthServiceGrpc.AuthServiceBlockingStub stub;

  public TokenHolder(
      String apiClientId, String apiClientSecret, AuthServiceGrpc.AuthServiceBlockingStub stub) {
    this.apiClientId = apiClientId;
    this.apiClientSecret = apiClientSecret;
    this.stub = stub;
  }

  public Token getToken() {
      if (tokenCache == null) {
          this.tokenCache =
                  Caffeine.newBuilder()
                          .expireAfter(
                                  new Expiry<CacheKey, Token>() {
                                      @Override
                                      public long expireAfterCreate(
                                              CacheKey cacheKey, Token authToken, long currentTime) {
                                          return getExpiryDuration(authToken);
                                      }

                                      @Override
                                      public long expireAfterUpdate(
                                              CacheKey cacheKey, Token authToken, long currentTime, long currentDuration) {
                                          return getExpiryDuration(authToken);
                                      }

                                      @Override
                                      public long expireAfterRead(
                                              CacheKey cacheKey, Token authToken, long currentTime, long currentDuration) {
                                          return currentDuration;
                                      }

                                      private long getExpiryDuration(Token authToken) {
                                          return Duration.between(now(), authToken.expiration())
                                                  .minusHours(1L) // Subtract an hour to have some margin
                                                  .toNanos();
                                      }
                                  })
                          .build(this::requestAccessToken);
      }
      return tokenCache.get(new CacheKey());
  }

  private Token requestAccessToken(CacheKey cacheKey) {
    final var response =
        stub.requestAccessToken(
            RequestAccessTokenRequest.newBuilder()
                .setClientId(apiClientId)
                .setClientSecret(apiClientSecret)
                .setGrantType("client_credentials")
                .build());

    final String accessToken = response.getAccessToken();
    final DecodedJWT decodedJWT = JWT.decode(accessToken);

    return new Token(
        accessToken,
        getClaimOrThrow(decodedJWT, ACCOUNT_NAME_CLAIM).asString(),
        now().plusSeconds(response.getExpiresIn()));
  }

  public record Token(String token, String account, Instant expiration) {}

  private record CacheKey() {}
}
