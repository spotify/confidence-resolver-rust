package confidence

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/open-feature/go-sdk/openfeature"
	adminv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/flags/admin/v1"
	resolverv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/flags/resolverinternal"
	resolvertypes "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/flags/resolvertypes"
	iamv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/iam/v1"
	"github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/resolver"
	"github.com/tetratelabs/wazero"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/types/known/structpb"
)

const (
	defaultPollIntervalSeconds = 10
	confidenceDomain           = "edge-grpc.spotify.com"
)

// StateProvider is an interface for providing resolver state
type StateProvider interface {
	Provide(ctx context.Context) ([]byte, error)
}

// LocalResolverProvider implements the OpenFeature FeatureProvider interface
// for local flag resolution using the Confidence WASM resolver
type LocalResolverProvider struct {
	// Configuration (set during construction)
	runtime             wazero.Runtime
	wasmBytes           []byte
	apiClientID         string
	apiClientSecret     string
	customStateProvider StateProvider
	configAccountId     string
	connFactory         ConnFactory
	clientSecret        string

	// Runtime state (set during initialization)
	resolverAPI     *SwapWasmResolverApi
	stateProvider   StateProvider
	accountId       string
	flagLogger      WasmFlagLogger
	cancelFunc      context.CancelFunc
	logPollInterval time.Duration
	wg              sync.WaitGroup
	mu              sync.Mutex
	initialized     bool
}

// NewLocalResolverProvider creates a new LocalResolverProvider without initializing
// Call Init() to perform actual initialization
func NewLocalResolverProvider(
	runtime wazero.Runtime,
	wasmBytes []byte,
	apiClientID string,
	apiClientSecret string,
	customStateProvider StateProvider,
	accountId string,
	connFactory ConnFactory,
	clientSecret string,
) *LocalResolverProvider {
	return &LocalResolverProvider{
		runtime:             runtime,
		wasmBytes:           wasmBytes,
		apiClientID:         apiClientID,
		apiClientSecret:     apiClientSecret,
		customStateProvider: customStateProvider,
		configAccountId:     accountId,
		connFactory:         connFactory,
		clientSecret:        clientSecret,
		logPollInterval:     getPollIntervalSeconds(),
	}
}

// NewLocalResolverProviderWithLogger creates a provider with custom logger for testing
// This is similar to NewLocalResolverProvider but allows injecting a custom logger
// The logger will be used during initialization instead of creating a new one
func NewLocalResolverProviderWithLogger(
	runtime wazero.Runtime,
	wasmBytes []byte,
	customStateProvider StateProvider,
	accountId string,
	flagLogger WasmFlagLogger,
	clientSecret string,
	connFactory ConnFactory,
) *LocalResolverProvider {
	p := &LocalResolverProvider{
		runtime:             runtime,
		wasmBytes:           wasmBytes,
		customStateProvider: customStateProvider,
		configAccountId:     accountId,
		connFactory:         connFactory,
		clientSecret:        clientSecret,
		logPollInterval:     getPollIntervalSeconds(),
	}
	// For testing: pre-set the logger so Init() will use it
	p.flagLogger = flagLogger
	return p
}

// Metadata returns the provider metadata
func (p *LocalResolverProvider) Metadata() openfeature.Metadata {
	return openfeature.Metadata{
		Name: "confidence-sdk-go-local",
	}
}

// BooleanEvaluation evaluates a boolean flag
func (p *LocalResolverProvider) BooleanEvaluation(
	ctx context.Context,
	flag string,
	defaultValue bool,
	evalCtx openfeature.FlattenedContext,
) openfeature.BoolResolutionDetail {
	result := p.ObjectEvaluation(ctx, flag, defaultValue, evalCtx)

	if result.Value == nil {
		return openfeature.BoolResolutionDetail{
			Value: defaultValue,
			ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
				Reason:          result.Reason,
				ResolutionError: result.ResolutionError,
			},
		}
	}

	boolVal, ok := result.Value.(bool)
	if !ok {
		return openfeature.BoolResolutionDetail{
			Value: defaultValue,
			ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
				Reason:          openfeature.ErrorReason,
				ResolutionError: openfeature.NewTypeMismatchResolutionError("value is not a boolean"),
			},
		}
	}

	return openfeature.BoolResolutionDetail{
		Value:                    boolVal,
		ProviderResolutionDetail: result.ProviderResolutionDetail,
	}
}

