package com.spotify.confidence;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.times;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import com.spotify.confidence.flags.admin.v1.ClientResolveInfo;
import com.spotify.confidence.flags.admin.v1.FlagResolveInfo;
import com.spotify.confidence.flags.resolver.v1.InternalFlagLoggerServiceGrpc;
import com.spotify.confidence.flags.resolver.v1.TelemetryData;
import com.spotify.confidence.flags.resolver.v1.WriteFlagLogsRequest;
import com.spotify.confidence.flags.resolver.v1.WriteFlagLogsResponse;
import com.spotify.confidence.flags.resolver.v1.events.ClientInfo;
import com.spotify.confidence.flags.resolver.v1.events.FlagAssigned;
import java.util.ArrayList;
import java.util.List;
import org.junit.jupiter.api.Test;
import org.mockito.ArgumentCaptor;

class GrpcWasmFlagLoggerTest {

  @Test
  void testEmptyRequest_shouldSkip() {
    // Given
    final var mockStub =
        mock(InternalFlagLoggerServiceGrpc.InternalFlagLoggerServiceBlockingStub.class);
    final var logger = createLoggerWithMockStub(mockStub);
    final var emptyRequest = WriteFlagLogsRequest.newBuilder().build();

    // When
    logger.write(emptyRequest);

    // Then
    verify(mockStub, never()).clientWriteFlagLogs(any());
    logger.shutdown();
  }

  @Test
  void testSmallRequest_shouldSendAsIs() {
    // Given
    final var mockStub =
        mock(InternalFlagLoggerServiceGrpc.InternalFlagLoggerServiceBlockingStub.class);
    when(mockStub.clientWriteFlagLogs(any()))
        .thenReturn(WriteFlagLogsResponse.getDefaultInstance());
    final var logger = createLoggerWithMockStub(mockStub);

    final var request =
        WriteFlagLogsRequest.newBuilder()
            .addAllFlagAssigned(createFlagAssignedList(100))
            .addClientResolveInfo(
                ClientResolveInfo.newBuilder().setClient("clients/test-client").build())
            .addFlagResolveInfo(FlagResolveInfo.newBuilder().setFlag("flags/test-flag").build())
            .build();

    final ArgumentCaptor<WriteFlagLogsRequest> captor =
        ArgumentCaptor.forClass(WriteFlagLogsRequest.class);

    // When
    logger.write(request);

    // Then
    verify(mockStub, times(1)).clientWriteFlagLogs(captor.capture());

    final WriteFlagLogsRequest sentRequest = captor.getValue();
    assertEquals(100, sentRequest.getFlagAssignedCount());
    assertEquals(1, sentRequest.getClientResolveInfoCount());
    assertEquals(1, sentRequest.getFlagResolveInfoCount());

    logger.shutdown();
  }

  @Test
  void testLargeRequest_shouldChunkWithMetadataInFirstChunkOnly() {
    // Given
    final var mockStub =
        mock(InternalFlagLoggerServiceGrpc.InternalFlagLoggerServiceBlockingStub.class);
    when(mockStub.clientWriteFlagLogs(any()))
        .thenReturn(WriteFlagLogsResponse.getDefaultInstance());
    final var logger = createLoggerWithMockStub(mockStub);

    final int totalFlags = 2500; // Will create 3 chunks: 1000, 1000, 500
    final var request =
        WriteFlagLogsRequest.newBuilder()
            .addAllFlagAssigned(createFlagAssignedList(totalFlags))
            .setTelemetryData(TelemetryData.newBuilder().build())
            .addClientResolveInfo(
                ClientResolveInfo.newBuilder().setClient("clients/test-client").build())
            .addFlagResolveInfo(FlagResolveInfo.newBuilder().setFlag("flags/test-flag").build())
            .build();

    final ArgumentCaptor<WriteFlagLogsRequest> captor =
        ArgumentCaptor.forClass(WriteFlagLogsRequest.class);

    // When
    logger.write(request);

    // Then
    verify(mockStub, times(3)).clientWriteFlagLogs(captor.capture());

    final List<WriteFlagLogsRequest> sentRequests = captor.getAllValues();
    assertEquals(3, sentRequests.size());

    // First chunk: 1000 flag_assigned + metadata
    final WriteFlagLogsRequest firstChunk = sentRequests.get(0);
    assertEquals(1000, firstChunk.getFlagAssignedCount());
    assertTrue(firstChunk.hasTelemetryData());
    assertEquals(1, firstChunk.getClientResolveInfoCount());
    assertEquals("clients/test-client", firstChunk.getClientResolveInfo(0).getClient());
    assertEquals(1, firstChunk.getFlagResolveInfoCount());
    assertEquals("flags/test-flag", firstChunk.getFlagResolveInfo(0).getFlag());

    // Second chunk: 1000 flag_assigned only, no metadata
    final WriteFlagLogsRequest secondChunk = sentRequests.get(1);
    assertEquals(1000, secondChunk.getFlagAssignedCount());
    assertEquals(false, secondChunk.hasTelemetryData());
    assertEquals(0, secondChunk.getClientResolveInfoCount());
    assertEquals(0, secondChunk.getFlagResolveInfoCount());

    // Third chunk: 500 flag_assigned only, no metadata
    final WriteFlagLogsRequest thirdChunk = sentRequests.get(2);
    assertEquals(500, thirdChunk.getFlagAssignedCount());
    assertEquals(false, thirdChunk.hasTelemetryData());
    assertEquals(0, thirdChunk.getClientResolveInfoCount());
    assertEquals(0, thirdChunk.getFlagResolveInfoCount());

    logger.shutdown();
  }

