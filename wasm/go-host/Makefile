# go-host Makefile
# Go WASM host example

.PHONY: build
build:
	bash generate_proto.sh
	go build .

.PHONY: run
run:
	bash generate_proto.sh
	go run .

.PHONY: clean
clean:
	rm -rf proto/
	go clean

