# Confidence OpenFeature Local Provider Demo

This demo application demonstrates how to use the Confidence OpenFeature Local Provider in Go.

## Overview

The demo shows how to:
- Initialize the LocalResolverFactory with authentication
- Create and register the OpenFeature provider
- Evaluate different types of flags (boolean, string, integer, float, object)
- Use nested flag paths
- Pass evaluation context with targeting keys

## Prerequisites

1. **Go 1.21 or later** installed
2. **Confidence Account** with API credentials
3. **WASM Resolver Module** (resolver.wasm file)

## Configuration

The demo uses environment variables for configuration:

### Required Credentials

```bash
export CONFIDENCE_API_CLIENT_ID="your-api-client-id"
export CONFIDENCE_API_CLIENT_SECRET="your-api-client-secret"
export CONFIDENCE_CLIENT_SECRET="your-application-client-secret"
```

### Optional Configuration

```bash
# Service addresses (defaults to EU region)
export CONFIDENCE_RESOLVER_STATE_SERVICE_ADDR="resolver.eu.confidence.dev:443"
export CONFIDENCE_FLAG_LOGGER_SERVICE_ADDR="events.eu.confidence.dev:443"
export CONFIDENCE_AUTH_SERVICE_ADDR="iam.eu.confidence.dev:443"

# WASM file path (default: ../../../bin/resolver.wasm)
export CONFIDENCE_WASM_FILE_PATH="/path/to/resolver.wasm"

# State fetching interval in seconds (default: 300 = 5 minutes)
export CONFIDENCE_RESOLVER_POLL_INTERVAL_SECONDS="300"
```

### For US Region

If you're using the US region, set these environment variables:

```bash
export CONFIDENCE_RESOLVER_STATE_SERVICE_ADDR="resolver.us.confidence.dev:443"
export CONFIDENCE_FLAG_LOGGER_SERVICE_ADDR="events.us.confidence.dev:443"
export CONFIDENCE_AUTH_SERVICE_ADDR="iam.us.confidence.dev:443"
```

## Getting Your Credentials

1. **API Client ID and Secret**:
   - Go to your Confidence dashboard
   - Navigate to Settings → API Credentials
   - Create a new API credential or use an existing one
   - Copy the Client ID and Client Secret

2. **Application Client Secret**:
   - Go to your Confidence dashboard
   - Navigate to your application settings
   - Copy the Client Secret for your application

3. **WASM Resolver Module**:
   - Download from the Confidence SDK releases
   - Or build from source: `cd ../../../wasm && cargo build --release`
   - The WASM file should be at `../../../bin/resolver.wasm`

## Running the Demo

1. **Install dependencies**:
   ```bash
   go mod tidy
   ```

2. **Set environment variables**:
   ```bash
   export CONFIDENCE_API_CLIENT_ID="your-api-client-id"
   export CONFIDENCE_API_CLIENT_SECRET="your-api-client-secret"
   export CONFIDENCE_CLIENT_SECRET="your-client-secret"
   ```

3. **Run the demo**:
   ```bash
   go run main.go
   ```

## Expected Output

```
Starting Confidence OpenFeature Local Provider Demo
Auth Service: iam.eu.confidence.dev:443
Resolver State Service: resolver.eu.confidence.dev:443
Flag Logger Service: events.eu.confidence.dev:443
WASM File: ../../../bin/resolver.wasm

Loaded WASM module (XXXXX bytes)
Creating LocalResolverFactory...
LocalResolverFactory created successfully
Creating OpenFeature provider...
OpenFeature provider registered

=== Flag Evaluation Demo ===

1. Evaluating boolean flag 'feature-toggle':
  Value: true
  Variant: control
  Reason: TARGETING_MATCH

2. Evaluating string flag 'welcome-message':
  Value: Welcome!
  Variant: variant-a
  Reason: TARGETING_MATCH

...

=== Demo Complete ===
```

## Demo Examples

The demo includes examples of:

### 1. Boolean Flag
```go
boolResult := client.BooleanValueDetails(ctx, "feature-toggle", false, evalCtx)
```

