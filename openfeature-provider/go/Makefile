# openfeature-provider/go Makefile
# Go OpenFeature local provider

ROOT := $(realpath $(CURDIR)/../..)

# Stamps and directories
BUILD_STAMP := .build.stamp
PROTO_DIR := confidence/proto
WASM_ARTIFACT := $(ROOT)/wasm/confidence_resolver.wasm

# Source files
GO_SRC := $(shell find confidence -name '*.go' -not -path './confidence/proto/*')
PROTO_GEN := $(shell find $(PROTO_DIR) -name '*.pb.go' 2>/dev/null)

.PHONY: install build test lint clean proto

# Always build wasm if not in docker
ifneq ($(IN_DOCKER_BUILD),1)
.PHONY: build-wasm
build-wasm:
	@$(MAKE) -C $(ROOT) wasm/confidence_resolver.wasm
$(BUILD_STAMP): | build-wasm
endif

# Install Go dependencies
install:
	cd confidence && go mod download
	cd confidence && go mod verify

# Generate protobuf files
proto:
	./scripts/generate_proto.sh

# Build (compile check)
$(BUILD_STAMP): confidence/go.mod confidence/go.sum $(GO_SRC) $(PROTO_GEN)
	cd confidence && go build ./...
	@touch $@

build: $(BUILD_STAMP)

# Run tests
test: $(BUILD_STAMP)
	cd confidence && go test -v ./...

# Lint using gofmt and go vet
lint:
	@echo "Checking gofmt..."
	@test -z "$$(cd confidence && gofmt -l . | grep -v '^proto/' | tee /dev/stderr)" || (echo "Files need formatting. Run: cd confidence && gofmt -w ." && exit 1)
	@echo "Running go vet..."
	@cd confidence && go vet ./...
	@echo "✅ Lint passed"

# Clean build artifacts
clean:
	rm -rf $(PROTO_DIR)
	rm -f $(BUILD_STAMP)
	cd confidence && go clean -cache -testcache -modcache

.DEFAULT_GOAL := build
