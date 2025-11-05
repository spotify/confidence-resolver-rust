package confidence

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	adminv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/flags/admin/v1"
	resolverv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/flags/resolverinternal"
	iamv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/iam/v1"
	"github.com/tetratelabs/wazero"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const (
	defaultPollIntervalSeconds = 10
	confidenceDomain           = "edge-grpc.spotify.com"
)

// StateProvider is an interface for providing resolver state
type StateProvider interface {
	Provide(ctx context.Context) ([]byte, error)
}

// LocalResolverFactory creates and manages local flag resolvers with scheduled state fetching
type LocalResolverFactory struct {
	resolverAPI     *SwapWasmResolverApi
	stateProvider   StateProvider
	accountId       string
	flagLogger      WasmFlagLogger
	cancelFunc      context.CancelFunc
	logPollInterval time.Duration
	wg              sync.WaitGroup
	mu              sync.Mutex
}

// NewLocalResolverFactory creates a new LocalResolverFactory with gRPC clients and WASM bytes
// If customStateProvider is not nil, it will be used; otherwise creates a FlagsAdminStateFetcher
// Exposure logging is automatically enabled with gRPC services, disabled with custom StateProvider
// accountId is required when using customStateProvider, otherwise it's extracted from the token
func NewLocalResolverFactory(
	ctx context.Context,
	runtime wazero.Runtime,
	wasmBytes []byte,
	apiClientID string,
	apiClientSecret string,
	customStateProvider StateProvider,
	accountId string,
	connFactory ConnFactory,
) (*LocalResolverFactory, error) {
	logPollInterval := getPollIntervalSeconds()

	var flagLogger WasmFlagLogger
	var initialState []byte
	var resolvedAccountId string
	var stateProvider StateProvider

	// If custom StateProvider is provided, use it
	if customStateProvider != nil {
		// When using custom StateProvider, accountId must be provided
		if accountId == "" {
			return nil, fmt.Errorf("accountId is required when using custom StateProvider")
		}
		resolvedAccountId = accountId
		stateProvider = customStateProvider

		// Get initial state from provider
		var err error
		initialState, err = customStateProvider.Provide(ctx)
		if err != nil {
			log.Printf("Initial state fetch from provider failed, using empty state: %v", err)
			initialState = []byte{}
		}

		// When using custom StateProvider, no gRPC logger service is available
		// Exposure logging is disabled
		flagLogger = NewNoOpWasmFlagLogger()
	} else {
		// Create FlagsAdminStateFetcher and use it as StateProvider
		// Create TLS credentials for secure connections
		tlsCreds := credentials.NewTLS(nil)

		// Connection factory is provided by the builder
		factory := connFactory

		// Base dial options with transport credentials
		baseOpts := []grpc.DialOption{
			grpc.WithTransportCredentials(tlsCreds),
		}

		// Create auth service connection (no auth interceptor for this one)
		unauthConn, err := factory(ctx, confidenceDomain, baseOpts)
		if err != nil {
			return nil, err
		}

		authService := iamv1.NewAuthServiceClient(unauthConn)

		// Create token holder
		tokenHolder := NewTokenHolder(apiClientID, apiClientSecret, authService)

		// Create JWT auth interceptor
		authInterceptor := NewJwtAuthInterceptor(tokenHolder)

		authConnection, err := factory(ctx, confidenceDomain, append(
			append([]grpc.DialOption{}, baseOpts...),
			grpc.WithUnaryInterceptor(authInterceptor.UnaryClientInterceptor()),
		))
		if err != nil {
			return nil, err
		}

		// Get account name from token
		token, err := tokenHolder.GetToken(ctx)
		if err != nil {
			log.Printf("Warning: failed to get initial token, account name will be unknown: %v", err)
			// TODO should we return an error here?
			// return nil, fmt.Errorf("failed to get initial token: %w", err)
		}
		accountName := "unknown"
		if token != nil {
			accountName = token.Account
		}

		resolverStateService := adminv1.NewResolverStateServiceClient(authConnection)
		flagLoggerService := resolverv1.NewInternalFlagLoggerServiceClient(authConnection)

		// Create state fetcher (which implements StateProvider)
		stateFetcher := NewFlagsAdminStateFetcher(resolverStateService, accountName)
		stateProvider = stateFetcher

		// Get initial state using StateProvider interface
		initialState, err = stateProvider.Provide(ctx)
		if err != nil {
			log.Printf("Initial state fetch failed, using empty state: %v", err)
			// TODO should we return an error here?
			// return nil, fmt.Errorf("failed to get initial state: %w", err)
		}
		if initialState == nil {
			initialState = []byte{}
		}

		resolvedAccountId = stateFetcher.GetAccountID()
		if resolvedAccountId == "" {
			resolvedAccountId = "unknown"
		}

		flagLogger = NewGrpcWasmFlagLogger(flagLoggerService)
	}

	// Create SwapWasmResolverApi with initial state
	resolverAPI, err := NewSwapWasmResolverApi(ctx, runtime, wasmBytes, flagLogger, initialState, resolvedAccountId)
	if err != nil {
		return nil, err
	}

	// Create factory
	factory := &LocalResolverFactory{
		resolverAPI:     resolverAPI,
		stateProvider:   stateProvider,
		accountId:       resolvedAccountId,
		flagLogger:      flagLogger,
		logPollInterval: logPollInterval,
	}

	// Start scheduled tasks
	factory.startScheduledTasks(ctx)

	return factory, nil
}

