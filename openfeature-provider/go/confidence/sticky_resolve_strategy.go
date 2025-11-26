package confidence

import (
	"context"

	"github.com/spotify/confidence-resolver/openfeature-provider/go/confidence/proto/resolver"
)

// StickyResolveStrategy is the interface for handling sticky resolve scenarios.
// Two main implementations are available:
// 1. ResolverFallback - Falls back to remote gRPC service when materializations are missing
// 2. MaterializationRepository - Loads/stores materializations locally to eliminate network calls
type StickyResolveStrategy interface {
	// Close releases any resources held by the strategy
	Close()
}

// ResolverFallback is a strategy that falls back to a remote resolver when
// materializations are missing. This is the default behavior.
type ResolverFallback interface {
	StickyResolveStrategy
	// Resolve performs flag resolution using the remote service
	Resolve(ctx context.Context, request *resolver.ResolveFlagsRequest) (*resolver.ResolveFlagsResponse, error)
}

// MaterializationInfo holds information about a materialized assignment
type MaterializationInfo struct {
	// UnitInMaterialization indicates if the unit exists in the materialization
	UnitInMaterialization bool
	// RuleToVariant maps rule IDs to their assigned variant names
	RuleToVariant map[string]string
}

// ToProto converts MaterializationInfo to its protobuf representation
func (m *MaterializationInfo) ToProto() *resolver.MaterializationInfo {
	return &resolver.MaterializationInfo{
		UnitInInfo:    m.UnitInMaterialization,
		RuleToVariant: m.RuleToVariant,
	}
}

// MaterializationRepository is a strategy that stores and retrieves materializations locally.
// Implement this interface to store assignments in your own storage (Redis, database, etc.)
// to eliminate network calls and improve latency.
type MaterializationRepository interface {
	StickyResolveStrategy

	// LoadMaterializedAssignmentsForUnit loads the materialized assignments for a unit.
	// Returns a map of materialization name to MaterializationInfo.
	// If no assignments exist for the unit, returns an empty map (not an error).
	LoadMaterializedAssignmentsForUnit(ctx context.Context, unit, materialization string) (map[string]*MaterializationInfo, error)

	// StoreAssignment stores materialization assignments for a unit.
	// The assignments map is keyed by materialization name.
	StoreAssignment(ctx context.Context, unit string, assignments map[string]*MaterializationInfo) error
}
