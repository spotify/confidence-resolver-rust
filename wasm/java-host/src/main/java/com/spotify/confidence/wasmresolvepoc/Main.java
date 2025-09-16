package com.spotify.confidence.wasmresolvepoc;

import com.dylibso.chicory.wasm.Parser;
import com.dylibso.chicory.wasm.WasmModule;
import com.google.protobuf.ByteString;
import com.google.protobuf.Struct;
import com.google.protobuf.Value;
import com.spotify.confidence.flags.resolver.v1.ResolveFlagsRequest;
import com.spotify.confidence.flags.resolver.v1.ResolveFlagsResponse;
import com.spotify.confidence.flags.resolver.v1.ResolveReason;
import com.spotify.confidence.flags.resolver.v1.SetResolverStateRequest;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
 

public class Main {

    private final ResolverApi resolverApi;

    public Main() {
        Path wasmModulePath = Path.of("../confidence_resolver.wasm");
        WasmModule module = Parser.parse(wasmModulePath);
        resolverApi = new ResolverApi(module);

    }

    

    public static void main(String[] args) throws IOException {
        Main main = new Main();
        Path resolveStatePath = Path.of("../resolver_state.pb");
        byte[] resolveState = Files.readAllBytes(resolveStatePath);

        main.resolverApi
                .setResolverState(SetResolverStateRequest.newBuilder()
                        .setState(ByteString.copyFrom(resolveState)).setAccountId("confidence-demo-june").build());

        // Verify RESOLVE_REASON_MATCH reason and non-empty variant for tutorial_visitor
        final ResolveFlagsResponse verifyResp = main.resolverApi.resolve(
                ResolveFlagsRequest.newBuilder()
                        .setClientSecret("mkjJruAATQWjeY7foFIWfVAcBWnci2YF")
                        .setApply(false)
                        .setEvaluationContext(Struct.newBuilder()
                                .putFields("visitor_id", Value.newBuilder().setStringValue("tutorial_visitor").build())
                                .build())
                        .addFlags("flags/tutorial-feature")
                        .build()
        );
        if (verifyResp.getResolvedFlagsCount() == 0) {
            throw new RuntimeException("No flags resolved for tutorial-feature");
        }
        final var rf = verifyResp.getResolvedFlags(0);
        if (rf.getReason() != ResolveReason.RESOLVE_REASON_MATCH) {
            throw new RuntimeException("Expected reason RESOLVE_REASON_MATCH, got " + rf.getReason());
        }
        if (rf.getVariant().isEmpty()) {
            throw new RuntimeException("Expected non-empty variant for tutorial-feature");
        }
        // Extract string title value
        String titleVal = null;
        final var value = rf.getValue();
        if (value.containsFields("title") && value.getFieldsOrThrow("title").hasStringValue()) {
            titleVal = value.getFieldsOrThrow("title").getStringValue();
        } else if (value.containsFields("value") && value.getFieldsOrThrow("value").hasStringValue()) {
            titleVal = value.getFieldsOrThrow("value").getStringValue();
        } else {
            for (var entry : value.getFieldsMap().entrySet()) {
                if (entry.getValue().hasStringValue()) {
                    titleVal = entry.getValue().getStringValue();
                    break;
                }
            }
        }
        System.out.println("tutorial-feature verified: reason=RESOLVE_REASON_MATCH variant=" + rf.getVariant() + " title=" + titleVal);

        // Done: single flag verified above
    }
} 