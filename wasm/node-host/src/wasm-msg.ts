import { BinaryWriter } from "@bufbuild/protobuf/wire";
import { Request, Response } from "./message.ts";

type Codec<T> = {
  encode(message: T): BinaryWriter;
  decode(input: Uint8Array): T;
}

type FunctionDef<Req, Res, Async extends boolean> = 
  Async extends true ? (req: Req) => Promise<Res> : (req: Req) => Res;


type ApiMethod<N extends string, Req, Res, Async extends boolean, T = {}> = Id<T & {
  [K in N]: FunctionDef<Req, Res, Async>
}>

type Id<T> = {} & { [K in keyof T]: T[K] };

type MessageFn = (req:number) => number

const PREFIX = 'wasm_msg_'

interface ModuleExports {
  [key:`wasm_msg_guest_${string}`]: MessageFn;
  memory: WebAssembly.Memory;
  wasm_msg_alloc: (size:number) => number;
  wasm_msg_free: (ptr:number) => void;
}

interface ModuleImports {
  
  memory?: WebAssembly.Memory;
  wasm_msg: {
    [key:`wasm_msg_${string}`]: MessageFn;
  }
}

export class ApiBuilder<T = {}> {

  private readonly imports: ModuleImports = { wasm_msg: {
    wasm_msg_current_thread_id: () => 0,
  } };
  private exports: ModuleExports | null = null;
  private readonly api:any = {};

  // memory(initial: number):ApiBuilder<T> {
  //   this.imports.memory = new WebAssembly.Memory({ initial });
  //   return this;
  // }

  guest<N extends string, Req, Res, Async extends boolean>(name: N, reqCodec:Codec<Req>, resCodec:Codec<Res>, async: Async): ApiBuilder<ApiMethod<N, Req, Res, Async, T>> {
    this.api[name] = (req:Req) => {
      const reqPtr = this.transferRequest(req, reqCodec);
      const resPtr = this.exports!['wasm_msg_guest_' + name](reqPtr);
      return this.consumeResponse(resPtr, resCodec);
    }

    return this as any;
  }

  guestRaw<N extends string>(name: N): ApiBuilder<ApiMethod<N, Uint8Array, Uint8Array, false, T>> {
    this.api[name] = (req:Uint8Array) => {
      const reqPtr = this.transfer({ data: req }, Request);
      const resPtr = this.exports!['wasm_msg_guest_' + name](reqPtr);
      const { data, error} = this.consume(resPtr, Response);
      if (error) {
        throw new Error(error);
      }
      return data!;
    }
    return this as any;
  }

  host<N extends string, Req, Res, Async extends boolean>(name: N, reqCodec:Codec<Req>, resCodec:Codec<Res>, async: Async, impl: FunctionDef<Req, Res, Async>): ApiBuilder<T> {
    if (async) {
      throw new Error('Async is not supported')
    }
    this.imports.wasm_msg['wasm_msg_host_' + name] = (reqPtr:number) => {
      const req = this.consumeRequest(reqPtr, reqCodec);
      try {
        const res = impl(req);      
        return this.transferResponseSuccess(res, resCodec);
      } catch (error) {
        return this.transferResponseError(error instanceof Error ? error.message : String(error));
      }
    };
    return this;
  }

  private transferRequest<T>(value:T, codec:Codec<T>): number {
    const data = codec.encode(value).finish();
    return this.transfer({ data }, Request);
  }

  private transferResponseSuccess<T>(value:T, codec:Codec<T>): number {
    const data = codec.encode(value).finish();
    return this.transfer({ data }, Response);
  }
  private transferResponseError<T>(error:string): number {
    return this.transfer({ error }, Response);
  }

  private consumeRequest<T>(ptr:number, codec:Codec<T>): T {
    const req:Request = this.consume(ptr, Request);
    return codec.decode(req.data);
  }

  private consumeResponse<T>(ptr:number, codec:Codec<T>): T {
    const { data, error }:Response = this.consume(ptr, Response);
    if (error) {
      throw new Error(error);
    }
    return codec.decode(data!);
  }

  private transfer<T>(data:T, codec:Codec<T>): number {
    const encoded = codec.encode(data).finish();
    const ptr = this.exports!.wasm_msg_alloc(encoded.length);
    this.viewBuffer(ptr).set(encoded);
    return ptr;
  }

  private consume<T>(ptr:number, codec:Codec<T>): T {
    const data = this.viewBuffer(ptr);
    const res = codec.decode(data);
    this.free(ptr);
    return res;
  }

  private viewBuffer(ptr:number): Uint8Array {
    const size = new DataView(this.exports!.memory.buffer).getUint32(ptr - 4, true);
    const data = new Uint8Array(this.exports!.memory.buffer, ptr, size - 4);
    return data;
  }

  private free(ptr:number) {
    this.exports!.wasm_msg_free(ptr);
  }

  build(wasmModule: WebAssembly.Module): T {
    const instance = new WebAssembly.Instance(wasmModule, this.imports as any);
    this.exports = instance.exports as ModuleExports;
    return this.api as T;
  }
}

