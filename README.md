# Confidence Rust Flags Resolver

The Confidence Flag Resolver implemented in Rust, plus example hosts and a Cloudflare Worker build. This workspace compiles the core resolver to native and WebAssembly and demonstrates how to call it from Go, Node.js, Python, and Java.

## Repository layout

- `confidence-resolver`: Core resolver crate
- `confidence-cloudflare-resolver`: Cloudflare Worker-compatible WASM target
- `wasm-msg`: Minimal WASM messaging layer shared by hosts
- `wasm/*-host`: Small host apps (Go, Node.js, Python, Java) calling the resolver. These are only intended to be examples, as well as being used for integration tests.
- `data/`: Sample local development data (e.g., resolver state)

## Prerequisites

- Rust (stable) and the `wasm32-unknown-unknown` target
- For examples: Go, Node.js + Yarn, Python 3, Java + Maven
- Optional: Cloudflare Wrangler if you plan to deploy the Worker

Install the WASM target:

```bash
rustup target add wasm32-unknown-unknown
```

## Common tasks

- Build all targets:
```bash
make build
```

- Test core resolver:
```bash
make test
```

- Lint all crates:
```bash
make lint
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
