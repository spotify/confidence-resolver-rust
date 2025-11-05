package confidence

import (
	"context"
	"errors"
	"testing"

	iamv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/iam/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestNewJwtAuthInterceptor(t *testing.T) {
	tokenHolder := &TokenHolder{}
	interceptor := NewJwtAuthInterceptor(tokenHolder)

	if interceptor == nil {
		t.Fatal("Expected interceptor to be created, got nil")
	}
	if interceptor.tokenHolder != tokenHolder {
		t.Error("Expected tokenHolder to be set correctly")
	}
}

func TestJwtAuthInterceptor_UnaryClientInterceptor_Success(t *testing.T) {
	// Create a valid JWT token with the account name claim
	validToken := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJodHRwczovL2NvbmZpZGVuY2UuZGV2L2FjY291bnRfbmFtZSI6InRlc3QtYWNjb3VudCJ9.signature"

	mockStub := &mockAuthServiceClient{
		requestAccessTokenFunc: func(ctx context.Context, req *iamv1.RequestAccessTokenRequest) (*iamv1.AccessToken, error) {
			return &iamv1.AccessToken{
				AccessToken: validToken,
				ExpiresIn:   3600,
			}, nil
		},
	}
	tokenHolder := NewTokenHolder("test-client", "test-secret", mockStub)

	interceptor := NewJwtAuthInterceptor(tokenHolder)
	unaryInterceptor := interceptor.UnaryClientInterceptor()

	// Create test variables
	ctx := context.Background()
	method := "/test.Service/Method"
	req := "test-request"
	reply := "test-reply"
	invoked := false

	// Mock invoker that captures the context
	var capturedCtx context.Context
	mockInvoker := func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		capturedCtx = ctx
		invoked = true
		return nil
	}

	// Call the interceptor
	err := unaryInterceptor(ctx, method, req, reply, nil, mockInvoker)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if !invoked {
		t.Error("Expected invoker to be called")
	}

	// Verify that authorization header was added
	md, ok := metadata.FromOutgoingContext(capturedCtx)
	if !ok {
		t.Fatal("Expected metadata to be present in context")
	}

	authHeaders := md.Get("authorization")
	if len(authHeaders) != 1 {
		t.Fatalf("Expected 1 authorization header, got %d", len(authHeaders))
	}
	expectedAuth := "Bearer " + validToken
	if authHeaders[0] != expectedAuth {
		t.Errorf("Expected authorization header to be '%s', got '%s'", expectedAuth, authHeaders[0])
	}
}

func TestJwtAuthInterceptor_UnaryClientInterceptor_TokenError(t *testing.T) {
	// Create a token holder that returns an error
	mockStub := &mockAuthServiceClient{
		requestAccessTokenFunc: func(ctx context.Context, req *iamv1.RequestAccessTokenRequest) (*iamv1.AccessToken, error) {
			return nil, errors.New("auth service unavailable")
		},
	}
	tokenHolder := NewTokenHolder("test-client", "test-secret", mockStub)

	interceptor := NewJwtAuthInterceptor(tokenHolder)
	unaryInterceptor := interceptor.UnaryClientInterceptor()

	ctx := context.Background()
	invoked := false

	mockInvoker := func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		invoked = true
		return nil
	}

	// Call the interceptor - should fail to get token
	err := unaryInterceptor(ctx, "method", "req", "reply", nil, mockInvoker)

	if err == nil {
		t.Error("Expected error when token is not available")
	}
	if invoked {
		t.Error("Expected invoker not to be called when token fails")
	}
}

