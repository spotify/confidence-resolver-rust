package confidence

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	adminv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/flags/admin/v1"
	iamv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/iam/v1"
	"github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/resolver"
	"github.com/tetratelabs/wazero"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, nil))
}

func loadTestResolverState(t *testing.T) []byte {
	dataPath := filepath.Join("..", "..", "..", "data", "resolver_state_current.pb")
	data, err := os.ReadFile(dataPath)
	if err != nil {
		t.Skipf("Skipping test - could not load test resolver state: %v", err)
	}
	return data
}

func loadTestAccountID(t *testing.T) string {
	dataPath := filepath.Join("..", "..", "..", "data", "account_id")
	data, err := os.ReadFile(dataPath)
	if err != nil {
		t.Skipf("Skipping test - could not load test account ID: %v", err)
	}
	return strings.TrimSpace(string(data))
}

// Helper function to create minimal valid resolver state for testing
func createMinimalResolverState() []byte {
	clientName := "clients/test-client"
	credentialName := "clients/test-client/credentials/test-credential"

	state := &adminv1.ResolverState{
		Flags: []*adminv1.Flag{},
		Clients: []*iamv1.Client{
			{
				Name: clientName,
			},
		},
		ClientCredentials: []*iamv1.ClientCredential{
			{
				Name: credentialName, // Must start with client name
				Credential: &iamv1.ClientCredential_ClientSecret_{
					ClientSecret: &iamv1.ClientCredential_ClientSecret{
						Secret: "test-secret",
					},
				},
			},
		},
	}
	data, err := proto.Marshal(state)
	if err != nil {
		panic("Failed to create minimal state: " + err.Error())
	}
	return data
}

// Helper to create a resolver state with a flag that requires materializations
func createStateWithStickyFlag() []byte {
	state := &adminv1.ResolverState{
		Flags: []*adminv1.Flag{
			{
				Name: "flags/sticky-test-flag",
				Variants: []*adminv1.Flag_Variant{
					{
						Name: "flags/sticky-test-flag/variants/on",
						Value: &structpb.Struct{
							Fields: map[string]*structpb.Value{
								"enabled": structpb.NewBoolValue(true),
							},
						},
					},
					{
						Name: "flags/sticky-test-flag/variants/off",
						Value: &structpb.Struct{
							Fields: map[string]*structpb.Value{
								"enabled": structpb.NewBoolValue(false),
							},
						},
					},
				},
				State: adminv1.Flag_ACTIVE,
				// Associate this flag with the test client
				Clients: []string{"clients/test-client"},
				Rules: []*adminv1.Flag_Rule{
					{
						Name:                 "flags/sticky-test-flag/rules/sticky-rule",
						Segment:              "segments/always-true",
						TargetingKeySelector: "user_id",
						Enabled:              true,
						AssignmentSpec: &adminv1.Flag_Rule_AssignmentSpec{
							BucketCount: 10000,
							Assignments: []*adminv1.Flag_Rule_Assignment{
								{
									AssignmentId: "variant-assignment",
									Assignment: &adminv1.Flag_Rule_Assignment_Variant{
										Variant: &adminv1.Flag_Rule_Assignment_VariantAssignment{
											Variant: "flags/sticky-test-flag/variants/on",
										},
									},
									BucketRanges: []*adminv1.Flag_Rule_BucketRange{
										{
											Upper: 10000,
										},
									},
								},
							},
						},
						// This rule requires a materialization named "experiment_v1"
						MaterializationSpec: &adminv1.Flag_Rule_MaterializationSpec{
							ReadMaterialization:  "experiment_v1",
							WriteMaterialization: "experiment_v1",
							Mode: &adminv1.Flag_Rule_MaterializationSpec_MaterializationReadMode{
								MaterializationMustMatch:     false,
								SegmentTargetingCanBeIgnored: false,
							},
						},
					},
				},
			},
		},
		SegmentsNoBitsets: []*adminv1.Segment{
			{
				Name: "segments/always-true",
				// Empty segment - may not match any users
			},
		},
		Clients: []*iamv1.Client{
			{
				Name: "clients/test-client",
			},
		},
		ClientCredentials: []*iamv1.ClientCredential{
			{
				// ClientCredential name must start with the client name
				Name: "clients/test-client/credentials/test-credential",
				Credential: &iamv1.ClientCredential_ClientSecret_{
					ClientSecret: &iamv1.ClientCredential_ClientSecret{
						Secret: "test-secret",
					},
				},
			},
		},
	}
	data, err := proto.Marshal(state)
	if err != nil {
		panic("Failed to create state with sticky flag: " + err.Error())
	}
	return data
}

