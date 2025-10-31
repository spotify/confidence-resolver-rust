package confidence

import (
	"context"
	"fmt"
	"os"

	"github.com/tetratelabs/wazero"
)

const (
	ConfidenceDomain = "edge-grpc.spotify.com"
)

// ProviderConfig holds configuration for the Confidence provider
type ProviderConfig struct {
	// Required: API credentials
	APIClientID     string
	APIClientSecret string
	ClientSecret    string

	// Optional: Custom service addresses (for advanced use cases only)
	// If not provided, defaults to EU region
	ResolverStateServiceAddr string
	FlagLoggerServiceAddr    string
	AuthServiceAddr          string

	// Optional: Custom WASM bytes (for advanced use cases only)
	// If not provided, loads from default location
	WasmBytes []byte
	// Optional: Disable logging (default: false)
	DisableLogging bool
}

// ProviderConfigWithStateProvider holds configuration for the Confidence provider with a custom StateProvider
type ProviderConfigWithStateProvider struct {
	// Required: Client secret for signing evaluations
	ClientSecret string

	// Required: State provider for fetching resolver state
	StateProvider StateProvider

	// Required: Account ID
	AccountId string

	// Optional: Disable logging (default: false)
	DisableLogging bool

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

	// Set service addresses to defaults if not provided
	resolverStateServiceAddr := config.ResolverStateServiceAddr
	if resolverStateServiceAddr == "" {
		resolverStateServiceAddr = ConfidenceDomain
	}

	flagLoggerServiceAddr := config.FlagLoggerServiceAddr
	if flagLoggerServiceAddr == "" {
		flagLoggerServiceAddr = ConfidenceDomain
	}

	authServiceAddr := config.AuthServiceAddr
	if authServiceAddr == "" {
		authServiceAddr = ConfidenceDomain
	}

	// Load WASM bytes
	wasmBytes := config.WasmBytes
	if wasmBytes == nil {
		// Load from default filesystem location
		var err error
		wasmBytes, err = os.ReadFile("../../../wasm/confidence_resolver.wasm")
		if err != nil {
			return nil, fmt.Errorf("failed to load WASM module from ../../wasm/confidence_resolver.wasm: %w. Please provide WasmBytes in config or ensure WASM file exists", err)
		}
	}

	runtimeConfig := wazero.NewRuntimeConfig()
	wasmRuntime := wazero.NewRuntimeWithConfig(ctx, runtimeConfig)

	// Create LocalResolverFactory (no custom StateProvider, logging enabled)
	factory, err := NewLocalResolverFactory(
		ctx,
		wasmRuntime,
		wasmBytes,
		resolverStateServiceAddr,
		flagLoggerServiceAddr,
		authServiceAddr,
		config.APIClientID,
		config.APIClientSecret,
		nil,                   // stateProvider
		"",                    // accountId (will be extracted from token)
		config.DisableLogging, // disableLogging
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

	// Load WASM bytes
	wasmBytes := config.WasmBytes
	if wasmBytes == nil {
		// Load from default filesystem location
		var err error
		wasmBytes, err = os.ReadFile("../../../wasm/confidence_resolver.wasm")
		if err != nil {
			return nil, fmt.Errorf("failed to load WASM module from ../../../wasm/confidence_resolver.wasm: %w. Please provide WasmBytes in config or ensure WASM file exists", err)
		}
	}

	runtimeConfig := wazero.NewRuntimeConfig()
	wasmRuntime := wazero.NewRuntimeWithConfig(ctx, runtimeConfig)

	// Create LocalResolverFactory with StateProvider
	// When using StateProvider, we don't need gRPC service addresses or API credentials
	factory, err := NewLocalResolverFactory(
		ctx,
		wasmRuntime,
		wasmBytes,
		"",                    // resolverStateServiceAddr - not used with StateProvider
		"",                    // flagLoggerServiceAddr - not used with StateProvider
		"",                    // authServiceAddr - not used with StateProvider
		"",                    // apiClientID - not used with StateProvider
		"",                    // apiClientSecret - not used with StateProvider
		config.StateProvider,  // stateProvider
		config.AccountId,      // accountId - required with StateProvider
		config.DisableLogging, // disableLogging
	)
	if err != nil {
		wasmRuntime.Close(ctx)
		return nil, fmt.Errorf("failed to create resolver factory with provider: %w", err)
	}

	// Create provider
	provider := NewLocalResolverProvider(factory, config.ClientSecret)

	return provider, nil
}
