use arachne_protocol::{
    DeliveryProfile, DeliveryProfileConstraintError, DeliveryProfileConstraints,
    DeliveryProfileKind, DeliveryProfileTransitionError, DirectProfileSelection,
    LocalMeshProfileConfig, LocalMeshProfileConstraintError, LocalMeshProfileConstraints,
    LocalMeshProfileSelection, LocalMeshTransportKind,
};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct SdkDeliveryProfilePolicy {
    constraints: DeliveryProfileConstraints,
    local_mesh: Option<SdkLocalMeshPolicy>,
}

impl SdkDeliveryProfilePolicy {
    pub fn new(
        direct_allowed: bool,
        tor_maildrop_allowed: bool,
        local_mesh: Option<SdkLocalMeshPolicy>,
    ) -> Result<Self, SdkDeliveryProfilePolicyError> {
        let constraints = DeliveryProfileConstraints::new(
            direct_allowed,
            tor_maildrop_allowed,
            local_mesh.is_some(),
        )
        .map_err(map_delivery_constraint_error)?;
        Ok(Self {
            constraints,
            local_mesh,
        })
    }

    pub fn select_direct(
        self,
        acknowledgement: SdkDirectIpDisclosureAcknowledgement,
    ) -> Result<SdkDeliveryProfile, SdkDeliveryProfilePolicyError> {
        let profile = DeliveryProfile::direct(acknowledgement.into());
        self.select(profile)
    }

    pub fn select_tor_maildrop(self) -> Result<SdkDeliveryProfile, SdkDeliveryProfilePolicyError> {
        self.select(DeliveryProfile::tor_maildrop())
    }

    pub fn select_local_mesh(
        self,
        transport: SdkLocalMeshTransportKind,
    ) -> Result<SdkDeliveryProfile, SdkDeliveryProfilePolicyError> {
        let local_mesh = self
            .local_mesh
            .ok_or(SdkDeliveryProfilePolicyError::LocalMeshDisallowed)?;
        local_mesh.validate(transport)?;
        self.select(DeliveryProfile::local_mesh(
            LocalMeshProfileSelection::select(transport.into()),
        ))
    }

