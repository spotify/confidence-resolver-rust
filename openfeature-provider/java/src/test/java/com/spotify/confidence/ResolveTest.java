package com.spotify.confidence;

import static org.assertj.core.api.AssertionsForClassTypes.assertThatExceptionOfType;
import static org.assertj.core.api.AssertionsForInterfaceTypes.assertThat;
import static org.junit.jupiter.api.Assertions.assertEquals;

import com.google.protobuf.Struct;
import com.google.protobuf.util.Structs;
import com.google.protobuf.util.Values;
import com.spotify.confidence.flags.admin.v1.Flag;
import com.spotify.confidence.flags.admin.v1.Segment;
import com.spotify.confidence.flags.resolver.v1.ResolveReason;
import com.spotify.confidence.flags.types.v1.FlagSchema;
import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.BeforeAll;
import org.junit.jupiter.api.Test;

abstract class ResolveTest extends TestBase {
  private static final String flag1 = "flags/flag-1";

  private static final String flagOff = flag1 + "/variants/offf";
  private static final String flagOn = flag1 + "/variants/onnn";

  private static final Flag.Variant variantOff =
      Flag.Variant.newBuilder()
          .setName(flagOff)
          .setValue(Structs.of("data", Values.of("off")))
          .build();
  private static final Flag.Variant variantOn =
      Flag.Variant.newBuilder()
          .setName(flagOn)
          .setValue(Structs.of("data", Values.of("on")))
          .build();

  private static final FlagSchema.StructFlagSchema schema1 =
      FlagSchema.StructFlagSchema.newBuilder()
          .putSchema(
              "data",
              FlagSchema.newBuilder()
                  .setStringSchema(FlagSchema.StringFlagSchema.newBuilder().build())
                  .build())
          .putSchema(
              "extra",
              FlagSchema.newBuilder()
                  .setStringSchema(FlagSchema.StringFlagSchema.newBuilder().build())
                  .build())
          .build();
  private static final String segmentA = "segments/seg-a";
  static final byte[] exampleStateBytes;
  static final byte[] exampleStateWithMaterializationBytes;
  private static final Map<String, Flag> flags =
      Map.of(
          flag1,
          Flag.newBuilder()
              .setName(flag1)
              .setState(Flag.State.ACTIVE)
              .setSchema(schema1)
              .addVariants(variantOff)
              .addVariants(variantOn)
              .addClients(clientName)
              .addRules(
                  Flag.Rule.newBuilder()
                      .setName("MyRule")
                      .setSegment(segmentA)
                      .setEnabled(true)
                      .setAssignmentSpec(
                          Flag.Rule.AssignmentSpec.newBuilder()
                              .setBucketCount(2)
                              .addAssignments(
                                  Flag.Rule.Assignment.newBuilder()
                                      .setAssignmentId(flagOff)
                                      .setVariant(
                                          Flag.Rule.Assignment.VariantAssignment.newBuilder()
                                              .setVariant(flagOff)
                                              .build())
                                      .addBucketRanges(
                                          Flag.Rule.BucketRange.newBuilder()
                                              .setLower(0)
                                              .setUpper(1)
                                              .build())
                                      .build())
                              .addAssignments(
                                  Flag.Rule.Assignment.newBuilder()
                                      .setAssignmentId(flagOn)
                                      .setVariant(
                                          Flag.Rule.Assignment.VariantAssignment.newBuilder()
                                              .setVariant(flagOn)
                                              .build())
                                      .addBucketRanges(
                                          Flag.Rule.BucketRange.newBuilder()
                                              .setLower(1)
                                              .setUpper(2)
                                              .build())
                                      .build())
                              .build())
                      .build())
              .build());

  private static final Map<String, Flag> flagsWithMaterialization =
      Map.of(
          flag1,
          Flag.newBuilder()
              .setName(flag1)
              .setState(Flag.State.ACTIVE)
              .setSchema(schema1)
              .addVariants(variantOff)
              .addVariants(variantOn)
              .addClients(clientName)
              .addRules(
                  Flag.Rule.newBuilder()
                      .setName("MyRule")
                      .setSegment(segmentA)
                      .setEnabled(true)
                      .setMaterializationSpec(
                          Flag.Rule.MaterializationSpec.newBuilder()
                              .setReadMaterialization("read-mat")
                              .setMode(
                                  Flag.Rule.MaterializationSpec.MaterializationReadMode.newBuilder()
                                      .setMaterializationMustMatch(true)
                                      .setSegmentTargetingCanBeIgnored(false)
                                      .build())
                              .setWriteMaterialization("write-mat")
                              .build())
                      .setAssignmentSpec(
                          Flag.Rule.AssignmentSpec.newBuilder()
                              .setBucketCount(2)
                              .addAssignments(
                                  Flag.Rule.Assignment.newBuilder()
                                      .setAssignmentId(flagOff)
                                      .setVariant(
                                          Flag.Rule.Assignment.VariantAssignment.newBuilder()
                                              .setVariant(flagOff)
                                              .build())
                                      .addBucketRanges(
                                          Flag.Rule.BucketRange.newBuilder()
                                              .setLower(0)
                                              .setUpper(1)
                                              .build())
                                      .build())
                              .addAssignments(
                                  Flag.Rule.Assignment.newBuilder()
                                      .setAssignmentId(flagOn)
                                      .setVariant(
                                          Flag.Rule.Assignment.VariantAssignment.newBuilder()
                                              .setVariant(flagOn)
                                              .build())
                                      .addBucketRanges(
                                          Flag.Rule.BucketRange.newBuilder()
                                              .setLower(1)
                                              .setUpper(2)
                                              .build())
                                      .build())
                              .build())
                      .build())
              .build());
  protected static final Map<String, Segment> segments =
      Map.of(segmentA, Segment.newBuilder().setName(segmentA).build());

