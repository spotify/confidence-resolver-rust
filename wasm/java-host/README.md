### Java example (java-host)

Run the Java host that calls into the Rust WASM guest to resolve a flag.

#### Prerequisites
- Rust and Cargo installed
- wasm32-unknown-unknown target: `rustup target add wasm32-unknown-unknown`
- Java 21+
- Maven 3.9+

#### Quick start (recommended)
From the repo root:

```bash
make run-java-host
```

This builds the WASM guest and runs the Java example.

#### Manual steps
From the repo root:

```bash
# 1) Build the WASM guest
cargo build -p rust-guest --target wasm32-unknown-unknown --release

# 2) Run the Java host
cd wasm/java-host
mvn -q package exec:java
```

The program prints a verification line like:
```
tutorial-feature verified: reason=RESOLVE_REASON_MATCH variant=... value=...
```
followed by timing measurements.


