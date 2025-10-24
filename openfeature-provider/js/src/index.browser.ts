import { ConfidenceServerProviderLocal, ProviderOptions } from './ConfidenceServerProviderLocal';
import { WasmResolver } from './WasmResolver';

const wasmUrl = new URL('confidence_resolver.wasm', import.meta.url);

const module = await WebAssembly.compileStreaming(fetch(wasmUrl));
const resolver = new WasmResolver(module);

export function createConfidenceServerProvider(options:ProviderOptions):ConfidenceServerProviderLocal {
  return new ConfidenceServerProviderLocal(resolver, options)
}
