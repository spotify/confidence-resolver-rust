package confidence

import (
	"context"
	"fmt"

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
	StateProvider StateProvider

	// Required: Account ID
	AccountId string

	// Optional: Custom WASM bytes (for advanced use cases only)
	// If not provided, loads from default location
	WasmBytes []byte
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

	// Use embedded WASM module
	wasmBytes := defaultWasmBytes

	runtimeConfig := wazero.NewRuntimeConfig()
	wasmRuntime := wazero.NewRuntimeWithConfig(ctx, runtimeConfig)

	// Build connection factory (use default if none provided)
	connFactory := config.ConnFactory
	if connFactory == nil {
		connFactory = func(ctx context.Context, target string, defaultOpts []grpc.DialOption) (grpc.ClientConnInterface, error) {
			return grpc.NewClient(target, defaultOpts...)
		}
	}

	// Create LocalResolverFactory (no custom StateProvider)
	factory, err := NewLocalResolverFactory(
		ctx,
		wasmRuntime,
		wasmBytes,
		config.APIClientID,
		config.APIClientSecret,
		nil, // stateProvider
		"",  // accountId (will be extracted from token)
		connFactory,
	)
	if err != nil {
		wasmRuntime.Close(ctx)
		return nil, fmt.Errorf("failed to create resolver factory: %w", err)
	}

	// Create provider
	provider := NewLocalResolverProvider(factory, config.ClientSecret)

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
	if config.AccountId == "" {
		return nil, fmt.Errorf("AccountId is required")
	}

	// Use custom WASM bytes if provided, otherwise use embedded default
	wasmBytes := config.WasmBytes
	if wasmBytes == nil {
		wasmBytes = defaultWasmBytes
	}

	runtimeConfig := wazero.NewRuntimeConfig()
	wasmRuntime := wazero.NewRuntimeWithConfig(ctx, runtimeConfig)

	// Build connection factory (use default)
	connFactory := func(ctx context.Context, target string, defaultOpts []grpc.DialOption) (grpc.ClientConnInterface, error) {
		return grpc.NewClient(target, defaultOpts...)
	}

	// Create LocalResolverFactory with StateProvider
	// When using StateProvider, we don't need gRPC service addresses or API credentials
	factory, err := NewLocalResolverFactory(
		ctx,
		wasmRuntime,
		wasmBytes,
		"",                   // apiClientID - not used with StateProvider
		"",                   // apiClientSecret - not used with StateProvider
		config.StateProvider, // stateProvider
		config.AccountId,     // accountId - required with StateProvider
		connFactory,          // connFactory - unused here but passed for consistency
	)
	if err != nil {
		wasmRuntime.Close(ctx)
		return nil, fmt.Errorf("failed to create resolver factory with provider: %w", err)
	}

	// Create provider
	provider := NewLocalResolverProvider(factory, config.ClientSecret)

	return provider, nil
}
