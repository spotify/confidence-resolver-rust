# confidence-cloudflare-resolver Makefile
# Cloudflare Worker - deployment via deployer/Dockerfile

.PHONY: lint
lint:
	RUSTFLAGS='--cfg getrandom_backend="wasm_js"' \
		cargo clippy --lib --target wasm32-unknown-unknown --release

.PHONY: clean
clean:
	cargo clean

# Note: Build/deployment handled by deployer/Dockerfile
# See: deployer/README.md

