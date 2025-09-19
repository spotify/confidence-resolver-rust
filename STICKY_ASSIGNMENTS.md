# Sticky Assignments in Confidence Flag Resolver

## Overview

Sticky assignments are a feature in the Confidence Flag Resolver that allows flag assignments to persist across multiple resolve requests. This ensures consistent user experiences and enables advanced experimentation workflows by maintaining assignment state over time.

## What are Sticky Assignments?

Sticky assignments work by storing flag assignment information (materializations) that can be referenced in future resolve requests. Instead of randomly assigning users to variants each time a flag is resolved, the system can "stick" to previous assignments when certain conditions are met.

### Key Concepts

- **Materialization**: The persisted record of a flag assignment for a specific unit (user/entity)
- **Unit**: The entity being assigned (typically a user ID or targeting key)
- **Materialization Context**: Information about previous assignments passed to the resolver
- **Read/Write Materialization**: Rules specify whether to read from or write to materializations

## How It Works

### 1. Materialization Specification

Each flag rule can include a `MaterializationSpec` that defines:

```protobuf
message MaterializationSpec {
  // Where to read previous assignments from
  string read_materialization = 2;

  // Where to write new assignments to
  string write_materialization = 1;

  // How materialization reads should be treated
  MaterializationReadMode mode = 3;
}
```

### 2. Materialization Read Mode

The `MaterializationReadMode` controls how materializations interact with normal targeting:

```protobuf
message MaterializationReadMode {
  // If true, only units in the materialization will be considered
  // If false, units match if they're in materialization OR match segment
  bool materialization_must_match = 1;

  // If true, segment targeting is ignored for units in materialization
  // If false, both materialization and segment must match
  bool segment_targeting_can_be_ignored = 2;
}
```

### 3. Resolution Process

When resolving a flag with sticky assignments enabled:

1. **Check Dependencies**: Verify all required materializations are available
2. **Read Materialization**: Check if the unit has a previous assignment for this rule
3. **Apply Logic**: Based on `MaterializationReadMode`, determine if the stored assignment should be used
4. **Write Materialization**: If a new assignment is made and a write materialization is specified, store it

### 4. Materialization Context

The resolver accepts a `MaterializationContext` containing previous assignments:

```protobuf
message MaterializationContext {
  map<string, MaterializationInfo> unit_materialization_info = 1;
}

message MaterializationInfo {
  bool unit_in_info = 1;
  map<string, string> rule_to_variant = 2;
}
```

## Usage Patterns

### Basic Sticky Assignment

A rule with both read and write materialization will:
1. Check if the unit was previously assigned
2. Use the previous assignment if available
3. Store new assignments for future use

### Paused Intake

Setting `materialization_must_match = true` creates "paused intake":
- Only units already in the materialization will match the rule
- New units will skip this rule entirely
- Useful for controlled rollout scenarios

### Override Targeting

Setting `segment_targeting_can_be_ignored = true` allows:
- Units in materialization match the rule regardless of segment targeting
- Segment allocation proportions are ignored for these units
- Useful for maintaining assignments when targeting rules change

## API Integration

### Enable Sticky Assignments

Set `process_sticky = true` in the resolve request:

```protobuf
message ResolveFlagsRequest {
  // ... other fields ...

  // if the resolver should handle sticky assignments
  bool process_sticky = 6;

  // Context about the materialization required for the resolve
  MaterializationContext materialization_context = 7;

  // if a materialization info is missing, return immediately
  bool fail_fast_on_sticky = 8;
}
```

### Handling Missing Materializations

The resolver may return `MissingMaterializations` when required materialization data is unavailable:

```protobuf
message ResolveFlagResponseResult {
  oneof resolve_result {
    ResolveFlagsResponse response = 1;
    MissingMaterializations missing_materializations = 2;
  }
}

message MissingMaterializationItem {
  string unit = 1;
  string rule = 2;
  string read_materialization = 3;
}
```

## Use Cases

### 1. Consistent User Experience

Ensure users see the same variant across app sessions and devices by storing their assignments in a shared materialization store.

### 2. Experiment Analysis

Maintain assignment consistency during long-running experiments, even when targeting rules or traffic allocation changes.

### 3. Migration Scenarios

Gradually migrate users from one variant to another by updating materializations over time.

### 4. Controlled Rollout

Use "paused intake" mode to limit new user assignments while maintaining existing ones.

## Implementation Details

### Materialization Updates

