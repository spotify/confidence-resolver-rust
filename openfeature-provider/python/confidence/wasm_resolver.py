"""WASM resolver for flag evaluation using wasmtime runtime."""

import logging
import time
from pathlib import Path
from typing import Optional

from google.protobuf.timestamp_pb2 import Timestamp
from wasmtime import Config, Engine, Func, FuncType, Instance, Linker, Module, Store, ValType

# Proto imports will be generated
from confidence.proto import api_pb2, messages_pb2

logger = logging.getLogger(__name__)


class UnsafeWasmResolver:
    """Direct WASM interface without error recovery."""

    def __init__(self, module: Module, store: Store):
        """Initialize the WASM resolver with a compiled module.

        Args:
            module: Compiled WASM module
            store: WASM store for execution
        """
        self.store = store
        self.module = module

        # Register host functions and instantiate
        self._register_host_functions()

        # Get exported functions
        exports = self.instance.exports(self.store)
        self.wasm_msg_alloc = exports["wasm_msg_alloc"]
        self.wasm_msg_free = exports["wasm_msg_free"]
        self.wasm_msg_guest_resolve_with_sticky = exports["wasm_msg_guest_resolve_with_sticky"]
        self.wasm_msg_guest_set_resolver_state = exports["wasm_msg_guest_set_resolver_state"]
        self.wasm_msg_guest_flush_logs = exports["wasm_msg_guest_flush_logs"]
        self.memory = exports["memory"]

    def _register_host_functions(self) -> None:
        """Register host functions that can be called from WASM."""

        def current_time(_: int) -> int:
            """Host function to return current timestamp."""
            try:
                # Create timestamp
                timestamp = Timestamp()
                timestamp.FromDatetime(time.gmtime(time.time()))

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

        # Create function type: takes one i32 parameter, returns one i32
        func_type = FuncType([ValType.i32()], [ValType.i32()])
        host_func_time = Func(self.store, func_type, current_time)

        # Create linker and define imports
        linker = Linker(self.store.engine)
        linker.define(self.store, "wasm_msg", "wasm_msg_host_current_time", host_func_time)

        # Instantiate the module with imports
        self.instance = linker.instantiate(self.store, self.module)

    def resolve_with_sticky(
        self, request: api_pb2.ResolveWithStickyRequest
    ) -> api_pb2.ResolveWithStickyResponse:
        """Resolve flags with sticky assignments.

        Args:
            request: Resolve request with materialization context

        Returns:
            Resolve response with flag values or missing materializations

        Raises:
            Exception: If WASM returns an error
        """
        req_ptr = self._transfer_request(request)
        res_ptr = self.wasm_msg_guest_resolve_with_sticky(self.store, req_ptr)
        return self._consume_response(res_ptr, api_pb2.ResolveWithStickyResponse)

    def set_resolver_state(self, request: api_pb2.SetResolverStateRequest) -> None:
        """Set the resolver state in the WASM module.

        Args:
            request: State update request with account ID and state bytes

        Raises:
            Exception: If WASM returns an error
        """
        req_ptr = self._transfer_request(request)
        res_ptr = self.wasm_msg_guest_set_resolver_state(self.store, req_ptr)
        # Consume response to ensure no errors (returns Void)
        self._consume_response(res_ptr, messages_pb2.Void)

    def flush_logs(self) -> bytes:
        """Flush accumulated logs from WASM.

        Returns:
            Serialized WriteFlagLogsRequest as bytes

        Raises:
            Exception: If WASM returns an error
        """
        res_ptr = self.wasm_msg_guest_flush_logs(self.store, 0)
        response_data = self._consume(res_ptr)
        response = messages_pb2.Response()
        response.ParseFromString(response_data)

        if response.HasField("error"):
            raise Exception(f"WASM error during flush_logs: {response.error}")

        return response.data

    def _transfer_request(self, message) -> int:
        """Transfer a protobuf message to WASM memory as a Request.

        Args:
            message: Protobuf message to transfer

        Returns:
            Pointer to the message in WASM memory
        """
        data = message.SerializeToString()
        request = messages_pb2.Request()
        request.data = data
        return self._transfer(request.SerializeToString())

    def _transfer_response(self, message) -> int:
        """Transfer a response to WASM memory.

        Args:
            message: Response message to transfer

        Returns:
            Pointer to the message in WASM memory
        """
        return self._transfer(message.SerializeToString())

    def _transfer(self, data: bytes) -> int:
        """Allocate memory in WASM and copy data.

        Args:
            data: Bytes to transfer

        Returns:
            Pointer to the allocated memory
        """
        # Allocate memory in WASM
        ptr = self.wasm_msg_alloc(self.store, len(data))
        # Write data to WASM memory
        self.memory.write(self.store, data, ptr)
        return ptr

    def _consume_response(self, addr: int, message_type):
        """Consume a response from WASM memory and parse it.

        Args:
            addr: Pointer to response in WASM memory
            message_type: Protobuf message type to parse

        Returns:
            Parsed protobuf message

        Raises:
            Exception: If response contains an error
        """
        data = self._consume(addr)
        response = messages_pb2.Response()
        response.ParseFromString(data)

        if response.HasField("error"):
            raise Exception(f"WASM error: {response.error}")

        # Parse the data field into the expected message type
        result = message_type()
        result.ParseFromString(response.data)
        return result

    def _consume(self, addr: int) -> bytes:
        """Read data from WASM memory and free it.

        Args:
            addr: Pointer to data in WASM memory

        Returns:
            Data bytes read from memory
        """
        # Read length (4-byte prefix at addr-4)
        len_bytes = self.memory.read(self.store, addr - 4, addr)
        total_len = int.from_bytes(len_bytes, byteorder="little")
        length = total_len - 4

        # Read data
        data = self.memory.read(self.store, addr, addr + length)

        # Free memory
        self.wasm_msg_free(self.store, addr)

        # Need to make a defensive copy since data might be a view
        return bytes(data)


