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
    evalCtx := openfeature.NewEvaluationContext("user-123", map[string]interface{}{
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

### Environment Variables

Configure the provider behavior using environment variables:

- `CONFIDENCE_RESOLVER_POLL_INTERVAL_SECONDS`: How often to poll Confidence to get updates (default: `30` seconds)

### ProviderConfig

The `ProviderConfig` struct contains all configuration options for the provider:

#### Required Fields

- `ClientSecret` (string): The client secret used for authentication and flag evaluation

#### Optional Fields

- `Logger` (*slog.Logger): Custom logger for provider operations. If not provided, a default text logger is created. See [Logging](#logging) for details.
- `TransportHooks` (TransportHooks): Custom transport hooks for advanced use cases (e.g., custom gRPC interceptors, HTTP transport wrapping, TLS configuration)

#### Advanced: Testing with Custom State Provider

For testing purposes only, you can provide a custom `StateProvider` and `FlagLogger` to supply resolver state and control logging behavior:

```go
// WARNING: This is for testing only. Do not use in production.
provider, err := confidence.NewProviderForTest(ctx,
    confidence.ProviderTestConfig{
        StateProvider: myCustomStateProvider,
        FlagLogger:    myCustomFlagLogger,
        ClientSecret:  "your-client-secret",
        Logger:        myCustomLogger, // Optional: custom logger
    },
)
```

**Important**: This configuration requires you to provide both a `StateProvider` and `FlagLogger`. For production deployments, always use `NewProvider()` with `ProviderConfig`.

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