    fn select(
        self,
        profile: DeliveryProfile,
    ) -> Result<SdkDeliveryProfile, SdkDeliveryProfilePolicyError> {
        self.constraints
            .validate(profile)
            .map_err(map_delivery_constraint_error)?;
        Ok(SdkDeliveryProfile(profile))
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct SdkLocalMeshPolicy {
    constraints: LocalMeshProfileConstraints,
}

impl SdkLocalMeshPolicy {
    pub fn new(
        allowed: &[SdkLocalMeshTransportKind],
    ) -> Result<Self, SdkDeliveryProfilePolicyError> {
        if allowed.len() > 4 {
            return Err(SdkDeliveryProfilePolicyError::TooManyLocalMeshTransports);
        }
        let constraints = LocalMeshProfileConstraints::new(
            allowed.contains(&SdkLocalMeshTransportKind::Lan),
            allowed.contains(&SdkLocalMeshTransportKind::WifiHotspot),
            allowed.contains(&SdkLocalMeshTransportKind::WifiDirect),
            allowed.contains(&SdkLocalMeshTransportKind::Bluetooth),
        )
        .map_err(map_local_mesh_constraint_error)?;
        Ok(Self { constraints })
    }

    fn validate(
        self,
        transport: SdkLocalMeshTransportKind,
    ) -> Result<(), SdkDeliveryProfilePolicyError> {
        self.constraints
            .validate(LocalMeshProfileConfig::new(transport.into()))
            .map_err(map_local_mesh_constraint_error)
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct SdkDirectIpDisclosureAcknowledgement {
    _private: (),
}

impl SdkDirectIpDisclosureAcknowledgement {
    #[must_use]
    pub const fn acknowledge() -> Self {
        Self { _private: () }
    }
}

impl From<SdkDirectIpDisclosureAcknowledgement> for DirectProfileSelection {
    fn from(_: SdkDirectIpDisclosureAcknowledgement) -> Self {
        Self::acknowledge_ip_disclosure()
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct SdkDeliveryProfile(DeliveryProfile);

impl SdkDeliveryProfile {
    #[must_use]
    pub const fn kind(self) -> SdkDeliveryProfileKind {
        match self.0.kind() {
            DeliveryProfileKind::Direct => SdkDeliveryProfileKind::Direct,
            DeliveryProfileKind::TorMaildrop => SdkDeliveryProfileKind::TorMaildrop,
            DeliveryProfileKind::LocalMesh => SdkDeliveryProfileKind::LocalMesh,
        }
    }

    #[must_use]
    pub const fn has_direct_ip_disclosure_warning(self) -> bool {
        self.0.privacy_warning().is_some()
    }

    pub fn validate_automatic_replacement(
        self,
        replacement: Self,
    ) -> Result<(), SdkDeliveryProfilePolicyError> {
        self.0
            .validate_automatic_replacement(replacement.0)
            .map_err(map_transition_error)
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum SdkDeliveryProfileKind {
    Direct,
    TorMaildrop,
    LocalMesh,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum SdkLocalMeshTransportKind {
    Lan,
    WifiHotspot,
    WifiDirect,
    Bluetooth,
}

impl From<SdkLocalMeshTransportKind> for LocalMeshTransportKind {
    fn from(transport: SdkLocalMeshTransportKind) -> Self {
        match transport {
            SdkLocalMeshTransportKind::Lan => Self::Lan,
            SdkLocalMeshTransportKind::WifiHotspot => Self::WifiHotspot,
            SdkLocalMeshTransportKind::WifiDirect => Self::WifiDirect,
            SdkLocalMeshTransportKind::Bluetooth => Self::Bluetooth,
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, thiserror::Error)]
pub enum SdkDeliveryProfilePolicyError {
    #[error("at least one delivery profile must be allowed")]
    NoAllowedProfiles,
    #[error("direct delivery is disallowed")]
    DirectDisallowed,
    #[error("Tor maildrop delivery is disallowed")]
    TorMaildropDisallowed,
    #[error("local-mesh delivery is disallowed")]
    LocalMeshDisallowed,
    #[error("at least one local-mesh transport must be allowed")]
    NoAllowedLocalMeshTransports,
    #[error("local-mesh policy exceeds the transport limit")]
    TooManyLocalMeshTransports,
    #[error("local-mesh transport is disallowed")]
    LocalMeshTransportDisallowed,
    #[error("direct-to-Tor automatic replacement requires an explicit selection")]
    DirectToTorRequiresExplicitSelection,
}

fn map_delivery_constraint_error(
    error: DeliveryProfileConstraintError,
) -> SdkDeliveryProfilePolicyError {
    match error {
        DeliveryProfileConstraintError::NoAllowedProfiles => {
            SdkDeliveryProfilePolicyError::NoAllowedProfiles
        }
        DeliveryProfileConstraintError::Disallowed(DeliveryProfileKind::Direct) => {
            SdkDeliveryProfilePolicyError::DirectDisallowed
        }
        DeliveryProfileConstraintError::Disallowed(DeliveryProfileKind::TorMaildrop) => {
            SdkDeliveryProfilePolicyError::TorMaildropDisallowed
        }
        DeliveryProfileConstraintError::Disallowed(DeliveryProfileKind::LocalMesh) => {
            SdkDeliveryProfilePolicyError::LocalMeshDisallowed
        }
    }
}

fn map_local_mesh_constraint_error(
    error: LocalMeshProfileConstraintError,
) -> SdkDeliveryProfilePolicyError {
    match error {
        LocalMeshProfileConstraintError::NoAllowedTransports => {
            SdkDeliveryProfilePolicyError::NoAllowedLocalMeshTransports
        }
        LocalMeshProfileConstraintError::DisallowedTransport(_) => {
            SdkDeliveryProfilePolicyError::LocalMeshTransportDisallowed
        }
    }
}

fn map_transition_error(error: DeliveryProfileTransitionError) -> SdkDeliveryProfilePolicyError {
    match error {
        DeliveryProfileTransitionError::DirectToTorRequiresExplicitSelection => {
            SdkDeliveryProfilePolicyError::DirectToTorRequiresExplicitSelection
        }
    }
}
