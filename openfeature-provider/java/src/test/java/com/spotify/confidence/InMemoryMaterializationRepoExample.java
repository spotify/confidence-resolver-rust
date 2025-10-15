package com.spotify.confidence;

import java.util.HashMap;
import java.util.Map;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ConcurrentHashMap;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

public class InMemoryMaterializationRepoExample implements MaterializationRepository {

  private static final Logger logger =
      LoggerFactory.getLogger(InMemoryMaterializationRepoExample.class);
  private final Map<String, Map<String, MaterializationInfo>> storage = new ConcurrentHashMap<>();

  /**
   * Helper method to create a map with a default, empty MaterializationInfo.
   *
   * @param key The key to use in the returned map.
   * @return A map containing the key and a default MaterializationInfo object.
   */
  private static Map<String, MaterializationInfo> createEmptyMap(String key) {
    final MaterializationInfo emptyInfo = new MaterializationInfo(false, new HashMap<>());
    final Map<String, MaterializationInfo> map = new HashMap<>();
    map.put(key, emptyInfo);
    return map;
  }

  @Override
  public CompletableFuture<Map<String, MaterializationInfo>> loadMaterializedAssignmentsForUnit(
      String unit, String materialization) {
    final Map<String, MaterializationInfo> unitAssignments = storage.get(unit);
    if (unitAssignments != null) {
      if (unitAssignments.containsKey(materialization)) {
        final Map<String, MaterializationInfo> result = new HashMap<>();
        result.put(materialization, unitAssignments.get(materialization));
        logger.debug("Cache hit for unit: {}, materialization: {}", unit, materialization);
        return CompletableFuture.supplyAsync(() -> result);
      } else {
        logger.debug(
            "Materialization {} not found in cached data for unit: {}", materialization, unit);
        return CompletableFuture.completedFuture(createEmptyMap(materialization));
      }
    }

    // If unitAssignments was null (cache miss for the unit), return an empty map structure.
    return CompletableFuture.completedFuture(createEmptyMap(materialization));
  }

  @Override
  public CompletableFuture<Void> storeAssignment(
      String unit, Map<String, MaterializationInfo> assignments) {
    if (unit == null) {
      return CompletableFuture.completedFuture(null);
    }

    // Use 'compute' for an atomic update operation on the ConcurrentHashMap.
    storage.compute(
        unit,
        (k, existingEntry) -> {
          if (existingEntry == null) {
            // If no entry exists, create a new one.
            // We create a new HashMap to avoid storing a reference to the potentially mutable
            // 'assignments' map.
            return assignments == null ? new HashMap<>() : new HashMap<>(assignments);
          } else {
            // If an entry exists, merge the new assignments into it.
            // This is equivalent to Kotlin's 'existingEntry.plus(assignments ?: emptyMap())'.
            final Map<String, MaterializationInfo> newEntry = new HashMap<>(existingEntry);
            if (assignments != null) {
              newEntry.putAll(assignments);
            }
            return newEntry;
          }
        });

    final int assignmentCount = (assignments != null) ? assignments.size() : 0;
    logger.debug("Stored {} assignments for unit: {}", assignmentCount, unit);

    return CompletableFuture.completedFuture(null);
  }

  @Override
  public void close() {
    storage.clear();
    logger.debug("In-memory storage cleared.");
  }
}
