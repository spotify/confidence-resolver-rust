package confidence

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	adminv1 "github.com/spotify/confidence-resolver-rust/openfeature-provider/go/confidence/proto/confidence/flags/admin/v1"
	resolverv1 "github.com/spotify/confidence-resolver-rust/openfeature-provider/go/confidence/proto/confidence/flags/resolverinternal"
	iamv1 "github.com/spotify/confidence-resolver-rust/openfeature-provider/go/confidence/proto/confidence/iam/v1"
	"github.com/tetratelabs/wazero"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const (
	defaultPollIntervalSeconds = 15 // 15 seconds (for testing reload)
	defaultPollLogInterval     = 10 // 10 seconds
)

// StateProvider is an interface for providing resolver state
type StateProvider interface {
	Provide(ctx context.Context) ([]byte, error)
}

// LocalResolverFactory creates and manages local flag resolvers with scheduled state fetching
type LocalResolverFactory struct {
	resolverAPI     *SwapWasmResolverApi
	stateFetcher    *FlagsAdminStateFetcher
	stateProvider   StateProvider
	accountId       string
	flagLogger      WasmFlagLogger
	cancelFunc      context.CancelFunc
	pollInterval    time.Duration
	logPollInterval time.Duration
}

// NewLocalResolverFactory creates a new LocalResolverFactory with gRPC clients and WASM bytes
// If stateProvider is not nil, it will be used instead of FlagsAdminStateFetcher
// If disableLogging is true, all log requests will be dropped
// accountId is required when using stateProvider, otherwise it's extracted from the token
func NewLocalResolverFactory(
	ctx context.Context,
	runtime wazero.Runtime,
	wasmBytes []byte,
	resolverStateServiceAddr string,
	flagLoggerServiceAddr string,
	authServiceAddr string,
	apiClientID string,
	apiClientSecret string,
	stateProvider StateProvider,
	accountId string,
	disableLogging bool,
) (*LocalResolverFactory, error) {
	// Get poll interval from environment or use default
	pollInterval := getPollIntervalSeconds()
	logPollInterval := time.Duration(defaultPollLogInterval) * time.Second

	var stateFetcher *FlagsAdminStateFetcher
	var flagLogger WasmFlagLogger
	var initialState []byte
	var resolvedAccountId string

	// If StateProvider is provided, use it instead of gRPC state fetcher
	if stateProvider != nil {
		// When using StateProvider, accountId must be provided
		if accountId == "" {
			return nil, fmt.Errorf("accountId is required when using StateProvider")
		}
		resolvedAccountId = accountId

		// Get initial state from provider
		var err error
		initialState, err = stateProvider.Provide(ctx)
		if err != nil {
			log.Printf("Initial state fetch from provider failed, using empty state: %v", err)
			initialState = []byte{}
		}

		// Create flag logger based on disableLogging flag
		if disableLogging {
			flagLogger = NewNoOpWasmFlagLogger()
		} else {
			// If logging is enabled but using StateProvider, use NoOp for now
			// Users can extend this to provide their own logger
			flagLogger = NewNoOpWasmFlagLogger()
		}
	} else {
		// Use gRPC-based state fetcher and flag logger
		// Create TLS credentials for secure connections
		tlsCreds := credentials.NewTLS(nil)

		// Create auth service connection (no auth interceptor for this one)
		authConn, err := grpc.NewClient(authServiceAddr, grpc.WithTransportCredentials(tlsCreds))
		if err != nil {
			return nil, err
		}
		authService := iamv1.NewAuthServiceClient(authConn)

		// Create token holder
		tokenHolder := NewTokenHolder(apiClientID, apiClientSecret, authService)

		// Create JWT auth interceptor
		authInterceptor := NewJwtAuthInterceptor(tokenHolder)

		// Create gRPC connection for resolver state service with auth
		stateConn, err := grpc.NewClient(
			resolverStateServiceAddr,
			grpc.WithTransportCredentials(tlsCreds),
			grpc.WithUnaryInterceptor(authInterceptor.UnaryClientInterceptor()),
			grpc.WithStreamInterceptor(authInterceptor.StreamClientInterceptor()),
		)
		if err != nil {
			return nil, err
		}
		resolverStateService := adminv1.NewResolverStateServiceClient(stateConn)

		// Get account name from token
		token, err := tokenHolder.GetToken(ctx)
		if err != nil {
			log.Printf("Warning: failed to get initial token, account name will be unknown: %v", err)
		}
		accountName := "unknown"
		if token != nil {
			accountName = token.Account
		}

		// Create state fetcher
		stateFetcher = NewFlagsAdminStateFetcher(resolverStateService, accountName)

		// Do initial state fetch to get initial state for SwapWasmResolverApi
		if err := stateFetcher.Reload(ctx); err != nil {
			log.Printf("Initial state fetch failed, using empty state: %v", err)
		}

		initialState = stateFetcher.GetRawState()
		if initialState == nil {
			// Use empty state if no state available
			initialState = []byte{}
		}
		resolvedAccountId = stateFetcher.GetAccountID()
		if resolvedAccountId == "" {
			resolvedAccountId = "unknown"
		}

		// Create flag logger based on disableLogging flag
		if disableLogging {
			flagLogger = NewNoOpWasmFlagLogger()
		} else {
			// Create gRPC connection for flag logger service with auth
			loggerConn, err := grpc.NewClient(
				flagLoggerServiceAddr,
				grpc.WithTransportCredentials(tlsCreds),
				grpc.WithUnaryInterceptor(authInterceptor.UnaryClientInterceptor()),
				grpc.WithStreamInterceptor(authInterceptor.StreamClientInterceptor()),
			)
			if err != nil {
				return nil, err
			}
			flagLoggerService := resolverv1.NewInternalFlagLoggerServiceClient(loggerConn)
			flagLogger = NewGrpcWasmFlagLogger(flagLoggerService)
		}
	}

	// Create SwapWasmResolverApi with initial state
	resolverAPI, err := NewSwapWasmResolverApi(ctx, runtime, wasmBytes, flagLogger, initialState, resolvedAccountId)
	if err != nil {
		return nil, err
	}

	// Create factory
	factory := &LocalResolverFactory{
		resolverAPI:     resolverAPI,
		stateFetcher:    stateFetcher,
		stateProvider:   stateProvider,
		accountId:       resolvedAccountId,
		flagLogger:      flagLogger,
		pollInterval:    pollInterval,
		logPollInterval: logPollInterval,
	}

	// Start scheduled tasks
	factory.startScheduledTasks(ctx)

	return factory, nil
}