// StringEvaluation evaluates a string flag
func (p *LocalResolverProvider) StringEvaluation(
	ctx context.Context,
	flag string,
	defaultValue string,
	evalCtx openfeature.FlattenedContext,
) openfeature.StringResolutionDetail {
	result := p.ObjectEvaluation(ctx, flag, defaultValue, evalCtx)

	if result.Value == nil {
		return openfeature.StringResolutionDetail{
			Value: defaultValue,
			ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
				Reason:          result.Reason,
				ResolutionError: result.ResolutionError,
			},
		}
	}

	strVal, ok := result.Value.(string)
	if !ok {
		return openfeature.StringResolutionDetail{
			Value: defaultValue,
			ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
				Reason:          openfeature.ErrorReason,
				ResolutionError: openfeature.NewTypeMismatchResolutionError("value is not a string"),
			},
		}
	}

	return openfeature.StringResolutionDetail{
		Value:                    strVal,
		ProviderResolutionDetail: result.ProviderResolutionDetail,
	}
}

// FloatEvaluation evaluates a float flag
func (p *LocalResolverProvider) FloatEvaluation(
	ctx context.Context,
	flag string,
	defaultValue float64,
	evalCtx openfeature.FlattenedContext,
) openfeature.FloatResolutionDetail {
	result := p.ObjectEvaluation(ctx, flag, defaultValue, evalCtx)

	if result.Value == nil {
		return openfeature.FloatResolutionDetail{
			Value: defaultValue,
			ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
				Reason:          result.Reason,
				ResolutionError: result.ResolutionError,
			},
		}
	}

	floatVal, ok := result.Value.(float64)
	if !ok {
		return openfeature.FloatResolutionDetail{
			Value: defaultValue,
			ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
				Reason:          openfeature.ErrorReason,
				ResolutionError: openfeature.NewTypeMismatchResolutionError("value is not a float"),
			},
		}
	}

	return openfeature.FloatResolutionDetail{
		Value:                    floatVal,
		ProviderResolutionDetail: result.ProviderResolutionDetail,
	}
}

// IntEvaluation evaluates an int flag
func (p *LocalResolverProvider) IntEvaluation(
	ctx context.Context,
	flag string,
	defaultValue int64,
	evalCtx openfeature.FlattenedContext,
) openfeature.IntResolutionDetail {
	result := p.ObjectEvaluation(ctx, flag, defaultValue, evalCtx)

	if result.Value == nil {
		return openfeature.IntResolutionDetail{
			Value: defaultValue,
			ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
				Reason:          result.Reason,
				ResolutionError: result.ResolutionError,
			},
		}
	}

	// Handle both int64 and float64 (JSON numbers are float64)
	switch v := result.Value.(type) {
	case int64:
		return openfeature.IntResolutionDetail{
			Value:                    v,
			ProviderResolutionDetail: result.ProviderResolutionDetail,
		}
	case float64:
		return openfeature.IntResolutionDetail{
			Value:                    int64(v),
			ProviderResolutionDetail: result.ProviderResolutionDetail,
		}
	default:
		return openfeature.IntResolutionDetail{
			Value: defaultValue,
			ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
				Reason:          openfeature.ErrorReason,
				ResolutionError: openfeature.NewTypeMismatchResolutionError("value is not an integer"),
			},
		}
	}
}

