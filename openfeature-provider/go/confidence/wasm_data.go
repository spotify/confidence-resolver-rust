package confidence

import _ "embed"

// defaultWasmBytes contains the embedded WASM resolver module.
// This file is automatically populated during the build process from wasm/confidence_resolver.wasm.
// The WASM file is built from the Rust source in wasm/rust-guest/ and must be kept in sync.
//
// CI validates that this embedded file matches the built WASM to prevent version drift.

//go:embed wasm/confidence_resolver.wasm
var defaultWasmBytes []byte
