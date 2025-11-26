package confidence

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/open-feature/go-sdk/openfeature"
)

// End-to-end tests for LocalResolverProvider.
//
// These tests verify the provider against real Confidence service flags.
// The tests use the exact same client secret and flags as the Java E2E tests.

const flagClientSecret = "ti5Sipq5EluCYRG7I5cdbpWC3xq7JTWv"

var e2eClient openfeature.IClient

// setupE2E initializes the provider and OpenFeature API for E2E tests
func setupE2E(t *testing.T) {
	ctx := context.Background()

	// Create provider with NoOp logger to avoid "Unimplemented" gRPC errors
	// The production edge server doesn't implement ClientWriteFlagLogs endpoint
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	// Use custom provider setup with NoOp flag logger
	provider, err := createE2EProvider(ctx, flagClientSecret, logger)
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	// Set provider and wait for it to be ready
	err = openfeature.SetProviderAndWait(provider)
	if err != nil {
		t.Fatalf("Failed to set provider: %v", err)
	}

	// Set evaluation context with targeting key
	evalCtx := openfeature.NewEvaluationContext("test-a", map[string]interface{}{
		"sticky": false,
	})
	openfeature.SetEvaluationContext(evalCtx)

	e2eClient = openfeature.NewClient("e2e-test")
}

// createE2EProvider creates a provider for E2E testing
// Uses the real GrpcFlagLogger so you can see the logging flow,
// even though it will get "Unimplemented" errors from the production server
func createE2EProvider(ctx context.Context, clientSecret string, logger *slog.Logger) (*LocalResolverProvider, error) {
	// Create the real provider with all components to see the full logging flow
	// Note: This will attempt to send logs and get "Unimplemented" errors,
	// which is expected since the production edge server doesn't support log ingestion
	provider, err := NewProvider(ctx, ProviderConfig{
		ClientSecret: clientSecret,
		Logger:       logger,
	})
	if err != nil {
		return nil, err
	}

	return provider, nil
}

// teardownE2E cleans up after E2E tests
func teardownE2E(t *testing.T) {
	openfeature.Shutdown()
}

func TestE2E_ShouldResolveBoolean(t *testing.T) {
	setupE2E(t)
	defer teardownE2E(t)

	ctx := context.Background()

	// Keep test alive for 20 seconds, resolve every second
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	timeout := time.After(20 * time.Second)

	resolveCount := 0
	for {
		select {
		case <-timeout:
			t.Logf("Test completed after %d resolves", resolveCount)
			return
		case <-ticker.C:
			value, err := e2eClient.BooleanValue(ctx, "web-sdk-e2e-flag.bool", true, openfeature.EvaluationContext{})
			if err != nil {
				t.Fatalf("Failed to resolve boolean flag: %v", err)
			}

			if value != false {
				t.Errorf("Expected false, got %v", value)
			}
			resolveCount++
			t.Logf("Resolve #%d: value=%v", resolveCount, value)
		}
	}
}

func TestE2E_ShouldResolveInt(t *testing.T) {
	setupE2E(t)
	defer teardownE2E(t)

	ctx := context.Background()

	// Keep test alive for 20 seconds, resolve every second
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	timeout := time.After(20 * time.Second)

	resolveCount := 0
	for {
		select {
		case <-timeout:
			t.Logf("Test completed after %d resolves", resolveCount)
			return
		case <-ticker.C:
			value, err := e2eClient.IntValue(ctx, "web-sdk-e2e-flag.int", 10, openfeature.EvaluationContext{})
			if err != nil {
				t.Fatalf("Failed to resolve int flag: %v", err)
			}

			if value != 3 {
				t.Errorf("Expected 3, got %v", value)
			}
			resolveCount++
			t.Logf("Resolve #%d: value=%v", resolveCount, value)
		}
	}
}

func TestE2E_ShouldResolveDouble(t *testing.T) {
	setupE2E(t)
	defer teardownE2E(t)

	ctx := context.Background()

	// Keep test alive for 20 seconds, resolve every second
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	timeout := time.After(20 * time.Second)

	resolveCount := 0
	for {
		select {
		case <-timeout:
			t.Logf("Test completed after %d resolves", resolveCount)
			return
		case <-ticker.C:
			value, err := e2eClient.FloatValue(ctx, "web-sdk-e2e-flag.double", 10.0, openfeature.EvaluationContext{})
			if err != nil {
				t.Fatalf("Failed to resolve double flag: %v", err)
			}

			if value != 3.5 {
				t.Errorf("Expected 3.5, got %v", value)
			}
			resolveCount++
			t.Logf("Resolve #%d: value=%v", resolveCount, value)
		}
	}
}

