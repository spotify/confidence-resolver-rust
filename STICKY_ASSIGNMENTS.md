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

#### Detailed Resolution Flowchart

The following flowchart illustrates the complete sticky assignment resolution logic for a single rule:

```
┌─────────────────────────────────────────────┐
│  Start: Processing Rule with Materialization │
└──────────────────┬──────────────────────────┘
                   │
                   ▼
         ┌─────────────────────┐
         │ Read Materialization │
         │ Spec Defined?       │
         └──────┬──────────────┘
                │
         ┌──────┴──────┐
         │             │
        NO            YES
         │             │
         │             ▼
         │   ┌─────────────────────────┐
         │   │ Get MaterializationInfo │
         │   │ from Context            │
         │   └──────┬──────────────────┘
         │          │
         │   ┌──────┴──────┐
         │   │             │
         │  FOUND      NOT FOUND
         │   │             │
         │   │             ▼
         │   │    ┌────────────────────┐
         │   │    │ Return: Missing    │
         │   │    │ Materialization    │
         │   │    │ Error              │
         │   │    └────────────────────┘
         │   │
         │   ▼
         │ ┌────────────────────────────┐
         │ │ Check: unit_in_info flag   │
         │ └──────┬─────────────────────┘
         │        │
         │ ┌──────┴──────┐
         │ │             │
         │ FALSE        TRUE
         │ │             │
         │ │ Unit NOT    │ Unit IS
         │ │ in mat.     │ in mat.
         │ │             │
         │ ▼             ▼
         │ ┌──────────┐  ┌──────────────────────┐
         │ │materialization│ │segment_targeting_    │
         │ │_must_match?  │ │can_be_ignored?       │
         │ └──┬───────┘  └──┬────────────────────┘
         │    │             │
         │ ┌──┴──┐       ┌──┴──┐
         │ │     │       │     │
         │TRUE  FALSE   TRUE  FALSE
         │ │     │       │     │
         │ ▼     │       │     ▼
         │┌────────┐   │  ┌──────────────┐
         ││Skip    │   │  │Check Segment │
         ││Rule    │   │  │Match         │
         │└────────┘   │  └──────┬───────┘
         │         │   │         │
         │         │   │    ┌────┴────┐
         │         │   │    │         │
         │         │   │  MATCH   NO MATCH
         │         │   │    │         │
         │         ▼   ▼    ▼         │
         │       ┌──────────┐         │
         │       │mat_matched│         │
         │       │= false    │         │
         │       └─────┬─────┘         │
         │             │               │
         │             │      ┌────────┘
         │             │      │mat_matched
         │             │      │= segment result
         │             │      │
         │             │      ▼
         │             │  ┌──────────┐
         │             │  │mat_matched│
         │             │  │= true     │
         │             │  └─────┬─────┘
         │             │        │
         └─────────────┴────────┴────────┐
                                         │
                                         ▼
                               ┌─────────────────┐
                               │ mat_matched?    │
                               └────┬────────────┘
                                    │
                              ┌─────┴─────┐
                              │           │
                             YES          NO
                              │           │
                              ▼           ▼
                    ┌──────────────────┐  ┌────────────────┐
                    │ Check if variant │  │ Check Normal   │
                    │ exists in        │  │ Segment Match? │
                    │ rule_to_variant  │  └────┬───────────┘
                    └────┬─────────────┘       │
                         │                ┌────┴────┐
                    ┌────┴────┐           │         │
                    │         │          YES        NO
                   FOUND   NOT FOUND      │         │
                    │         │           │         ▼
                    ▼         │           │   ┌──────────┐
          ┌──────────────┐   │           │   │Skip Rule │
          │Return Sticky │   │           │   └──────────┘
          │Assignment    │   │           │
          │(no updates)  │   │           ▼
          └──────────────┘   │    ┌────────────────┐
                             │    │ Calculate      │
                             │    │ Bucket & Find  │
                             └────┤ Assignment     │
                                  └────┬───────────┘
                                       │
                                  ┌────┴────┐
                                  │         │
                                FOUND    NOT FOUND
                                  │         │
                                  ▼         ▼
                        ┌──────────────┐ ┌──────────┐
                        │Has write_mat?│ │Skip Rule │
                        └────┬─────────┘ └──────────┘
                             │
                        ┌────┴────┐
                        │         │
                       YES        NO
                        │         │
                        ▼         │
              ┌──────────────┐   │
              │Create Update │   │
              │for write_mat │   │
              └────┬─────────┘   │
                   │             │
                   └─────┬───────┘
                         │
                         ▼
               ┌──────────────────┐
               │ Return Assignment│
               │ with Updates     │
               └──────────────────┘
```

