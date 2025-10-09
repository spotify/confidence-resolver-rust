# python-host Makefile
# Python WASM host example

.PHONY: build
build:
	python3 -m venv .venv
	.venv/bin/pip install --upgrade pip
	.venv/bin/pip install --require-virtualenv wasmtime protobuf
	.venv/bin/python generate_proto.py --out .venv/proto

.PHONY: run
run:
	@if [ ! -d .venv ]; then $(MAKE) build; fi
	PYTHONPATH=$$(pwd)/.venv:$$(pwd)/.venv/proto:$$PYTHONPATH .venv/bin/python main.py

.PHONY: clean
clean:
	rm -rf .venv/

