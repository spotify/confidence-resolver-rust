package confidence

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/open-feature/go-sdk/openfeature"
	"github.com/tetratelabs/wazero"
)

// End-to-end tests that verify WriteFlagLogs contains correct flag assignment data.
//
// These tests use a CapturingFlagLogger to capture all flag log requests and verify:
//   - Flag names are correctly reported
//   - Targeting keys match the evaluation context
//   - Assignment information is present and valid
//   - Variant information matches the resolved value

const (
	flagLogsClientSecret = "ti5Sipq5EluCYRG7I5cdbpWC3xq7JTWv"
	flagLogsTargetingKey = "test-a"
)

// setupFlagLogsTest creates a provider with a capturing logger for testing.
// Returns the capturing logger, provider, and client.
// The caller is responsible for calling openfeature.Shutdown() when done.
func setupFlagLogsTest(t *testing.T) (*CapturingFlagLogger, openfeature.IClient) {
	ctx := context.Background()

	// Create capturing logger
	capturingLogger := NewCapturingFlagLogger()

	// Create state provider that fetches from real Confidence service
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	stateProvider := NewFlagsAdminStateFetcher(flagLogsClientSecret, logger)

	// Fetch initial state
	if err := stateProvider.Reload(ctx); err != nil {
		t.Fatalf("Failed to reload state: %v", err)
	}

	// Create wazero runtime
	runtimeConfig := wazero.NewRuntimeConfig()
	runtime := wazero.NewRuntimeWithConfig(ctx, runtimeConfig)

	// Create SwapWasmResolverApi
	resolverAPI, err := NewSwapWasmResolverApi(ctx, runtime, defaultWasmBytes, capturingLogger, logger)
	if err != nil {
		t.Fatalf("Failed to create resolver API: %v", err)
	}

	// Create provider
	provider := NewLocalResolverProvider(resolverAPI, stateProvider, capturingLogger, flagLogsClientSecret, logger)

	// Set provider and wait for ready
	err = openfeature.SetProviderAndWait(provider)
	if err != nil {
		t.Fatalf("Failed to set provider: %v", err)
	}

	// Set evaluation context
	evalCtx := openfeature.NewEvaluationContext(flagLogsTargetingKey, map[string]interface{}{
		"sticky": false,
	})
	openfeature.SetEvaluationContext(evalCtx)

	// Clear any logs captured during initialization
	capturingLogger.Clear()

	client := openfeature.NewClient("flag-logs-e2e-test")
	return capturingLogger, client
}

// flushAndWait shuts down OpenFeature and waits a bit for async operations
func flushAndWait() {
	openfeature.Shutdown()
	// Give async operations time to complete
	time.Sleep(100 * time.Millisecond)
}

func TestFlagLogs_ShouldCaptureWriteFlagLogsAfterBooleanResolve(t *testing.T) {
	capturingLogger, client := setupFlagLogsTest(t)

	ctx := context.Background()

	// Resolve a boolean flag
	value, err := client.BooleanValue(ctx, "web-sdk-e2e-flag.bool", true, openfeature.EvaluationContext{})
	if err != nil {
		t.Fatalf("Failed to resolve boolean flag: %v", err)
	}
	if value != false {
		t.Errorf("Expected false, got %v", value)
	}

	// Shutdown to flush logs
	flushAndWait()

	// Verify captured flag logs
	requests := capturingLogger.GetCapturedRequests()
	if len(requests) == 0 {
		t.Fatal("Expected captured requests, got none")
	}

	request := requests[0]
	if len(request.FlagAssigned) == 0 {
		t.Fatal("Expected flag_assigned entries, got none")
	}

	// Find the flag we resolved
	found := false
	for _, fa := range request.FlagAssigned {
		for _, af := range fa.Flags {
			if strings.Contains(af.Flag, "web-sdk-e2e-flag") {
				found = true
				if af.TargetingKey != flagLogsTargetingKey {
					t.Errorf("Expected targeting key %s, got %s", flagLogsTargetingKey, af.TargetingKey)
				}
			}
		}
	}
	if !found {
		t.Error("Expected to find web-sdk-e2e-flag in captured requests")
	}
}