// Helper function to create a ResolveWithStickyRequest
func createResolveWithStickyRequest(
	resolveRequest *resolver.ResolveFlagsRequest,
	materializations map[string]*resolver.MaterializationMap,
	failFast bool,
	notProcessSticky bool,
) *resolver.ResolveWithStickyRequest {
	if materializations == nil {
		materializations = make(map[string]*resolver.MaterializationMap)
	}
	return &resolver.ResolveWithStickyRequest{
		ResolveRequest:          resolveRequest,
		MaterializationsPerUnit: materializations,
		FailFastOnSticky:        failFast,
		NotProcessSticky:        notProcessSticky,
	}
}

// Helper function to create a tutorial-feature resolve request with standard test data
func createTutorialFeatureRequest() *resolver.ResolveFlagsRequest {
	return &resolver.ResolveFlagsRequest{
		Flags:        []string{"flags/tutorial-feature"},
		Apply:        true,
		ClientSecret: "mkjJruAATQWjeY7foFIWfVAcBWnci2YF",
		EvaluationContext: &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"visitor_id": structpb.NewStringValue("tutorial_visitor"),
			},
		},
	}
}

func TestSwapWasmResolverApi_NewSwapWasmResolverApi(t *testing.T) {
	ctx := context.Background()
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig())
	defer runtime.Close(ctx)

	flagLogger := NewNoOpWasmFlagLogger()
	initialState := createMinimalResolverState()
	accountId := "test-account"

	swap, err := NewSwapWasmResolverApi(ctx, runtime, defaultWasmBytes, flagLogger, testLogger(), nil)
	if err != nil {
		t.Fatalf("Failed to create SwapWasmResolverApi: %v", err)
	}
	defer swap.Close(ctx)

	// Initialize with test state
	if err := swap.UpdateStateAndFlushLogs(initialState, accountId); err != nil {
		t.Fatalf("Failed to initialize swap with state: %v", err)
	}

	if swap == nil {
		t.Fatal("Expected non-nil SwapWasmResolverApi")
	}

	if swap.runtime == nil {
		t.Error("Expected runtime to be set")
	}

	if swap.compiledModule == nil {
		t.Error("Expected compiled module to be set")
	}

	if swap.flagLogger == nil {
		t.Error("Expected flag logger to be set")
	}
}

func TestSwapWasmResolverApi_NewSwapWasmResolverApi_InvalidWasm(t *testing.T) {
	ctx := context.Background()
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig())
	defer runtime.Close(ctx)

	flagLogger := NewNoOpWasmFlagLogger()

	// Use invalid WASM bytes
	invalidWasmBytes := []byte("not valid wasm")

	_, err := NewSwapWasmResolverApi(ctx, runtime, invalidWasmBytes, flagLogger, testLogger(), nil)
	if err == nil {
		t.Fatal("Expected error when creating SwapWasmResolverApi with invalid WASM")
	}
}

