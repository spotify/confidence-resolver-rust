# Build Architecture

This document describes the Docker multi-stage build architecture used in this repository.

## Overview

The Dockerfile uses a multi-stage build approach to:
- Compile Rust code for both native and WebAssembly targets
- Build and test OpenFeature providers (JavaScript and Java)
- Package artifacts for distribution

## Build Stage Diagram

The diagram below shows all build stages and their dependencies:

```mermaid
flowchart TD
    %% Base images
    alpine[alpine:3.22]
    node[node:20-alpine]
    java[eclipse-temurin:17-jdk]

    %% Rust toolchain and dependency stages
    alpine --> rust-base[rust-base<br/>Rust toolchain + protoc]
    rust-base --> rust-deps[rust-deps<br/>Cached Rust dependencies]

    %% Native test/lint base
    rust-base --> rust-test-base[rust-test-base<br/>Base for native tests/lints]
    rust-deps -.->|copies cargo cache| rust-test-base

    %% WASM build base
    rust-base --> wasm-deps[wasm-deps<br/>WASM build environment]
    rust-deps -.->|copies cargo cache| wasm-deps

    %% Native Rust tests
    rust-test-base --> confidence-resolver.test[confidence-resolver.test<br/>✓ Core resolver tests]
    rust-test-base --> wasm-msg.test[wasm-msg.test<br/>✓ WASM msg tests]

    %% Native Rust lints
    rust-test-base --> confidence-resolver.lint[confidence-resolver.lint<br/>⚡ Core resolver lint]
    rust-test-base --> wasm-msg.lint[wasm-msg.lint<br/>⚡ WASM msg lint]

    %% WASM build and lint
    wasm-deps --> wasm-rust-guest.build[wasm-rust-guest.build<br/>🔨 Build WASM resolver]
    wasm-deps --> wasm-rust-guest.lint[wasm-rust-guest.lint<br/>⚡ WASM guest lint]
    wasm-deps --> confidence-cloudflare-resolver.lint[confidence-cloudflare-resolver.lint<br/>⚡ Cloudflare lint]

    %% WASM artifact
    wasm-rust-guest.build --> wasm-rust-guest.artifact[wasm-rust-guest.artifact<br/>📦 confidence_resolver.wasm]

    %% OpenFeature JS provider
    node --> openfeature-provider-js-base[openfeature-provider-js-base<br/>Node deps + proto gen]
    wasm-rust-guest.artifact -.->|copies WASM| openfeature-provider-js-base
    openfeature-provider-js-base --> openfeature-provider-js.test[openfeature-provider-js.test<br/>✓ Provider tests]
    openfeature-provider-js.test --> openfeature-provider-js.test_e2e[openfeature-provider-js.test_e2e<br/>✓ E2E tests]
    openfeature-provider-js-base --> openfeature-provider-js.build[openfeature-provider-js.build<br/>🔨 TypeScript build]
    openfeature-provider-js.build --> openfeature-provider-js.pack[openfeature-provider-js.pack<br/>📦 yarn pack]
    openfeature-provider-js.pack --> openfeature-provider-js.artifact[openfeature-provider-js.artifact<br/>📦 package.tgz]

    %% OpenFeature Java provider
    java --> openfeature-provider-java-base[openfeature-provider-java-base<br/>Maven + proto gen]
    wasm-rust-guest.artifact -.->|copies WASM| openfeature-provider-java-base
    openfeature-provider-java-base --> openfeature-provider-java.test[openfeature-provider-java.test<br/>✓ Java provider tests]
    openfeature-provider-java-base --> openfeature-provider-java.build[openfeature-provider-java.build<br/>🔨 Maven build]
    openfeature-provider-java.build --> openfeature-provider-java.publish[openfeature-provider-java.publish<br/>🚀 Maven Central]

    %% All stage aggregates everything
    wasm-rust-guest.artifact --> all[all<br/>✅ Complete build]
    confidence-resolver.test --> all
    wasm-msg.test --> all
    openfeature-provider-js.test --> all
    openfeature-provider-js.test_e2e --> all
    openfeature-provider-java.test --> all
    confidence-resolver.lint --> all
    wasm-msg.lint --> all
    wasm-rust-guest.lint --> all
    confidence-cloudflare-resolver.lint --> all
    openfeature-provider-js.build --> all
    openfeature-provider-java.build --> all

    %% Styling
    classDef baseImage fill:#e1f5ff,stroke:#0066cc
    classDef buildStage fill:#fff4e1,stroke:#ff8c00
    classDef testStage fill:#e8f5e9,stroke:#2e7d32
    classDef lintStage fill:#fff3e0,stroke:#f57c00
    classDef artifact fill:#f3e5f5,stroke:#7b1fa2
    classDef publish fill:#ffebee,stroke:#c62828
    classDef final fill:#c8e6c9,stroke:#388e3c

    class alpine,node,java baseImage
    class rust-base,rust-deps,rust-test-base,wasm-deps,openfeature-provider-js-base,openfeature-provider-java-base baseImage
    class wasm-rust-guest.build,openfeature-provider-js.build,openfeature-provider-java.build buildStage
    class confidence-resolver.test,wasm-msg.test,openfeature-provider-js.test,openfeature-provider-js.test_e2e,openfeature-provider-java.test testStage
    class confidence-resolver.lint,wasm-msg.lint,wasm-rust-guest.lint,confidence-cloudflare-resolver.lint lintStage
    class wasm-rust-guest.artifact,openfeature-provider-js.pack,openfeature-provider-js.artifact artifact
    class openfeature-provider-java.publish publish
    class all final
```

