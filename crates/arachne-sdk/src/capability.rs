pub const TRANSPORT_KIND_COUNT: usize = 6;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum TransportKind {
    TorMaildrop,
    Direct,
    Lan,
    WifiDirect,
    WifiHotspot,
    Bluetooth,
}

impl TransportKind {
    pub const ALL: [Self; TRANSPORT_KIND_COUNT] = [
        Self::TorMaildrop,
        Self::Direct,
        Self::Lan,
        Self::WifiDirect,
        Self::WifiHotspot,
        Self::Bluetooth,
    ];

    const fn index(self) -> usize {
        match self {
            Self::TorMaildrop => 0,
            Self::Direct => 1,
            Self::Lan => 2,
            Self::WifiDirect => 3,
            Self::WifiHotspot => 4,
            Self::Bluetooth => 5,
        }
    }
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

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct TransportCapabilityMatrix {
    capabilities: [TransportCapability; TRANSPORT_KIND_COUNT],
}

impl TransportCapabilityMatrix {
    pub fn new(capabilities: Vec<TransportCapability>) -> Result<Self, TransportCapabilityError> {
        let mut capabilities: [TransportCapability; TRANSPORT_KIND_COUNT] = capabilities
            .try_into()
            .map_err(|_| TransportCapabilityError::IncompleteMatrix)?;
        capabilities.sort_unstable_by_key(|capability| capability.kind().index());
        if capabilities
            .windows(2)
            .any(|pair| pair[0].kind() == pair[1].kind())
        {
            return Err(TransportCapabilityError::DuplicateTransport);
        }
        if capabilities
            .iter()
            .zip(TransportKind::ALL)
            .any(|(capability, kind)| capability.kind() != kind)
        {
            return Err(TransportCapabilityError::IncompleteMatrix);
        }
        Ok(Self { capabilities })
    }

    #[must_use]
    pub const fn capability(&self, kind: TransportKind) -> TransportCapability {
        self.capabilities[kind.index()]
    }

    #[must_use]
    pub const fn capabilities(&self) -> &[TransportCapability; TRANSPORT_KIND_COUNT] {
        &self.capabilities
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, thiserror::Error)]
pub enum TransportCapabilityError {
    #[error("Tor maildrop cannot be marked unsupported")]
    TorMaildropUnsupported,
    #[error("transport capability matrix is incomplete")]
    IncompleteMatrix,
    #[error("transport capability matrix contains a duplicate transport")]
    DuplicateTransport,
}

#[cfg(test)]
mod tests {
    use super::{
        TransportAvailability, TransportCapability, TransportCapabilityError,
        TransportCapabilityMatrix, TransportKind,
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

    #[test]
    fn matrix_requires_each_transport_once_and_orders_capabilities_canonically() {
        let matrix = TransportCapabilityMatrix::new(vec![
            TransportCapability::new(TransportKind::Bluetooth, TransportAvailability::Unavailable)
                .unwrap(),
            TransportCapability::new(TransportKind::TorMaildrop, TransportAvailability::Available)
                .unwrap(),
            TransportCapability::new(TransportKind::Lan, TransportAvailability::Available).unwrap(),
            TransportCapability::new(TransportKind::Direct, TransportAvailability::Available)
                .unwrap(),
            TransportCapability::new(
                TransportKind::WifiHotspot,
                TransportAvailability::PermissionDenied,
            )
            .unwrap(),
            TransportCapability::new(
                TransportKind::WifiDirect,
                TransportAvailability::Unsupported,
            )
            .unwrap(),
        ])
        .unwrap();
        assert_eq!(
            matrix.capability(TransportKind::WifiHotspot).availability(),
            TransportAvailability::PermissionDenied
        );
        assert_eq!(
            (*matrix.capabilities()).map(TransportCapability::kind),
            TransportKind::ALL
        );
        assert_eq!(
            TransportCapabilityMatrix::new(vec![
                TransportCapability::new(
                    TransportKind::TorMaildrop,
                    TransportAvailability::Available
                )
                .unwrap(),
            ]),
            Err(TransportCapabilityError::IncompleteMatrix)
        );
        assert_eq!(
            TransportCapabilityMatrix::new(vec![
                TransportCapability::new(
                    TransportKind::TorMaildrop,
                    TransportAvailability::Available
                )
                .unwrap(),
                TransportCapability::new(TransportKind::Direct, TransportAvailability::Available)
                    .unwrap(),
                TransportCapability::new(TransportKind::Lan, TransportAvailability::Available)
                    .unwrap(),
                TransportCapability::new(
                    TransportKind::WifiDirect,
                    TransportAvailability::Available
                )
                .unwrap(),
                TransportCapability::new(
                    TransportKind::WifiHotspot,
                    TransportAvailability::Available
                )
                .unwrap(),
                TransportCapability::new(
                    TransportKind::WifiHotspot,
                    TransportAvailability::Unavailable
                )
                .unwrap(),
            ]),
            Err(TransportCapabilityError::DuplicateTransport)
        );
    }
}
