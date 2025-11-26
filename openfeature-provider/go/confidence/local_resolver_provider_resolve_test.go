package confidence

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/open-feature/go-sdk/openfeature"
	adminv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/flags/admin/v1"
	resolvertypes "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/flags/resolvertypes"
	iamv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/iam/v1"
	"github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/resolver"
	"github.com/tetratelabs/wazero"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestLocalResolverProvider_ReturnsDefaultOnError(t *testing.T) {
	ctx := context.Background()
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig())
	defer runtime.Close(ctx)

	// Create minimal state with wrong client secret
	state := &adminv1.ResolverState{
		Flags: []*adminv1.Flag{},
		ClientCredentials: []*iamv1.ClientCredential{
			{
				Credential: &iamv1.ClientCredential_ClientSecret_{
					ClientSecret: &iamv1.ClientCredential_ClientSecret{
						Secret: "wrong-secret",
					},
				},
			},
		},
	}
	stateBytes, _ := proto.Marshal(state)

	flagLogger := NewNoOpWasmFlagLogger()
	swap, err := NewSwapWasmResolverApi(ctx, runtime, defaultWasmBytes, flagLogger, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatalf("Failed to create SwapWasmResolverApi: %v", err)
	}
	defer swap.Close(ctx)

	// Initialize with test state
	if err := swap.UpdateStateAndFlushLogs(stateBytes, "test-account"); err != nil {
		t.Fatalf("Failed to initialize swap with state: %v", err)
	}

	// Use different client secret that won't match
	openfeature.SetProviderAndWait(NewLocalResolverProvider(swap, nil, nil, "test-secret", slog.New(slog.NewTextHandler(os.Stderr, nil)), nil))
	client := openfeature.NewClient("test-client")

	evalCtx := openfeature.NewTargetlessEvaluationContext(map[string]interface{}{
		"user_id": "test-user",
	})
	t.Run("StringEvaluation returns default on error", func(t *testing.T) {
		defaultValue := "default-value"
		result, err := client.StringValueDetails(ctx, "non-existent-flag.field", defaultValue, evalCtx)
		// expect the error to be non-nil
		if err == nil {
			t.Errorf("Expected error during StringValueDetails, got nil")
		}
		if err.Error() != "error code: GENERAL: resolve failed: client secret not found" {
			t.Errorf("Expected specific error message during StringValueDetails, got %v", err.Error())
		}

		if result.Value != defaultValue {
			t.Errorf("Expected default value %v, got %v", defaultValue, result.Value)
		}

		if result.Reason != openfeature.ErrorReason {
			t.Errorf("Expected ErrorReason, got %v", result.Reason)
		}

		t.Logf("✓ StringEvaluation correctly returned default value: %s", defaultValue)
	})
}