**Legend:**
- 🔨 Build stages compile code
- ✓ Test stages run unit/integration tests
- ⚡ Lint stages run code quality checks
- 📦 Artifact stages extract build outputs
- 🚀 Publish stages deploy to registries
- ✅ Final `all` stage aggregates everything

## Key Features

### Dependency Caching
Rust dependencies are compiled once in the `rust-deps` stage and reused across all subsequent builds. This significantly speeds up incremental builds.

### Parallel Execution
Test and lint stages are independent and can run concurrently, reducing total build time.

### WASM Artifact Sharing
The core `confidence_resolver.wasm` is built once in `wasm-rust-guest.build` and shared across:
- OpenFeature JavaScript provider
- OpenFeature Java provider

### Targeted Builds
You can build specific components using Docker's `--target` flag:

```bash
# Build only the WASM artifact
docker build --target=wasm-rust-guest.artifact .

# Build and extract the npm package
docker build --target=openfeature-provider-js.artifact .

# Run only JavaScript provider tests
docker build --target=openfeature-provider-js.test .

# Build everything (default)
docker build .
```

## Stage Descriptions

### Base Stages

- **rust-base** (FROM alpine:3.22): Installs Rust toolchain via rustup, protoc, and build dependencies
- **rust-deps** (FROM rust-base): Compiles all Rust workspace dependencies (cached layer for faster rebuilds)
- **rust-test-base** (FROM rust-base): Copies dependency cache and source code for native testing/linting
- **wasm-deps** (FROM rust-base): Copies dependency cache and source code for WASM builds
- **openfeature-provider-js-base** (FROM node:20-alpine): Node.js environment with Yarn, dependencies, and proto generation
- **openfeature-provider-java-base** (FROM eclipse-temurin:17-jdk): Java environment with Maven and proto files

### Test Stages

- **confidence-resolver.test** (FROM rust-test-base): Unit tests for core resolver
- **wasm-msg.test** (FROM rust-test-base): Tests for WASM messaging layer
- **openfeature-provider-js.test** (FROM openfeature-provider-js-base): Unit tests for JavaScript provider
- **openfeature-provider-js.test_e2e** (FROM openfeature-provider-js.test): End-to-end tests (requires credentials via Docker secret)
- **openfeature-provider-java.test** (FROM openfeature-provider-java-base): Tests for Java provider

### Lint Stages

- **confidence-resolver.lint** (FROM rust-test-base): Clippy checks for core resolver
- **wasm-msg.lint** (FROM rust-test-base): Clippy checks for WASM messaging
- **wasm-rust-guest.lint** (FROM wasm-deps): Clippy checks for WASM guest
- **confidence-cloudflare-resolver.lint** (FROM wasm-deps): Clippy checks for Cloudflare resolver

### Build Stages

- **wasm-rust-guest.build** (FROM wasm-deps): Compiles Rust resolver to WebAssembly (wasm32-unknown-unknown target)
- **openfeature-provider-js.build** (FROM openfeature-provider-js-base): Compiles TypeScript to JavaScript
- **openfeature-provider-java.build** (FROM openfeature-provider-java-base): Builds Java provider with Maven

### Artifact Stages

- **wasm-rust-guest.artifact** (FROM scratch): Extracts `confidence_resolver.wasm` (rust_guest.wasm → confidence_resolver.wasm)
- **openfeature-provider-js.pack** (FROM openfeature-provider-js.build): Creates npm package tarball via `yarn pack`
- **openfeature-provider-js.artifact** (FROM scratch): Extracts package.tgz for distribution

### Publish Stages

- **openfeature-provider-java.publish** (FROM openfeature-provider-java.build): Publishes Java provider to Maven Central (requires GPG and Maven secrets)

### Aggregation Stage

- **all** (FROM scratch): Default stage that ensures all tests, lints, and builds complete successfully by copying marker files

## CI/CD Integration

The build stages are used in GitHub Actions workflows:

- **release-please.yml**: Publishes packages when releases are created
  - Uses `openfeature-provider-js.artifact` to extract npm package
  - Uses `openfeature-provider-java.publish` to deploy to Maven Central

## Docker Build Cache

The repository uses Docker layer caching to speed up builds in CI:

```yaml
cache-from: type=registry,ref=ghcr.io/${{ github.repository }}/cache:main
```

This allows GitHub Actions to reuse layers from previous builds.

## Dependency Flow

The `rust-deps` stage builds dummy source files to compile all workspace dependencies, creating a cached layer. This cache is then copied into:
- `rust-test-base` (for native tests and lints)
- `wasm-deps` (for WASM builds)

This approach ensures dependencies are only compiled once, even when building multiple targets.
