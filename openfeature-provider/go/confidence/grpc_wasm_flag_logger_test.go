package confidence

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	adminv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/flags/admin/v1"
	resolverevents "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/flags/resolverevents"
	resolverv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/flags/resolverinternal"
	"google.golang.org/grpc"
)

// mockInternalFlagLoggerServiceClient is a mock implementation for testing
type mockInternalFlagLoggerServiceClient struct {
	resolverv1.InternalFlagLoggerServiceClient
	writeFlagLogsFunc func(ctx context.Context, req *resolverv1.WriteFlagLogsRequest) (*resolverv1.WriteFlagLogsResponse, error)
}

func (m *mockInternalFlagLoggerServiceClient) WriteFlagLogs(ctx context.Context, req *resolverv1.WriteFlagLogsRequest, opts ...grpc.CallOption) (*resolverv1.WriteFlagLogsResponse, error) {
	if m.writeFlagLogsFunc != nil {
		return m.writeFlagLogsFunc(ctx, req)
	}
	return &resolverv1.WriteFlagLogsResponse{}, nil
}

func TestNewGrpcWasmFlagLogger(t *testing.T) {
	mockStub := &mockInternalFlagLoggerServiceClient{}
	logger := NewGrpcWasmFlagLogger(mockStub)

	if logger == nil {
		t.Fatal("Expected logger to be created, got nil")
	}
	if logger.stub == nil {
		t.Error("Expected stub to be set correctly")
	}
	if logger.writer == nil {
		t.Error("Expected writer to be initialized")
	}
}

func TestGrpcWasmFlagLogger_Write_Empty(t *testing.T) {
	callCount := 0
	mockStub := &mockInternalFlagLoggerServiceClient{
		writeFlagLogsFunc: func(ctx context.Context, req *resolverv1.WriteFlagLogsRequest) (*resolverv1.WriteFlagLogsResponse, error) {
			callCount++
			return &resolverv1.WriteFlagLogsResponse{}, nil
		},
	}

	logger := NewGrpcWasmFlagLogger(mockStub)
	ctx := context.Background()

	// Empty request should be skipped
	request := &resolverv1.WriteFlagLogsRequest{}
	err := logger.Write(ctx, request)
	if err != nil {
		t.Errorf("Expected no error for empty request, got %v", err)
	}

	// Wait for async processing
	logger.Shutdown()

	if callCount != 0 {
		t.Errorf("Expected 0 calls for empty request, got %d", callCount)
	}
}

func TestGrpcWasmFlagLogger_Write_SmallRequest(t *testing.T) {
	var callCount int32
	var receivedRequests []*resolverv1.WriteFlagLogsRequest

	mockStub := &mockInternalFlagLoggerServiceClient{
		writeFlagLogsFunc: func(ctx context.Context, req *resolverv1.WriteFlagLogsRequest) (*resolverv1.WriteFlagLogsResponse, error) {
			atomic.AddInt32(&callCount, 1)
			receivedRequests = append(receivedRequests, req)
			return &resolverv1.WriteFlagLogsResponse{}, nil
		},
	}

	logger := NewGrpcWasmFlagLogger(mockStub)
	ctx := context.Background()

	// Create a small request (below chunk threshold)
	request := &resolverv1.WriteFlagLogsRequest{
		FlagAssigned: make([]*resolverevents.FlagAssigned, 100),
	}

	err := logger.Write(ctx, request)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Wait for async processing
	logger.Shutdown()

	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call for small request, got %d", callCount)
	}
	if len(receivedRequests) != 1 {
		t.Fatalf("Expected 1 received request, got %d", len(receivedRequests))
	}
	if len(receivedRequests[0].FlagAssigned) != 100 {
		t.Errorf("Expected 100 flag_assigned entries, got %d", len(receivedRequests[0].FlagAssigned))
	}
}

