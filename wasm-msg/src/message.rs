extern crate alloc;

use crate::memory::{consume_buffer, transfer_buffer};
use alloc::string::String;
use alloc::vec::Vec;

// Include the generated protobuf code
pub mod proto {
    include!(concat!(env!("OUT_DIR"), "/wasm_msg.rs"));
}

/// Consumes a request from guest memory, decoding it and freeing the memory.
/// Returns the decoded request data.
pub(crate) fn consume_request<T>(ptr: *mut u8) -> T
where
    T: prost::Message + Default,
{
    // First consume the request wrapper
    let request = consume_message::<proto::Request>(ptr);

    // Then decode the actual request
    T::decode(request.data.as_slice()).expect("consume_request: failed to decode request")
}

/// Consumes a response from host memory, decoding it and freeing the memory.
/// Returns the decoded response data or error.
pub(crate) fn consume_response<T>(ptr: *mut u8) -> Result<T, String>
where
    T: prost::Message + Default,
{
    // First consume the response wrapper
    let response = consume_message::<proto::Response>(ptr);

    // Extract the response from the wrapper
    match response.result {
        Some(proto::response::Result::Data(data)) => {
            let result =
                T::decode(data.as_slice()).expect("consume_response: failed to decode response");
            Ok(result)
        }
        Some(proto::response::Result::Error(e)) => Err(e),
        _ => panic!("consume_response: invalid response type"),
    }
}

/// Transfers a request to guest memory, encoding it and allocating memory.
/// Returns a pointer to the allocated memory containing the encoded request.
pub(crate) fn transfer_request<T>(request: T) -> *mut u8
where
    T: prost::Message,
{
    // First encode the request
    let mut encoded = Vec::new();
    request
        .encode(&mut encoded)
        .expect("transfer_request: failed to encode request");

    // Create and transfer the request wrapper
    let request = proto::Request { data: encoded };
    transfer_message(request)
}

/// Transfers a response to host memory, encoding it and allocating memory.
/// Returns a pointer to the allocated memory containing the encoded response.
pub(crate) fn transfer_response<T>(response: Result<T, String>) -> *mut u8
where
    T: prost::Message,
{
    // Create the response wrapper
    let response = match response {
        Ok(resp) => {
            // Encode the response
            let mut encoded = Vec::new();
            resp.encode(&mut encoded)
                .expect("transfer_response: failed to encode response");
            proto::Response {
                result: Some(proto::response::Result::Data(encoded)),
            }
        }
        Err(e) => proto::Response {
            result: Some(proto::response::Result::Error(e)),
        },
    };

    // Transfer the response wrapper
    transfer_message(response)
}

/// Consume a message from memory, decoding it and freeing the allocation.
/// The pointer should point to the data area (after the size field).
/// Returns the decoded message or an error.
pub(crate) fn consume_message<T>(ptr: *mut u8) -> T
where
    T: prost::Message + Default,
{
    if ptr.is_null() {
        panic!("consume_message: called with null pointer");
    }

    // Decode the message
    consume_buffer(ptr, |buf| {
        T::decode(buf).expect("consume_message: failed to decode message")
    })
}

/// Transfer a message to memory, encoding it and allocating memory.
/// Returns a pointer to the allocated memory containing the encoded message.
pub(crate) fn transfer_message<T>(message: T) -> *mut u8
where
    T: prost::Message,
{
    // Encode the message
    let mut encoded = Vec::new();
    message
        .encode(&mut encoded)
        .expect("transfer_message: failed to encode message");

    transfer_buffer(encoded)
}
