#![forbid(unsafe_code)]

mod error;
mod secret;
#[cfg(feature = "test-fixtures")]
pub mod test_fixture;

pub use error::{Error, Result};
pub use secret::Secret;
