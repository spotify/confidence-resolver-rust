import fs from 'node:fs/promises'
import { ConfidenceServerProviderLocal, ProviderOptions } from './ConfidenceServerProviderLocal';
import { WasmResolver } from './WasmResolver';

const wasmPath = require.resolve('./confidence_resolver.wasm');
const buffer = await fs.readFile(wasmPath);

const module = await WebAssembly.compile(buffer as BufferSource);
const resolver = await WasmResolver.load(module);

export function createConfidenceServerProvider(options:ProviderOptions):ConfidenceServerProviderLocal {
  return new ConfidenceServerProviderLocal(resolver, options)
}
