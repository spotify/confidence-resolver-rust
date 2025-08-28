# Python Host for WASM Resolve POC

This is a Python implementation of the WASM host equivalent to the Java and Go hosts.

## Prerequisites

- Python 3.10+
- `protoc` (Protocol Buffers compiler)

#### Quick start (recommended)
From the repo root:

```bash
make run-python-host
```

## Setup

1. Create and activate a virtual environment (once per machine):
```bash
python3 -m venv .venv
source .venv/bin/activate
python -m pip install --upgrade pip
pip install wasmtime protobuf
```

2. Generate protobuf files into a temp dir inside the venv:
```bash
python generate_proto.py --out .venv/proto
export PYTHONPATH=$(pwd)/.venv:$(pwd)/.venv/proto:$PYTHONPATH
```

## Usage

Run the verification and performance test:
```bash
PYTHONPATH=$(pwd)/.venv:$(pwd)/.venv/proto:$PYTHONPATH python main.py
```

## Structure

- `main.py` - Main entry point (equivalent to `Main.java` and `main.go`)
- `resolver_api.py` - WASM interop and resolver API
- `generate_proto.py` - Script to generate Python protobuf files (supports `--out`)
- (Generated code lives under `.venv/proto` when using the steps above)

## Key Differences from Go/Java

1. **WASM Runtime**: Uses `wasmtime` instead of `wazero` (Go) or `chicory` (Java)
2. **Memory Management**: Similar pointer-based approach but with Python's `wasmtime` API
3. **Host Functions**: Registered using `wasmtime.Func` with Python callbacks
4. **Protobuf**: Uses `google.protobuf` Python library

## Performance

The Python implementation should be slower than Go/Java due to:
- Python's interpreted nature
- GIL (Global Interpreter Lock) overhead
- Additional abstraction layers in `wasmtime` Python bindings