// ObjectEvaluation evaluates an object flag (core implementation)
func (p *LocalResolverProvider) ObjectEvaluation(
	ctx context.Context,
	flag string,
	defaultValue interface{},
	evalCtx openfeature.FlattenedContext,
) openfeature.InterfaceResolutionDetail {
	// Parse flag path (supports "flag.path.to.value" syntax)
	flagPath, path := parseFlagPath(flag)

	// Process targeting key (convert "targetingKey" to "targeting_key")
	processedCtx := processTargetingKey(evalCtx)

	// Convert evaluation context to protobuf Struct
	protoCtx, err := flattenedContextToProto(processedCtx)
	if err != nil {
		log.Printf("Failed to convert evaluation context to proto: %v", err)
		return openfeature.InterfaceResolutionDetail{
			Value: defaultValue,
			ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
				Reason:          openfeature.ErrorReason,
				ResolutionError: openfeature.NewGeneralResolutionError(fmt.Sprintf("failed to convert context: %v", err)),
			},
		}
	}

	// Build resolve request
	requestFlagName := "flags/" + flagPath
	request := &resolver.ResolveFlagsRequest{
		Flags:             []string{requestFlagName},
		Apply:             true,
		ClientSecret:      p.clientSecret,
		EvaluationContext: protoCtx,
	}

	// Get resolver API
	resolverAPI := p.resolverAPI

	// Create ResolveWithSticky request
	stickyRequest := &resolver.ResolveWithStickyRequest{
		ResolveRequest:          request,
		MaterializationsPerUnit: make(map[string]*resolver.MaterializationMap),
		FailFastOnSticky:        true,
		NotProcessSticky:        false,
	}

	// Resolve flags with sticky support
	stickyResponse, err := resolverAPI.ResolveWithSticky(stickyRequest)
	if err != nil {
		log.Printf("Failed to resolve flag '%s': %v", flagPath, err)
		return openfeature.InterfaceResolutionDetail{
			Value: defaultValue,
			ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
				Reason:          openfeature.ErrorReason,
				ResolutionError: openfeature.NewGeneralResolutionError(fmt.Sprintf("resolve failed: %v", err)),
			},
		}
	}

	// Extract the actual resolve response from the sticky response
	var response *resolver.ResolveFlagsResponse
	switch result := stickyResponse.ResolveResult.(type) {
	case *resolver.ResolveWithStickyResponse_Success_:
		response = result.Success.Response
	case *resolver.ResolveWithStickyResponse_MissingMaterializations_:
		log.Printf("Missing materializations for flag '%s'", flagPath)
		return openfeature.InterfaceResolutionDetail{
			Value: defaultValue,
			ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
				Reason:          openfeature.ErrorReason,
				ResolutionError: openfeature.NewGeneralResolutionError("missing materializations"),
			},
		}
	default:
		log.Printf("Unexpected resolve result type for flag '%s'", flagPath)
		return openfeature.InterfaceResolutionDetail{
			Value: defaultValue,
			ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
				Reason:          openfeature.ErrorReason,
				ResolutionError: openfeature.NewGeneralResolutionError("unexpected resolve result"),
			},
		}
	}

	// Check if flag was found
	if len(response.ResolvedFlags) == 0 {
		log.Printf("No active flag '%s' was found", flagPath)
		return openfeature.InterfaceResolutionDetail{
			Value: defaultValue,
			ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
				Reason:          openfeature.ErrorReason,
				ResolutionError: openfeature.NewFlagNotFoundResolutionError(fmt.Sprintf("flag '%s' not found", flagPath)),
			},
		}
	}

	resolvedFlag := response.ResolvedFlags[0]

	// Verify flag name matches
	if resolvedFlag.Flag != requestFlagName {
		log.Printf("Unexpected flag '%s' from resolver", resolvedFlag.Flag)
		return openfeature.InterfaceResolutionDetail{
			Value: defaultValue,
			ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
				Reason:          openfeature.ErrorReason,
				ResolutionError: openfeature.NewFlagNotFoundResolutionError("unexpected flag returned"),
			},
		}
	}

	// Check if variant is assigned
	if resolvedFlag.Variant == "" {
		return openfeature.InterfaceResolutionDetail{
			Value: defaultValue,
			ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
				ResolutionError: openfeature.ResolutionError{},
				Reason:          mapResolveReasonToOpenFeature(resolvedFlag.Reason),
			},
		}
	}

	// Convert protobuf struct to Go interface{}
	value := protoStructToGo(resolvedFlag.Value)

	// If a path was specified, extract the nested value
	if path != "" {
		value = getValueForPath(path, value)
	}

	// If value is nil, use default
	if value == nil {
		value = defaultValue
	}

	return openfeature.InterfaceResolutionDetail{
		Value: value,
		ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
			Variant:         resolvedFlag.Variant,
			ResolutionError: openfeature.ResolutionError{},
			Reason:          mapResolveReasonToOpenFeature(resolvedFlag.Reason),
		},
	}
}

// Hooks returns provider hooks (none for this implementation)
func (p *LocalResolverProvider) Hooks() []openfeature.Hook {
	return []openfeature.Hook{}
}