func TestSwapWasmResolverApi_WithRealState(t *testing.T) {
	ctx := context.Background()
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig())
	defer runtime.Close(ctx)

	flagLogger := NewNoOpWasmFlagLogger()

	// Load real test state from data directory
	testState := loadTestResolverState(t)
	testAcctID := loadTestAccountID(t)

	swap, err := NewSwapWasmResolverApi(ctx, runtime, defaultWasmBytes, flagLogger, testLogger(), nil)
	if err != nil {
		t.Fatalf("Failed to create SwapWasmResolverApi with real state: %v", err)
	}
	defer swap.Close(ctx)

	// Initialize with test state
	if err := swap.UpdateStateAndFlushLogs(testState, testAcctID); err != nil {
		t.Fatalf("Failed to initialize swap with state: %v", err)
	}

	request := createResolveWithStickyRequest(
		createTutorialFeatureRequest(),
		nil,   // empty materializations
		true,  // failFast
		false, // notProcessSticky
	)

	response, err := swap.ResolveWithSticky(ctx, request)
	if err != nil {
		t.Fatalf("Unexpected error resolving tutorial-feature flag: %v", err)
	}

	if response == nil {
		t.Fatal("Expected non-nil response")
	}

	if len(response.ResolvedFlags) != 1 {
		t.Fatalf("Expected 1 resolved flag, got %d", len(response.ResolvedFlags))
	}

	resolvedFlag := response.ResolvedFlags[0]

	// Verify the exact flag name
	if resolvedFlag.Flag != "flags/tutorial-feature" {
		t.Errorf("Expected flag 'flags/tutorial-feature', got '%s'", resolvedFlag.Flag)
	}

	// Verify the exact variant
	// The tutorial-visitor segment should resolve to the exciting-welcome variant
	expectedVariant := "flags/tutorial-feature/variants/exciting-welcome"
	if resolvedFlag.Variant != expectedVariant {
		t.Errorf("Expected variant '%s', got '%s'", expectedVariant, resolvedFlag.Variant)
	}

	// Verify the reason is MATCH (successful targeting match)
	if resolvedFlag.Reason.String() != "RESOLVE_REASON_MATCH" {
		t.Errorf("Expected reason RESOLVE_REASON_MATCH, got %v", resolvedFlag.Reason)
	}

	// Verify the resolved value has the expected structure and content
	if resolvedFlag.Value == nil {
		t.Fatal("Expected non-nil value in resolved flag")
	}

	fields := resolvedFlag.Value.GetFields()
	if fields == nil {
		t.Fatal("Expected fields in resolved value")
	}

	// Verify the exact message value from the exciting-welcome variant
	expectedMessage := "We are very excited to welcome you to Confidence! This is a message from the tutorial flag."
	messageValue, hasMessage := fields["message"]
	if !hasMessage {
		t.Error("Expected 'message' field in resolved value")
	} else if messageValue.GetStringValue() != expectedMessage {
		t.Errorf("Expected message '%s', got '%s'", expectedMessage, messageValue.GetStringValue())
	}

	// Verify the exact title value from the exciting-welcome variant
	expectedTitle := "Welcome to Confidence!"
	titleValue, hasTitle := fields["title"]
	if !hasTitle {
		t.Error("Expected 'title' field in resolved value")
	} else if titleValue.GetStringValue() != expectedTitle {
		t.Errorf("Expected title '%s', got '%s'", expectedTitle, titleValue.GetStringValue())
	}

	t.Logf("✓ Successfully resolved flag with correct variant and values")
}

func TestSwapWasmResolverApi_UpdateStateAndFlushLogs(t *testing.T) {
	ctx := context.Background()
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig())
	defer runtime.Close(ctx)

	flagLogger := NewNoOpWasmFlagLogger()

	// Load real test state
	initialState := loadTestResolverState(t)
	accountId := loadTestAccountID(t)

	swap, err := NewSwapWasmResolverApi(ctx, runtime, defaultWasmBytes, flagLogger, testLogger(), nil)
	if err != nil {
		t.Fatalf("Failed to create SwapWasmResolverApi: %v", err)
	}
	defer swap.Close(ctx)

	// Initialize with test state
	if err := swap.UpdateStateAndFlushLogs(initialState, accountId); err != nil {
		t.Fatalf("Failed to initialize swap with state: %v", err)
	}

	// Update with new state - the key test is that UpdateStateAndFlushLogs succeeds
	newState := loadTestResolverState(t)
	err = swap.UpdateStateAndFlushLogs(newState, accountId)
	if err != nil {
		t.Fatalf("UpdateStateAndFlushLogs failed: %v", err)
	}

	// Verify that we can successfully resolve after the state update
	request := createResolveWithStickyRequest(
		createTutorialFeatureRequest(),
		nil,   // empty materializations
		true,  // failFast
		false, // notProcessSticky
	)

	response, err := swap.ResolveWithSticky(ctx, request)
	if err != nil {
		t.Fatalf("Resolve failed after update: %v", err)
	}

	if response == nil {
		t.Fatal("Expected successful resolve response")
	}

	// Verify we got the expected resolution
	if len(response.ResolvedFlags) != 1 {
		t.Errorf("Expected 1 resolved flag, got %d", len(response.ResolvedFlags))
	}

	if response.ResolvedFlags[0].Variant != "flags/tutorial-feature/variants/exciting-welcome" {
		t.Errorf("Expected exciting-welcome variant, got %s", response.ResolvedFlags[0].Variant)
	}

	t.Logf("✓ State update successful and flag resolution works correctly")
}

