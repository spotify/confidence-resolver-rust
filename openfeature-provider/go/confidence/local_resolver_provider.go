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

const defaultPollIntervalSeconds = 30

// LocalResolverProvider implements the OpenFeature FeatureProvider interface
// for local flag resolution using the Confidence WASM resolver
type LocalResolverProvider struct {
	resolverAPI          WasmResolverApi
	stateProvider        StateProvider
	flagLogger           FlagLogger
	clientSecret         string
	logger               *slog.Logger
	cancelFunc           context.CancelFunc
	wg                   sync.WaitGroup
	mu                   sync.Mutex
	pollInterval         time.Duration
	materializationStore MaterializationStore
}

// Compile-time interface conformance checks
var (
	_ openfeature.FeatureProvider = (*LocalResolverProvider)(nil)
	_ openfeature.StateHandler    = (*LocalResolverProvider)(nil)
)

// NewLocalResolverProvider creates a new LocalResolverProvider
func NewLocalResolverProvider(
	resolverAPI WasmResolverApi,
	stateProvider StateProvider,
	flagLogger FlagLogger,
	clientSecret string,
	logger *slog.Logger,
	materializationStore MaterializationStore,
) *LocalResolverProvider {
	// Create a default logger if none provided
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		}))
	}

	return &LocalResolverProvider{
		resolverAPI:          resolverAPI,
		stateProvider:        stateProvider,
		flagLogger:           flagLogger,
		clientSecret:         clientSecret,
		logger:               logger,
		pollInterval:         getPollIntervalSeconds(),
		materializationStore: materializationStore,
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

	var detail openfeature.BoolResolutionDetail

	if result.Value == nil {
		detail = openfeature.BoolResolutionDetail{
			Value: defaultValue,
			ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
				Reason:          result.Reason,
				ResolutionError: result.ResolutionError,
			},
		}
	} else if boolVal, ok := result.Value.(bool); !ok {
		detail = openfeature.BoolResolutionDetail{
			Value: defaultValue,
			ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
				Reason:          openfeature.ErrorReason,
				ResolutionError: openfeature.NewTypeMismatchResolutionError("value is not a boolean"),
			},
		}
	} else {
		detail = openfeature.BoolResolutionDetail{
			Value:                    boolVal,
			ProviderResolutionDetail: result.ProviderResolutionDetail,
		}
	}

	p.logResolutionErrorIfPresent(flag, detail.ProviderResolutionDetail)
	return detail
}

// StringEvaluation evaluates a string flag
func (p *LocalResolverProvider) StringEvaluation(
	ctx context.Context,
	flag string,
	defaultValue string,
	evalCtx openfeature.FlattenedContext,
) openfeature.StringResolutionDetail {
	result := p.ObjectEvaluation(ctx, flag, defaultValue, evalCtx)

	var detail openfeature.StringResolutionDetail

	if result.Value == nil {
		detail = openfeature.StringResolutionDetail{
			Value: defaultValue,
			ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
				Reason:          result.Reason,
				ResolutionError: result.ResolutionError,
			},
		}
	} else if strVal, ok := result.Value.(string); !ok {
		detail = openfeature.StringResolutionDetail{
			Value: defaultValue,
			ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
				Reason:          openfeature.ErrorReason,
				ResolutionError: openfeature.NewTypeMismatchResolutionError("value is not a string"),
			},
		}
	} else {
		detail = openfeature.StringResolutionDetail{
			Value:                    strVal,
			ProviderResolutionDetail: result.ProviderResolutionDetail,
		}
	}

	p.logResolutionErrorIfPresent(flag, detail.ProviderResolutionDetail)
	return detail
}

// FloatEvaluation evaluates a float flag
func (p *LocalResolverProvider) FloatEvaluation(
	ctx context.Context,
	flag string,
	defaultValue float64,
	evalCtx openfeature.FlattenedContext,
) openfeature.FloatResolutionDetail {
	result := p.ObjectEvaluation(ctx, flag, defaultValue, evalCtx)

	var detail openfeature.FloatResolutionDetail

	if result.Value == nil {
		detail = openfeature.FloatResolutionDetail{
			Value: defaultValue,
			ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
				Reason:          result.Reason,
				ResolutionError: result.ResolutionError,
			},
		}
	} else if floatVal, ok := result.Value.(float64); !ok {
		detail = openfeature.FloatResolutionDetail{
			Value: defaultValue,
			ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
				Reason:          openfeature.ErrorReason,
				ResolutionError: openfeature.NewTypeMismatchResolutionError("value is not a float"),
			},
		}
	} else {
		detail = openfeature.FloatResolutionDetail{
			Value:                    floatVal,
			ProviderResolutionDetail: result.ProviderResolutionDetail,
		}
	}

	p.logResolutionErrorIfPresent(flag, detail.ProviderResolutionDetail)
	return detail
}

