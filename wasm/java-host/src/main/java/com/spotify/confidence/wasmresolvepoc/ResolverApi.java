package com.spotify.confidence.wasmresolvepoc;

import com.dylibso.chicory.compiler.MachineFactoryCompiler;
import com.dylibso.chicory.runtime.ExportFunction;
import com.dylibso.chicory.runtime.ImportFunction;
import com.dylibso.chicory.runtime.ImportValues;
import com.dylibso.chicory.runtime.Instance;
import com.dylibso.chicory.runtime.Memory;
import com.dylibso.chicory.wasm.WasmModule;
import com.dylibso.chicory.wasm.types.FunctionType;
import com.dylibso.chicory.wasm.types.ValType;
import com.google.protobuf.ByteString;
import com.google.protobuf.GeneratedMessage;
import com.google.protobuf.InvalidProtocolBufferException;
import com.google.protobuf.Timestamp;
import com.spotify.confidence.flags.resolver.v1.ResolveFlagsRequest;
import com.spotify.confidence.flags.resolver.v1.ResolveFlagsResponse;
import com.spotify.confidence.flags.resolver.v1.ResolvedFlag;

import rust_guest.Messages.SetResolverStateRequest;
import rust_guest.Messages;
import rust_guest.Messages.ResolveSimpleRequest;
import rust_guest.Types;

import java.util.List;
import java.util.function.Function;

public class ResolverApi {

  private static final FunctionType HOST_FN_TYPE = FunctionType.of(List.of(ValType.I32), List.of(ValType.I32));
  private final Instance instance;

  // interop
  private final ExportFunction wasmMsgAlloc;
  private final ExportFunction wasmMsgFree;

  // api
  private final ExportFunction wasmMsgGuestSetResolverState;
  private final ExportFunction wasmMsgGuestResolve;
  private final ExportFunction wasmMsgGuestResolveSimple;

  public ResolverApi(WasmModule module) {

    instance = Instance.builder(module)
            .withImportValues(ImportValues.builder()
                    .addFunction(createImportFunction("current_time", Messages.Void::parseFrom, this::currentTime))
                    .addFunction(createImportFunction("log_resolve", Types.LogResolveRequest::parseFrom, this::logResolve))
                    .addFunction(createImportFunction("log_assign", Types.LogAssignRequest::parseFrom, this::logAssign))
                    .addFunction(new ImportFunction("wasm_msg", "wasm_msg_current_thread_id", FunctionType.of(List.of(), List.of(ValType.I32)), (instance1, args) -> new long[]{0}))
                    .build())
            .withMachineFactory(MachineFactoryCompiler::compile)
            .build();
    wasmMsgAlloc = instance.export("wasm_msg_alloc");
    wasmMsgFree = instance.export("wasm_msg_free");
    wasmMsgGuestSetResolverState = instance.export("wasm_msg_guest_set_resolver_state");
    wasmMsgGuestResolve = instance.export("wasm_msg_guest_resolve");
    wasmMsgGuestResolveSimple = instance.export("wasm_msg_guest_resolve_simple");
  }

  private GeneratedMessage logAssign(Types.LogAssignRequest logAssignRequest) {
    System.out.println("logAssign");
    return Messages.Void.getDefaultInstance();
  }

  private GeneratedMessage logResolve(Types.LogResolveRequest logResolveRequest) {
    return Messages.Void.getDefaultInstance();
  }

  private Timestamp currentTime(Messages.Void unused) {
    return Timestamp.getDefaultInstance();
  }

  public void setResolverState(SetResolverStateRequest state) {
    final byte[] request = Messages.Request.newBuilder()
            .setData(state.toByteString())
            .build().toByteArray();
    int addr = transfer(request);
    int respPtr = (int) wasmMsgGuestSetResolverState.apply(addr)[0];
    consumeResponse(respPtr, Messages.Void::parseFrom);
  }

  public ResolveFlagsResponse resolve(ResolveFlagsRequest request) {
    int reqPtr = transferRequest(request);
    int respPtr = (int) wasmMsgGuestResolve.apply(reqPtr)[0];
    return consumeResponse(respPtr, ResolveFlagsResponse::parseFrom);
  }

  public ResolvedFlag resolve_simple(ResolveSimpleRequest request) {
    int reqPtr = transferRequest(request);
    int respPtr = (int) wasmMsgGuestResolveSimple.apply(reqPtr)[0];
    return consumeResponse(respPtr, ResolvedFlag::parseFrom);
  }

  private <T extends GeneratedMessage> T consumeResponse(int addr, ParserFn<T> codec) {
      try {
          Messages.Response response = Messages.Response.parseFrom(consume(addr));
          if(response.hasError()) {
            throw new RuntimeException(response.getError());
          } else {
            return codec.apply(response.getData().toByteArray());
          }
      } catch (InvalidProtocolBufferException e) {
          throw new RuntimeException(e);
      }
  }

  private <T extends GeneratedMessage> T consumeRequest(int addr, ParserFn<T> codec) {
    try {
      Messages.Request request = Messages.Request.parseFrom(consume(addr));
      return codec.apply(request.getData().toByteArray());
    } catch (InvalidProtocolBufferException e) {
      throw new RuntimeException(e);
    }
  }

  private int transferRequest(GeneratedMessage message) {
    final byte[] request = Messages.Request.newBuilder()
            .setData(message.toByteString())
            .build().toByteArray();
    return transfer(request);
  }

  private int transferResponseSuccess(GeneratedMessage response) {
    final byte[] wrapperBytes = Messages.Response.newBuilder()
            .setData(response.toByteString())
            .build().toByteArray();
    return transfer(wrapperBytes);
  }

  private int transferResponseError(String error) {
    final byte[] wrapperBytes = Messages.Response.newBuilder()
            .setError(error)
            .build().toByteArray();
    return transfer(wrapperBytes);
  }

  private byte[] consume(int addr) {
    final Memory mem = instance.memory();
    final int len = (int) (mem.readU32(addr - 4) - 4L);
    final byte[] data = mem.readBytes(addr, len);
    wasmMsgFree.apply(addr);
    return data;
  }

  private int transfer(byte[] data) {
    final Memory mem = instance.memory();
    int addr = (int) wasmMsgAlloc.apply(data.length)[0];
    mem.write(addr, data);
    return addr;
  }

  private <T extends GeneratedMessage> ImportFunction createImportFunction(String name, ParserFn<T> reqCodec, Function<T, GeneratedMessage> impl) {
    return new ImportFunction("wasm_msg", "wasm_msg_host_" + name, HOST_FN_TYPE, (instance1, args) -> {
        try {
            final T message = consumeRequest((int) args[0], reqCodec);
            final GeneratedMessage response = impl.apply(message);
            return new long[]{transferResponseSuccess(response)};
        } catch (Exception e) {
            return new long[]{transferResponseError(e.getMessage())};
        }
    });
  }

  private interface ParserFn<T> {

    T apply(byte[] data) throws InvalidProtocolBufferException;
  }

}
