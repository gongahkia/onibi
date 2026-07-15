use yeokcham_protocol::LocalMeshTransportKind;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum LocalTransportAvailability {
    Available,
    PermissionDenied,
    Unavailable,
}

impl LocalTransportAvailability {
    pub fn require_available(
        self,
        kind: LocalMeshTransportKind,
    ) -> Result<(), LocalTransportAvailabilityError> {
        match self {
            Self::Available => Ok(()),
            Self::PermissionDenied => Err(LocalTransportAvailabilityError::PermissionDenied(kind)),
            Self::Unavailable => Err(LocalTransportAvailabilityError::Unavailable(kind)),
        }
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum LocalTransportAvailabilityError {
    #[error("permission to use the local transport is denied")]
    PermissionDenied(LocalMeshTransportKind),
    #[error("the local transport is unavailable")]
    Unavailable(LocalMeshTransportKind),
}

#[cfg(test)]
mod tests {
    use yeokcham_protocol::LocalMeshTransportKind;

    use super::{LocalTransportAvailability, LocalTransportAvailabilityError};

    #[test]
    fn local_transport_availability_fails_closed() {
        assert!(
            LocalTransportAvailability::Available
                .require_available(LocalMeshTransportKind::Lan)
                .is_ok()
        );
        assert_eq!(
            LocalTransportAvailability::PermissionDenied
                .require_available(LocalMeshTransportKind::Bluetooth)
                .unwrap_err(),
            LocalTransportAvailabilityError::PermissionDenied(LocalMeshTransportKind::Bluetooth)
        );
        assert_eq!(
            LocalTransportAvailability::Unavailable
                .require_available(LocalMeshTransportKind::WifiDirect)
                .unwrap_err(),
            LocalTransportAvailabilityError::Unavailable(LocalMeshTransportKind::WifiDirect)
        );
    }
}