// startScheduledTasks starts the background tasks for state fetching and log polling
func (f *LocalResolverFactory) startScheduledTasks(parentCtx context.Context) {
	ctx, cancel := context.WithCancel(parentCtx)
	f.cancelFunc = cancel

	// Use StateProvider if available, otherwise use stateFetcher
	if f.stateProvider != nil {
		// Ticker for state fetching using StateProvider
		go func() {
			stateTicker := time.NewTicker(f.pollInterval)
			defer stateTicker.Stop()

			for {
				select {
				case <-stateTicker.C:
					state, err := f.stateProvider.Provide(ctx)
					if err != nil {
						log.Printf("State fetch from provider failed: %v", err)
					} else {
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
	} else {
		// Ticker for state fetching using FlagsAdminStateFetcher
		go func() {
			stateTicker := time.NewTicker(f.pollInterval)
			defer stateTicker.Stop()

			for {
				select {
				case <-stateTicker.C:
					if err := f.stateFetcher.Reload(ctx); err != nil {
						log.Printf("State fetch failed: %v", err)
					}
				case <-ctx.Done():
					return
				}
			}
		}()

		// Ticker for flushing logs (only when using gRPC state fetcher)
		go func() {
			logTicker := time.NewTicker(f.logPollInterval)
			defer logTicker.Stop()

			for {
				select {
				case <-logTicker.C:
					state := f.stateFetcher.GetRawState()
					accountId := f.stateFetcher.GetAccountID()
					if state != nil && accountId != "" {
						if err := f.resolverAPI.UpdateStateAndFlushLogs(state, accountId); err != nil {
							log.Printf("Failed to update state and flush logs: %v", err)
						} else {
							log.Printf("Updated resolver state and flushed logs for account %s", accountId)
						}
					}
				case <-ctx.Done():
					return
				}
			}
		}()
	}
}

// Shutdown stops all scheduled tasks and cleans up resources
func (f *LocalResolverFactory) Shutdown(ctx context.Context) {
	if f.cancelFunc != nil {
		f.cancelFunc()
	}
	if f.flagLogger != nil {
		f.flagLogger.Shutdown()
	}
	if f.resolverAPI != nil {
		f.resolverAPI.Close(ctx)
	}
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
