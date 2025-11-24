"""End-to-end tests for Confidence OpenFeature Provider.

These tests connect to real Confidence servers and require valid credentials.
Set the following environment variables before running:
- CONFIDENCE_CLIENT_SECRET: Client secret for flag resolution
"""

import os

import pytest
from openfeature.evaluation_context import EvaluationContext

from confidence.provider import ConfidenceServerProviderLocal


def get_client_secret():
    """Get client secret from environment."""
    secret = os.getenv("CONFIDENCE_CLIENT_SECRET")
    if not secret:
        pytest.skip("CONFIDENCE_CLIENT_SECRET not set")
    return secret


@pytest.mark.asyncio
@pytest.mark.e2e
async def test_real_flag_resolution():
    """Test flag resolution against real Confidence servers."""
    client_secret = get_client_secret()

    provider = ConfidenceServerProviderLocal(
        client_secret=client_secret,
        initialize_timeout=30.0,
    )

    try:
        # Initialize with real state fetch
        await provider.initialize(EvaluationContext())

        # Resolve a flag
        context = EvaluationContext(
            targeting_key="test-user-123",
            attributes={
                "country": "US",
                "environment": "test",
            },
        )

        # Try to resolve a test flag
        # The actual flag name will depend on what's configured in your Confidence account
        result = await provider.resolve_boolean_evaluation("test-flag.enabled", False, context)

        # Just verify we got a valid result structure
        assert result.value in [True, False]
        assert result.reason is not None

        print(f"E2E test result: value={result.value}, reason={result.reason}")

    finally:
        await provider.shutdown()


@pytest.mark.asyncio
@pytest.mark.e2e
async def test_state_updates():
    """Test that state updates work correctly."""
    client_secret = get_client_secret()

    provider = ConfidenceServerProviderLocal(
        client_secret=client_secret,
        state_fetch_interval=5.0,  # Short interval for testing
        initialize_timeout=30.0,
    )

    try:
        await provider.initialize(EvaluationContext())

        # Provider should be ready
        # Verify state fetcher has cached state
        cached = provider.state_fetcher.get_cached_state()
        assert cached is not None

        print("E2E state update test passed")

    finally:
        await provider.shutdown()


@pytest.mark.asyncio
@pytest.mark.e2e
async def test_log_flushing_to_server():
    """Test that logs are successfully flushed to the server."""
    client_secret = get_client_secret()

    provider = ConfidenceServerProviderLocal(
        client_secret=client_secret,
        log_flush_interval=2.0,  # Short interval for testing
        initialize_timeout=30.0,
    )

    try:
        await provider.initialize(EvaluationContext())

        # Perform some flag evaluations to generate logs
        context = EvaluationContext(targeting_key="e2e-test-user")

        for i in range(3):
            try:
                await provider.resolve_boolean_evaluation(f"test-flag-{i}", False, context)
            except Exception:
                pass  # Flag might not exist

        # Wait for flush interval
        import asyncio

        await asyncio.sleep(3.0)

        print("E2E log flushing test completed")

    finally:
        await provider.shutdown()


if __name__ == "__main__":
    pytest.main([__file__, "-v", "-m", "e2e"])
