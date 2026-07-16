use crate::{
    LocalDaemonEndpointError, SdkClientError, SdkConfigError, SdkContactError,
    SdkDeliveryProfilePolicyError, SdkEventError, SdkEventStreamError, SdkIdentityError,
    SdkMessageEnvelopeError, SdkMessageError, SdkMessageExpiryError, SdkMessageIdentifierError,
    TransportCapabilityError,
};

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum SdkError {
    #[error("SDK client operation failed")]
    Client(#[from] SdkClientError),
    #[error("SDK configuration is invalid")]
    Configuration(#[from] SdkConfigError),
    #[error("local daemon endpoint is invalid")]
    DaemonEndpoint(#[from] LocalDaemonEndpointError),
    #[error("SDK contact operation failed")]
    Contact(#[from] SdkContactError),
    #[error("SDK delivery-profile policy is invalid")]
    DeliveryProfilePolicy(#[from] SdkDeliveryProfilePolicyError),
    #[error("SDK event is invalid")]
    Event(#[from] SdkEventError),
    #[error("SDK event stream failed")]
    EventStream(#[from] SdkEventStreamError),
    #[error("SDK identity operation failed")]
    Identity(#[from] SdkIdentityError),
    #[error("SDK message envelope is invalid")]
    MessageEnvelope(#[from] SdkMessageEnvelopeError),
    #[error("SDK message expiry is invalid")]
    MessageExpiry(#[from] SdkMessageExpiryError),
    #[error("SDK message identifier is invalid")]
    MessageIdentifier(#[from] SdkMessageIdentifierError),
    #[error("SDK message operation failed")]
    Message(#[from] SdkMessageError),
    #[error("SDK transport capability is invalid")]
    TransportCapability(#[from] TransportCapabilityError),
}

#[cfg(test)]
mod tests {
    use super::SdkError;
    use crate::{SdkClientError, SdkMessageExpiryError};

    #[test]
    fn preserves_the_sdk_error_family_when_converted() {
        assert_eq!(
            SdkError::from(SdkClientError::AlreadyRunning),
            SdkError::Client(SdkClientError::AlreadyRunning)
        );
        assert_eq!(
            SdkError::from(SdkMessageExpiryError::TimestampOverflow),
            SdkError::MessageExpiry(SdkMessageExpiryError::TimestampOverflow)
        );
    }
}
