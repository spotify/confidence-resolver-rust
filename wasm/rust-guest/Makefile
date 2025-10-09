# rust-guest (WASM) Makefile
# Deployable WASM module - no tests (logic tested in dependencies)

.PHONY: build
build:
	cargo build --target wasm32-unknown-unknown --profile wasm
	@echo "WASM size: $$(ls -lh ../../target/wasm32-unknown-unknown/wasm/rust_guest.wasm | awk '{print $$5}')"

.PHONY: lint
lint:
	cargo clippy --target wasm32-unknown-unknown --lib --release

.PHONY: clean
clean:
	cargo clean
