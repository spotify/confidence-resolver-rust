import fs from 'node:fs/promises'
import { fileURLToPath } from 'node:url';
import { ConfidenceServerProviderLocal, ProviderOptions } from './ConfidenceServerProviderLocal';
import { WasmResolver } from './WasmResolver';

const wasmUrl = new URL('confidence_resolver.wasm', import.meta.url);
const buffer = await fs.readFile(fileURLToPath(wasmUrl));

const module = await WebAssembly.compile(buffer as BufferSource);
const resolver = await WasmResolver.load(module);

export function createConfidenceServerProvider(options:ProviderOptions):ConfidenceServerProviderLocal {
  return new ConfidenceServerProviderLocal(resolver, options)
}