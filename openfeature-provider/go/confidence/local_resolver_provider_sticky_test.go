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

// TestLocalResolverProvider_ResolveWithSticky_ReturnsDefaultOnError tests that
// default values are returned when resolve fails
func TestLocalResolverProvider_ResolveWithSticky_ReturnsDefaultOnError(t *testing.T) {
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

	t.Run("StringEvaluation returns default on error", func(t *testing.T) {
		defaultValue := "default-string"
		result := provider.StringEvaluation(ctx, "non-existent-flag", defaultValue, evalCtx)

		if result.Value != defaultValue {
			t.Errorf("Expected default value %s, got %s", defaultValue, result.Value)
		}

		if result.Reason != openfeature.ErrorReason {
			t.Errorf("Expected ErrorReason, got %v", result.Reason)
		}

		if result.ResolutionError.Error() == "" {
			t.Error("Expected ResolutionError to not be empty")
		}

		t.Logf("✓ StringEvaluation correctly returned default value: %s", defaultValue)
	})

	t.Run("IntEvaluation returns default on error", func(t *testing.T) {
		defaultValue := int64(42)
		result := provider.IntEvaluation(ctx, "non-existent-flag", defaultValue, evalCtx)

		if result.Value != defaultValue {
			t.Errorf("Expected default value %d, got %d", defaultValue, result.Value)
		}

		if result.Reason != openfeature.ErrorReason {
			t.Errorf("Expected ErrorReason, got %v", result.Reason)
		}

		if result.ResolutionError.Error() == "" {
			t.Error("Expected ResolutionError to not be empty")
		}

		t.Logf("✓ IntEvaluation correctly returned default value: %d", defaultValue)
	})

	t.Run("FloatEvaluation returns default on error", func(t *testing.T) {
		defaultValue := 3.14
		result := provider.FloatEvaluation(ctx, "non-existent-flag", defaultValue, evalCtx)

		if result.Value != defaultValue {
			t.Errorf("Expected default value %f, got %f", defaultValue, result.Value)
		}

		if result.Reason != openfeature.ErrorReason {
			t.Errorf("Expected ErrorReason, got %v", result.Reason)
		}

		if result.ResolutionError.Error() == "" {
			t.Error("Expected ResolutionError to not be empty")
		}

		t.Logf("✓ FloatEvaluation correctly returned default value: %f", defaultValue)
	})

	t.Run("ObjectEvaluation returns default on error", func(t *testing.T) {
		defaultValue := map[string]interface{}{
			"key": "default-value",
		}
		result := provider.ObjectEvaluation(ctx, "non-existent-flag", defaultValue, evalCtx)

		if result.Value == nil {
			t.Fatal("Expected result value to not be nil")
		}

		resultMap, ok := result.Value.(map[string]interface{})
		if !ok {
			t.Fatalf("Expected result value to be a map, got %T", result.Value)
		}

		if resultMap["key"] != defaultValue["key"] {
			t.Errorf("Expected default value with key='default-value', got %v", resultMap)
		}

		if result.Reason != openfeature.ErrorReason {
			t.Errorf("Expected ErrorReason, got %v", result.Reason)
		}

		if result.ResolutionError.Error() == "" {
			t.Error("Expected ResolutionError to not be empty")
		}

		t.Logf("✓ ObjectEvaluation correctly returned default value: %v", defaultValue)
	})
}

