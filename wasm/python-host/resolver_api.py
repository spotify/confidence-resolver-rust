#!/usr/bin/env python3

import struct
from typing import List, Dict, Any

from google.protobuf import message
from datetime import datetime
from google.protobuf.timestamp_pb2 import Timestamp
from wasmtime import Engine, Store, Module, Instance, Func, Config, ValType, FuncType, Linker

# Import generated protobuf modules
from proto import messages_pb2
from proto.resolver import api_pb2

class ResolverApi:
    """Handles communication with the WASM module"""

    def __init__(self, wasm_bytes: bytes):
        # Create WASM engine and store
        # Create config and enable fuel consumption
        config = Config()
        # config.consume_fuel = True
        self.engine = Engine(config)
        self.store = Store(self.engine)

        # Compile the WASM module
        self.module = Module(self.engine, wasm_bytes)

        # Register host functions
        self._register_host_functions()

        # Get exported functions
        self.wasm_msg_alloc = self.instance.exports(self.store)["wasm_msg_alloc"]
        self.wasm_msg_free = self.instance.exports(self.store)["wasm_msg_free"]
        self.wasm_msg_guest_set_resolver_state = self.instance.exports(self.store)["wasm_msg_guest_set_resolver_state"]
        self.wasm_msg_guest_resolve = self.instance.exports(self.store)["wasm_msg_guest_resolve"]

    def _register_host_functions(self):
        """Register host functions that can be called from WASM"""

        def log_resolve(ptr: int) -> int:
            # Ignore payload; return Void
            response = messages_pb2.Response()
            response.data = messages_pb2.Void().SerializeToString()
            return self._transfer_response(response)

        def log_assign(ptr: int) -> int:
            # Ignore payload; return Void
            response = messages_pb2.Response()
            response.data = messages_pb2.Void().SerializeToString()
            return self._transfer_response(response)

        def current_time(ptr: int) -> int:
            """Host function to return current timestamp"""
            try:
                # Create timestamp
                timestamp = Timestamp()
                timestamp.FromDatetime(datetime.now())

                # Create response wrapper
                response = messages_pb2.Response()
                response.data = timestamp.SerializeToString()

                # Transfer response to WASM memory
                return self._transfer_response(response)
            except Exception as e:
                # Return error response
                error_response = messages_pb2.Response()
                error_response.error = str(e)
                return self._transfer_response(error_response)

        # # Register the host function
        # self.store.set_fuel(1000000)  # Add fuel for execution

        # Create function type: takes one i32 parameter, returns one i32
        func_type = FuncType([ValType.i32()], [ValType.i32()])
        host_func_time = Func(self.store, func_type, current_time)
        host_func_log_resolve = Func(self.store, func_type, log_resolve)
        host_func_log_assign = Func(self.store, func_type, log_assign)

        linker = Linker(self.store.engine)

        # Define the import with module and name
        linker.define(self.store, "wasm_msg", "wasm_msg_host_current_time", host_func_time)
        linker.define(self.store, "wasm_msg", "wasm_msg_host_log_resolve", host_func_log_resolve)
        linker.define(self.store, "wasm_msg", "wasm_msg_host_log_assign", host_func_log_assign)

        # Optional: current thread id function
        def current_thread_id() -> int:
            return 0
        linker.define(self.store, "wasm_msg", "wasm_msg_current_thread_id", Func(self.store, FuncType([], [ValType.i32()]), current_thread_id))

        # Instantiate the module with imports
        self.instance = linker.instantiate(self.store, self.module)

    def set_resolver_state(self, state: bytes) -> None:
        """Set the resolver state in the WASM module"""
        # Create request wrapper
        request = messages_pb2.Request()
        request.data = state
        # Transfer request to WASM memory - ensure it's a bytestring
        serialized_data: bytes = request.SerializeToString()
        req_ptr = self._transfer(serialized_data)
        # Call the WASM function
        results = self.wasm_msg_guest_set_resolver_state(self.store, req_ptr)
        resp_ptr = results
        # Consume the response
        self._consume_response(resp_ptr, lambda data: None)

    def resolve(self, request: api_pb2.ResolveFlagsRequest) -> api_pb2.ResolveFlagsResponse:
        """Resolve flags using the WASM module"""
        # Transfer request to WASM memory
        req_ptr = self._transfer_request(request)
        # Call the WASM function
        results = self.wasm_msg_guest_resolve(self.store, req_ptr)
        resp_ptr = results
        # Consume the response
        response = self._consume_response(resp_ptr, lambda data: self._parse_resolve_response(data))
        return response

    def _transfer_request(self, message: message.Message) -> int:
        """Transfer a protobuf message to WASM memory"""
        data = message.SerializeToString()
        request = messages_pb2.Request()
        request.data = data
        return self._transfer(request.SerializeToString())

    def _transfer_response(self, message: message.Message) -> int:
        """Transfer a response to WASM memory"""
        return self._transfer(message.SerializeToString())

    def _transfer(self, data: bytes) -> int:
        """Allocate memory in WASM and copy data"""
        # Allocate memory in WASM
        memory = self.instance.exports(self.store)["memory"]
        results = self.wasm_msg_alloc(self.store, len(data))
        # Write data to WASM memory
        memory.write(self.store, data, results)

        return results

    def _consume_response(self, addr: int, codec) -> Any:
        """Consume a response from WASM memory"""
        data = self._consume(addr)
        response = messages_pb2.Response()
        response.ParseFromString(data)

        if response.HasField('error'):
            raise Exception(f"WASM error: {response.error}")
        resp = codec(response.data)
        return resp

    def _consume(self, addr: int) -> bytes:
        """Read data from WASM memory and free it"""
        # Read length (assuming 4-byte length prefix)
        memory = self.instance.exports(self.store)["memory"]
        len_bytes = memory.read(self.store, addr - 4, addr)
        total_len = int.from_bytes(len_bytes, byteorder='little')
        length = total_len - 4

        # Read data
        data = memory.read(self.store, addr, addr + length)
        # Free memory
        self.wasm_msg_free(self.store, addr)

        return data

    def _parse_resolve_response(self, data: bytes) -> api_pb2.ResolveFlagsResponse:
        """Parse resolve response from protobuf data"""
        # Parse the protobuf bytes into ResolveFlagsResponse
        resolve_response = api_pb2.ResolveFlagsResponse()
        resolve_response.ParseFromString(data)
        return resolve_response