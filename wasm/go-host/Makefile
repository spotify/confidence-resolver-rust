# go-host Makefile
# Go WASM host example

ROOT := $(realpath $(CURDIR)/../..)

BUILD_STAMP := .build.stamp
SRC := $(shell find . -name '*.go')
PROTO := $(shell find proto -name '*.go' 2>/dev/null)
WASM_ARTIFACT := ../confidence_resolver.wasm

.PHONY: build run clean 

# Always build wasm if not in docker
ifneq ($(IN_DOCKER_BUILD),1)
.PHONY: $(WASM_ARTIFACT)
endif

$(WASM_ARTIFACT):
	$(MAKE) -C $(ROOT) wasm/confidence_resolver.wasm


$(BUILD_STAMP): go.mod go.sum $(SRC) $(PROTO)
	bash generate_proto.sh
	go build .
	touch $@

build: $(BUILD_STAMP)

run: $(WASM_ARTIFACT) $(BUILD_STAMP)
	go run .

clean:
	rm -rf proto/ $(BUILD_STAMP)
	go clean



