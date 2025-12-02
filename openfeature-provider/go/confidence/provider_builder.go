package confidence

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	resolverv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/flags/resolverinternal"
	"github.com/tetratelabs/wazero"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const confidenceDomain = "edge-grpc.spotify.com"

type ProviderConfig struct {
	ClientSecret         string
	Logger               *slog.Logger
	TransportHooks       TransportHooks       // Optional: defaults to DefaultTransportHooks
	MaterializationStore MaterializationStore // Optional: defaults to UnsupportedMaterializationStore
}

type ProviderTestConfig struct {
	StateProvider        StateProvider
	FlagLogger           FlagLogger
	ClientSecret         string
	Logger               *slog.Logger
	MaterializationStore MaterializationStore // Optional: defaults to UnsupportedMaterializationStore
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
	hooks := config.TransportHooks
	if hooks == nil {
		hooks = DefaultTransportHooks
	}

	tlsCreds := credentials.NewTLS(nil)
	baseOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(tlsCreds),
	}

	target, opts := hooks.ModifyGRPCDial(confidenceDomain, baseOpts)
	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		wasmRuntime.Close(ctx)
		return nil, fmt.Errorf("failed to create connection: %w", err)
	}

	// Create state provider and flag logger
	flagLoggerService := resolverv1.NewInternalFlagLoggerServiceClient(conn)
	// Build HTTP transport using hooks and pass into state fetcher
	transport := hooks.WrapHTTP(http.DefaultTransport)
	stateProvider := NewFlagsAdminStateFetcherWithTransport(config.ClientSecret, logger, transport)
	flagLogger := NewGrpcWasmFlagLogger(flagLoggerService, config.ClientSecret, logger)

	resolverAPI, err := NewSwapWasmResolverApi(ctx, wasmRuntime, defaultWasmBytes, flagLogger, logger)
	if err != nil {
		wasmRuntime.Close(ctx)
		return nil, fmt.Errorf("failed to create resolver API: %w", err)
	}

	materializationStore := config.MaterializationStore
	if materializationStore == nil {
		materializationStore = NewUnsupportedMaterializationStore()
	}

	provider := NewLocalResolverProvider(resolverAPI, stateProvider, flagLogger, config.ClientSecret, logger, materializationStore)

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

	materializationStore := config.MaterializationStore
	if materializationStore == nil {
		materializationStore = NewUnsupportedMaterializationStore()
	}

	provider := NewLocalResolverProvider(resolverAPI, config.StateProvider, config.FlagLogger, config.ClientSecret, logger, materializationStore)

	return provider, nil
}
