# Confidence Rust Flags Resolver

![Status: Experimental](https://img.shields.io/badge/status-experimental-orange)

The Confidence Flag Resolver implemented in Rust, plus example hosts and a Cloudflare Worker build. This workspace compiles the core resolver to native and WebAssembly and demonstrates how to call it from Go, Node.js, Python, and Java.

## Repository layout

- `confidence-resolver`: Core resolver crate
- `confidence-cloudflare-resolver`: Cloudflare Worker-compatible WASM target
- `wasm-msg`: Minimal WASM messaging layer shared by hosts
- `wasm/python-host`: Python host example calling the resolver. Only intended to be an example and used for integration tests.
- `data/`: Sample local development data (e.g., resolver state)

## Prerequisites

**Option 1: Docker only**
- Docker - Everything runs in containers, no other tools needed

**Option 2: Local development**
- Rust toolchain (automatically installed via `rust-toolchain.toml`)
- For Python host example: Python 3

## Quick Start

```bash
# With Docker (reproducible, no setup needed)
docker build .                    # Build, test, lint everything
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

# Run Python host example
make run-python
```

## Running the Python host example

There is a Python host implementation in the `wasm/python-host` folder.
It is used for integration tests, but you can manually run it:

```bash
make run-python-host
```

## Cloudflare Worker build

Build the Cloudflare-compatible resolver (WASM):

```bash
make cloudflare
```

You can then integrate with Wrangler using `confidence-cloudflare-resolver/wrangler.toml`.

## Benchmarks (WIP)

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

## Supply Chain Security

This repository implements **binary provenance** for the WASM binary embedded in provider packages. All releases include:
- Cryptographically attested WASM binaries (via GitHub attestations)
- SHA-256 checksums published to GitHub releases
- Deterministic builds using pinned toolchains and Docker

See [SECURITY.md](SECURITY.md) for verification instructions and detailed security policies.

## License

See `LICENSE` for details.
