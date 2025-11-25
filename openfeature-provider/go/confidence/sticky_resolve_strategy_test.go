package confidence

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/resolver"
	"github.com/tetratelabs/wazero"
	"google.golang.org/protobuf/types/known/structpb"
)

// ============================================================================
// Mock implementations for testing
// ============================================================================

// mockResolverFallback implements ResolverFallback for testing
type mockResolverFallback struct {
	resolveFunc func(ctx context.Context, request *resolver.ResolveFlagsRequest) (*resolver.ResolveFlagsResponse, error)
	closeCalled bool
}

func (m *mockResolverFallback) Resolve(ctx context.Context, request *resolver.ResolveFlagsRequest) (*resolver.ResolveFlagsResponse, error) {
	if m.resolveFunc != nil {
		return m.resolveFunc(ctx, request)
	}
	return &resolver.ResolveFlagsResponse{}, nil
}

func (m *mockResolverFallback) Close() {
	m.closeCalled = true
}

// Compile-time check
var _ ResolverFallback = (*mockResolverFallback)(nil)

// mockMaterializationRepository implements MaterializationRepository for testing
type mockMaterializationRepository struct {
	mu             sync.Mutex
	storage        map[string]map[string]*MaterializationInfo // unit -> materialization -> info
	loadFunc       func(ctx context.Context, unit, materialization string) (map[string]*MaterializationInfo, error)
	storeFunc      func(ctx context.Context, unit string, assignments map[string]*MaterializationInfo) error
	loadCallCount  int
	storeCallCount int
	closeCalled    bool
}

func newMockMaterializationRepository() *mockMaterializationRepository {
	return &mockMaterializationRepository{
		storage: make(map[string]map[string]*MaterializationInfo),
	}
}

func (m *mockMaterializationRepository) LoadMaterializedAssignmentsForUnit(ctx context.Context, unit, materialization string) (map[string]*MaterializationInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loadCallCount++

	if m.loadFunc != nil {
		return m.loadFunc(ctx, unit, materialization)
	}

	result := make(map[string]*MaterializationInfo)
	if unitData, ok := m.storage[unit]; ok {
		for k, v := range unitData {
			result[k] = v
		}
	}
	return result, nil
}

func (m *mockMaterializationRepository) StoreAssignment(ctx context.Context, unit string, assignments map[string]*MaterializationInfo) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.storeCallCount++

	if m.storeFunc != nil {
		return m.storeFunc(ctx, unit, assignments)
	}

	if m.storage[unit] == nil {
		m.storage[unit] = make(map[string]*MaterializationInfo)
	}
	for k, v := range assignments {
		m.storage[unit][k] = v
	}
	return nil
}

func (m *mockMaterializationRepository) Close() {
	m.closeCalled = true
}

// Compile-time check
var _ MaterializationRepository = (*mockMaterializationRepository)(nil)

// ============================================================================
// Integration tests with SwapWasmResolverApi
// ============================================================================