func TestGrpcWasmFlagLogger_Write_Chunking(t *testing.T) {
	var callCount int32
	var receivedRequests []*resolverv1.WriteFlagLogsRequest

	mockStub := &mockInternalFlagLoggerServiceClient{
		writeFlagLogsFunc: func(ctx context.Context, req *resolverv1.WriteFlagLogsRequest) (*resolverv1.WriteFlagLogsResponse, error) {
			atomic.AddInt32(&callCount, 1)
			receivedRequests = append(receivedRequests, req)
			return &resolverv1.WriteFlagLogsResponse{}, nil
		},
	}

	logger := NewGrpcWasmFlagLogger(mockStub)
	ctx := context.Background()

	// Create a large request (above chunk threshold)
	numFlags := MaxFlagAssignedPerChunk + 500
	request := &resolverv1.WriteFlagLogsRequest{
		FlagAssigned:      make([]*resolverevents.FlagAssigned, numFlags),
		ClientResolveInfo: []*adminv1.ClientResolveInfo{{Client: "test-client"}},
		FlagResolveInfo:   []*adminv1.FlagResolveInfo{{Flag: "test-flag"}},
	}

	err := logger.Write(ctx, request)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Wait for async processing
	logger.Shutdown()

	// Should be split into 2 chunks
	expectedChunks := 2
	if atomic.LoadInt32(&callCount) != int32(expectedChunks) {
		t.Errorf("Expected %d chunks, got %d", expectedChunks, callCount)
	}

	if len(receivedRequests) != expectedChunks {
		t.Fatalf("Expected %d received requests, got %d", expectedChunks, len(receivedRequests))
	}

	// Note: Chunks may arrive in any order due to async processing
	// Find which chunk has metadata
	var chunkWithMetadata, chunkWithoutMetadata *resolverv1.WriteFlagLogsRequest
	for _, chunk := range receivedRequests {
		if len(chunk.ClientResolveInfo) > 0 || len(chunk.FlagResolveInfo) > 0 {
			chunkWithMetadata = chunk
		} else {
			chunkWithoutMetadata = chunk
		}
	}

	// First chunk should have MaxFlagAssignedPerChunk entries and metadata
	if chunkWithMetadata == nil {
		t.Fatal("Expected to find chunk with metadata")
	}
	if len(chunkWithMetadata.FlagAssigned) != MaxFlagAssignedPerChunk {
		t.Errorf("Expected metadata chunk to have %d entries, got %d", MaxFlagAssignedPerChunk, len(chunkWithMetadata.FlagAssigned))
	}
	if len(chunkWithMetadata.ClientResolveInfo) != 1 {
		t.Error("Expected metadata chunk to have client resolve info")
	}
	if len(chunkWithMetadata.FlagResolveInfo) != 1 {
		t.Error("Expected metadata chunk to have flag resolve info")
	}

	// Second chunk should have remaining entries and no metadata
	if chunkWithoutMetadata == nil {
		t.Fatal("Expected to find chunk without metadata")
	}
	if len(chunkWithoutMetadata.FlagAssigned) != 500 {
		t.Errorf("Expected chunk without metadata to have 500 entries, got %d", len(chunkWithoutMetadata.FlagAssigned))
	}
	if len(chunkWithoutMetadata.ClientResolveInfo) != 0 {
		t.Error("Expected chunk without metadata to have no client resolve info")
	}
	if len(chunkWithoutMetadata.FlagResolveInfo) != 0 {
		t.Error("Expected chunk without metadata to have no flag resolve info")
	}
}

func TestGrpcWasmFlagLogger_CreateChunks(t *testing.T) {
	mockStub := &mockInternalFlagLoggerServiceClient{}
	logger := NewGrpcWasmFlagLogger(mockStub)

	// Create request with metadata
	request := &resolverv1.WriteFlagLogsRequest{
		FlagAssigned:      make([]*resolverevents.FlagAssigned, 2500),
		ClientResolveInfo: []*adminv1.ClientResolveInfo{{Client: "test"}},
		FlagResolveInfo:   []*adminv1.FlagResolveInfo{{Flag: "flag"}},
	}

	chunks := logger.createFlagAssignedChunks(request)

	// Should create 3 chunks: 1000 + 1000 + 500
	if len(chunks) != 3 {
		t.Errorf("Expected 3 chunks, got %d", len(chunks))
	}

	// Verify chunk sizes
	if len(chunks[0].FlagAssigned) != MaxFlagAssignedPerChunk {
		t.Errorf("Expected chunk 0 to have %d entries, got %d", MaxFlagAssignedPerChunk, len(chunks[0].FlagAssigned))
	}
	if len(chunks[1].FlagAssigned) != MaxFlagAssignedPerChunk {
		t.Errorf("Expected chunk 1 to have %d entries, got %d", MaxFlagAssignedPerChunk, len(chunks[1].FlagAssigned))
	}
	if len(chunks[2].FlagAssigned) != 500 {
		t.Errorf("Expected chunk 2 to have 500 entries, got %d", len(chunks[2].FlagAssigned))
	}

	// Only first chunk should have metadata
	if len(chunks[0].ClientResolveInfo) == 0 || len(chunks[0].FlagResolveInfo) == 0 {
		t.Error("Expected first chunk to have metadata")
	}
	if len(chunks[1].ClientResolveInfo) != 0 || len(chunks[1].FlagResolveInfo) != 0 {
		t.Error("Expected second chunk to have no metadata")
	}
	if len(chunks[2].ClientResolveInfo) != 0 || len(chunks[2].FlagResolveInfo) != 0 {
		t.Error("Expected third chunk to have no metadata")
	}
}

