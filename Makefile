# Root Makefile
# Local development commands - delegates to component Makefiles

.PHONY: test
test:
	$(MAKE) -C confidence-resolver test
	$(MAKE) -C wasm-msg test
	$(MAKE) -C openfeature-provider/js test

.PHONY: lint
lint:
	$(MAKE) -C confidence-resolver lint
	$(MAKE) -C wasm-msg lint
	$(MAKE) -C wasm/rust-guest lint
	$(MAKE) -C confidence-cloudflare-resolver lint
	cargo fmt --all --check
	# Note: openfeature-provider/js linted via its test suite

.PHONY: build
build:
	$(MAKE) -C wasm/rust-guest build
	$(MAKE) -C openfeature-provider/js build

.PHONY: run-node
run-node:
	$(MAKE) -C wasm/node-host run

.PHONY: all
all: lint test build
	@echo "✅ All checks passed!"

.PHONY: run-java
run-java:
	$(MAKE) -C wasm/java-host run

.PHONY: run-go
run-go:
	$(MAKE) -C wasm/go-host run

.PHONY: run-python
run-python:
	$(MAKE) -C wasm/python-host run

.PHONY: clean
clean:
	$(MAKE) -C confidence-resolver clean
	$(MAKE) -C wasm-msg clean
	$(MAKE) -C wasm/rust-guest clean
	$(MAKE) -C confidence-cloudflare-resolver clean
	$(MAKE) -C wasm/node-host clean
	$(MAKE) -C wasm/java-host clean
	$(MAKE) -C wasm/go-host clean
	$(MAKE) -C wasm/python-host clean
	$(MAKE) -C openfeature-provider/js clean

.PHONY: help
help:
	@echo "Confidence Resolver Build System"
	@echo ""
  @echo "Local commands:"
	@echo "  make           - Run all (lint + test + build)"
	@echo "  make test      - Test libraries"
	@echo "  make lint      - Lint all crates"
	@echo "  make build     - Build WASM deployables"
	@echo "  make run-node   - Run Node.js example"
	@echo "  make run-java   - Run Java example"
	@echo "  make run-go     - Run Go example"
	@echo "  make run-python - Run Python example"
	@echo "  make clean      - Clean all"
	@echo ""
	@echo "Docker commands:"
	@echo "  docker build .                                      - Full CI (parallel)"
	@echo "  docker build --target=confidence-resolver.test ."
	@echo "  docker build --target=wasm-rust-guest.build ."
	@echo ""
	@echo "Per-component:"
	@echo "  make -C confidence-resolver test"
	@echo "  make -C wasm/rust-guest build"
	@echo "  make -C wasm/node-host run"
	@echo "  make -C wasm/java-host run"
	@echo "  make -C wasm/go-host run"
	@echo "  make -C wasm/python-host run"
	@echo ""
	@echo "Docker host examples:"
	@echo "  docker run --rm \$$(docker build --target=node-host-run -q .)"
	@echo "  docker run --rm \$$(docker build --target=java-host-run -q .)"
	@echo "  docker run --rm \$$(docker build --target=go-host-run -q .)"
	@echo "  docker run --rm \$$(docker build --target=python-host-run -q .)"
	@echo ""
	@echo "Cloudflare deployment:"
	@echo "  See confidence-cloudflare-resolver/deployer/README.md"

.DEFAULT_GOAL := all