// startScheduledTasks starts the background tasks for state fetching and log polling
func (f *LocalResolverFactory) startScheduledTasks(parentCtx context.Context) {
	ctx, cancel := context.WithCancel(parentCtx)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelFunc = cancel

	// Ticker for state fetching and log flushing using StateProvider
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		ticker := time.NewTicker(f.logPollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// Fetch latest state
				state, err := f.stateProvider.Provide(ctx)
				if err != nil {
					log.Printf("State fetch failed: %v", err)
				}

				// Update state and flush logs (even if state fetch failed, use cached state)
				if state != nil && f.accountId != "" {
					if err := f.resolverAPI.UpdateStateAndFlushLogs(state, f.accountId); err != nil {
						log.Printf("Failed to update state and flush logs: %v", err)
					} else {
						log.Printf("Updated resolver state and flushed logs for account %s", f.accountId)
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Shutdown stops all scheduled tasks and cleans up resources
func (f *LocalResolverFactory) Shutdown(ctx context.Context) {
	// lock to prevent concurrent shutdowns
	f.mu.Lock()
	defer f.mu.Unlock()
	log.Println("Shutting down local resolver factory")
	if f.cancelFunc == nil {
		log.Println("Scheduled tasks already cancelled")
		return
	}
	f.cancelFunc()
	f.cancelFunc = nil
	log.Println("Cancelled scheduled tasks")

	// Wait for background goroutines to exit
	f.wg.Wait()
	// Close resolver API first (which flushes final logs)
	if f.resolverAPI != nil {
		f.resolverAPI.Close(ctx)
		log.Println("Closed resolver API")
	}
	// Then shutdown flag logger (which waits for log sends to complete)
	if f.flagLogger != nil {
		f.flagLogger.Shutdown()
		log.Println("Shut down flag logger")
	}
	log.Println("Local resolver factory shut down")
}

// GetSwapResolverAPI returns the SwapWasmResolverApi
func (f *LocalResolverFactory) GetSwapResolverAPI() *SwapWasmResolverApi {
	return f.resolverAPI
}

// GetFlagLogger returns the flag logger
func (f *LocalResolverFactory) GetFlagLogger() WasmFlagLogger {
	return f.flagLogger
}

// getPollIntervalSeconds gets the poll interval from environment or returns default
func getPollIntervalSeconds() time.Duration {
	if envVal := os.Getenv("CONFIDENCE_RESOLVER_POLL_INTERVAL_SECONDS"); envVal != "" {
		if seconds, err := strconv.ParseInt(envVal, 10, 64); err == nil {
			return time.Duration(seconds) * time.Second
		}
	}
	return time.Duration(defaultPollIntervalSeconds) * time.Second
}
