# openfeature-provider/js Makefile
# TypeScript OpenFeature provider

ROOT := $(realpath $(CURDIR)/../..)

# Stamps and inputs
INSTALL_STAMP := .install.stamp
BUILD_STAMP := .build.stamp
GEN_DIR := src/proto
GEN_TS := $(GEN_DIR)/messages.ts $(GEN_DIR)/api.ts
PROTO_SRC := proto/messages.proto proto/api.proto
SRC := $(shell find src -name '*.ts')
CONFIG := package.json yarn.lock tsconfig.json tsdown.config.ts vitest.config.ts
WASM_ARTIFACT := $(ROOT)/wasm/confidence_resolver.wasm

.PHONY: install build test clean 

# Always build wasm if not in docker
ifneq ($(IN_DOCKER_BUILD),1)
.PHONY: build-wasm
build-wasm:
	@$(MAKE) -C $(ROOT) wasm/confidence_resolver.wasm
$(BUILD_STAMP): | build-wasm
endif

$(WASM_ARTIFACT):
	@mkdir -p $(dir $@)
	@if [ -n "$$CONFIDENCE_RESOLVER_VERSION" ]; then \
	  REPO="$$CONFIDENCE_RESOLVER_REPO"; \
	  if [ -z "$$REPO" ]; then REPO="$$GITHUB_REPOSITORY"; fi; \
	  if [ -z "$$REPO" ]; then REPO=$(shell git config --get remote.origin.url | sed -E 's#.*[:/]([^/]+/[^/]+?)(\\.git)?$#\\1#'); fi; \
	  URL="https://github.com/$$REPO/releases/download/confidence_resolver-v$$CONFIDENCE_RESOLVER_VERSION/confidence_resolver.wasm"; \
	  echo "Downloading WASM from $$URL"; \
	  if [ -n "$$GITHUB_TOKEN" ]; then \
	    curl -fsSL -H "Authorization: Bearer $$GITHUB_TOKEN" -o $@ "$$URL"; \
	  else \
	    curl -fsSL -o $@ "$$URL"; \
	  fi; \
	else \
	  $(MAKE) -C $(ROOT) wasm/confidence_resolver.wasm; \
	fi

# Install dependencies only when package manifests change
$(INSTALL_STAMP): package.json yarn.lock
	yarn install --immutable
	@touch $@

# Generate TypeScript protos when sources or tools change
$(GEN_TS): $(PROTO_SRC) $(INSTALL_STAMP)
	@mkdir -p $(GEN_DIR)
	yarn proto:gen

$(BUILD_STAMP): $(WASM_ARTIFACT) $(INSTALL_STAMP) $(GEN_TS) $(SRC) $(CONFIG)
	yarn build
	@touch $@

install: $(INSTALL_STAMP) $(GEN_TS)

build: $(BUILD_STAMP)

test: $(WASM_ARTIFACT) $(INSTALL_STAMP) $(GEN_TS)
	yarn test --run --exclude='**/*.e2e.test.ts'

clean:
	rm -rf node_modules dist src/proto $(INSTALL_STAMP) $(BUILD_STAMP)

