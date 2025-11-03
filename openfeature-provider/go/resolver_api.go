package confidence

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	resolverv1 "github.com/spotify/confidence-resolver-rust/openfeature-provider/go/confidence/proto/confidence/flags/resolverinternal"
	"github.com/spotify/confidence-resolver-rust/openfeature-provider/go/confidence/proto/resolver"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	messages "github.com/spotify/confidence-resolver-rust/openfeature-provider/go/confidence/proto"
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
	wasmMsgGuestFlushLogs        api.Function
	wasmMsgGuestResolve          api.Function
	wasmMsgGuestResolveSimple    api.Function

	// Flag logger for writing logs
	flagLogger WasmFlagLogger

	// Mutex to protect concurrent access to WASM instance
	// All WASM operations require exclusive access
	mu sync.Mutex

	// Flag to indicate instance is being closed/replaced
	isClosing bool

	// Flag to track if this is the first resolve call
	firstResolve bool
}

// InitializeWasmRuntime registers host functions and compiles the WASM module
// This should be called once per runtime, not per instance
func InitializeWasmRuntime(ctx context.Context, runtime wazero.Runtime, wasmBytes []byte) (wazero.CompiledModule, error) {
	// Register host functions as a separate module (only once)
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
		return nil, fmt.Errorf("failed to register host functions: %w", err)
	}

	// Compile the WASM module
	module, err := runtime.CompileModule(ctx, wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to compile WASM module: %w", err)
	}

	return module, nil
}

// NewResolverApiFromCompiled creates a new ResolverApi instance from a pre-compiled module
func NewResolverApiFromCompiled(ctx context.Context, runtime wazero.Runtime, compiledModule wazero.CompiledModule, flagLogger WasmFlagLogger) *ResolverApi {
	// Instantiate the module with a unique name to allow multiple instances
	// wazero requires unique module names for multiple instantiations
	config := wazero.NewModuleConfig().WithName("")
	instance, err := runtime.InstantiateModule(ctx, compiledModule, config)
	if err != nil {
		panic(fmt.Sprintf("Failed to instantiate WASM module: %v", err))
	}

	// Get exported functions
	wasmMsgAlloc := instance.ExportedFunction("wasm_msg_alloc")
	wasmMsgFree := instance.ExportedFunction("wasm_msg_free")
	wasmMsgGuestSetResolverState := instance.ExportedFunction("wasm_msg_guest_set_resolver_state")
	wasmMsgGuestFlushLogs := instance.ExportedFunction("wasm_msg_guest_flush_logs")
	wasmMsgGuestResolve := instance.ExportedFunction("wasm_msg_guest_resolve")

	if wasmMsgAlloc == nil || wasmMsgFree == nil || wasmMsgGuestSetResolverState == nil || wasmMsgGuestFlushLogs == nil || wasmMsgGuestResolve == nil {
		panic("Required WASM exports not found")
	}

	return &ResolverApi{
		instance:                     instance,
		module:                       compiledModule,
		runtime:                      runtime,
		wasmMsgAlloc:                 wasmMsgAlloc,
		wasmMsgFree:                  wasmMsgFree,
		wasmMsgGuestSetResolverState: wasmMsgGuestSetResolverState,
		wasmMsgGuestFlushLogs:        wasmMsgGuestFlushLogs,
		wasmMsgGuestResolve:          wasmMsgGuestResolve,
		flagLogger:                   flagLogger,
		firstResolve:                 true,
	}
}

