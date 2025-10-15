package com.spotify.confidence;

/**
 * Functional interface for providing AccountState instances.
 *
 * <p>The untyped nature of this interface allows high flexibility for testing, but it's not advised
 * to be used in production.
 *
 * <p>This can be useful if the provider implementer defines the AccountState proto schema in a
 * different Java package.
 */
@FunctionalInterface
public interface AccountStateProvider {

  /**
   * Provides an AccountState protobuf, from this proto specification: {@link
   * com.spotify.confidence.flags.admin.v1.AccountState}
   *
   * @return the AccountState protobuf containing flag configurations and metadata
   * @throws RuntimeException if the AccountState cannot be provided
   */
  byte[] provide();
}
