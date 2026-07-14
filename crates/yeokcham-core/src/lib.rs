#![forbid(unsafe_code)]

mod error;
mod secret;

pub use error::{Error, Result};
pub use secret::Secret;