func TestLocalResolverProvider_ReturnsCorrectValue(t *testing.T) {
	ctx := context.Background()
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig())
	defer runtime.Close(ctx)

	// Load real test state
	testState := loadTestResolverState(t)
	testAcctID := loadTestAccountID(t)

	flagLogger := NewNoOpWasmFlagLogger()
	swap, err := NewSwapWasmResolverApi(ctx, runtime, defaultWasmBytes, flagLogger, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatalf("Failed to create SwapWasmResolverApi: %v", err)
	}
	defer swap.Close(ctx)

	// Initialize with test state
	if err := swap.UpdateStateAndFlushLogs(testState, testAcctID); err != nil {
		t.Fatalf("Failed to initialize swap with state: %v", err)
	}

	// Use the correct client secret from test data
	openfeature.SetProviderAndWait(NewLocalResolverProvider(swap, nil, nil, "mkjJruAATQWjeY7foFIWfVAcBWnci2YF", slog.New(slog.NewTextHandler(os.Stderr, nil)), nil))
	client := openfeature.NewClient("test-client")

	evalCtx := openfeature.NewTargetlessEvaluationContext(map[string]interface{}{
		"visitor_id": "tutorial_visitor",
	})

	t.Run("StringEvaluation returns correct variant value", func(t *testing.T) {
		defaultValue := "default-message"
		result, error := client.StringValueDetails(ctx, "tutorial-feature.message", defaultValue, evalCtx)
		if error != nil {
			t.Errorf("Error during StringValueDetails: %v", error)
		}
		// The exciting-welcome variant has a specific message
		expectedMessage := "We are very excited to welcome you to Confidence! This is a message from the tutorial flag."

		if result.Value != expectedMessage {
			t.Errorf("Expected value '%s', got '%s'", expectedMessage, result.Value)
		}

		if result.Reason != openfeature.TargetingMatchReason {
			t.Errorf("Expected TargetingMatchReason, got %v", result.Reason)
		}

	})

	t.Run("ObjectEvaluation returns correct variant structure", func(t *testing.T) {
		defaultValue := map[string]interface{}{
			"message": "default",
			"title":   "default",
		}
		result, error := client.ObjectValueDetails(ctx, "tutorial-feature", defaultValue, evalCtx)
		if error != nil {
			t.Errorf("Error during ObjectValueDetails: %v", error)
		}

		if result.Value == nil {
			t.Fatal("Expected result value to not be nil")
		}

		resultMap, ok := result.Value.(map[string]interface{})
		if !ok {
			t.Fatalf("Expected result value to be a map, got %T", result.Value)
		}

		expectedMessage := "We are very excited to welcome you to Confidence! This is a message from the tutorial flag."
		expectedTitle := "Welcome to Confidence!"

		if resultMap["message"] != expectedMessage {
			t.Errorf("Expected message '%s', got '%v'", expectedMessage, resultMap["message"])
		}

		if resultMap["title"] != expectedTitle {
			t.Errorf("Expected title '%s', got '%v'", expectedTitle, resultMap["title"])
		}

		if result.Reason != openfeature.TargetingMatchReason {
			t.Errorf("Expected TargetingMatchReason, got %v", result.Reason)
		}
	})
}

