#!/usr/bin/env python3
"""Generate Python protobuf files from proto definitions."""

import subprocess
import sys
from pathlib import Path


def generate_proto() -> None:
    """Generate Python protobuf files from proto definitions."""
    # Get the proto source directory
    script_dir = Path(__file__).parent
    proto_src_dir = script_dir.parent / "confidence" / "proto"

    if not proto_src_dir.exists():
        print(f"Proto directory not found: {proto_src_dir}", file=sys.stderr)
        sys.exit(1)

    # Output directory is the same as source (in-place generation)
    proto_out_dir = proto_src_dir

    print(f"Generating protobuf files from {proto_src_dir}")

    # Find all .proto files (excluding test-only.proto)
    proto_files = [
        p for p in proto_src_dir.glob("*.proto") if not p.name.startswith("test-only")
    ]

    if not proto_files:
        print(f"No .proto files found in {proto_src_dir}", file=sys.stderr)
        sys.exit(1)

    # Generate Python files for each proto
    for proto_file in proto_files:
        print(f"Generating {proto_file.name}...")
        try:
            subprocess.run(
                [
                    "protoc",
                    f"--python_out={proto_out_dir}",
                    f"--pyi_out={proto_out_dir}",  # Generate type stubs
                    f"--proto_path={proto_src_dir}",
                    str(proto_file),
                ],
                check=True,
                capture_output=True,
                text=True,
            )
            pb_file = proto_file.stem + "_pb2.py"
            print(f"  Generated {proto_out_dir / pb_file}")
        except subprocess.CalledProcessError as e:
            print(f"Error generating {proto_file.name}:", file=sys.stderr)
            print(e.stderr, file=sys.stderr)
            sys.exit(1)

    print("Protobuf generation completed successfully!")


if __name__ == "__main__":
    generate_proto()