// Init initializes the provider (part of StateHandler interface)
// This performs all heavy initialization: gRPC setup, token fetching, state fetching, and starting background tasks
func (p *LocalResolverProvider) Init(evaluationContext openfeature.EvaluationContext) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.initialized {
		return nil
	}

	ctx := context.Background()

	var flagLogger WasmFlagLogger
	var initialState []byte
	var resolvedAccountId string
	var stateProvider StateProvider

	// If custom StateProvider is provided, use it
	if p.customStateProvider != nil {
		// When using custom StateProvider, accountId must be provided
		if p.configAccountId == "" {
			return fmt.Errorf("accountId is required when using custom StateProvider")
		}
		resolvedAccountId = p.configAccountId
		stateProvider = p.customStateProvider

		// Get initial state from provider
		var err error
		initialState, err = p.customStateProvider.Provide(ctx)
		if err != nil {
			log.Printf("Initial state fetch from provider failed, using empty state: %v", err)
			initialState = []byte{}
		}

		// Check if logger is already set (for testing)
		if p.flagLogger != nil {
			flagLogger = p.flagLogger
		} else {
			// When using custom StateProvider, no gRPC logger service is available
			// Exposure logging is disabled
			flagLogger = NewNoOpWasmFlagLogger()
		}
	} else {
		// Create FlagsAdminStateFetcher and use it as StateProvider
		// Create TLS credentials for secure connections
		tlsCreds := credentials.NewTLS(nil)

		// Connection factory
		factory := p.connFactory

		// Base dial options with transport credentials
		baseOpts := []grpc.DialOption{
			grpc.WithTransportCredentials(tlsCreds),
		}

		// Create auth service connection (no auth interceptor for this one)
		unauthConn, err := factory(ctx, confidenceDomain, baseOpts)
		if err != nil {
			return err
		}

		authService := iamv1.NewAuthServiceClient(unauthConn)

		// Create token holder
		tokenHolder := NewTokenHolder(p.apiClientID, p.apiClientSecret, authService)

		// Create JWT auth interceptor
		authInterceptor := NewJwtAuthInterceptor(tokenHolder)

		authConnection, err := factory(ctx, confidenceDomain, append(
			append([]grpc.DialOption{}, baseOpts...),
			grpc.WithUnaryInterceptor(authInterceptor.UnaryClientInterceptor()),
		))
		if err != nil {
			return err
		}

		// Get account name from token
		token, err := tokenHolder.GetToken(ctx)
		if err != nil {
			log.Printf("Warning: failed to get initial token, account name will be unknown: %v", err)
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
	resolverAPI, err := NewSwapWasmResolverApi(ctx, p.runtime, p.wasmBytes, flagLogger, initialState, resolvedAccountId)
	if err != nil {
		return err
	}

	// Store initialized state
	p.resolverAPI = resolverAPI
	p.stateProvider = stateProvider
	p.accountId = resolvedAccountId
	p.flagLogger = flagLogger

	// Start scheduled tasks
	p.startScheduledTasks(ctx)

	p.initialized = true
	return nil
}

// startScheduledTasks starts the background tasks for state fetching and log polling
func (p *LocalResolverProvider) startScheduledTasks(parentCtx context.Context) {
	ctx, cancel := context.WithCancel(parentCtx)
	p.cancelFunc = cancel

	// Ticker for state fetching and log flushing using StateProvider
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		ticker := time.NewTicker(p.logPollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// Fetch latest state
				state, err := p.stateProvider.Provide(ctx)
				if err != nil {
					log.Printf("State fetch failed: %v", err)
				}

				// Update state and flush logs (even if state fetch failed, use cached state)
				if state != nil && p.accountId != "" {
					if err := p.resolverAPI.UpdateStateAndFlushLogs(state, p.accountId); err != nil {
						log.Printf("Failed to update state and flush logs: %v", err)
					} else {
						log.Printf("Updated resolver state and flushed logs for account %s", p.accountId)
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Shutdown closes the provider and cleans up resources (part of StateHandler interface)
func (p *LocalResolverProvider) Shutdown() {
	// Lock to prevent concurrent shutdowns
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.initialized {
		log.Println("Provider not initialized, nothing to shutdown")
		return
	}

	log.Println("Shutting down local resolver provider")
	if p.cancelFunc == nil {
		log.Println("Scheduled tasks already cancelled")
		return
	}
	p.cancelFunc()
	p.cancelFunc = nil
	log.Println("Cancelled scheduled tasks")

	// Wait for background goroutines to exit
	p.wg.Wait()
	// Close resolver API first (which flushes final logs)
	if p.resolverAPI != nil {
		p.resolverAPI.Close(context.Background())
		log.Println("Closed resolver API")
	}
	// Then shutdown flag logger (which waits for log sends to complete)
	if p.flagLogger != nil {
		p.flagLogger.Shutdown()
		log.Println("Shut down flag logger")
	}
	log.Println("Local resolver provider shut down")
	p.initialized = false
}

// parseFlagPath splits a flag key into flag name and path
// e.g., "my-flag.nested.value" -> ("my-flag", "nested.value")
func parseFlagPath(key string) (flagName string, path string) {
	parts := strings.SplitN(key, ".", 2)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}

// processTargetingKey converts "targetingKey" to "targeting_key" in the context
func processTargetingKey(evalCtx openfeature.FlattenedContext) openfeature.FlattenedContext {
	newEvalContext := make(openfeature.FlattenedContext)
	for k, v := range evalCtx {
		newEvalContext[k] = v
	}

	if targetingKey, exists := evalCtx["targetingKey"]; exists {
		newEvalContext["targeting_key"] = targetingKey
		delete(newEvalContext, "targetingKey")
	}

	return newEvalContext
}

// flattenedContextToProto converts OpenFeature FlattenedContext to protobuf Struct
func flattenedContextToProto(ctx openfeature.FlattenedContext) (*structpb.Struct, error) {
	fields := make(map[string]*structpb.Value)

	for key, value := range ctx {
		protoValue, err := goValueToProto(value)
		if err != nil {
			return nil, fmt.Errorf("failed to convert field '%s': %w", key, err)
		}
		fields[key] = protoValue
	}

	return &structpb.Struct{Fields: fields}, nil
}

// goValueToProto converts a Go value to protobuf Value
func goValueToProto(value interface{}) (*structpb.Value, error) {
	switch v := value.(type) {
	case nil:
		return structpb.NewNullValue(), nil
	case bool:
		return structpb.NewBoolValue(v), nil
	case int:
		return structpb.NewNumberValue(float64(v)), nil
	case int64:
		return structpb.NewNumberValue(float64(v)), nil
	case float64:
		return structpb.NewNumberValue(v), nil
	case string:
		return structpb.NewStringValue(v), nil
	case []interface{}:
		values := make([]*structpb.Value, len(v))
		for i, item := range v {
			val, err := goValueToProto(item)
			if err != nil {
				return nil, err
			}
			values[i] = val
		}
		return structpb.NewListValue(&structpb.ListValue{Values: values}), nil
	case map[string]interface{}:
		fields := make(map[string]*structpb.Value)
		for key, val := range v {
			protoVal, err := goValueToProto(val)
			if err != nil {
				return nil, err
			}
			fields[key] = protoVal
		}
		return structpb.NewStructValue(&structpb.Struct{Fields: fields}), nil
	default:
		return nil, fmt.Errorf("unsupported type: %T", v)
	}
}

// protoStructToGo converts protobuf Struct to Go map[string]interface{}
func protoStructToGo(s *structpb.Struct) interface{} {
	if s == nil {
		return nil
	}

	result := make(map[string]interface{})
	for key, val := range s.Fields {
		result[key] = protoValueToGo(val)
	}
	return result
}

// protoValueToGo converts protobuf Value to Go interface{}
func protoValueToGo(value *structpb.Value) interface{} {
	if value == nil {
		return nil
	}

	switch v := value.Kind.(type) {
	case *structpb.Value_NullValue:
		return nil
	case *structpb.Value_BoolValue:
		return v.BoolValue
	case *structpb.Value_NumberValue:
		return v.NumberValue
	case *structpb.Value_StringValue:
		return v.StringValue
	case *structpb.Value_ListValue:
		result := make([]interface{}, len(v.ListValue.Values))
		for i, val := range v.ListValue.Values {
			result[i] = protoValueToGo(val)
		}
		return result
	case *structpb.Value_StructValue:
		result := make(map[string]interface{})
		for key, val := range v.StructValue.Fields {
			result[key] = protoValueToGo(val)
		}
		return result
	default:
		return nil
	}
}

// getValueForPath extracts a nested value from a map using dot notation
// e.g., "nested.value" from map{"nested": map{"value": 42}} returns 42
func getValueForPath(path string, value interface{}) interface{} {
	if path == "" {
		return value
	}

	parts := strings.Split(path, ".")
	current := value

	for _, part := range parts {
		switch v := current.(type) {
		case map[string]interface{}:
			current = v[part]
		default:
			return nil
		}
	}

	return current
}

// mapResolveReasonToOpenFeature converts Confidence ResolveReason to OpenFeature Reason
func mapResolveReasonToOpenFeature(reason resolvertypes.ResolveReason) openfeature.Reason {
	switch reason {
	case resolvertypes.ResolveReason_RESOLVE_REASON_MATCH:
		return openfeature.TargetingMatchReason
	case resolvertypes.ResolveReason_RESOLVE_REASON_NO_SEGMENT_MATCH:
		return openfeature.DefaultReason
	case resolvertypes.ResolveReason_RESOLVE_REASON_FLAG_ARCHIVED:
		return openfeature.DisabledReason
	case resolvertypes.ResolveReason_RESOLVE_REASON_TARGETING_KEY_ERROR:
		return openfeature.ErrorReason
	case resolvertypes.ResolveReason_RESOLVE_REASON_ERROR:
		return openfeature.ErrorReason
	default:
		return openfeature.UnknownReason
	}
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
