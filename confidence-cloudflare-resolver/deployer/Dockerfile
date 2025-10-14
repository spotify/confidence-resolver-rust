FROM rust:1.86 AS rust-base

RUN rustup toolchain install 1.86.0 && \
    rustup default 1.86.0 && \
    rustup target add wasm32-unknown-unknown

# Execute next commands in the directory /workspace
WORKDIR /workspace

RUN apt-get update && \
    apt install -y protobuf-compiler jq curl git && \
    protoc --version && jq --version && curl --version && git --version

# Install cargo-chef to leverage Docker layer caching of Rust deps
RUN cargo install cargo-chef --locked

# --- Planner stage: compute dependency recipe ---
FROM rust-base AS planner
WORKDIR /workspace
# Copy the full repo to compute the recipe. This stage is not cached across
# source changes, but downstream cook stage will be, based on the recipe only.
COPY . .

RUN if [ -d .git ]; then git describe --tags --exact-match 2>/dev/null > /workspace/.release_tag || true; fi
RUN cargo chef prepare --recipe-path recipe.json

# --- Cacher stage: build dependency layers only (cached unless Cargo.toml changes) ---
FROM rust-base AS cacher
WORKDIR /workspace
COPY --from=planner /workspace/recipe.json recipe.json
RUN RUSTFLAGS='--cfg getrandom_backend="wasm_js"' cargo chef cook --release --recipe-path recipe.json --target wasm32-unknown-unknown

# --- Final stage: add Node/Wrangler and runtime tooling, reuse cached deps ---
FROM rust-base AS final

ENV NVM_DIR=/root/.nvm
ENV NODE_VERSION=22.12.0

RUN curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.39.3/install.sh | bash \
    && . $NVM_DIR/nvm.sh \
    && nvm install $NODE_VERSION \
    && nvm use $NODE_VERSION \
    && nvm alias default $NODE_VERSION \
    && npm install -g npm@latest

# Add Node.js and npm to PATH
ENV PATH="$NVM_DIR/versions/node/v$NODE_VERSION/bin:$PATH"

# Confirm versions
RUN node -v && npm -v

# Install Wrangler CLI using npm
RUN npm install -g wrangler@latest

# Pre-install the Rust worker build tool so wrangler doesn't run cargo install at deploy-time
ARG WORKER_BUILD_VERSION="0.1.11"
ENV WORKER_BUILD_VERSION=${WORKER_BUILD_VERSION}
RUN cargo install worker-build --locked --version ${WORKER_BUILD_VERSION}

ENV PATH=/usr/local/bin:$PATH
ENV PATH=/usr/local/cargo/bin:$PATH

# Optionally pass the commit SHA at build time
ARG COMMIT_SHA=""
ENV COMMIT_SHA=${COMMIT_SHA}

# Copy the repository contents
COPY . .

# Reuse dependency cache produced by cargo-chef
COPY --from=cacher /workspace/target /workspace/target
# Bring over only cargo caches (not binaries) to preserve preinstalled tools
COPY --from=cacher /usr/local/cargo/registry /usr/local/cargo/registry

# Clean sample/runtime data from the copied repo to avoid leaking into image
RUN rm -rf data/*

# Ensure deploy script is executable
RUN chmod +x confidence-cloudflare-resolver/deployer/script.sh

# Default command runs the deployer script
CMD ["./confidence-cloudflare-resolver/deployer/script.sh"]