FROM rust:1.82

RUN rustup update stable && rustup target add wasm32-unknown-unknown

# Execute next commands in the directory /workspace
WORKDIR /workspace

RUN apt-get update && \
    apt install -y protobuf-compiler jq && \
    protoc --version && jq --version

# Install curl and other necessary tools
RUN apt-get update && apt-get install -y \
    curl \
    && rm -rf /var/lib/apt/lists/*

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

ENV PATH=/usr/local/bin:$PATH

# Optionally pass the commit SHA at build time
ARG COMMIT_SHA=""
ENV COMMIT_SHA=${COMMIT_SHA}

# Copy entire repository (build context is repo root) into a subdir
# to match script paths (it cds into confidence-resolver-rust)
COPY . .

# Ensure deploy script is executable
RUN chmod +x confidence-cloudflare-resolver/deployer/script.sh

# Default command runs the deployer script
CMD ["./confidence-cloudflare-resolver/deployer/script.sh"]

# Remove sample/runtime data from the copied repo
RUN rm -rf data/*