func TestSwapWasmResolverApi_WithResolverFallback_MissingMaterializations(t *testing.T) {
	ctx := context.Background()
	runtime := createTestWasmRuntime(ctx, t)
	defer runtime.Close(ctx)

	// Create state with a flag that requires materializations
	stickyState := createStateWithStickyFlag()
	accountId := "test-account"

	// Track if fallback was called
	fallbackCalled := false
	expectedResponse := &resolver.ResolveFlagsResponse{
		ResolvedFlags: []*resolver.ResolvedFlag{
			{
				Flag:    "flags/sticky-test-flag",
				Variant: "flags/sticky-test-flag/variants/on",
				Value: &structpb.Struct{
					Fields: map[string]*structpb.Value{
						"enabled": structpb.NewBoolValue(true),
					},
				},
			},
		},
	}

	fallback := &mockResolverFallback{
		resolveFunc: func(ctx context.Context, request *resolver.ResolveFlagsRequest) (*resolver.ResolveFlagsResponse, error) {
			fallbackCalled = true
			// Verify the request contains the expected flag
			if len(request.Flags) != 1 || request.Flags[0] != "flags/sticky-test-flag" {
				t.Errorf("Unexpected flags in fallback request: %v", request.Flags)
			}
			return expectedResponse, nil
		},
	}

	flagLogger := NewNoOpWasmFlagLogger()
	swap, err := NewSwapWasmResolverApi(ctx, runtime, defaultWasmBytes, flagLogger, testLogger(), fallback)
	if err != nil {
		t.Fatalf("Failed to create SwapWasmResolverApi: %v", err)
	}
	defer swap.Close(ctx)

	// Initialize with test state
	if err := swap.UpdateStateAndFlushLogs(stickyState, accountId); err != nil {
		t.Fatalf("Failed to initialize swap with state: %v", err)
	}

	// Create request with fail-fast enabled (triggers fallback)
	stickyRequest := &resolver.ResolveWithStickyRequest{
		ResolveRequest: &resolver.ResolveFlagsRequest{
			Flags:        []string{"flags/sticky-test-flag"},
			Apply:        true,
			ClientSecret: "test-secret",
			EvaluationContext: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"user_id": structpb.NewStringValue("test-user-123"),
				},
			},
		},
		MaterializationsPerUnit: make(map[string]*resolver.MaterializationMap),
		FailFastOnSticky:        true,
		NotProcessSticky:        false,
	}

	response, err := swap.ResolveWithSticky(ctx, stickyRequest)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if !fallbackCalled {
		t.Error("Expected fallback to be called when materializations are missing")
	}

	if response == nil {
		t.Fatal("Expected non-nil response")
	}

	if len(response.ResolvedFlags) != 1 {
		t.Errorf("Expected 1 resolved flag, got %d", len(response.ResolvedFlags))
	}
}

func TestSwapWasmResolverApi_WithMaterializationRepository_LoadsAndRetries(t *testing.T) {
	ctx := context.Background()
	runtime := createTestWasmRuntime(ctx, t)
	defer runtime.Close(ctx)

	// Create state with a flag that requires materializations
	stickyState := createStateWithStickyFlag()
	accountId := "test-account"

	repo := newMockMaterializationRepository()
	// Pre-populate with materialization data
	repo.storage["test-user-123"] = map[string]*MaterializationInfo{
		"experiment_v1": {
			UnitInMaterialization: true,
			RuleToVariant:         map[string]string{"flags/sticky-test-flag/rules/sticky-rule": "flags/sticky-test-flag/variants/on"},
		},
	}

	flagLogger := NewNoOpWasmFlagLogger()
	swap, err := NewSwapWasmResolverApi(ctx, runtime, defaultWasmBytes, flagLogger, testLogger(), repo)
	if err != nil {
		t.Fatalf("Failed to create SwapWasmResolverApi: %v", err)
	}
	defer swap.Close(ctx)

	// Initialize with test state
	if err := swap.UpdateStateAndFlushLogs(stickyState, accountId); err != nil {
		t.Fatalf("Failed to initialize swap with state: %v", err)
	}

	// Create request without materializations - should trigger load from repo
	stickyRequest := &resolver.ResolveWithStickyRequest{
		ResolveRequest: &resolver.ResolveFlagsRequest{
			Flags:        []string{"flags/sticky-test-flag"},
			Apply:        true,
			ClientSecret: "test-secret",
			EvaluationContext: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"user_id": structpb.NewStringValue("test-user-123"),
				},
			},
		},
		MaterializationsPerUnit: make(map[string]*resolver.MaterializationMap),
		FailFastOnSticky:        false, // Don't fail fast, load from repo
		NotProcessSticky:        false,
	}

	response, err := swap.ResolveWithSticky(ctx, stickyRequest)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify repository was called to load materializations
	if repo.loadCallCount == 0 {
		t.Error("Expected repository LoadMaterializedAssignmentsForUnit to be called")
	}

	if response == nil {
		t.Fatal("Expected non-nil response")
	}

	// The flag should be resolved with the variant from the materialization
	if len(response.ResolvedFlags) != 1 {
		t.Errorf("Expected 1 resolved flag, got %d", len(response.ResolvedFlags))
	}

	if response.ResolvedFlags[0].Variant != "flags/sticky-test-flag/variants/on" {
		t.Errorf("Expected variant 'flags/sticky-test-flag/variants/on', got '%s'", response.ResolvedFlags[0].Variant)
	}
}

