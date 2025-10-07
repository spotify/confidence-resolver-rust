package com.spotify.confidence.wasmresolvepoc;

import com.dylibso.chicory.annotations.WasmModuleInterface;
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

@WasmModuleInterface(WasmResource.absoluteFile)
public class ResolverApi implements ResolverApi_ModuleImports, ResolverApi_WasmMsg {

  private static final FunctionType HOST_FN_TYPE = FunctionType.of(List.of(ValType.I32), List.of(ValType.I32));
  private final Instance instance;
  private final ResolverApi_ModuleExports exports;

  public ResolverApi() {
    instance = Instance.builder(ConfidenceResolver.load())
            .withImportValues(this.toImportValues())
            .withMachineFactory(ConfidenceResolver::create)
            .build();
    exports = new ResolverApi_ModuleExports(instance);
  }

  private Timestamp currentTime(Messages.Void unused) {
    return Timestamp.getDefaultInstance();
  }

  public void setResolverState(SetResolverStateRequest state) {
    final byte[] request = Messages.Request.newBuilder()
            .setData(state.toByteString())
            .build().toByteArray();
    int addr = transfer(request);
    int respPtr = exports.wasmMsgGuestSetResolverState(addr);
    consumeResponse(respPtr, Messages.Void::parseFrom);
  }

  public ResolveFlagsResponse resolve(ResolveFlagsRequest request) {
    int reqPtr = transferRequest(request);
    int respPtr = exports.wasmMsgGuestResolve(reqPtr);
    return consumeResponse(respPtr, ResolveFlagsResponse::parseFrom);
  }

  public ResolvedFlag resolve_simple(ResolveSimpleRequest request) {
    int reqPtr = transferRequest(request);
    int respPtr = exports.wasmMsgGuestResolveSimple(reqPtr);
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
    exports.wasmMsgFree(addr);
    return data;
  }

  private int transfer(byte[] data) {
    final Memory mem = instance.memory();
    int addr = exports.wasmMsgAlloc(data.length);
    mem.write(addr, data);
    return addr;
  }

  @Override
  public ResolverApi_WasmMsg wasmMsg() {
    return this;
  }

  @Override
  public int wasmMsgHostCurrentTime(int arg0) {
    try {
      return transferResponseSuccess(currentTime(Messages.Void.getDefaultInstance()));
    } catch (Exception e) {
      return transferResponseError(e.getMessage());
    }
  }

  private interface ParserFn<T> {

    T apply(byte[] data) throws InvalidProtocolBufferException;
  }

}