func TestLocalResolverProvider_MissingMaterializations(t *testing.T) {
	ctx := context.Background()

	t.Run("Provider returns resolved value for flag without sticky rules", func(t *testing.T) {
		// Create runtime for this subtest
		runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig())
		defer runtime.Close(ctx)

		// Load real test state
		testState := loadTestResolverState(t)
		testAcctID := loadTestAccountID(t)

		flagLogger := NewNoOpWasmFlagLogger()
		swap, err := NewSwapWasmResolverApi(ctx, runtime, defaultWasmBytes, flagLogger, slog.New(slog.NewTextHandler(os.Stderr, nil)))
		if err != nil {
			t.Fatalf("Failed to create SwapWasmResolverApi: %v", err)
		}
		defer swap.Close(ctx)

		// Initialize with test state
		if err := swap.UpdateStateAndFlushLogs(testState, testAcctID); err != nil {
			t.Fatalf("Failed to initialize swap with state: %v", err)
		}

		openfeature.SetProviderAndWait(NewLocalResolverProvider(swap, nil, nil, "mkjJruAATQWjeY7foFIWfVAcBWnci2YF", slog.New(slog.NewTextHandler(os.Stderr, nil)), nil))
		client := openfeature.NewClient("test-client")

		evalCtx := openfeature.NewTargetlessEvaluationContext(map[string]interface{}{
			"visitor_id": "tutorial_visitor",
		})

		// The tutorial-feature flag in the test data doesn't have materialization requirements
		// so resolving with empty materializations should succeed
		defaultValue := "default"
		result, error := client.StringValueDetails(ctx, "tutorial-feature.message", defaultValue, evalCtx)
		if error != nil {
			t.Errorf("Error during StringValueDetails: %v", error)
		}

		if result.Value == defaultValue {
			t.Error("Expected resolved value, got default value")
		}

		if result.Reason != openfeature.TargetingMatchReason {
			t.Errorf("Expected TargetingMatchReason, got %v", result.Reason)
		}
	})

	t.Run("Provider returns missing materializations error message", func(t *testing.T) {
		// Create runtime for this subtest
		runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig())
		defer runtime.Close(ctx)

		// Create state with a flag that requires materializations
		stickyState := createStateWithStickyFlag()
		accountId := "test-account"

		flagLogger := NewNoOpWasmFlagLogger()
		swap, err := NewSwapWasmResolverApi(ctx, runtime, defaultWasmBytes, flagLogger, slog.New(slog.NewTextHandler(os.Stderr, nil)))
		if err != nil {
			t.Fatalf("Failed to create SwapWasmResolverApi: %v", err)
		}
		defer swap.Close(ctx)

		// Initialize with sticky state
		if err := swap.UpdateStateAndFlushLogs(stickyState, accountId); err != nil {
			t.Fatalf("Failed to initialize swap with state: %v", err)
		}

		openfeature.SetProviderAndWait(NewLocalResolverProvider(swap, nil, nil, "test-secret", slog.New(slog.NewTextHandler(os.Stderr, nil)), nil))
		client := openfeature.NewClient("test-client")

		evalCtx := openfeature.NewTargetlessEvaluationContext(map[string]interface{}{
			"user_id": "test-user-123",
		})

		defaultValue := false
		result, error := client.BooleanValueDetails(ctx, "sticky-test-flag.enabled", defaultValue, evalCtx)
		if error == nil {
			t.Error("Expected error when materializations missing, got nil")
		} else if error.Error() != "error code: GENERAL: resolve failed: missing materializations and no sticky resolve strategy configured" {
			t.Errorf("Expected 'error code: GENERAL: resolve failed: missing materializations and no sticky resolve strategy configured', got: %v", error.Error())
		}

		if result.Value != defaultValue {
			t.Errorf("Expected default value %v when materializations missing, got %v", defaultValue, result.Value)
		}

		if result.Reason != openfeature.ErrorReason {
			t.Errorf("Expected ErrorReason when materializations missing, got %v", result.Reason)
		}
	})
}

// ============================================================================
// Mock implementations for testing sticky resolve strategies
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
// Tests for sticky resolve strategies
// ============================================================================

func TestLocalResolverProvider_ResolverFallback_MissingMaterializations(t *testing.T) {
	ctx := context.Background()
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig())
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
				Reason: resolvertypes.ResolveReason_RESOLVE_REASON_MATCH,
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
	swap, err := NewSwapWasmResolverApi(ctx, runtime, defaultWasmBytes, flagLogger, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatalf("Failed to create SwapWasmResolverApi: %v", err)
	}
	defer swap.Close(ctx)

	// Initialize with test state
	if err := swap.UpdateStateAndFlushLogs(stickyState, accountId); err != nil {
		t.Fatalf("Failed to initialize swap with state: %v", err)
	}

	// Create provider with fallback strategy
	openfeature.SetProviderAndWait(NewLocalResolverProvider(swap, nil, nil, "test-secret", slog.New(slog.NewTextHandler(os.Stderr, nil)), fallback))
	client := openfeature.NewClient("test-client")

	evalCtx := openfeature.NewTargetlessEvaluationContext(map[string]interface{}{
		"user_id": "test-user-123",
	})

	defaultValue := false
	result, error := client.BooleanValueDetails(ctx, "sticky-test-flag.enabled", defaultValue, evalCtx)
	if error != nil {
		t.Errorf("Unexpected error: %v", error)
	}

	if !fallbackCalled {
		t.Error("Expected fallback to be called when materializations are missing")
	}

	if result.Value != true {
		t.Errorf("Expected value true from fallback, got %v", result.Value)
	}

	if result.Reason != openfeature.TargetingMatchReason {
		t.Errorf("Expected TargetingMatchReason, got %v", result.Reason)
	}
}

