# wasm-msg Makefile
# Support library - linted, tested when tests exist

.PHONY: test
test:
	@echo "ℹ️  No tests yet for wasm-msg"
	# When tests exist: cargo test --release --lib

.PHONY: lint
lint:
	cargo fmt --check
	cargo clippy --lib --release

.PHONY: clean
clean:
	cargo clean