func TestE2E_ShouldResolveString(t *testing.T) {
	setupE2E(t)
	defer teardownE2E(t)

	ctx := context.Background()

	// Keep test alive for 20 seconds, resolve every second
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	timeout := time.After(20 * time.Second)

	resolveCount := 0
	for {
		select {
		case <-timeout:
			t.Logf("Test completed after %d resolves", resolveCount)
			return
		case <-ticker.C:
			value, err := e2eClient.StringValue(ctx, "web-sdk-e2e-flag.str", "default", openfeature.EvaluationContext{})
			if err != nil {
				t.Fatalf("Failed to resolve string flag: %v", err)
			}

			if value != "control" {
				t.Errorf("Expected 'control', got %v", value)
			}
			resolveCount++
			t.Logf("Resolve #%d: value=%v", resolveCount, value)
		}
	}
}

func TestE2E_ShouldResolveStruct(t *testing.T) {
	setupE2E(t)
	defer teardownE2E(t)

	ctx := context.Background()

	// Keep test alive for 20 seconds, resolve every second
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	timeout := time.After(20 * time.Second)

	resolveCount := 0
	for {
		select {
		case <-timeout:
			t.Logf("Test completed after %d resolves", resolveCount)
			return
		case <-ticker.C:
			value, err := e2eClient.ObjectValue(ctx, "web-sdk-e2e-flag.obj", map[string]interface{}{}, openfeature.EvaluationContext{})
			if err != nil {
				t.Fatalf("Failed to resolve struct flag: %v", err)
			}

			structMap, ok := value.(map[string]interface{})
			if !ok {
				t.Fatalf("Expected value to be a map[string]interface{}, got %T", value)
			}

			// Verify int field
			if intVal, ok := structMap["int"].(int64); !ok || intVal != 4 {
				t.Errorf("Expected int field to be 4, got %v", structMap["int"])
			}

			// Verify str field
			if strVal, ok := structMap["str"].(string); !ok || strVal != "obj control" {
				t.Errorf("Expected str field to be 'obj control', got %v", structMap["str"])
			}

			// Verify bool field
			if boolVal, ok := structMap["bool"].(bool); !ok || boolVal != false {
				t.Errorf("Expected bool field to be false, got %v", structMap["bool"])
			}

			// Verify double field
			if doubleVal, ok := structMap["double"].(float64); !ok || doubleVal != 3.6 {
				t.Errorf("Expected double field to be 3.6, got %v", structMap["double"])
			}

			// Verify obj-obj field is an empty map
			if objObjVal, ok := structMap["obj-obj"].(map[string]interface{}); !ok || len(objObjVal) != 0 {
				t.Errorf("Expected obj-obj field to be an empty map, got %v", structMap["obj-obj"])
			}

			resolveCount++
			t.Logf("Resolve #%d: struct verified", resolveCount)
		}
	}
}

func TestE2E_ShouldResolveSubValueFromStruct(t *testing.T) {
	setupE2E(t)
	defer teardownE2E(t)

	ctx := context.Background()

	// Keep test alive for 20 seconds, resolve every second
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	timeout := time.After(20 * time.Second)

	resolveCount := 0
	for {
		select {
		case <-timeout:
			t.Logf("Test completed after %d resolves", resolveCount)
			return
		case <-ticker.C:
			value, err := e2eClient.BooleanValue(ctx, "web-sdk-e2e-flag.obj.bool", true, openfeature.EvaluationContext{})
			if err != nil {
				t.Fatalf("Failed to resolve sub-value from struct: %v", err)
			}

			if value != false {
				t.Errorf("Expected false, got %v", value)
			}
			resolveCount++
			t.Logf("Resolve #%d: value=%v", resolveCount, value)
		}
	}
}

func TestE2E_ShouldResolveSubValueFromStructWithDetails(t *testing.T) {
	setupE2E(t)
	defer teardownE2E(t)

	ctx := context.Background()

	// Keep test alive for 20 seconds, resolve every second
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	timeout := time.After(20 * time.Second)

	resolveCount := 0
	for {
		select {
		case <-timeout:
			t.Logf("Test completed after %d resolves", resolveCount)
			return
		case <-ticker.C:
			details, err := e2eClient.FloatValueDetails(ctx, "web-sdk-e2e-flag.obj.double", 1.0, openfeature.EvaluationContext{})
			if err != nil {
				t.Fatalf("Failed to resolve sub-value from struct with details: %v", err)
			}

			if details.Value != 3.6 {
				t.Errorf("Expected value to be 3.6, got %v", details.Value)
			}

			if details.Variant != "flags/web-sdk-e2e-flag/variants/control" {
				t.Errorf("Expected variant to be 'flags/web-sdk-e2e-flag/variants/control', got %v", details.Variant)
			}

			if details.Reason != "RESOLVE_REASON_MATCH" {
				t.Errorf("Expected reason to be 'RESOLVE_REASON_MATCH', got %v", details.Reason)
			}

			resolveCount++
			t.Logf("Resolve #%d: value=%v, variant=%v", resolveCount, details.Value, details.Variant)
		}
	}
}

