### Node.js example (node-host)

Run the Node host that calls into the Rust WASM guest to resolve a flag.

#### Prerequisites
- Rust and Cargo installed
- wasm32-unknown-unknown target: `rustup target add wasm32-unknown-unknown`
- Node.js 20+ (22+ recommended)
- Yarn (Corepack): `corepack enable`
- Protocol Buffers (protoc)

#### Quick start (recommended)
From the repo root:

```bash
make run-js-host
```

This builds the WASM guest, generates TS protos, and runs the example.

#### Manual steps
From the repo root:

```bash
# 1) Build the WASM guest
cargo build -p rust-guest --target wasm32-unknown-unknown --release

# 2) Install deps, generate TS protos, and run
cd wasm/node-host
yarn install --immutable
yarn proto:gen
yarn start
```

On startup, it prints a verification line like:
```
tutorial-feature verified: reason=RESOLVE_REASON_MATCH variant=... value=...
```
followed by timing measurements.


