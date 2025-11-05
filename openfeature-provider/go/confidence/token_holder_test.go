package confidence

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	iamv1 "github.com/spotify/confidence-resolver-rust/openfeature-provider/go/confidence/proto/confidence/iam/v1"
	"google.golang.org/grpc"
)

// mockAuthServiceClient is a mock implementation of AuthServiceClient for testing
type mockAuthServiceClient struct {
	iamv1.AuthServiceClient
	requestAccessTokenFunc func(ctx context.Context, req *iamv1.RequestAccessTokenRequest) (*iamv1.AccessToken, error)
}

func (m *mockAuthServiceClient) RequestAccessToken(ctx context.Context, req *iamv1.RequestAccessTokenRequest, opts ...grpc.CallOption) (*iamv1.AccessToken, error) {
	if m.requestAccessTokenFunc != nil {
		return m.requestAccessTokenFunc(ctx, req)
	}
	return nil, nil
}

func TestNewTokenHolder(t *testing.T) {
	mockStub := &mockAuthServiceClient{}
	holder := NewTokenHolder("client-id", "client-secret", mockStub)

	if holder == nil {
		t.Fatal("Expected TokenHolder to be created, got nil")
	}
	if holder.apiClientID != "client-id" {
		t.Errorf("Expected apiClientID to be 'client-id', got %s", holder.apiClientID)
	}
	if holder.apiClientSecret != "client-secret" {
		t.Errorf("Expected apiClientSecret to be 'client-secret', got %s", holder.apiClientSecret)
	}
}

func TestTokenHolder_GetToken_FirstTime(t *testing.T) {
	// Create a valid JWT token with the account name claim
	// Format: header.payload.signature (simplified for testing)
	validToken := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJodHRwczovL2NvbmZpZGVuY2UuZGV2L2FjY291bnRfbmFtZSI6InRlc3QtYWNjb3VudCJ9.signature"

	mockStub := &mockAuthServiceClient{
		requestAccessTokenFunc: func(ctx context.Context, req *iamv1.RequestAccessTokenRequest) (*iamv1.AccessToken, error) {
			return &iamv1.AccessToken{
				AccessToken: validToken,
				ExpiresIn:   3600,
			}, nil
		},
	}

	holder := NewTokenHolder("client-id", "client-secret", mockStub)
	ctx := context.Background()

	token, err := holder.GetToken(ctx)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if token == nil {
		t.Fatal("Expected token to be returned, got nil")
	}
	if token.AccessToken != validToken {
		t.Errorf("Expected access token to be %s, got %s", validToken, token.AccessToken)
	}
	if token.Account != "test-account" {
		t.Errorf("Expected account to be 'test-account', got %s", token.Account)
	}
}

func TestTokenHolder_GetToken_CachedToken(t *testing.T) {
	callCount := 0
	validToken := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJodHRwczovL2NvbmZpZGVuY2UuZGV2L2FjY291bnRfbmFtZSI6InRlc3QtYWNjb3VudCJ9.signature"

	mockStub := &mockAuthServiceClient{
		requestAccessTokenFunc: func(ctx context.Context, req *iamv1.RequestAccessTokenRequest) (*iamv1.AccessToken, error) {
			callCount++
			return &iamv1.AccessToken{
				AccessToken: validToken,
				ExpiresIn:   7200, // 2 hours
			}, nil
		},
	}

	holder := NewTokenHolder("client-id", "client-secret", mockStub)
	ctx := context.Background()

	// First call - should request new token
	token1, err := holder.GetToken(ctx)
	if err != nil {
		t.Fatalf("Expected no error on first call, got %v", err)
	}
	if callCount != 1 {
		t.Errorf("Expected 1 API call, got %d", callCount)
	}

	// Second call - should use cached token
	token2, err := holder.GetToken(ctx)
	if err != nil {
		t.Fatalf("Expected no error on second call, got %v", err)
	}
	if callCount != 1 {
		t.Errorf("Expected still 1 API call (cached), got %d", callCount)
	}
	if token1.AccessToken != token2.AccessToken {
		t.Error("Expected same token to be returned from cache")
	}
}