#### Key Decision Points

1. **Unit Not in Materialization (`unit_in_info = false`)**:
   - If `materialization_must_match = true`: **Skip rule** (paused intake - only existing users)
   - Otherwise: Continue with normal segment evaluation

2. **Unit in Materialization (`unit_in_info = true`)**:
   - If `segment_targeting_can_be_ignored = true`: **Match immediately** (sticky users bypass targeting)
   - Otherwise: Still check segment match (sticky users must meet targeting criteria)

3. **Materialization Matched**:
   - Look up previously assigned variant from `rule_to_variant` map
   - Return sticky assignment (no new updates needed)

4. **Normal Assignment**:
   - Calculate bucket using hash
   - Find matching assignment in bucket ranges
   - If `write_materialization` specified: Create update for persistence

### 4. Materialization Context

The resolver accepts materialization context via the `materializations_per_unit` map in `ResolveWithStickyRequest`. This map goes from unit (targeting key) to `MaterializationMap`, which contains all the materialization information for that unit.

## Usage Patterns

### Basic Sticky Assignment

A rule with both read and write materialization will:
1. Check if the unit was previously assigned
2. Use the previous assignment if available
3. Store new assignments for future use

### Paused Intake

Setting `materialization_must_match = true` creates "paused intake":
- Only units already in the materialization (where `unit_in_info = true`) can proceed with rule evaluation
- New units (where `unit_in_info = false`) will skip this rule entirely
- Useful for controlled rollout scenarios where you want to stop accepting new users into an experiment while maintaining existing assignments

### Override Targeting

Setting `segment_targeting_can_be_ignored = true` allows:
- Units already in materialization (where `unit_in_info = true`) match the rule regardless of segment targeting
- Previously assigned variants are returned even if segment criteria no longer match
- Useful for maintaining assignments when targeting rules change or evolve over time

## API Integration

### Enable Sticky Assignments

Use the `ResolveWithStickyRequest` message for sticky assignment support:

```protobuf
message ResolveWithStickyRequest {
  ResolveFlagsRequest resolve_request = 1;

  // Context about the materialization required for the resolve
  // Map from unit (targeting key) to materialization data
  map<string, MaterializationMap> materializations_per_unit = 2;

  // if a materialization info is missing, return immediately
  bool fail_fast_on_sticky = 3;
}

message MaterializationMap {
  // materialization name to info
  map<string, MaterializationInfo> info_map = 1;
}

message MaterializationInfo {
  // true = unit IS in the materialization (has been assigned)
  // false = unit is NOT in the materialization (new user)
  bool unit_in_info = 1;
  
  // Map of rule names to assigned variant names for this unit
  map<string, string> rule_to_variant = 2;
}
```

### Handling Missing Materializations

The resolver may return `MissingMaterializations` when required materialization data is unavailable:

