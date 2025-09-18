package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/spotify/confidence/wasm-resolve-poc/go-host/proto/resolver"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	messages "github.com/spotify/confidence/wasm-resolve-poc/go-host/proto"
)

// ResolverApi handles communication with the WASM module
type ResolverApi struct {
	instance api.Module
	module   wazero.CompiledModule
	runtime  wazero.Runtime

	// WASM exports
	wasmMsgAlloc                 api.Function
	wasmMsgFree                  api.Function
	wasmMsgGuestSetResolverState api.Function
	wasmMsgGuestResolve          api.Function
	wasmMsgGuestResolveSimple    api.Function
}

// NewResolverApi creates a new ResolverApi instance
func NewResolverApi(ctx context.Context, runtime wazero.Runtime, wasmBytes []byte) *ResolverApi {
	// Register host functions as a separate module
	_, err := runtime.NewHostModuleBuilder("wasm_msg").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, ptr uint32) uint32 {
			// log_resolve: ignore payload, return Void
			response := &messages.Response{Result: &messages.Response_Data{Data: mustMarshal(&messages.Void{})}}
			return transferResponse(mod, response)
		}).
		Export("wasm_msg_host_log_resolve").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, ptr uint32) uint32 {
			// log_assign: ignore payload, return Void
			response := &messages.Response{Result: &messages.Response_Data{Data: mustMarshal(&messages.Void{})}}
			return transferResponse(mod, response)
		}).
		Export("wasm_msg_host_log_assign").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, ptr uint32) uint32 {
			// Return current timestamp
			now := time.Now()
			timestamp := timestamppb.New(now)

			// Create response wrapper
			response := &messages.Response{
				Result: &messages.Response_Data{
					Data: mustMarshal(timestamp),
				},
			}

			return transferResponse(mod, response)
		}).
		Export("wasm_msg_host_current_time").
		NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module) uint32 {
			return 0
		}).
		Export("wasm_msg_current_thread_id").
		Instantiate(ctx)
	if err != nil {
		panic(fmt.Sprintf("Failed to register host functions: %v", err))
	}

	// Compile the WASM module
	module, err := runtime.CompileModule(ctx, wasmBytes)
	if err != nil {
		panic(fmt.Sprintf("Failed to compile WASM module: %v", err))
	}

	// Instantiate the module
	instance, err := runtime.InstantiateModule(ctx, module, wazero.NewModuleConfig())
	if err != nil {
		panic(fmt.Sprintf("Failed to instantiate WASM module: %v", err))
	}

	// Get exported functions
	wasmMsgAlloc := instance.ExportedFunction("wasm_msg_alloc")
	wasmMsgFree := instance.ExportedFunction("wasm_msg_free")
	wasmMsgGuestSetResolverState := instance.ExportedFunction("wasm_msg_guest_set_resolver_state")
	wasmMsgGuestResolve := instance.ExportedFunction("wasm_msg_guest_resolve")
	wasmMsgGuestResolveSimple := instance.ExportedFunction("wasm_msg_guest_resolve_simple")

	if wasmMsgAlloc == nil || wasmMsgFree == nil || wasmMsgGuestSetResolverState == nil || wasmMsgGuestResolve == nil || wasmMsgGuestResolveSimple == nil {
		panic("Required WASM exports not found")
	}

	return &ResolverApi{
		instance:                     instance,
		module:                       module,
		runtime:                      runtime,
		wasmMsgAlloc:                 wasmMsgAlloc,
		wasmMsgFree:                  wasmMsgFree,
		wasmMsgGuestSetResolverState: wasmMsgGuestSetResolverState,
		wasmMsgGuestResolve:          wasmMsgGuestResolve,
		wasmMsgGuestResolveSimple:    wasmMsgGuestResolveSimple,
	}
}

// Close closes the WASM instance and runtime
func (r *ResolverApi) Close(ctx context.Context) {
	if r.instance != nil {
		r.instance.Close(ctx)
	}
	if r.module != nil {
		r.module.Close(ctx)
	}
}

// SetResolverState sets the resolver state in the WASM module
func (r *ResolverApi) SetResolverState(state []byte, accountId string) error {
	ctx := context.Background()

	// Create ResolverStateRequest message
	resolverStateRequest := &messages.SetResolverStateRequest{
		State:     state,
		AccountId: accountId,
	}

	// Transfer request to WASM memory
	reqPtr := r.transferRequest(resolverStateRequest)

	// Call the WASM function
	results, err := r.wasmMsgGuestSetResolverState.Call(ctx, uint64(reqPtr))
	if err != nil {
		return fmt.Errorf("failed to call wasm_msg_guest_set_resolver_state: %w", err)
	}

	// Consume the response
	respPtr := uint32(results[0])
	r.consume(respPtr)

	return nil
}

