# java-host Makefile
# Java WASM host example

ROOT := $(realpath $(CURDIR)/../..)

BUILD_STAMP := .build.stamp
SRC := $(shell find src -name '*.java' 2>/dev/null)
WASM_ARTIFACT := ../confidence_resolver.wasm

.PHONY: build run clean 

# Always build wasm if not in docker
ifneq ($(IN_DOCKER_BUILD),1)
.PHONY: $(WASM_ARTIFACT)
endif

$(WASM_ARTIFACT):
	$(MAKE) -C $(ROOT) wasm/confidence_resolver.wasm

$(BUILD_STAMP): pom.xml $(SRC)
	mvn -q package
	touch $@

build: $(BUILD_STAMP)

run: $(WASM_ARTIFACT) $(BUILD_STAMP)
	mvn -q exec:java

clean:
	mvn clean
	rm -f $(BUILD_STAMP)



