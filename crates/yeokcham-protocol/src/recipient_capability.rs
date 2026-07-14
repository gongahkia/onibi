use minicbor::{Decoder, Encoder};

use crate::{
    DirectProfileConfig, LocalMeshProfileConfig, MailboxCapability, TorMaildropProfileConfig,
};

pub const RECIPIENT_CAPABILITY_SCHEMA_VERSION: u8 = 1;
const RECIPIENT_CAPABILITY_FIELDS: u64 = 4;
const DIRECT_PROFILE_KIND: u8 = 1;
const TOR_MAILDROP_PROFILE_KIND: u8 = 2;
const LOCAL_MESH_PROFILE_KIND: u8 = 3;

#[derive(Debug)]
pub enum RecipientCapability {
    Direct(DirectProfileConfig),
    TorMaildrop {
        config: TorMaildropProfileConfig,
        mailbox_capability: MailboxCapability,
    },
    LocalMesh(LocalMeshProfileConfig),
}

impl RecipientCapability {
    pub fn encode(&self) -> Result<Vec<u8>, RecipientCapabilityError> {
        let (profile_kind, profile_configuration, mailbox_capability) = match self {
            Self::Direct(config) => (
                DIRECT_PROFILE_KIND,
                config
                    .encode()
                    .map_err(|_| RecipientCapabilityError::InvalidDirectProfileConfiguration)?,
                Vec::new(),
            ),
            Self::TorMaildrop {
                config,
                mailbox_capability,
            } => (
                TOR_MAILDROP_PROFILE_KIND,
                config.encode().map_err(|_| {
                    RecipientCapabilityError::InvalidTorMaildropProfileConfiguration
                })?,
                mailbox_capability
                    .encode()
                    .map_err(|_| RecipientCapabilityError::InvalidMailboxCapability)?,
            ),
            Self::LocalMesh(config) => (
                LOCAL_MESH_PROFILE_KIND,
                config
                    .encode()
                    .map_err(|_| RecipientCapabilityError::InvalidLocalMeshProfileConfiguration)?,
                Vec::new(),
            ),
        };
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(RECIPIENT_CAPABILITY_FIELDS)
            .map_err(|_| RecipientCapabilityError::Encode)?
            .u8(RECIPIENT_CAPABILITY_SCHEMA_VERSION)
            .map_err(|_| RecipientCapabilityError::Encode)?
            .u8(profile_kind)
            .map_err(|_| RecipientCapabilityError::Encode)?
            .bytes(&profile_configuration)
            .map_err(|_| RecipientCapabilityError::Encode)?
            .bytes(&mailbox_capability)
            .map_err(|_| RecipientCapabilityError::Encode)?;
        Ok(encoder.into_writer())
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, RecipientCapabilityError> {
        let mut decoder = Decoder::new(encoded);
        if decoder
            .array()
            .map_err(|_| RecipientCapabilityError::Decode)?
            != Some(RECIPIENT_CAPABILITY_FIELDS)
        {
            return Err(RecipientCapabilityError::InvalidShape);
        }
        let schema_version = decoder.u8().map_err(|_| RecipientCapabilityError::Decode)?;
        if schema_version != RECIPIENT_CAPABILITY_SCHEMA_VERSION {
            return Err(RecipientCapabilityError::UnsupportedSchemaVersion(
                schema_version,
            ));
        }
        let profile_kind = decoder.u8().map_err(|_| RecipientCapabilityError::Decode)?;
        let profile_configuration = decoder
            .bytes()
            .map_err(|_| RecipientCapabilityError::Decode)?;
        let mailbox_capability = decoder
            .bytes()
            .map_err(|_| RecipientCapabilityError::Decode)?;
        if decoder.position() != encoded.len() {
            return Err(RecipientCapabilityError::TrailingBytes);
        }
        match profile_kind {
            DIRECT_PROFILE_KIND => {
                if !mailbox_capability.is_empty() {
                    return Err(RecipientCapabilityError::UnexpectedMailboxCapability);
                }
                let config = DirectProfileConfig::decode(profile_configuration)
                    .map_err(|_| RecipientCapabilityError::InvalidDirectProfileConfiguration)?;
                Ok(Self::Direct(config))
            }
            TOR_MAILDROP_PROFILE_KIND => {
                if mailbox_capability.is_empty() {
                    return Err(RecipientCapabilityError::MissingMailboxCapability);
                }
                let config =
                    TorMaildropProfileConfig::decode(profile_configuration).map_err(|_| {
                        RecipientCapabilityError::InvalidTorMaildropProfileConfiguration
                    })?;
                let mailbox_capability = MailboxCapability::decode(mailbox_capability)
                    .map_err(|_| RecipientCapabilityError::InvalidMailboxCapability)?;
                Ok(Self::TorMaildrop {
                    config,
                    mailbox_capability,
                })
            }
            LOCAL_MESH_PROFILE_KIND => {
                if !mailbox_capability.is_empty() {
                    return Err(RecipientCapabilityError::UnexpectedMailboxCapability);
                }
                let config = LocalMeshProfileConfig::decode(profile_configuration)
                    .map_err(|_| RecipientCapabilityError::InvalidLocalMeshProfileConfiguration)?;
                Ok(Self::LocalMesh(config))
            }
            _ => Err(RecipientCapabilityError::UnknownProfileKind(profile_kind)),
        }
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum RecipientCapabilityError {
    #[error("unsupported recipient-capability schema version: {0}")]
    UnsupportedSchemaVersion(u8),
    #[error("unknown recipient capability profile kind: {0}")]
    UnknownProfileKind(u8),
    #[error("recipient capability has an invalid direct-profile configuration")]
    InvalidDirectProfileConfiguration,
    #[error("recipient capability has an invalid Tor-maildrop-profile configuration")]
    InvalidTorMaildropProfileConfiguration,
    #[error("recipient capability has an invalid local-mesh-profile configuration")]
    InvalidLocalMeshProfileConfiguration,
    #[error("recipient capability has an invalid mailbox capability")]
    InvalidMailboxCapability,
    #[error("Tor-maildrop recipient capability requires a mailbox capability")]
    MissingMailboxCapability,
    #[error("recipient capability profile must not carry a mailbox capability")]
    UnexpectedMailboxCapability,
    #[error("recipient capability must be a four-element definite-length CBOR array")]
    InvalidShape,
    #[error("CBOR decoding failed")]
    Decode,
    #[error("CBOR encoding failed")]
    Encode,
    #[error("trailing bytes after recipient capability")]
    TrailingBytes,
}

#[cfg(test)]
mod tests {
    use super::{
        RECIPIENT_CAPABILITY_SCHEMA_VERSION, RecipientCapability, RecipientCapabilityError,
    };
    use crate::{
        DirectProfileConfig, LocalMeshProfileConfig, LocalMeshTransportKind,
        MAILBOX_CAPABILITY_TOKEN_BYTES, MAILBOX_IDENTIFIER_BYTES, MailboxCapability,
        TOR_ONION_SERVICE_PUBLIC_KEY_BYTES, TorMaildropProfileConfig,
    };

    fn mailbox_capability() -> MailboxCapability {
        MailboxCapability::new(
            [0x22; MAILBOX_IDENTIFIER_BYTES],
            [0x33; MAILBOX_CAPABILITY_TOKEN_BYTES],
        )
        .unwrap()
    }

    #[test]
    fn canonical_profile_capabilities_round_trip() {
        let direct = RecipientCapability::Direct(
            DirectProfileConfig::new("192.0.2.1:4444".parse().unwrap()).unwrap(),
        );
        let direct_configuration = match &direct {
            RecipientCapability::Direct(config) => config.encode().unwrap(),
            _ => unreachable!(),
        };
        let mut expected_direct = vec![0x84, 0x01, 0x01, 0x4b];
        expected_direct.extend(direct_configuration);
        expected_direct.push(0x40);
        assert_eq!(direct.encode().unwrap(), expected_direct);
        assert!(matches!(
            RecipientCapability::decode(&expected_direct).unwrap(),
            RecipientCapability::Direct(_)
        ));

        let tor = RecipientCapability::TorMaildrop {
            config: TorMaildropProfileConfig::new([0x11; TOR_ONION_SERVICE_PUBLIC_KEY_BYTES], 4444)
                .unwrap(),
            mailbox_capability: mailbox_capability(),
        };
        let (tor_configuration, tor_mailbox_capability) = match &tor {
            RecipientCapability::TorMaildrop {
                config,
                mailbox_capability,
            } => (
                config.encode().unwrap(),
                mailbox_capability.encode().unwrap(),
            ),
            _ => unreachable!(),
        };
        let configuration_length = u8::try_from(tor_configuration.len()).unwrap();
        let mailbox_capability_length = u8::try_from(tor_mailbox_capability.len()).unwrap();
        let mut expected_tor = vec![0x84, 0x01, 0x02, 0x58, configuration_length];
        expected_tor.extend(tor_configuration);
        expected_tor.extend([0x58, mailbox_capability_length]);
        expected_tor.extend(tor_mailbox_capability);
        assert_eq!(tor.encode().unwrap(), expected_tor);
        match RecipientCapability::decode(&expected_tor).unwrap() {
            RecipientCapability::TorMaildrop {
                config,
                mailbox_capability,
            } => {
                assert_eq!(config.virtual_port(), 4444);
                assert_eq!(
                    mailbox_capability.mailbox_id(),
                    &[0x22; MAILBOX_IDENTIFIER_BYTES]
                );
                assert!(format!("{mailbox_capability:?}").contains("REDACTED"));
            }
            _ => unreachable!(),
        }

        let local_mesh = RecipientCapability::LocalMesh(LocalMeshProfileConfig::new(
            LocalMeshTransportKind::Bluetooth,
        ));
        let local_mesh_configuration = match &local_mesh {
            RecipientCapability::LocalMesh(config) => config.encode().unwrap(),
            _ => unreachable!(),
        };
        let mut expected_local_mesh = vec![0x84, 0x01, 0x03, 0x43];
        expected_local_mesh.extend(local_mesh_configuration);
        expected_local_mesh.push(0x40);
        assert_eq!(local_mesh.encode().unwrap(), expected_local_mesh);
        assert!(matches!(
            RecipientCapability::decode(&expected_local_mesh).unwrap(),
            RecipientCapability::LocalMesh(_)
        ));
    }

    #[test]
    fn rejects_invalid_or_cross_profile_capabilities() {
        assert_eq!(
            RecipientCapability::decode(&[0x84, 0x02, 0x01, 0x40, 0x40]).unwrap_err(),
            RecipientCapabilityError::UnsupportedSchemaVersion(2)
        );
        assert_eq!(
            RecipientCapability::decode(&[
                0x84,
                RECIPIENT_CAPABILITY_SCHEMA_VERSION,
                0x04,
                0x40,
                0x40,
            ])
            .unwrap_err(),
            RecipientCapabilityError::UnknownProfileKind(4)
        );
        assert_eq!(
            RecipientCapability::decode(&[0x84, 0x01, 0x02, 0x40, 0x40]).unwrap_err(),
            RecipientCapabilityError::MissingMailboxCapability
        );
        assert_eq!(
            RecipientCapability::decode(&[0x84, 0x01, 0x01, 0x40, 0x41, 0x01]).unwrap_err(),
            RecipientCapabilityError::UnexpectedMailboxCapability
        );
        assert_eq!(
            RecipientCapability::decode(&[0x9f, 0x01, 0x01, 0xff]).unwrap_err(),
            RecipientCapabilityError::InvalidShape
        );
        assert_eq!(
            RecipientCapability::decode(&[0x84, 0x01, 0x01, 0x40, 0x40, 0]).unwrap_err(),
            RecipientCapabilityError::TrailingBytes
        );
    }
}
