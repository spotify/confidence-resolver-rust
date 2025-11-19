# Confidence OpenFeature Provider Demo (Node.js/TypeScript)

Demo application showing how to use the Confidence OpenFeature Local Provider in a Node.js environment.

## Prerequisites

- Node.js 18+
- Yarn (recommended) or NPM
- Confidence API credentials

## Setup

The demo resides within the main provider package. You need to install dependencies and build the project (to generate the WASM module) before running the demo.

1. **Install Dependencies and Build**

   ```bash
   # From openfeature-provider/js directory
   yarn install
   yarn build
   ```

2. **Set Environment Variables**

   Set the required environment variables:

   ```bash
   export CONFIDENCE_API_CLIENT_ID="your-api-client-id"
   export CONFIDENCE_API_CLIENT_SECRET="your-api-client-secret"
   export CONFIDENCE_CLIENT_SECRET="your-client-secret"
   ```

   Get your credentials from the [Confidence dashboard](https://confidence.spotify.com/).

## Run

Run the demo using `tsx`:

```bash
# From openfeature-provider/js directory
npx tsx demo/index.ts
```

The demo loads the compiled WASM resolver and runs concurrent flag evaluations to test performance.



