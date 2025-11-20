package confidence

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/resolver"
	"github.com/tetratelabs/wazero"
)

var ErrNotInitialized = errors.New("resolver not initialized: call UpdateStateAndFlushLogs first")

// WasmResolverApi is an interface for resolver API operations
type WasmResolverApi interface {
	// UpdateStateAndFlushLogs updates the resolver with new state and flushes any pending logs
	UpdateStateAndFlushLogs(state []byte, accountId string) error
	// ResolveWithSticky resolves flags with sticky assignment support
	ResolveWithSticky(request *resolver.ResolveWithStickyRequest) (*resolver.ResolveWithStickyResponse, error)
	// Close closes the resolver and flushes any pending logs
	Close(ctx context.Context)
}

// SwapWasmResolverApi wraps ResolverApi and allows atomic swapping of instances
// Similar to Java's SwapWasmResolverApi, it creates a new instance on each state update,
// swaps it atomically, and closes the old one (which flushes logs)
type SwapWasmResolverApi struct {
	// Atomic reference to the current ResolverApi instance
	currentInstance atomic.Value // stores *ResolverApi

	// Mutex to protect the swap operation
	swapMu sync.Mutex

	// Dependencies needed to create new instances
	runtime        wazero.Runtime
	compiledModule wazero.CompiledModule
	flagLogger     FlagLogger
	logger         *slog.Logger

	// Unique client instance ID for metric deduplication
	// Generated once at creation and passed to all WASM instances
	clientInstanceID string
}

// Compile-time interface conformance check
var _ WasmResolverApi = (*SwapWasmResolverApi)(nil)

// NewSwapWasmResolverApi creates a new SwapWasmResolverApi
// The instance is created without initial state (lazy initialization).
// Call UpdateStateAndFlushLogs to initialize with state.
func NewSwapWasmResolverApi(
	ctx context.Context,
	runtime wazero.Runtime,
	wasmBytes []byte,
	flagLogger FlagLogger,
	logger *slog.Logger,
) (*SwapWasmResolverApi, error) {
	// Initialize host functions and compile module once
	compiledModule, err := InitializeWasmRuntime(ctx, runtime, wasmBytes)
	if err != nil {
		return nil, err
	}

	// Generate unique client instance ID (stable across WASM recreations)
	clientInstanceID := uuid.New().String()

	swap := &SwapWasmResolverApi{
		runtime:          runtime,
		compiledModule:   compiledModule,
		flagLogger:       flagLogger,
		logger:           logger,
		clientInstanceID: clientInstanceID,
	}

	// Store nil to indicate lazy initialization
	swap.currentInstance.Store((*ResolverApi)(nil))

	return swap, nil
}

func (s *SwapWasmResolverApi) UpdateStateAndFlushLogs(state []byte, accountId string) error {
	// Lock to ensure only one swap operation happens at a time
	s.swapMu.Lock()
	defer s.swapMu.Unlock()

	ctx := context.Background()

	// Create new instance with updated state
	newInstance := NewResolverApiFromCompiled(ctx, s.runtime, s.compiledModule, s.flagLogger, s.logger)
	if err := newInstance.SetResolverState(state, accountId, s.clientInstanceID); err != nil {
		return err
	}

	// Atomically swap to the new instance
	oldInstance := s.currentInstance.Swap(newInstance).(*ResolverApi)

	// Close the old instance (which flushes logs), but only if it's not nil
	// (nil indicates this was the first initialization)
	if oldInstance != nil {
		oldInstance.Close(ctx)
	}
	return nil
}

func (s *SwapWasmResolverApi) ResolveWithSticky(request *resolver.ResolveWithStickyRequest) (*resolver.ResolveWithStickyResponse, error) {
	// Load the current instance
	instance := s.currentInstance.Load().(*ResolverApi)

	// Check if instance is nil (not yet initialized)
	if instance == nil {
		return nil, ErrNotInitialized
	}

	response, err := instance.ResolveWithSticky(request)

	// If instance is closed, retry with the current instance (which may have been swapped)
	if err != nil && errors.Is(err, ErrInstanceClosed) {
		instance = s.currentInstance.Load().(*ResolverApi)
		// Check again for nil after reload
		if instance == nil {
			return nil, ErrNotInitialized
		}
		return instance.ResolveWithSticky(request)
	}

	return response, err
}

// Close closes the current ResolverApi instance
func (s *SwapWasmResolverApi) Close(ctx context.Context) {
	instance := s.currentInstance.Load().(*ResolverApi)
	if instance != nil {
		instance.Close(ctx)
	}
}
