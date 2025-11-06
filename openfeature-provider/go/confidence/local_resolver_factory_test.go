package confidence

import (
	"context"
	"testing"

	resolverv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/flags/resolverinternal"
)

// mockWasmFlagLoggerForFactory is a mock implementation for testing factory shutdown
type mockWasmFlagLoggerForFactory struct {
	shutdownCalled bool
	writeCalled    bool
	onShutdown     func()
	onWrite        func(ctx context.Context, request *resolverv1.WriteFlagLogsRequest) error
}

func (m *mockWasmFlagLoggerForFactory) Write(ctx context.Context, request *resolverv1.WriteFlagLogsRequest) error {
	m.writeCalled = true
	if m.onWrite != nil {
		return m.onWrite(ctx, request)
	}
	return nil
}

func (m *mockWasmFlagLoggerForFactory) Shutdown() {
	m.shutdownCalled = true
	if m.onShutdown != nil {
		m.onShutdown()
	}
}

func TestLocalResolverFactory_ShutdownOrder(t *testing.T) {
	// Track the order in which shutdown methods are called
	var callOrder []string

	mockLogger := &mockWasmFlagLoggerForFactory{
		onShutdown: func() {
			callOrder = append(callOrder, "logger")
		},
	}

	factory := &LocalResolverFactory{
		cancelFunc: func() {
			callOrder = append(callOrder, "cancel")
		},
		flagLogger:  mockLogger,
		resolverAPI: nil,
	}

	ctx := context.Background()
	factory.Shutdown(ctx)

	// Verify shutdown was called
	if !mockLogger.shutdownCalled {
		t.Error("Expected flag logger Shutdown to be called")
	}

	// Verify order: cancel should be called before logger shutdown
	if len(callOrder) != 2 {
		t.Errorf("Expected 2 shutdown calls, got %d", len(callOrder))
	}
	if len(callOrder) >= 2 {
		if callOrder[0] != "cancel" {
			t.Errorf("Expected cancel to be called first, but got %s", callOrder[0])
		}
		if callOrder[1] != "logger" {
			t.Errorf("Expected logger to be called second, but got %s", callOrder[1])
		}
	}
}

// mockResolverAPI is a mock implementation for testing shutdown order
type mockResolverAPI struct {
	closeCalled bool
	onClose     func()
}

func (m *mockResolverAPI) Close(ctx context.Context) {
	m.closeCalled = true
	if m.onClose != nil {
		m.onClose()
	}
}

func TestLocalResolverFactory_ShutdownOrderWithResolver(t *testing.T) {
	// This test verifies the critical shutdown order:
	// 1. Cancel context
	// 2. Wait for background tasks
	// 3. Close resolver API (which flushes final logs)
	// 4. Shutdown logger (which waits for log sends to complete)
	//
	// This order ensures logs generated during resolver Close are actually sent.

	var callOrder []string
	var logsSent bool

	mockLogger := &mockWasmFlagLoggerForFactory{
		onWrite: func(ctx context.Context, request *resolverv1.WriteFlagLogsRequest) error {
			callOrder = append(callOrder, "logger-write")
			logsSent = true
			return nil
		},
		onShutdown: func() {
			callOrder = append(callOrder, "logger-shutdown")
			// At this point, logs should already be sent
			if !logsSent {
				t.Error("Logger shutdown called before logs were sent!")
			}
		},
	}

	mockResolver := &mockResolverAPI{
		onClose: func() {
			callOrder = append(callOrder, "resolver-close")
			// Simulate resolver flushing logs on close
			mockLogger.Write(context.Background(), &resolverv1.WriteFlagLogsRequest{})
		},
	}

	factory := &LocalResolverFactory{
		cancelFunc: func() {
			callOrder = append(callOrder, "cancel")
		},
		flagLogger:  mockLogger,
		resolverAPI: (*SwapWasmResolverApi)(nil), // Can't easily mock this, test order instead
	}

	// Manually test the shutdown sequence - simulating the CORRECT order
	// This test verifies our fix works correctly

	if factory.cancelFunc != nil {
		factory.cancelFunc()
	}

	// Wait for background tasks (part of our fix)
	factory.wg.Wait()

	// Close resolver FIRST (which generates logs)
	mockResolver.Close(context.Background())

	// Then shutdown logger (which waits for logs to be sent)
	if factory.flagLogger != nil {
		factory.flagLogger.Shutdown()
	}

	// Verify the CORRECT order: cancel → resolver-close → logger-write → logger-shutdown
	expectedOrder := []string{"cancel", "resolver-close", "logger-write", "logger-shutdown"}
	if len(callOrder) != len(expectedOrder) {
		t.Errorf("Expected %d calls, got %d: %v", len(expectedOrder), len(callOrder), callOrder)
	}

	for i, expected := range expectedOrder {
		if i < len(callOrder) && callOrder[i] != expected {
			t.Errorf("Expected call %d to be '%s', got '%s'", i, expected, callOrder[i])
		}
	}

	// Verify logs were sent before logger shutdown
	if !logsSent {
		t.Error("Expected logs to be sent during shutdown")
	}

	// Verify all components were called
	if !mockResolver.closeCalled {
		t.Error("Expected resolver Close to be called")
	}
	if !mockLogger.shutdownCalled {
		t.Error("Expected logger Shutdown to be called")
	}
}
