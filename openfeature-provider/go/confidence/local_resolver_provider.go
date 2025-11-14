package confidence

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/open-feature/go-sdk/openfeature"
	resolvertypes "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/flags/resolvertypes"
	"github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/resolver"
	"google.golang.org/protobuf/types/known/structpb"
)

const defaultPollIntervalSeconds = 10

// LocalResolverProvider implements the OpenFeature FeatureProvider interface
// for local flag resolution using the Confidence WASM resolver
type LocalResolverProvider struct {
	resolverAPI   WasmResolverApi
	stateProvider StateProvider
	flagLogger    WasmFlagLogger
	clientSecret  string
	logger        *slog.Logger
	cancelFunc    context.CancelFunc
	wg            sync.WaitGroup
	mu            sync.Mutex
	pollInterval  time.Duration
}

// NewLocalResolverProvider creates a new LocalResolverProvider
func NewLocalResolverProvider(
	resolverAPI WasmResolverApi,
	stateProvider StateProvider,
	flagLogger WasmFlagLogger,
	clientSecret string,
	logger *slog.Logger,
) *LocalResolverProvider {
	// Create a default logger if none provided
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
	}

	return &LocalResolverProvider{
		resolverAPI:   resolverAPI,
		stateProvider: stateProvider,
		flagLogger:    flagLogger,
		clientSecret:  clientSecret,
		logger:        logger,
		pollInterval:  getPollIntervalSeconds(),
	}
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
		p.logger.Error("Failed to convert evaluation context to proto", "error", err)
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

	// Create ResolveWithSticky request
	stickyRequest := &resolver.ResolveWithStickyRequest{
		ResolveRequest:          request,
		MaterializationsPerUnit: make(map[string]*resolver.MaterializationMap),
		FailFastOnSticky:        true,
		NotProcessSticky:        false,
	}

	// Resolve flags with sticky support
	stickyResponse, err := p.resolverAPI.ResolveWithSticky(stickyRequest)
	if err != nil {
		p.logger.Error("Failed to resolve flag", "flag", flagPath, "error", err)
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
		p.logger.Error("Missing materializations for flag", "flag", flagPath)
		return openfeature.InterfaceResolutionDetail{
			Value: defaultValue,
			ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
				Reason:          openfeature.ErrorReason,
				ResolutionError: openfeature.NewGeneralResolutionError("missing materializations"),
			},
		}
	default:
		p.logger.Error("Unexpected resolve result type for flag", "flag", flagPath)
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
		p.logger.Info("No active flag was found", "flag", flagPath)
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
		p.logger.Error("Unexpected flag from resolver", "expected", requestFlagName, "got", resolvedFlag.Flag)
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
// Fetches initial state and starts background tasks for state updates and log flushing
func (p *LocalResolverProvider) Init(evaluationContext openfeature.EvaluationContext) error {
	ctx := context.Background()

	// Check if required components are present
	if p.stateProvider == nil {
		return fmt.Errorf("state provider is nil, cannot initialize")
	}

	if p.resolverAPI == nil {
		return fmt.Errorf("resolver API is nil, cannot initialize")
	}

	// Fetch initial state and accountID from StateProvider
	initialState, accountId, err := p.stateProvider.Provide(ctx)
	if err != nil {
		p.logger.Error("Failed to fetch initial state", "error", err)
		return fmt.Errorf("failed to fetch initial state: %w", err)
	}

	if accountId == "" {
		p.logger.Warn("AccountID is empty after state fetch")
		accountId = "unknown"
	}

	// Update resolver with initial state (triggers WASM compilation and initialization)
	if err := p.resolverAPI.UpdateStateAndFlushLogs(initialState, accountId); err != nil {
		p.logger.Error("Failed to initialize resolver with initial state", "error", err)
		return fmt.Errorf("failed to initialize resolver: %w", err)
	}

	// Start background tasks for state updates and log flushing
	p.startScheduledTasks(ctx)

	p.logger.Info("Provider initialized successfully")
	return nil
}

// Shutdown closes the provider and cleans up resources (part of StateHandler interface)
func (p *LocalResolverProvider) Shutdown() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.logger != nil {
		p.logger.Info("Shutting down provider")
	}

	// Cancel background tasks
	if p.cancelFunc != nil {
		p.cancelFunc()
		p.cancelFunc = nil
		if p.logger != nil {
			p.logger.Info("Cancelled scheduled tasks")
		}
	}

	// Wait for background goroutines to exit
	p.wg.Wait()

	ctx := context.Background()

	// Close resolver API (which flushes final logs)
	if p.resolverAPI != nil {
		p.resolverAPI.Close(ctx)
		if p.logger != nil {
			p.logger.Info("Closed resolver API")
		}
	}

	// Shutdown flag logger (which waits for log sends to complete)
	if p.flagLogger != nil {
		p.flagLogger.Shutdown()
		if p.logger != nil {
			p.logger.Info("Shut down flag logger")
		}
	}

	if p.logger != nil {
		p.logger.Info("Provider shut down")
	}
}

// startScheduledTasks starts the background tasks for state fetching and log polling
func (p *LocalResolverProvider) startScheduledTasks(parentCtx context.Context) {
	ctx, cancel := context.WithCancel(parentCtx)
	p.mu.Lock()
	p.cancelFunc = cancel
	p.mu.Unlock()

	// Ticker for state fetching and log flushing
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		ticker := time.NewTicker(p.pollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// Fetch latest state and accountID
				state, accountId, err := p.stateProvider.Provide(ctx)
				if err != nil {
					p.logger.Error("State fetch failed", "error", err)
					continue
				}

				if accountId == "" {
					p.logger.Warn("AccountID is empty, skipping state update")
					continue
				}

				// Update state and flush logs
				if err := p.resolverAPI.UpdateStateAndFlushLogs(state, accountId); err != nil {
					p.logger.Error("Failed to update state and flush logs", "error", err)
				} else {
					p.logger.Info("Updated resolver state and flushed logs", "account", accountId)
				}
			case <-ctx.Done():
				return
			}
		}
	}()
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