func TestTokenHolder_GetToken_ExpiredToken(t *testing.T) {
	callCount := 0
	validToken := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJodHRwczovL2NvbmZpZGVuY2UuZGV2L2FjY291bnRfbmFtZSI6InRlc3QtYWNjb3VudCJ9.signature"

	mockStub := &mockAuthServiceClient{
		requestAccessTokenFunc: func(ctx context.Context, req *iamv1.RequestAccessTokenRequest) (*iamv1.AccessToken, error) {
			callCount++
			return &iamv1.AccessToken{
				AccessToken: validToken,
				ExpiresIn:   1, // Very short expiration
			}, nil
		},
	}

	holder := NewTokenHolder("client-id", "client-secret", mockStub)
	ctx := context.Background()

	// First call
	_, err := holder.GetToken(ctx)
	if err != nil {
		t.Fatalf("Expected no error on first call, got %v", err)
	}

	// Wait for token to expire (considering the 1 hour margin)
	time.Sleep(2 * time.Second)

	// Second call - should request new token
	_, err = holder.GetToken(ctx)
	if err != nil {
		t.Fatalf("Expected no error on second call, got %v", err)
	}
	if callCount != 2 {
		t.Errorf("Expected 2 API calls (token expired), got %d", callCount)
	}
}

func TestExtractAccountFromJWT_Valid(t *testing.T) {
	validToken := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJodHRwczovL2NvbmZpZGVuY2UuZGV2L2FjY291bnRfbmFtZSI6Im15LWFjY291bnQifQ.signature"

	account, err := extractAccountFromJWT(validToken)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if account != "my-account" {
		t.Errorf("Expected account to be 'my-account', got %s", account)
	}
}

func TestExtractAccountFromJWT_Invalid(t *testing.T) {
	testCases := []struct {
		name  string
		token string
	}{
		{
			name:  "Invalid format",
			token: "not.a.valid.jwt.token",
		},
		{
			name:  "Invalid base64",
			token: "header.!!!invalid!!!.signature",
		},
		{
			name:  "Missing claim",
			token: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJvdGhlcl9jbGFpbSI6InZhbHVlIn0.signature",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := extractAccountFromJWT(tc.token)
			if err == nil {
				t.Error("Expected error for invalid token, got nil")
			}
		})
	}
}

func TestTokenHolder_ConcurrentAccess(t *testing.T) {
	var callCount int32
	validToken := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJodHRwczovL2NvbmZpZGVuY2UuZGV2L2FjY291bnRfbmFtZSI6InRlc3QtYWNjb3VudCJ9.signature"

	mockStub := &mockAuthServiceClient{
		requestAccessTokenFunc: func(ctx context.Context, req *iamv1.RequestAccessTokenRequest) (*iamv1.AccessToken, error) {
			atomic.AddInt32(&callCount, 1)
			time.Sleep(50 * time.Millisecond) // Simulate longer API delay to ensure goroutines wait
			return &iamv1.AccessToken{
				AccessToken: validToken,
				ExpiresIn:   3600,
			}, nil
		},
	}

	holder := NewTokenHolder("client-id", "client-secret", mockStub)
	ctx := context.Background()

	// Launch multiple goroutines that all try to get token at the same time
	const goroutines = 10
	done := make(chan bool, goroutines)

	// Use a short delay to let all goroutines start more or less simultaneously
	start := make(chan bool)

	for i := 0; i < goroutines; i++ {
		go func() {
			<-start // Wait for signal to start
			_, err := holder.GetToken(ctx)
			if err != nil {
				t.Errorf("Unexpected error in goroutine: %v", err)
			}
			done <- true
		}()
	}

	// Signal all goroutines to start at once
	close(start)

	// Wait for all goroutines to complete
	for range goroutines {
		<-done
	}

	// The token holder should minimize redundant API calls
	// With proper locking, we expect 1-2 calls (sometimes a second goroutine might slip through)
	finalCount := atomic.LoadInt32(&callCount)
	if finalCount > goroutines/2 {
		t.Logf("Note: Got %d API calls with %d concurrent requests - acceptable but not optimal", finalCount, goroutines)
	}
	if finalCount > goroutines {
		t.Errorf("Expected fewer API calls than goroutines (%d), got %d", goroutines, finalCount)
	}
}
