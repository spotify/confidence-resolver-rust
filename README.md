# Confidence Rust Flags Resolver

![Status: Experimental](https://img.shields.io/badge/status-experimental-orange)

The Confidence flag resolver implemented in Rust, local-resolve OpenFeature Providers and edge-compatible resolver runnables.

## Repository layout

- `confidence-resolver`: Core resolver crate
- `wasm` and `wasm-msg`: WASM resolver with communication contract towards the hosting environment 
- `data`: Sample local development data (e.g., resolver state)

The tools and SDKs published for direct usage:
- `confidence-cloudflare-resolver`: Confidence resolver service as a Cloudflare Worker 
- `openfeature-provider`: The OpenFeature providers for flag resolving

To deploy the Cloudflare resolver, follow [this README](./confidence-cloudflare-resolver/deployer/).

To integrate OpenFeature SDKs in your environment, you can refer to the READMEs for the desired language in the [openfeature-provider](./openfeature-provider) folder.


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
