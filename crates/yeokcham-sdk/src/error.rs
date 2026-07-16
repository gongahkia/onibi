use crate::{SdkConfigError, SdkEventError, TransportCapabilityError};

#[derive(Debug, thiserror::Error)]
pub enum SdkError {
    #[error("SDK configuration is invalid")]
    Configuration(#[from] SdkConfigError),
    #[error("SDK event is invalid")]
    Event(#[from] SdkEventError),
    #[error("SDK transport capability is invalid")]
    TransportCapability(#[from] TransportCapabilityError),
}
