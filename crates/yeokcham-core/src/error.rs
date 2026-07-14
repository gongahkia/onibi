use thiserror::Error;

pub type Result<T, E = Error> = std::result::Result<T, E>;

#[derive(Debug, Error)]
pub enum Error {
    #[error("invalid input: {0}")]
    InvalidInput(&'static str),
    #[error("unsupported protocol version: {0}")]
    UnsupportedVersion(u16),
    #[error("resource limit exceeded: {0}")]
    ResourceLimit(&'static str),
    #[error("state violation: {0}")]
    State(&'static str),
}
