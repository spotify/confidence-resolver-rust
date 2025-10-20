import { BinaryWriter } from '@bufbuild/protobuf/wire';
import { Request, Response, Void } from './proto/messages';
import { Timestamp } from './proto/google/protobuf/timestamp';
import { ResolveWithStickyRequest, ResolveWithStickyResponse, SetResolverStateRequest } from './proto/api';
import { LocalResolver } from './LocalResolver';

type Codec<T> = {
  encode(message: T): BinaryWriter;
  decode(input: Uint8Array): T;
};

export class WasmResolver implements LocalResolver {
  private exports: any;
  private imports: any;

  private constructor() {
    this.imports = {
      wasm_msg: {
        wasm_msg_host_current_time: () => {
          const ptr = this.transferRequest({ seconds: Date.now(), nanos: 0 }, Timestamp);
          return ptr;
        },
      },
    };
  }

  resolveWithSticky(request: ResolveWithStickyRequest): ResolveWithStickyResponse {
    const reqPtr = this.transferRequest(request, ResolveWithStickyRequest);
    const resPtr = this.exports.wasm_msg_guest_resolve_with_sticky(reqPtr);
    return this.consumeResponse(resPtr, ResolveWithStickyResponse);
  }

  setResolverState(request: SetResolverStateRequest): void {
    const reqPtr = this.transferRequest(request, SetResolverStateRequest);
    const resPtr = this.exports.wasm_msg_guest_set_resolver_state(reqPtr);
    this.consumeResponse(resPtr, Void);
  }

  flushLogs():Uint8Array {
    const resPtr = this.exports.wasm_msg_guest_flush_logs(0);
    const {data, error} = this.consume(resPtr, Response);
    if(error) {
      throw new Error(error);
    }
    return data!;
  }

  private transferRequest<T>(value: T, codec: Codec<T>): number {
    const data = codec.encode(value).finish();
    return this.transfer({ data }, Request);
  }

  private consumeResponse<T>(ptr: number, codec: Codec<T>): T {
    const { data, error }: Response = this.consume(ptr, Response);
    if (error) {
      throw new Error(error);
    }
    return codec.decode(data!);
  }

  private transfer<T>(data: T, codec: Codec<T>): number {
    const encoded = codec.encode(data).finish();
    const ptr = this.exports.wasm_msg_alloc(encoded.length);
    this.viewBuffer(ptr).set(encoded);
    return ptr;
  }

  private consume<T>(ptr: number, codec: Codec<T>): T {
    const data = this.viewBuffer(ptr);
    // we need this defensive copy cause codec.decode might returns views into the buffer
    const res = codec.decode(data.slice());
    this.free(ptr);
    return res;
  }

  private viewBuffer(ptr: number): Uint8Array {
    const size = new DataView(this.exports.memory.buffer).getUint32(ptr - 4, true);
    const data = new Uint8Array(this.exports.memory.buffer, ptr, size - 4);
    return data;
  }

  private free(ptr: number) {
    this.exports.wasm_msg_free(ptr);
  }

  static async load(module: WebAssembly.Module): Promise<WasmResolver> {
    const wasmResolver = new WasmResolver();
    const instance = await WebAssembly.instantiate(module, wasmResolver.imports);
    wasmResolver.exports = instance.exports;
    return wasmResolver;
  }
}