### 2. String Flag
```go
stringResult := client.StringValueDetails(ctx, "welcome-message", "Hello!", evalCtx)
```

### 3. Integer Flag
```go
intResult := client.IntValueDetails(ctx, "max-items", 10, evalCtx)
```

### 4. Float Flag
```go
floatResult := client.FloatValueDetails(ctx, "discount-rate", 0.0, evalCtx)
```

### 5. Object Flag
```go
objectResult := client.ObjectValueDetails(ctx, "theme-config", map[string]interface{}{}, evalCtx)
```

### 6. Nested Flag Value (Dot Notation)
```go
// Returns flag_value["primaryColor"]
stringResult := client.StringValueDetails(ctx, "theme-config.primaryColor", "#000000", evalCtx)
```

### 7. Targeting Key
```go
evalCtx := openfeature.NewTargetingKey("session-xyz")
boolResult := client.BooleanValueDetails(ctx, "beta-feature", false, evalCtx)
```

## Evaluation Context

The demo shows different ways to create evaluation contexts:

```go
// With targeting key and attributes
evalCtx := openfeature.NewEvaluationContext(
    "user-123",
    map[string]interface{}{
        "email":  "user@example.com",
        "region": "US",
        "plan":   "premium",
    },
)

// Just targeting key
evalCtx := openfeature.NewTargetingKey("session-xyz")
```

## Troubleshooting

### "FLAG_NOT_FOUND" Errors

If you see flag not found errors:
1. Verify you have flags created in your Confidence account
2. Check that the flag names match exactly
3. Ensure your client secret is correct

### Authentication Errors

If you see authentication errors:
1. Verify your API credentials are correct
2. Check that you're using the correct region (EU vs US)
3. Ensure your credentials have the necessary permissions

### WASM File Not Found

If you see WASM file errors:
1. Check the file path: `CONFIDENCE_WASM_FILE_PATH`
2. Build the WASM module if needed
3. Verify the file exists and is readable

### Connection Errors

If you see connection errors:
1. Check your internet connection
2. Verify the service addresses are correct for your region
3. Check if you're behind a proxy or firewall

## Next Steps

After running the demo:

1. **Create your own flags** in the Confidence dashboard
2. **Modify the demo** to evaluate your flags
3. **Integrate into your application** using the same pattern
4. **Add your own evaluation context** attributes based on your use case

## Integration Example

Here's a minimal example of integrating the provider into your application:

```go
package main

import (
    "context"
    "log"
    "os"

    "github.com/open-feature/go-sdk/openfeature"
    provider "github.com/spotify/confidence-resolver-rust/openfeature-provider/go"
    "github.com/tetratelabs/wazero"
)

func main() {
    ctx := context.Background()

    // Load WASM
    wasmBytes, _ := os.ReadFile("resolver.wasm")
    runtime := wazero.NewRuntime(ctx)
    defer runtime.Close(ctx)

    // Create factory
    factory, err := provider.NewLocalResolverFactory(
        ctx, runtime, wasmBytes,
        "resolver.eu.confidence.dev:443",
        "events.eu.confidence.dev:443",
        "iam.eu.confidence.dev:443",
        os.Getenv("CONFIDENCE_API_CLIENT_ID"),
        os.Getenv("CONFIDENCE_API_CLIENT_SECRET"),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer factory.Shutdown(ctx)

    // Register provider
    confidenceProvider := provider.NewLocalResolverProvider(
        factory,
        os.Getenv("CONFIDENCE_CLIENT_SECRET"),
    )
    defer confidenceProvider.Shutdown()
    openfeature.SetProvider(confidenceProvider)

    // Use OpenFeature client
    client := openfeature.NewClient("my-app")
    enabled := client.BooleanValue(ctx, "my-feature", false, openfeature.EvaluationContext{})

    if enabled {
        log.Println("Feature enabled!")
    }
}
```

## Resources

- [Confidence Documentation](https://confidence.dev/docs)
- [OpenFeature Go SDK](https://github.com/open-feature/go-sdk)
- [OpenFeature Specification](https://openfeature.dev/specification)