func TestSwapWasmResolverApi_NoStrategy_MissingMaterializations_ReturnsError(t *testing.T) {
	ctx := context.Background()
	runtime := createTestWasmRuntime(ctx, t)
	defer runtime.Close(ctx)

	// Create state with a flag that requires materializations
	stickyState := createStateWithStickyFlag()
	accountId := "test-account"

	// No sticky strategy configured
	flagLogger := NewNoOpWasmFlagLogger()
	swap, err := NewSwapWasmResolverApi(ctx, runtime, defaultWasmBytes, flagLogger, testLogger(), nil)
	if err != nil {
		t.Fatalf("Failed to create SwapWasmResolverApi: %v", err)
	}
	defer swap.Close(ctx)

	// Initialize with test state
	if err := swap.UpdateStateAndFlushLogs(stickyState, accountId); err != nil {
		t.Fatalf("Failed to initialize swap with state: %v", err)
	}

	stickyRequest := &resolver.ResolveWithStickyRequest{
		ResolveRequest: &resolver.ResolveFlagsRequest{
			Flags:        []string{"flags/sticky-test-flag"},
			Apply:        true,
			ClientSecret: "test-secret",
			EvaluationContext: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"user_id": structpb.NewStringValue("test-user-123"),
				},
			},
		},
		MaterializationsPerUnit: make(map[string]*resolver.MaterializationMap),
		FailFastOnSticky:        true,
		NotProcessSticky:        false,
	}

	_, err = swap.ResolveWithSticky(ctx, stickyRequest)
	if err == nil {
		t.Fatal("Expected error when no sticky strategy and materializations missing")
	}

	expectedErrMsg := "missing materializations and no sticky resolve strategy configured"
	if err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedErrMsg, err.Error())
	}
}

func TestSwapWasmResolverApi_ResolverFallback_Error(t *testing.T) {
	ctx := context.Background()
	runtime := createTestWasmRuntime(ctx, t)
	defer runtime.Close(ctx)

	// Create state with a flag that requires materializations
	stickyState := createStateWithStickyFlag()
	accountId := "test-account"

	expectedError := errors.New("fallback service unavailable")
	fallback := &mockResolverFallback{
		resolveFunc: func(ctx context.Context, request *resolver.ResolveFlagsRequest) (*resolver.ResolveFlagsResponse, error) {
			return nil, expectedError
		},
	}

	flagLogger := NewNoOpWasmFlagLogger()
	swap, err := NewSwapWasmResolverApi(ctx, runtime, defaultWasmBytes, flagLogger, testLogger(), fallback)
	if err != nil {
		t.Fatalf("Failed to create SwapWasmResolverApi: %v", err)
	}
	defer swap.Close(ctx)

	// Initialize with test state
	if err := swap.UpdateStateAndFlushLogs(stickyState, accountId); err != nil {
		t.Fatalf("Failed to initialize swap with state: %v", err)
	}

	stickyRequest := &resolver.ResolveWithStickyRequest{
		ResolveRequest: &resolver.ResolveFlagsRequest{
			Flags:        []string{"flags/sticky-test-flag"},
			Apply:        true,
			ClientSecret: "test-secret",
			EvaluationContext: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"user_id": structpb.NewStringValue("test-user-123"),
				},
			},
		},
		MaterializationsPerUnit: make(map[string]*resolver.MaterializationMap),
		FailFastOnSticky:        true,
		NotProcessSticky:        false,
	}

	_, err = swap.ResolveWithSticky(ctx, stickyRequest)
	if err == nil {
		t.Fatal("Expected error when fallback fails")
	}

	if !errors.Is(err, expectedError) {
		t.Errorf("Expected error to contain '%v', got '%v'", expectedError, err)
	}
}

// Helper to create test WASM runtime
func createTestWasmRuntime(ctx context.Context, t *testing.T) wazero.Runtime {
	return wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig())
}
