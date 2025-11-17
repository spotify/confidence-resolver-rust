package confidence

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	adminv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/flags/admin/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// StateProvider is an interface for providing resolver state and account ID
type StateProvider interface {
	Provide(ctx context.Context) ([]byte, string, error)
}

// FlagsAdminStateFetcher fetches and updates the resolver state from the admin service
type FlagsAdminStateFetcher struct {
	resolverStateService adminv1.ResolverStateServiceClient
	etag                 atomic.Value // stores string
	rawResolverState     atomic.Value // stores []byte
	resolverStateURI     atomic.Value // stores *adminv1.ResolverStateUriResponse
	refreshTime          atomic.Value // stores time.Time
	accountID            atomic.Value // stores string
	httpClient           *http.Client
	logger               *slog.Logger
}

// Compile-time interface conformance check
var _ StateProvider = (*FlagsAdminStateFetcher)(nil)

// NewFlagsAdminStateFetcher creates a new FlagsAdminStateFetcher
func NewFlagsAdminStateFetcher(
	resolverStateService adminv1.ResolverStateServiceClient,
	logger *slog.Logger,
) *FlagsAdminStateFetcher {
	f := &FlagsAdminStateFetcher{
		resolverStateService: resolverStateService,
		logger:               logger,
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

// getResolverFileURI gets the signed URI for downloading the resolver state
func (f *FlagsAdminStateFetcher) getResolverFileURI(ctx context.Context) (*adminv1.ResolverStateUriResponse, error) {
	now := time.Now()

	// Check if we have a cached URI that's still valid
	if cached := f.resolverStateURI.Load(); cached != nil {
		cachedURI := cached.(*adminv1.ResolverStateUriResponse)
		if refreshTime := f.refreshTime.Load(); refreshTime != nil {
			if now.Before(refreshTime.(time.Time)) {
				return cachedURI, nil
			}
		}
	}

	// Fetch new URI with timeout
	rpcCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := f.resolverStateService.ResolverStateUri(rpcCtx, &adminv1.ResolverStateUriRequest{})
	if err != nil {
		return nil, err
	}

	f.resolverStateURI.Store(resp)

	// Calculate refresh time (half of TTL)
	expireTime := resp.ExpireTime.AsTime()
	ttl := expireTime.Sub(now)
	refreshTime := now.Add(ttl / 2)
	f.refreshTime.Store(refreshTime)

	return resp, nil
}

// fetchAndUpdateStateIfChanged fetches the state from the signed URI if it has changed
func (f *FlagsAdminStateFetcher) fetchAndUpdateStateIfChanged(ctx context.Context) error {
	response, err := f.getResolverFileURI(ctx)
	if err != nil {
		return err
	}

	f.accountID.Store(response.Account)
	uri := response.SignedUri

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
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
		return err
	}

	// Read the new state
	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// Get and store the new ETag
	etag := resp.Header.Get("ETag")
	f.etag.Store(etag)

	// Update the raw state
	f.rawResolverState.Store(bytes)

	f.logger.Info("Loaded resolver state", "etag", etag)

	return nil
}

// toInstant converts a protobuf Timestamp to time.Time
func toInstant(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}
	return ts.AsTime()
}
