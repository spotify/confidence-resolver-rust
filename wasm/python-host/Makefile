# python-host Makefile
# Python WASM host example

ROOT := $(realpath $(CURDIR)/../..)

BUILD_STAMP := .build.stamp
PROTO_OUT := .venv/proto
REQ := $(wildcard requirements.txt)
SRC := $(shell find . -name '*.py')
WASM_ARTIFACT := ../confidence_resolver.wasm

.PHONY: build run clean 

# Always build wasm if not in docker
ifneq ($(IN_DOCKER_BUILD),1)
.PHONY: $(WASM_ARTIFACT)
endif

$(WASM_ARTIFACT):
	$(MAKE) -C $(ROOT) wasm/confidence_resolver.wasm

$(BUILD_STAMP): $(REQ) generate_proto.py $(SRC)
	python3 -m venv .venv
	.venv/bin/pip install --upgrade pip
	.venv/bin/pip install --require-virtualenv wasmtime protobuf
	.venv/bin/python generate_proto.py --out $(PROTO_OUT)
	touch $@

build: $(BUILD_STAMP)

run: $(WASM_ARTIFACT) $(BUILD_STAMP)
	PYTHONPATH=$$(pwd)/.venv:$$(pwd)/.venv/proto:$$PYTHONPATH .venv/bin/python main.py

clean:
	rm -rf .venv/ $(BUILD_STAMP)



