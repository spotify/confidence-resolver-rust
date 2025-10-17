# openfeature-provider/java Makefile
# Java OpenFeature local provider

ROOT := $(realpath $(CURDIR)/../..)

# Stamps and inputs
BUILD_STAMP := .build.stamp
INSTALL_STAMP := .install.stamp
SRC := $(shell find src -name '*.java' 2>/dev/null)
RESOURCES_WASM := src/main/resources/wasm/confidence_resolver.wasm
LOCAL_WASM := $(ROOT)/wasm/confidence_resolver.wasm
MARKER := .embedded-wasm-version

.PHONY: FORCE
FORCE:

.PHONY: build test clean

$(RESOURCES_WASM):
	@mkdir -p $(dir $(RESOURCES_WASM))
	@if [ "$(IN_DOCKER_BUILD)" = "1" ]; then \
		test -f $(LOCAL_WASM) || (echo "Missing $(LOCAL_WASM) (expected via Docker COPY)"; exit 1); \
	else \
		$(MAKE) -C $(ROOT) wasm/confidence_resolver.wasm; \
	fi
	@echo "Copying WASM from $(LOCAL_WASM) to $(RESOURCES_WASM)"
	cp $(LOCAL_WASM) $(RESOURCES_WASM)
	@HASH_S256=$$(if command -v sha256sum >/dev/null 2>&1; then sha256sum $(RESOURCES_WASM) | awk '{print $$1}'; else shasum -a 256 $(RESOURCES_WASM) | awk '{print $$1}'; fi); \
	if [ "$(IN_DOCKER_BUILD)" = "1" ]; then \
		if [ -f "$(MARKER)" ]; then \
			MARKER_VAL=$$(cat $(MARKER)); \
			if [ "sha256:$$HASH_S256" != "$$MARKER_VAL" ]; then \
				echo "ERROR: Embedded WASM SHA mismatch. Expected $$MARKER_VAL, got sha256:$$HASH_S256"; \
				exit 1; \
			fi; \
		else \
			echo "ERROR: Missing $(MARKER) during Docker build; refusing to proceed"; \
			exit 1; \
		fi; \
	else \
		echo sha256:$$HASH_S256 > $(MARKER); \
	fi

$(RESOURCES_WASM): FORCE

# A lightweight install target to prep resources
$(INSTALL_STAMP): pom.xml $(RESOURCES_WASM)
	touch $@


$(BUILD_STAMP): $(INSTALL_STAMP) pom.xml $(SRC)
	mvn -q -DskipTests protobuf:compile
	@if [ -f "$(GEN_MESSAGES)" ]; then \
		mkdir -p src/main/java/com/spotify/confidence/wasm; \
		cp $(GEN_MESSAGES) src/main/java/com/spotify/confidence/wasm/Messages.java; \
	fi
	mvn -q -DskipTests package
	touch $@

install: $(INSTALL_STAMP)

build: $(RESOURCES_WASM) $(BUILD_STAMP)

test: $(BUILD_STAMP)
	mvn -q test

clean:
	mvn -q clean
	rm -f $(BUILD_STAMP)
	rm -f $(INSTALL_STAMP)
	rm -f $(RESOURCES_WASM)


