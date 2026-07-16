#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum TransportKind {
    TorMaildrop,
    Direct,
    Lan,
    WifiDirect,
    WifiHotspot,
    Bluetooth,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum TransportAvailability {
    Available,
    PermissionDenied,
    Unavailable,
    Unsupported,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct TransportCapability {
    kind: TransportKind,
    availability: TransportAvailability,
}

impl TransportCapability {
    pub fn new(
        kind: TransportKind,
        availability: TransportAvailability,
    ) -> Result<Self, TransportCapabilityError> {
        if kind == TransportKind::TorMaildrop && availability == TransportAvailability::Unsupported
        {
            return Err(TransportCapabilityError::TorMaildropUnsupported);
        }
        Ok(Self { kind, availability })
    }

    #[must_use]
    pub const fn kind(self) -> TransportKind {
        self.kind
    }

    #[must_use]
    pub const fn availability(self) -> TransportAvailability {
        self.availability
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, thiserror::Error)]
pub enum TransportCapabilityError {
    #[error("Tor maildrop cannot be marked unsupported")]
    TorMaildropUnsupported,
}

#[cfg(test)]
mod tests {
    use super::{
        TransportAvailability, TransportCapability, TransportCapabilityError, TransportKind,
    };

    #[test]
    fn reports_explicit_transport_availability_without_fallback() {
        let capability = TransportCapability::new(
            TransportKind::Bluetooth,
            TransportAvailability::PermissionDenied,
        )
        .unwrap();
        assert_eq!(capability.kind(), TransportKind::Bluetooth);
        assert_eq!(
            capability.availability(),
            TransportAvailability::PermissionDenied
        );
        assert_eq!(
            TransportCapability::new(
                TransportKind::TorMaildrop,
                TransportAvailability::Unsupported
            ),
            Err(TransportCapabilityError::TorMaildropUnsupported)
        );
    }
}
