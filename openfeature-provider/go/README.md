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

## Getting Your Credentials

You'll need a **client secret** from Confidence to use this provider.

**📖 See the [Integration Guide: Getting Your Credentials](../INTEGRATION_GUIDE.md#getting-your-credentials)** for step-by-step instructions on:
- How to navigate the Confidence dashboard
- Creating a Backend integration
- Creating a test flag for verification
- Best practices for credential storage

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

    // Create provider with your client secret
    provider, err := confidence.NewProvider(ctx, confidence.ProviderConfig{
        ClientSecret: "your-client-secret", // Get from Confidence dashboard
    })
    if err != nil {
        log.Fatalf("Failed to create provider: %v", err)
    }

    // Set the provider and wait for initialization
    openfeature.SetProviderAndWait(provider)

    // Get a client
    client := openfeature.NewClient("my-app")

    // Create evaluation context with user attributes for targeting
    evalCtx := openfeature.NewEvaluationContext("user-123", map[string]interface{}{
        "country": "US",
        "plan":    "premium",
    })

    // Evaluate a flag
    value, err := client.BooleanValue(ctx, "test-flag.enabled", false, evalCtx)
    if err != nil {
        log.Printf("Flag evaluation failed, using default: %v", err)
    }

    log.Printf("Flag value: %v", value)
}
```

## Evaluation Context

The evaluation context contains information about the user/session being evaluated for targeting and A/B testing.

### Go-Specific Examples

```go
// Simple attributes
evalCtx := openfeature.NewEvaluationContext("user-123", map[string]interface{}{
    "country": "US",
    "plan":    "premium",
    "age":     25,
})
```

## Error Handling

The provider uses a **default value fallback** pattern - when evaluation fails, it returns your specified default value instead of throwing an error.

**📖 See the [Integration Guide: Error Handling](../INTEGRATION_GUIDE.md#error-handling)** for:
- Common failure scenarios
- Error codes and meanings
- Production best practices
- Monitoring recommendations

### Go-Specific Examples

```go
// The provider returns the default value on errors
value, err := client.BooleanValue(ctx, "my-flag.enabled", false, evalCtx)
if err != nil {
    // Log the error for debugging
    log.Printf("Flag evaluation failed, using default: %v", err)
}
// value will be 'false' if evaluation failed

// For critical flags, you might want to check the error
if err != nil && strings.Contains(err.Error(), "FLAG_NOT_FOUND") {
    log.Warn("Flag 'my-flag' not found in Confidence - check flag name")
}

// During initialization with timeout
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

provider, err := confidence.NewProvider(ctx, confidence.ProviderConfig{
    ClientSecret: "your-client-secret",
})
if err != nil {
    log.Fatalf("Provider initialization failed: %v", err)
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

## Shutdown

**Important**: Always shut down the provider when your application exits to ensure proper cleanup and log flushing.

```go
// Shutdown the provider on application exit
    openfeature.Shutdown()
```

### What Happens During Shutdown?

1. **Flushes pending logs** to Confidence (exposure events, resolve analytics)
2. **Closes gRPC connections** and releases network resources
3. **Stops background tasks** (state polling, log batching)
4. **Releases WASM instance** and memory

The shutdown respects the context timeout you provide.

## License

See the root `LICENSE` file.
