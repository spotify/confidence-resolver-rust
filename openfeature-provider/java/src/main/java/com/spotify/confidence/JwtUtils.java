package com.spotify.confidence;

import static io.grpc.Metadata.ASCII_STRING_MARSHALLER;

import com.auth0.jwt.exceptions.JWTVerificationException;
import com.auth0.jwt.interfaces.Claim;
import com.auth0.jwt.interfaces.DecodedJWT;
import io.grpc.Metadata;
import java.util.Optional;

class JwtUtils {

  public static final Metadata.Key<String> AUTHORIZATION_METADATA_KEY =
      Metadata.Key.of("Authorization", ASCII_STRING_MARSHALLER);

  public static final String ACCOUNT_NAME_CLAIM = "https://confidence.dev/account_name";

  public static Claim getClaimOrThrow(final DecodedJWT jwt, final String claim)
      throws JWTVerificationException {

    return getClaim(jwt, claim)
        .orElseThrow(
            () ->
                new JWTVerificationException(
                    String.format("Missing required claim '%s' in JWT.", claim)));
  }

  public static Optional<Claim> getClaim(final DecodedJWT jwt, final String claim)
      throws JWTVerificationException {
    if (!jwt.getClaims().containsKey(claim)) {
      return Optional.empty();
    } else {
      return Optional.of(jwt.getClaim(claim));
    }
  }

  public static String getTokenAsHeader(String rawToken) {
    return String.format("Bearer %s", rawToken);
  }
}
