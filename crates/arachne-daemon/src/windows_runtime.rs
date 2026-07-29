#![allow(unsafe_code)]

use windows::Win32::{
    Foundation::RPC_E_CHANGED_MODE,
    System::WinRT::{RO_INIT_MULTITHREADED, RoInitialize, RoUninitialize},
};

pub struct WindowsRuntime {
    initialized_here: bool,
}

impl WindowsRuntime {
    pub fn initialize() -> Option<Self> {
        match unsafe { RoInitialize(RO_INIT_MULTITHREADED) } {
            // safety: initializes only the calling thread
            Ok(()) => Some(Self {
                initialized_here: true,
            }),
            Err(error) if error.code() == RPC_E_CHANGED_MODE => Some(Self {
                initialized_here: false,
            }),
            Err(_) => None,
        }
    }
}

impl Drop for WindowsRuntime {
    fn drop(&mut self) {
        if self.initialized_here {
            unsafe { RoUninitialize() }; // safety: balances this instance's successful initialization
        }
    }
}
