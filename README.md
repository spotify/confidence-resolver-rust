# Confidence Rust Flags Resolver

![Status: Experimental](https://img.shields.io/badge/status-experimental-orange)

The Confidence flag resolver implemented in Rust, local-resolve OpenFeature Providers and edge-compatible resolver runnables.

## SDKs and Tools

**OpenFeature Providers** — Local flag resolution for your application:
- [Go](./openfeature-provider/go/README.md)
- [Java](./openfeature-provider/java/README.md)
- [JavaScript/TypeScript](./openfeature-provider/js/README.md)
- [Ruby](./openfeature-provider/ruby/README.md)

See the [Integration Guide](./openfeature-provider/INTEGRATION_GUIDE.md) for setup instructions.

**Edge Resolver** — Deploy the Confidence resolver at the edge:
- [Cloudflare Worker](./confidence-cloudflare-resolver/deployer/README.md)

## Internal Crates

- `confidence-resolver`: Core resolver logic
- `wasm` and `wasm-msg`: WASM resolver with communication contract towards the hosting environment 
- `data`: Sample local development data (e.g., resolver state)


## Development - Quick Start

```bash
# With Docker (reproducible, no setup needed)
docker build .                    # Build, test, lint everything
```
Without docker, the building is managed via Makefile:
```
make                              # Same, using Makefile

# E2E tests require Confidence credentials passed as Docker secret
# Create openfeature-provider/js/.env.test with your credentials, then:
docker build \
  --secret id=js_e2e_test_env,src=openfeature-provider/js/.env.test \
  .

# With local tools (fast iteration)
make test                         # Run tests
make lint                         # Run linting
make build                        # Build WASM

# Build the Cloudflare-compatible resolver (WASM):
make cloudflare
```

## Benchmarks

Small local benchmarks exist for Go and Node.js to validate end-to-end wiring. They are a work-in-progress and do not produce meaningful or representative performance numbers yet.

Run with Docker (streams all logs, cleans up containers afterward):

```bash
# Go benchmark
make go-bench

# Node.js benchmark
make js-bench
```

Notes:
- Each target starts a dedicated mock server container and a one-shot bench container, then tears everything down.
- Use `docker compose up ... go-bench` or `... js-bench` to run them individually without Make.

## License

See `LICENSE` for details.