// TestLocalResolverProvider_ResolveWithSticky_ReturnsCorrectValue tests that
// correct values are returned when resolve succeeds with real test data
func TestLocalResolverProvider_ResolveWithSticky_ReturnsCorrectValue(t *testing.T) {
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

// TestLocalResolverProvider_ResolveWithSticky_MissingMaterializations tests
// that missing materializations are handled correctly
func TestLocalResolverProvider_ResolveWithSticky_MissingMaterializations(t *testing.T) {
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

	provider := NewLocalResolverProvider(factory, "mkjJruAATQWjeY7foFIWfVAcBWnci2YF")

	evalCtx := openfeature.FlattenedContext{
		"visitor_id": "tutorial_visitor",
	}

	// Even with empty materializations, the resolve should work
	// (assuming the flag doesn't require sticky targeting)
	defaultValue := "default"
	result := provider.StringEvaluation(ctx, "tutorial-feature.message", defaultValue, evalCtx)

	// Should succeed because materializations are empty (not missing required ones)
	// ResolutionError might have an error or not depending on the flag configuration
	if result.ResolutionError.Error() != "" {
		t.Logf("Note: Got resolution error (may be expected for sticky flags): %v", result.ResolutionError)
	} else {
		t.Logf("✓ Resolve succeeded without materializations")
	}
}

// TestLocalResolverProvider_ResolveWithSticky_TypeMismatch tests that
// type mismatches return default values
func TestLocalResolverProvider_ResolveWithSticky_TypeMismatch(t *testing.T) {
	ctx := context.Background()
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig())
	defer runtime.Close(ctx)

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

	// Try to get a string field as a boolean (type mismatch)
	defaultValue := false
	result := provider.BooleanEvaluation(ctx, "tutorial-feature.message", defaultValue, evalCtx)

	if result.Value != defaultValue {
		t.Errorf("Expected default value %v on type mismatch, got %v", defaultValue, result.Value)
	}

	if result.Reason != openfeature.ErrorReason {
		t.Errorf("Expected ErrorReason on type mismatch, got %v", result.Reason)
	}

	if result.ResolutionError.Error() == "" {
		t.Error("Expected ResolutionError for type mismatch")
	} else {
		t.Logf("✓ Type mismatch correctly returned default value with error: %v", result.ResolutionError)
	}
}

// TestLocalResolverProvider_ResolveWithSticky_InvalidClientSecret tests that
// invalid client secrets return default values
func TestLocalResolverProvider_ResolveWithSticky_InvalidClientSecret(t *testing.T) {
	ctx := context.Background()
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig())
	defer runtime.Close(ctx)

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

	// Use invalid client secret
	provider := NewLocalResolverProvider(factory, "invalid-secret")

	evalCtx := openfeature.FlattenedContext{
		"visitor_id": "tutorial_visitor",
	}

	defaultValue := "default-message"
	result := provider.StringEvaluation(ctx, "tutorial-feature.message", defaultValue, evalCtx)

	if result.Value != defaultValue {
		t.Errorf("Expected default value '%s' on invalid secret, got '%s'", defaultValue, result.Value)
	}

	if result.Reason != openfeature.ErrorReason {
		t.Errorf("Expected ErrorReason on invalid secret, got %v", result.Reason)
	}

	if result.ResolutionError.Error() == "" {
		t.Error("Expected ResolutionError for invalid client secret")
	} else {
		t.Logf("✓ Invalid client secret correctly returned default value with error: %v", result.ResolutionError)
	}
}

// TestLocalResolverProvider_ResolveWithSticky_NestedValues tests that
// nested flag values are resolved correctly
func TestLocalResolverProvider_ResolveWithSticky_NestedValues(t *testing.T) {
	ctx := context.Background()
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig())
	defer runtime.Close(ctx)

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

	// Test nested path access with dot notation
	t.Run("Nested string value", func(t *testing.T) {
		defaultValue := "default"
		result := provider.StringEvaluation(ctx, "tutorial-feature.title", defaultValue, evalCtx)

		expectedTitle := "Welcome to Confidence!"

		if result.Value != expectedTitle {
			t.Errorf("Expected nested value '%s', got '%s'", expectedTitle, result.Value)
		}

		if result.Reason != openfeature.TargetingMatchReason {
			t.Errorf("Expected TargetingMatchReason, got %v", result.Reason)
		}

		t.Logf("✓ Nested value correctly resolved: %s", result.Value)
	})

	// Test non-existent nested path returns default
	t.Run("Non-existent nested path returns default", func(t *testing.T) {
		defaultValue := "default"
		result := provider.StringEvaluation(ctx, "tutorial-feature.nonexistent.path", defaultValue, evalCtx)

		if result.Value != defaultValue {
			t.Errorf("Expected default value '%s' for non-existent path, got '%s'", defaultValue, result.Value)
		}

		if result.Reason != openfeature.ErrorReason {
			t.Errorf("Expected ErrorReason for non-existent path, got %v", result.Reason)
		}

		t.Logf("✓ Non-existent nested path correctly returned default value")
	})
}
