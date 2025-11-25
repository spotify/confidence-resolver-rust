package confidence

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/resolver"
	"github.com/tetratelabs/wazero"
)

var ErrNotInitialized = errors.New("resolver not initialized: call UpdateStateAndFlushLogs first")

// WasmResolverApi is an interface for resolver API operations
type WasmResolverApi interface {
	// UpdateStateAndFlushLogs updates the resolver with new state and flushes any pending logs
	UpdateStateAndFlushLogs(state []byte, accountId string) error
	// ResolveWithSticky resolves flags with sticky assignment support
	ResolveWithSticky(ctx context.Context, request *resolver.ResolveWithStickyRequest) (*resolver.ResolveFlagsResponse, error)
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
	runtime               wazero.Runtime
	compiledModule        wazero.CompiledModule
	flagLogger            FlagLogger
	logger                *slog.Logger
	stickyResolveStrategy StickyResolveStrategy
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
	stickyResolveStrategy StickyResolveStrategy,
) (*SwapWasmResolverApi, error) {
	// Initialize host functions and compile module once
	compiledModule, err := InitializeWasmRuntime(ctx, runtime, wasmBytes)
	if err != nil {
		return nil, err
	}

	swap := &SwapWasmResolverApi{
		runtime:               runtime,
		compiledModule:        compiledModule,
		flagLogger:            flagLogger,
		logger:                logger,
		stickyResolveStrategy: stickyResolveStrategy,
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
	if err := newInstance.SetResolverState(state, accountId); err != nil {
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

func (s *SwapWasmResolverApi) ResolveWithSticky(ctx context.Context, request *resolver.ResolveWithStickyRequest) (*resolver.ResolveFlagsResponse, error) {
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
		response, err = instance.ResolveWithSticky(request)
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	// Handle the response based on result type
	switch result := response.ResolveResult.(type) {
	case *resolver.ResolveWithStickyResponse_Success_:
		success := result.Success
		// Store updates if present and we have a MaterializationRepository
		if len(success.GetUpdates()) > 0 {
			s.storeUpdates(ctx, success.GetUpdates())
		}
		return success.Response, nil

	case *resolver.ResolveWithStickyResponse_MissingMaterializations_:
		missingMaterializations := result.MissingMaterializations

		// Check for ResolverFallback first - return early if so
		if fallback, ok := s.stickyResolveStrategy.(ResolverFallback); ok {
			return fallback.Resolve(ctx, request.GetResolveRequest())
		}

		// Handle MaterializationRepository case
		if repo, ok := s.stickyResolveStrategy.(MaterializationRepository); ok {
			updatedRequest, err := s.handleMissingMaterializations(ctx, request, missingMaterializations.GetItems(), repo)
			if err != nil {
				return nil, fmt.Errorf("failed to handle missing materializations: %w", err)
			}
			return s.ResolveWithSticky(ctx, updatedRequest)
		}

		// If no strategy is configured, return an error
		if s.stickyResolveStrategy == nil {
			return nil, fmt.Errorf("missing materializations and no sticky resolve strategy configured")
		}

		return nil, fmt.Errorf("unknown sticky resolve strategy type: %T", s.stickyResolveStrategy)

	default:
		return nil, fmt.Errorf("unexpected resolve result type: %T", response.ResolveResult)
	}
}

// storeUpdates stores materialization updates asynchronously if we have a MaterializationRepository
func (s *SwapWasmResolverApi) storeUpdates(ctx context.Context, updates []*resolver.ResolveWithStickyResponse_MaterializationUpdate) {
	repo, ok := s.stickyResolveStrategy.(MaterializationRepository)
	if !ok {
		return
	}

	// Store updates asynchronously
	go func() {
		// Group updates by unit
		updatesByUnit := make(map[string][]*resolver.ResolveWithStickyResponse_MaterializationUpdate)
		for _, update := range updates {
			updatesByUnit[update.GetUnit()] = append(updatesByUnit[update.GetUnit()], update)
		}

		// Store assignments for each unit
		for unit, unitUpdates := range updatesByUnit {
			assignments := make(map[string]*MaterializationInfo)
			for _, update := range unitUpdates {
				ruleToVariant := map[string]string{update.GetRule(): update.GetVariant()}
				assignments[update.GetWriteMaterialization()] = &MaterializationInfo{
					UnitInMaterialization: true,
					RuleToVariant:         ruleToVariant,
				}
			}

			if err := repo.StoreAssignment(ctx, unit, assignments); err != nil {
				s.logger.Error("Failed to store materialization updates",
					"unit", unit,
					"error", err)
			}
		}
	}()
}

// handleMissingMaterializations loads missing materializations from the repository
// and returns an updated request with the materializations added
func (s *SwapWasmResolverApi) handleMissingMaterializations(
	ctx context.Context,
	request *resolver.ResolveWithStickyRequest,
	missingItems []*resolver.ResolveWithStickyResponse_MissingMaterializationItem,
	repo MaterializationRepository,
) (*resolver.ResolveWithStickyRequest, error) {
	// Group missing items by unit for efficient loading
	missingByUnit := make(map[string][]*resolver.ResolveWithStickyResponse_MissingMaterializationItem)
	for _, item := range missingItems {
		missingByUnit[item.GetUnit()] = append(missingByUnit[item.GetUnit()], item)
	}

	// Create the materializations per unit map
	materializationsPerUnit := make(map[string]*resolver.MaterializationMap)

	// Copy existing materializations
	for k, v := range request.GetMaterializationsPerUnit() {
		materializationsPerUnit[k] = v
	}

	// Load materialized assignments for all missing units
	for unit, items := range missingByUnit {
		for _, item := range items {
			loadedAssignments, err := repo.LoadMaterializedAssignmentsForUnit(ctx, unit, item.GetReadMaterialization())
			if err != nil {
				return nil, fmt.Errorf("failed to load materializations for unit %s: %w", unit, err)
			}

			// Ensure the map exists for this unit
			if materializationsPerUnit[unit] == nil {
				materializationsPerUnit[unit] = &resolver.MaterializationMap{
					InfoMap: make(map[string]*resolver.MaterializationInfo),
				}
			}

			// Add loaded assignments to the materialization map
			for name, info := range loadedAssignments {
				materializationsPerUnit[unit].InfoMap[name] = info.ToProto()
			}
		}
	}

	// Create a new request with the updated materializations
	return &resolver.ResolveWithStickyRequest{
		ResolveRequest:          request.GetResolveRequest(),
		MaterializationsPerUnit: materializationsPerUnit,
		FailFastOnSticky:        request.GetFailFastOnSticky(),
		NotProcessSticky:        request.GetNotProcessSticky(),
	}, nil
}

// Close closes the current ResolverApi instance
func (s *SwapWasmResolverApi) Close(ctx context.Context) {
	instance := s.currentInstance.Load().(*ResolverApi)
	if instance != nil {
		instance.Close(ctx)
	}
}
