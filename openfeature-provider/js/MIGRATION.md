# Guide on migration from "online" provider to Local provider

This should guide you from moving from `@spotify-confidence/openfeature-server-provider` ([repo](https://github.com/spotify/confidence-sdk-js/blob/main/packages/openfeature-server-provider)) to `@spotify-confidence/openfeature-server-provider-local` ([repo](https://github.com/spotify/confidence-resolver/tree/main/openfeature-provider/js)).

## What's changing?

This migration moves from an **"online" evaluation model** (network call per flag) to a **"local" evaluation model** (WebAssembly-based evaluation with background state sync). This provides:

- **Near-zero latency** for flag evaluations (vs 20-200ms per call)
- **Higher throughput** for high-traffic applications
- **Better resilience** (continues working with cached state if temporarily disconnected)

The main change is adding two new credentials (`apiClientId` and `apiClientSecret`) that require workspace admin access to create.

## Dependencies

Simply switch out your version of `@spotify-confidence/openfeature-server-provider` to the latest version of `@spotify-confidence/openfeature-server-provider-local`.

The dependencies to Openfeature (`@openfeature/server-sdk` & `@openfeature/core`) should remain intact.

## Constructing

Where you previously used either type of approaches to construct your Confidence open feature provider:
```ts
const provider = createConfidenceServerProvider({
  clientSecret: 'your-client-secret',
  fetchImplementation: fetch,
  timeout: 1000,
});

// or
const confidenceInstance: Confidence // created separately
const provider = createConfidenceServerProvider(confidenceInstance);
```

### The new constructor has two new required options:

```ts
import { createConfidenceServerProvider } from '@spotify-confidence/openfeature-server-provider-local';

const provider = createConfidenceServerProvider({
  flagClientSecret: 'your-client-secret', // this is the same client secret as before
  apiClientId: process.env.CONFIDENCE_API_CLIENT_ID!, // <----- New and mandatory!
  apiClientSecret: process.env.CONFIDENCE_API_CLIENT_SECRET!, // <-----  New and mandatory!
  // initializeTimeout?: number
  // flushInterval?: number
  // fetch?: typeof fetch (Node <18 or custom transport)
});
```

## Breaking Changes

### Parameter rename: `clientSecret` → `flagClientSecret`
The client id parameter has been renamed from `clientSecret` to `flagClientSecret`. The value can remain the same, just update the parameter name:

```ts
// Old
const provider = createConfidenceServerProvider({
  clientSecret: 'your-client-secret',
});

// New
const provider = createConfidenceServerProvider({
  flagClientSecret: 'your-client-secret',
});
```

### New required credentials
You must now provide two additional credentials:
- `apiClientId`: Confidence API Client ID
- `apiClientSecret`: Confidence API Client Secret

These require workspace admin access to create (see "Obtaining the new credentials" section below).

### What happened to the `timeout` parameter?

The old provider used `timeout` to control network request timeouts for each flag evaluation. When exceeded, default values were returned.

The new provider works differently:
- Flag evaluations happen **locally in WebAssembly** (no network calls during evaluation)
- The optional `initializeTimeout` parameter (default: 30 seconds) controls how long to wait for the initial resolver state fetch
- You typically don't need to configure timeouts anymore

**Migration**: Remove the `timeout` parameter. If you need to control initialization wait time, use `initializeTimeout` instead.

## Usage
Since this is just another Provider meant to be used with the OpenFeature SDK; the integration when accessing flag values remain the same.

## Obtaining the new credentials

`CONFIDENCE_API_CLIENT_ID` and `CONFIDENCE_API_CLIENT_SECRET` are credentials for a Confidence API Client with some specific security policies applied.

Someone with admin access in your confidence workspace can generate a new API client like this:

**1. Access the API clients in the Confidence Admin Panel.**

<img src="../../img/admin-panel-api-clients.png" alt="admin panel api clients" width="400" />

**2. Create a new API client and name it well:**

<img src="../../img/create-api-client.png" alt="create API client" width="300" />

**3. Store the API client ID and API client secret in a safe place**

The credentials need to be accessible from your application, for example as environment variables.

**4. Navigate to Policies in the Confidence Admin Panel.**

<img src="../../img/admin-panel-policies.png" alt="admin panel policies" width="400" />

**5. Create a new Policy where you assign `Flags Resolver Logger` and `Flags Resolver Sidecar` to your created API client:**

<img src="../../img/create-policy.png" alt="create policy" width="300" />

## Key Differences: Online vs Local Provider

### Performance
- **Old (online) provider**: Makes a network call to Confidence for every flag evaluation (20-200ms per call)
- **New (local) provider**: Evaluates flags locally in WebAssembly with near-zero latency

The local provider fetches resolver state in the background every 30 seconds and evaluates flags quickly, in-process.

### Credentials
- **Old provider**: 1 credential (`clientSecret`)
- **New provider**: 3 credentials (`flagClientSecret`, `apiClientId`, `apiClientSecret`)

### Trade-offs
- **Pros**: Dramatically faster evaluations, higher throughput, works offline with cached state
- **Cons**: More complex setup, requires admin to create API client with specific policies
- **Note**: Sticky assignments still require a network call as a fallback

## Testing and verifying

As with any good software development practice we suggest you test this locally or in a test environment before launching in production.

To enable debug logging during testing, install the `debug` package and set the `DEBUG` environment variable:

```bash
yarn add debug
DEBUG=cnfd:* node your-app.js
```

This will show detailed logs about resolver state fetches, retries, and flag evaluations.

We suggest that you create a new flag in Confidence and resolve that flag using the new OpenFeature provider. To verify correctness you should:
#### 1 Verify the expected result in the flag evaluation in your application
#### 2 Verify that flag resolves are visible
<img src="../../img/resolve-graph.png" alt="resolve graph" width="300" />

#### 3 Verify applies are registered correctly - "last apply just now"
<img src="../../img/applies-registered.png" alt="applies registered" width="300" />

#### 4 Verify that flag rules are registering "resolved X times" updates
<img src="../../img/rule-resolved.png" alt="rule resolved" width="300" />

