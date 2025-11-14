package confidence

import (
	"context"
	"log/slog"
	"sync"
	"time"

	resolverv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/flags/resolverinternal"
)

const (
	// MaxFlagAssignedPerChunk is the max number of flag_assigned entries per chunk
	// to avoid exceeding gRPC max message size
	MaxFlagAssignedPerChunk = 1000
)

// WasmFlagLogger is an interface for writing flag logs
type WasmFlagLogger interface {
	Write(ctx context.Context, request *resolverv1.WriteFlagLogsRequest) error
	Shutdown()
}

// FlagLogWriter is a function type for writing flag logs
type FlagLogWriter func(ctx context.Context, request *resolverv1.WriteFlagLogsRequest) error

// GrpcWasmFlagLogger implements WasmFlagLogger using gRPC
type GrpcWasmFlagLogger struct {
	stub   resolverv1.InternalFlagLoggerServiceClient
	writer FlagLogWriter
	logger *slog.Logger
	wg     sync.WaitGroup
}

// Compile-time interface conformance check
var _ WasmFlagLogger = (*GrpcWasmFlagLogger)(nil)

// NewGrpcWasmFlagLogger creates a new GrpcWasmFlagLogger
func NewGrpcWasmFlagLogger(stub resolverv1.InternalFlagLoggerServiceClient, logger *slog.Logger) *GrpcWasmFlagLogger {
	flagLogger := &GrpcWasmFlagLogger{
		stub:   stub,
		logger: logger,
	}

	// Set up the default writer that sends requests asynchronously
	flagLogger.writer = func(ctx context.Context, request *resolverv1.WriteFlagLogsRequest) error {
		flagLogger.wg.Add(1)
		go func() {
			defer flagLogger.wg.Done()
			// Create a context with timeout for the RPC
			rpcCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			if _, err := stub.WriteFlagLogs(rpcCtx, request); err != nil {
				logger.Error("Failed to write flag logs", "error", err)
			} else {
				logger.Info("Successfully sent flag log", "entries", len(request.FlagAssigned))
			}
		}()
		return nil
	}

	return flagLogger
}

// NewGrpcWasmFlagLoggerWithWriter creates a new GrpcWasmFlagLogger with a custom writer (for testing)
func NewGrpcWasmFlagLoggerWithWriter(stub resolverv1.InternalFlagLoggerServiceClient, writer FlagLogWriter, logger *slog.Logger) *GrpcWasmFlagLogger {
	return &GrpcWasmFlagLogger{
		stub:   stub,
		writer: writer,
		logger: logger,
	}
}

// Write writes flag logs, splitting into chunks if necessary
func (g *GrpcWasmFlagLogger) Write(ctx context.Context, request *resolverv1.WriteFlagLogsRequest) error {
	flagAssignedCount := len(request.FlagAssigned)
	clientResolveCount := len(request.ClientResolveInfo)
	flagResolveCount := len(request.FlagResolveInfo)

	if clientResolveCount == 0 && flagAssignedCount == 0 && flagResolveCount == 0 {
		g.logger.Debug("Skipping empty flag log request")
		return nil
	}

	// Log total counts
	g.logger.Debug("Writing flag logs",
		"flag_assigned", flagAssignedCount,
		"client_resolve_info", clientResolveCount,
		"flag_resolve_info", flagResolveCount)

	// If flag_assigned list is small enough, send everything as-is
	if flagAssignedCount <= MaxFlagAssignedPerChunk {
		return g.sendAsync(ctx, request)
	}

	// Split flag_assigned into chunks and send each chunk asynchronously
	g.logger.Debug("Splitting flag_assigned entries into chunks",
		"total_entries", flagAssignedCount,
		"chunk_size", MaxFlagAssignedPerChunk)

	chunks := g.createFlagAssignedChunks(request)
	for _, chunk := range chunks {
		if err := g.sendAsync(ctx, chunk); err != nil {
			g.logger.Error("Failed to send flag log chunk", "error", err)
			return err
		}
	}

	return nil
}

// createFlagAssignedChunks splits the WriteFlagLogsRequest into chunks
func (g *GrpcWasmFlagLogger) createFlagAssignedChunks(request *resolverv1.WriteFlagLogsRequest) []*resolverv1.WriteFlagLogsRequest {
	chunks := make([]*resolverv1.WriteFlagLogsRequest, 0)
	totalFlags := len(request.FlagAssigned)

	for i := 0; i < totalFlags; i += MaxFlagAssignedPerChunk {
		end := i + MaxFlagAssignedPerChunk
		if end > totalFlags {
			end = totalFlags
		}

		chunkBuilder := &resolverv1.WriteFlagLogsRequest{
			FlagAssigned: request.FlagAssigned[i:end],
		}

		// Include telemetry and resolve info only in the first chunk
		if i == 0 {
			if request.TelemetryData != nil {
				chunkBuilder.TelemetryData = request.TelemetryData
			}
			chunkBuilder.ClientResolveInfo = request.ClientResolveInfo
			chunkBuilder.FlagResolveInfo = request.FlagResolveInfo
		}

		chunks = append(chunks, chunkBuilder)
	}

	return chunks
}

// sendAsync sends the request asynchronously using the writer
func (g *GrpcWasmFlagLogger) sendAsync(ctx context.Context, request *resolverv1.WriteFlagLogsRequest) error {
	return g.writer(ctx, request)
}

// Shutdown waits for all pending async writes to complete
func (g *GrpcWasmFlagLogger) Shutdown() {
	g.wg.Wait()
}

// NoOpWasmFlagLogger is a flag logger that drops all requests (for disabled logging)
type NoOpWasmFlagLogger struct{}

// Compile-time interface conformance check
var _ WasmFlagLogger = (*NoOpWasmFlagLogger)(nil)

// NewNoOpWasmFlagLogger creates a new NoOpWasmFlagLogger
func NewNoOpWasmFlagLogger() *NoOpWasmFlagLogger {
	return &NoOpWasmFlagLogger{}
}

// Write drops the request without sending it
func (n *NoOpWasmFlagLogger) Write(ctx context.Context, request *resolverv1.WriteFlagLogsRequest) error {
	// Drop the request - do nothing
	return nil
}

// Shutdown does nothing
func (n *NoOpWasmFlagLogger) Shutdown() {
	// Nothing to shut down
}
