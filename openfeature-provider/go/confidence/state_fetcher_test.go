package confidence

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	adminv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/flags/admin/v1"
	"google.golang.org/protobuf/proto"
)

func TestNewFlagsAdminStateFetcher(t *testing.T) {
	fetcher := NewFlagsAdminStateFetcher("test-client-secret", slog.New(slog.NewTextHandler(os.Stderr, nil)))

	if fetcher == nil {
		t.Fatal("Expected fetcher to be created, got nil")
	}
	if fetcher.httpClient == nil {
		t.Error("Expected HTTP client to be initialized")
	}
	if fetcher.clientSecret != "test-client-secret" {
		t.Errorf("Expected clientSecret to be 'test-client-secret', got %s", fetcher.clientSecret)
	}

	// Should have empty state initially
	state := fetcher.GetRawState()
	if state == nil {
		t.Error("Expected initial state to be set")
	}
}

func TestFlagsAdminStateFetcher_GetRawState(t *testing.T) {
	fetcher := NewFlagsAdminStateFetcher("test-client-secret", slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// Initial state should be empty but not nil
	state := fetcher.GetRawState()
	if state == nil {
		t.Error("Expected state to not be nil")
	}
}

func TestFlagsAdminStateFetcher_GetAccountID(t *testing.T) {
	fetcher := NewFlagsAdminStateFetcher("test-client-secret", slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// Initially empty
	if fetcher.GetAccountID() != "" {
		t.Error("Expected account ID to be empty initially")
	}

	// Set account ID
	fetcher.accountID.Store("account-123")
	if fetcher.GetAccountID() != "account-123" {
		t.Errorf("Expected account ID to be 'account-123', got %s", fetcher.GetAccountID())
	}
}

// TestFlagsAdminStateFetcher_Reload_Success tests successful state fetching from CDN
// Note: This is a unit test that verifies the parsing logic, not actual CDN access
func TestFlagsAdminStateFetcher_Reload_Success(t *testing.T) {
	// Create a test HTTP server that serves ClientResolverState
	testState := &adminv1.ResolverState{
		Flags: []*adminv1.Flag{},
	}
	clientState := &adminv1.ClientResolverState{
		Account: "test-account-123",
		State:   testState,
	}
	stateBytes, _ := proto.Marshal(clientState)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", "test-etag")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(stateBytes)
	}))
	defer server.Close()

	fetcher := NewFlagsAdminStateFetcher(server.URL, slog.New(slog.NewTextHandler(os.Stderr, nil)))
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

// TestFlagsAdminStateFetcher_Reload_NotModified tests ETag-based caching
func TestFlagsAdminStateFetcher_Reload_NotModified(t *testing.T) {
	requestCount := 0
	testState := &adminv1.ResolverState{Flags: []*adminv1.Flag{}}
	clientState := &adminv1.ClientResolverState{
		Account: "test-account",
		State:   testState,
	}
	stateBytes, _ := proto.Marshal(clientState)

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

	fetcher := NewFlagsAdminStateFetcher(server.URL, slog.New(slog.NewTextHandler(os.Stderr, nil)))
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

// TestFlagsAdminStateFetcher_Reload_Error tests error handling
func TestFlagsAdminStateFetcher_Reload_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	fetcher := NewFlagsAdminStateFetcher(server.URL, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	ctx := context.Background()

	err := fetcher.Reload(ctx)
	if err == nil {
		t.Error("Expected error from reload")
	}
}

// TestFlagsAdminStateFetcher_Provide tests the Provide method
func TestFlagsAdminStateFetcher_Provide(t *testing.T) {
	testState := &adminv1.ResolverState{Flags: []*adminv1.Flag{}}
	clientState := &adminv1.ClientResolverState{
		Account: "test-account",
		State:   testState,
	}
	stateBytes, _ := proto.Marshal(clientState)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(stateBytes)
	}))
	defer server.Close()

	fetcher := NewFlagsAdminStateFetcher(server.URL, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	ctx := context.Background()

	// Provide should fetch and return state and accountID
	state, accountID, err := fetcher.Provide(ctx)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if state == nil {
		t.Error("Expected state to be returned")
	}
	if accountID != "test-account" {
		t.Errorf("Expected accountID 'test-account', got %s", accountID)
	}
}

// TestFlagsAdminStateFetcher_Provide_ReturnsStateOnError tests error handling in Provide
func TestFlagsAdminStateFetcher_Provide_ReturnsStateOnError(t *testing.T) {
	testState := &adminv1.ResolverState{Flags: []*adminv1.Flag{}}
	clientState := &adminv1.ClientResolverState{
		Account: "test-account",
		State:   testState,
	}
	stateBytes, _ := proto.Marshal(clientState)

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

	fetcher := NewFlagsAdminStateFetcher(server.URL, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	ctx := context.Background()

	// First call succeeds
	state1, accountID1, err := fetcher.Provide(ctx)
	if err != nil {
		t.Errorf("Expected no error on first call, got %v", err)
	}
	if accountID1 != "test-account" {
		t.Errorf("Expected accountID 'test-account', got %s", accountID1)
	}

	// Second call will fail
	state2, accountID2, err := fetcher.Provide(ctx)
	if err == nil {
		t.Error("Expected error to be returned when service fails")
	}
	if state2 == nil {
		t.Error("Expected cached state to be returned despite error")
	}
	if string(state1) != string(state2) {
		t.Error("Expected cached state to match previous state")
	}
	if accountID1 != accountID2 {
		t.Error("Expected cached accountID to match previous accountID")
	}
}

// TestFlagsAdminStateFetcher_HTTPTimeout tests HTTP timeout handling
func TestFlagsAdminStateFetcher_HTTPTimeout(t *testing.T) {
	// Create a server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	fetcher := NewFlagsAdminStateFetcher(server.URL, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	// Set short timeout for test
	fetcher.httpClient.Timeout = 100 * time.Millisecond

	ctx := context.Background()

	err := fetcher.Reload(ctx)
	if err == nil {
		t.Error("Expected timeout error")
	}
}