func TestGrpcWasmFlagLogger_WithCustomWriter(t *testing.T) {
	mockStub := &mockInternalFlagLoggerServiceClient{}
	callCount := 0

	customWriter := func(ctx context.Context, request *resolverv1.WriteFlagLogsRequest) error {
		callCount++
		return nil
	}

	logger := NewGrpcWasmFlagLoggerWithWriter(mockStub, customWriter)
	ctx := context.Background()

	request := &resolverv1.WriteFlagLogsRequest{
		FlagAssigned: make([]*resolverevents.FlagAssigned, 10),
	}

	err := logger.Write(ctx, request)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Custom writer is called synchronously
	if callCount != 1 {
		t.Errorf("Expected custom writer to be called once, got %d calls", callCount)
	}
}

func TestGrpcWasmFlagLogger_ErrorHandling(t *testing.T) {
	var callCount int32
	expectedErr := errors.New("test error")

	mockStub := &mockInternalFlagLoggerServiceClient{
		writeFlagLogsFunc: func(ctx context.Context, req *resolverv1.WriteFlagLogsRequest) (*resolverv1.WriteFlagLogsResponse, error) {
			atomic.AddInt32(&callCount, 1)
			return nil, expectedErr
		},
	}

	logger := NewGrpcWasmFlagLogger(mockStub)
	ctx := context.Background()

	request := &resolverv1.WriteFlagLogsRequest{
		FlagAssigned: make([]*resolverevents.FlagAssigned, 10),
	}

	// Write should not return error (async)
	err := logger.Write(ctx, request)
	if err != nil {
		t.Errorf("Expected no error from Write (async), got %v", err)
	}

	// Wait for async processing
	logger.Shutdown()

	// Should still have attempted the call
	if atomic.LoadInt32(&callCount) != 1 {
		t.Errorf("Expected 1 call attempt, got %d", callCount)
	}
}

func TestGrpcWasmFlagLogger_Shutdown(t *testing.T) {
	var processedCount int32
	mockStub := &mockInternalFlagLoggerServiceClient{
		writeFlagLogsFunc: func(ctx context.Context, req *resolverv1.WriteFlagLogsRequest) (*resolverv1.WriteFlagLogsResponse, error) {
			time.Sleep(50 * time.Millisecond) // Simulate slow processing
			atomic.AddInt32(&processedCount, 1)
			return &resolverv1.WriteFlagLogsResponse{}, nil
		},
	}

	logger := NewGrpcWasmFlagLogger(mockStub)
	ctx := context.Background()

	// Send multiple requests
	for i := 0; i < 5; i++ {
		request := &resolverv1.WriteFlagLogsRequest{
			FlagAssigned: make([]*resolverevents.FlagAssigned, 10),
		}
		_ = logger.Write(ctx, request)
	}

	// Shutdown should wait for all to complete
	logger.Shutdown()

	if atomic.LoadInt32(&processedCount) != 5 {
		t.Errorf("Expected all 5 requests to be processed, got %d", processedCount)
	}
}

func TestNoOpWasmFlagLogger(t *testing.T) {
	logger := NewNoOpWasmFlagLogger()
	ctx := context.Background()

	request := &resolverv1.WriteFlagLogsRequest{
		FlagAssigned: make([]*resolverevents.FlagAssigned, 100),
	}

	// Should not return error
	err := logger.Write(ctx, request)
	if err != nil {
		t.Errorf("Expected no error from NoOp logger, got %v", err)
	}

	// Shutdown should not panic
	logger.Shutdown()
}
