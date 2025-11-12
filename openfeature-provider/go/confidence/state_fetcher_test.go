package confidence

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	adminv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/flags/admin/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// mockResolverStateServiceClient is a mock implementation for testing
type mockResolverStateServiceClient struct {
	adminv1.ResolverStateServiceClient
	resolverStateUriFunc func(ctx context.Context, req *adminv1.ResolverStateUriRequest) (*adminv1.ResolverStateUriResponse, error)
}

func (m *mockResolverStateServiceClient) ResolverStateUri(ctx context.Context, req *adminv1.ResolverStateUriRequest, opts ...grpc.CallOption) (*adminv1.ResolverStateUriResponse, error) {
	if m.resolverStateUriFunc != nil {
		return m.resolverStateUriFunc(ctx, req)
	}
	return nil, errors.New("not implemented")
}

func TestNewFlagsAdminStateFetcher(t *testing.T) {
	mockService := &mockResolverStateServiceClient{}
	fetcher := NewFlagsAdminStateFetcher(mockService, "test-account", slog.New(slog.NewTextHandler(os.Stderr, nil)))

	if fetcher == nil {
		t.Fatal("Expected fetcher to be created, got nil")
	}
	if fetcher.accountName != "test-account" {
		t.Errorf("Expected account name to be 'test-account', got %s", fetcher.accountName)
	}
	if fetcher.httpClient == nil {
		t.Error("Expected HTTP client to be initialized")
	}

	// Should have empty state initially
	state := fetcher.GetRawState()
	if state == nil {
		t.Error("Expected initial state to be set")
	}
}

func TestFlagsAdminStateFetcher_GetRawState(t *testing.T) {
	mockService := &mockResolverStateServiceClient{}
	fetcher := NewFlagsAdminStateFetcher(mockService, "test-account", slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// Initial state should be empty but not nil
	state := fetcher.GetRawState()
	if state == nil {
		t.Error("Expected state to not be nil")
	}
}

func TestFlagsAdminStateFetcher_GetAccountID(t *testing.T) {
	mockService := &mockResolverStateServiceClient{}
	fetcher := NewFlagsAdminStateFetcher(mockService, "test-account", slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// Initially empty
	if fetcher.GetAccountID() != "" {
		t.Error("Expected account ID to be empty initially")
	}

	// Set account ID
	fetcher.accountID = "account-123"
	if fetcher.GetAccountID() != "account-123" {
		t.Errorf("Expected account ID to be 'account-123', got %s", fetcher.GetAccountID())
	}
}

func TestFlagsAdminStateFetcher_Reload_Success(t *testing.T) {
	// Create a test HTTP server that serves resolver state
	testState := &adminv1.ResolverState{
		Flags: []*adminv1.Flag{},
	}
	stateBytes, _ := proto.Marshal(testState)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", "test-etag")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(stateBytes)
	}))
	defer server.Close()

	// Mock service that returns signed URI
	mockService := &mockResolverStateServiceClient{
		resolverStateUriFunc: func(ctx context.Context, req *adminv1.ResolverStateUriRequest) (*adminv1.ResolverStateUriResponse, error) {
			return &adminv1.ResolverStateUriResponse{
				SignedUri:  server.URL,
				Account:    "test-account-123",
				ExpireTime: timestamppb.New(time.Now().Add(1 * time.Hour)),
			}, nil
		},
	}

	fetcher := NewFlagsAdminStateFetcher(mockService, "test-account", slog.New(slog.NewTextHandler(os.Stderr, nil)))
	ctx := context.Background()

	err := fetcher.Reload(ctx)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Verify state was updated
	state := fetcher.GetRawState()
	if state == nil {
		t.Fatal("Expected state to be loaded")
	}

	// Verify account ID was set
	if fetcher.GetAccountID() != "test-account-123" {
		t.Errorf("Expected account ID to be 'test-account-123', got %s", fetcher.GetAccountID())
	}

	// Verify ETag was stored
	if etag := fetcher.etag.Load(); etag == nil || etag.(string) != "test-etag" {
		t.Error("Expected ETag to be stored")
	}
}

