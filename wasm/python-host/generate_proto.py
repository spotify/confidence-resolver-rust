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

    # Get the confidence-resolver protos directory (needed for imports)
    confidence_protos_dir = Path(__file__).parent.parent.parent / "confidence-resolver" / "protos"

    # Determine output directory (defaults to local proto/, can be overridden)
    python_proto_dir = out_dir if out_dir is not None else (Path(__file__).parent / "proto")
    python_proto_dir.mkdir(exist_ok=True)

    # Ensure resolver package directory exists for __init__.py
    resolver_dir = python_proto_dir / "resolver"
    resolver_dir.mkdir(exist_ok=True)

    # Generate all proto files in one command to ensure correct imports
    # This allows protoc to properly resolve dependencies between proto files
    proto_files = []
    messages_proto = proto_dir / "messages.proto"
    types_proto = proto_dir / "types.proto"
    api_proto = proto_dir / "resolver" / "api.proto"
    confidence_types_proto = confidence_protos_dir / "confidence" / "flags" / "resolver" / "v1" / "types.proto"

    if messages_proto.exists():
        proto_files.append(str(messages_proto))
    if types_proto.exists():
        proto_files.append(str(types_proto))
    if api_proto.exists():
        proto_files.append(str(api_proto))
    if confidence_types_proto.exists():
        proto_files.append(str(confidence_types_proto))

    if proto_files:
        # Build protoc command with multiple proto paths
        protoc_cmd = [
            "protoc",
            f"--python_out={python_proto_dir}",
            f"--proto_path={proto_dir}",
        ]
        
        # Add confidence-resolver protos path if it exists
        if confidence_protos_dir.exists():
            protoc_cmd.append(f"--proto_path={confidence_protos_dir}")
        
        # Add system proto path for google protos (installed via libprotobuf-dev)
        # These are typically in /usr/include on Debian/Ubuntu systems
        system_proto_path = Path("/usr/include")
        if system_proto_path.exists():
            protoc_cmd.append(f"--proto_path={system_proto_path}")
        
        protoc_cmd.extend(proto_files)
        
        subprocess.run(protoc_cmd, check=True)
        print(f"Generated {len(proto_files)} proto file(s)")
        for pf in proto_files:
            print(f"  - {Path(pf).name}")

    # Create __init__.py files for Python package structure
    (python_proto_dir / "__init__.py").touch()
    (resolver_dir / "__init__.py").touch()
    
    # Create __init__.py for confidence package structure if it exists
    confidence_dir = python_proto_dir / "confidence"
    if confidence_dir.exists():
        (confidence_dir / "__init__.py").touch()
        flags_dir = confidence_dir / "flags"
        if flags_dir.exists():
            (flags_dir / "__init__.py").touch()
            resolver_v1_dir = flags_dir / "resolver" / "v1"
            if resolver_v1_dir.exists():
                (flags_dir / "resolver" / "__init__.py").touch()
                (resolver_v1_dir / "__init__.py").touch()

    print("Protobuf generation completed!")

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Generate Python protobuf files")
    parser.add_argument("--out", dest="out", type=str, default=None, help="Output directory for generated code")
    args = parser.parse_args()
    out_dir = Path(args.out) if args.out else None
    generate_proto(out_dir)