```protobuf
message ResolveWithStickyResponse {
  oneof resolve_result {
    Success success = 1;
    MissingMaterializations missing_materializations = 2;
  }

  message Success {
    ResolveFlagsResponse response = 1;
    repeated MaterializationUpdate updates = 2;
  }

  message MissingMaterializations {
    repeated MissingMaterializationItem items = 1;
  }

  message MissingMaterializationItem {
    string unit = 1;
    string rule = 2;
    string read_materialization = 3;
  }

  message MaterializationUpdate {
    string unit = 1;
    string write_materialization = 2;
    string rule = 3;
    string variant = 4;
  }
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

When assignments are made, the resolver returns `MaterializationUpdate` objects in the `Success` response:

```protobuf
message MaterializationUpdate {
  string unit = 1;
  string write_materialization = 2;
  string rule = 3;
  string variant = 4;
}
```

These updates should be persisted by the client and included in the `materializations_per_unit` map for future resolve requests.

### Error Handling

- **Missing Materializations**: When required materialization data is unavailable
- **Fail Fast**: `fail_fast_on_sticky` controls whether to return immediately or continue processing
- **Dependency Checking**: The resolver validates all materialization dependencies before evaluation

## Multi-Flag Resolution Flow

When resolving multiple flags with sticky assignments, the resolver uses a sophisticated flow to handle missing materializations efficiently:

```
┌──────────────────────────────────────┐
│ Start: resolve_flags_sticky()       │
│ Input: Multiple flags + context     │
└────────────┬─────────────────────────┘
             │
             ▼
   ┌─────────────────────┐
   │ For each flag:      │
   │ Process flag        │
   └──────┬──────────────┘
          │
          ▼
   ┌───────────────────────────┐
   │ Try to resolve flag       │
   └──────┬────────────────────┘
          │
    ┌─────┴─────┐
    │           │
  Success    Error
    │           │
    │      ┌────┴──────┐
    │      │           │
    │  Missing    Other
    │  Mat.       Error
    │   │           │
    │   ▼           ▼
    │ ┌──────────────────┐
    │ │fail_fast_on_     │
    │ │sticky = true?    │
    │ └────┬────────┬────┘
    │      │        │
    │     YES      NO
    │      │        │
    │      ▼        ▼
    │  ┌────────┐ ┌───────────────┐
    │  │Return  │ │Set has_missing│
    │  │Missing │ │= true; break  │
    │  │Mat.    │ │loop           │
    │  │(empty) │ └────┬──────────┘
    │  └────────┘      │
    │                  │
    ▼                  ▼
 ┌───────────────────────┐
 │ All flags processed?  │
 └────┬──────────────────┘
      │
 ┌────┴────┐
 │         │
YES       NO
 │         └──────┐
 │                │
 ▼                │
┌──────────────┐  │
│has_missing   │  │
│= true?       │  │
└──┬───────────┘  │
   │              │
┌──┴──┐           │
│     │           │
YES   NO          │
│     │           │
▼     │           │
┌──────────────┐  │
│Collect all   │  │
│missing mat.  │  │
│dependencies  │  │
└──┬───────────┘  │
   │              │
   ▼              │
┌──────────────┐  │
│Return        │  │
│Missing       │  │
│Mat. List     │  │
└──────────────┘  │
                  │
       ┌──────────┘
       │
       ▼
