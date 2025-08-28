#![no_std]

extern crate alloc;
extern crate paste;

// Re-export commonly used items
pub use paste::paste;

// Crate modules
pub mod memory;
pub mod message;
pub mod sync;
pub mod tls;

pub use sync::WasmResult;

/// Macro to generate WASM handler functions with a more ergonomic syntax.
///
/// # Example
/// ```rust
/// wasm_msg_guest! {
///     fn echo(request: EchoRequest) -> WasmResult<EchoResponse> {
///         Ok(EchoResponse { text: request.text })
///     }
///     fn process(request: ProcessRequest) -> WasmResult<ProcessResponse> {
///         Ok(ProcessResponse { result: process_data(request.data) })
///     }
/// }
/// ```
#[macro_export]
macro_rules! wasm_msg_guest {
    (
        $(
            fn $name:ident($request_param:ident: $request:ty) -> WasmResult<$response:ty> $body:block
        )*
    ) => {
        $(
            // Generate the handler function
            pub fn $name($request_param: $request) -> WasmResult<$response> $body

            // Generate the WASM export with a single identifier using paste
            $crate::paste! {
                #[doc(hidden)]
                #[no_mangle]
                pub extern "C" fn [<wasm_msg_guest_ $name>](ptr: *mut u8) -> *mut u8
                where
                    $request: prost::Message + Default,
                    $response: prost::Message,
                {
                    $crate::sync::call_sync_guest(ptr, $name)
                }
            }
        )*
    };
}

/// Macro to declare host functions that can be called from WASM.
///
/// # Example
/// ```rust
/// wasm_msg_host! {
///     fn get_config(request: GetConfigRequest) -> WasmResult<GetConfigResponse>;
///     fn set_config(request: SetConfigRequest) -> WasmResult<SetConfigResponse>;
/// }
/// ```
#[macro_export]
macro_rules! wasm_msg_host {
    (
        $(
            fn $name:ident($request_param:ident: $request:ty) -> WasmResult<$response:ty>;
        )*
    ) => {
        $crate::paste! {
            $(
                #[link(wasm_import_module = "wasm_msg")]
                extern "C" {
                    fn [<wasm_msg_host_ $name>](ptr: *mut u8) -> *mut u8;
                }

                pub fn $name($request_param: $request) -> WasmResult<$response>
                where
                    $request: prost::Message,
                    $response: prost::Message + Default,
                {
                    $crate::sync::call_sync_host($request_param, [<wasm_msg_host_ $name>])
                }
            )*
        }
    };
}
