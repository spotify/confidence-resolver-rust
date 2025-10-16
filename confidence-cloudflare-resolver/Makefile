# confidence-cloudflare-resolver Makefile
# Cloudflare Worker - deployment via deployer/Dockerfile

.PHONY: build lint clean

build:
	RUSTFLAGS='--cfg getrandom_backend="wasm_js"' \
		cargo build --target wasm32-unknown-unknown --release
		
lint:
	RUSTFLAGS='--cfg getrandom_backend="wasm_js"' \
		cargo clippy --lib --target wasm32-unknown-unknown --release

clean:
	cargo clean

# Note: Build/deployment handled by deployer/Dockerfile
# See: deployer/README.md

