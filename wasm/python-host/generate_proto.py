#!/usr/bin/env python3

import subprocess
import sys
from pathlib import Path
import argparse

def generate_proto(out_dir: Path | None = None):
    """Generate Python protobuf files from proto definitions"""

    # Get the proto directory
    proto_dir = Path(__file__).parent.parent / "proto"
    if not proto_dir.exists():
        print(f"Proto directory not found: {proto_dir}")
        sys.exit(1)

    # Determine output directory (defaults to local proto/, can be overridden)
    python_proto_dir = out_dir if out_dir is not None else (Path(__file__).parent / "proto")
    python_proto_dir.mkdir(exist_ok=True)

    # Generate Python files for messages.proto
    messages_proto = proto_dir / "messages.proto"
    if messages_proto.exists():
        subprocess.run([
            "protoc",
            f"--python_out={python_proto_dir}",
            f"--proto_path={proto_dir}",
            str(messages_proto)
        ], check=True)
        print(f"Generated {python_proto_dir / 'messages_pb2.py'}")

    # Ensure resolver package directory exists for __init__.py
    resolver_dir = python_proto_dir / "resolver"
    resolver_dir.mkdir(exist_ok=True)

    # Generate Python files for resolver/api.proto
    api_proto = proto_dir / "resolver" / "api.proto"
    if api_proto.exists():
        subprocess.run([
            "protoc",
            f"--python_out={python_proto_dir}",
            f"--proto_path={proto_dir}",
            str(api_proto)
        ], check=True)
        print(f"Generated {python_proto_dir / 'resolver' / 'api_pb2.py'}")

    # Create __init__.py files
    (python_proto_dir / "__init__.py").touch()
    (resolver_dir / "__init__.py").touch()

    print("Protobuf generation completed!")

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Generate Python protobuf files")
    parser.add_argument("--out", dest="out", type=str, default=None, help="Output directory for generated code")
    args = parser.parse_args()
    out_dir = Path(args.out) if args.out else None
    generate_proto(out_dir)