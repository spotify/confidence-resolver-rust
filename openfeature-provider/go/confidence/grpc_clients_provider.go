package confidence

import (
	"context"
	"fmt"
	"log/slog"

	adminv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/flags/admin/v1"
	resolverv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/flags/resolverinternal"
	iamv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/iam/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const confidenceDomain = "edge-grpc.spotify.com"

// NewGrpcClients creates a StateProvider and FlagLogger backed by Confidence gRPC services
// Returns the StateProvider and FlagLogger
func NewGrpcClients(
	ctx context.Context,
	apiClientID string,
	apiClientSecret string,
	connFactory ConnFactory,
	logger *slog.Logger,
) (StateProvider, FlagLogger, error) {
	// Create TLS credentials for secure connections
	tlsCreds := credentials.NewTLS(nil)

	// Base dial options with transport credentials
	baseOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(tlsCreds),
	}

	// Create auth service connection (no auth interceptor for this one)
	unauthConn, err := connFactory(ctx, confidenceDomain, baseOpts)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create unauthenticated connection: %w", err)
	}

	authService := iamv1.NewAuthServiceClient(unauthConn)

	// Create token holder
	tokenHolder := NewTokenHolder(apiClientID, apiClientSecret, authService, logger)

	// Create JWT auth interceptor
	authInterceptor := NewJwtAuthInterceptor(tokenHolder)

	// Create authenticated connection with auth interceptor
	authConnection, err := connFactory(ctx, confidenceDomain, append(
		append([]grpc.DialOption{}, baseOpts...),
		grpc.WithUnaryInterceptor(authInterceptor.UnaryClientInterceptor()),
	))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create authenticated connection: %w", err)
	}

	// Create gRPC service clients
	resolverStateService := adminv1.NewResolverStateServiceClient(authConnection)
	flagLoggerService := resolverv1.NewInternalFlagLoggerServiceClient(authConnection)

	// Create state fetcher (implements StateProvider interface)
	stateFetcher := NewFlagsAdminStateFetcher(resolverStateService, logger)

	// Create flag logger
	flagLogger := NewGrpcWasmFlagLogger(flagLoggerService, logger)

	return stateFetcher, flagLogger, nil
}
