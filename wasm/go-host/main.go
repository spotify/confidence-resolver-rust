package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/spotify/confidence/wasm-resolve-poc/go-host/proto/resolver"

	"github.com/tetratelabs/wazero"
	"google.golang.org/protobuf/types/known/structpb"
)

func main() {
	// Load the WASM module
	wasmPath := filepath.Join("..", "..", "target", "wasm32-unknown-unknown", "release", "rust_guest.wasm")
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		log.Fatalf("Failed to read WASM file: %v", err)
	}

	// Load resolver state
	resolverStatePath := filepath.Join("..", "resolver_state.pb")
	resolverState, err := os.ReadFile(resolverStatePath)
	if err != nil {
		log.Fatalf("Failed to read resolver state: %v", err)
	}

	// Create WASM runtime
	ctx := context.Background()
	runtime := wazero.NewRuntime(ctx)
	defer runtime.Close(ctx)

	// Create resolver API
	api := NewResolverApi(ctx, runtime, wasmBytes)
	defer api.Close(ctx)

	// Set resolver state
	err = api.SetResolverState(resolverState)
	if err != nil {
		log.Fatalf("Failed to set resolver state: %v", err)
	}

	// Verify tutorial-feature resolves with RESOLVE_REASON_MATCH and has a variant (not default)
	{
		evalContext, err := structpb.NewStruct(map[string]interface{}{
			"targeting_key": "tutorial_visitor",
			"visitor_id":    "tutorial_visitor",
		})
		if err != nil {
			log.Fatalf("Failed to create evaluation context: %v", err)
		}
		req := &resolver.ResolveFlagsRequest{
			ClientSecret:      "mkjJruAATQWjeY7foFIWfVAcBWnci2YF",
			Apply:             false,
			EvaluationContext: evalContext,
			Flags:             []string{"flags/tutorial-feature"},
		}
		resp, err := api.Resolve(req)
		if err != nil {
			log.Fatalf("Resolve failed: %v", err)
		}
		if resp == nil || len(resp.ResolvedFlags) == 0 {
			log.Fatalf("No flags resolved for tutorial-feature")
		}
		rf := resp.ResolvedFlags[0]
		if rf.Reason != resolver.ResolveReason_RESOLVE_REASON_MATCH {
			log.Fatalf("Expected reason RESOLVE_REASON_MATCH, got %v", rf.Reason)
		}
		if rf.Variant == "" {
			log.Fatalf("Expected a non-empty variant for tutorial-feature")
		}
		// Extract the string title value
		title := ""
		if rf.Value != nil {
			m := rf.Value.AsMap()
			if v, ok := m["title"].(string); ok {
				title = v
			} else if v, ok := m["value"].(string); ok {
				title = v
			} else {
				for _, anyVal := range m {
					if s, ok := anyVal.(string); ok {
						title = s
						break
					}
				}
			}
		}
		fmt.Printf("tutorial-feature verified: reason=RESOLVE_REASON_MATCH variant=%s title=%s\n", rf.Variant, title)
	}

}
