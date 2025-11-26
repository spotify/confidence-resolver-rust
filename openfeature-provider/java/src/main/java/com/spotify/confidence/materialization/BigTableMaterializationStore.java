package com.spotify.confidence.materialization;

import static com.google.cloud.bigtable.data.v2.models.Filters.FILTERS;

import com.google.cloud.bigtable.data.v2.BigtableDataClient;
import com.google.cloud.bigtable.data.v2.models.BulkMutation;
import com.google.cloud.bigtable.data.v2.models.Filters;
import com.google.cloud.bigtable.data.v2.models.Row;
import com.google.cloud.bigtable.data.v2.models.RowMutationEntry;
import com.google.cloud.bigtable.data.v2.models.TableId;
import java.util.HashMap;
import java.util.HashSet;
import java.util.Map;
import java.util.Optional;
import java.util.Set;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.CompletionStage;
import java.util.concurrent.Future;

public class BigTableMaterializationStore implements MaterializationReader, MaterializationWriter {
  public static final TableId TABLE_ID = TableId.of("materializations");
  public static final String COLUMN_FAMILY_NAME = "mats";

  private final BigtableDataClient client;

  public BigTableMaterializationStore(BigtableDataClient client) {
    this.client = client;
  }

  @Override
  public CompletionStage<Void> setVariants(Map<MaterializationKey, String> variants) {
    final Map<String, RowMutationEntry> rowMutations = new HashMap<>();
    for (var entry : variants.entrySet()) {
      MaterializationKey key = entry.getKey();
      String variant = entry.getValue();
      rowMutations
          .computeIfAbsent(
              key.unit(),
              unit -> RowMutationEntry.create(unit).setCell(COLUMN_FAMILY_NAME, key.materialization(), ""))
          .setCell(COLUMN_FAMILY_NAME, key.materialization() + "_" + key.rule(), variant);
    }
    final BulkMutation bulkMutation = BulkMutation.create(TABLE_ID);
    for (RowMutationEntry mutation : rowMutations.values()) {
      bulkMutation.add(mutation);
    }
    return toCompletableFuture(client.bulkMutateRowsAsync(bulkMutation));
  }

  @Override
  public CompletionStage<Map<MaterializationKey, Optional<String>>> getVariants(
      Set<MaterializationKey> keys) {
    // Group keys by unit (row key)
    Map<String, Set<MaterializationKey>> keysByUnit = new HashMap<>();
    for (MaterializationKey key : keys) {
      keysByUnit.computeIfAbsent(key.unit(), u -> new HashSet<>()).add(key);
    }

    // Execute one read per unit row
    Map<MaterializationKey, CompletableFuture<Optional<String>>> futures = new HashMap<>();
    for (var entry : keysByUnit.entrySet()) {
      String unit = entry.getKey();
      Set<MaterializationKey> unitKeys = entry.getValue();

      // Build cell qualifiers to read
      Set<String> qualifiers = new HashSet<>();
      for (MaterializationKey key : unitKeys) {
        qualifiers.add(key.materialization() + "_" + key.rule());
      }

      CompletableFuture<Optional<Row>> rowFuture = readRow(unit, qualifiers);

      // Map each key to its result
      for (MaterializationKey key : unitKeys) {
        String qualifier = key.materialization() + "_" + key.rule();
        futures.put(key, rowFuture.thenApply(maybeRow -> extractCell(maybeRow, qualifier)));
      }
    }

    return allCompleted(futures);
  }

  @Override
  public CompletionStage<Set<MaterializationKey>> checkInclusions(Set<MaterializationKey> keys) {
    // Group keys by unit (row key)
    Map<String, Set<MaterializationKey>> keysByUnit = new HashMap<>();
    for (MaterializationKey key : keys) {
      keysByUnit.computeIfAbsent(key.unit(), u -> new HashSet<>()).add(key);
    }

    // Execute one read per unit row
    Map<MaterializationKey, CompletableFuture<Boolean>> futures = new HashMap<>();
    for (var entry : keysByUnit.entrySet()) {
      String unit = entry.getKey();
      Set<MaterializationKey> unitKeys = entry.getValue();

      // Build cell qualifiers to read (just the materialization name for inclusion check)
      Set<String> qualifiers = new HashSet<>();
      for (MaterializationKey key : unitKeys) {
        qualifiers.add(key.materialization());
      }

      CompletableFuture<Optional<Row>> rowFuture = readRow(unit, qualifiers);

      // Map each key to its result
      for (MaterializationKey key : unitKeys) {
        futures.put(
            key, rowFuture.thenApply(maybeRow -> extractCell(maybeRow, key.materialization()).isPresent()));
      }
    }

    // Collect keys where inclusion is true
    return allCompleted(futures).thenApply(results -> {
      Set<MaterializationKey> included = new HashSet<>();
      for (var entry : results.entrySet()) {
        if (entry.getValue()) {
          included.add(entry.getKey());
        }
      }
      return included;
    });
  }

  private CompletableFuture<Optional<Row>> readRow(String rowKey, Set<String> qualifiers) {
    final Filters.InterleaveFilter cellFilter = FILTERS.interleave();
    for (String qualifier : qualifiers) {
      cellFilter.filter(FILTERS.qualifier().exactMatch(qualifier));
    }
    final Filters.Filter filter =
        FILTERS
            .chain()
            .filter(FILTERS.limit().cellsPerColumn(1))
            .filter(FILTERS.family().exactMatch(COLUMN_FAMILY_NAME))
            .filter(cellFilter);

    return toCompletableFuture(client.readRowAsync(TABLE_ID, rowKey, filter))
        .thenApply(Optional::ofNullable);
  }

  private Optional<String> extractCell(Optional<Row> maybeRow, String qualifier) {
    return maybeRow.flatMap(
        row ->
            row.getCells(COLUMN_FAMILY_NAME, qualifier).stream()
                .findFirst()
                .map(cell -> cell.getValue().toStringUtf8()));
  }

  private static <K, V> CompletableFuture<Map<K, V>> allCompleted(
      Map<K, CompletableFuture<V>> futures) {
    CompletableFuture<Void> all =
        CompletableFuture.allOf(futures.values().toArray(CompletableFuture[]::new));
    return all.thenApply(
        v -> {
          Map<K, V> results = new HashMap<>();
          for (var entry : futures.entrySet()) {
            results.put(entry.getKey(), entry.getValue().join());
          }
          return results;
        });
  }

  private static <T> CompletableFuture<T> toCompletableFuture(Future<T> future) {
    throw new UnsupportedOperationException();
  }
}
