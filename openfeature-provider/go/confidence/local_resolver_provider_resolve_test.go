package confidence

import (
	"context"
	"testing"

	"github.com/open-feature/go-sdk/openfeature"
	adminv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/flags/admin/v1"
	iamv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/iam/v1"
	"github.com/tetratelabs/wazero"
	"google.golang.org/protobuf/proto"
)

// TestLocalResolverProvider_ReturnsDefaultOnError tests that
// default values are returned when resolve fails
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
	swap, err := NewSwapWasmResolverApi(ctx, runtime, defaultWasmBytes, flagLogger, stateBytes, "test-account")
	if err != nil {
		t.Fatalf("Failed to create SwapWasmResolverApi: %v", err)
	}
	defer swap.Close(ctx)

	factory := &LocalResolverFactory{
		resolverAPI: swap,
	}

	// Use different client secret that won't match
	provider := NewLocalResolverProvider(factory, "test-secret")

	evalCtx := openfeature.FlattenedContext{
		"user_id": "test-user",
	}

	t.Run("BooleanEvaluation returns default on error", func(t *testing.T) {
		defaultValue := true
		result := provider.BooleanEvaluation(ctx, "non-existent-flag", defaultValue, evalCtx)

		if result.Value != defaultValue {
			t.Errorf("Expected default value %v, got %v", defaultValue, result.Value)
		}

		if result.Reason != openfeature.ErrorReason {
			t.Errorf("Expected ErrorReason, got %v", result.Reason)
		}

		if result.ResolutionError.Error() == "" {
			t.Error("Expected ResolutionError to not be empty")
		}

		t.Logf("✓ BooleanEvaluation correctly returned default value: %v", defaultValue)
	})
}

// TestLocalResolverProvider_ReturnsCorrectValue tests that
// correct values are returned when resolve succeeds with real test data
func TestLocalResolverProvider_ReturnsCorrectValue(t *testing.T) {
	ctx := context.Background()
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig())
	defer runtime.Close(ctx)

	// Load real test state
	testState := loadTestResolverState(t)
	testAcctID := loadTestAccountID(t)

	flagLogger := NewNoOpWasmFlagLogger()
	swap, err := NewSwapWasmResolverApi(ctx, runtime, defaultWasmBytes, flagLogger, testState, testAcctID)
	if err != nil {
		t.Fatalf("Failed to create SwapWasmResolverApi: %v", err)
	}
	defer swap.Close(ctx)

	factory := &LocalResolverFactory{
		resolverAPI: swap,
	}

	// Use the correct client secret from test data
	provider := NewLocalResolverProvider(factory, "mkjJruAATQWjeY7foFIWfVAcBWnci2YF")

	evalCtx := openfeature.FlattenedContext{
		"visitor_id": "tutorial_visitor",
	}

	t.Run("StringEvaluation returns correct variant value", func(t *testing.T) {
		defaultValue := "default-message"
		result := provider.StringEvaluation(ctx, "tutorial-feature.message", defaultValue, evalCtx)

		// The exciting-welcome variant has a specific message
		expectedMessage := "We are very excited to welcome you to Confidence! This is a message from the tutorial flag."

		if result.Value != expectedMessage {
			t.Errorf("Expected value '%s', got '%s'", expectedMessage, result.Value)
		}

		if result.Reason != openfeature.TargetingMatchReason {
			t.Errorf("Expected TargetingMatchReason, got %v", result.Reason)
		}

		if result.ResolutionError.Error() != "" {
			t.Errorf("Expected no error, got %v", result.ResolutionError)
		}

		t.Logf("✓ StringEvaluation correctly returned variant value: %s", result.Value)
	})

	t.Run("ObjectEvaluation returns correct variant structure", func(t *testing.T) {
		defaultValue := map[string]interface{}{
			"message": "default",
			"title":   "default",
		}
		result := provider.ObjectEvaluation(ctx, "tutorial-feature", defaultValue, evalCtx)

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

		if result.ResolutionError.Error() != "" {
			t.Errorf("Expected no error, got %v", result.ResolutionError)
		}

		t.Logf("✓ ObjectEvaluation correctly returned variant structure: %v", resultMap)
	})
}

