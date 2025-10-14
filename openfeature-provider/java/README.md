# Confidence OpenFeature Local Provider

![Status: Experimental](https://img.shields.io/badge/status-experimental-orange)

A high-performance OpenFeature provider for [Confidence](https://confidence.spotify.com/) feature flags that evaluates flags locally for minimal latency.

## Features

- **Local Resolution**: Evaluates feature flags locally using WebAssembly (WASM)
- **Low Latency**: No network calls during flag evaluation
- **Automatic Sync**: Periodically syncs flag configurations from Confidence
- **Exposure Logging**: Fully supported exposure logging (and other resolve analytics)
- **OpenFeature Compatible**: Works with the standard OpenFeature SDK

## Installation

Add this dependency to your `pom.xml`:
<!-- x-release-please-start-version -->
```xml
<dependency>
    <groupId>com.spotify.confidence</groupId>
    <artifactId>openfeature-provider-local</artifactId>
    <version>0.6.1-SNAPSHOT</version>
</dependency>
```
<!---x-release-please-end-->

## Quick Start

```java
import com.spotify.confidence.ApiSecret;
import com.spotify.confidence.OpenFeatureLocalResolveProvider;
import dev.openfeature.sdk.OpenFeatureAPI;
import dev.openfeature.sdk.Client;

// Create API credentials
ApiSecret apiSecret = new ApiSecret("your-client-id", "your-client-secret");
String clientSecret = "your-application-client-secret";

// Create and register the provider
OpenFeatureLocalResolveProvider provider = 
    new OpenFeatureLocalResolveProvider(apiSecret, clientSecret);
OpenFeatureAPI.getInstance().setProvider(provider);

// Use OpenFeature client
Client client = OpenFeatureAPI.getInstance().getClient();
String value = client.getStringValue("my-flag", "default-value");
```

## Configuration


### Exposure Logging

Enable or disable exposure logging:

```java
// Enable exposure logging (default)
new OpenFeatureLocalResolveProvider(apiSecret, clientSecret, true);

// Disable exposure logging
new OpenFeatureLocalResolveProvider(apiSecret, clientSecret, false);
```

## Credentials

You need two types of credentials:

1. **API Secret** (`ApiSecret`): For authenticating with the Confidence API
   - Contains `clientId` and `clientSecret` for your Confidence application
   
2. **Client Secret** (`String`): For flag resolution authentication
   - Application-specific secret for flag evaluation

Both can be obtained from your Confidence dashboard.

## Sticky Resolve

The provider supports **Sticky Resolve** for consistent variant assignments across flag evaluations. This ensures users receive the same variant even when their targeting attributes change, and enables pausing experiment intake.

**By default, sticky assignments are managed by Confidence servers.** When sticky assignment data is needed, the provider makes a network call to Confidence, which maintains the sticky repository server-side with automatic 90-day TTL management. This is a fully supported production approach that requires no additional setup.


Optionally, you can implement a custom `MaterializationRepository` to manage sticky assignments in your own storage (Redis, database, etc.) to eliminate network calls and improve latency:

```java
// Optional: Custom storage for sticky assignments
MaterializationRepository repository = new RedisMaterializationRepository(jedisPool, "myapp");
OpenFeatureLocalResolveProvider provider = new OpenFeatureLocalResolveProvider(
    apiSecret,
    clientSecret,
    repository
);
```

For detailed information on how sticky resolve works and how to implement custom storage backends, see [STICKY_RESOLVE.md](STICKY_RESOLVE.md).

## Requirements

- Java 17+
- OpenFeature SDK 1.6.1+