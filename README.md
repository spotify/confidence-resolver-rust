# Confidence Rust Flags Resolver

![Status: Experimental](https://img.shields.io/badge/status-experimental-orange)

The Confidence Flag Resolver implemented in Rust, plus example hosts and a Cloudflare Worker build. This workspace compiles the core resolver to native and WebAssembly and demonstrates how to call it from Go, Node.js, Python, and Java.

## Repository layout

- `confidence-resolver`: Core resolver crate
- `confidence-cloudflare-resolver`: Cloudflare Worker-compatible WASM target
- `wasm-msg`: Minimal WASM messaging layer shared by hosts
- `wasm/*-host`: Small host apps (Go, Node.js, Python, Java) calling the resolver. These are only intended to be examples, as well as being used for integration tests.
- `data/`: Sample local development data (e.g., resolver state)

## Prerequisites

**Option 1: Docker only**
- Docker - Everything runs in containers, no other tools needed

**Option 2: Local development**
- Rust toolchain (automatically installed via `rust-toolchain.toml`)
- For host examples: Go, Node.js + Yarn, Python 3, Java + Maven

## Quick Start

```bash
# With Docker (reproducible, no setup needed)
docker build .                    # Build, test, lint everything
make                              # Same, using Makefile

# With Docker + e2e tests (requires Confidence credentials)
docker build \
  --build-arg JS_E2E_CONFIDENCE_API_CLIENT_ID=<your-client-id> \
  --build-arg JS_E2E_CONFIDENCE_API_CLIENT_SECRET=<your-secret> \
  .

# With local tools (fast iteration)
make test                         # Run tests
make lint                         # Run linting
make build                        # Build WASM

# Run host examples
make run-node
make run-java
make run-go
make run-python
```

## Running the example hosts

There are host implementations for different languages in the `wasm` folder.
They are used for integration tests, but if you want you manually run them:

```bash
make run-go-host
make run-js-host
make run-python-host
make run-java-host
```

## Cloudflare Worker build

Build the Cloudflare-compatible resolver (WASM):

```bash
make cloudflare
```

You can then integrate with Wrangler using `confidence-cloudflare-resolver/wrangler.toml`.

## License

See `LICENSE` for details.