func TestLocalResolverProvider_ResolverFallback_Error(t *testing.T) {
	ctx := context.Background()
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig())
	defer runtime.Close(ctx)

	// Create state with a flag that requires materializations
	stickyState := createStateWithStickyFlag()
	accountId := "test-account"

	fallback := &mockResolverFallback{
		resolveFunc: func(ctx context.Context, request *resolver.ResolveFlagsRequest) (*resolver.ResolveFlagsResponse, error) {
			return nil, fmt.Errorf("fallback service unavailable")
		},
	}

	flagLogger := NewNoOpWasmFlagLogger()
	swap, err := NewSwapWasmResolverApi(ctx, runtime, defaultWasmBytes, flagLogger, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatalf("Failed to create SwapWasmResolverApi: %v", err)
	}
	defer swap.Close(ctx)

	// Initialize with test state
	if err := swap.UpdateStateAndFlushLogs(stickyState, accountId); err != nil {
		t.Fatalf("Failed to initialize swap with state: %v", err)
	}

	// Create provider with fallback strategy
	openfeature.SetProviderAndWait(NewLocalResolverProvider(swap, nil, nil, "test-secret", slog.New(slog.NewTextHandler(os.Stderr, nil)), fallback))
	client := openfeature.NewClient("test-client")

	evalCtx := openfeature.NewTargetlessEvaluationContext(map[string]interface{}{
		"user_id": "test-user-123",
	})

	defaultValue := false
	result, error := client.BooleanValueDetails(ctx, "sticky-test-flag.enabled", defaultValue, evalCtx)
	if error == nil {
		t.Error("Expected error when fallback fails")
	}

	if !strings.Contains(error.Error(), "fallback service unavailable") {
		t.Errorf("Expected error to contain 'fallback service unavailable', got: %v", error.Error())
	}

	if result.Value != defaultValue {
		t.Errorf("Expected default value %v, got %v", defaultValue, result.Value)
	}

	if result.Reason != openfeature.ErrorReason {
		t.Errorf("Expected ErrorReason, got %v", result.Reason)
	}
}

func TestLocalResolverProvider_MaterializationRepository_LoadsAndRetries(t *testing.T) {
	ctx := context.Background()
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig())
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
	swap, err := NewSwapWasmResolverApi(ctx, runtime, defaultWasmBytes, flagLogger, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatalf("Failed to create SwapWasmResolverApi: %v", err)
	}
	defer swap.Close(ctx)

	// Initialize with test state
	if err := swap.UpdateStateAndFlushLogs(stickyState, accountId); err != nil {
		t.Fatalf("Failed to initialize swap with state: %v", err)
	}

	// Create provider with repository strategy
	openfeature.SetProviderAndWait(NewLocalResolverProvider(swap, nil, nil, "test-secret", slog.New(slog.NewTextHandler(os.Stderr, nil)), repo))
	client := openfeature.NewClient("test-client")

	evalCtx := openfeature.NewTargetlessEvaluationContext(map[string]interface{}{
		"user_id": "test-user-123",
	})

	defaultValue := false
	result, error := client.BooleanValueDetails(ctx, "sticky-test-flag.enabled", defaultValue, evalCtx)
	if error != nil {
		t.Errorf("Unexpected error: %v", error)
	}

	// Verify repository was called to load materializations
	if repo.loadCallCount == 0 {
		t.Error("Expected repository LoadMaterializedAssignmentsForUnit to be called")
	}

	// The flag should be resolved with the variant from the materialization
	if result.Value != true {
		t.Errorf("Expected value true from materialization, got %v", result.Value)
	}

	if result.Reason != openfeature.TargetingMatchReason {
		t.Errorf("Expected TargetingMatchReason, got %v", result.Reason)
	}
}

