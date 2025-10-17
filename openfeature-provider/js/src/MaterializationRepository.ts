// ============================================================================
// WIP: MaterializationRepository - Custom Storage for Sticky Assignments
// ============================================================================
// This interface is currently in development and not yet part of the public API.
// When complete, it will allow users to provide their own storage backend
// (Redis, database, file system, etc.) for sticky assignment materializations.
// ============================================================================

import type {
  MaterializationInfo,
} from './proto/api';


/**
 * WIP: Strategy for storing and loading materialized assignments locally.
 *
 * Use this when you want to:
 * - Store assignments in a database, Redis, or other persistent storage
 * - Avoid network calls for materialization data
 * - Have full control over TTL and storage mechanism
 *
 * @example
 * ```typescript
 * class RedisMaterializationRepository implements MaterializationRepository {
 *   constructor(private redis: RedisClient) {}
 *
 *   async loadMaterializedAssignmentsForUnit(unit: string, materialization: string) {
 *     // Load ALL materializations for this unit
 *     const data = await this.redis.get(`unit:${unit}`);
 *     if (!data) {
 *       return new Map();
 *     }
 *     const parsed = JSON.parse(data);
 *     return new Map(Object.entries(parsed));
 *   }
 *
 *   async storeAssignment(unit: string, assignments: Map<string, MaterializationInfo>) {
 *     const serialized = JSON.stringify(Object.fromEntries(assignments));
 *     await this.redis.set(`unit:${unit}`, serialized, { EX: 60*60*24*90 });
 *   }
 *
 *   close(): void {
 *     this.redis.disconnect();
 *   }
 * }
 * ```
 */
export interface MaterializationRepository {
  /**
   * Load ALL stored materialization assignments for a targeting unit.
   *
   * This method loads all materialization data for the given unit at once,
   * not just a specific materialization. This allows efficient bulk loading
   * from storage.
   *
   * @param unit - The targeting key (e.g., user ID, session ID)
   * @param materialization - The materialization ID being requested (for context/filtering)
   * @returns Map of materialization ID to MaterializationInfo for this unit
   */
  loadMaterializedAssignmentsForUnit(
    unit: string,
    materialization: string
  ): Promise<Map<string, MaterializationInfo>>;

  /**
   * Store materialization assignments for a targeting unit.
   *
   * This stores all materialization info for the given unit. The map contains
   * materialization IDs as keys and their corresponding info as values.
   *
   * @param unit - The targeting key (e.g., user ID, session ID)
   * @param assignments - Map of materialization ID to MaterializationInfo
   */
  storeAssignment(
    unit: string,
    assignments: Map<string, MaterializationInfo>
  ): Promise<void>;

  /**
   * Close and cleanup any resources used by this repository.
   */
  close(): void | Promise<void>;
}

