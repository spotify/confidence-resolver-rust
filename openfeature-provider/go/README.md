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
        APIClientID:     "your-api-client-id",
        APIClientSecret: "your-api-client-secret",
        ClientSecret:    "your-client-secret",
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

- `APIClientID` (string): OAuth client ID for Confidence IAM
- `APIClientSecret` (string): OAuth client secret for Confidence IAM
- `ClientSecret` (string): The flag client secret used during evaluation

#### Optional Fields

- `Logger` (*slog.Logger): Custom logger for provider operations. If not provided, a default text logger is created. See [Logging](#logging) for details.
- `ResolverStateServiceAddr` (string): Custom address for the resolver state service. Defaults to `edge-grpc.spotify.com`
- `FlagLoggerServiceAddr` (string): Custom address for the flag logger service. Defaults to `edge-grpc.spotify.com`
- `AuthServiceAddr` (string): Custom address for the auth service. Defaults to `edge-grpc.spotify.com`

> **Note**: The optional service address fields are for advanced use cases only. For production deployments, use the default global region by omitting these fields.

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

## Credentials

You need two types of credentials from your [Confidence dashboard](https://confidence.spotify.com/):

1. **API Credentials** (for authenticating with the Confidence API):
   - `APIClientID`: OAuth client ID for your Confidence application
   - `APIClientSecret`: OAuth client secret for your Confidence application

2. **Client Secret** (for flag resolution authentication):
   - `ClientSecret`: Application-specific identifier for flag evaluation


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
    APIClientID:     "your-api-client-id",
    APIClientSecret: "your-api-client-secret",
    ClientSecret:    "your-client-secret",
    Logger:          logger,
})
```

The provider logs at different levels: `Debug` (flag resolution details), `Info` (state updates), `Warn` (non-critical issues), and `Error` (failures).

## Troubleshooting

### Provider Creation Fails

If provider creation fails, verify:
- `APIClientID`, `APIClientSecret`, and `ClientSecret` are correct
- Your application has network access to `edge-grpc.spotify.com` (or custom service addresses if configured)
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
