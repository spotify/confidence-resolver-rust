package confidence

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// JwtAuthInterceptor is a gRPC unary interceptor that attaches JWT tokens to requests
type JwtAuthInterceptor struct {
	tokenHolder *TokenHolder
}

// NewJwtAuthInterceptor creates a new JWT auth interceptor
func NewJwtAuthInterceptor(tokenHolder *TokenHolder) *JwtAuthInterceptor {
	return &JwtAuthInterceptor{
		tokenHolder: tokenHolder,
	}
}

// UnaryClientInterceptor returns a gRPC unary client interceptor that adds auth headers
func (i *JwtAuthInterceptor) UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply interface{},
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		// Get the token
		token, err := i.tokenHolder.GetToken(ctx)
		if err != nil {
			return fmt.Errorf("failed to get auth token: %w", err)
		}

		// Add Authorization header to metadata
		md := metadata.New(map[string]string{
			"authorization": fmt.Sprintf("Bearer %s", token.AccessToken),
		})

		// Merge with existing metadata if present
		if existingMd, ok := metadata.FromOutgoingContext(ctx); ok {
			md = metadata.Join(existingMd, md)
		}

		// Create new context with metadata
		ctx = metadata.NewOutgoingContext(ctx, md)

		// Invoke the RPC
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// StreamClientInterceptor returns a gRPC stream client interceptor that adds auth headers
func (i *JwtAuthInterceptor) StreamClientInterceptor() grpc.StreamClientInterceptor {
	return func(
		ctx context.Context,
		desc *grpc.StreamDesc,
		cc *grpc.ClientConn,
		method string,
		streamer grpc.Streamer,
		opts ...grpc.CallOption,
	) (grpc.ClientStream, error) {
		// Get the token
		token, err := i.tokenHolder.GetToken(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get auth token: %w", err)
		}

		// Add Authorization header to metadata
		md := metadata.New(map[string]string{
			"authorization": fmt.Sprintf("Bearer %s", token.AccessToken),
		})

		// Merge with existing metadata if present
		if existingMd, ok := metadata.FromOutgoingContext(ctx); ok {
			md = metadata.Join(existingMd, md)
		}

		// Create new context with metadata
		ctx = metadata.NewOutgoingContext(ctx, md)

		// Create the stream
		return streamer(ctx, desc, cc, method, opts...)
	}
}
