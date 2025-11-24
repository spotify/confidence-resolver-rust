#!/usr/bin/env python3
"""Basic usage example for Confidence OpenFeature Provider."""

import asyncio
import os

from openfeature import api
from openfeature.evaluation_context import EvaluationContext

from confidence import ConfidenceServerProviderLocal


async def main():
    """Demonstrate basic usage of the Confidence provider."""
    # Get client secret from environment
    client_secret = os.getenv("CONFIDENCE_CLIENT_SECRET")
    if not client_secret:
        print("Please set CONFIDENCE_CLIENT_SECRET environment variable")
        return

    # Create provider
    provider = ConfidenceServerProviderLocal(
        client_secret=client_secret,
        state_fetch_interval=30.0,  # Fetch state every 30 seconds
        log_flush_interval=10.0,  # Flush logs every 10 seconds
    )

    try:
        # Set provider
        await api.set_provider_async(provider)
        print("Provider initialized successfully")

        # Get client
        client = api.get_client()

        # Create evaluation context
        context = EvaluationContext(
            targeting_key="example-user-123",
            attributes={
                "country": "US",
                "environment": "development",
                "version": "1.0.0",
            },
        )

        # Evaluate boolean flag
        print("\n--- Boolean Flag Example ---")
        feature_enabled = await client.get_boolean_value(
            flag_key="tutorial-feature.enabled",
            default_value=False,
            evaluation_context=context,
        )
        print(f"tutorial-feature.enabled = {feature_enabled}")

        # Evaluate string flag
        print("\n--- String Flag Example ---")
        message = await client.get_string_value(
            flag_key="welcome-message.text",
            default_value="Welcome!",
            evaluation_context=context,
        )
        print(f"welcome-message.text = {message}")

        # Evaluate number flag
        print("\n--- Number Flag Example ---")
        timeout = await client.get_integer_value(
            flag_key="api-config.timeout",
            default_value=30,
            evaluation_context=context,
        )
        print(f"api-config.timeout = {timeout}")

        # Evaluate object flag
        print("\n--- Object Flag Example ---")
        config = await client.get_object_value(
            flag_key="feature-config",
            default_value={},
            evaluation_context=context,
        )
        print(f"feature-config = {config}")

        # Get detailed evaluation
        print("\n--- Detailed Evaluation Example ---")
        details = await client.get_boolean_details(
            flag_key="tutorial-feature.enabled",
            default_value=False,
            evaluation_context=context,
        )
        print(f"Value: {details.value}")
        print(f"Reason: {details.reason}")
        print(f"Variant: {details.variant}")

    finally:
        # Shutdown (performs final log flush)
        print("\nShutting down...")
        await api.shutdown_async()
        print("Shutdown complete")


if __name__ == "__main__":
    asyncio.run(main())
