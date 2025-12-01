# Confidence OpenFeature Providers — Unified Integration Guide

![Status: Experimental](https://img.shields.io/badge/status-experimental-orange)

This guide covers integration with Confidence feature flags using OpenFeature providers. All providers implement the [OpenFeature specification](https://openfeature.dev/specification), allowing you to use standard OpenFeature APIs for flag evaluation.

---

## Table of Contents

1. [Overview](#overview)
2. [Getting Your Credentials](#getting-your-credentials)
3. [Installation](#installation)
4. [Quick Start](#quick-start)
5. [Flag Evaluation](#flag-evaluation)
6. [Error Handling](#error-handling)
7. [Shutdown](#shutdown)
8. [Configuration](#configuration)
9. [Sticky Assignments](#sticky-assignments)
10. [Logging](#logging)
11. [Language-Specific Notes](#language-specific-notes)

---

## Overview

Confidence provides OpenFeature providers for multiple languages:

| Language | Package | Resolver Type |
|----------|---------|---------------|
| **Go** | `github.com/spotify/confidence-resolver/openfeature-provider/go` | Local (WASM) |
| **Java** | `com.spotify.confidence:openfeature-provider-local` | Local (WASM) |
| **JavaScript** | `@spotify-confidence/openfeature-server-provider-local` | Local (WASM) |
| **Ruby** | `confidence-openfeature-provider` | Online |

### Features (Local Resolver Providers)

- **Local Resolution**: Evaluates feature flags locally using WebAssembly (WASM)
- **Low Latency**: No network calls during flag evaluation
- **Automatic Sync**: Periodically syncs flag configurations from Confidence (default: every 30s)
- **Exposure Logging**: Built-in exposure logging and analytics
- **OpenFeature Compatible**: Works with standard OpenFeature SDKs

---

## Getting Your Credentials

Before integrating any Confidence provider, you need a **client secret** from your Confidence account:

1. Log into the [Confidence dashboard](https://confidence.spotify.com/)
2. Navigate to the **Clients** section
3. Create a new client secret for the client you intend to use (or create a new client)
4. Select **Backend** as integration type

> ⚠️ **Important**: Never expose your Backend client secret outside your organization.

---

## Installation

### Go

```bash
go get github.com/spotify/confidence-resolver/openfeature-provider/go
go mod tidy
```

**Requirements:** Go 1.24+, OpenFeature Go SDK 1.16.0+

### Java

Add to your `pom.xml`:

```xml
<dependency>
    <groupId>com.spotify.confidence</groupId>
    <artifactId>openfeature-provider-local</artifactId>
    <version>0.8.0</version>
</dependency>
```

**Requirements:** Java 17+, OpenFeature SDK 1.6.1+

### JavaScript

```bash
yarn add @spotify-confidence/openfeature-server-provider-local

# Optional: enable debug logs
yarn add debug
```

**Requirements:** Node.js 18+ (or provide compatible `fetch`), WebAssembly support

### Ruby

```bash
bundle add confidence-openfeature-provider

# Or without bundler:
gem install confidence-openfeature-provider
```

---

## Quick Start

### Go

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
        ClientSecret: "your-client-secret",
    })
    if err != nil {
        log.Fatalf("Failed to create provider: %v", err)
    }
    defer provider.Shutdown(ctx)

    // Set the provider and wait for initialization
    openfeature.SetProviderAndWait(provider)

    // Get a client
    client := openfeature.NewClient("my-app")

    // Create evaluation context
    evalCtx := openfeature.NewEvaluationContext("user-123", map[string]interface{}{
        "country": "US",
        "plan":    "premium",
    })

    // Evaluate a flag
    value, err := client.BooleanValue(ctx, "test-flag.enabled", false, evalCtx)
    log.Printf("Flag value: %v", value)
}
```

### Java

```java
import com.spotify.confidence.OpenFeatureLocalResolveProvider;
import dev.openfeature.sdk.OpenFeatureAPI;
import dev.openfeature.sdk.Client;
import dev.openfeature.sdk.MutableContext;

// Create and register the provider
OpenFeatureLocalResolveProvider provider =
    new OpenFeatureLocalResolveProvider("your-client-secret");
OpenFeatureAPI.getInstance().setProviderAndWait(provider);

// Get a client
Client client = OpenFeatureAPI.getInstance().getClient();

// Create evaluation context
MutableContext ctx = new MutableContext("user-123");
ctx.add("country", "US");
ctx.add("plan", "premium");

// Evaluate a flag
Boolean enabled = client.getBooleanValue("test-flag.enabled", false, ctx);
System.out.println("Flag value: " + enabled);

// Shutdown when done (see Shutdown section)
```

### JavaScript

```typescript
import { OpenFeature } from '@openfeature/server-sdk';
import { createConfidenceServerProvider } from '@spotify-confidence/openfeature-server-provider-local';

const provider = createConfidenceServerProvider({
  flagClientSecret: process.env.CONFIDENCE_FLAG_CLIENT_SECRET!,
});

// Wait for initialization
await OpenFeature.setProviderAndWait(provider);

const client = OpenFeature.getClient();

// Create evaluation context
const context = {
  targetingKey: 'user-123',
  country: 'US',
  plan: 'premium',
};

// Evaluate a flag
const enabled = await client.getBooleanValue('test-flag.enabled', false, context);
console.log('Flag value:', enabled);

// Shutdown when done
await provider.onClose();
```

### Ruby

```ruby
require "openfeature/sdk"
require "confidence/openfeature"

# Configure provider
OpenFeature::SDK.configure do |config|
  api_client = Confidence::OpenFeature::APIClient.new(
    client_secret: ENV['CONFIDENCE_CLIENT_SECRET'],
    region: Confidence::OpenFeature::Region::EU
  )
  config.provider = Confidence::OpenFeature::Provider.new(api_client: api_client)
end

# Create client
client = OpenFeature::SDK.build_client(name: "my-app")

# Create evaluation context
ctx = OpenFeature::SDK::EvaluationContext.new(
  targeting_key: "user-123",
  attributes: { country: "US", plan: "premium" }
)

# Evaluate a flag
value = client.fetch_boolean_value(
  flag_key: "test-flag.enabled",
  default_value: false,
  evaluation_context: ctx
)
puts "Flag value: #{value}"
```

---

## Flag Evaluation

All providers support standard OpenFeature value types:

| Type | Method | Default Example |
|------|--------|-----------------|
| Boolean | `getBooleanValue` / `fetch_boolean_value` | `false` |
| String | `getStringValue` / `fetch_string_value` | `"default"` |
| Integer | `getIntValue` / `fetch_integer_value` | `0` |
| Float | `getFloatValue` / `fetch_number_value` | `0.0` |
| Object | `getObjectValue` / `fetch_object_value` | `{}` |

### Nested Flag Values

Use dot notation to access nested values within structured flags:

```typescript
// Flag "experiments" with payload: { groupA: { ratio: 0.5 } }
const ratio = await client.getNumberValue('experiments.groupA.ratio', 0, context);
```

---

## Error Handling

All Confidence providers use a **default value fallback** pattern. When evaluation fails, your specified default value is returned instead of throwing an error.

### Common Failure Scenarios

| Scenario | What Happens | Common Causes |
|----------|--------------|---------------|
| **Flag doesn't exist** | Returns default | Flag not created, wrong name, not published |
| **Type mismatch** | Returns default | Requesting boolean for string flag |
| **Network failure** | Returns default | API unreachable (Ruby) or initialization failed |
| **Provider not ready** | Returns default | Called before initialization complete |
| **Invalid context** | Returns default | Malformed attributes |

### Error Codes

- `FLAG_NOT_FOUND`: The flag doesn't exist in Confidence
- `TYPE_MISMATCH`: Wrong value type requested
- `PROVIDER_NOT_READY`: Provider still initializing
- `PARSE_ERROR`: Response couldn't be parsed
- `GENERAL_ERROR`: Other errors

### Getting Error Details

**Go:**
```go
value, err := client.BooleanValue(ctx, "my-flag.enabled", false, evalCtx)
if err != nil {
    log.Printf("Flag evaluation failed: %v", err)
}
```

**Java:**
```java
FlagEvaluationDetails<Boolean> details = client.getBooleanDetails("my-flag.enabled", false, ctx);
if (details.getErrorCode() != null) {
    System.err.println("Error: " + details.getErrorMessage());
}
```

**JavaScript:**
```typescript
const details = await client.getBooleanDetails('my-flag.enabled', false, context);
if (details.errorCode) {
    console.error('Error:', details.errorMessage);
}
```

**Ruby:**
```ruby
details = client.fetch_boolean_details(
  flag_key: "my-flag.enabled",
  default_value: false,
  evaluation_context: context
)
if details.error_code
  Rails.logger.error("Error: #{details.error_message}")
end
```

### Best Practices

1. **Choose safe defaults** — Default to "off" for risky features
2. **Log errors for debugging** — Track evaluation failures in your monitoring
3. **Monitor error rates** — Alert if error rate exceeds threshold (e.g., >5%)
4. **Test error scenarios** — Verify app works when Confidence is unreachable

---

## Shutdown

Always shut down the provider when your application exits to ensure proper cleanup and log flushing.

### Go

```go
defer func() {
    if err := provider.Shutdown(ctx); err != nil {
        log.Printf("Error during shutdown: %v", err)
    }
}()
```

### Java

```java
// Important: Call shutdown directly on the provider instance
OpenFeatureAPI.getInstance().getProvider().shutdown();
```

> **Note:** Due to an [upstream issue](https://github.com/open-feature/java-sdk/issues/1745) in the OpenFeature Java SDK, calling `OpenFeatureAPI.getInstance().shutdown()` may not wait for completion. Call `shutdown()` directly on the provider.

### JavaScript

```typescript
// Flush logs and clean up
await provider.onClose();
```

### Ruby

```ruby
# In application shutdown handler
at_exit do
  OpenFeature::SDK.shutdown
end

# Rails: config/puma.rb
on_worker_shutdown do
  OpenFeature::SDK.shutdown
end
```

### What Happens During Shutdown

| Action | Local Providers | Ruby (Online) |
|--------|----------------|---------------|
| Flush pending logs | ✅ | N/A |
| Close network connections | ✅ gRPC/HTTP | ✅ HTTP |
| Stop background tasks | ✅ Polling, batching | N/A |
| Release WASM resources | ✅ | N/A |

---

## Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `CONFIDENCE_RESOLVER_POLL_INTERVAL_SECONDS` | State refresh interval | `30` |
| `CONFIDENCE_NUMBER_OF_WASM_INSTANCES` | WASM instances (Java only) | CPU cores |

### Provider Options

**Go:**
```go
confidence.ProviderConfig{
    ClientSecret:   "your-secret",
    Logger:         customLogger,       // Optional: *slog.Logger
    TransportHooks: customHooks,        // Optional: custom gRPC/HTTP hooks
}
```

**Java:**
```java
// Custom channel factory for advanced scenarios
LocalProviderConfig config = new LocalProviderConfig(customChannelFactory);
OpenFeatureLocalResolveProvider provider =
    new OpenFeatureLocalResolveProvider(config, "client-secret");
```

**JavaScript:**
```typescript
createConfidenceServerProvider({
  flagClientSecret: 'your-secret',
  initializeTimeout: 30000,  // Max ms to wait for init
  flushInterval: 10000,      // Log flush interval in ms
  fetch: customFetch,        // Custom fetch implementation
});
```

**Ruby:**
```ruby
Confidence::OpenFeature::APIClient.new(
  client_secret: ENV['CONFIDENCE_CLIENT_SECRET'],
  region: Confidence::OpenFeature::Region::EU  # or Region::US
)
```

---

## Sticky Assignments

Sticky assignments ensure users receive consistent variant assignments across evaluations. This is essential for A/B testing integrity.

### How It Works

1. First evaluation resolves the flag using local WASM resolver
2. If sticky assignment is needed, provider fetches/stores materialization data
3. Subsequent evaluations return the same variant, even if:
   - User's context attributes change
   - Flag's targeting rules are modified
   - New assignments are paused

### Server-Managed (Default)

By default, sticky assignments are managed by Confidence servers:

- **Zero configuration** — Works out of the box
- **90-day TTL** — Automatically renewed on access
- **Global availability** — Consistent across all your services
- **Latency trade-off** — Requires network call (50-200ms typical)

### Custom Storage (Java Only)

For lower latency, implement custom storage:

```java
MaterializationRepository repository = new RedisMaterializationRepository(jedisPool, "myapp");
OpenFeatureLocalResolveProvider provider = new OpenFeatureLocalResolveProvider(
    clientSecret,
    repository
);
```

See [STICKY_RESOLVE.md](../openfeature-provider/java/STICKY_RESOLVE.md) for implementation details.

---

## Logging

### Go

Uses `log/slog` for structured logging:

```go
import "log/slog"

logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelDebug,
}))

provider, _ := confidence.NewProvider(ctx, confidence.ProviderConfig{
    ClientSecret: "your-secret",
    Logger:       logger,
})
```

### JavaScript

Uses the optional `debug` library:

```bash
# Enable all logs
DEBUG=cnfd:* node app.js

# Specific levels
DEBUG=cnfd:info,cnfd:fetch:* node app.js
```

Log namespaces:
- `cnfd:debug` / `cnfd:info` / `cnfd:warn` / `cnfd:error`
- `cnfd:fetch:*` — Network request details

### Java / Ruby

Standard logging frameworks are used. Configure your application's logging as usual.

---

## Language-Specific Notes

### Go

- Uses goroutines for background state fetching and log flushing
- Context cancellation is respected for timeouts
- Provider implements OpenFeature's `FeatureProvider` interface

#### Testing with Custom State Provider

For testing purposes only, you can provide a custom `StateProvider` and `FlagLogger`:

```go
// WARNING: This is for testing only. Do not use in production.
provider, err := confidence.NewProviderForTest(ctx,
    confidence.ProviderTestConfig{
        StateProvider: myCustomStateProvider,
        FlagLogger:    myCustomFlagLogger,
        ClientSecret:  "your-client-secret",
        Logger:        myCustomLogger, // Optional
    },
)
```

> **Important**: This configuration requires you to provide both a `StateProvider` and `FlagLogger`. For production deployments, always use `NewProvider()` with `ProviderConfig`.

### Java

- WASM instance count can be tuned via `CONFIDENCE_NUMBER_OF_WASM_INSTANCES`
- Custom `ChannelFactory` allows advanced gRPC configuration
- Direct `shutdown()` call required due to SDK issue

### JavaScript

- WASM is bundled with the package (no extra config for Node.js)
- Browser builds use `import.meta.url` for WASM resolution
- Custom `fetch` can be injected for testing or Node.js < 18

#### Migration from Online Resolver

If you're currently using the [online resolver](https://github.com/spotify/confidence-sdk-js/tree/main/packages/openfeature-server-provider), migration to the local resolver is straightforward. See the [Migration Guide](../openfeature-provider/js/MIGRATION.md) for details.

#### Browser Usage

The package exports a browser ESM build that compiles the WASM via streaming and stores a singleton Promise for initialization. This ensures the WASM module is only loaded once regardless of how many times the provider is instantiated.

### Ruby

> ⚠️ **Important**: The Ruby provider uses the **online resolver** approach — **each flag evaluation makes a network call** to Confidence. This introduces latency (typically 50-200ms) compared to local WASM evaluation. Proper error handling is critical for network failures.

- Region must be specified (EU or US)
- Multi-process servers (Puma, Unicorn) need per-worker shutdown

#### Rails Shutdown Patterns

In multi-process servers, each worker process should shut down its own provider instance:

```ruby
# config/puma.rb
on_worker_shutdown do
  ActiveSupport::Notifications.instrument("shutdown.confidence") do
    OpenFeature::SDK.shutdown
  end
end

# config/environments/production.rb
config.after_initialize do
  at_exit do
    OpenFeature::SDK.shutdown
  end
end
```

---

## Additional Resources

- [Confidence Documentation](https://confidence.spotify.com/docs)
- [OpenFeature Specification](https://openfeature.dev/specification)
- [Sticky Assignments Technical Guide](../STICKY_ASSIGNMENTS.md)

---

## License

See [LICENSE](../LICENSE) for details.

