### Go example (go-host)

Run the Go host that calls into the Rust WASM guest to resolve a flag.

#### Prerequisites
- Rust and Cargo installed
- wasm32-unknown-unknown target: `rustup target add wasm32-unknown-unknown`
- Go 1.22+ (1.25+ recommended on recent macOS)
- Protocol Buffers (protoc)
- protoc-gen-go: `go install google.golang.org/protobuf/cmd/protoc-gen-go@latest`

#### Quick start (recommended)
From the repo root:

```bash
make run-go-host
```

This builds the WASM guest, generates Go protos, and runs the example.

#### Manual steps
From the repo root:

```bash
# 1) Build the WASM guest
cargo build -p rust-guest --target wasm32-unknown-unknown --release

# 2) Generate Go protobufs and run
cd wasm/go-host
bash generate_proto.sh
go run .
```

The program prints timing measurements. It also verifies `flags/tutorial-feature` resolves with reason RESOLVE_REASON_MATCH and a non-empty variant for the `tutorial_visitor` key.

#### macOS note
On macOS 15+, use Go ≥ 1.23 to avoid the dyld LC_UUID issue.


