package confidence

import (
	"context"
	"log"
	"sync"
	"time"

	resolverv1 "github.com/spotify/confidence-resolver-rust/openfeature-provider/go/confidence/proto/confidence/flags/resolverinternal"
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
	wg     sync.WaitGroup
}

// NewGrpcWasmFlagLogger creates a new GrpcWasmFlagLogger
func NewGrpcWasmFlagLogger(stub resolverv1.InternalFlagLoggerServiceClient) *GrpcWasmFlagLogger {
	logger := &GrpcWasmFlagLogger{
		stub: stub,
	}

	// Set up the default writer that sends requests asynchronously
	logger.writer = func(ctx context.Context, request *resolverv1.WriteFlagLogsRequest) error {
		logger.wg.Add(1)
		go func() {
			defer logger.wg.Done()
			// Create a context with timeout for the RPC
			rpcCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			if _, err := stub.WriteFlagLogs(rpcCtx, request); err != nil {
				log.Printf("Failed to write flag logs: %v", err)
			} else {
				log.Printf("Successfully sent flag log with %d entries", len(request.FlagAssigned))
			}
		}()
		return nil
	}

	return logger
}

// NewGrpcWasmFlagLoggerWithWriter creates a new GrpcWasmFlagLogger with a custom writer (for testing)
func NewGrpcWasmFlagLoggerWithWriter(stub resolverv1.InternalFlagLoggerServiceClient, writer FlagLogWriter) *GrpcWasmFlagLogger {
	return &GrpcWasmFlagLogger{
		stub:   stub,
		writer: writer,
	}
}

// Write writes flag logs, splitting into chunks if necessary
func (g *GrpcWasmFlagLogger) Write(ctx context.Context, request *resolverv1.WriteFlagLogsRequest) error {
	flagAssignedCount := len(request.FlagAssigned)
	clientResolveCount := len(request.ClientResolveInfo)
	flagResolveCount := len(request.FlagResolveInfo)

	if clientResolveCount == 0 && flagAssignedCount == 0 && flagResolveCount == 0 {
		log.Printf("Skipping empty flag log request")
		return nil
	}

	// Log total counts
	log.Printf("Writing flag logs: %d flag_assigned, %d client_resolve_info, %d flag_resolve_info",
		flagAssignedCount, clientResolveCount, flagResolveCount)

	// If flag_assigned list is small enough, send everything as-is
	if flagAssignedCount <= MaxFlagAssignedPerChunk {
		return g.sendAsync(ctx, request)
	}

	// Split flag_assigned into chunks and send each chunk asynchronously
	log.Printf("Splitting %d flag_assigned entries into chunks of %d",
		flagAssignedCount, MaxFlagAssignedPerChunk)

	chunks := g.createFlagAssignedChunks(request)
	for _, chunk := range chunks {
		if err := g.sendAsync(ctx, chunk); err != nil {
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
