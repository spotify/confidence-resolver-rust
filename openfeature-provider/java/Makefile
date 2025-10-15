# openfeature-provider/java Makefile
# Java OpenFeature local provider

ROOT := $(realpath $(CURDIR)/../..)

# Stamps and inputs
BUILD_STAMP := .build.stamp
INSTALL_STAMP := .install.stamp
SRC := $(shell find src -name '*.java' 2>/dev/null)
RESOURCES_WASM := src/main/resources/wasm/confidence_resolver.wasm
LOCAL_WASM := $(ROOT)/wasm/confidence_resolver.wasm

.PHONY: build test clean

# Always build wasm if not in docker
ifneq ($(IN_DOCKER_BUILD),1)
.PHONY: build-wasm
build-wasm:
	@$(MAKE) -C $(ROOT) wasm/confidence_resolver.wasm
$(BUILD_STAMP): | build-wasm
endif

ifneq ($(IN_DOCKER_BUILD),1)
$(RESOURCES_WASM):
	@mkdir -p $(dir $@)
	cp $(LOCAL_WASM) $@
else
# In Docker, copy the pre-staged WASM into resources
$(RESOURCES_WASM):
	@mkdir -p $(dir $@)
	@test -f $(LOCAL_WASM) || (echo "Missing $(LOCAL_WASM) (expected via Docker COPY)"; exit 1)
	cp $(LOCAL_WASM) $@
endif

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

build: $(BUILD_STAMP)

test: $(BUILD_STAMP)
	mvn -q test

clean:
	mvn -q clean
	rm -f $(BUILD_STAMP)
	rm -f $(INSTALL_STAMP)
	rm -f $(RESOURCES_WASM)


