# Makefile for building the multi-target Rust workspace.

# Use bash as the shell, which is more predictable than sh.
SHELL=/bin/bash

# Build/test targets (no file prereqs; Cargo rebuilds as needed)
.PHONY: resolver resolver-test resolver-lint rust-guest rust-guest-lint cloudflare cloudflare-lint run-go-host run-js-host run-python-host run-java-host \
        go-host js-host python-host java-host test lint build

# Build/test
resolver:
	@echo "Building confidence_resolver..."
	cargo build -p confidence_resolver

resolver-test:
	@echo "Testing confidence_resolver..."
	cargo test -p confidence_resolver --lib

resolver-lint:
	@echo "Linting confidence_resolver..."
	cargo clippy -p confidence_resolver --lib --release

rust-guest:
	@echo "Building rust-guest (wasm32-unknown-unknown)..."
	cargo build -p rust-guest --target wasm32-unknown-unknown --profile wasm
	@echo "Final WASM size: $$(/bin/ls -lh target/wasm32-unknown-unknown/wasm/rust_guest.wasm | awk '{print $$5}')"

rust-guest-lint:
	@echo "Linting rust-guest (wasm32-unknown-unknown)..."
	cargo clippy -p rust-guest --target wasm32-unknown-unknown --lib --release

cloudflare:
	@echo "Building confidence-cloudflare-resolver (wasm32-unknown-unknown)..."
	RUSTFLAGS='--cfg getrandom_backend="wasm_js"' cargo build -p confidence-cloudflare-resolver --target wasm32-unknown-unknown --release

cloudflare-lint:
	@echo "Linting confidence-cloudflare-resolver (wasm32-unknown-unknown)..."
	RUSTFLAGS='--cfg getrandom_backend="wasm_js"' cargo clippy -p confidence-cloudflare-resolver --lib --target wasm32-unknown-unknown --release

# Produce a stable artifact location for CI hosts
wasm/confidence_resolver.wasm: | rust-guest
	@echo "Copying rust_guest.wasm to wasm/confidence_resolver.wasm..."
	cp target/wasm32-unknown-unknown/wasm/rust_guest.wasm wasm/confidence_resolver.wasm


# Run examples (depend on stable wasm artifact)
run-go-host: wasm/confidence_resolver.wasm
	cd wasm/go-host && bash generate_proto.sh && go run .

run-node-host: wasm/confidence_resolver.wasm
	cd wasm/node-host && yarn install --frozen-lockfile && yarn proto:gen && yarn start

run-python-host: wasm/confidence_resolver.wasm
	cd wasm/python-host \
		&& python3 -m venv .venv \
		&& .venv/bin/python -m pip install --upgrade pip \
		&& .venv/bin/python -m pip install --require-virtualenv wasmtime protobuf \
		&& .venv/bin/python generate_proto.py --out .venv/proto \
		&& PYTHONPATH=$$(pwd)/.venv:$$(pwd)/.venv/proto:$$PYTHONPATH .venv/bin/python main.py

run-java-host: wasm/confidence_resolver.wasm
	cd wasm/java-host && mvn -q package exec:java

# Aggregate test runs (only resolver has tests today)
test: resolver-test

lint: resolver-lint cloudflare-lint rust-guest-lint
	cargo fmt --all --check

build: resolver cloudflare rust-guest