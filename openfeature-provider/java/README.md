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
    <version>0.7.4</version>
</dependency>
```
<!-- x-release-please-end -->

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
OpenFeatureAPI.getInstance().setProviderAndWait(provider); // important to use setProviderAndWait()

// Use OpenFeature client
Client client = OpenFeatureAPI.getInstance().getClient();
String value = client.getStringValue("my-flag", "default-value");
```

## Configuration

### Environment Variables

Configure the provider behavior using environment variables:

- `CONFIDENCE_RESOLVER_POLL_INTERVAL_SECONDS`: How often to poll Confidence to get updates (default: `300` seconds)

// Deprecated in favour of a custom ChannelFactory:
- `CONFIDENCE_DOMAIN`: Override the default Confidence service endpoint (default: `edge-grpc.spotify.com`)
- `CONFIDENCE_GRPC_PLAINTEXT`: Use plaintext gRPC connections instead of TLS (default: `false`)

### Custom Channel Factory (Advanced)

For testing or advanced production scenarios, you can provide a custom `ChannelFactory` to control how gRPC channels are created:

```java
import com.spotify.confidence.LocalProviderConfig;
import com.spotify.confidence.ChannelFactory;

// Example: Custom channel factory for testing with in-process server
ChannelFactory mockFactory = (target, interceptors) ->
    InProcessChannelBuilder.forName("test-server")
        .usePlaintext()
        .intercept(interceptors.toArray(new ClientInterceptor[0]))
        .build();

ApiSecret apiSecret = new ApiSecret("client-id", "client-secret");
LocalProviderConfig config = new LocalProviderConfig(apiSecret, mockFactory);
OpenFeatureLocalResolveProvider provider =
    new OpenFeatureLocalResolveProvider(config, "client-secret");
```

This is particularly useful for:
- **Unit testing**: Inject in-process channels with mock gRPC servers
- **Integration testing**: Point to local test servers
- **Production customization**: Custom TLS settings, proxies, or connection pooling
- **Debugging**: Add custom logging or tracing interceptors

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