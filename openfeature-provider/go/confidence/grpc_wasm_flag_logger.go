package confidence

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	resolverv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/flags/resolverinternal"
	"google.golang.org/grpc/metadata"
)

const (
	// MaxFlagAssignedPerChunk is the max number of flag_assigned entries per chunk
	// to avoid exceeding gRPC max message size
	MaxFlagAssignedPerChunk = 1000
)

type FlagLogger interface {
	Write(ctx context.Context, request *resolverv1.WriteFlagLogsRequest) error
	Shutdown()
}

type GrpcFlagLogger struct {
	stub         resolverv1.InternalFlagLoggerServiceClient
	clientSecret string
	logger       *slog.Logger
	wg           sync.WaitGroup
}

// Compile-time interface conformance check
var _ FlagLogger = (*GrpcFlagLogger)(nil)

func NewGrpcWasmFlagLogger(stub resolverv1.InternalFlagLoggerServiceClient, clientSecret string, logger *slog.Logger) *GrpcFlagLogger {
	return &GrpcFlagLogger{
		stub:         stub,
		clientSecret: clientSecret,
		logger:       logger,
	}
}

// Write writes flag logs, splitting into chunks if necessary
func (g *GrpcFlagLogger) Write(ctx context.Context, request *resolverv1.WriteFlagLogsRequest) error {
	flagAssignedCount := len(request.FlagAssigned)
	clientResolveCount := len(request.ClientResolveInfo)
	flagResolveCount := len(request.FlagResolveInfo)

	if clientResolveCount == 0 && flagAssignedCount == 0 && flagResolveCount == 0 {
		g.logger.Debug("Skipping empty flag log request")
		return nil
	}

	if request.TelemetryData != nil {
		sdkID := "nil"
		sdkVersion := "nil"
		if request.TelemetryData.Sdk != nil {
			sdkID = request.TelemetryData.Sdk.GetId().String()
			sdkVersion = request.TelemetryData.Sdk.Version
		}
		g.logger.Info("Telemetry Data",
			"sdk_id", sdkID,
			"sdk_version", sdkVersion)
	}

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
func (g *GrpcFlagLogger) createFlagAssignedChunks(request *resolverv1.WriteFlagLogsRequest) []*resolverv1.WriteFlagLogsRequest {
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

func (g *GrpcFlagLogger) sendAsync(ctx context.Context, request *resolverv1.WriteFlagLogsRequest) error {
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		// Create a context with timeout for the RPC
		rpcCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Add Authorization header with client secret
		md := metadata.Pairs("authorization", fmt.Sprintf("ClientSecret %s", g.clientSecret))
		rpcCtx = metadata.NewOutgoingContext(rpcCtx, md)

		if _, err := g.stub.ClientWriteFlagLogs(rpcCtx, request); err != nil {
			g.logger.Error("Failed to write flag logs", "error", err)
		} else {
			g.logger.Info("Successfully sent flag log", "entries", len(request.FlagAssigned))
		}
	}()
	return nil
}

// Shutdown waits for all pending async writes to complete
func (g *GrpcFlagLogger) Shutdown() {
	g.wg.Wait()
}

// NoOpWasmFlagLogger is a flag logger that drops all requests (for disabled logging)
type NoOpWasmFlagLogger struct{}

// Compile-time interface conformance check
var _ FlagLogger = (*NoOpWasmFlagLogger)(nil)

func NewNoOpWasmFlagLogger() *NoOpWasmFlagLogger {
	return &NoOpWasmFlagLogger{}
}

func (n *NoOpWasmFlagLogger) Write(ctx context.Context, request *resolverv1.WriteFlagLogsRequest) error {
	// Drop the request - do nothing
	return nil
}

func (n *NoOpWasmFlagLogger) Shutdown() {
	// Nothing to shut down
}
