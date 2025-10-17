// ============================================================================
// WIP: Materialization Utilities - Helper functions for MaterializationRepository
// ============================================================================
// These utilities are currently in development and not yet part of the public API.
// They support the MaterializationRepository feature for custom sticky assignment storage.
// ============================================================================

import type {
  MaterializationInfo,
  MaterializationMap,
  ResolveWithStickyRequest,
  ResolveWithStickyResponse_MaterializationUpdate,
  ResolveWithStickyResponse_MissingMaterializationItem,
} from './proto/api';
import type { MaterializationRepository } from './MaterializationRepository';

/**
 * WIP: Handle missing materializations by loading from repository and building updated request.
 * Matches Java implementation logic.
 */
export async function handleMissingMaterializations(
  request: ResolveWithStickyRequest,
  items: ResolveWithStickyResponse_MissingMaterializationItem[],
  repository: MaterializationRepository
): Promise<ResolveWithStickyRequest> {
  // Group missing items by unit for efficient loading
  const missingByUnit = new Map<string, ResolveWithStickyResponse_MissingMaterializationItem[]>();
  for (const item of items) {
    if (!missingByUnit.has(item.unit)) {
      missingByUnit.set(item.unit, []);
    }
    missingByUnit.get(item.unit)!.push(item);
  }

  const materializationPerUnitMap: { [key: string]: MaterializationMap } = {};

  // Load materialized assignments for all missing units
  for (const [unit, materializationInfoItems] of missingByUnit) {
    for (const item of materializationInfoItems) {
      try {
        // Load ALL assignments for this unit
        const loadedAssignments = await repository.loadMaterializedAssignmentsForUnit(
          unit,
          item.readMaterialization
        );

        // Initialize map for this unit if not exists
        if (!materializationPerUnitMap[unit]) {
          materializationPerUnitMap[unit] = { infoMap: {} };
        }

        // Merge loaded assignments into the map
        for (const [materializationId, info] of loadedAssignments) {
          materializationPerUnitMap[unit].infoMap[materializationId] = info;
        }
      } catch (error) {
        throw new Error(
          `Failed to load materializations for unit ${unit}: ${error instanceof Error ? error.message : String(error)}`
        );
      }
    }
  }

  // Return new request with updated materialization context
  // Merge with existing materializations from the request
  return {
    ...request,
    materializationsPerUnit: {
      ...request.materializationsPerUnit,
      ...materializationPerUnitMap
    },
    failFastOnSticky: false  // Don't fail fast on retry
  };
}

/**
 * Store materialization updates using the repository.
 * Groups updates by unit before storing.
 *
 * Runs asynchronously without blocking the main resolve path.
 * Errors are logged but don't propagate to avoid affecting flag resolution.
 */
export function storeUpdates(
  updates: ResolveWithStickyResponse_MaterializationUpdate[],
  repository: MaterializationRepository
): void {
  if (updates.length === 0) {
    return;
  }

  // Run async without blocking - fire-and-forget
  void (async () => {
    try {
      const updatesByUnit = new Map<string, ResolveWithStickyResponse_MaterializationUpdate[]>();
      for (const update of updates) {
        if (!updatesByUnit.has(update.unit)) {
          updatesByUnit.set(update.unit, []);
        }
        updatesByUnit.get(update.unit)!.push(update);
      }

      // Store assignments for each unit
      for (const [unit, unitUpdates] of updatesByUnit) {
        const assignments = new Map<string, MaterializationInfo>();

        for (const update of unitUpdates) {
          const ruleToVariant = { [update.rule]: update.variant };
          assignments.set(update.writeMaterialization, {
            unitInInfo: true,
            ruleToVariant
          });
        }

        try {
          await repository.storeAssignment(unit, assignments);
        } catch (error) {
          // Log error but don't propagate to avoid affecting main resolve path
          console.error(
            `Failed to store materialization updates for unit ${unit}:`,
            error instanceof Error ? error.message : String(error)
          );
        }
      }
    } catch (error) {
      // Catch any unexpected errors in the grouping logic
      console.error('Failed to store materialization updates:', error);
    }
  })();
}
