package com.spotify.confidence;

import static com.spotify.confidence.GrpcUtil.createConfidenceChannel;

import com.google.common.annotations.VisibleForTesting;
import com.spotify.confidence.flags.resolver.v1.InternalFlagLoggerServiceGrpc;
import com.spotify.confidence.flags.resolver.v1.WriteFlagLogsRequest;
import com.spotify.confidence.iam.v1.AuthServiceGrpc;
import io.grpc.Channel;
import io.grpc.ClientInterceptors;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

@FunctionalInterface
interface FlagLogWriter {
  void write(WriteFlagLogsRequest request);
}

public class GrpcWasmFlagLogger implements WasmFlagLogger {
  private static final Logger logger = LoggerFactory.getLogger(GrpcWasmFlagLogger.class);
  // Max number of flag_assigned entries per chunk to avoid exceeding gRPC max message size
  private static final int MAX_FLAG_ASSIGNED_PER_CHUNK = 1000;
  private final InternalFlagLoggerServiceGrpc.InternalFlagLoggerServiceBlockingStub stub;
  private final ExecutorService executorService;
  private final FlagLogWriter writer;

  @VisibleForTesting
  public GrpcWasmFlagLogger(ApiSecret apiSecret, FlagLogWriter writer) {
    this.stub = createAuthenticatedStub(apiSecret);
    this.executorService = Executors.newCachedThreadPool();
    this.writer = writer;
  }

  public GrpcWasmFlagLogger(ApiSecret apiSecret) {
    this.stub = createAuthenticatedStub(apiSecret);
    this.executorService = Executors.newCachedThreadPool();
    this.writer =
        request ->
            executorService.submit(
                () -> {
                  try {
                    stub.writeFlagLogs(request);
                    logger.debug(
                        "Successfully sent flag log with {} entries",
                        request.getFlagAssignedCount());
                  } catch (Exception e) {
                    logger.error("Failed to write flag logs", e);
                  }
                });
  }

  private static InternalFlagLoggerServiceGrpc.InternalFlagLoggerServiceBlockingStub
      createAuthenticatedStub(ApiSecret apiSecret) {
    final var channel = createConfidenceChannel();
    final AuthServiceGrpc.AuthServiceBlockingStub authService =
        AuthServiceGrpc.newBlockingStub(channel);
    final TokenHolder tokenHolder =
        new TokenHolder(apiSecret.clientId(), apiSecret.clientSecret(), authService);
    final Channel authenticatedChannel =
        ClientInterceptors.intercept(channel, new JwtAuthClientInterceptor(tokenHolder));
    return InternalFlagLoggerServiceGrpc.newBlockingStub(authenticatedChannel);
  }

  @Override
  public void write(WriteFlagLogsRequest request) {
    if (request.getClientResolveInfoList().isEmpty()
        && request.getFlagAssignedList().isEmpty()
        && request.getFlagResolveInfoList().isEmpty()) {
      logger.debug("Skipping empty flag log request");
      return;
    }

    if (request.hasTelemetryData()) {
      String sdkId = "nil";
      String sdkVersion = "nil";
      if (request.getTelemetryData().hasSdk()) {
        sdkId = request.getTelemetryData().getSdk().getId().toString();
        sdkVersion = request.getTelemetryData().getSdk().getVersion();
      }
      String logMsg = String.format(
          "Telemetry Data: dropped_events=%d, resolve_rps=%.2f, sdk_id=%s, sdk_version=%s",
          request.getTelemetryData().getDroppedEvents(),
          request.getTelemetryData().getResolveRps(),
          sdkId,
          sdkVersion);
      logger.debug(logMsg);
    }

    final int flagAssignedCount = request.getFlagAssignedCount();

    // If flag_assigned list is small enough, send everything as-is
    if (flagAssignedCount <= MAX_FLAG_ASSIGNED_PER_CHUNK) {
      sendAsync(request);
      return;
    }

    // Split flag_assigned into chunks and send each chunk asynchronously
    logger.debug(
        "Splitting {} flag_assigned entries into chunks of {}",
        flagAssignedCount,
        MAX_FLAG_ASSIGNED_PER_CHUNK);

    final List<WriteFlagLogsRequest> chunks = createFlagAssignedChunks(request);
    for (WriteFlagLogsRequest chunk : chunks) {
      sendAsync(chunk);
    }
  }

  private List<WriteFlagLogsRequest> createFlagAssignedChunks(WriteFlagLogsRequest request) {
    final List<WriteFlagLogsRequest> chunks = new ArrayList<>();
    final int totalFlags = request.getFlagAssignedCount();

    for (int i = 0; i < totalFlags; i += MAX_FLAG_ASSIGNED_PER_CHUNK) {
      final int end = Math.min(i + MAX_FLAG_ASSIGNED_PER_CHUNK, totalFlags);
      final WriteFlagLogsRequest.Builder chunkBuilder =
          WriteFlagLogsRequest.newBuilder()
              .addAllFlagAssigned(request.getFlagAssignedList().subList(i, end));

      // Include telemetry and resolve info only in the first chunk
      if (i == 0) {
        if (request.hasTelemetryData()) {
          chunkBuilder.setTelemetryData(request.getTelemetryData());
        }
        chunkBuilder
            .addAllClientResolveInfo(request.getClientResolveInfoList())
            .addAllFlagResolveInfo(request.getFlagResolveInfoList());
      }

      chunks.add(chunkBuilder.build());
    }

    return chunks;
  }

  private void sendAsync(WriteFlagLogsRequest request) {
    writer.write(request);
  }

  /**
   * Shutdown the executor service. This will allow any pending async writes to complete. Call this
   * when the application is shutting down.
   */
  @Override
  public void shutdown() {
    executorService.shutdown();
  }
}