func TestJwtAuthInterceptor_UnaryClientInterceptor_MergesMetadata(t *testing.T) {
	validToken := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJodHRwczovL2NvbmZpZGVuY2UuZGV2L2FjY291bnRfbmFtZSI6InRlc3QtYWNjb3VudCJ9.signature"

	mockStub := &mockAuthServiceClient{
		requestAccessTokenFunc: func(ctx context.Context, req *iamv1.RequestAccessTokenRequest) (*iamv1.AccessToken, error) {
			return &iamv1.AccessToken{
				AccessToken: validToken,
				ExpiresIn:   3600,
			}, nil
		},
	}
	tokenHolder := NewTokenHolder("test-client", "test-secret", mockStub)

	interceptor := NewJwtAuthInterceptor(tokenHolder)
	unaryInterceptor := interceptor.UnaryClientInterceptor()

	// Create context with existing metadata
	existingMd := metadata.New(map[string]string{
		"existing-header": "existing-value",
	})
	ctx := metadata.NewOutgoingContext(context.Background(), existingMd)

	var capturedCtx context.Context
	mockInvoker := func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		capturedCtx = ctx
		return nil
	}

	err := unaryInterceptor(ctx, "method", "req", "reply", nil, mockInvoker)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Verify both headers are present
	md, ok := metadata.FromOutgoingContext(capturedCtx)
	if !ok {
		t.Fatal("Expected metadata to be present")
	}

	// Check authorization header
	authHeaders := md.Get("authorization")
	expectedAuth := "Bearer " + validToken
	if len(authHeaders) != 1 || authHeaders[0] != expectedAuth {
		t.Error("Expected authorization header to be present and correct")
	}

	// Check existing header
	existingHeaders := md.Get("existing-header")
	if len(existingHeaders) != 1 || existingHeaders[0] != "existing-value" {
		t.Error("Expected existing header to be preserved")
	}
}

func TestJwtAuthInterceptor_UnaryClientInterceptor_InvokerError(t *testing.T) {
	validToken := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJodHRwczovL2NvbmZpZGVuY2UuZGV2L2FjY291bnRfbmFtZSI6InRlc3QtYWNjb3VudCJ9.signature"

	mockStub := &mockAuthServiceClient{
		requestAccessTokenFunc: func(ctx context.Context, req *iamv1.RequestAccessTokenRequest) (*iamv1.AccessToken, error) {
			return &iamv1.AccessToken{
				AccessToken: validToken,
				ExpiresIn:   3600,
			}, nil
		},
	}
	tokenHolder := NewTokenHolder("test-client", "test-secret", mockStub)

	interceptor := NewJwtAuthInterceptor(tokenHolder)
	unaryInterceptor := interceptor.UnaryClientInterceptor()

	ctx := context.Background()
	expectedErr := errors.New("invoker error")

	mockInvoker := func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
		return expectedErr
	}

	err := unaryInterceptor(ctx, "method", "req", "reply", nil, mockInvoker)

	if err != expectedErr {
		t.Errorf("Expected error from invoker to be propagated, got %v", err)
	}
}

func TestJwtAuthInterceptor_StreamClientInterceptor_Success(t *testing.T) {
	validToken := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJodHRwczovL2NvbmZpZGVuY2UuZGV2L2FjY291bnRfbmFtZSI6InRlc3QtYWNjb3VudCJ9.signature"

	mockStub := &mockAuthServiceClient{
		requestAccessTokenFunc: func(ctx context.Context, req *iamv1.RequestAccessTokenRequest) (*iamv1.AccessToken, error) {
			return &iamv1.AccessToken{
				AccessToken: validToken,
				ExpiresIn:   3600,
			}, nil
		},
	}
	tokenHolder := NewTokenHolder("test-client", "test-secret", mockStub)

	interceptor := NewJwtAuthInterceptor(tokenHolder)
	streamInterceptor := interceptor.StreamClientInterceptor()

	ctx := context.Background()
	desc := &grpc.StreamDesc{}
	method := "/test.Service/StreamMethod"
	streamed := false

	// Mock streamer that captures the context
	var capturedCtx context.Context
	mockStreamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		capturedCtx = ctx
		streamed = true
		return nil, nil
	}

	// Call the interceptor
	_, err := streamInterceptor(ctx, desc, nil, method, mockStreamer)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if !streamed {
		t.Error("Expected streamer to be called")
	}

	// Verify that authorization header was added
	md, ok := metadata.FromOutgoingContext(capturedCtx)
	if !ok {
		t.Fatal("Expected metadata to be present in context")
	}

	authHeaders := md.Get("authorization")
	if len(authHeaders) != 1 {
		t.Fatalf("Expected 1 authorization header, got %d", len(authHeaders))
	}
	expectedAuth := "Bearer " + validToken
	if authHeaders[0] != expectedAuth {
		t.Errorf("Expected authorization header to be '%s', got '%s'", expectedAuth, authHeaders[0])
	}
}

