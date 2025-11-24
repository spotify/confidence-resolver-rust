"""State fetcher for downloading flag resolver state from CDN."""

import logging
from typing import Optional

import httpx

logger = logging.getLogger(__name__)


class StateFetcher:
    """Fetches and caches resolver state from the Confidence CDN."""

    def __init__(self, client_secret: str, timeout: float = 30.0):
        """Initialize the state fetcher.

        Args:
            client_secret: Client secret for authentication
            timeout: HTTP request timeout in seconds
        """
        self.client_secret = client_secret
        self.cdn_url = f"https://confidence-resolver-state-cdn.spotifycdn.com/{client_secret}"
        self.timeout = timeout

        # State caching
        self._etag: Optional[str] = None
        self._cached_state: Optional[bytes] = None

        # HTTP client
        self._http_client = httpx.AsyncClient(timeout=timeout)

    async def close(self) -> None:
        """Close the HTTP client."""
        await self._http_client.aclose()

    async def fetch_state(self) -> bytes:
        """Fetch the resolver state from CDN.

        The CDN returns raw bytes (typically a serialized protobuf).

        Returns:
            Raw state bytes from CDN

        Raises:
            httpx.HTTPError: If the HTTP request fails
        """
        headers = {}
        if self._etag:
            headers["If-None-Match"] = self._etag

        try:
            response = await self._http_client.get(self.cdn_url, headers=headers)

            # Check if content was modified
            if response.status_code == 304:
                logger.debug("State not modified (304), using cached state")
                # Return cached state if available
                if self._cached_state is not None:
                    return self._cached_state
                # If no cache, this is unexpected but try again without ETag
                logger.warning("Received 304 but no cached state, retrying without ETag")
                response = await self._http_client.get(self.cdn_url)

            response.raise_for_status()

            # Update ETag from response
            new_etag = response.headers.get("etag")
            if new_etag:
                self._etag = new_etag
                logger.debug(f"Updated ETag: {new_etag}")

            # Get raw state bytes
            state_bytes = response.content

            # Cache the state
            self._cached_state = state_bytes

            logger.info(f"Fetched state successfully (size: {len(state_bytes)} bytes)")

            return state_bytes

        except httpx.HTTPError as e:
            logger.error(f"Failed to fetch state from CDN: {e}")
            # If we have cached state, return it to maintain availability
            if self._cached_state is not None:
                logger.warning("Returning cached state due to fetch error")
                return self._cached_state
            # No cache available, re-raise the error
            raise

    def get_cached_state(self) -> Optional[bytes]:
        """Get the cached state bytes if available.

        Returns:
            Cached state bytes or None if no cache
        """
        return self._cached_state
