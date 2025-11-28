# Security Policy

We're big believers in protecting your privacy and security. As a company, we not only have a vested interest, but also a deep desire to see the Internet remain as safe as possible for us all.

So, needless to say, we take security issues very seriously.

In our opinion, the practice of 'responsible disclosure' is the best way to safeguard the Internet. It allows individuals to notify companies like Spotify of any security threats before going public with the information. This gives us a fighting chance to resolve the problem before the criminally-minded become aware of it.

Responsible disclosure is the industry best practice, and we recommend it as a procedure to anyone researching security vulnerabilities.

## Reporting a Vulnerability

If you have discovered a vulnerability in this open source project or another serious security issue,
please submit it to the Spotify bounty program hosted by HackerOne.

https://hackerone.com/spotify

---

## Binary Provenance and Supply Chain Security

This repository implements binary provenance to ensure transparency and trust in our WebAssembly (WASM) resolver that is embedded in our OpenFeature provider packages.

### The Challenge

Our Java, JavaScript, Go, and Ruby provider packages include a pre-compiled WASM binary (`confidence_resolver.wasm`). While this provides convenience and performance, it creates a "black box" problem: how can users verify that the binary corresponds to the source code in this repository?

### Our Solution: Provenance Attestation

We use **reproducible builds** and **GitHub attestations** to create a verifiable chain of custody from source code to distributed binaries.

## Verifying the WASM Binary

### 1. Download the Published WASM

Each provider release includes the canonical WASM binary and its SHA-256 checksum as release assets:

```bash
# Example: For Java provider version 0.8.0
gh release download openfeature-provider/java-v0.8.0 \
  -p "confidence_resolver.wasm*" \
  --repo spotify/confidence-resolver
```

This downloads:
- `confidence_resolver.wasm` - The canonical binary
- `confidence_resolver.wasm.sha256` - SHA-256 checksum

### 2. Verify the Attestation

GitHub attestations provide cryptographic proof that the WASM was built from specific source code by our CI pipeline:

```bash
# Verify the attestation (requires gh CLI v2.49.0+)
gh attestation verify confidence_resolver.wasm \
  --repo spotify/confidence-resolver
```

This verifies:
- ✅ The binary was built by GitHub Actions (not locally or maliciously)
- ✅ The build came from a specific commit in this repository
- ✅ The attestation is signed and recorded in Sigstore's transparency log

### 3. Verify the Embedded WASM in Provider Packages

#### Java Provider

Extract and verify the WASM from the JAR:

```bash
# Download the JAR from Maven Central
# Extract the WASM
unzip -p openfeature-provider-local-0.8.0.jar \
  wasm/confidence_resolver.wasm > extracted.wasm

# Compare with the published WASM
sha256sum extracted.wasm confidence_resolver.wasm
```

The checksums should match, proving the JAR contains the attested WASM.

#### JavaScript Provider

```bash
# Extract from npm package
npm pack @spotify-confidence/openfeature-provider-local
tar -xzf spotify-confidence-openfeature-provider-local-*.tgz
sha256sum package/dist/confidence_resolver.wasm confidence_resolver.wasm
```

#### Go Provider

The Go provider embeds WASM at compile time. Verify the embedded binary:

```bash
# In the repository
sha256sum openfeature-provider/go/confidence/wasm/confidence_resolver.wasm \
  confidence_resolver.wasm
```

## Verifying Provider Package Attestations

We also attest the provider packages themselves:

### Java Provider (JAR)

```bash
# Verify the JAR attestation
gh attestation verify openfeature-provider-local-0.8.0.jar \
  --repo spotify/confidence-resolver
```

### Ruby Provider (Gem)

```bash
# Verify the gem attestation
gh attestation verify spotify_confidence_openfeature_provider-0.8.0.gem \
  --repo spotify/confidence-resolver
```

### JavaScript Provider

JavaScript packages are published to npm with built-in provenance:

```bash
# View provenance statement
npm view @spotify-confidence/openfeature-provider-local --json | jq .dist.attestations
```

## Deterministic Builds

Our builds are designed to be deterministic in CI:

1. **Pinned Rust toolchain**: `rust-toolchain.toml` locks Rust to version 1.90.0
2. **Docker-based builds**: Isolated, consistent build environment
3. **Deterministic compiler flags**: LTO, single codegen unit, stripped symbols

### Building Locally

To build the WASM locally:

```bash
# Using Docker (recommended - ensures correct dependencies)
docker build --target wasm-rust-guest.artifact --output type=local,dest=. .

# Or using local Rust toolchain (requires wasm32-unknown-unknown target)
cd wasm/rust-guest
cargo build --target wasm32-unknown-unknown --profile wasm
```

**Note**: Local builds should produce identical binaries when using the same Rust toolchain version and Docker environment. The attestation provides cryptographic proof of the build's provenance.

## Supply Chain Security Features

| Feature | WASM Binary | Java Provider | JS Provider | Ruby Provider | Go Provider |
|---------|-------------|---------------|-------------|---------------|-------------|
| **GitHub Attestation** | ✅ | ✅ | ✅ (npm provenance) | ✅ | N/A (source) |
| **Published to GitHub Releases** | ✅ | ❌ (Maven Central) | ❌ (npm) | ❌ (RubyGems) | N/A |
| **SHA-256 Checksum** | ✅ | ❌ | ❌ | ❌ | N/A |
| **Package Signing** | N/A | ✅ (GPG) | ✅ (Sigstore) | ❌ | N/A |
| **Embedded WASM Hash** | N/A | Manual verify | Manual verify | Manual verify | CI validated |

## WASM Synchronization

Different providers handle WASM embedding differently:

- **Go Provider**: WASM is committed to the repository (required for `//go:embed` at compile time)
  - **CI Validation**: Docker builds automatically validate committed WASM matches the latest build
  - **Manual Sync**: Maintainers run `make sync-wasm-go` after WASM changes
  - If validation fails: `❌ ERROR: WASM files are out of sync!` - follow the error message instructions

- **Java, JavaScript, Ruby Providers**: WASM is NOT committed
  - WASM is copied from the build artifact during Docker builds
  - Always uses the current WASM automatically - no manual sync needed

## Security Best Practices for Users

1. **Always verify attestations** for production deployments
2. **Pin specific versions** in your dependency management
3. **Review release notes** for security-related changes
4. **Monitor GitHub Security Advisories** for this repository
5. **Scan dependencies** regularly using tools like Dependabot or Snyk