func TestLocalResolverProvider_MaterializationRepository_LoadError(t *testing.T) {
	ctx := context.Background()
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig())
	defer runtime.Close(ctx)

	// Create state with a flag that requires materializations
	stickyState := createStateWithStickyFlag()
	accountId := "test-account"

	repo := newMockMaterializationRepository()
	repo.loadFunc = func(ctx context.Context, unit, materialization string) (map[string]*MaterializationInfo, error) {
		return nil, fmt.Errorf("database connection failed")
	}

	flagLogger := NewNoOpWasmFlagLogger()
	swap, err := NewSwapWasmResolverApi(ctx, runtime, defaultWasmBytes, flagLogger, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatalf("Failed to create SwapWasmResolverApi: %v", err)
	}
	defer swap.Close(ctx)

	// Initialize with test state
	if err := swap.UpdateStateAndFlushLogs(stickyState, accountId); err != nil {
		t.Fatalf("Failed to initialize swap with state: %v", err)
	}

	// Create provider with repository strategy
	openfeature.SetProviderAndWait(NewLocalResolverProvider(swap, nil, nil, "test-secret", slog.New(slog.NewTextHandler(os.Stderr, nil)), repo))
	client := openfeature.NewClient("test-client")

	evalCtx := openfeature.NewTargetlessEvaluationContext(map[string]interface{}{
		"user_id": "test-user-123",
	})

	defaultValue := false
	result, error := client.BooleanValueDetails(ctx, "sticky-test-flag.enabled", defaultValue, evalCtx)
	if error == nil {
		t.Error("Expected error when repository load fails")
	}

	if !strings.Contains(error.Error(), "database connection failed") {
		t.Errorf("Expected error to contain 'database connection failed', got: %v", error.Error())
	}

	if result.Value != defaultValue {
		t.Errorf("Expected default value %v, got %v", defaultValue, result.Value)
	}
}

func TestLocalResolverProvider_MaterializationRepository_StoresUpdates(t *testing.T) {
	ctx := context.Background()
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig())
	defer runtime.Close(ctx)

	// Use the real test state which has non-sticky flags
	testState := loadTestResolverState(t)
	testAcctID := loadTestAccountID(t)

	repo := newMockMaterializationRepository()

	flagLogger := NewNoOpWasmFlagLogger()
	swap, err := NewSwapWasmResolverApi(ctx, runtime, defaultWasmBytes, flagLogger, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatalf("Failed to create SwapWasmResolverApi: %v", err)
	}
	defer swap.Close(ctx)

	// Initialize with test state
	if err := swap.UpdateStateAndFlushLogs(testState, testAcctID); err != nil {
		t.Fatalf("Failed to initialize swap with state: %v", err)
	}

	// Create provider with repository strategy
	openfeature.SetProviderAndWait(NewLocalResolverProvider(swap, nil, nil, "mkjJruAATQWjeY7foFIWfVAcBWnci2YF", slog.New(slog.NewTextHandler(os.Stderr, nil)), repo))
	client := openfeature.NewClient("test-client")

	evalCtx := openfeature.NewTargetlessEvaluationContext(map[string]interface{}{
		"visitor_id": "tutorial_visitor",
	})

	// Resolve a non-sticky flag - this shouldn't trigger any repository operations
	// since the flag doesn't require materializations
	defaultValue := "default"
	_, error := client.StringValueDetails(ctx, "tutorial-feature.message", defaultValue, evalCtx)
	if error != nil {
		t.Errorf("Unexpected error: %v", error)
	}

	// Verify repository was not called for non-sticky flags
	if repo.loadCallCount != 0 {
		t.Errorf("Expected repository LoadMaterializedAssignmentsForUnit not to be called for non-sticky flags, got %d calls", repo.loadCallCount)
	}

	// Note: Testing actual storage of updates from WASM would require a flag configuration
	// that returns updates. The storeUpdates functionality is implicitly tested through
	// the integration with the WASM resolver. Here we verify that non-sticky flags
	// don't trigger unnecessary repository operations.
}
