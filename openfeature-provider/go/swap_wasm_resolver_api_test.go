package confidence

import (
	"errors"
	"testing"
)

// Note: SwapWasmResolverApi requires a real WASM runtime and ResolverApi instances
// which are complex to mock. The critical logic is tested through integration tests.
// These unit tests verify the error handling and basic structure.

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

