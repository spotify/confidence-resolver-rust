package confidence

import (
	"context"
	"fmt"
	"log/slog"

	resolverv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/flags/resolverinternal"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const confidenceDomain = "edge-grpc.spotify.com"

func NewGrpcClients(
	ctx context.Context,
	clientSecret string,
	connFactory ConnFactory,
	logger *slog.Logger,
) (StateProvider, FlagLogger, error) {
	tlsCreds := credentials.NewTLS(nil)

	baseOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(tlsCreds),
	}

	conn, err := connFactory(ctx, confidenceDomain, baseOpts)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create connection: %w", err)
	}

	flagLoggerService := resolverv1.NewInternalFlagLoggerServiceClient(conn)
	stateFetcher := NewFlagsAdminStateFetcher(clientSecret, logger)
	flagLogger := NewGrpcWasmFlagLogger(flagLoggerService, clientSecret, logger)

	return stateFetcher, flagLogger, nil
}
