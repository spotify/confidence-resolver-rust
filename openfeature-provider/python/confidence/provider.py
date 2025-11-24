"""Confidence OpenFeature Provider with local resolve capabilities."""

import asyncio
import logging
from pathlib import Path
from typing import Any, Optional, Union

from google.protobuf.struct_pb2 import Struct
from openfeature.evaluation_context import EvaluationContext
from openfeature.exception import (
    ErrorCode,
    FlagNotFoundError,
    GeneralError,
    ParseError,
    TypeMismatchError,
)
from openfeature.flag_evaluation import FlagResolutionDetails, Reason
from openfeature.provider import AbstractProvider, Metadata, ProviderStatus

from confidence.flag_logger import FlagLogger
from confidence.proto import api_pb2
from confidence.state_fetcher import StateFetcher
from confidence.wasm_resolver import WasmResolver

logger = logging.getLogger(__name__)

# Default intervals
DEFAULT_STATE_INTERVAL = 30.0  # seconds
DEFAULT_FLUSH_INTERVAL = 10.0  # seconds
DEFAULT_INITIALIZE_TIMEOUT = 30.0  # seconds


class ConfidenceServerProviderLocal(AbstractProvider):
    """OpenFeature Provider for Confidence with local flag resolution.

    This provider uses a WASM-based resolver for local flag evaluation,
    with periodic state updates and log flushing.
    """

    def __init__(
        self,
        client_secret: str,
        *,
        state_fetch_interval: float = DEFAULT_STATE_INTERVAL,
        log_flush_interval: float = DEFAULT_FLUSH_INTERVAL,
        initialize_timeout: float = DEFAULT_INITIALIZE_TIMEOUT,
    ):
        """Initialize the Confidence provider.

        Args:
            client_secret: Client secret for authentication
            state_fetch_interval: Interval for state fetching in seconds
            log_flush_interval: Interval for log flushing in seconds
            initialize_timeout: Timeout for initialization in seconds
        """
        self.client_secret = client_secret
        self.state_fetch_interval = state_fetch_interval
        self.log_flush_interval = log_flush_interval
        self.initialize_timeout = initialize_timeout

        # Initialize components with packaged WASM
        wasm_path = Path(__file__).parent / "wasm" / "confidence_resolver.wasm"
        self.resolver = WasmResolver(wasm_path)
        self.state_fetcher = StateFetcher(client_secret)
        self.flag_logger = FlagLogger()

        # Metadata
        self._metadata = Metadata(name="ConfidenceServerProviderLocal")

        # Status tracking
        self._status = ProviderStatus.NOT_READY

        # Background tasks
        self._state_task: Optional[asyncio.Task] = None
        self._flush_task: Optional[asyncio.Task] = None
        self._shutdown_event = asyncio.Event()

    def get_metadata(self) -> Metadata:
        """Get provider metadata."""
        return self._metadata

    def get_provider_hooks(self) -> list:
        """Get provider hooks."""
        return []

    async def initialize(self, evaluation_context: EvaluationContext) -> None:
        """Initialize the provider.

        This performs the initial state fetch and starts background tasks
        for periodic state updates and log flushing.

        Args:
            evaluation_context: Initial evaluation context (unused)

        Raises:
            GeneralError: If initialization fails
        """
        try:
            # Fetch initial state with timeout
            try:
                state_bytes = await asyncio.wait_for(
                    self.state_fetcher.fetch_state(), timeout=self.initialize_timeout
                )

                # Parse SetResolverStateRequest from CDN response
                state_request = api_pb2.SetResolverStateRequest()
                state_request.ParseFromString(state_bytes)

                # Set resolver state
                self.resolver.set_resolver_state(state_request)

                logger.info(
                    f"Initial state loaded successfully (account: {state_request.account_id})"
                )

            except asyncio.TimeoutError:
                logger.error(f"State fetch timed out after {self.initialize_timeout}s")
                self._status = ProviderStatus.ERROR
                raise GeneralError("Failed to initialize: state fetch timeout")

            # Start background tasks
            self._state_task = asyncio.create_task(self._state_update_loop())
            self._flush_task = asyncio.create_task(self._flush_loop())

            self._status = ProviderStatus.READY
            logger.info("Provider initialized and ready")

        except Exception as e:
            self._status = ProviderStatus.ERROR
            logger.error(f"Initialization failed: {e}")
            raise GeneralError(f"Failed to initialize provider: {e}")

    async def shutdown(self) -> None:
        """Shutdown the provider.

        Performs final log flush and cleans up resources.
        """
        logger.info("Shutting down provider")

        # Signal shutdown to background tasks
        self._shutdown_event.set()

        # Cancel background tasks
        if self._state_task:
            self._state_task.cancel()
            try:
                await self._state_task
            except asyncio.CancelledError:
                pass

        if self._flush_task:
            self._flush_task.cancel()
            try:
                await self._flush_task
            except asyncio.CancelledError:
                pass

        # Final flush with short timeout
        try:
            await asyncio.wait_for(self._flush_logs(), timeout=3.0)
        except Exception as e:
            logger.warning(f"Final flush failed: {e}")

        # Close HTTP clients
        await self.state_fetcher.close()
        await self.flag_logger.close()

        logger.info("Provider shutdown complete")

    async def _state_update_loop(self) -> None:
        """Background task for periodic state updates."""
        while not self._shutdown_event.is_set():
            try:
                await asyncio.sleep(self.state_fetch_interval)

                # Fetch and parse state
                state_bytes = await self.state_fetcher.fetch_state()
                state_request = api_pb2.SetResolverStateRequest()
                state_request.ParseFromString(state_bytes)

                # Update resolver
                self.resolver.set_resolver_state(state_request)

                logger.debug(f"State updated successfully (account: {state_request.account_id})")

            except asyncio.CancelledError:
                break
            except Exception as e:
                logger.error(f"State update failed: {e}")
                # Continue running despite errors

    async def _flush_loop(self) -> None:
        """Background task for periodic log flushing."""
        while not self._shutdown_event.is_set():
            try:
                await asyncio.sleep(self.log_flush_interval)
                await self._flush_logs()

            except asyncio.CancelledError:
                break
            except Exception as e:
                logger.error(f"Log flush failed: {e}")
                # Continue running despite errors

    async def _flush_logs(self) -> None:
        """Flush accumulated logs to the backend."""
        try:
            log_data = self.resolver.flush_logs()
            await self.flag_logger.write_logs(log_data)
        except Exception as e:
            logger.error(f"Failed to flush logs: {e}")
            raise

    async def resolve_boolean_details(
        self,
        flag_key: str,
        default_value: bool,
        evaluation_context: Optional[EvaluationContext] = None,
    ) -> FlagResolutionDetails[bool]:
        """Resolve a boolean flag value."""
        result = await self._evaluate(flag_key, default_value, evaluation_context)
        if not isinstance(result.value, bool):
            raise TypeMismatchError(f"Flag {flag_key} is not a boolean")
        return result

    async def resolve_string_details(
        self,
        flag_key: str,
        default_value: str,
        evaluation_context: Optional[EvaluationContext] = None,
    ) -> FlagResolutionDetails[str]:
        """Resolve a string flag value."""
        result = await self._evaluate(flag_key, default_value, evaluation_context)
        if not isinstance(result.value, str):
            raise TypeMismatchError(f"Flag {flag_key} is not a string")
        return result

    async def resolve_integer_details(
        self,
        flag_key: str,
        default_value: int,
        evaluation_context: Optional[EvaluationContext] = None,
    ) -> FlagResolutionDetails[int]:
        """Resolve an integer flag value."""
        result = await self._evaluate(flag_key, default_value, evaluation_context)
        if not isinstance(result.value, int):
            raise TypeMismatchError(f"Flag {flag_key} is not an integer")
        return result

    async def resolve_float_details(
        self,
        flag_key: str,
        default_value: float,
        evaluation_context: Optional[EvaluationContext] = None,
    ) -> FlagResolutionDetails[float]:
        """Resolve a float flag value."""
        result = await self._evaluate(flag_key, default_value, evaluation_context)
        if not isinstance(result.value, (int, float)):
            raise TypeMismatchError(f"Flag {flag_key} is not a number")
        return FlagResolutionDetails(
            value=float(result.value),
            reason=result.reason,
            variant=result.variant,
            error_code=result.error_code,
            error_message=result.error_message,
            flag_metadata=result.flag_metadata,
        )

    async def resolve_object_details(
        self,
        flag_key: str,
        default_value: Union[dict, list],
        evaluation_context: Optional[EvaluationContext] = None,
    ) -> FlagResolutionDetails[Union[dict, list]]:
        """Resolve an object/dict flag value."""
        return await self._evaluate(flag_key, default_value, evaluation_context)

    async def _evaluate(
        self, flag_key: str, default_value: Any, evaluation_context: Optional[EvaluationContext]
    ) -> FlagResolutionDetails[Any]:
        """Internal evaluation method.

        Args:
            flag_key: Flag key (may include path like "flag.field")
            default_value: Default value if flag not found
            evaluation_context: Evaluation context

        Returns:
            Resolution details with flag value

        Raises:
            GeneralError: If evaluation fails
        """
        # Parse flag key (handle nested paths)
        parts = flag_key.split(".")
        flag_name = parts[0]
        path = parts[1:] if len(parts) > 1 else []

        # Build resolve request
        resolve_request = api_pb2.ResolveFlagsRequest()
        resolve_request.flags.append(flag_name)
        resolve_request.client_secret = self.client_secret
        resolve_request.apply = True

        # Convert evaluation context to protobuf Struct
        if evaluation_context:
            context_struct = Struct()
            for key, value in evaluation_context.attributes.items():
                self._set_struct_value(context_struct.fields[key], value)
            resolve_request.evaluation_context.CopyFrom(context_struct)

        # Build sticky request (fail fast on missing materializations)
        sticky_request = api_pb2.ResolveWithStickyRequest()
        sticky_request.resolve_request.CopyFrom(resolve_request)
        sticky_request.fail_fast_on_sticky = True

        try:
            # Call WASM resolver
            response = self.resolver.resolve_with_sticky(sticky_request)

            # Check if we got missing materializations
            if response.HasField("missing_materializations"):
                logger.info(f"Missing materializations for {flag_name}, falling back to remote")
                # TODO: Implement remote resolve fallback
                raise FlagNotFoundError(f"Flag {flag_name} requires remote resolve (not implemented)")

            # Extract successful response
            if not response.HasField("success"):
                raise GeneralError(f"Unexpected response format for flag {flag_name}")

            resolve_response = response.success.response

            # Find the flag in response
            resolved_flag = None
            for flag in resolve_response.resolved_flags:
                if flag.flag == flag_name:
                    resolved_flag = flag
                    break

            if not resolved_flag:
                raise FlagNotFoundError(f"Flag {flag_name} not found")

            # Extract value from nested path
            value = self._extract_value(resolved_flag.value, path)

            # Convert resolve reason to OpenFeature reason
            reason = self._convert_reason(resolved_flag.reason)

            return FlagResolutionDetails(
                value=value,
                reason=reason,
                variant=resolved_flag.variant,
            )

        except Exception as e:
            logger.error(f"Evaluation failed for {flag_key}: {e}")
            if isinstance(e, (FlagNotFoundError, TypeMismatchError)):
                raise
            raise GeneralError(f"Failed to evaluate flag {flag_key}: {e}")

    def _set_struct_value(self, value_field: Any, python_value: Any) -> None:
        """Set a protobuf Struct field from a Python value."""
        if python_value is None:
            value_field.null_value = 0
        elif isinstance(python_value, bool):
            value_field.bool_value = python_value
        elif isinstance(python_value, (int, float)):
            value_field.number_value = float(python_value)
        elif isinstance(python_value, str):
            value_field.string_value = python_value
        elif isinstance(python_value, dict):
            for k, v in python_value.items():
                self._set_struct_value(value_field.struct_value.fields[k], v)
        elif isinstance(python_value, list):
            for item in python_value:
                list_value = value_field.list_value.values.add()
                self._set_struct_value(list_value, item)

    def _extract_value(self, struct_value: Struct, path: list[str]) -> Any:
        """Extract a value from a Struct following a path.

        Args:
            struct_value: Protobuf Struct containing the value
            path: List of field names to navigate

        Returns:
            The extracted value

        Raises:
            ParseError: If path navigation fails
        """
        current = self._struct_to_dict(struct_value)

        for field in path:
            if not isinstance(current, dict) or field not in current:
                raise ParseError(f"Path {'.'.join(path)} not found in flag value")
            current = current[field]

        return current

    def _struct_to_dict(self, struct: Struct) -> dict:
        """Convert a protobuf Struct to a Python dict."""
        result = {}
        for key, value in struct.fields.items():
            result[key] = self._value_to_python(value)
        return result

    def _value_to_python(self, value: Any) -> Any:
        """Convert a protobuf Value to a Python value."""
        kind = value.WhichOneof("kind")
        if kind == "null_value":
            return None
        elif kind == "bool_value":
            return value.bool_value
        elif kind == "number_value":
            num = value.number_value
            # Return int if it's a whole number
            if num == int(num):
                return int(num)
            return num
        elif kind == "string_value":
            return value.string_value
        elif kind == "struct_value":
            return self._struct_to_dict(value.struct_value)
        elif kind == "list_value":
            return [self._value_to_python(v) for v in value.list_value.values]
        return None

    def _convert_reason(self, resolve_reason: int) -> str:
        """Convert ResolveReason enum to OpenFeature Reason.

        Args:
            resolve_reason: ResolveReason enum value

        Returns:
            OpenFeature Reason string
        """
        # Map ResolveReason to OpenFeature Reason
        if resolve_reason == api_pb2.RESOLVE_REASON_MATCH:
            return Reason.TARGETING_MATCH
        elif resolve_reason == api_pb2.RESOLVE_REASON_NO_SEGMENT_MATCH:
            return Reason.DEFAULT
        elif resolve_reason == api_pb2.RESOLVE_REASON_FLAG_ARCHIVED:
            return Reason.DISABLED
        elif resolve_reason == api_pb2.RESOLVE_REASON_TARGETING_KEY_ERROR:
            return Reason.ERROR
        elif resolve_reason == api_pb2.RESOLVE_REASON_ERROR:
            return Reason.ERROR
        else:
            return Reason.UNKNOWN