// FlushLogs flushes any pending logs from the WASM module and writes them via the flag logger
func (r *ResolverApi) FlushLogs() error {
	r.mu.Lock()
	// Mark instance as closing to prevent new resolves
	r.isClosing = true
	defer r.mu.Unlock()

	ctx := context.Background()
	// Create Void request
	voidRequest := &messages.Void{}
	reqPtr := r.transferRequest(voidRequest)

	// Call the WASM flush_logs function
	results, err := r.wasmMsgGuestFlushLogs.Call(ctx, uint64(reqPtr))
	if err != nil {
		return fmt.Errorf("failed to call wasm_msg_guest_flush_logs: %w", err)
	}

	// Consume the response which contains WriteFlagLogsRequest
	respPtr := uint32(results[0])

	// If the response pointer is 0, there are no logs to flush
	if respPtr == 0 {
		return nil
	}
	logRequest := &resolverv1.WriteFlagLogsRequest{}
	err = r.consumeResponse(respPtr, logRequest)
	if err != nil {
		println(err.Error())
		// Silently ignore errors during flush - this is normal during shutdown
		// when the WASM module may have already been partially cleaned up
		return nil
	}

	// Write logs via the flag logger if we have one
	if r.flagLogger != nil && (len(logRequest.FlagAssigned) > 0 || len(logRequest.ClientResolveInfo) > 0 || len(logRequest.FlagResolveInfo) > 0) {
		if err := r.flagLogger.Write(ctx, logRequest); err != nil {
			log.Printf("Failed to write flushed logs: %v", err)
		}
	} else {
		log.Printf("No flag logs were found")
	}

	return nil
}

// Close closes the WASM instance
// Note: This does NOT close the compiled module, as it may be shared across instances
func (r *ResolverApi) Close(ctx context.Context) {
	log.Printf("Flushing WASM instance")
	err := r.FlushLogs()
	if err != nil {
		log.Printf("Flushing failed: %v", err)
		return
	}
	if r.instance != nil {
		err := r.instance.Close(ctx)
		if err != nil {
			return
		}
	}
}

// SetResolverState sets the resolver state in the WASM module
func (r *ResolverApi) SetResolverState(state []byte, accountId string) error {
	// Use write lock for SetResolverState - blocks all other access
	r.mu.Lock()
	defer r.mu.Unlock()

	ctx := context.Background()
	log.Printf("Setting resolver state for account %s", accountId)

	// Create SetResolverStateRequest
	setStateRequest := &messages.SetResolverStateRequest{
		State:     state,
		AccountId: accountId,
	}

	// Wrap in Request
	request := &messages.Request{
		Data: mustMarshal(setStateRequest),
	}

	// Transfer request to WASM memory
	req, _ := proto.Marshal(request)
	reqPtr := r.transfer(req)

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

// ErrInstanceClosed is returned when the WASM instance is being closed/swapped
var ErrInstanceClosed = errors.New("WASM instance is closed or being replaced")

// Resolve resolves flags using the WASM module
func (r *ResolverApi) Resolve(request *resolver.ResolveFlagsRequest) (*resolver.ResolveFlagsResponse, error) {
	// Acquire lock first, then check isClosing flag to prevent race condition
	// where instance could be marked as closing between check and lock acquisition.
	// If closing, return immediately with ErrInstanceClosed to prevent using stale instance.
	r.mu.Lock()
	if r.isClosing {
		defer r.mu.Unlock()
		return nil, ErrInstanceClosed
	}
	defer r.mu.Unlock()

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
		log.Printf("Resolve failed with error: %v", err)
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

	if data == nil {
		return fmt.Errorf("failed to consume WASM memory at address: %d", addr)
	}

	if err := proto.Unmarshal(data, response); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}

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
		// Log memory size when allocation fails
		if r.instance != nil && r.instance.Memory() != nil {
			memorySize := r.instance.Memory().Size()
			log.Printf("Failed to allocate %d bytes. Current WASM memory size: %d bytes (%d pages). Error: %v",
				len(data), memorySize, memorySize/65536, err)
		}
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
	lenBytes, ok := memory.Read(addr-4, 4)
	if !ok {
		return nil
	}
	length := binary.LittleEndian.Uint32(lenBytes) - 4

	// Read data
	data, ok := memory.Read(addr, length)
	if !ok {
		return nil
	}

	// Make a copy of the data before freeing the WASM memory
	// This prevents race conditions where the caller uses the data after we free it
	dataCopy := make([]byte, length)
	copy(dataCopy, data)

	// Free memory
	ctx := context.Background()
	_, err := r.wasmMsgFree.Call(ctx, uint64(addr))
	if err != nil {
		return nil
	}

	return dataCopy
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
