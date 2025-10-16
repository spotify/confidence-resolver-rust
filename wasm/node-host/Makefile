# node-host Makefile
# Local development commands only

ROOT := $(realpath $(CURDIR)/../..)

# Stamps and generated proto outputs
INSTALL_STAMP := .install.stamp
BUILD_STAMP := .build.stamp
GEN_DIR := src/proto
GEN_TS := $(GEN_DIR)/messages.ts $(GEN_DIR)/resolver/api.ts
PROTO_SRC := ../proto/messages.proto ../proto/resolver/api.proto
# Optional in Docker (src/ and tsconfig may be copied later in the stage)
SRC := $(shell [ -d src ] && find src -name '*.ts' -print || true)
CONFIG := package.json yarn.lock $(wildcard tsconfig.json)
WASM_ARTIFACT := ../confidence_resolver.wasm

.PHONY: build run clean 

# Always build wasm if not in docker
ifneq ($(IN_DOCKER_BUILD),1)
.PHONY: build-wasm
build-wasm:
	@$(MAKE) -C $(ROOT) wasm/confidence_resolver.wasm
$(BUILD_STAMP): | build-wasm
endif

# Install dependencies only when package manifests change
$(INSTALL_STAMP): package.json yarn.lock
	yarn install --immutable
	touch $@

# Generate TypeScript protos when sources or tools change
$(GEN_TS): $(PROTO_SRC) $(INSTALL_STAMP)
	@mkdir -p $(GEN_DIR)
	yarn proto:gen

$(BUILD_STAMP): $(INSTALL_STAMP) $(GEN_TS) $(SRC) $(CONFIG)
	@touch $@

build: $(BUILD_STAMP)

run: $(WASM_ARTIFACT) $(INSTALL_STAMP) $(GEN_TS)
	yarn start

clean:
	rm -rf node_modules src/proto $(INSTALL_STAMP) $(BUILD_STAMP)

