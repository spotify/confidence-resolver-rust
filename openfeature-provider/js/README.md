# Confidence OpenFeature Local Provider for JavaScript

OpenFeature provider for the Spotify Confidence resolver (local mode, powered by WebAssembly). It periodically fetches resolver state, evaluates flags locally, and flushes evaluation logs to the Confidence backend.

## Features
- Local flag evaluation via WASM (no per-eval network calls)
- Automatic state refresh and batched flag log flushing
- Pluggable `fetch` with retries, timeouts and routing
- Optional logging using `debug`

## Requirements
- Node.js 18+ (built-in `fetch`) or provide a compatible `fetch`
- WebAssembly support (Node 18+/modern browsers)

---

## Installation

```bash
yarn add @spotify-confidence/openfeature-server-provider-local

# Optional: enable logs by installing the peer dependency
yarn add debug
```

Notes:
- `debug` is an optional peer. Install it if you want logs. Without it, logging is a no-op.
- Types and bundling are ESM-first; Node is supported, and a browser build is provided for modern bundlers.

---

## Getting Your Credentials

You'll need a **client secret** from Confidence to use this provider.

**📖 See the [Integration Guide: Getting Your Credentials](../INTEGRATION_GUIDE.md#getting-your-credentials)** for step-by-step instructions on:
- How to navigate the Confidence dashboard
- Creating a Backend integration
- Creating a test flag for verification
- Best practices for credential storage

---

## Quick start (Node)

```ts
import { OpenFeature } from '@openfeature/server-sdk';
import { createConfidenceServerProvider } from '@spotify-confidence/openfeature-server-provider-local';

const provider = createConfidenceServerProvider({
  flagClientSecret: process.env.CONFIDENCE_FLAG_CLIENT_SECRET!,
  // initializeTimeout?: number
  // flushInterval?: number
  // fetch?: typeof fetch (Node <18 or custom transport)
});

// Wait for the provider to be ready (fetches initial resolver state)
await OpenFeature.setProviderAndWait(provider);

const client = OpenFeature.getClient();

// Create evaluation context with user attributes for targeting
const context = {
  targetingKey: 'user-123',
  country: 'US',
  plan: 'premium',
};

// Evaluate a boolean flag
const enabled = await client.getBooleanValue('test-flag.enabled', false, context);
console.log('Flag value:', enabled);

// Evaluate a nested value from an object flag using dot-path
// e.g. flag key "experiments" with payload { groupA: { ratio: 0.5 } }
const ratio = await client.getNumberValue('experiments.groupA.ratio', 0, context);

// On shutdown, flush any pending logs
await provider.onClose();
```

---

## Evaluation Context

The evaluation context contains information about the user/session being evaluated for targeting and A/B testing.

### TypeScript/JavaScript Examples

```typescript
// Simple attributes
const context = {
  targetingKey: 'user-123',
  country: 'US',
  plan: 'premium',
  age: 25,
};
```

---

## Error Handling

The provider uses a **default value fallback** pattern - when evaluation fails, it returns your specified default value instead of throwing an error.

**📖 See the [Integration Guide: Error Handling](../INTEGRATION_GUIDE.md#error-handling)** for:
- Common failure scenarios
- Error codes and meanings
- Production best practices
- Monitoring recommendations

### TypeScript/JavaScript Examples

```typescript
// The provider returns the default value on errors
const enabled = await client.getBooleanValue('my-flag.enabled', false, context);
// enabled will be 'false' if evaluation failed

// For detailed error information, use getBooleanDetails()
const details = await client.getBooleanDetails('my-flag.enabled', false, context);
if (details.errorCode) {
    console.error('Flag evaluation error:', details.errorMessage);
    console.log('Reason:', details.reason);
}
```

---

## Options

- `flagClientSecret` (string, required): The flag client secret used during evaluation and authentication.
- `initializeTimeout` (number, optional): Max ms to wait for initial state fetch. Defaults to 30_000.
- `flushInterval` (number, optional): Interval in ms for sending evaluation logs. Defaults to 10_000.
- `fetch` (optional): Custom `fetch` implementation. Required for Node < 18; for Node 18+ you can omit.

The provider periodically:
- Refreshes resolver state (default every 30s)
- Flushes flag evaluation logs to the backend

---

## Migration from online resolver
If you're currently using the ["online resolver"](https://github.com/spotify/confidence-sdk-js/tree/main/packages/openfeature-server-provider) and want to improve the resolve latency, [migration](MIGRATION.md) is easy!

---

## Sticky Assignments

The provider supports **Sticky Assignments** for consistent variant assignments across flag evaluations.

**📖 See the [Integration Guide: Sticky Assignments](../INTEGRATION_GUIDE.md#sticky-assignments)** for:
- How sticky assignments work
- Server-managed storage (zero configuration)
- Latency considerations
- Custom storage options (currently Java-only, coming soon to JavaScript)

---

## Logging (optional)

Logging uses the `debug` library if present; otherwise, all log calls are no-ops.

Namespaces:
- Core: `cnfd:*`
- Fetch/middleware: `cnfd:fetch:*` (e.g. retries, auth renewals, request summaries)

Log levels are hierarchical:
- `cnfd:debug` enables debug, info, warn, and error
- `cnfd:info` enables info, warn, and error
- `cnfd:warn` enables warn and error
- `cnfd:error` enables error only

Enable logs:

- Node:
```bash
DEBUG=cnfd:* node app.js
# or narrower
DEBUG=cnfd:info,cnfd:fetch:* node app.js
```

- Browser (in DevTools console):
```js
localStorage.debug = 'cnfd:*';
```

Install `debug` if you haven’t:

```bash
yarn add debug
```

---

## WebAssembly asset notes

- Node: the WASM (`confidence_resolver.wasm`) is resolved from the installed package automatically; no extra config needed.
- Browser: the ESM build resolves the WASM via `new URL('confidence_resolver.wasm', import.meta.url)` so modern bundlers (Vite/Rollup/Webpack 5 asset modules) will include it. If your bundler does not, configure it to treat the `.wasm` file as a static asset.

---

## Using in browsers

The package exports a browser ESM build that compiles the WASM via streaming and uses the global `fetch`. Integrate it with your OpenFeature SDK variant for the web similarly to Node, then register the provider before evaluation. Credentials must be available to the runtime (e.g. through your app’s configuration layer).

---

## Testing

- You can inject a custom `fetch` via the `fetch` option to stub network behavior in tests.
- The provider batches logs; call `await provider.onClose()` in tests to flush them deterministically.

---

## License

See the root `LICENSE`.

## Formatting

Code is formatted using prettier, you can format all files by running

```sh
yarn format
```
