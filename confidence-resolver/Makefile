# confidence-resolver Makefile
# Core library - tested natively, never built standalone

.PHONY: test
test:
	cargo test --release --lib

.PHONY: lint
lint:
	cargo fmt --check
	cargo clippy --lib --release -- -D warnings

.PHONY: clean
clean:
	cargo clean
