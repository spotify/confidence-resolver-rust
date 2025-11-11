PROTO_SRC := $(shell find proto/mock -type f -name '*.proto' | sort)
GEN_DIR := genproto
GOBIN := $(shell go env GOPATH)/bin

.PHONY: gen run-iam clean

gen: $(PROTO_SRC)
	@mkdir -p $(GEN_DIR)
	PATH="$(GOBIN):$$PATH" protoc -I proto \
		--go_out=paths=source_relative:$(GEN_DIR) \
		--go-grpc_out=paths=source_relative:$(GEN_DIR) \
		--grpc-gateway_out=paths=source_relative,allow_delete_body=true:$(GEN_DIR) \
		$(PROTO_SRC)

clean:
	rm -rf $(GEN_DIR)


