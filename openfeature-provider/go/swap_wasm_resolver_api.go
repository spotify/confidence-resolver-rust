package confidence

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/spotify/confidence-resolver-rust/openfeature-provider/go/confidence/proto/resolver"
	"github.com/tetratelabs/wazero"
)

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
	flagLogger     WasmFlagLogger
}

// NewSwapWasmResolverApi creates a new SwapWasmResolverApi with an initial state
func NewSwapWasmResolverApi(
	ctx context.Context,
	runtime wazero.Runtime,
	wasmBytes []byte,
	flagLogger WasmFlagLogger,
	initialState []byte,
	accountId string,
) (*SwapWasmResolverApi, error) {
	// Initialize host functions and compile module once
	compiledModule, err := InitializeWasmRuntime(ctx, runtime, wasmBytes)
	if err != nil {
		return nil, err
	}

	swap := &SwapWasmResolverApi{
		runtime:        runtime,
		compiledModule: compiledModule,
		flagLogger:     flagLogger,
	}

	// Create initial instance
	initialInstance := NewResolverApiFromCompiled(ctx, runtime, compiledModule, flagLogger)
	if err := initialInstance.SetResolverState(initialState, accountId); err != nil {
		return nil, err
	}

	swap.currentInstance.Store(initialInstance)

	return swap, nil
}

func (s *SwapWasmResolverApi) UpdateStateAndFlushLogs(state []byte, accountId string) error {
	// Lock to ensure only one swap operation happens at a time
	s.swapMu.Lock()
	defer s.swapMu.Unlock()

	ctx := context.Background()

	// Create a new instance with the updated state
	newInstance := NewResolverApiFromCompiled(ctx, s.runtime, s.compiledModule, s.flagLogger)
	if err := newInstance.SetResolverState(state, accountId); err != nil {
		return err
	}

	// Atomically swap to the new instance
	oldInstance := s.currentInstance.Swap(newInstance).(*ResolverApi)

	// Close the old instance (which flushes logs)
	oldInstance.Close(ctx)
	return nil
}

func (s *SwapWasmResolverApi) Resolve(request *resolver.ResolveFlagsRequest) (*resolver.ResolveFlagsResponse, error) {
	// Lock to ensure resolve doesn't happen during swap
	instance := s.currentInstance.Load().(*ResolverApi)
	response, err := instance.Resolve(request)

	// If instance is closed, retry with the current instance (which may have been swapped)
	if err != nil && errors.Is(err, ErrInstanceClosed) {
		instance = s.currentInstance.Load().(*ResolverApi)
		return instance.Resolve(request)
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
