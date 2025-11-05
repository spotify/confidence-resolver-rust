package confidence

import (
	"context"
	"io"
	"log"
	"net/http"
	"sync/atomic"
	"time"

	adminv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/flags/admin/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// FlagsAdminStateFetcher fetches and updates the resolver state from the admin service
type FlagsAdminStateFetcher struct {
	resolverStateService adminv1.ResolverStateServiceClient
	accountName          string
	etag                 atomic.Value // stores string
	rawResolverState     atomic.Value // stores []byte
	resolverStateURI     atomic.Value // stores *adminv1.ResolverStateUriResponse
	refreshTime          atomic.Value // stores time.Time
	accountID            string
	httpClient           *http.Client
}

// NewFlagsAdminStateFetcher creates a new FlagsAdminStateFetcher
func NewFlagsAdminStateFetcher(
	resolverStateService adminv1.ResolverStateServiceClient,
	accountName string,
) *FlagsAdminStateFetcher {
	f := &FlagsAdminStateFetcher{
		resolverStateService: resolverStateService,
		accountName:          accountName,
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
	return f.accountID
}

// Reload fetches and updates the state if it has changed
func (f *FlagsAdminStateFetcher) Reload(ctx context.Context) error {
	if err := f.fetchAndUpdateStateIfChanged(ctx); err != nil {
		log.Printf("Failed to reload, ignoring reload: %v", err)
		return err
	}
	return nil
}

// Provide implements the StateProvider interface
// Returns the latest resolver state, fetching it if needed
// On error, returns cached state (if available) to maintain availability
func (f *FlagsAdminStateFetcher) Provide(ctx context.Context) ([]byte, error) {
	// Try to fetch the latest state
	err := f.Reload(ctx)
	// Always return the current state (cached or fresh)
	// This ensures availability even if fetch fails
	return f.GetRawState(), err
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

	f.accountID = response.Account
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

	log.Printf("Loaded resolver state for %s, etag=%s", f.accountName, etag)

	return nil
}

// toInstant converts a protobuf Timestamp to time.Time
func toInstant(ts *timestamppb.Timestamp) time.Time {
	if ts == nil {
		return time.Time{}
	}
	return ts.AsTime()
}
