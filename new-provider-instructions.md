# Building a new Provider
These are instructions for constructing a new OpenFeature Provider for Confidence with local resolve capabilities in a some programming language. Meant to be used as context to AI agents when they provide the first attempts. The new provider may be referenced to as "host" and the rust/wasm as "guest".

# Prerequisites

1. The language we are building for need to already have an OpenFeature SDK that works with server side environments. The list of supported languages can be found [here](https://openfeature.dev/ecosystem?instant_search%5BrefinementList%5D%5Btype%5D%5B0%5D=SDK&instant_search%5BrefinementList%5D%5Bcategory%5D%5B0%5D=Server) but it is also worth checking [here](https://github.com/orgs/open-feature/repositories?q=sdk), the type of SDK we are looking for is considered a "dynamic context" SDK.
2. Since the resolver is written in Rust and exposed via WASM, the language needs to have a good WASM runtime library to interop with that. Search github for good alternatives that have many stars. For example we use [tetratelabs/wazero](https://github.com/wazero/wazero) on Go and [dylibso/chicory](https://github.com/dylibso/chicory) for Java.
3. Understand the OpenFeature [spec](https://openfeature.dev/specification/), specifically the parts about [Providers](https://openfeature.dev/specification/sections/providers), [Evaluation API](https://openfeature.dev/specification/sections/flag-evaluation) and [Evaluation Context](https://openfeature.dev/specification/sections/evaluation-context).
4. Decide wether to use grpc or http when doing flag logging.

# Architecture
* The proto files needed are located at `confidence-resolver/protos`. Depending on the language they may need to be copied into or
* The main class should be an OpenFeature FeatureProvider, suggestible named ConfidenceProviderLocal or similar. It should inherit/implement the "Feature Provider Interface".
* The main initialization work (heavy lifting) should happen in the ["Initialization" method](https://openfeature.dev/specification/sections/providers#24-initialization).
* The classes used by the Provider should all have interfaces so they can be mocked in tests.
* The main Provider class should have a helper that fetch the state file for the Confidence account from a Spotify CDN. An AccountStateProvider. Provides the state as a byte-array.
* The main Provider class should have a helper (LocalResolver) that handle the resolve functionality. 

## HTTP or gRPC
Depending on the host, exposure logging may be done via http or gRPC. This needs to be decided on a per case basis. 

The gRPC domain is `edge-grpc.spotify.com` while HTTP urls can be found in `openfeature-provider/js/src/ConfidenceServerProviderLocal.ts`. The are hosted at `https://resolver.confidence.dev/`.


# Constructor dependencies
To start with the Provider should be constructed simply with the `clientSecret` string. It is highly suggestible to provider constructors to be used in tests so we can mock out dependencies and provide good unit and integration testing coverage.

# State fetching
* The file is fetched by building a URL using the `clientSecret`: `"https://confidence-resolver-state-cdn.spotifycdn.com/<clientSecret>"` . Simply `GET` that url.
* Use ETag for performant http caching.
* Keep an in memory copy of the state so it can be provided to the FeatureProvider if network is down.
* The fetched byte[] will conform to a proto type `ClientResolverState` (contain `string account = 1; ResolverState state = 2;`).

# Flag resolving
The LocalResolver class should load the wasm file into the runtime and have methods to 
```ts 
resolve(request: ResolveWithStickyRequest): ResolveWithStickyResponse;
setResolverState(request: SetResolverStateRequest): void;
flushLogs(): Uint8Array;
flushAssigned(): Uint8Array;
```
It will be responsible for all things wasm runtime integration. Exposing WASM exports in a devx friendly manner, allocating memory, send messages to the wasm and consume the responses. A reference implementation can be found at `openfeature-provider/js/src/WasmResolver.ts`.

## Sticky Resolves and Materialization needed.

! Be advised; subject to change !

The Provider should allow for the user to pass a `MaterializationRepository` to the provider. 

In some cases when resolving, the resolver needs more data to evaluate the rule. It will then respond with a `resolve_result` of type `MissingMaterializations`. The provider should then do one of two things:
1. Find the missing materializations using the `MaterializationRepository` and try to resolve again.
2. Fall back to a remote resolve - calling Confidence servers to resolve the flag value. 

In some providers, the concept of `StickyResolveStrategy` exist, this is an interface passed to the constructor where it can be decided wether to do a remote resolve fallback or to pass `MaterializationRepository`.

### Handle MaterializationUpdate

In the case where the provider has a working `MaterializationRepository` it needs to handle any returned `MaterializationUpdate` from the resolver (part of `ResolveWithStickyResponse`).

# Exposure/Flag logging
The flag logging happens against the `WriteFlagLogs` API (`rpc WriteFlagLogs` in `confidence-resolver/protos/confidence/flags/resolver/v1/internal_api.proto` or `https://resolver.confidence.dev/v1/flagLogs:write`) which accepts a `WriteFlagLogsRequest`. The byte array returned from the WASM flushLogs methods conforms to this and can be passed opaquely to this endpoint from WASM.

# Build details in the repo
Always add a make file with all the tasks that may need to be done. Also make sure that there are equivalent Docker targets in the main `Dockerfile` so that the new code builds and tests in CI.

# Testing
TBW

# Telemetry
In the resolve message (`ResolveFlagsRequest`) there is a field for `Sdk`. Fill this with `version` and `sdkId`. There should be an additional enum added for the new Provider which needs to happen on the backend so it is fine to pass a `custom_id` until then.

# Releasing
We use release please, even though the project likely isn't a rust project, configure it as such in `release-please-config.json` and add it to the `.release-please-manifest.json`. 

To pickup dependency changes to the wasm and rust resolver. There's likely a new job in the `release-please.yml` that will need to be added.