func TestSwapWasmResolverApi_MultipleUpdates(t *testing.T) {
	ctx := context.Background()
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig())
	defer runtime.Close(ctx)

	flagLogger := NewNoOpWasmFlagLogger()

	// Load real test state
	initialState := loadTestResolverState(t)
	accountId := loadTestAccountID(t)

	swap, err := NewSwapWasmResolverApi(ctx, runtime, defaultWasmBytes, flagLogger, testLogger(), nil)
	if err != nil {
		t.Fatalf("Failed to create SwapWasmResolverApi: %v", err)
	}
	defer swap.Close(ctx)

	// Initialize with test state
	if err := swap.UpdateStateAndFlushLogs(initialState, accountId); err != nil {
		t.Fatalf("Failed to initialize swap with state: %v", err)
	}

	// Perform multiple state updates to verify the swap mechanism works correctly
	for i := 0; i < 3; i++ {
		newState := loadTestResolverState(t)
		err := swap.UpdateStateAndFlushLogs(newState, accountId)
		if err != nil {
			t.Fatalf("Update %d failed: %v", i, err)
		}

		// Verify that Resolve successfully works after each update
		request := createResolveWithStickyRequest(
			createTutorialFeatureRequest(),
			nil,   // empty materializations
			true,  // failFast
			false, // notProcessSticky
		)

		response, resolveErr := swap.ResolveWithSticky(ctx, request)
		if resolveErr != nil {
			t.Fatalf("Update %d: Resolve failed: %v", i, resolveErr)
		}

		if response == nil {
			t.Fatalf("Update %d: Expected successful resolve response", i)
		}

		// Verify we got the expected variant after each swap
		if len(response.ResolvedFlags) != 1 {
			t.Errorf("Update %d: Expected 1 resolved flag, got %d", i, len(response.ResolvedFlags))
		} else if response.ResolvedFlags[0].Variant != "flags/tutorial-feature/variants/exciting-welcome" {
			t.Errorf("Update %d: Expected exciting-welcome variant, got %s", i, response.ResolvedFlags[0].Variant)
		}

		t.Logf("Update %d: ✓ Swap successful, flag resolves correctly", i)
	}
}

func TestSwapWasmResolverApi_Close(t *testing.T) {
	ctx := context.Background()
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig())
	defer runtime.Close(ctx)

	flagLogger := NewNoOpWasmFlagLogger()
	initialState := createMinimalResolverState()
	accountId := "test-account"

	swap, err := NewSwapWasmResolverApi(ctx, runtime, defaultWasmBytes, flagLogger, testLogger(), nil)
	if err != nil {
		t.Fatalf("Failed to create SwapWasmResolverApi: %v", err)
	}

	// Initialize with test state
	if err := swap.UpdateStateAndFlushLogs(initialState, accountId); err != nil {
		t.Fatalf("Failed to initialize swap with state: %v", err)
	}

	// Close should not panic
	swap.Close(ctx)

	// Note: Closing again may cause issues with WASM module, so we don't test double-close
}

func TestErrInstanceClosed(t *testing.T) {
	err := ErrInstanceClosed
	if err.Error() != "WASM instance is closed or being replaced" {
		t.Errorf("Unexpected error message: %s", err.Error())
	}

	// Test that errors.Is works with it
	testErr := ErrInstanceClosed
	if !errors.Is(testErr, ErrInstanceClosed) {
		t.Error("Expected errors.Is to work with ErrInstanceClosed")
	}
}

