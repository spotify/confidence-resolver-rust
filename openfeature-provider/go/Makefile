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
	go mod download
	go mod verify

# Generate protobuf files
proto:
	./scripts/generate_proto.sh

# Build (compile check)
$(BUILD_STAMP): go.mod go.sum $(GO_SRC) $(PROTO_GEN)
	go build ./...
	@touch $@

build: $(BUILD_STAMP)

# Run tests (excluding proto directory)
test: $(BUILD_STAMP)
	go test -v $$(go list ./... | grep -v '/proto')

# Lint using gofmt and go vet
lint:
	@echo "Checking gofmt..."
	@test -z "$$(gofmt -l . | grep -v '^proto/' | tee /dev/stderr)" || (echo "Files need formatting. Run: gofmt -w ." && exit 1)
	@echo "Running go vet..."
	@go vet ./...
	@echo "✅ Lint passed"

# Clean build artifacts
clean:
	rm -rf $(PROTO_DIR)
	rm -f $(BUILD_STAMP)
	cd confidence && go clean -cache -testcache -modcache

.DEFAULT_GOAL := build
