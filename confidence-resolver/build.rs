use std::env;
use std::io::Result;
use std::path::PathBuf;

fn main() -> Result<()> {
    let root = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("protos");
    let proto_files = vec![
        root.join("confidence/flags/admin/v1/types.proto"),
        root.join("confidence/flags/admin/v1/resolver.proto"),
        root.join("confidence/flags/resolver/v1/api.proto"),
        root.join("confidence/flags/resolver/v1/internal_api.proto"),
        root.join("confidence/flags/resolver/v1/events/events.proto"),
    ];

    // Tell cargo to recompile if any of these proto files are changed
    for proto_file in &proto_files {
        println!("cargo:rerun-if-changed={}", proto_file.display());
    }

    let descriptor_path = PathBuf::from(env::var("OUT_DIR").unwrap()).join("proto_descriptor.bin");

    let mut config = prost_build::Config::new();

    config
        .file_descriptor_set_path(&descriptor_path)
        .btree_map(["."]);

    #[cfg(feature = "std")]
    {
        // Override prost-types with pbjson-types when std feature is enabled
        config
            .compile_well_known_types()
            .extern_path(".google.protobuf", "::pbjson_types");
    }

    // Generate prost structs
    config.compile_protos(&proto_files, &[root])?;

    #[cfg(feature = "std")]
    {
        // Generate pbjson serde implementations
        let descriptor_set = std::fs::read(&descriptor_path)?;
        pbjson_build::Builder::new()
            .register_descriptors(&descriptor_set)?
            .ignore_unknown_fields()
            .btree_map(["."])
            .build(&[
                ".confidence.flags.admin.v1",
                ".confidence.flags.resolver.v1",
                ".confidence.flags.resolver.v1.events",
                ".confidence.flags.types.v1",
                ".confidence.auth.v1",
                ".confidence.iam.v1",
                ".google.type",
            ])?;
    }

    Ok(())
}
