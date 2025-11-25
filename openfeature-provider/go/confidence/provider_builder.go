package confidence

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	resolverv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/flags/resolverinternal"
	"github.com/tetratelabs/wazero"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const confidenceDomain = "edge-grpc.spotify.com"

// ConnFactory is an optional testing/advanced hook for customizing gRPC connections.
// If nil, a default connection to the Confidence service is created.
type ConnFactory func(ctx context.Context, target string, defaultOpts []grpc.DialOption) (grpc.ClientConnInterface, error)

type ProviderConfig struct {
	ClientSecret string
	Logger       *slog.Logger
	ConnFactory  ConnFactory // Optional: for testing/advanced use cases
}

type ProviderTestConfig struct {
	StateProvider StateProvider
	FlagLogger    FlagLogger
	Logger        *slog.Logger
}

func NewProvider(ctx context.Context, config ProviderConfig) (*LocalResolverProvider, error) {
	if config.ClientSecret == "" {
		return nil, fmt.Errorf("ClientSecret is required")
	}

	logger := config.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
	}

	runtimeConfig := wazero.NewRuntimeConfig()
	wasmRuntime := wazero.NewRuntimeWithConfig(ctx, runtimeConfig)

	// Create gRPC connection for flag logger
	connFactory := config.ConnFactory
	if connFactory == nil {
		connFactory = func(ctx context.Context, target string, defaultOpts []grpc.DialOption) (grpc.ClientConnInterface, error) {
			return grpc.NewClient(target, defaultOpts...)
		}
	}

	tlsCreds := credentials.NewTLS(nil)
	baseOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(tlsCreds),
	}

	conn, err := connFactory(ctx, confidenceDomain, baseOpts)
	if err != nil {
		wasmRuntime.Close(ctx)
		return nil, fmt.Errorf("failed to create connection: %w", err)
	}

	// Create state provider and flag logger
	flagLoggerService := resolverv1.NewInternalFlagLoggerServiceClient(conn)
	stateProvider := NewFlagsAdminStateFetcher(config.ClientSecret, logger)
	flagLogger := NewGrpcWasmFlagLogger(flagLoggerService, config.ClientSecret, logger)

	resolverAPI, err := NewSwapWasmResolverApi(ctx, wasmRuntime, defaultWasmBytes, flagLogger, logger)
	if err != nil {
		wasmRuntime.Close(ctx)
		return nil, fmt.Errorf("failed to create resolver API: %w", err)
	}

	provider := NewLocalResolverProvider(resolverAPI, stateProvider, flagLogger, config.ClientSecret, logger)

	return provider, nil
}

// NewProviderForTest creates a provider with mocked StateProvider and FlagLogger for testing
func NewProviderForTest(ctx context.Context, config ProviderTestConfig) (*LocalResolverProvider, error) {
	if config.StateProvider == nil {
		return nil, fmt.Errorf("StateProvider is required")
	}
	if config.FlagLogger == nil {
		return nil, fmt.Errorf("FlagLogger is required")
	}

	logger := config.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
	}

	runtimeConfig := wazero.NewRuntimeConfig()
	wasmRuntime := wazero.NewRuntimeWithConfig(ctx, runtimeConfig)

	resolverAPI, err := NewSwapWasmResolverApi(ctx, wasmRuntime, defaultWasmBytes, config.FlagLogger, logger)
	if err != nil {
		wasmRuntime.Close(ctx)
		return nil, fmt.Errorf("failed to create resolver API: %w", err)
	}

	provider := NewLocalResolverProvider(resolverAPI, config.StateProvider, config.FlagLogger, "", logger)

	return provider, nil
}