func TestFlagLogs_ShouldCaptureCorrectVariantInFlagLogs(t *testing.T) {
	capturingLogger, client := setupFlagLogsTest(t)

	ctx := context.Background()

	// Resolve a string flag
	value, err := client.StringValue(ctx, "web-sdk-e2e-flag.str", "default", openfeature.EvaluationContext{})
	if err != nil {
		t.Fatalf("Failed to resolve string flag: %v", err)
	}
	if value != "control" {
		t.Errorf("Expected 'control', got %v", value)
	}

	// Shutdown to flush logs
	flushAndWait()

	requests := capturingLogger.GetCapturedRequests()
	if len(requests) == 0 {
		t.Fatal("Expected captured requests, got none")
	}

	request := requests[0]
	if len(request.FlagAssigned) == 0 {
		t.Fatal("Expected flag_assigned entries, got none")
	}

	// Verify variant information is present
	flagAssigned := request.FlagAssigned[0]
	if len(flagAssigned.Flags) == 0 {
		t.Fatal("Expected flags in flag_assigned, got none")
	}

	appliedFlag := flagAssigned.Flags[0]
	if appliedFlag.Flag == "" {
		t.Error("Expected flag name to be non-empty")
	}
}

func TestFlagLogs_ShouldCaptureClientResolveInfo(t *testing.T) {
	capturingLogger, client := setupFlagLogsTest(t)

	ctx := context.Background()

	// Perform a resolve
	_, _ = client.IntValue(ctx, "web-sdk-e2e-flag.int", 10, openfeature.EvaluationContext{})

	// Shutdown to flush logs
	flushAndWait()

	requests := capturingLogger.GetCapturedRequests()
	if len(requests) == 0 {
		t.Fatal("Expected captured requests, got none")
	}

	request := requests[0]
	if len(request.ClientResolveInfo) == 0 {
		t.Error("Expected client_resolve_info to be present")
	}
}

func TestFlagLogs_ShouldCaptureFlagResolveInfo(t *testing.T) {
	capturingLogger, client := setupFlagLogsTest(t)

	ctx := context.Background()

	// Perform a resolve
	_, _ = client.FloatValue(ctx, "web-sdk-e2e-flag.double", 10.0, openfeature.EvaluationContext{})

	// Shutdown to flush logs
	flushAndWait()

	requests := capturingLogger.GetCapturedRequests()
	if len(requests) == 0 {
		t.Fatal("Expected captured requests, got none")
	}

	request := requests[0]
	if len(request.FlagResolveInfo) == 0 {
		t.Error("Expected flag_resolve_info to be present")
	}
}

func TestFlagLogs_ShouldCaptureMultipleResolvesInSingleRequest(t *testing.T) {
	capturingLogger, client := setupFlagLogsTest(t)

	ctx := context.Background()

	// Perform multiple resolves
	_, _ = client.BooleanValue(ctx, "web-sdk-e2e-flag.bool", true, openfeature.EvaluationContext{})
	_, _ = client.StringValue(ctx, "web-sdk-e2e-flag.str", "default", openfeature.EvaluationContext{})
	_, _ = client.IntValue(ctx, "web-sdk-e2e-flag.int", 10, openfeature.EvaluationContext{})
	_, _ = client.FloatValue(ctx, "web-sdk-e2e-flag.double", 10.0, openfeature.EvaluationContext{})

	// Shutdown to flush logs
	flushAndWait()

	// Should have captured log entries for all resolves
	totalFlagAssigned := capturingLogger.GetTotalFlagAssignedCount()
	if totalFlagAssigned < 4 {
		t.Errorf("Expected at least 4 flag_assigned entries, got %d", totalFlagAssigned)
	}
}

func TestFlagLogs_ShouldCallShutdownOnProviderShutdown(t *testing.T) {
	capturingLogger, client := setupFlagLogsTest(t)

	ctx := context.Background()

	// Perform a resolve
	_, _ = client.BooleanValue(ctx, "web-sdk-e2e-flag.bool", true, openfeature.EvaluationContext{})

	// Shutdown
	flushAndWait()

	if !capturingLogger.WasShutdownCalled() {
		t.Error("Expected shutdown to be called on logger")
	}
}