When assignments are made, the resolver returns `MaterializationUpdate` objects:

```protobuf
message MaterializationUpdate {
  string unit = 1;
  string write_materialization = 2;
  string rule = 3;
  string variant = 4;
}
```

These should be persisted by the client for use in future resolve requests.

### Error Handling

- **Missing Materializations**: When required materialization data is unavailable
- **Fail Fast**: `fail_fast_on_sticky` controls whether to return immediately or continue processing
- **Dependency Checking**: The resolver validates all materialization dependencies before evaluation

## Advanced Optimizations

### Fail Fast on First Missing Materialization

The `fail_fast_on_sticky` parameter provides a performance optimization for handling missing materializations:

**Behavior:**
- When `fail_fast_on_sticky = true`: As soon as any flag encounters a missing materialization dependency, the resolver immediately returns all accumulated missing materializations without processing remaining flags
- When `fail_fast_on_sticky = false`: The resolver continues processing all flags and collects all missing materializations before returning

**Use Cases:**
- **Discovery Mode**: Set to `false` when you want to collect all missing materializations across all flags in a single request
- **Production Mode**: Set to `true` when you want immediate feedback about missing dependencies to avoid unnecessary processing

**Example Flow:**
```
Flag A: ✅ Has materialization → Process normally
Flag B: ❌ Missing materialization + fail_fast=true → Return immediately with [Flag B missing item]
Flag C: (Not processed due to fail_fast)
```

### Rule Evaluation Skipping Optimization

The resolver implements a sophisticated optimization to avoid unnecessary rule evaluation when materialization dependencies are missing:

**The `skip_on_not_missing` Mechanism:**

1. **Dependency Discovery Phase**: When processing multiple flags, if any previous flag had missing materializations, subsequent flags enter "discovery mode"

2. **Two-Pass Evaluation**:
   - **Pass 1**: Check for missing materializations only (skip rule evaluation)
   - **Pass 2**: If all materializations are available, re-evaluate with full rule processing

3. **Optimization Logic**:
   ```rust
   skip_on_not_missing: !missing_materialization_items.is_empty()
   ```

**How It Works:**

```
Processing Flag 1:
├── Rule 1: Missing materialization X → Collect missing item
├── Rule 2: Skip evaluation (skip_on_not_missing=true)
├── Result: Flag 1 has missing materializations

Processing Flag 2:
├── skip_on_not_missing = true (because Flag 1 had missing deps)
├── All rules: Only check for missing materializations, don't evaluate
├── Result: Collect any additional missing items for Flag 2
```

**Benefits:**
- **Performance**: Avoids expensive rule evaluation (segment matching, bucket calculation) when dependencies are missing
- **Consistency**: Ensures all missing materializations are discovered before any rule evaluation begins
- **Atomicity**: Either all flags resolve successfully with their materializations, or all missing dependencies are returned

**Complete Resolution Flow:**

1. **First Pass**: Process all flags in discovery mode to find all missing materializations
2. **Early Return**: If `fail_fast_on_sticky=true` and missing deps found, return immediately
3. **Second Pass**: If all materializations available, re-process all flags with full evaluation
4. **Success**: Return resolved flags with materialization updates

This optimization ensures efficient handling of complex dependency graphs while maintaining correctness and performance.

### Performance Considerations

- **Materialization lookups happen before rule evaluation**: Dependencies are checked first to avoid expensive operations
- **Failed materialization dependencies skip rule evaluation**: No segment matching or bucket calculation when deps missing
- **Two-phase resolution**: Discovery phase finds all missing deps, evaluation phase only runs when all deps available
- **Batch processing**: Multiple flags can share materialization context for efficient processing

## Best Practices

1. **Consistent Storage**: Use reliable storage for materialization data to ensure assignment consistency
2. **Version Management**: Consider materialization versioning for complex migration scenarios
3. **Monitoring**: Track materialization hit rates and assignment consistency
4. **Testing**: Verify sticky behavior with different materialization states
5. **Cleanup**: Implement materialization cleanup for archived flags or expired experiments

## Example Workflow

1. User requests flag resolution without materialization context
2. Resolver assigns variants and returns `MaterializationUpdate`s
3. Client stores materialization data
4. Subsequent requests include `MaterializationContext`
5. Resolver uses stored assignments when available, creating new ones as needed
6. Process continues with updated materialization context

This approach ensures assignment consistency while allowing new users to be assigned according to current targeting rules.
