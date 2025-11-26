# Confidence OpenFeature Provider for Go

![Status: Experimental](https://img.shields.io/badge/status-experimental-orange)

A high-performance OpenFeature provider for [Confidence](https://confidence.spotify.com/) feature flags that evaluates flags locally for minimal latency.

## Features

- **Local Resolution**: Evaluates feature flags locally using WebAssembly (WASM)
- **Low Latency**: No network calls during flag evaluation
- **Automatic Sync**: Periodically syncs flag configurations from Confidence
- **Exposure Logging**: Fully supported exposure logging and resolve analytics
- **OpenFeature Compatible**: Works with the standard OpenFeature Go SDK

## Installation

```bash
go get github.com/spotify/confidence-resolver/openfeature-provider/go
go mod tidy
```

## Requirements

- Go 1.24+
- OpenFeature Go SDK 1.16.0+

## Quick Start

```go
package main

import (
    "context"
    "log"

    "github.com/open-feature/go-sdk/openfeature"
    "github.com/spotify/confidence-resolver/openfeature-provider/go/confidence"
)

func main() {
    ctx := context.Background()

    // Create provider with required credentials
    provider, err := confidence.NewProvider(ctx, confidence.ProviderConfig{
        ClientSecret: "your-client-secret",
    })
    if err != nil {
        log.Fatalf("Failed to create provider: %v", err)
    }

    // Set the provider
    openfeature.SetProviderAndWait(provider)

    // Get a client
    client := openfeature.NewClient("my-app")

    // Evaluate a flag
    evalCtx = openfeature.NewEvaluationContext("user-123", map[string]interface{}{
        "country": "US",
        "plan":    "premium",
    })

    value, err := client.BooleanValue(ctx, "my-flag.enabled", false, evalCtx)
    if err != nil {
        log.Printf("Flag evaluation failed: %v", err)
    }

    log.Printf("Flag value: %v", value)
}
```

## Configuration

### ProviderConfig

The `ProviderConfig` struct contains all configuration options for the provider:

#### Required Fields

- `ClientSecret` (string): The client secret used for authentication and flag evaluation

#### Optional Fields

- `Logger` (*slog.Logger): Custom logger for provider operations. If not provided, a default text logger is created. See [Logging](#logging) for details.
- `StickyResolveStrategy` (StickyResolveStrategy): Strategy for handling sticky resolve scenarios. Defaults to `RemoteResolverFallback`. See [Sticky Resolve](#sticky-resolve) for details.
- `ConnFactory` (func): Custom gRPC connection factory for advanced use cases (e.g., custom interceptors, TLS configuration)

#### Advanced: Testing with Custom State Provider

For testing purposes only, you can provide a custom `StateProvider` to supply resolver state from local sources (e.g., a file cache):

```go
// WARNING: This is for testing only. Do not use in production.
provider, err := confidence.NewProviderWithStateProvider(ctx,
    confidence.ProviderConfigWithStateProvider{
        ClientSecret:  "your-client-secret",
        StateProvider: myCustomStateProvider,
        AccountId:     "your-account-id",
        // WasmBytes: customWasmBytes, // Optional: custom WASM module
    },
)
```

**Important**: This configuration disables automatic state fetching and exposure logging. For production deployments, always use `NewProvider()` with `ProviderConfig`.

## Sticky Resolve

Sticky Resolve ensures users receive the same variant throughout an experiment, even if their targeting attributes change or you pause new assignments.

**Two main use cases:**
1. **Consistent experience** - User moves countries but keeps the same variant
2. **Pause intake** - Stop new assignments while maintaining existing ones

### Default: Server-Side Storage (RemoteResolverFallback)

By default, the provider uses `RemoteResolverFallback` which automatically handles sticky assignments via network calls to Confidence when needed.

**Flow:**
1. Local WASM resolver attempts to resolve
2. If sticky data needed → network call to Confidence
3. Confidence checks its sticky repository, returns variant
4. Assignment stored server-side with 90-day TTL (auto-renewed on access)

**Server-side configuration (in Confidence UI):**
- Optionally skip targeting criteria for sticky assignments
- Pause/resume new entity intake
- Automatic TTL management

### Custom: Local Storage (MaterializationRepository)

Implement `MaterializationRepository` to store assignments locally and eliminate network calls:

```go
type MaterializationRepository interface {
    StickyResolveStrategy

    // LoadMaterializedAssignmentsForUnit loads assignments for a unit.
    // Returns a map of materialization name to MaterializationInfo.
    LoadMaterializedAssignmentsForUnit(ctx context.Context, unit, materialization string) (map[string]*MaterializationInfo, error)

    // StoreAssignment stores materialization assignments for a unit.
    StoreAssignment(ctx context.Context, unit string, assignments map[string]*MaterializationInfo) error
}

type MaterializationInfo struct {
    // UnitInMaterialization indicates if the unit exists in the materialization
    UnitInMaterialization bool
    // RuleToVariant maps rule IDs to their assigned variant names
    RuleToVariant map[string]string
}
```

### Example: In-Memory Repository

```go
type InMemoryMaterializationRepository struct {
    mu       sync.RWMutex
    storage  map[string]map[string]*confidence.MaterializationInfo // unit -> materialization -> info
}

func (r *InMemoryMaterializationRepository) LoadMaterializedAssignmentsForUnit(
    ctx context.Context, unit, materialization string,
) (map[string]*confidence.MaterializationInfo, error) {
    r.mu.RLock()
    defer r.mu.RUnlock()

    if unitData, ok := r.storage[unit]; ok {
        result := make(map[string]*confidence.MaterializationInfo)
        for k, v := range unitData {
            result[k] = v
        }
        return result, nil
    }
    return make(map[string]*confidence.MaterializationInfo), nil
}

func (r *InMemoryMaterializationRepository) StoreAssignment(
    ctx context.Context, unit string, assignments map[string]*confidence.MaterializationInfo,
) error {
    r.mu.Lock()
    defer r.mu.Unlock()

    if r.storage == nil {
        r.storage = make(map[string]map[string]*confidence.MaterializationInfo)
    }
    if r.storage[unit] == nil {
        r.storage[unit] = make(map[string]*confidence.MaterializationInfo)
    }

    for k, v := range assignments {
        r.storage[unit][k] = v
    }
    return nil
}

func (r *InMemoryMaterializationRepository) Close() {}
```

**Usage:**

```go
provider, err := confidence.NewProvider(ctx, confidence.ProviderConfig{
    APIClientID:           "your-api-client-id",
    APIClientSecret:       "your-api-client-secret",
    ClientSecret:          "your-client-secret",
    StickyResolveStrategy: &InMemoryMaterializationRepository{},
})
```

### Choosing a Strategy

| Strategy | Best For | Trade-offs |
|----------|----------|------------|
| **RemoteResolverFallback** (default) | Most apps | Simple, managed by Confidence. Network calls when needed. |
| **MaterializationRepository** (in-memory) | Single-instance apps, testing | Fast, no network. Lost on restart. |
| **MaterializationRepository** (Redis/DB) | Distributed/Multi instance apps | No network calls. Requires storage infra. |

## Credentials

Get your client secret from your [Confidence dashboard](https://confidence.spotify.com/):

- `ClientSecret`: The client secret used for authentication and flag evaluation


## WebAssembly Module

The WASM module (`confidence_resolver.wasm`) is embedded in the Go binary using Go 1.16+ embed directives. No external WASM file is required at runtime.

## Flag Evaluation

The provider supports all OpenFeature value types:

```go
// Boolean flags
enabled, err := client.BooleanValue(ctx, "feature.enabled", false, evalCtx)

// String flags
color, err := client.StringValue(ctx, "feature.button_color", "blue", evalCtx)

// Integer flags
timeout, err := client.IntValue(ctx, "feature.timeout-ms", 5000, evalCtx)

// Float flags
ratio, err := client.FloatValue(ctx, "feature.sampling_ratio", 0.5, evalCtx)

// Object/structured flags
config, err := client.ObjectValue(ctx, "feature", map[string]interface{}{}, evalCtx)
```

## Logging

The provider uses `log/slog` for structured logging. By default, logs at `Info` level and above are written to `stderr`.

You can provide a custom logger to control log level, format, and destination:

```go
import "log/slog"

// JSON logger with debug level
logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelDebug,
}))

provider, err := confidence.NewProvider(ctx, confidence.ProviderConfig{
    ClientSecret: "your-client-secret",
    Logger:       logger,
})
```

The provider logs at different levels: `Debug` (flag resolution details), `Info` (state updates), `Warn` (non-critical issues), and `Error` (failures).

## Troubleshooting

### Provider Creation Fails

If provider creation fails, verify:
- `ClientSecret` is correct
- Your application has network access to the Confidence CDN
- Credentials have the necessary permissions in your Confidence dashboard

### No Flag Evaluations Work

Common issues:
- Ensure you've called `openfeature.SetProviderAndWait(provider)` before creating clients
- Check that your flags are published and active in Confidence

### Performance Issues

For optimal performance:
- Reuse the same `provider` instance across your application
- Create OpenFeature clients once and reuse them

## License

See the root `LICENSE` file.
