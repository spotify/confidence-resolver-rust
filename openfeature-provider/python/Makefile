# openfeature-provider/python Makefile
# Python OpenFeature provider

ROOT := $(realpath $(CURDIR)/../..)

# Stamps and inputs
INSTALL_STAMP := .install.stamp
BUILD_STAMP := .build.stamp
PROTO_STAMP := .proto.stamp
GEN_DIR := confidence/proto
PROTO_SRC := $(GEN_DIR)/messages.proto $(GEN_DIR)/api.proto
GEN_PY := $(GEN_DIR)/messages_pb2.py $(GEN_DIR)/api_pb2.py
SRC := $(shell find confidence -name '*.py' -not -path '*/proto/*_pb2.py' 2>/dev/null)
TESTS := $(shell find tests -name '*.py' 2>/dev/null)
WASM_ARTIFACT := $(ROOT)/wasm/confidence_resolver.wasm
WASM_DEST := confidence/wasm/confidence_resolver.wasm

.PHONY: install proto build test test-e2e lint clean

# Always build wasm if not in docker
ifneq ($(IN_DOCKER_BUILD),1)
.PHONY: build-wasm
build-wasm:
	@$(MAKE) -C $(ROOT) wasm/confidence_resolver.wasm

# Copy WASM to local location for local development
$(WASM_DEST): $(WASM_ARTIFACT)
	@mkdir -p confidence/wasm
	cp $(WASM_ARTIFACT) $(WASM_DEST)

$(BUILD_STAMP): | build-wasm
$(BUILD_STAMP): $(WASM_DEST)
endif

# Install dependencies
$(INSTALL_STAMP): pyproject.toml
ifneq ($(IN_DOCKER_BUILD),1)
	python3 -m venv .venv
	.venv/bin/pip install --upgrade pip
	.venv/bin/pip install -e .[dev]
else
	pip install -e .[dev]
endif
	@touch $@

install: $(INSTALL_STAMP)

# Define Python/pip commands based on environment
ifneq ($(IN_DOCKER_BUILD),1)
PYTHON := .venv/bin/python3
PIP := .venv/bin/pip
PYTEST := .venv/bin/pytest
RUFF := .venv/bin/ruff
BLACK := .venv/bin/black
MYPY := .venv/bin/mypy
else
PYTHON := python3
PIP := pip
PYTEST := pytest
RUFF := ruff
BLACK := black
MYPY := mypy
endif

# Generate protobuf files
$(PROTO_STAMP): $(PROTO_SRC) $(INSTALL_STAMP)
	$(PYTHON) scripts/generate_proto.py
	@touch $@

proto: $(PROTO_STAMP)

# Build package
$(BUILD_STAMP): $(WASM_ARTIFACT) $(INSTALL_STAMP) $(PROTO_STAMP) $(SRC) pyproject.toml
	$(PYTHON) -m build
	@touch $@

build: $(BUILD_STAMP)

# Run unit and integration tests
test: $(WASM_DEST) $(INSTALL_STAMP) $(PROTO_STAMP)
	$(PYTEST) tests/ -v --ignore=tests/e2e_test.py

# Run e2e tests (requires credentials)
test-e2e: $(WASM_DEST) $(INSTALL_STAMP) $(PROTO_STAMP)
	$(PYTEST) tests/e2e_test.py -v

# Run linters
lint: $(INSTALL_STAMP)
	$(RUFF) check confidence/ tests/
	$(BLACK) --check confidence/ tests/
	$(MYPY) confidence/

# Format code
format: $(INSTALL_STAMP)
	$(RUFF) check --fix confidence/ tests/
	$(BLACK) confidence/ tests/

# Clean build artifacts
clean:
	rm -rf build/ dist/ *.egg-info/
	rm -rf $(GEN_DIR)/*_pb2.py $(GEN_DIR)/*_pb2.pyi
	rm -rf .pytest_cache/ .ruff_cache/ .mypy_cache/
	rm -rf __pycache__/ */__pycache__/ */*/__pycache__/
	rm -rf .venv/
	rm -f $(WASM_DEST)
	rm -f $(INSTALL_STAMP) $(BUILD_STAMP) $(PROTO_STAMP)

.DEFAULT_GOAL := build