// IntEvaluation evaluates an int flag
func (p *LocalResolverProvider) IntEvaluation(
	ctx context.Context,
	flag string,
	defaultValue int64,
	evalCtx openfeature.FlattenedContext,
) openfeature.IntResolutionDetail {
	result := p.ObjectEvaluation(ctx, flag, defaultValue, evalCtx)

	var detail openfeature.IntResolutionDetail

	if result.Value == nil {
		detail = openfeature.IntResolutionDetail{
			Value: defaultValue,
			ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
				Reason:          result.Reason,
				ResolutionError: result.ResolutionError,
			},
		}
	} else {
		// Handle both int64 and float64 (JSON numbers are float64)
		switch v := result.Value.(type) {
		case int64:
			detail = openfeature.IntResolutionDetail{
				Value:                    v,
				ProviderResolutionDetail: result.ProviderResolutionDetail,
			}
		case float64:
			detail = openfeature.IntResolutionDetail{
				Value:                    int64(v),
				ProviderResolutionDetail: result.ProviderResolutionDetail,
			}
		default:
			detail = openfeature.IntResolutionDetail{
				Value: defaultValue,
				ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
					Reason:          openfeature.ErrorReason,
					ResolutionError: openfeature.NewTypeMismatchResolutionError("value is not an integer"),
				},
			}
		}
	}

	p.logResolutionErrorIfPresent(flag, detail.ProviderResolutionDetail)
	return detail
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
		Sdk: &resolvertypes.Sdk{
			Sdk: &resolvertypes.Sdk_Id{
				Id: resolvertypes.SdkId_SDK_ID_GO_LOCAL_PROVIDER,
			},
			Version: Version,
		},
	}

	// Create ResolveWithSticky request
	stickyRequest := &resolver.ResolveWithStickyRequest{
		ResolveRequest:          request,
		MaterializationsPerUnit: make(map[string]*resolver.MaterializationMap),
		FailFastOnSticky:        false,
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

	// Handle the sticky response
	response, err := p.handleStickyResponse(ctx, stickyRequest, stickyResponse)
	if err != nil {
		p.logger.Error("Failed to handle sticky response", "flag", flagPath, "error", err)
		return openfeature.InterfaceResolutionDetail{
			Value: defaultValue,
			ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
				Reason:          openfeature.ErrorReason,
				ResolutionError: openfeature.NewGeneralResolutionError(fmt.Sprintf("resolve failed: %v", err)),
			},
		}
	}

	// Check if flag was found
	if len(response.ResolvedFlags) == 0 {
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
		var found bool
		value, found = getValueForPath(path, value)
		// If path was specified but not found, return FLAG_NOT_FOUND error
		if !found {
			return openfeature.InterfaceResolutionDetail{
				Value: defaultValue,
				ProviderResolutionDetail: openfeature.ProviderResolutionDetail{
					Reason:          openfeature.ErrorReason,
					ResolutionError: openfeature.NewFlagNotFoundResolutionError(fmt.Sprintf("path '%s' not found in flag '%s'", path, flagPath)),
				},
			}
		}
	}

	// If value is nil (flag has no value), use default
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
		p.logger.Error("AccountID is empty in the fetched state, this should not happen")
		return fmt.Errorf("AccountID is empty in the initial state")
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
			p.logger.Debug("Cancelled scheduled tasks")
		}
	}

	// Wait for background goroutines to exit
	p.wg.Wait()

	ctx := context.Background()

	// Close resolver API (which flushes final logs)
	if p.resolverAPI != nil {
		p.resolverAPI.Close(ctx)
		if p.logger != nil {
			p.logger.Debug("Closed resolver API")
		}
	}

	// Shutdown flag logger (which waits for log sends to complete)
	if p.flagLogger != nil {
		p.flagLogger.Shutdown()
		if p.logger != nil {
			p.logger.Debug("Shut down flag logger")
		}
	}

	if p.logger != nil {
		p.logger.Info("Provider has been shut down")
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
					p.logger.Error("AccountID inside fetched state is empty, skipping this state update attempt")
					continue
				}

				// Update state and flush logs
				if err := p.resolverAPI.UpdateStateAndFlushLogs(state, accountId); err != nil {
					p.logger.Error("Failed to update state and flush logs", "error", err)
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
// Returns (value, found) where found indicates if the path was fully traversed
func getValueForPath(path string, value interface{}) (interface{}, bool) {
	if path == "" {
		return value, true
	}

	parts := strings.Split(path, ".")
	current := value

	for _, part := range parts {
		switch v := current.(type) {
		case map[string]interface{}:
			var exists bool
			current, exists = v[part]
			if !exists {
				return nil, false
			}
		default:
			// Can't traverse further - path not found
			return nil, false
		}
	}

	return current, true
}

// logResolutionErrorIfPresent logs a warning if the resolution detail contains an error
func (p *LocalResolverProvider) logResolutionErrorIfPresent(flag string, detail openfeature.ProviderResolutionDetail) {
	errStr := detail.ResolutionError.Error()
	// Empty ResolutionError returns ": ", so check for meaningful error
	if errStr != "" && errStr != ": " {
		p.logger.Warn("Flag evaluation error", "flag", flag, "error_code", errStr)
	}
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

// handleStickyResponse processes the sticky response and returns the actual resolve response
func (p *LocalResolverProvider) handleStickyResponse(
	ctx context.Context,
	request *resolver.ResolveWithStickyRequest,
	stickyResponse *resolver.ResolveWithStickyResponse,
) (*resolver.ResolveFlagsResponse, error) {
	switch result := stickyResponse.ResolveResult.(type) {
	case *resolver.ResolveWithStickyResponse_Success_:
		success := result.Success
		// Store updates if present
		if len(success.GetUpdates()) > 0 {
			p.storeUpdates(ctx, success.GetUpdates())
		}
		return success.Response, nil

	case *resolver.ResolveWithStickyResponse_MissingMaterializations_:
		missingMaterializations := result.MissingMaterializations
		// Try to load missing materializations from store
		updatedRequest, err := p.handleMissingMaterializations(ctx, request, missingMaterializations.GetItems())
		if err != nil {
			return nil, fmt.Errorf("failed to handle missing materializations: %w", err)
		}
		// Retry with the updated request
		retryResponse, err := p.resolverAPI.ResolveWithSticky(updatedRequest)
		if err != nil {
			return nil, err
		}
		// Recursively handle the response (in case there are more missing materializations)
		return p.handleStickyResponse(ctx, updatedRequest, retryResponse)

	default:
		return nil, fmt.Errorf("unexpected resolve result type: %T", stickyResponse.ResolveResult)
	}
}

// storeUpdates stores materialization updates asynchronously
func (p *LocalResolverProvider) storeUpdates(ctx context.Context, updates []*resolver.ResolveWithStickyResponse_MaterializationUpdate) {
	// Convert protobuf updates to WriteOp slice
	writeOps := make([]WriteOp, len(updates))
	for i, update := range updates {
		writeOps[i] = NewWriteOpVariant(
			update.GetWriteMaterialization(),
			update.GetUnit(),
			update.GetRule(),
			update.GetVariant(),
		)
	}

	// Store updates asynchronously
	go func() {
		if err := p.materializationStore.Write(ctx, writeOps); err != nil {
			// Check if it's an unsupported operation error (expected for UnsupportedMaterializationStore)
			if _, ok := err.(*MaterializationNotSupportedError); !ok {
				p.logger.Error("Failed to store materialization updates", "error", err)
			}
		}
	}()
}

// handleMissingMaterializations loads missing materializations from the store
// and returns an updated request with the materializations added
func (p *LocalResolverProvider) handleMissingMaterializations(
	ctx context.Context,
	request *resolver.ResolveWithStickyRequest,
	missingItems []*resolver.ResolveWithStickyResponse_MissingMaterializationItem,
) (*resolver.ResolveWithStickyRequest, error) {
	// Convert missing items to ReadOp slice
	readOps := make([]ReadOp, len(missingItems))
	for i, item := range missingItems {
		readOps[i] = NewReadOpVariant(
			item.GetReadMaterialization(),
			item.GetUnit(),
			item.GetRule(),
		)
	}

	// Read from the store
	results, err := p.materializationStore.Read(ctx, readOps)
	if err != nil {
		return nil, err
	}

	// Convert results to protobuf MaterializationMap format
	// Group by unit for efficiency
	materializationsPerUnit := make(map[string]*resolver.MaterializationMap)

	// Copy existing materializations
	for k, v := range request.GetMaterializationsPerUnit() {
		materializationsPerUnit[k] = v
	}

	// Add loaded materializations
	for _, result := range results {
		variantResult, ok := result.(*ReadResultVariant)
		if !ok {
			continue
		}

		unit := variantResult.Unit()
		mat := variantResult.Materialization()

		// Ensure the map exists for this unit
		if materializationsPerUnit[unit] == nil {
			materializationsPerUnit[unit] = &resolver.MaterializationMap{
				InfoMap: make(map[string]*resolver.MaterializationInfo),
			}
		}

		// Get or create the info for this materialization
		if materializationsPerUnit[unit].InfoMap[mat] == nil {
			materializationsPerUnit[unit].InfoMap[mat] = &resolver.MaterializationInfo{
				UnitInInfo:    false,
				RuleToVariant: make(map[string]string),
			}
		}

		// Add the variant if it exists
		if variantResult.Variant() != nil {
			materializationsPerUnit[unit].InfoMap[mat].RuleToVariant[variantResult.Rule()] = *variantResult.Variant()
			materializationsPerUnit[unit].InfoMap[mat].UnitInInfo = true
		}
	}

	// Create a new request with the updated materializations
	return &resolver.ResolveWithStickyRequest{
		ResolveRequest:          request.GetResolveRequest(),
		MaterializationsPerUnit: materializationsPerUnit,
		FailFastOnSticky:        request.GetFailFastOnSticky(),
		NotProcessSticky:        request.GetNotProcessSticky(),
	}, nil
}
