package testutil

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	adminv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/flags/admin/v1"
	resolverv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/flags/resolverinternal"
	iamv1 "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/confidence/iam/v1"
	"github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/resolver"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

var repoRoot string

func init() {
	// Resolve paths relative to this source file to avoid dependence on cwd.
	if _, thisFile, _, ok := runtime.Caller(0); ok {
		// helpers.go lives at: openfeature-provider/go/confidence/internal/testutil/helpers.go
		// repo root is five directories up from this file
		repoRoot = filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "..")
	} else {
		panic("failed to resolve test repo root via runtime.Caller")
	}
}

type MockFlagLogger struct {
	writeFunc    func(request *resolverv1.WriteFlagLogsRequest)
	shutdownFunc func()
}

func (m *MockFlagLogger) Shutdown() {
	if m.shutdownFunc != nil {
		m.shutdownFunc()
	}
}

func (m *MockFlagLogger) Write(request *resolverv1.WriteFlagLogsRequest) {
	m.writeFunc(request)
}

type StateProviderMock struct {
	AccountID string
	State     []byte
	Err       error
}

func (m *StateProviderMock) Provide(_ context.Context) ([]byte, string, error) {
	return m.State, m.AccountID, m.Err
}

func LoadTestResolverState(t *testing.T) []byte {
	dataPath := filepath.Join(repoRoot, "data", "resolver_state_current.pb")
	data, err := os.ReadFile(dataPath)
	if err != nil {
		t.Skipf("Skipping test - could not load test resolver state: %v", err)
	}
	return data
}

func LoadTestAccountID(t *testing.T) string {
	dataPath := filepath.Join(repoRoot, "data", "account_id")
	data, err := os.ReadFile(dataPath)
	if err != nil {
		t.Skipf("Skipping test - could not load test account ID: %v", err)
	}
	return strings.TrimSpace(string(data))
}

// Helper function to create minimal valid resolver state for testing
func CreateMinimalResolverState() []byte {
	clientName := "clients/test-client"
	credentialName := "clients/test-client/credentials/test-credential"

	state := &adminv1.ResolverState{
		Flags: []*adminv1.Flag{},
		Clients: []*iamv1.Client{
			{
				Name: clientName,
			},
		},
		ClientCredentials: []*iamv1.ClientCredential{
			{
				Name: credentialName, // Must start with client name
				Credential: &iamv1.ClientCredential_ClientSecret_{
					ClientSecret: &iamv1.ClientCredential_ClientSecret{
						Secret: "test-secret",
					},
				},
			},
		},
	}
	data, err := proto.Marshal(state)
	if err != nil {
		panic("Failed to create minimal state: " + err.Error())
	}
	return data
}

// Helper to create a resolver state with a flag that requires materializations
func CreateStateWithStickyFlag() []byte {
	state := &adminv1.ResolverState{
		Flags: []*adminv1.Flag{
			{
				Name: "flags/sticky-test-flag",
				Variants: []*adminv1.Flag_Variant{
					{
						Name: "flags/sticky-test-flag/variants/on",
						Value: &structpb.Struct{
							Fields: map[string]*structpb.Value{
								"enabled": structpb.NewBoolValue(true),
							},
						},
					},
					{
						Name: "flags/sticky-test-flag/variants/off",
						Value: &structpb.Struct{
							Fields: map[string]*structpb.Value{
								"enabled": structpb.NewBoolValue(false),
							},
						},
					},
				},
				State: adminv1.Flag_ACTIVE,
				// Associate this flag with the test client
				Clients: []string{"clients/test-client"},
				Rules: []*adminv1.Flag_Rule{
					{
						Name:                 "flags/sticky-test-flag/rules/sticky-rule",
						Segment:              "segments/always-true",
						TargetingKeySelector: "user_id",
						Enabled:              true,
						AssignmentSpec: &adminv1.Flag_Rule_AssignmentSpec{
							BucketCount: 10000,
							Assignments: []*adminv1.Flag_Rule_Assignment{
								{
									AssignmentId: "variant-assignment",
									Assignment: &adminv1.Flag_Rule_Assignment_Variant{
										Variant: &adminv1.Flag_Rule_Assignment_VariantAssignment{
											Variant: "flags/sticky-test-flag/variants/on",
										},
									},
									BucketRanges: []*adminv1.Flag_Rule_BucketRange{
										{
											Upper: 10000,
										},
									},
								},
							},
						},
						// This rule requires a materialization named "experiment_v1"
						MaterializationSpec: &adminv1.Flag_Rule_MaterializationSpec{
							ReadMaterialization:  "experiment_v1",
							WriteMaterialization: "experiment_v1",
							Mode: &adminv1.Flag_Rule_MaterializationSpec_MaterializationReadMode{
								MaterializationMustMatch:     false,
								SegmentTargetingCanBeIgnored: false,
							},
						},
					},
				},
			},
		},
		SegmentsNoBitsets: []*adminv1.Segment{
			{
				Name: "segments/always-true",
				// Empty segment - may not match any users
			},
		},
		Clients: []*iamv1.Client{
			{
				Name: "clients/test-client",
			},
		},
		ClientCredentials: []*iamv1.ClientCredential{
			{
				// ClientCredential name must start with the client name
				Name: "clients/test-client/credentials/test-credential",
				Credential: &iamv1.ClientCredential_ClientSecret_{
					ClientSecret: &iamv1.ClientCredential_ClientSecret{
						Secret: "test-secret",
					},
				},
			},
		},
	}
	data, err := proto.Marshal(state)
	if err != nil {
		panic("Failed to create state with sticky flag: " + err.Error())
	}
	return data
}

// Helper function to create a ResolveWithStickyRequest
func CreateResolveWithStickyRequest(
	resolveRequest *resolver.ResolveFlagsRequest,
	materializations map[string]*resolver.MaterializationMap,
	failFast bool,
	notProcessSticky bool,
) *resolver.ResolveWithStickyRequest {
	if materializations == nil {
		materializations = make(map[string]*resolver.MaterializationMap)
	}
	return &resolver.ResolveWithStickyRequest{
		ResolveRequest:          resolveRequest,
		MaterializationsPerUnit: materializations,
		FailFastOnSticky:        failFast,
		NotProcessSticky:        notProcessSticky,
	}
}

// Helper function to create a tutorial-feature resolve request with standard test data
func CreateTutorialFeatureRequest() *resolver.ResolveFlagsRequest {
	return &resolver.ResolveFlagsRequest{
		Flags:        []string{"flags/tutorial-feature"},
		Apply:        true,
		ClientSecret: "mkjJruAATQWjeY7foFIWfVAcBWnci2YF",
		EvaluationContext: &structpb.Struct{
			Fields: map[string]*structpb.Value{
				"visitor_id": structpb.NewStringValue("tutorial_visitor"),
			},
		},
	}
}
