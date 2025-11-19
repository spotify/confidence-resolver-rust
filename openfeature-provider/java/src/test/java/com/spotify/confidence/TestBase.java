package com.spotify.confidence;

import static org.mockito.Mockito.mock;

import com.google.protobuf.Struct;
import com.google.protobuf.util.Structs;
import com.google.protobuf.util.Values;
import com.spotify.confidence.flags.resolver.v1.ResolveFlagsRequest;
import com.spotify.confidence.flags.resolver.v1.ResolveFlagsResponse;
import com.spotify.confidence.flags.resolver.v1.WriteFlagLogsRequest;
import com.spotify.confidence.iam.v1.Client;
import com.spotify.confidence.iam.v1.ClientCredential;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ExecutionException;
import org.junit.jupiter.api.BeforeEach;

public class TestBase {
  protected final ResolverFallback mockFallback = mock(ResolverFallback.class);
  protected static final ClientCredential.ClientSecret secret =
      ClientCredential.ClientSecret.newBuilder().setSecret("very-secret").build();
  protected final byte[] desiredStateBytes;
  static final String account = "accounts/foo";
  static final String clientName = "clients/client";
  static final String credentialName = clientName + "/credentials/creddy";
  protected static final Map<ClientCredential.ClientSecret, AccountClient> secrets =
      Map.of(
          secret,
          new AccountClient(
              account,
              Client.newBuilder().setName(clientName).build(),
              ClientCredential.newBuilder()
                  .setName(credentialName)
                  .setClientSecret(secret)
                  .build()));
  private final ResolverApi resolverApi;

  protected TestBase(byte[] stateBytes) {
    this.desiredStateBytes = stateBytes;
    final var wasmResolverApi =
        new SwapWasmResolverApi(
            new WasmFlagLogger() {
              @Override
              public void write(WriteFlagLogsRequest request) {}

              @Override
              public void shutdown() {}
            },
            desiredStateBytes,
            "",
            mockFallback);
    this.resolverApi = wasmResolverApi;
  }

  protected static void setup() {}

  @BeforeEach
  protected void setUp() {}

  protected ResolveFlagsResponse resolveWithContext(
      List<String> flags, String username, Struct struct, boolean apply, String secret) {
      return resolverApi.resolve(
              ResolveFlagsRequest.newBuilder()
                  .addAllFlags(flags)
                  .setClientSecret(secret)
                  .setEvaluationContext(
                      Structs.of("targeting_key", Values.of(username), "bar", Values.of(struct)))
                  .setApply(apply)
                  .build());
  }

  protected ResolveFlagsResponse resolveWithNumericTargetingKey(
      List<String> flags, Number targetingKey, Struct struct) {

      final var builder =
          ResolveFlagsRequest.newBuilder()
              .addAllFlags(flags)
              .setClientSecret(secret.getSecret())
              .setApply(true);

      if (targetingKey instanceof Double || targetingKey instanceof Float) {
        builder.setEvaluationContext(
            Structs.of(
                "targeting_key", Values.of(targetingKey.doubleValue()), "bar", Values.of(struct)));
      } else {
        builder.setEvaluationContext(
            Structs.of(
                "targeting_key", Values.of(targetingKey.longValue()), "bar", Values.of(struct)));
      }

      return resolverApi.resolve(builder.build());
  }

  protected ResolveFlagsResponse resolveWithContext(
      List<String> flags, String username, Struct struct, boolean apply) {
    return resolveWithContext(flags, username, struct, apply, secret.getSecret());
  }
}
