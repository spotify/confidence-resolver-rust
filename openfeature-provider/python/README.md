# Confidence OpenFeature Provider for Python

Python implementation of the [OpenFeature](https://openfeature.dev) Provider for [Confidence](https://confidence.spotify.com) with local resolve capabilities using WASM-based flag evaluation.

## Features

- **Local Flag Resolution**: Uses a WASM-based resolver for fast, local flag evaluation
- **Automatic State Updates**: Periodically fetches the latest flag configuration from CDN
- **Async/Await Support**: Full async support using Python's asyncio
- **ETag Caching**: Efficient state fetching with HTTP ETag-based caching
- **Periodic Log Flushing**: Automatically sends flag evaluation logs to Confidence backend
- **OpenFeature Compatible**: Implements the OpenFeature Provider interface

## Requirements

- Python 3.9 or higher
- OpenFeature SDK for Python
- wasmtime (WASM runtime)
- httpx (async HTTP client)
- protobuf (protocol buffers support)

## Installation

```bash
pip install confidence-openfeature-provider
```

## Usage

### Basic Setup

```python
import asyncio
from openfeature import api
from openfeature.evaluation_context import EvaluationContext
from confidence import ConfidenceServerProviderLocal

async def main():
    # Create and set the Confidence provider
    provider = ConfidenceServerProviderLocal(
        client_secret="your_client_secret_here"
    )

    # Set the provider for OpenFeature
    await api.set_provider_async(provider)

    # Get a client
    client = api.get_client()

    # Evaluate a flag
    context = EvaluationContext(
        targeting_key="user-123",
        attributes={
            "country": "US",
            "environment": "production"
        }
    )

    enabled = await client.get_boolean_value(
        "my-feature.enabled",
        default_value=False,
        evaluation_context=context
    )

    print(f"Feature enabled: {enabled}")

    # Clean up
    await api.shutdown_async()

if __name__ == "__main__":
    asyncio.run(main())
```

### Configuration Options

The provider accepts several configuration parameters:

```python
provider = ConfidenceServerProviderLocal(
    client_secret="your_client_secret",
    # How often to fetch state updates (seconds)
    state_fetch_interval=30.0,
    # How often to flush logs (seconds)
    log_flush_interval=10.0,
    # Timeout for initialization (seconds)
    initialize_timeout=30.0,
    # Optional: custom WASM binary path
    wasm_path=Path("/path/to/confidence_resolver.wasm")
)
```

### Nested Flag Values

You can access nested properties in flag values using dot notation:

```python
# If flag "my-config" returns {"ui": {"theme": "dark", "size": "large"}}
theme = await client.get_string_value(
    "my-config.ui.theme",
    default_value="light",
    evaluation_context=context
)
```

### Evaluation Context

The evaluation context is used for flag targeting and segmentation:

```python
context = EvaluationContext(
    targeting_key="user-456",  # Required for targeting
    attributes={
        "email": "user@example.com",
        "subscription": "premium",
        "country": "SE",
        "version": "2.1.0"
    }
)

result = await client.get_object_evaluation(
    "feature-config",
    default_value={},
    evaluation_context=context
)
```

## Development

### Setup Development Environment

```bash
# Clone the repository
git clone https://github.com/spotify/confidence-resolver.git
cd confidence-resolver/openfeature-provider/python

# Install dependencies (creates .venv automatically)
make install

# Generate proto files
make proto
```

The `make install` command will automatically:
- Create a Python virtual environment in `.venv/`
- Install all dependencies including dev tools
- When running in Docker (`IN_DOCKER_BUILD=1`), it skips venv creation

All `make` commands will use the virtual environment automatically. If you need to run Python commands directly:

```bash
source .venv/bin/activate  # Activate venv
python3 --version          # Now using venv Python
```

### Running Tests

```bash
# Run unit and integration tests
make test

# Run e2e tests (requires credentials)
export CONFIDENCE_CLIENT_SECRET=your_secret
make test-e2e

# Run linter
make lint

# Format code
make format
```

### Building

```bash
# Build wheel package
make build

# This creates dist/confidence-openfeature-provider-*.whl
```

## Architecture

The provider consists of several components:

- **WasmResolver**: Interfaces with the WASM module for flag evaluation
- **StateFetcher**: Fetches flag state from CDN with ETag caching
- **FlagLogger**: Sends flag evaluation logs to Confidence backend
- **ConfidenceServerProviderLocal**: Main provider implementing OpenFeature interface

### State Management

1. Initial state is fetched during provider initialization
2. Background task periodically updates state (default: every 30 seconds)
3. ETag-based HTTP caching minimizes bandwidth usage
4. State is cached in memory for resilience during network issues

### Log Flushing

1. Flag evaluations generate logs in WASM
2. Background task periodically flushes logs (default: every 10 seconds)
3. Logs are sent as protobuf to Confidence backend
4. Final flush occurs during provider shutdown

## Contributing

Contributions are welcome! Please follow these guidelines:

1. Follow conventional commit style
2. Ensure tests pass: `make test`
3. Format code: `make format`
4. Update CHANGELOG.md for user-facing changes

## License

Apache-2.0

## Links

- [Confidence Documentation](https://confidence.spotify.com)
- [OpenFeature Documentation](https://openfeature.dev)
- [GitHub Repository](https://github.com/spotify/confidence-resolver)
