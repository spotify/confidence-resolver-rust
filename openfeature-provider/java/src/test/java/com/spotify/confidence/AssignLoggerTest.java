package com.spotify.confidence;

import static org.assertj.core.api.AssertionsForClassTypes.assertThatExceptionOfType;
import static org.assertj.core.api.AssertionsForInterfaceTypes.assertThat;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import com.codahale.metrics.MetricRegistry;
import com.spotify.confidence.flags.resolver.v1.InternalFlagLoggerServiceGrpc;
import com.spotify.confidence.flags.resolver.v1.ResolveTokenV1;
import com.spotify.confidence.flags.resolver.v1.Sdk;
import com.spotify.confidence.flags.resolver.v1.WriteFlagAssignedRequest;
import com.spotify.confidence.flags.resolver.v1.WriteFlagAssignedResponse;
import com.spotify.confidence.flags.resolver.v1.events.FlagAssigned;
import com.spotify.confidence.iam.v1.Client;
import com.spotify.confidence.iam.v1.ClientCredential;
import java.time.Instant;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.atomic.AtomicInteger;
import org.apache.commons.lang3.RandomStringUtils;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

public class AssignLoggerTest {
  private InternalFlagLoggerServiceGrpc.InternalFlagLoggerServiceBlockingStub stub;
  private AssignLogger logger;

  @BeforeEach
  public void beforeEach() {
    stub = mock(InternalFlagLoggerServiceGrpc.InternalFlagLoggerServiceBlockingStub.class);
    logger = new AssignLogger(stub, null, new MetricRegistry(), 8 * 1024 * 1024);
  }

  @Test
  public void canSendAssignsThatNeedMultipleRequests() {
    when(stub.writeFlagAssigned(any())).thenReturn(WriteFlagAssignedResponse.getDefaultInstance());
    addManyAssigns();
    logger.checkpoint();
    assertThat(logger.queuedAssigns()).isEmpty();
    verify(stub, times(2)).writeFlagAssigned(any());
  }

  @Test
  public void ifFirstWriteFailsThenAllMessagesAreKept() {
    addManyAssigns();
    final var stateBefore = logger.queuedAssigns();
    assertThat(stateBefore).hasSize(10000);
    when(stub.writeFlagAssigned(any())).thenThrow(new RuntimeException("Throw up"));

    assertThatExceptionOfType(RuntimeException.class)
        .isThrownBy(() -> logger.checkpoint())
        .withMessage("Throw up");

    final var stateAfter = logger.queuedAssigns();
    assertThat(stateAfter).containsExactlyInAnyOrderElementsOf(stateBefore);
  }

  @Test
  public void ifFirstWriteWorksButSecondFailsThenTheNonSentMessagesAreKept() {
    addManyAssigns();
    final var stateBefore = logger.queuedAssigns();
    assertThat(stateBefore).hasSize(10000);
    final AtomicInteger remaining = new AtomicInteger(10000);
    final List<FlagAssigned> sent = new ArrayList<>();
    when(stub.writeFlagAssigned(any()))
        .then(
            invocation -> {
              final WriteFlagAssignedRequest req =
                  invocation.getArgument(0, WriteFlagAssignedRequest.class);
              remaining.getAndUpdate(i -> i - req.getFlagAssignedCount());
              sent.addAll(req.getFlagAssignedList());
              return WriteFlagAssignedResponse.getDefaultInstance();
            })
        .thenThrow(new RuntimeException("Throw up"));

    assertThatExceptionOfType(RuntimeException.class)
        .isThrownBy(() -> logger.checkpoint())
        .withMessage("Throw up");

    final var stateAfter = logger.queuedAssigns();
    assertThat(remaining.get()).isGreaterThan(0);
    assertThat(remaining.get()).isLessThan(10000);

    assertThat(stateAfter).hasSize(remaining.get());
    assertThat(stateAfter).doesNotContainAnyElementsOf(sent);
  }

  @Test
  void reducesCapacityBySizeOfAssigned() {
    final String resolveId = RandomStringUtils.randomAlphabetic(100);
    final Sdk sdk = Sdk.getDefaultInstance();
    final List<FlagToApply> flagsToApply =
        List.of(
            new FlagToApply(
                Instant.now(),
                ResolveTokenV1.AssignedFlag.newBuilder()
                    .setAssignmentId(RandomStringUtils.randomAlphabetic(100))
                    .setFlag(RandomStringUtils.randomAlphabetic(100))
                    .build()));
    final AccountClient accountClient =
        new AccountClient(
            "foobar", Client.getDefaultInstance(), ClientCredential.getDefaultInstance());
    final int size =
        FlagLogger.createFlagAssigned(resolveId, sdk, flagsToApply, accountClient)
            .getSerializedSize();
    final long capacityBefore = logger.remainingCapacity();
    logger.logAssigns(resolveId, sdk, flagsToApply, accountClient);
    assertThat(logger.remainingCapacity()).isEqualTo(capacityBefore - size);
  }

  @Test
  void increasesCapacityOnSuccessfulSend() {
    when(stub.writeFlagAssigned(any())).thenReturn(WriteFlagAssignedResponse.getDefaultInstance());
    final long capacityBefore = logger.remainingCapacity();
    addManyAssigns();
    assertThat(logger.remainingCapacity()).isLessThan(capacityBefore);
    logger.checkpoint();
    assertThat(logger.remainingCapacity()).isEqualTo(capacityBefore);
  }

  @Test
  void capacityUnchangedOnFailedSend() {
    when(stub.writeFlagAssigned(any())).thenThrow(new RuntimeException("Throw up"));
    addManyAssigns();
    final long capacityAfter = logger.remainingCapacity();

    assertThatExceptionOfType(RuntimeException.class).isThrownBy(logger::checkpoint);
    assertThat(logger.remainingCapacity()).isEqualTo(capacityAfter);
  }

  private void addManyAssigns() {
    // this should be large enough so we have to split in 2 requests
    for (int i = 0; i < 10000; i++) {
      logger.logAssigns(
          RandomStringUtils.randomAlphabetic(100),
          Sdk.getDefaultInstance(),
          List.of(
              new FlagToApply(
                  Instant.now(),
                  ResolveTokenV1.AssignedFlag.newBuilder()
                      .setAssignmentId(RandomStringUtils.randomAlphabetic(100))
                      .setFlag(RandomStringUtils.randomAlphabetic(100))
                      .build())),
          new AccountClient(
              "foobar", Client.getDefaultInstance(), ClientCredential.getDefaultInstance()));
    }
  }
}