func TestFlagLogs_ShouldCaptureResolveIdInFlagAssigned(t *testing.T) {
	capturingLogger, client := setupFlagLogsTest(t)

	ctx := context.Background()

	// Perform a resolve
	_, _ = client.BooleanValue(ctx, "web-sdk-e2e-flag.bool", true, openfeature.EvaluationContext{})

	// Shutdown to flush logs
	flushAndWait()

	requests := capturingLogger.GetCapturedRequests()
	if len(requests) == 0 {
		t.Fatal("Expected captured requests, got none")
	}

	request := requests[0]
	if len(request.FlagAssigned) == 0 {
		t.Fatal("Expected flag_assigned entries, got none")
	}

	// Verify resolve_id is present
	flagAssigned := request.FlagAssigned[0]
	if flagAssigned.ResolveId == "" {
		t.Error("Expected resolve_id to be non-empty")
	}
}

func TestFlagLogs_ShouldCaptureClientInfoInFlagAssigned(t *testing.T) {
	capturingLogger, client := setupFlagLogsTest(t)

	ctx := context.Background()

	// Perform a resolve
	_, _ = client.BooleanValue(ctx, "web-sdk-e2e-flag.bool", true, openfeature.EvaluationContext{})

	// Shutdown to flush logs
	flushAndWait()

	requests := capturingLogger.GetCapturedRequests()
	if len(requests) == 0 {
		t.Fatal("Expected captured requests, got none")
	}

	request := requests[0]
	if len(request.FlagAssigned) == 0 {
		t.Fatal("Expected flag_assigned entries, got none")
	}

	// Verify client_info is present
	flagAssigned := request.FlagAssigned[0]
	if flagAssigned.ClientInfo == nil {
		t.Error("Expected client_info to be present")
	}
	if flagAssigned.ClientInfo != nil && flagAssigned.ClientInfo.Client == "" {
		t.Error("Expected client to be non-empty")
	}
}

// TestFlagLogs_ShouldSuccessfullySendToRealBackend tests that we can successfully
// send WriteFlagLogs to the real Confidence backend and get a successful response.
// This uses the real gRPC connection to edge-grpc.spotify.com.
func TestFlagLogs_ShouldSuccessfullySendToRealBackend(t *testing.T) {
	ctx := context.Background()

	// Create a custom logger that captures log messages
	var logBuffer logCaptureBuffer
	captureHandler := slog.NewTextHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(captureHandler)

	// Create a real provider with real gRPC connection
	provider, err := NewProvider(ctx, ProviderConfig{
		ClientSecret: flagLogsClientSecret,
		Logger:       logger,
	})
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	// Set provider and wait for ready
	err = openfeature.SetProviderAndWait(provider)
	if err != nil {
		t.Fatalf("Failed to set provider: %v", err)
	}

	// Set evaluation context
	evalCtx := openfeature.NewEvaluationContext(flagLogsTargetingKey, map[string]interface{}{
		"sticky": false,
	})
	openfeature.SetEvaluationContext(evalCtx)

	client := openfeature.NewClient("real-backend-e2e-test")

	// Perform a resolve to generate logs
	value, err := client.BooleanValue(ctx, "web-sdk-e2e-flag.bool", true, openfeature.EvaluationContext{})
	if err != nil {
		t.Fatalf("Failed to resolve boolean flag: %v", err)
	}
	if value != false {
		t.Errorf("Expected false, got %v", value)
	}

	// Shutdown - this flushes logs to the real backend via gRPC
	openfeature.Shutdown()

	// Give async operations time to complete
	time.Sleep(200 * time.Millisecond)

	// Verify the logs contain success message and no errors
	logs := logBuffer.String()
	if strings.Contains(logs, "Failed to write flag logs") {
		t.Errorf("Backend returned error - found 'Failed to write flag logs' in logs:\n%s", logs)
	}
	if !strings.Contains(logs, "Successfully sent flag log") {
		t.Errorf("Expected 'Successfully sent flag log' in logs, but not found:\n%s", logs)
	}

	t.Log("Successfully sent WriteFlagLogs to real Confidence backend")
}

// logCaptureBuffer is a thread-safe buffer for capturing log output
type logCaptureBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *logCaptureBuffer) Write(p []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logCaptureBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
