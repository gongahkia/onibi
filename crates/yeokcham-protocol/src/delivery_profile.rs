use minicbor::{Decoder, Encoder};

pub const DELIVERY_PROFILE_SCHEMA_VERSION: u8 = 1;
const DELIVERY_PROFILE_FIELDS: u64 = 2;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum DeliveryProfileKind {
    Direct = 1,
    TorMaildrop = 2,
    LocalMesh = 3,
}

impl DeliveryProfileKind {
    #[must_use]
    pub const fn privacy_warning(self) -> Option<DeliveryProfilePrivacyWarning> {
        match self {
            Self::Direct => Some(DeliveryProfilePrivacyWarning::DirectIpDisclosure),
            Self::TorMaildrop | Self::LocalMesh => None,
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum DeliveryProfilePrivacyWarning {
    DirectIpDisclosure,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct DirectProfileSelection {
    _private: (),
}

impl DirectProfileSelection {
    #[must_use]
    pub const fn acknowledge_ip_disclosure() -> Self {
        Self { _private: () }
    }
}

impl DeliveryProfilePrivacyWarning {
    #[must_use]
    pub const fn message(self) -> &'static str {
        match self {
            Self::DirectIpDisclosure => "Direct delivery exposes your IP address to the recipient.",
        }
    }
}

impl TryFrom<u8> for DeliveryProfileKind {
    type Error = DeliveryProfileError;

    fn try_from(value: u8) -> Result<Self, Self::Error> {
        match value {
            1 => Ok(Self::Direct),
            2 => Ok(Self::TorMaildrop),
            3 => Ok(Self::LocalMesh),
            _ => Err(DeliveryProfileError::UnknownProfileKind(value)),
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct DeliveryProfile {
    schema_version: u8,
    kind: DeliveryProfileKind,
}

impl DeliveryProfile {
    #[must_use]
    pub const fn direct(_selection: DirectProfileSelection) -> Self {
        Self::from_kind(DeliveryProfileKind::Direct)
    }

    #[must_use]
    pub const fn tor_maildrop() -> Self {
        Self::from_kind(DeliveryProfileKind::TorMaildrop)
    }

    #[must_use]
    pub const fn local_mesh() -> Self {
        Self::from_kind(DeliveryProfileKind::LocalMesh)
    }

    const fn from_kind(kind: DeliveryProfileKind) -> Self {
        Self {
            schema_version: DELIVERY_PROFILE_SCHEMA_VERSION,
            kind,
        }
    }

    #[must_use]
    pub const fn schema_version(self) -> u8 {
        self.schema_version
    }

    #[must_use]
    pub const fn kind(self) -> DeliveryProfileKind {
        self.kind
    }

    #[must_use]
    pub const fn privacy_warning(self) -> Option<DeliveryProfilePrivacyWarning> {
        self.kind.privacy_warning()
    }

    pub fn validate_automatic_replacement(
        self,
        replacement: Self,
    ) -> Result<(), DeliveryProfileTransitionError> {
        if self.kind == DeliveryProfileKind::Direct
            && replacement.kind == DeliveryProfileKind::TorMaildrop
        {
            return Err(DeliveryProfileTransitionError::DirectToTorRequiresExplicitSelection);
        }
        Ok(())
    }

    pub fn encode(self) -> Result<Vec<u8>, DeliveryProfileError> {
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(DELIVERY_PROFILE_FIELDS)
            .map_err(|_| DeliveryProfileError::Encode)?
            .u8(self.schema_version)
            .map_err(|_| DeliveryProfileError::Encode)?
            .u8(self.kind as u8)
            .map_err(|_| DeliveryProfileError::Encode)?;
        Ok(encoder.into_writer())
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, DeliveryProfileError> {
        let mut decoder = Decoder::new(encoded);
        if decoder.array().map_err(|_| DeliveryProfileError::Decode)?
            != Some(DELIVERY_PROFILE_FIELDS)
        {
            return Err(DeliveryProfileError::InvalidShape);
        }
        let schema_version = decoder.u8().map_err(|_| DeliveryProfileError::Decode)?;
        if schema_version != DELIVERY_PROFILE_SCHEMA_VERSION {
            return Err(DeliveryProfileError::UnsupportedSchemaVersion(
                schema_version,
            ));
        }
        let kind =
            DeliveryProfileKind::try_from(decoder.u8().map_err(|_| DeliveryProfileError::Decode)?)?;
        if decoder.position() != encoded.len() {
            return Err(DeliveryProfileError::TrailingBytes);
        }
        Ok(Self::from_kind(kind))
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct DeliveryProfileConstraints {
    direct_allowed: bool,
    tor_maildrop_allowed: bool,
    local_mesh_allowed: bool,
}

impl DeliveryProfileConstraints {
    pub fn new(
        direct_allowed: bool,
        tor_maildrop_allowed: bool,
        local_mesh_allowed: bool,
    ) -> Result<Self, DeliveryProfileConstraintError> {
        if !direct_allowed && !tor_maildrop_allowed && !local_mesh_allowed {
            return Err(DeliveryProfileConstraintError::NoAllowedProfiles);
        }
        Ok(Self {
            direct_allowed,
            tor_maildrop_allowed,
            local_mesh_allowed,
        })
    }

    #[must_use]
    pub fn decide(self, profile: DeliveryProfile) -> DeliveryProfilePolicyDecision {
        let allowed = match profile.kind() {
            DeliveryProfileKind::Direct => self.direct_allowed,
            DeliveryProfileKind::TorMaildrop => self.tor_maildrop_allowed,
            DeliveryProfileKind::LocalMesh => self.local_mesh_allowed,
        };
        if allowed {
            DeliveryProfilePolicyDecision::Allow
        } else {
            DeliveryProfilePolicyDecision::Deny(DeliveryProfileConstraintError::Disallowed(
                profile.kind(),
            ))
        }
    }

    pub fn validate(self, profile: DeliveryProfile) -> Result<(), DeliveryProfileConstraintError> {
        match self.decide(profile) {
            DeliveryProfilePolicyDecision::Allow => Ok(()),
            DeliveryProfilePolicyDecision::Deny(error) => Err(error),
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum DeliveryProfilePolicyDecision {
    Allow,
    Deny(DeliveryProfileConstraintError),
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, thiserror::Error)]
pub enum DeliveryProfileTransitionError {
    #[error("replacing direct delivery with Tor maildrop requires explicit profile selection")]
    DirectToTorRequiresExplicitSelection,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, thiserror::Error)]
pub enum DeliveryProfileConstraintError {
    #[error("at least one delivery profile must be allowed")]
    NoAllowedProfiles,
    #[error("delivery profile is disallowed: {0:?}")]
    Disallowed(DeliveryProfileKind),
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum DeliveryProfileError {
    #[error("unsupported delivery-profile schema version: {0}")]
    UnsupportedSchemaVersion(u8),
    #[error("unknown delivery-profile kind: {0}")]
    UnknownProfileKind(u8),
    #[error("delivery profile must be a two-element definite-length CBOR array")]
    InvalidShape,
    #[error("CBOR decoding failed")]
    Decode,
    #[error("CBOR encoding failed")]
    Encode,
    #[error("trailing bytes after delivery profile")]
    TrailingBytes,
}

#[cfg(test)]
mod tests {
    use super::{
        DeliveryProfile, DeliveryProfileConstraintError, DeliveryProfileConstraints,
        DeliveryProfileError, DeliveryProfileKind, DeliveryProfilePolicyDecision,
        DeliveryProfilePrivacyWarning, DeliveryProfileTransitionError, DirectProfileSelection,
    };

    #[test]
    fn canonical_profiles_round_trip() {
        for (profile, encoded) in [
            (
                DeliveryProfile::direct(DirectProfileSelection::acknowledge_ip_disclosure()),
                [0x82, 0x01, 0x01],
            ),
            (DeliveryProfile::tor_maildrop(), [0x82, 0x01, 0x02]),
            (DeliveryProfile::local_mesh(), [0x82, 0x01, 0x03]),
        ] {
            assert_eq!(profile.encode().unwrap(), encoded);
            assert_eq!(DeliveryProfile::decode(&encoded).unwrap(), profile);
        }
    }

    #[test]
    fn rejects_unknown_versions_and_kinds() {
        assert_eq!(
            DeliveryProfile::decode(&[0x82, 0x02, 0x01]).unwrap_err(),
            DeliveryProfileError::UnsupportedSchemaVersion(2)
        );
        assert_eq!(
            DeliveryProfile::decode(&[0x82, 0x01, 0x04]).unwrap_err(),
            DeliveryProfileError::UnknownProfileKind(4)
        );
    }

    #[test]
    fn rejects_noncanonical_shapes_and_trailing_data() {
        assert_eq!(
            DeliveryProfile::decode(&[0x9f, 0x01, 0x01, 0xff]).unwrap_err(),
            DeliveryProfileError::InvalidShape
        );
        assert_eq!(
            DeliveryProfile::decode(&[0x82, 0x01, 0x01, 0]).unwrap_err(),
            DeliveryProfileError::TrailingBytes
        );
    }

    #[test]
    fn validates_profile_allowlist() {
        let constraints = DeliveryProfileConstraints::new(false, true, false).unwrap();
        assert!(
            constraints
                .validate(DeliveryProfile::tor_maildrop())
                .is_ok()
        );
        assert_eq!(
            constraints
                .validate(DeliveryProfile::direct(
                    DirectProfileSelection::acknowledge_ip_disclosure()
                ))
                .unwrap_err(),
            DeliveryProfileConstraintError::Disallowed(DeliveryProfileKind::Direct)
        );
        assert_eq!(
            DeliveryProfileConstraints::new(false, false, false).unwrap_err(),
            DeliveryProfileConstraintError::NoAllowedProfiles
        );
    }

    #[test]
    fn decides_profile_policy_without_boolean_fallback() {
        let constraints = DeliveryProfileConstraints::new(true, false, false).unwrap();
        assert_eq!(
            constraints.decide(DeliveryProfile::direct(
                DirectProfileSelection::acknowledge_ip_disclosure()
            )),
            DeliveryProfilePolicyDecision::Allow
        );
        assert_eq!(
            constraints.decide(DeliveryProfile::local_mesh()),
            DeliveryProfilePolicyDecision::Deny(DeliveryProfileConstraintError::Disallowed(
                DeliveryProfileKind::LocalMesh
            ))
        );
    }

    #[test]
    fn exposes_direct_ip_disclosure_warning() {
        let direct = DeliveryProfile::direct(DirectProfileSelection::acknowledge_ip_disclosure());
        assert_eq!(
            direct.privacy_warning(),
            Some(DeliveryProfilePrivacyWarning::DirectIpDisclosure)
        );
        assert_eq!(
            direct.privacy_warning().unwrap().message(),
            "Direct delivery exposes your IP address to the recipient."
        );
        assert_eq!(DeliveryProfile::tor_maildrop().privacy_warning(), None);
        assert_eq!(DeliveryProfile::local_mesh().privacy_warning(), None);
    }

    #[test]
    fn rejects_silent_direct_to_tor_replacement() {
        let direct = DeliveryProfile::direct(DirectProfileSelection::acknowledge_ip_disclosure());
        assert_eq!(
            direct
                .validate_automatic_replacement(DeliveryProfile::tor_maildrop())
                .unwrap_err(),
            DeliveryProfileTransitionError::DirectToTorRequiresExplicitSelection
        );
        assert!(
            DeliveryProfile::tor_maildrop()
                .validate_automatic_replacement(DeliveryProfile::direct(
                    DirectProfileSelection::acknowledge_ip_disclosure()
                ))
                .is_ok()
        );
    }
}