// State from data sample, flag without sticky rules
func TestSwapWasmResolverApi_ResolveFlagWithNoStickyRules(t *testing.T) {
	ctx := context.Background()
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig())
	defer runtime.Close(ctx)

	flagLogger := NewNoOpWasmFlagLogger()
	testState := loadTestResolverState(t)
	testAcctID := loadTestAccountID(t)

	wasmResolver, err := NewSwapWasmResolverApi(ctx, runtime, defaultWasmBytes, flagLogger, testLogger(), nil)
	if err != nil {
		t.Fatalf("Failed to create SwapWasmResolverApi with sample state: %v", err)
	}
	defer wasmResolver.Close(ctx)

	// Initialize with test state
	if err := wasmResolver.UpdateStateAndFlushLogs(testState, testAcctID); err != nil {
		t.Fatalf("Failed to initialize swap with state: %v", err)
	}

	stickyRequest := createResolveWithStickyRequest(
		createTutorialFeatureRequest(),
		nil,   // empty materializations
		true,  // failFast
		false, // notProcessSticky
	)

	response, err := wasmResolver.ResolveWithSticky(ctx, stickyRequest)
	if err != nil {
		t.Fatalf("Unexpected error resolving tutorial-feature flag with sticky: %v", err)
	}

	if response == nil {
		t.Fatal("Expected non-nil response")
	}

	if len(response.ResolvedFlags) != 1 {
		t.Fatalf("Expected 1 resolved flag, got %d", len(response.ResolvedFlags))
	}

	resolvedFlag := response.ResolvedFlags[0]

	if resolvedFlag.Flag != "flags/tutorial-feature" {
		t.Errorf("Expected flag 'flags/tutorial-feature', got '%s'", resolvedFlag.Flag)
	}

	expectedVariant := "flags/tutorial-feature/variants/exciting-welcome"
	if resolvedFlag.Variant != expectedVariant {
		t.Errorf("Expected variant '%s', got '%s'", expectedVariant, resolvedFlag.Variant)
	}

	if resolvedFlag.Reason.String() != "RESOLVE_REASON_MATCH" {
		t.Errorf("Expected reason RESOLVE_REASON_MATCH, got %v", resolvedFlag.Reason)
	}

	if resolvedFlag.Value == nil {
		t.Fatal("Expected non-nil value in resolved flag")
	}

	fields := resolvedFlag.Value.GetFields()
	if fields == nil {
		t.Fatal("Expected fields in resolved value")
	}

	expectedMessage := "We are very excited to welcome you to Confidence! This is a message from the tutorial flag."
	messageValue, hasMessage := fields["message"]
	if !hasMessage {
		t.Error("Expected 'message' field in resolved value")
	} else if messageValue.GetStringValue() != expectedMessage {
		t.Errorf("Expected message '%s', got '%s'", expectedMessage, messageValue.GetStringValue())
	}

	expectedTitle := "Welcome to Confidence!"
	titleValue, hasTitle := fields["title"]
	if !hasTitle {
		t.Error("Expected 'title' field in resolved value")
	} else if titleValue.GetStringValue() != expectedTitle {
		t.Errorf("Expected title '%s', got '%s'", expectedTitle, titleValue.GetStringValue())
	}
}

func TestSwapWasmResolverApi_ResolveFlagWithStickyRules_MissingMaterializations(t *testing.T) {
	ctx := context.Background()
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig())
	defer runtime.Close(ctx)

	flagLogger := NewNoOpWasmFlagLogger()
	stickyState := createStateWithStickyFlag()
	accountId := "test-account"

	// No sticky strategy configured - should return error for missing materializations
	swap, err := NewSwapWasmResolverApi(ctx, runtime, defaultWasmBytes, flagLogger, testLogger(), nil)
	if err != nil {
		t.Fatalf("Failed to create SwapWasmResolverApi: %v", err)
	}
	defer swap.Close(ctx)

	// Initialize with test state
	if err := swap.UpdateStateAndFlushLogs(stickyState, accountId); err != nil {
		t.Fatalf("Failed to initialize swap with state: %v", err)
	}

	stickyRequest := createResolveWithStickyRequest(
		&resolver.ResolveFlagsRequest{
			Flags:        []string{"flags/sticky-test-flag"},
			Apply:        true,
			ClientSecret: "test-secret",
			EvaluationContext: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"user_id": structpb.NewStringValue("test-user-123"),
				},
			},
		},
		nil,   // empty materializations - missing the required "experiment_v1" materialization
		true,  // failFast
		false, // notProcessSticky
	)

	// With no sticky strategy configured, should return an error when materializations are missing
	_, err = swap.ResolveWithSticky(ctx, stickyRequest)
	if err == nil {
		t.Fatal("Expected error for missing materializations with no sticky strategy")
	}

	// Verify the error message mentions missing materializations
	if !strings.Contains(err.Error(), "missing materializations") {
		t.Errorf("Expected error message to mention missing materializations, got: %v", err)
	}
}
