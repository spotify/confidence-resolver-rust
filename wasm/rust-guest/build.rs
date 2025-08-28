fn main() {
    let mut config = prost_build::Config::new();
    // Needed for proto3 optional fields on older protoc versions available in CI images
    config.protoc_arg("--experimental_allow_proto3_optional");

    config
        .compile_protos(&["messages.proto", "types.proto"], &["../proto", "proto"])
        .unwrap_or_else(|e| panic!("Failed to compile protos {:?}", e));
}
