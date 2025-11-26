package com.spotify.confidence;

import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.Set;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.CompletionStage;
import java.util.concurrent.Future;

import com.google.cloud.bigtable.data.v2.BigtableDataClient;
import com.google.cloud.bigtable.data.v2.models.BulkMutation;
import com.google.cloud.bigtable.data.v2.models.Filters;
import static com.google.cloud.bigtable.data.v2.models.Filters.FILTERS;
import com.google.cloud.bigtable.data.v2.models.Row;
import com.google.cloud.bigtable.data.v2.models.RowMutationEntry;
import com.google.cloud.bigtable.data.v2.models.TableId;
import com.spotify.confidence.MaterializationStore.ReadResult.InclusionResult;
import com.spotify.confidence.MaterializationStore.ReadResult.VariantResult;

public sealed interface MaterializationStore permits MaterializationStore.Readable, MaterializationStore.Writable {

  // ----- Read side -----

  sealed interface ReadOp permits ReadOp.Inclusion, ReadOp.GetVariant {
    String materialization();
    String unit();

    // Query: does the materialization include the key?
    record Inclusion(String materialization, String unit) implements ReadOp {
      InclusionResult toResult(boolean included) {
        return new InclusionResult(materialization, unit, included);
      }
    }

    // Query: get variants for specific rules for this key
    record GetVariant(String materialization, String unit, String rule) implements ReadOp {
      VariantResult toResult(Optional<String> variant) {
        return new VariantResult(materialization, unit, rule, variant);
      }
    }
  }

  sealed interface ReadResult permits ReadResult.InclusionResult, ReadResult.VariantResult {
    String materialization();
    String unit();

    // Result for Inclusion
    record InclusionResult(String materialization, String unit, boolean included) implements ReadResult {}

    // Result for GetRules
    record VariantResult(String materialization, String unit, String rule, Optional<String> variant) implements ReadResult {}
  }

  public non-sealed interface Readable extends MaterializationStore {
    // Returns results in the same order as the input ops
    CompletionStage<List<ReadResult>> read(List<ReadOp> ops);
  }

  // ----- Write side -----

  sealed interface WriteOp permits WriteOp.SetVariant {
    String materialization();
    String unit();

    // Upsert a variant
    record SetVariant(String materialization, String unit, String rule, String variant) implements WriteOp {}

  }

  public non-sealed interface Writable extends MaterializationStore {
    CompletionStage<Void> write(Set<WriteOp> ops);
  }
}

class BigTableMaterializationStore implements MaterializationStore.Readable, MaterializationStore.Writable {
  public static final TableId TABLE_ID = TableId.of("materializations");
  public static final String COLUMN_FAMILY_NAME = "mats";

  final BigtableDataClient client;
  
  public BigTableMaterializationStore(BigtableDataClient client) {
    this.client = client;
  }

  @Override
  public CompletionStage<Void> write(Set<WriteOp> ops) {
    final Map<String, RowMutationEntry> rowMutations = new HashMap<>();
    for(WriteOp op : ops) {
      switch (op) {
        case WriteOp.SetVariant sv -> {
          rowMutations.computeIfAbsent(sv.unit(), 
            key -> RowMutationEntry.create(key).setCell(COLUMN_FAMILY_NAME, sv.materialization(), "")
          )
          .setCell(COLUMN_FAMILY_NAME, sv.materialization() + "_" + sv.rule(), sv.variant());
        }
      }
    }
    final BulkMutation bulkMutation = BulkMutation.create(TABLE_ID);
    for(RowMutationEntry mutation : rowMutations.values()) {
      bulkMutation.add(mutation);
    }
    return toCompletableFuture(client.bulkMutateRowsAsync(bulkMutation));
  }

  private class RowQuery {

    private final String rowKey;
    private final Set<String> cellsToRead = new HashSet<>();
    private final CompletableFuture<Optional<Row>> result = new CompletableFuture<>();

    RowQuery(String rowKey) {
      this.rowKey = rowKey;
    }

    CompletableFuture<ReadResult> addOp(ReadOp op) {
      return switch (op) {
          case ReadOp.Inclusion inclusionOp -> addInclusionOp(inclusionOp);
          case ReadOp.GetVariant variantOp -> addVariantOp(variantOp);
      };
    }

    private CompletableFuture<ReadResult> addVariantOp(ReadOp.GetVariant op) {
      return readCell(op.materialization() + "_" + op.rule()).thenApply(maybeValue -> op.toResult(maybeValue));
    }

    private CompletableFuture<ReadResult> addInclusionOp(ReadOp.Inclusion op) {
      return readCell(op.materialization()).thenApply(maybeValue -> op.toResult(maybeValue.isPresent()));
    }
    
    private CompletableFuture<Optional<String>> readCell(String qualifier) {
      cellsToRead.add(qualifier);
      return result.thenApply(maybeRow -> maybeRow
        .flatMap(row -> row
          .getCells(COLUMN_FAMILY_NAME, qualifier)
          .stream()
          .findFirst()
          .map(cell -> cell.getValue().toStringUtf8())
        )
      );
    }

    void execute() {
      final Filters.InterleaveFilter cellFilter = FILTERS.interleave();
      for(String cell : cellsToRead) {
        cellFilter.filter(FILTERS.qualifier().exactMatch(cell));
      }
      final Filters.Filter filter = FILTERS.chain()
        .filter(FILTERS.limit().cellsPerColumn(1))
        .filter(FILTERS.family().exactMatch(COLUMN_FAMILY_NAME))
        .filter(cellFilter);
      toCompletableFuture(client.readRowAsync(TABLE_ID, rowKey, filter)).whenComplete((row, ex) -> {
        if(ex == null) {
          result.complete(Optional.ofNullable(row));
        } else {
          result.completeExceptionally(ex);
        }
      });
    }

  }

  @Override
  public CompletionStage<List<ReadResult>> read(List<ReadOp> ops) {
    final Map<String, RowQuery> rowsToRead = new HashMap<>();
    List<CompletableFuture<ReadResult>> results = ops.stream()
      .map(op -> rowsToRead.computeIfAbsent(op.unit(), RowQuery::new).addOp(op))
      .toList();
    rowsToRead.values().forEach(RowQuery::execute);
    return allCompleted(results);
  }

  private static <T> CompletableFuture<List<T>> allCompleted(List<CompletableFuture<T>> futures) {
    CompletableFuture<Void> all = CompletableFuture.allOf(futures.toArray(CompletableFuture[]::new));
    return all.thenApply(v -> futures.stream().map(CompletableFuture::join).toList());
  }

  private static <T> CompletableFuture<T> toCompletableFuture(Future<T> future) {
    throw new UnsupportedOperationException();
  }

}