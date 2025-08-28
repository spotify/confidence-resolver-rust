use core::cell::UnsafeCell;

const MAX_CONCURRENT_THREADS: usize = 16;

#[link(wasm_import_module = "wasm_msg")]
extern "C" {
    fn wasm_msg_current_thread_id() -> usize;
}

pub struct ThreadLocalStorage<T> {
    storage: [UnsafeCell<Option<T>>; MAX_CONCURRENT_THREADS],
}
// SAFETY: ThreadLocalStorage is designed to be thread-safe when accessed through
// the Context's thread_id. Each thread gets its own slot based on thread_id,
// so there's no data race between threads. The Option wrapper ensures we can
// initialize it lazily.
unsafe impl<T> Sync for ThreadLocalStorage<T> {}

impl<T: Default> ThreadLocalStorage<T> {
    pub fn get(&self) -> &mut T {
        self.get_or_init(T::default)
    }
}

impl<T> ThreadLocalStorage<T> {
    #[allow(clippy::mut_from_ref)]
    fn get_slot(&self, slot: usize) -> &mut Option<T> {
        if slot >= MAX_CONCURRENT_THREADS {
            panic!("Thread ID out of bounds");
        }
        unsafe { &mut *self.storage[slot].get() }
    }

    pub const fn new() -> Self {
        Self {
            storage: [const { UnsafeCell::new(None) }; MAX_CONCURRENT_THREADS],
        }
    }

    #[allow(clippy::mut_from_ref)]
    pub fn get_or_init(&self, init: impl FnOnce() -> T) -> &mut T {
        let thread_id = unsafe { wasm_msg_current_thread_id() };
        self.get_slot(thread_id).get_or_insert_with(init)
    }
}
