#!/usr/bin/env python3

import os
import sys
from pathlib import Path
from google.protobuf.struct_pb2 import Struct
import proto.resolver.api_pb2 as api_pb2
from resolver_api import ResolverApi

 

def main():
    # Load the WASM module
    wasm_path = Path(__file__).parent / ".." / "confidence_resolver.wasm"
    if not wasm_path.exists():
        print(f"WASM file not found: {wasm_path}")
        sys.exit(1)

    # Load resolver state
    resolver_state_path = Path(__file__).parent / ".." / "resolver_state.pb"
    if not resolver_state_path.exists():
        print(f"Resolver state file not found: {resolver_state_path}")
        sys.exit(1)

    resolver_state = resolver_state_path.read_bytes()

    # Create resolver API
    api = ResolverApi(wasm_path.read_bytes())

    # Set resolver state
    try:
        api.set_resolver_state(resolver_state, "confidence-demo-june")
    except Exception as e:
        print(f"Failed to set resolver state: {e}")
        sys.exit(1)

    # Verify MATCH reason and non-empty variant for tutorial_visitor
    verify_ctx = Struct()
    verify_ctx.fields["targeting_key"].string_value = "tutorial_visitor"
    verify_ctx.fields["visitor_id"].string_value = "tutorial_visitor"
    verify_req = api_pb2.ResolveFlagsRequest(
        client_secret="mkjJruAATQWjeY7foFIWfVAcBWnci2YF",
        apply=False,
        evaluation_context=verify_ctx,
        flags=["flags/tutorial-feature"],
    )
    verify_resp = api.resolve(verify_req)
    if not verify_resp or not verify_resp.resolved_flags:
        print("No flags resolved for tutorial-feature")
        sys.exit(1)
    rf = verify_resp.resolved_flags[0]
    if rf.reason != api_pb2.RESOLVE_REASON_MATCH:
        print(f"Expected reason RESOLVE_REASON_MATCH, got {rf.reason}")
        sys.exit(1)
    if not rf.variant:
        print("Expected non-empty variant for tutorial-feature")
        sys.exit(1)
    # Extract string title value
    title_val = None
    if rf.value and rf.value.fields:
        if "title" in rf.value.fields and rf.value.fields["title"].HasField("string_value"):
            title_val = rf.value.fields["title"].string_value
        elif "value" in rf.value.fields and rf.value.fields["value"].HasField("string_value"):
            title_val = rf.value.fields["value"].string_value
        else:
            for v in rf.value.fields.values():
                if v.HasField("string_value"):
                    title_val = v.string_value
                    break
    print(f"tutorial-feature verified: reason=RESOLVE_REASON_MATCH variant={rf.variant} title={title_val}")

    # Done: single flag verified above

if __name__ == "__main__":
    main()