func TestFlagsAdminStateFetcher_Reload_NotModified(t *testing.T) {
	requestCount := 0
	testState := &adminv1.ResolverState{Flags: []*adminv1.Flag{}}
	stateBytes, _ := proto.Marshal(testState)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		// First request - return state with ETag
		if requestCount == 1 {
			w.Header().Set("ETag", "test-etag")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(stateBytes)
			return
		}

		// Second request - check If-None-Match header
		if r.Header.Get("If-None-Match") == "test-etag" {
			w.WriteHeader(http.StatusNotModified)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(stateBytes)
	}))
	defer server.Close()

	mockService := &mockResolverStateServiceClient{
		resolverStateUriFunc: func(ctx context.Context, req *adminv1.ResolverStateUriRequest) (*adminv1.ResolverStateUriResponse, error) {
			return &adminv1.ResolverStateUriResponse{
				SignedUri:  server.URL,
				Account:    "test-account",
				ExpireTime: timestamppb.New(time.Now().Add(1 * time.Hour)),
			}, nil
		},
	}

	fetcher := NewFlagsAdminStateFetcher(mockService, "test-account", slog.New(slog.NewTextHandler(os.Stderr, nil)))
	ctx := context.Background()

	// First reload - gets state
	err := fetcher.Reload(ctx)
	if err != nil {
		t.Errorf("Expected no error on first reload, got %v", err)
	}

	initialState := fetcher.GetRawState()

	// Second reload - should get 304 Not Modified
	err = fetcher.Reload(ctx)
	if err != nil {
		t.Errorf("Expected no error on second reload, got %v", err)
	}

	// State should be unchanged
	secondState := fetcher.GetRawState()
	if string(initialState) != string(secondState) {
		t.Error("Expected state to be unchanged after 304 Not Modified")
	}

	if requestCount != 2 {
		t.Errorf("Expected 2 HTTP requests, got %d", requestCount)
	}
}

func TestFlagsAdminStateFetcher_Reload_URICaching(t *testing.T) {
	uriCallCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("state"))
	}))
	defer server.Close()

	mockService := &mockResolverStateServiceClient{
		resolverStateUriFunc: func(ctx context.Context, req *adminv1.ResolverStateUriRequest) (*adminv1.ResolverStateUriResponse, error) {
			uriCallCount++
			return &adminv1.ResolverStateUriResponse{
				SignedUri:  server.URL,
				Account:    "test-account",
				ExpireTime: timestamppb.New(time.Now().Add(10 * time.Second)),
			}, nil
		},
	}

	fetcher := NewFlagsAdminStateFetcher(mockService, "test-account", slog.New(slog.NewTextHandler(os.Stderr, nil)))
	ctx := context.Background()

	// First reload
	_ = fetcher.Reload(ctx)
	if uriCallCount != 1 {
		t.Errorf("Expected 1 URI call, got %d", uriCallCount)
	}

	// Second reload immediately - should use cached URI
	_ = fetcher.Reload(ctx)
	if uriCallCount != 1 {
		t.Errorf("Expected still 1 URI call (cached), got %d", uriCallCount)
	}
}

func TestFlagsAdminStateFetcher_Reload_Error(t *testing.T) {
	mockService := &mockResolverStateServiceClient{
		resolverStateUriFunc: func(ctx context.Context, req *adminv1.ResolverStateUriRequest) (*adminv1.ResolverStateUriResponse, error) {
			return nil, errors.New("service error")
		},
	}

	fetcher := NewFlagsAdminStateFetcher(mockService, "test-account", slog.New(slog.NewTextHandler(os.Stderr, nil)))
	ctx := context.Background()

	err := fetcher.Reload(ctx)
	if err == nil {
		t.Error("Expected error from reload")
	}
}