// Resolve resolves flags using the WASM module
func (r *ResolverApi) Resolve(request *resolver.ResolveFlagsRequest) (*resolver.ResolveFlagsResponse, error) {
	ctx := context.Background()
	// Transfer request to WASM memory
	reqPtr := r.transferRequest(request)

	// Call the WASM function
	results, err := r.wasmMsgGuestResolve.Call(ctx, uint64(reqPtr))
	if err != nil {
		return nil, fmt.Errorf("failed to call wasm_msg_guest_resolve: %w", err)
	}

	// Consume the response
	respPtr := uint32(results[0])
	response := &resolver.ResolveFlagsResponse{}
	err = r.consumeResponse(respPtr, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

func (r *ResolverApi) ResolveSimple(request *messages.ResolveSimpleRequest) (*resolver.ResolvedFlag, error) {
	ctx := context.Background()
	reqPtr := r.transferRequest(request)

	results, err := r.wasmMsgGuestResolveSimple.Call(ctx, uint64(reqPtr))
	if err != nil {
		return nil, fmt.Errorf("failed to call wasm_msg_guest_resolve_simple: %w", err)
	}

	respPtr := uint32(results[0])

	response := &resolver.ResolvedFlag{}
	err = r.consumeResponse(respPtr, response)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// transferRequest transfers a protobuf message to WASM memory
func (r *ResolverApi) transferRequest(message proto.Message) uint32 {
	data := mustMarshal(message)
	request := &messages.Request{
		Data: data,
	}
	return r.transfer(mustMarshal(request))
}

// transferResponseSuccess transfers a successful response to WASM memory
func (r *ResolverApi) transferResponseSuccess(response proto.Message) uint32 {
	data := mustMarshal(response)
	resp := &messages.Response{
		Result: &messages.Response_Data{
			Data: data,
		},
	}
	return r.transfer(mustMarshal(resp))
}

// transferResponseError transfers an error response to WASM memory
func (r *ResolverApi) transferResponseError(error string) uint32 {
	resp := &messages.Response{
		Result: &messages.Response_Error{
			Error: error,
		},
	}
	return r.transfer(mustMarshal(resp))
}

// consumeResponse consumes a response from WASM memory
func (r *ResolverApi) consumeResponse(addr uint32, target proto.Message) error {
	response := &messages.Response{}
	data := r.consume(addr)

	mustUnmarshal(data, response)

	if err := response.GetError(); err != "" {
		return errors.New(err)
	}
	return proto.Unmarshal(response.GetData(), target)
}

// consumeRequest consumes a request from WASM memory
func (r *ResolverApi) consumeRequest(addr uint32, target proto.Message) error {
	request := &messages.Request{}
	data := r.consume(addr)
	mustUnmarshal(data, request)

	return proto.Unmarshal(request.Data, target)
}

// transfer allocates memory in WASM and copies data
func (r *ResolverApi) transfer(data []byte) uint32 {
	ctx := context.Background()

	// Allocate memory in WASM
	results, err := r.wasmMsgAlloc.Call(ctx, uint64(len(data)))
	if err != nil {
		panic(fmt.Sprintf("Failed to allocate memory: %v", err))
	}

	addr := uint32(results[0])

	// Write data to WASM memory
	memory := r.instance.Memory()
	memory.Write(addr, data)

	return addr
}

// consume reads data from WASM memory and frees it
func (r *ResolverApi) consume(addr uint32) []byte {
	memory := r.instance.Memory()

	// Read length (assuming 4-byte length prefix)
	lenBytes, _ := memory.Read(addr-4, 4)
	length := binary.LittleEndian.Uint32(lenBytes) - 4

	// Read data
	data, _ := memory.Read(addr, length)

	// Free memory
	ctx := context.Background()
	r.wasmMsgFree.Call(ctx, uint64(addr))

	return data
}

// transferResponse is a helper function for host functions to transfer responses
func transferResponse(m api.Module, response proto.Message) uint32 {
	data := mustMarshal(response)

	// Allocate memory
	ctx := context.Background()
	results, err := m.ExportedFunction("wasm_msg_alloc").Call(ctx, uint64(len(data)))
	if err != nil {
		panic(fmt.Sprintf("Failed to allocate memory in host function: %v", err))
	}

	addr := uint32(results[0])
	memory := m.Memory()
	memory.Write(addr, data)

	return addr
}

// mustMarshal is a helper function that panics on marshal errors
func mustMarshal(message proto.Message) []byte {
	data, err := proto.Marshal(message)
	if err != nil {
		panic(fmt.Sprintf("Failed to marshal protobuf: %v", err))
	}
	return data
}

func mustUnmarshal(data []byte, target proto.Message) {
	if err := proto.Unmarshal(data, target); err != nil {
		panic(fmt.Sprintf("Failed to unmarshal protobuf: %v", err))
	}
}