  static {
    exampleStateBytes = buildResolverStateBytes(flags);
    exampleStateWithMaterializationBytes = buildResolverStateBytes(flagsWithMaterialization);
  }

  protected ResolveTest(boolean isWasm) {
    super(exampleStateBytes);
  }

  @BeforeAll
  public static void beforeAll() {
    TestBase.setup();
  }

  @Test
  public void testInvalidSecret() {
    assertThatExceptionOfType(RuntimeException.class)
        .isThrownBy(
            () ->
                resolveWithContext(
                    List.of("flags/asd"),
                    "foo",
                    Struct.newBuilder().build(),
                    true,
                    "invalid-secret"))
        .withMessage("client secret not found");
  }

  @Test
  public void testInvalidFlag() {
    final var response =
        resolveWithContext(List.of("flags/asd"), "foo", Struct.newBuilder().build(), false);
    assertThat(response.getResolvedFlagsList()).isEmpty();
    assertThat(response.getResolveId()).isNotEmpty();
  }

  @Test
  public void testResolveFlag() {
    final var response =
        resolveWithContext(List.of(flag1), "foo", Struct.newBuilder().build(), true);
    assertThat(response.getResolveId()).isNotEmpty();
    final Struct expectedValue = variantOn.getValue();

    assertEquals(variantOn.getName(), response.getResolvedFlags(0).getVariant());
    assertEquals(expectedValue, response.getResolvedFlags(0).getValue());
    assertEquals(schema1, response.getResolvedFlags(0).getFlagSchema());
  }

  @Test
  public void testResolveFlagWithEncryptedResolveToken() {
    final var response =
        resolveWithContext(List.of(flag1), "foo", Struct.newBuilder().build(), false);
    assertThat(response.getResolveId()).isNotEmpty();
    final Struct expectedValue = variantOn.getValue();

    assertEquals(variantOn.getName(), response.getResolvedFlags(0).getVariant());
    assertEquals(expectedValue, response.getResolvedFlags(0).getValue());
    assertEquals(schema1, response.getResolvedFlags(0).getFlagSchema());
    assertThat(response.getResolveToken()).isNotEmpty();
  }

  //  @Test
  //  public void perf() {
  //    final ScheduledExecutorService flagsFetcherExecutor =
  //            Executors.newScheduledThreadPool(1, new
  // ThreadFactoryBuilder().setDaemon(true).build());
  //
  //    flagsFetcherExecutor.scheduleAtFixedRate(
  //            () -> {
  //              System.out.println("flagsFetcherExecutor started");
  //              resolverServiceFactory.setState(desiredState.toProto().toByteArray());
  //              System.out.println("flagsFetcherExecutor ended");
  //            },
  //            2,
  //            2,
  //            TimeUnit.SECONDS);
  //
  //    for (int i = 1; i <= 100; i++) {
  //      final var start = System.currentTimeMillis();
  //      for (int j = 0; j < 10000; j++) {
  //        resolveWithContext(List.of(flag1), "foo", "bar", Struct.newBuilder().build(), true);
  //      }
  //      System.out.println(
  //          "Iteration " + i + " took " + (System.currentTimeMillis() - start) + " ms");
  //    }
  //  }

  @Test
  public void testTooLongKey() {
    assertThatExceptionOfType(RuntimeException.class)
        .isThrownBy(
            () ->
                resolveWithContext(
                    List.of(flag1), "a".repeat(101), Struct.newBuilder().build(), false))
        .withMessageContaining("Targeting key is too larger, max 100 characters.");
  }

  @Test
  public void testResolveIntegerTargetingKeyTyped() {
    final var response =
        resolveWithNumericTargetingKey(List.of(flag1), 1234567890, Struct.newBuilder().build());

    assertThat(response.getResolvedFlagsList()).hasSize(1);
    assertEquals(ResolveReason.RESOLVE_REASON_MATCH, response.getResolvedFlags(0).getReason());
  }

  @Test
  public void testResolveDecimalUsername() {
    final var response =
        resolveWithNumericTargetingKey(List.of(flag1), 3.14159d, Struct.newBuilder().build());

    assertThat(response.getResolvedFlagsList()).hasSize(1);
    assertEquals(
        ResolveReason.RESOLVE_REASON_TARGETING_KEY_ERROR, response.getResolvedFlags(0).getReason());
  }

  private static byte[] buildResolverStateBytes(Map<String, Flag> flagsMap) {
    final var builder = com.spotify.confidence.flags.admin.v1.ResolverState.newBuilder();
    builder.addAllFlags(flagsMap.values());
    builder.addAllSegmentsNoBitsets(segments.values());
    // All-one bitset for each segment
    segments
        .keySet()
        .forEach(
            name ->
                builder.addBitsets(
                    com.spotify.confidence.flags.admin.v1.ResolverState.PackedBitset.newBuilder()
                        .setSegment(name)
                        .setFullBitset(true)
                        .build()));
    builder.addClients(
        com.spotify.confidence.iam.v1.Client.newBuilder().setName(clientName).build());
    builder.addClientCredentials(
        com.spotify.confidence.iam.v1.ClientCredential.newBuilder()
            .setName(credentialName)
            .setClientSecret(TestBase.secret)
            .build());
    return builder.build().toByteArray();
  }
}
