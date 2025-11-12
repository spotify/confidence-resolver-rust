package confidence

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	iamv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/iam/v1"
)

const (
	accountNameClaim = "https://confidence.dev/account_name"
	// Refresh token 1 hour before expiration
	tokenRefreshMargin = 1 * time.Hour
)

// Token represents an access token with its metadata
type Token struct {
	AccessToken string
	Account     string
	Expiration  time.Time
}

// TokenHolder manages access token caching and retrieval
type TokenHolder struct {
	apiClientID     string
	apiClientSecret string
	stub            iamv1.AuthServiceClient
	logger          *slog.Logger

	mu    sync.RWMutex
	token *Token
}

// NewTokenHolder creates a new TokenHolder
func NewTokenHolder(apiClientID, apiClientSecret string, stub iamv1.AuthServiceClient, logger *slog.Logger) *TokenHolder {
	return &TokenHolder{
		apiClientID:     apiClientID,
		apiClientSecret: apiClientSecret,
		stub:            stub,
		logger:          logger,
	}
}

// GetToken retrieves a cached or new token
func (h *TokenHolder) GetToken(ctx context.Context) (*Token, error) {
	h.mu.RLock()
	token := h.token
	h.mu.RUnlock()

	// Check if we have a valid cached token
	if token != nil && time.Now().Before(token.Expiration.Add(-tokenRefreshMargin)) {
		return token, nil
	}

	// Need to request a new token
	h.mu.Lock()
	defer h.mu.Unlock()

	// Double-check after acquiring write lock
	if h.token != nil && time.Now().Before(h.token.Expiration.Add(-tokenRefreshMargin)) {
		return h.token, nil
	}

	// Request new token
	newToken, err := h.requestAccessToken(ctx)
	if err != nil {
		return nil, err
	}

	h.token = newToken
	return newToken, nil
}

// requestAccessToken requests a new access token from the auth service
func (h *TokenHolder) requestAccessToken(ctx context.Context) (*Token, error) {
	request := &iamv1.RequestAccessTokenRequest{
		ClientId:     h.apiClientID,
		ClientSecret: h.apiClientSecret,
		GrantType:    "client_credentials",
	}

	// Create a context with a 10-second deadline for the RPC
	rpcCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	response, err := h.stub.RequestAccessToken(rpcCtx, request)
	if err != nil {
		return nil, fmt.Errorf("failed to request access token: %w", err)
	}

	accessToken := response.GetAccessToken()
	expiresIn := response.GetExpiresIn()

	// Decode JWT to extract account name
	account, err := extractAccountFromJWT(accessToken)
	if err != nil {
		h.logger.Error("Failed to extract account from JWT", "error", err)
		account = "unknown"
	}

	expiration := time.Now().Add(time.Duration(expiresIn) * time.Second)

	return &Token{
		AccessToken: accessToken,
		Account:     account,
		Expiration:  expiration,
	}, nil
}

// extractAccountFromJWT extracts the account name claim from a JWT token
func extractAccountFromJWT(token string) (string, error) {
	// JWT format: header.payload.signature
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid JWT format")
	}

	// Decode the payload (second part)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("failed to decode JWT payload: %w", err)
	}

	// Parse JSON
	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("failed to parse JWT claims: %w", err)
	}

	// Extract account name claim
	accountClaim, ok := claims[accountNameClaim]
	if !ok {
		return "", fmt.Errorf("missing required claim '%s' in JWT", accountNameClaim)
	}

	accountName, ok := accountClaim.(string)
	if !ok {
		return "", fmt.Errorf("claim '%s' is not a string", accountNameClaim)
	}

	return accountName, nil
}