func TestFlagsAdminStateFetcher_Provide(t *testing.T) {
	testState := &adminv1.ResolverState{Flags: []*adminv1.Flag{}}
	stateBytes, _ := proto.Marshal(testState)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(stateBytes)
	}))
	defer server.Close()

	mockService := &mockResolverStateServiceClient{
		resolverStateUriFunc: func(ctx context.Context, req *adminv1.ResolverStateUriRequest) (*adminv1.ResolverStateUriResponse, error) {
			return &adminv1.ResolverStateUriResponse{
				SignedUri:  server.URL,
				Account:    "test-account",
				ExpireTime: timestamppb.New(time.Now().Add(1 * time.Hour)),
			}, nil
		},
	}

	fetcher := NewFlagsAdminStateFetcher(mockService, "test-account", slog.New(slog.NewTextHandler(os.Stderr, nil)))
	ctx := context.Background() // Provide should fetch and return state
	state, err := fetcher.Provide(ctx)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if state == nil {
		t.Error("Expected state to be returned")
	}
}

func TestFlagsAdminStateFetcher_Provide_ReturnsStateOnError(t *testing.T) {
	testState := &adminv1.ResolverState{Flags: []*adminv1.Flag{}}
	stateBytes, _ := proto.Marshal(testState)

	httpCallCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpCallCount++
		if httpCallCount == 1 {
			// First call succeeds
			w.Header().Set("ETag", "etag1")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(stateBytes)
		} else {
			// Subsequent calls fail
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	uriCallCount := 0
	mockService := &mockResolverStateServiceClient{
		resolverStateUriFunc: func(ctx context.Context, req *adminv1.ResolverStateUriRequest) (*adminv1.ResolverStateUriResponse, error) {
			uriCallCount++
			if uriCallCount > 1 {
				return nil, errors.New("service error")
			}
			// Return short expiration so second call will try to refresh
			return &adminv1.ResolverStateUriResponse{
				SignedUri:  server.URL,
				Account:    "test-account",
				ExpireTime: timestamppb.New(time.Now().Add(100 * time.Millisecond)),
			}, nil
		},
	}

	fetcher := NewFlagsAdminStateFetcher(mockService, "test-account", slog.New(slog.NewTextHandler(os.Stderr, nil)))
	ctx := context.Background()

	// First call succeeds
	state1, err := fetcher.Provide(ctx)
	if err != nil {
		t.Errorf("Expected no error on first call, got %v", err)
	}

	// Wait for URI to expire
	time.Sleep(200 * time.Millisecond)

	// Second call will try to refresh and fail
	state2, err := fetcher.Provide(ctx)
	if err == nil {
		t.Error("Expected error to be returned when service fails")
	}
	if state2 == nil {
		t.Error("Expected cached state to be returned despite error")
	}
	if string(state1) != string(state2) {
		t.Error("Expected cached state to match previous state")
	}
}

func TestFlagsAdminStateFetcher_HTTPTimeout(t *testing.T) {
	// Create a server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mockService := &mockResolverStateServiceClient{
		resolverStateUriFunc: func(ctx context.Context, req *adminv1.ResolverStateUriRequest) (*adminv1.ResolverStateUriResponse, error) {
			return &adminv1.ResolverStateUriResponse{
				SignedUri:  server.URL,
				Account:    "test-account",
				ExpireTime: timestamppb.New(time.Now().Add(1 * time.Hour)),
			}, nil
		},
	}

	fetcher := NewFlagsAdminStateFetcher(mockService, "test-account", slog.New(slog.NewTextHandler(os.Stderr, nil)))
	// Set short timeout for test
	fetcher.httpClient.Timeout = 100 * time.Millisecond

	ctx := context.Background()

	err := fetcher.Reload(ctx)
	if err == nil {
		t.Error("Expected timeout error")
	}
}

func TestToInstant(t *testing.T) {
	// Test with valid timestamp
	now := time.Now()
	ts := timestamppb.New(now)
	result := toInstant(ts)

	// Allow small difference due to precision
	if result.Sub(now).Abs() > time.Millisecond {
		t.Errorf("Expected time to match, got difference of %v", result.Sub(now))
	}

	// Test with nil timestamp
	nilResult := toInstant(nil)
	if !nilResult.IsZero() {
		t.Error("Expected zero time for nil timestamp")
	}
}
