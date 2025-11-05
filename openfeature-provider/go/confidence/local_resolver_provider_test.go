package confidence

import (
	"context"
	"testing"

	"github.com/open-feature/go-sdk/openfeature"
	resolverv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/flags/resolverinternal"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestNewLocalResolverProvider(t *testing.T) {
	factory := &LocalResolverFactory{}
	provider := NewLocalResolverProvider(factory, "test-secret")

	if provider == nil {
		t.Fatal("Expected provider to be created, got nil")
	}
	if provider.clientSecret != "test-secret" {
		t.Errorf("Expected client secret to be 'test-secret', got %s", provider.clientSecret)
	}
	if provider.factory != factory {
		t.Error("Expected factory to be set correctly")
	}
}

func TestLocalResolverProvider_Metadata(t *testing.T) {
	provider := NewLocalResolverProvider(&LocalResolverFactory{}, "secret")
	metadata := provider.Metadata()

	if metadata.Name != "confidence-sdk-go-local" {
		t.Errorf("Expected provider name to be 'confidence-sdk-go-local', got %s", metadata.Name)
	}
}

func TestLocalResolverProvider_Hooks(t *testing.T) {
	provider := NewLocalResolverProvider(&LocalResolverFactory{}, "secret")
	hooks := provider.Hooks()

	if hooks == nil {
		t.Error("Expected hooks to not be nil")
	}
	if len(hooks) != 0 {
		t.Errorf("Expected 0 hooks, got %d", len(hooks))
	}
}

func TestParseFlagPath(t *testing.T) {
	testCases := []struct {
		name         string
		input        string
		expectedFlag string
		expectedPath string
	}{
		{
			name:         "Simple flag name",
			input:        "my-flag",
			expectedFlag: "my-flag",
			expectedPath: "",
		},
		{
			name:         "Flag with path",
			input:        "my-flag.nested.value",
			expectedFlag: "my-flag",
			expectedPath: "nested.value",
		},
		{
			name:         "Flag with single level path",
			input:        "flag.value",
			expectedFlag: "flag",
			expectedPath: "value",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			flag, path := parseFlagPath(tc.input)
			if flag != tc.expectedFlag {
				t.Errorf("Expected flag '%s', got '%s'", tc.expectedFlag, flag)
			}
			if path != tc.expectedPath {
				t.Errorf("Expected path '%s', got '%s'", tc.expectedPath, path)
			}
		})
	}
}

func TestProcessTargetingKey(t *testing.T) {
	testCases := []struct {
		name     string
		input    openfeature.FlattenedContext
		expected map[string]interface{}
	}{
		{
			name: "Converts targetingKey to targeting_key",
			input: openfeature.FlattenedContext{
				"targetingKey": "user-123",
				"other":        "value",
			},
			expected: map[string]interface{}{
				"targeting_key": "user-123",
				"other":         "value",
			},
		},
		{
			name: "No targetingKey",
			input: openfeature.FlattenedContext{
				"other": "value",
			},
			expected: map[string]interface{}{
				"other": "value",
			},
		},
		{
			name:     "Empty context",
			input:    openfeature.FlattenedContext{},
			expected: map[string]interface{}{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := processTargetingKey(tc.input)

			if len(result) != len(tc.expected) {
				t.Errorf("Expected %d keys, got %d", len(tc.expected), len(result))
			}

			for key, expectedValue := range tc.expected {
				if result[key] != expectedValue {
					t.Errorf("Expected key '%s' to have value '%v', got '%v'", key, expectedValue, result[key])
				}
			}

			// Ensure targetingKey is not present if it was in input
			if _, hasTargetingKey := tc.input["targetingKey"]; hasTargetingKey {
				if _, stillHas := result["targetingKey"]; stillHas {
					t.Error("Expected targetingKey to be removed")
				}
			}
		})
	}
}

func TestGoValueToProto(t *testing.T) {
	testCases := []struct {
		name        string
		input       interface{}
		expectError bool
	}{
		{
			name:        "Nil value",
			input:       nil,
			expectError: false,
		},
		{
			name:        "Bool value",
			input:       true,
			expectError: false,
		},
		{
			name:        "Int value",
			input:       42,
			expectError: false,
		},
		{
			name:        "Int64 value",
			input:       int64(42),
			expectError: false,
		},
		{
			name:        "Float64 value",
			input:       3.14,
			expectError: false,
		},
		{
			name:        "String value",
			input:       "hello",
			expectError: false,
		},
		{
			name:        "Array value",
			input:       []interface{}{1, "two", true},
			expectError: false,
		},
		{
			name: "Map value",
			input: map[string]interface{}{
				"key": "value",
			},
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := goValueToProto(tc.input)
			if tc.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
				if result == nil {
					t.Error("Expected result to not be nil")
				}
			}
		})
	}
}

