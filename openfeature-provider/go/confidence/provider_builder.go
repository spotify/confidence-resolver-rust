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

// ProviderConfig holds configuration for the Confidence provider
type ProviderConfig struct {
	// Required: API credentials
	APIClientID     string
	APIClientSecret string
	ClientSecret    string

	// Optional: Custom logger for provider operations
	// If not provided, a default slog.Logger will be created
	Logger *slog.Logger

	// Advanced/testing: override connection creation
	ConnFactory ConnFactory
}

// ProviderConfigWithStateProvider holds configuration for the Confidence provider with a custom StateProvider
// WARNING: This configuration is intended for testing and advanced use cases ONLY.
// For production deployments, use NewProvider() with ProviderConfig instead, which provides:
//   - Automatic state fetching from Confidence backend
//   - Built-in authentication and secure connections
//   - Exposure event logging for analytics
//
// Only use this when you need to provide a custom state source (e.g., local file cache for testing).
type ProviderConfigWithStateProvider struct {
	// Required: Client secret for signing evaluations
	ClientSecret string

	// Required: State provider for fetching resolver state
	// The StateProvider must implement GetAccountID() to provide the account ID
	StateProvider StateProvider

	// Optional: Custom logger for provider operations
	// If not provided, a default slog.Logger will be created
	Logger *slog.Logger
}

// NewProvider creates a new Confidence OpenFeature provider with simple configuration
func NewProvider(ctx context.Context, config ProviderConfig) (*LocalResolverProvider, error) {
	// Validate required fields
	if config.APIClientID == "" {
		return nil, fmt.Errorf("APIClientID is required")
	}
	if config.APIClientSecret == "" {
		return nil, fmt.Errorf("APIClientSecret is required")
	}
	if config.ClientSecret == "" {
		return nil, fmt.Errorf("ClientSecret is required")
	}

	// Create logger if not provided
	logger := config.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
	}

	runtimeConfig := wazero.NewRuntimeConfig()
	wasmRuntime := wazero.NewRuntimeWithConfig(ctx, runtimeConfig)

	// Build connection factory (use default if none provided)
	connFactory := config.ConnFactory
	if connFactory == nil {
		connFactory = func(ctx context.Context, target string, defaultOpts []grpc.DialOption) (grpc.ClientConnInterface, error) {
			return grpc.NewClient(target, defaultOpts...)
		}
	}

	// Create gRPC StateProvider and FlagLogger
	stateProvider, flagLogger, err := NewGrpcStateProvider(
		ctx,
		config.APIClientID,
		config.APIClientSecret,
		connFactory,
		logger,
	)
	if err != nil {
		wasmRuntime.Close(ctx)
		return nil, fmt.Errorf("failed to create gRPC state provider: %w", err)
	}

	// Create SwapWasmResolverApi without initial state (lazy initialization)
	// State will be set during Provider.Init()
	resolverAPI, err := NewSwapWasmResolverApi(ctx, wasmRuntime, defaultWasmBytes, flagLogger, logger)
	if err != nil {
		wasmRuntime.Close(ctx)
		return nil, fmt.Errorf("failed to create resolver API: %w", err)
	}

	// Create provider
	provider := NewLocalResolverProvider(resolverAPI, stateProvider, flagLogger, config.ClientSecret, logger)

	return provider, nil
}

// NewProviderWithStateProvider creates a new Confidence OpenFeature provider with a custom StateProvider
// Should only be used for testing purposes. Will not emit exposure logging.
func NewProviderWithStateProvider(ctx context.Context, config ProviderConfigWithStateProvider) (*LocalResolverProvider, error) {
	// Validate required fields
	if config.ClientSecret == "" {
		return nil, fmt.Errorf("ClientSecret is required")
	}
	if config.StateProvider == nil {
		return nil, fmt.Errorf("StateProvider is required")
	}

	// Create logger if not provided
	logger := config.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
	}

	runtimeConfig := wazero.NewRuntimeConfig()
	wasmRuntime := wazero.NewRuntimeWithConfig(ctx, runtimeConfig)

	// When using custom StateProvider, no gRPC logger service is available
	// Exposure logging is disabled
	flagLogger := NewNoOpWasmFlagLogger()

	// Create SwapWasmResolverApi without initial state (lazy initialization)
	// State will be set during Provider.Init()
	resolverAPI, err := NewSwapWasmResolverApi(ctx, wasmRuntime, defaultWasmBytes, flagLogger, logger)
	if err != nil {
		wasmRuntime.Close(ctx)
		return nil, fmt.Errorf("failed to create resolver API: %w", err)
	}

	// Create provider
	provider := NewLocalResolverProvider(resolverAPI, config.StateProvider, flagLogger, config.ClientSecret, logger)

	return provider, nil
}
