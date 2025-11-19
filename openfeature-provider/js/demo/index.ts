import { ConfidenceServerProviderLocal } from '../src/ConfidenceServerProviderLocal';
import { WasmResolver } from '../src/WasmResolver';
import { OpenFeature, EvaluationContext } from '@openfeature/server-sdk';
import { readFile } from 'node:fs/promises';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

// Polyfill globals
(globalThis as any).SDK_VERSION = "0.1.1-demo";
(globalThis as any).__ASSERT__ = false;
(globalThis as any).__TEST__ = false;

const __dirname = dirname(fileURLToPath(import.meta.url));

async function main() {
  console.log("Starting Confidence OpenFeature Local Provider JS Demo");

  const apiClientId = process.env.CONFIDENCE_API_CLIENT_ID;
  const apiClientSecret = process.env.CONFIDENCE_API_CLIENT_SECRET;
  const clientSecret = process.env.CONFIDENCE_CLIENT_SECRET;

  if (!apiClientId || !apiClientSecret || !clientSecret) {
    console.error("ERROR: Please set environment variables: CONFIDENCE_API_CLIENT_ID, CONFIDENCE_API_CLIENT_SECRET, CONFIDENCE_CLIENT_SECRET");
    process.exit(1);
  }

  // Load WASM
  const wasmPath = resolve(__dirname, '../dist/confidence_resolver.wasm');
  console.log(`Loading WASM from ${wasmPath}`);
  const buffer = await readFile(wasmPath);
  const module = await WebAssembly.compile(buffer);
  const resolver = new WasmResolver(module);

  process.env.DEBUG_CONFIDENCE = 'true';
  const provider = new ConfidenceServerProviderLocal(resolver, {
    apiClientId,
    apiClientSecret,
    flagClientSecret: clientSecret,
    flushInterval: 5000, // 5s flush for demo
  });

  await OpenFeature.setProviderAndWait(provider);
  console.log("OpenFeature provider registered");

  const client = OpenFeature.getClient("demo-app");

  console.log("=== Flag Evaluation Demo ===");
  const durationSeconds = 30;
  const endTime = Date.now() + durationSeconds * 1000;
  let totalResolves = 0;
  let errorCount = 0;

  // Simple loop to generate load
  const loop = async () => {
    while (Date.now() < endTime) {
      try {
        const ctx: EvaluationContext = {
          targeting_key: "user-123",
          user_id: "vahid",
          visitor_id: "vahid"
        };

        const value = await client.getStringValue("mattias-boolean-flag", "default", ctx);
        if (value) {
          totalResolves++;
        } else {
          errorCount++;
        }
      } catch (e) {
        errorCount++;
        console.error(e);
      }
      // Yield to allow event loop to process other things (like flush)
      await new Promise(resolve => setTimeout(resolve, 0));
    }
  };

  // Run 10 concurrent loops
  const loops = Array(10).fill(null).map(() => loop());
  await Promise.all(loops);

  console.log("Demo completed:");
  console.log(`Total resolves: ${totalResolves}`);
  console.log(`Errors: ${errorCount}`);
  console.log(`Approx RPS: ${totalResolves / durationSeconds}`);

  console.log("Waiting for logs to flush...");
  await new Promise(resolve => setTimeout(resolve, 5000));

  await OpenFeature.close();
}

main().catch(console.error);

