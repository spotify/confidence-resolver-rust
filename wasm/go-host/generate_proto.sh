#!/bin/bash

# Add Go bin to PATH
export PATH=$PATH:$(go env GOPATH)/bin

# Generate protobuf Go files
echo "Generating protobuf Go files..."

mkdir -p proto

# Generate messages.pb.go with M argument for go_package mapping
protoc --proto_path=../proto \
       --go_out=proto \
       --go_opt=paths=source_relative \
       --go_opt=Mmessages.proto=github.com/spotify/confidence/wasm-resolve-poc/go-host/proto/messages \
       messages.proto

# Generate resolver_api.pb.go with M argument for go_package mapping
protoc --proto_path=../proto \
       --go_out=proto \
       --go_opt=paths=source_relative \
       --go_opt=Mresolver/api.proto=github.com/spotify/confidence/wasm-resolve-poc/go-host/proto/resolver \
       resolver/api.proto

echo "Protobuf generation complete!"
echo "Generated files:"
find proto -name "*.go" -type f 