// TestLocalResolverProvider_MissingMaterializations tests
// that missing materializations are handled correctly
func TestLocalResolverProvider_MissingMaterializations(t *testing.T) {
	ctx := context.Background()
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig())
	defer runtime.Close(ctx)

	t.Run("Provider returns resolved value for flag without sticky rules", func(t *testing.T) {
		// Load real test state
		testState := loadTestResolverState(t)
		testAcctID := loadTestAccountID(t)

		flagLogger := NewNoOpWasmFlagLogger()
		swap, err := NewSwapWasmResolverApi(ctx, runtime, defaultWasmBytes, flagLogger, testState, testAcctID)
		if err != nil {
			t.Fatalf("Failed to create SwapWasmResolverApi: %v", err)
		}
		defer swap.Close(ctx)

		factory := &LocalResolverFactory{
			resolverAPI: swap,
		}

		provider := NewLocalResolverProvider(factory, "mkjJruAATQWjeY7foFIWfVAcBWnci2YF")

		evalCtx := openfeature.FlattenedContext{
			"visitor_id": "tutorial_visitor",
		}

		// The tutorial-feature flag in the test data doesn't have materialization requirements
		// so resolving with empty materializations should succeed
		defaultValue := "default"
		result := provider.StringEvaluation(ctx, "tutorial-feature.message", defaultValue, evalCtx)

		// Should succeed - tutorial-feature doesn't require materializations
		if result.ResolutionError.Error() != "" {
			t.Errorf("Expected successful resolve for flag without sticky rules, got error: %v", result.ResolutionError)
		}

		if result.Value == defaultValue {
			t.Error("Expected resolved value, got default value")
		}

		if result.Reason != openfeature.TargetingMatchReason {
			t.Errorf("Expected TargetingMatchReason, got %v", result.Reason)
		}

		t.Logf("✓ Resolve succeeded for flag without materialization requirements")
	})

	t.Run("Provider returns missing materializations error message", func(t *testing.T) {
		// Create state with a flag that requires materializations
		stickyState := createStateWithStickyFlag()
		accountId := "test-account"

		flagLogger := NewNoOpWasmFlagLogger()
		swap, err := NewSwapWasmResolverApi(ctx, runtime, defaultWasmBytes, flagLogger, stickyState, accountId)
		if err != nil {
			t.Fatalf("Failed to create SwapWasmResolverApi: %v", err)
		}
		defer swap.Close(ctx)

		factory := &LocalResolverFactory{
			resolverAPI: swap,
		}

		provider := NewLocalResolverProvider(factory, "test-secret")

		evalCtx := openfeature.FlattenedContext{
			"user_id": "test-user-123",
		}

		// Try to evaluate a flag that requires materializations without providing them
		defaultValue := false
		result := provider.BooleanEvaluation(ctx, "sticky-test-flag.enabled", defaultValue, evalCtx)

		// Should return the default value
		if result.Value != defaultValue {
			t.Errorf("Expected default value %v when materializations missing, got %v", defaultValue, result.Value)
		}

		// Should have ErrorReason
		if result.Reason != openfeature.ErrorReason {
			t.Errorf("Expected ErrorReason when materializations missing, got %v", result.Reason)
		}

		// Should have an error with the exact message "missing materializations"
		if result.ResolutionError.Error() == "" {
			t.Error("Expected ResolutionError when materializations missing")
		}

		expectedErrorMsg := "missing materializations"
		if result.ResolutionError.Error() != "GENERAL: missing materializations" {
			t.Errorf("Expected error message 'GENERAL: %s', got: %v", expectedErrorMsg, result.ResolutionError)
		}

		t.Logf("✓ Provider correctly returned default value with 'missing materializations' error message")
	})
}