func TestJwtAuthInterceptor_StreamClientInterceptor_TokenError(t *testing.T) {
	mockStub := &mockAuthServiceClient{
		requestAccessTokenFunc: func(ctx context.Context, req *iamv1.RequestAccessTokenRequest) (*iamv1.AccessToken, error) {
			return nil, errors.New("auth service unavailable")
		},
	}
	tokenHolder := NewTokenHolder("test-client", "test-secret", mockStub)

	interceptor := NewJwtAuthInterceptor(tokenHolder)
	streamInterceptor := interceptor.StreamClientInterceptor()

	ctx := context.Background()
	streamed := false

	mockStreamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		streamed = true
		return nil, nil
	}

	_, err := streamInterceptor(ctx, &grpc.StreamDesc{}, nil, "method", mockStreamer)

	if err == nil {
		t.Error("Expected error when token is not available")
	}
	if streamed {
		t.Error("Expected streamer not to be called when token fails")
	}
}

func TestJwtAuthInterceptor_StreamClientInterceptor_MergesMetadata(t *testing.T) {
	validToken := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJodHRwczovL2NvbmZpZGVuY2UuZGV2L2FjY291bnRfbmFtZSI6InRlc3QtYWNjb3VudCJ9.signature"

	mockStub := &mockAuthServiceClient{
		requestAccessTokenFunc: func(ctx context.Context, req *iamv1.RequestAccessTokenRequest) (*iamv1.AccessToken, error) {
			return &iamv1.AccessToken{
				AccessToken: validToken,
				ExpiresIn:   3600,
			}, nil
		},
	}
	tokenHolder := NewTokenHolder("test-client", "test-secret", mockStub)

	interceptor := NewJwtAuthInterceptor(tokenHolder)
	streamInterceptor := interceptor.StreamClientInterceptor()

	// Create context with existing metadata
	existingMd := metadata.New(map[string]string{
		"stream-header": "stream-value",
	})
	ctx := metadata.NewOutgoingContext(context.Background(), existingMd)

	var capturedCtx context.Context
	mockStreamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		capturedCtx = ctx
		return nil, nil
	}

	_, err := streamInterceptor(ctx, &grpc.StreamDesc{}, nil, "method", mockStreamer)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Verify both headers are present
	md, ok := metadata.FromOutgoingContext(capturedCtx)
	if !ok {
		t.Fatal("Expected metadata to be present")
	}

	// Check authorization header
	authHeaders := md.Get("authorization")
	expectedAuth := "Bearer " + validToken
	if len(authHeaders) != 1 || authHeaders[0] != expectedAuth {
		t.Error("Expected authorization header to be present and correct")
	}

	// Check existing header
	streamHeaders := md.Get("stream-header")
	if len(streamHeaders) != 1 || streamHeaders[0] != "stream-value" {
		t.Error("Expected existing header to be preserved")
	}
}

func TestJwtAuthInterceptor_StreamClientInterceptor_StreamerError(t *testing.T) {
	validToken := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJodHRwczovL2NvbmZpZGVuY2UuZGV2L2FjY291bnRfbmFtZSI6InRlc3QtYWNjb3VudCJ9.signature"

	mockStub := &mockAuthServiceClient{
		requestAccessTokenFunc: func(ctx context.Context, req *iamv1.RequestAccessTokenRequest) (*iamv1.AccessToken, error) {
			return &iamv1.AccessToken{
				AccessToken: validToken,
				ExpiresIn:   3600,
			}, nil
		},
	}
	tokenHolder := NewTokenHolder("test-client", "test-secret", mockStub)

	interceptor := NewJwtAuthInterceptor(tokenHolder)
	streamInterceptor := interceptor.StreamClientInterceptor()

	ctx := context.Background()
	expectedErr := errors.New("streamer error")

	mockStreamer := func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		return nil, expectedErr
	}

	_, err := streamInterceptor(ctx, &grpc.StreamDesc{}, nil, "method", mockStreamer)

	if err != expectedErr {
		t.Errorf("Expected error from streamer to be propagated, got %v", err)
	}
}
