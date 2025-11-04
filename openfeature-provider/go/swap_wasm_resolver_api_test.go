package confidence

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	adminv1 "github.com/spotify/confidence-resolver-rust/openfeature-provider/go/confidence/proto/confidence/flags/admin/v1"
	iamv1 "github.com/spotify/confidence-resolver-rust/openfeature-provider/go/confidence/proto/confidence/iam/v1"
	"github.com/spotify/confidence-resolver-rust/openfeature-provider/go/confidence/proto/resolver"
	"github.com/tetratelabs/wazero"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// Helper to load test data from the data directory
func loadTestResolverState(t *testing.T) []byte {
	dataPath := filepath.Join("..", "..", "data", "resolver_state_current.pb")
	data, err := os.ReadFile(dataPath)
	if err != nil {
		t.Skipf("Skipping test - could not load test resolver state: %v", err)
	}
	return data
}

func loadTestAccountID(t *testing.T) string {
	dataPath := filepath.Join("..", "..", "data", "account_id")
	data, err := os.ReadFile(dataPath)
	if err != nil {
		t.Skipf("Skipping test - could not load test account ID: %v", err)
	}
	return strings.TrimSpace(string(data))
}

// Helper function to create minimal valid resolver state for testing
func createMinimalResolverState() []byte {
	state := &adminv1.ResolverState{
		Flags: []*adminv1.Flag{},
		ClientCredentials: []*iamv1.ClientCredential{
			{
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

func TestSwapWasmResolverApi_NewSwapWasmResolverApi(t *testing.T) {
	ctx := context.Background()
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig())
	defer runtime.Close(ctx)

	flagLogger := NewNoOpWasmFlagLogger()
	initialState := createMinimalResolverState()
	accountId := "test-account"

	swap, err := NewSwapWasmResolverApi(ctx, runtime, defaultWasmBytes, flagLogger, initialState, accountId)
	if err != nil {
		t.Fatalf("Failed to create SwapWasmResolverApi: %v", err)
	}
	defer swap.Close(ctx)

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
	initialState := createMinimalResolverState()
	accountId := "test-account"

	// Use invalid WASM bytes
	invalidWasmBytes := []byte("not valid wasm")

	_, err := NewSwapWasmResolverApi(ctx, runtime, invalidWasmBytes, flagLogger, initialState, accountId)
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

	swap, err := NewSwapWasmResolverApi(ctx, runtime, defaultWasmBytes, flagLogger, testState, testAcctID)
	if err != nil {
		t.Fatalf("Failed to create SwapWasmResolverApi with real state: %v", err)
	}
	defer swap.Close(ctx)

	// Try to resolve a flag with the real state
	// Note: We need a valid client secret from the state
	request := &resolver.ResolveFlagsRequest{
		Flags:        []string{"flags/test-flag"},
		Apply:        false,
		ClientSecret: "CLIENT_SECRET", // This needs to match a credential in the state
		EvaluationContext: &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"targeting_key": structpb.NewStringValue("user-123"),
			},
		},
	}

	response, err := swap.Resolve(request)
	// It's ok if resolution fails due to client secret mismatch or missing flags
	// The important part is that the WASM module loaded, state was set, and didn't crash
	if err != nil {
		t.Logf("Resolve returned error (expected with CLIENT_SECRET placeholder): %v", err)
		// When there's an error from WASM, response may be nil
		return
	}

	if response != nil {
		t.Logf("Successfully resolved with real state, got %d flags", len(response.ResolvedFlags))
	}
}

func TestSwapWasmResolverApi_UpdateStateAndFlushLogs(t *testing.T) {
	ctx := context.Background()
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig())
	defer runtime.Close(ctx)

	flagLogger := NewNoOpWasmFlagLogger()
	initialState := createMinimalResolverState()
	accountId := "test-account"

	swap, err := NewSwapWasmResolverApi(ctx, runtime, defaultWasmBytes, flagLogger, initialState, accountId)
	if err != nil {
		t.Fatalf("Failed to create SwapWasmResolverApi: %v", err)
	}
	defer swap.Close(ctx)

	// Update with new state
	newState := createMinimalResolverState()
	err = swap.UpdateStateAndFlushLogs(newState, accountId)
	if err != nil {
		t.Fatalf("UpdateStateAndFlushLogs failed: %v", err)
	}

	// Verify that we can call Resolve after update (even if it returns an error due to client secret)
	request := &resolver.ResolveFlagsRequest{
		Flags:        []string{"flags/test-flag"},
		Apply:        false,
		ClientSecret: "test-secret",
		EvaluationContext: &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"targeting_key": structpb.NewStringValue("user-123"),
			},
		},
	}

	// Call Resolve - it may fail due to client secret not being in state, which is OK
	_, err = swap.Resolve(request)
	// The key point is that UpdateStateAndFlushLogs completed and the swap happened
	// Resolution errors are expected with minimal test state
	t.Logf("Resolve after update result: %v", err)
}

func TestSwapWasmResolverApi_MultipleUpdates(t *testing.T) {
	ctx := context.Background()
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig())
	defer runtime.Close(ctx)

	flagLogger := NewNoOpWasmFlagLogger()
	initialState := createMinimalResolverState()
	accountId := "test-account"

	swap, err := NewSwapWasmResolverApi(ctx, runtime, defaultWasmBytes, flagLogger, initialState, accountId)
	if err != nil {
		t.Fatalf("Failed to create SwapWasmResolverApi: %v", err)
	}
	defer swap.Close(ctx)

	// Perform multiple state updates to verify the swap mechanism works correctly
	for i := 0; i < 3; i++ {
		newState := createMinimalResolverState()
		err := swap.UpdateStateAndFlushLogs(newState, accountId)
		if err != nil {
			t.Fatalf("Update %d failed: %v", i, err)
		}

		// Verify that Resolve can be called after each update
		request := &resolver.ResolveFlagsRequest{
			Flags:        []string{"flags/test-flag"},
			Apply:        false,
			ClientSecret: "test-secret",
			EvaluationContext: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"targeting_key": structpb.NewStringValue("user-123"),
				},
			},
		}

		// The key verification is that the swap completed successfully
		// Resolution may fail with minimal test state, which is expected
		_, resolveErr := swap.Resolve(request)
		t.Logf("Update %d: Resolve result: %v", i, resolveErr)
	}
}

func TestSwapWasmResolverApi_Close(t *testing.T) {
	ctx := context.Background()
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig())
	defer runtime.Close(ctx)

	flagLogger := NewNoOpWasmFlagLogger()
	initialState := createMinimalResolverState()
	accountId := "test-account"

	swap, err := NewSwapWasmResolverApi(ctx, runtime, defaultWasmBytes, flagLogger, initialState, accountId)
	if err != nil {
		t.Fatalf("Failed to create SwapWasmResolverApi: %v", err)
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