┌────────────────┐
│Return Success  │
│with Resolved   │
│Flags + Updates │
└────────────────┘
```

### Fail Fast vs. Discovery Mode

**Fail Fast Mode (`fail_fast_on_sticky = true`)**:
- Immediately returns when first missing materialization is detected
- Returns empty missing materialization list (signals caller to handle it)
- Stops processing remaining flags
- Best for production: fast failure for immediate remediation

**Discovery Mode (`fail_fast_on_sticky = false`)**:
- Continues processing all flags even when missing materializations are found
- Collects ALL missing materializations across all flags
- Calls `collect_missing_materializations()` to gather complete dependency list
- Best for initialization: discover all required materializations in one pass

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

### Performance Considerations

- **Early dependency validation**: When a rule requires a read_materialization, the resolver checks for it in the context before processing the rule logic
- **Fail fast on missing dependencies**: When a required MaterializationInfo is not found in the context, the resolver immediately returns an error without attempting rule evaluation
- **Selective dependency collection**: In discovery mode (`fail_fast_on_sticky = false`), after detecting any missing materialization, the resolver uses `collect_missing_materializations()` to efficiently gather all missing dependencies across all flags without full rule evaluation
- **Shared context efficiency**: Multiple flags can reference the same materialization context, avoiding redundant lookups

## Best Practices

1. **Consistent Storage**: Use reliable storage for materialization data to ensure assignment consistency
2. **Version Management**: Consider materialization versioning for complex migration scenarios
3. **Monitoring**: Track materialization hit rates and assignment consistency
4. **Testing**: Verify sticky behavior with different materialization states
5. **Cleanup**: Implement materialization cleanup for archived flags or expired experiments

## Example Workflow

1. User requests flag resolution with empty or minimal `materializations_per_unit` map
2. Resolver assigns variants and returns `MaterializationUpdate`s in the success response
3. Client stores materialization data (variant assignments per unit/rule/materialization)
4. Subsequent requests include the stored data in the `materializations_per_unit` map
5. Resolver uses stored assignments when available, creating new ones as needed
6. Process continues with updated materialization context from new updates

This approach ensures assignment consistency while allowing new users to be assigned according to current targeting rules.

## Quick Reference

### Key Flag Values

| Field | Value | Meaning |
|-------|-------|---------|
| `unit_in_info` | `true` | Unit **IS** in materialization (already assigned) |
| `unit_in_info` | `false` | Unit **is NOT** in materialization (new user) |
| `materialization_must_match` | `true` | Only accept units already in materialization (paused intake) |
| `materialization_must_match` | `false` | Accept both existing and new units |
| `segment_targeting_can_be_ignored` | `true` | Units in materialization bypass segment checks |
| `segment_targeting_can_be_ignored` | `false` | Units in materialization still need segment match |
| `fail_fast_on_sticky` | `true` | Return immediately on first missing materialization |
| `fail_fast_on_sticky` | `false` | Collect all missing materializations before returning |

### Behavior Matrix

| unit_in_info | materialization_must_match | Result |
|--------------|---------------------------|--------|
| `false` (new) | `true` | **Skip rule** (paused intake) |
| `false` (new) | `false` | Normal segment evaluation |
| `true` (existing) | `true` | Continue processing |
| `true` (existing) | `false` | Continue processing |

| unit_in_info | segment_targeting_can_be_ignored | Result |
|--------------|----------------------------------|--------|
| `true` (existing) | `true` | **Match immediately** (bypass targeting) |
| `true` (existing) | `false` | Check segment match required |
| `false` (new) | any | Not applicable (handled by must_match) |

### Common Configuration Patterns

**Standard Sticky Assignment:**
```
read_materialization: "experiment_v1"
write_materialization: "experiment_v1"
mode:
  materialization_must_match: false
  segment_targeting_can_be_ignored: false
```
*Behavior:* Sticky for existing users, new users get assigned normally

**Paused Intake:**
```
read_materialization: "experiment_v1"
write_materialization: ""  # No new assignments
mode:
  materialization_must_match: true
  segment_targeting_can_be_ignored: false
```
*Behavior:* Only existing users proceed, new users skip this rule

**Sticky Override (Ignore Targeting Changes):**
```
read_materialization: "experiment_v1"
write_materialization: "experiment_v1"
mode:
  materialization_must_match: false
  segment_targeting_can_be_ignored: true
```
*Behavior:* Existing users keep assignment regardless of targeting changes

**Full Lockdown:**
```
read_materialization: "experiment_v1"
write_materialization: ""
mode:
  materialization_must_match: true
  segment_targeting_can_be_ignored: true
```
*Behavior:* Only existing users, no new assignments, bypass targeting checks

