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
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.Set;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.CompletionStage;
import java.util.concurrent.Future;

public class BigTableMaterializationStore implements MaterializationReader, MaterializationWriter {
  public static final TableId TABLE_ID = TableId.of("materializations");
  public static final String COLUMN_FAMILY_NAME = "mats";

  final BigtableDataClient client;

  public BigTableMaterializationStore(BigtableDataClient client) {
    this.client = client;
  }

  @Override
  public CompletionStage<Void> write(Set<WriteOp> ops) {
    final Map<String, RowMutationEntry> rowMutations = new HashMap<>();
    for (WriteOp op : ops) {
      if (op instanceof WriteOp.SetVariant sv) {
        rowMutations
            .computeIfAbsent(
                sv.unit(),
                key ->
                      RowMutationEntry.create(key)
                          .setCell(COLUMN_FAMILY_NAME, sv.materialization(), ""))
              .setCell(COLUMN_FAMILY_NAME, sv.materialization() + "_" + sv.rule(), sv.variant());
      } else {
        throw new IllegalArgumentException("Unknown WriteOp: " + op);
      }
    }
    final BulkMutation bulkMutation = BulkMutation.create(TABLE_ID);
    for (RowMutationEntry mutation : rowMutations.values()) {
      bulkMutation.add(mutation);
    }
    return toCompletableFuture(client.bulkMutateRowsAsync(bulkMutation));
  }

  // Helper class for read queries
  private class RowQueryBuilder {

    private final String rowKey;
    private final Set<String> cellsToRead = new HashSet<>();
    private final CompletableFuture<Optional<Row>> result = new CompletableFuture<>();

    private RowQueryBuilder(String rowKey) {
      this.rowKey = rowKey;
    }

    CompletableFuture<ReadResult> addOp(ReadOp op) {
      if (op instanceof ReadOp.Inclusion) {
        return addInclusionOp((ReadOp.Inclusion) op);
      } else if (op instanceof ReadOp.GetVariant) {
        return addVariantOp((ReadOp.GetVariant) op);
      } else {
        throw new IllegalArgumentException("Unknown ReadOp: " + op);
      }
    }

    private CompletableFuture<ReadResult> addVariantOp(ReadOp.GetVariant op) {
      return readCell(op.materialization() + "_" + op.rule())
          .thenApply(maybeValue -> op.toResult(maybeValue));
    }

    private CompletableFuture<ReadResult> addInclusionOp(ReadOp.Inclusion op) {
      return readCell(op.materialization())
          .thenApply(maybeValue -> op.toResult(maybeValue.isPresent()));
    }

    private CompletableFuture<Optional<String>> readCell(String qualifier) {
      cellsToRead.add(qualifier);
      return result.thenApply(
          maybeRow ->
              maybeRow.flatMap(
                  row ->
                      row.getCells(COLUMN_FAMILY_NAME, qualifier).stream()
                          .findFirst()
                          .map(cell -> cell.getValue().toStringUtf8())));
    }

    void execute() {
      final Filters.InterleaveFilter cellFilter = FILTERS.interleave();
      for (String cell : cellsToRead) {
        cellFilter.filter(FILTERS.qualifier().exactMatch(cell));
      }
      final Filters.Filter filter =
          FILTERS
              .chain()
              .filter(FILTERS.limit().cellsPerColumn(1))
              .filter(FILTERS.family().exactMatch(COLUMN_FAMILY_NAME))
              .filter(cellFilter);
      toCompletableFuture(client.readRowAsync(TABLE_ID, rowKey, filter))
          .whenComplete(
              (row, ex) -> {
                if (ex == null) {
                  result.complete(Optional.ofNullable(row));
                } else {
                  result.completeExceptionally(ex);
                }
              });
    }
  }

  @Override
  public CompletionStage<List<ReadResult>> read(List<ReadOp> ops) {
    final Map<String, RowQueryBuilder> rowsToRead = new HashMap<>();
    List<CompletableFuture<ReadResult>> results =
        ops.stream()
            .map(op -> rowsToRead.computeIfAbsent(op.unit(), RowQueryBuilder::new).addOp(op))
            .toList();
    rowsToRead.values().forEach(RowQueryBuilder::execute);
    return allCompleted(results);
  }

  private static <T> CompletableFuture<List<T>> allCompleted(List<CompletableFuture<T>> futures) {
    CompletableFuture<Void> all =
        CompletableFuture.allOf(futures.toArray(CompletableFuture[]::new));
    return all.thenApply(v -> futures.stream().map(CompletableFuture::join).toList());
  }

  private static <T> CompletableFuture<T> toCompletableFuture(Future<T> future) {
    throw new UnsupportedOperationException();
  }
}
