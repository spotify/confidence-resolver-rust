# Confidence OpenFeature Provider Demo

Demo application showing how to use the Confidence OpenFeature Local Provider in Go.

## Prerequisites

- Go 1.24+
- Confidence API credentials

## Setup

Set the required environment variables:

```bash
export CONFIDENCE_API_CLIENT_ID="your-api-client-id"
export CONFIDENCE_API_CLIENT_SECRET="your-api-client-secret"
export CONFIDENCE_CLIENT_SECRET="your-client-secret"
```

Get your credentials from the [Confidence dashboard](https://confidence.spotify.com/).

## Run

```bash
go run main.go
```

The demo runs concurrent flag evaluations to test performance and state synchronization.
