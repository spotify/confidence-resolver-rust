package confidence

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/open-feature/go-sdk/openfeature"
	messages "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto"
	adminv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/flags/admin/v1"
	iamv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/iam/v1"
	"google.golang.org/protobuf/proto"
)

func TestLocalResolverProvider_ReturnsDefaultOnError(t *testing.T) {
	ctx := context.Background()
	runtime := NewWasmResolverFactory(ctx, noopLogSink)
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

	swap := runtime.New()
	defer swap.Close(ctx)

	// Initialize with test state
	if err := swap.SetResolverState(&messages.SetResolverStateRequest{
		State:     stateBytes,
		AccountId: "test-account",
	}); err != nil {
		t.Fatalf("Failed to initialize swap with state: %v", err)
	}

	// Use different client secret that won't match
	openfeature.SetProviderAndWait(NewLocalResolverProvider(swap, nil, nil, "test-secret", slog.New(slog.NewTextHandler(os.Stderr, nil))))
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
	runtime := NewWasmResolverFactory(ctx, noopLogSink)
	defer runtime.Close(ctx)

	// Load real test state
	testState := loadTestResolverState(t)
	testAcctID := loadTestAccountID(t)

	swap := runtime.New()
	defer swap.Close(ctx)

	// Initialize with test state
	setStateRequest := &messages.SetResolverStateRequest{
		State:     testState,
		AccountId: testAcctID,
	}
	if err := swap.SetResolverState(setStateRequest); err != nil {
		t.Fatalf("Failed to initialize swap with state: %v", err)
	}

	// Use the correct client secret from test data
	openfeature.SetProviderAndWait(NewLocalResolverProvider(swap, nil, nil, "mkjJruAATQWjeY7foFIWfVAcBWnci2YF", slog.New(slog.NewTextHandler(os.Stderr, nil))))
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
		runtime := NewWasmResolverFactory(ctx, noopLogSink)
		defer runtime.Close(ctx)

		// Load real test state
		testState := loadTestResolverState(t)
		testAcctID := loadTestAccountID(t)

		swap := runtime.New()
		defer swap.Close(ctx)

		// Initialize with test state
		if err := swap.SetResolverState(&messages.SetResolverStateRequest{
			State:     testState,
			AccountId: testAcctID,
		}); err != nil {
			t.Fatalf("Failed to initialize swap with state: %v", err)
		}

		openfeature.SetProviderAndWait(NewLocalResolverProvider(swap, nil, nil, "mkjJruAATQWjeY7foFIWfVAcBWnci2YF", slog.New(slog.NewTextHandler(os.Stderr, nil))))
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
		runtime := NewWasmResolverFactory(ctx, noopLogSink)
		defer runtime.Close(ctx)

		// Create state with a flag that requires materializations
		stickyState := createStateWithStickyFlag()
		accountId := "test-account"

		swap := runtime.New()
		defer swap.Close(ctx)

		// Initialize with sticky state
		if err := swap.SetResolverState(&messages.SetResolverStateRequest{
			State:     stickyState,
			AccountId: accountId,
		}); err != nil {
			t.Fatalf("Failed to initialize swap with state: %v", err)
		}

		openfeature.SetProviderAndWait(NewLocalResolverProvider(swap, nil, nil, "test-secret", slog.New(slog.NewTextHandler(os.Stderr, nil))))
		client := openfeature.NewClient("test-client")

		evalCtx := openfeature.NewTargetlessEvaluationContext(map[string]interface{}{
			"user_id": "test-user-123",
		})

		defaultValue := false
		result, error := client.BooleanValueDetails(ctx, "sticky-test-flag.enabled", defaultValue, evalCtx)
		if error == nil {
			t.Error("Expected error when materializations missing, got nil")
		} else if error.Error() != "error code: GENERAL: missing materializations" {
			t.Errorf("Expected 'error code: GENERAL: missing materializations', got: %v", error.Error())
		}

		if result.Value != defaultValue {
			t.Errorf("Expected default value %v when materializations missing, got %v", defaultValue, result.Value)
		}

		if result.Reason != openfeature.ErrorReason {
			t.Errorf("Expected ErrorReason when materializations missing, got %v", result.Reason)
		}
	})
}