func TestProtoValueToGo(t *testing.T) {
	testCases := []struct {
		name     string
		input    *structpb.Value
		expected interface{}
	}{
		{
			name:     "Nil value",
			input:    nil,
			expected: nil,
		},
		{
			name:     "Null value",
			input:    structpb.NewNullValue(),
			expected: nil,
		},
		{
			name:     "Bool value",
			input:    structpb.NewBoolValue(true),
			expected: true,
		},
		{
			name:     "Number value",
			input:    structpb.NewNumberValue(42.5),
			expected: 42.5,
		},
		{
			name:     "String value",
			input:    structpb.NewStringValue("hello"),
			expected: "hello",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := protoValueToGo(tc.input)
			if result != tc.expected {
				t.Errorf("Expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestProtoStructToGo(t *testing.T) {
	// Test nil struct
	result := protoStructToGo(nil)
	if result != nil {
		t.Error("Expected nil result for nil struct")
	}

	// Test struct with values
	pbStruct := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"name":   structpb.NewStringValue("test"),
			"count":  structpb.NewNumberValue(42),
			"active": structpb.NewBoolValue(true),
		},
	}

	result = protoStructToGo(pbStruct)
	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("Expected result to be map[string]interface{}")
	}

	if resultMap["name"] != "test" {
		t.Errorf("Expected name to be 'test', got %v", resultMap["name"])
	}
	if resultMap["count"] != 42.0 {
		t.Errorf("Expected count to be 42.0, got %v", resultMap["count"])
	}
	if resultMap["active"] != true {
		t.Errorf("Expected active to be true, got %v", resultMap["active"])
	}
}

func TestGetValueForPath(t *testing.T) {
	testData := map[string]interface{}{
		"level1": map[string]interface{}{
			"level2": map[string]interface{}{
				"level3": "deep-value",
			},
			"simple": "simple-value",
		},
		"top": "top-value",
	}

	testCases := []struct {
		name       string
		path       string
		expected   interface{}
		checkIsMap bool
	}{
		{
			name:       "Empty path",
			path:       "",
			expected:   nil, // Will check map separately
			checkIsMap: true,
		},
		{
			name:     "Top level value",
			path:     "top",
			expected: "top-value",
		},
		{
			name:     "Nested value",
			path:     "level1.simple",
			expected: "simple-value",
		},
		{
			name:     "Deep nested value",
			path:     "level1.level2.level3",
			expected: "deep-value",
		},
		{
			name:     "Non-existent path",
			path:     "does.not.exist",
			expected: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := getValueForPath(tc.path, testData)
			if tc.checkIsMap {
				// For empty path, should return the original map
				if _, ok := result.(map[string]interface{}); !ok {
					t.Errorf("Expected result to be a map, got %T", result)
				}
			} else {
				if result != tc.expected {
					t.Errorf("Expected %v, got %v", tc.expected, result)
				}
			}
		})
	}
}

func TestGetValueForPath_NonMapValue(t *testing.T) {
	// Test with non-map value in path
	testData := map[string]interface{}{
		"value": "string-value",
	}

	result := getValueForPath("value.nested", testData)
	if result != nil {
		t.Errorf("Expected nil for path through non-map value, got %v", result)
	}
}

func TestFlattenedContextToProto(t *testing.T) {
	ctx := openfeature.FlattenedContext{
		"string": "value",
		"number": 42,
		"bool":   true,
	}

	result, err := flattenedContextToProto(ctx)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if len(result.Fields) != 3 {
		t.Errorf("Expected 3 fields, got %d", len(result.Fields))
	}
}

func TestFlattenedContextToProto_InvalidValue(t *testing.T) {
	ctx := openfeature.FlattenedContext{
		"invalid": make(chan int), // Channels cannot be converted
	}

	_, err := flattenedContextToProto(ctx)
	if err == nil {
		t.Error("Expected error for invalid value type")
	}
}

func TestLocalResolverProvider_Shutdown(t *testing.T) {
	factory := &LocalResolverFactory{
		cancelFunc: func() {
			// Shutdown called
		},
	}

	provider := NewLocalResolverProvider(factory, "secret")
	provider.Shutdown()

	// Note: The actual shutdown behavior depends on the factory implementation
	// This just verifies the method exists and can be called without panicking
}

// mockWasmFlagLogger is a mock implementation for testing shutdown behavior
type mockWasmFlagLogger struct {
	shutdownCalled bool
}

func (m *mockWasmFlagLogger) Write(ctx context.Context, request *resolverv1.WriteFlagLogsRequest) error {
	return nil
}

func (m *mockWasmFlagLogger) Shutdown() {
	m.shutdownCalled = true
}

func TestLocalResolverProvider_ShutdownFlushesLogs(t *testing.T) {
	mockLogger := &mockWasmFlagLogger{}
	cancelCalled := false

	factory := &LocalResolverFactory{
		flagLogger: mockLogger,
		cancelFunc: func() {
			cancelCalled = true
		},
	}

	provider := NewLocalResolverProvider(factory, "secret")

	// Shutdown should propagate to the factory
	provider.Shutdown()

	// Verify that shutdown was called on all components
	if !cancelCalled {
		t.Error("Expected cancel function to be called")
	}

	if !mockLogger.shutdownCalled {
		t.Error("Expected flag logger Shutdown to be called, which flushes pending logs")
	}
}
