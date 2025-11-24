package confidence

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	adminv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/flags/admin/v1"
	"google.golang.org/protobuf/proto"
)

// StateProvider is an interface for providing resolver state and account ID
type StateProvider interface {
	Provide(ctx context.Context) ([]byte, string, error)
}

// FlagsAdminStateFetcher fetches and updates the resolver state from the CDN
type FlagsAdminStateFetcher struct {
	clientSecret     string
	etag             atomic.Value // stores string
	rawResolverState atomic.Value // stores []byte
	accountID        atomic.Value // stores string
	httpClient       *http.Client
	logger           *slog.Logger
}

// Compile-time interface conformance check
var _ StateProvider = (*FlagsAdminStateFetcher)(nil)

// NewFlagsAdminStateFetcher creates a new FlagsAdminStateFetcher
func NewFlagsAdminStateFetcher(
	clientSecret string,
	logger *slog.Logger,
) *FlagsAdminStateFetcher {
	f := &FlagsAdminStateFetcher{
		clientSecret: clientSecret,
		logger:       logger,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	// Initialize with empty state
	emptyState := &adminv1.ResolverState{}
	if b, err := proto.Marshal(emptyState); err == nil {
		f.rawResolverState.Store(b)
	}
	return f
}

// GetRawState returns the current raw resolver state
func (f *FlagsAdminStateFetcher) GetRawState() []byte {
	if state := f.rawResolverState.Load(); state != nil {
		return state.([]byte)
	}
	return nil
}

// GetAccountID returns the account ID
func (f *FlagsAdminStateFetcher) GetAccountID() string {
	if accountID := f.accountID.Load(); accountID != nil {
		return accountID.(string)
	}
	return ""
}

// Reload fetches and updates the state if it has changed
func (f *FlagsAdminStateFetcher) Reload(ctx context.Context) error {
	if err := f.fetchAndUpdateStateIfChanged(ctx); err != nil {
		f.logger.Warn("Failed to reload, ignoring reload", "error", err)
		return err
	}
	return nil
}

// Provide implements the StateProvider interface
// Returns the latest resolver state and account ID, fetching it if needed
// On error, returns cached state (if available) to maintain availability
func (f *FlagsAdminStateFetcher) Provide(ctx context.Context) ([]byte, string, error) {
	// Try to fetch the latest state
	err := f.Reload(ctx)
	// Always return the current state and accountID (cached or fresh)
	// This ensures availability even if fetch fails
	return f.GetRawState(), f.GetAccountID(), err
}

// fetchAndUpdateStateIfChanged fetches the state from the CDN if it has changed
func (f *FlagsAdminStateFetcher) fetchAndUpdateStateIfChanged(ctx context.Context) error {
	// Build CDN URL directly using client secret
	// If client secret starts with http:// or https://, use it as-is (for testing)
	var cdnURL string
	if len(f.clientSecret) > 7 && (f.clientSecret[:7] == "http://" || f.clientSecret[:8] == "https://") {
		cdnURL = f.clientSecret
	} else {
		cdnURL = "https://confidence-resolver-state-cdn.spotifycdn.com/" + f.clientSecret
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cdnURL, nil)
	if err != nil {
		return err
	}

	// Add If-None-Match header if we have a previous ETag
	if previousEtag := f.etag.Load(); previousEtag != nil {
		req.Header.Set("If-None-Match", previousEtag.(string))
	}

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Check if content was modified
	if resp.StatusCode == http.StatusNotModified {
		// Not modified, nothing to update
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Read the new state
	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// Parse ClientResolverState
	clientState := &adminv1.ClientResolverState{}
	if err := proto.Unmarshal(bytes, clientState); err != nil {
		return fmt.Errorf("failed to unmarshal ClientResolverState: %w", err)
	}

	// Extract account ID
	f.accountID.Store(clientState.Account)

	// Serialize the nested ResolverState back to bytes
	stateBytes, err := proto.Marshal(clientState.State)
	if err != nil {
		return fmt.Errorf("failed to marshal ResolverState: %w", err)
	}

	// Get and store the new ETag
	etag := resp.Header.Get("ETag")
	f.etag.Store(etag)

	// Update the raw state
	f.rawResolverState.Store(stateBytes)

	f.logger.Info("Loaded resolver state", "etag", etag, "account", clientState.Account)

	return nil
}
