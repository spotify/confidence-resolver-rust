# wasm-msg Makefile
# Support library - linted, tested when tests exist

.PHONY: test lint clean

test:
	@echo "ℹ️  No tests yet for wasm-msg"
	# When tests exist: cargo test --release --lib

lint:
	cargo fmt --check
	cargo clippy --lib --release -- -D warnings

clean:
	cargo clean

