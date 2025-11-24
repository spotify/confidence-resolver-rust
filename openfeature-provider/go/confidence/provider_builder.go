package confidence

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/tetratelabs/wazero"
	"google.golang.org/grpc"
)

// ConnFactory is an advanced/testing hook allowing callers to customize how
// gRPC connections are created. The provider will pass the computed target and
// its default DialOptions (TLS and required interceptors where applicable).
// Implementations may modify options, change targets, or replace the dialing
// mechanism entirely. Returning a connection with incompatible security/auth
// can break functionality; use with care.
type ConnFactory func(ctx context.Context, target string, defaultOpts []grpc.DialOption) (grpc.ClientConnInterface, error)

type ProviderConfig struct {
	ClientSecret string
	Logger       *slog.Logger
	ConnFactory  ConnFactory
}

type ProviderConfigWithStateProvider struct {
	ClientSecret  string
	StateProvider StateProvider
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

	connFactory := config.ConnFactory
	if connFactory == nil {
		connFactory = func(ctx context.Context, target string, defaultOpts []grpc.DialOption) (grpc.ClientConnInterface, error) {
			return grpc.NewClient(target, defaultOpts...)
		}
	}

	stateProvider, flagLogger, err := NewGrpcClients(
		ctx,
		config.ClientSecret,
		connFactory,
		logger,
	)
	if err != nil {
		wasmRuntime.Close(ctx)
		return nil, fmt.Errorf("failed to create gRPC clients: %w", err)
	}

	resolverAPI, err := NewSwapWasmResolverApi(ctx, wasmRuntime, defaultWasmBytes, flagLogger, logger)
	if err != nil {
		wasmRuntime.Close(ctx)
		return nil, fmt.Errorf("failed to create resolver API: %w", err)
	}

	provider := NewLocalResolverProvider(resolverAPI, stateProvider, flagLogger, config.ClientSecret, logger)

	return provider, nil
}

func NewProviderWithStateProvider(ctx context.Context, config ProviderConfigWithStateProvider) (*LocalResolverProvider, error) {
	if config.ClientSecret == "" {
		return nil, fmt.Errorf("ClientSecret is required")
	}
	if config.StateProvider == nil {
		return nil, fmt.Errorf("StateProvider is required")
	}

	logger := config.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
	}

	runtimeConfig := wazero.NewRuntimeConfig()
	wasmRuntime := wazero.NewRuntimeWithConfig(ctx, runtimeConfig)

	flagLogger := NewNoOpWasmFlagLogger()

	resolverAPI, err := NewSwapWasmResolverApi(ctx, wasmRuntime, defaultWasmBytes, flagLogger, logger)
	if err != nil {
		wasmRuntime.Close(ctx)
		return nil, fmt.Errorf("failed to create resolver API: %w", err)
	}

	provider := NewLocalResolverProvider(resolverAPI, config.StateProvider, flagLogger, config.ClientSecret, logger)

	return provider, nil
}
