"""Flag logger for sending flag evaluation logs to Confidence."""

import logging

import httpx

logger = logging.getLogger(__name__)


class FlagLogger:
    """Sends flag evaluation logs to Confidence backend."""

    WRITE_FLAG_LOGS_URL = "https://resolver.confidence.dev/v1/flagLogs:write"

    def __init__(self, timeout: float = 5.0):
        """Initialize the flag logger.

        Args:
            timeout: HTTP request timeout in seconds
        """
        self.timeout = timeout
        self._http_client = httpx.AsyncClient(timeout=timeout)

    async def close(self) -> None:
        """Close the HTTP client."""
        await self._http_client.aclose()

    async def write_logs(self, log_data: bytes) -> None:
        """Write flag logs to the backend.

        Args:
            log_data: Serialized WriteFlagLogsRequest protobuf bytes

        Raises:
            httpx.HTTPError: If the HTTP request fails
        """
        if not log_data or len(log_data) == 0:
            logger.debug("No logs to send, skipping flush")
            return

        try:
            response = await self._http_client.post(
                self.WRITE_FLAG_LOGS_URL,
                content=log_data,
                headers={"Content-Type": "application/x-protobuf"},
            )
            response.raise_for_status()
            logger.debug(f"Successfully wrote {len(log_data)} bytes of logs")

        except httpx.HTTPError as e:
            logger.error(f"Failed to write flag logs: {e}")
            # We don't re-raise here as log writing failures should not break the provider
            # Logs will be accumulated and retried on next flush
