extern crate alloc;
use alloc::alloc::{alloc, dealloc, Layout};
use alloc::vec::Vec;
use core::mem;
use core::ptr;

/// Custom allocation function that stores total allocation size before the data
/// Returns a pointer to the data area (after the size)
#[doc(hidden)]
#[no_mangle]
pub extern "C" fn wasm_msg_alloc(size: usize) -> *mut u8 {
    // Calculate total allocation size (including the size field itself)
    let total_size = size + mem::size_of::<usize>();
    let layout = unsafe { Layout::from_size_align_unchecked(total_size, mem::align_of::<usize>()) };

    let ptr = unsafe { alloc(layout) };
    if ptr.is_null() {
        return ptr::null_mut();
    }

    // Store the total allocation size at the start
    unsafe {
        ptr::write(ptr as *mut usize, total_size);
    }

    // Return pointer to the data area (after the size)
    unsafe { ptr.add(mem::size_of::<usize>()) }
}

/// Custom free function that reads total allocation size from before the data
#[allow(clippy::not_unsafe_ptr_arg_deref)]
#[doc(hidden)]
#[no_mangle]
pub extern "C" fn wasm_msg_free(ptr: *mut u8) {
    if ptr.is_null() {
        return;
    }

    // Get pointer to the size (before the data)
    let size_ptr = unsafe { ptr.sub(mem::size_of::<usize>()) };

    // Read the total allocation size
    let total_size = unsafe { ptr::read(size_ptr as *const usize) };
    let layout = unsafe { Layout::from_size_align_unchecked(total_size, mem::align_of::<usize>()) };

    // Free the entire allocation
    unsafe {
        dealloc(size_ptr, layout);
    }
}

/// View the buffer at the given pointer, returning a slice to the data.
/// The pointer should point to the data area (after the size field).
/// This is unsafe because it assumes the pointer is valid and the size field is correct.
/// The returned slice is only valid as long as the memory at ptr is valid.
fn view_buffer<'a>(ptr: *mut u8) -> &'a [u8] {
    // Safety: We trust the caller to provide valid memory
    unsafe {
        // Get pointer to the size (before the data)
        let size_ptr = ptr.sub(mem::size_of::<usize>());
        // Read the total allocation size and subtract the size field itself to get data length
        let total_size = ptr::read(size_ptr as *const usize);
        let len = total_size - mem::size_of::<usize>();
        core::slice::from_raw_parts(ptr, len)
    }
}

pub(crate) fn consume_buffer<F, R>(ptr: *mut u8, f: F) -> R
where
    F: FnOnce(&[u8]) -> R,
{
    let buf = view_buffer(ptr);
    let result = f(buf);
    wasm_msg_free(ptr);
    result
}

pub(crate) fn transfer_buffer(buf: Vec<u8>) -> *mut u8 {
    let ptr = wasm_msg_alloc(buf.len());
    if ptr.is_null() {
        panic!("transfer_buffer: failed to allocate memory");
    }
    unsafe {
        ptr::copy_nonoverlapping(buf.as_ptr(), ptr, buf.len());
    }
    ptr
}