class WasmResolver:
    """Safe WASM resolver with error recovery and instance reload."""

    def __init__(self, wasm_path: Path):
        """Initialize the WASM resolver.

        Args:
            wasm_path: Path to the WASM binary file
        """
        self.wasm_bytes = wasm_path.read_bytes()

        # Create WASM engine with configuration
        config = Config()
        self.engine = Engine(config)

        # Compile module (reusable)
        self.module = Module(self.engine, self.wasm_bytes)

        # Current state for recovery
        self._current_state: Optional[api_pb2.SetResolverStateRequest] = None

        # Buffered logs from failed instances
        self._buffered_logs: list[bytes] = []

        # Create initial delegate
        self._delegate = self._create_delegate()

    def _create_delegate(self) -> UnsafeWasmResolver:
        """Create a new WASM delegate instance.

        Returns:
            New UnsafeWasmResolver instance
        """
        store = Store(self.engine)
        return UnsafeWasmResolver(self.module, store)

    def _reload_instance(self, error: Exception) -> None:
        """Reload the WASM instance after an error.

        Args:
            error: The error that caused the reload
        """
        logger.error(f"Failure calling into WASM, reloading instance: {error}")

        # Try to flush logs from the failed instance
        try:
            self._buffered_logs.append(self._delegate.flush_logs())
        except Exception:
            logger.error("Failed to flush_logs on error")

        # Create new delegate
        self._delegate = self._create_delegate()

        # Restore state if we have it
        if self._current_state is not None:
            try:
                self._delegate.set_resolver_state(self._current_state)
            except Exception as e:
                logger.error(f"Failed to restore state after reload: {e}")

    def resolve_with_sticky(
        self, request: api_pb2.ResolveWithStickyRequest
    ) -> api_pb2.ResolveWithStickyResponse:
        """Resolve flags with sticky assignments.

        Args:
            request: Resolve request with materialization context

        Returns:
            Resolve response with flag values or missing materializations

        Raises:
            Exception: If WASM returns an error (after reload attempt)
        """
        try:
            return self._delegate.resolve_with_sticky(request)
        except Exception as error:
            # Check if it's a WASM runtime error (wasmtime uses RuntimeError)
            if isinstance(error, RuntimeError):
                self._reload_instance(error)
            raise

    def set_resolver_state(self, request: api_pb2.SetResolverStateRequest) -> None:
        """Set the resolver state in the WASM module.

        Args:
            request: State update request with account ID and state bytes

        Raises:
            Exception: If WASM returns an error (after reload attempt)
        """
        self._current_state = request
        try:
            self._delegate.set_resolver_state(request)
        except Exception as error:
            if isinstance(error, RuntimeError):
                self._reload_instance(error)
            raise

    def flush_logs(self) -> bytes:
        """Flush accumulated logs from WASM.

        Returns:
            Serialized WriteFlagLogsRequest as bytes

        Raises:
            Exception: If WASM returns an error (after reload attempt)
        """
        try:
            # Flush current delegate's logs
            self._buffered_logs.append(self._delegate.flush_logs())

            # Concatenate all buffered logs
            total_len = sum(len(chunk) for chunk in self._buffered_logs)
            buffer = bytearray(total_len)
            offset = 0
            for chunk in self._buffered_logs:
                buffer[offset : offset + len(chunk)] = chunk
                offset += len(chunk)

            # Clear buffer
            self._buffered_logs.clear()

            return bytes(buffer)
        except Exception as error:
            if isinstance(error, RuntimeError):
                self._reload_instance(error)
            raise
