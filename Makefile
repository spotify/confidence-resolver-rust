# Root Makefile
# Local development commands - delegates to component Makefiles

TARGET_WASM := target/wasm32-unknown-unknown/wasm/rust_guest.wasm

.PHONY: $(TARGET_WASM) test integration-test lint build all clean

$(TARGET_WASM):
	$(MAKE) -C wasm/rust-guest build

wasm/confidence_resolver.wasm: $(TARGET_WASM)
	mkdir -p wasm
	cp $(TARGET_WASM) $@

test:
	$(MAKE) -C confidence-resolver test
	$(MAKE) -C wasm-msg test
	$(MAKE) -C openfeature-provider/js test

integration-test:
	$(MAKE) -C wasm/node-host run
	$(MAKE) -C wasm/java-host run
	$(MAKE) -C wasm/go-host run
	$(MAKE) -C wasm/python-host run


lint:
	$(MAKE) -C confidence-resolver lint
	$(MAKE) -C wasm-msg lint
	$(MAKE) -C wasm/rust-guest lint
	$(MAKE) -C confidence-cloudflare-resolver lint
	cargo fmt --all --check

build: wasm/confidence_resolver.wasm
	$(MAKE) -C openfeature-provider/js build

all: lint test build
	@echo "✅ All checks passed!"

clean:
	cargo clean
	$(MAKE) -C wasm/node-host clean
	$(MAKE) -C wasm/java-host clean
	$(MAKE) -C wasm/go-host clean
	$(MAKE) -C wasm/python-host clean
	$(MAKE) -C openfeature-provider/js clean

.DEFAULT_GOAL := all
