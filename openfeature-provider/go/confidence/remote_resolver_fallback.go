package confidence

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	resolvertypes "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/flags/resolvertypes"
	"github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/resolver"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const (
	flagResolverServicePath = "/confidence.flags.resolver.v1.FlagResolverService/ResolveFlags"
	resolverHost            = "resolver.eu.confidence.dev:443"
	defaultResolveTimeout   = 10 * time.Second
)

// RemoteResolverFallback is the default StickyResolveStrategy that falls back to
// the remote Confidence resolver service when the WASM resolver encounters
// missing materializations. This provides automatic handling of sticky assignments
// with server-side storage and 90-day TTL.
type RemoteResolverFallback struct {
	conn   grpc.ClientConnInterface
	logger *slog.Logger
}

// Compile-time interface conformance check
var _ ResolverFallback = (*RemoteResolverFallback)(nil)

// NewRemoteResolverFallback creates a new RemoteResolverFallback.
// It uses the default resolver host with TLS credentials.
func NewRemoteResolverFallback(logger *slog.Logger) (*RemoteResolverFallback, error) {
	return NewRemoteResolverFallbackWithConnFactory(
		context.Background(),
		nil, // use default connection factory
		logger,
	)
}

// NewRemoteResolverFallbackWithConnFactory creates a new RemoteResolverFallback with a custom connection factory.
// If connFactory is nil, uses the default gRPC connection with TLS.
func NewRemoteResolverFallbackWithConnFactory(
	ctx context.Context,
	connFactory ConnFactory,
	logger *slog.Logger,
) (*RemoteResolverFallback, error) {
	tlsCreds := credentials.NewTLS(nil)
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(tlsCreds),
	}

	var conn grpc.ClientConnInterface
	var err error

	if connFactory != nil {
		conn, err = connFactory(ctx, resolverHost, opts)
	} else {
		conn, err = grpc.NewClient(resolverHost, opts...)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection to resolver: %w", err)
	}

	return &RemoteResolverFallback{
		conn:   conn,
		logger: logger,
	}, nil
}

// Resolve performs flag resolution using the remote Confidence resolver service.
// This is called when the WASM resolver encounters missing materializations
// and the provider is configured with ResolverFallback strategy.
func (r *RemoteResolverFallback) Resolve(ctx context.Context, request *resolver.ResolveFlagsRequest) (*resolver.ResolveFlagsResponse, error) {
	if len(request.GetFlags()) == 0 {
		return &resolver.ResolveFlagsResponse{}, nil
	}

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(ctx, defaultResolveTimeout)
	defer cancel()

	// Set SDK info for the request
	if request.Sdk == nil {
		request.Sdk = &resolvertypes.Sdk{
			Sdk: &resolvertypes.Sdk_Id{
				Id: resolvertypes.SdkId_SDK_ID_GO_LOCAL_PROVIDER,
			},
			Version: Version,
		}
	}

	response := &resolver.ResolveFlagsResponse{}

	err := r.conn.Invoke(ctx, flagResolverServicePath, request, response)
	if err != nil {
		r.logger.Error("Failed to resolve flags via remote service", "error", err)
		return nil, fmt.Errorf("remote resolve failed: %w", err)
	}

	r.logger.Debug("Remote resolve successful", "flags_count", len(response.GetResolvedFlags()))
	return response, nil
}

// Close releases resources held by the RemoteResolverFallback.
// If the connection was created by this instance (not provided via ConnFactory),
// it attempts to close the underlying connection.
func (r *RemoteResolverFallback) Close() {
	// If conn implements io.Closer, close it
	if closer, ok := r.conn.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			r.logger.Warn("Failed to close gRPC connection", "error", err)
		}
	}
}