  @Test
  void testExactlyAtChunkBoundary_shouldCreateTwoChunks() {
    // Given
    final var mockStub =
        mock(InternalFlagLoggerServiceGrpc.InternalFlagLoggerServiceBlockingStub.class);
    when(mockStub.clientWriteFlagLogs(any()))
        .thenReturn(WriteFlagLogsResponse.getDefaultInstance());
    final var logger = createLoggerWithMockStub(mockStub);

    final int totalFlags = 2000; // Exactly 2 chunks of 1000
    final var request =
        WriteFlagLogsRequest.newBuilder()
            .addAllFlagAssigned(createFlagAssignedList(totalFlags))
            .setTelemetryData(TelemetryData.newBuilder().build())
            .build();

    final ArgumentCaptor<WriteFlagLogsRequest> captor =
        ArgumentCaptor.forClass(WriteFlagLogsRequest.class);

    // When
    logger.write(request);

    // Then
    verify(mockStub, times(2)).clientWriteFlagLogs(captor.capture());

    final List<WriteFlagLogsRequest> sentRequests = captor.getAllValues();
    assertEquals(2, sentRequests.size());

    // First chunk with metadata
    assertEquals(1000, sentRequests.get(0).getFlagAssignedCount());
    assertTrue(sentRequests.get(0).hasTelemetryData());

    // Second chunk without metadata
    assertEquals(1000, sentRequests.get(1).getFlagAssignedCount());
    assertEquals(false, sentRequests.get(1).hasTelemetryData());

    logger.shutdown();
  }

  @Test
  void testOnlyMetadata_noFlagAssigned_shouldSendAsIs() {
    // Given
    final var mockStub =
        mock(InternalFlagLoggerServiceGrpc.InternalFlagLoggerServiceBlockingStub.class);
    when(mockStub.clientWriteFlagLogs(any()))
        .thenReturn(WriteFlagLogsResponse.getDefaultInstance());
    final var logger = createLoggerWithMockStub(mockStub);

    final var request =
        WriteFlagLogsRequest.newBuilder()
            .setTelemetryData(TelemetryData.newBuilder().build())
            .addClientResolveInfo(
                ClientResolveInfo.newBuilder().setClient("clients/test-client").build())
            .build();

    final ArgumentCaptor<WriteFlagLogsRequest> captor =
        ArgumentCaptor.forClass(WriteFlagLogsRequest.class);

    // When
    logger.write(request);

    // Then
    verify(mockStub, times(1)).clientWriteFlagLogs(captor.capture());

    final WriteFlagLogsRequest sentRequest = captor.getValue();
    assertEquals(0, sentRequest.getFlagAssignedCount());
    assertTrue(sentRequest.hasTelemetryData());
    assertEquals(1, sentRequest.getClientResolveInfoCount());

    logger.shutdown();
  }

  // Helper methods

  private List<FlagAssigned> createFlagAssignedList(int count) {
    final List<FlagAssigned> list = new ArrayList<>();
    for (int i = 0; i < count; i++) {
      list.add(
          FlagAssigned.newBuilder()
              .setResolveId("resolve-" + i)
              .setClientInfo(
                  ClientInfo.newBuilder()
                      .setClient("clients/test-client")
                      .setClientCredential("clients/test-client/credentials/cred-1")
                      .build())
              .addFlags(
                  FlagAssigned.AppliedFlag.newBuilder()
                      .setFlag("flags/test-flag-" + i)
                      .setTargetingKey("user-" + i)
                      .setAssignmentId("assignment-" + i)
                      .build())
              .build());
    }
    return list;
  }

  private GrpcWasmFlagLogger createLoggerWithMockStub(
      InternalFlagLoggerServiceGrpc.InternalFlagLoggerServiceBlockingStub mockStub) {
    // Create logger with synchronous test writer
    return new GrpcWasmFlagLogger("test-client-secret", mockStub::clientWriteFlagLogs);
  }
}
