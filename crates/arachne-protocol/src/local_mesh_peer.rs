use arachne_core::IdentityPublicKey;
use minicbor::{Decoder, Encoder, data::Type};

use crate::{
    DirectProfileConfig, DirectProfileConfigError, LocalMeshProfileConfigError,
    LocalMeshTransportKind,
};

pub const LOCAL_MESH_PEER_SCHEMA_VERSION: u8 = 1;
pub const MAX_LOCAL_MESH_PEER_BYTES: usize = 512;
const LOCAL_MESH_PEER_FIELDS: u64 = 4;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct LocalMeshPeer {
    transport: LocalMeshTransportKind,
    identity: IdentityPublicKey,
    direct_profile: Option<DirectProfileConfig>,
}

impl LocalMeshPeer {
    pub fn new(
        transport: LocalMeshTransportKind,
        identity: IdentityPublicKey,
        direct_profile: Option<DirectProfileConfig>,
    ) -> Result<Self, LocalMeshPeerError> {
        validate_parts(transport, direct_profile)?;
        Ok(Self {
            transport,
            identity,
            direct_profile,
        })
    }

    #[must_use]
    pub const fn transport(self) -> LocalMeshTransportKind {
        self.transport
    }

    #[must_use]
    pub const fn identity(self) -> IdentityPublicKey {
        self.identity
    }

    #[must_use]
    pub const fn direct_profile(self) -> Option<DirectProfileConfig> {
        self.direct_profile
    }

    pub fn encode(self) -> Result<Vec<u8>, LocalMeshPeerError> {
        validate_parts(self.transport, self.direct_profile)?;
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(LOCAL_MESH_PEER_FIELDS)
            .map_err(|_| LocalMeshPeerError::Encode)?
            .u8(LOCAL_MESH_PEER_SCHEMA_VERSION)
            .map_err(|_| LocalMeshPeerError::Encode)?
            .u8(self.transport as u8)
            .map_err(|_| LocalMeshPeerError::Encode)?
            .bytes(self.identity.as_bytes())
            .map_err(|_| LocalMeshPeerError::Encode)?;
        match self.direct_profile {
            Some(profile) => encoder
                .bytes(
                    &profile
                        .encode()
                        .map_err(LocalMeshPeerError::DirectProfile)?,
                )
                .map_err(|_| LocalMeshPeerError::Encode)?,
            None => encoder.null().map_err(|_| LocalMeshPeerError::Encode)?,
        };
        let output = encoder.into_writer();
        if output.len() > MAX_LOCAL_MESH_PEER_BYTES {
            return Err(LocalMeshPeerError::PeerTooLarge);
        }
        Ok(output)
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, LocalMeshPeerError> {
        if encoded.len() > MAX_LOCAL_MESH_PEER_BYTES {
            return Err(LocalMeshPeerError::PeerTooLarge);
        }
        let mut decoder = Decoder::new(encoded);
        if decoder.array().map_err(|_| LocalMeshPeerError::Decode)? != Some(LOCAL_MESH_PEER_FIELDS)
        {
            return Err(LocalMeshPeerError::InvalidShape);
        }
        let version = decoder.u8().map_err(|_| LocalMeshPeerError::Decode)?;
        if version != LOCAL_MESH_PEER_SCHEMA_VERSION {
            return Err(LocalMeshPeerError::UnsupportedSchemaVersion(version));
        }
        let transport =
            LocalMeshTransportKind::try_from(decoder.u8().map_err(|_| LocalMeshPeerError::Decode)?)
                .map_err(LocalMeshPeerError::Transport)?;
        let identity = IdentityPublicKey::from_bytes(
            decoder
                .bytes()
                .map_err(|_| LocalMeshPeerError::Decode)?
                .try_into()
                .map_err(|_| LocalMeshPeerError::InvalidIdentity)?,
        )
        .map_err(|_| LocalMeshPeerError::InvalidIdentity)?;
        let direct_profile = match decoder.datatype().map_err(|_| LocalMeshPeerError::Decode)? {
            Type::Null => {
                decoder.null().map_err(|_| LocalMeshPeerError::Decode)?;
                None
            }
            Type::Bytes => Some(
                DirectProfileConfig::decode(
                    decoder.bytes().map_err(|_| LocalMeshPeerError::Decode)?,
                )
                .map_err(LocalMeshPeerError::DirectProfile)?,
            ),
            _ => return Err(LocalMeshPeerError::InvalidDirectProfile),
        };
        if decoder.position() != encoded.len() {
            return Err(LocalMeshPeerError::TrailingBytes);
        }
        let peer = Self::new(transport, identity, direct_profile)?;
        if peer.encode()? != encoded {
            return Err(LocalMeshPeerError::NonCanonicalEncoding);
        }
        Ok(peer)
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum LocalMeshPeerError {
    #[error("local-mesh peer exceeds the configured limit")]
    PeerTooLarge,
    #[error("unsupported local-mesh peer schema version: {0}")]
    UnsupportedSchemaVersion(u8),
    #[error("local-mesh peer must be a four-element definite-length CBOR array")]
    InvalidShape,
    #[error("local-mesh peer identity is invalid")]
    InvalidIdentity,
    #[error("local-mesh peer transport is invalid: {0}")]
    Transport(#[source] LocalMeshProfileConfigError),
    #[error("local-mesh peer direct profile is invalid: {0}")]
    DirectProfile(#[source] DirectProfileConfigError),
    #[error("local-mesh peer direct profile must be bytes or null")]
    InvalidDirectProfile,
    #[error("Bluetooth local-mesh peers must not include a direct profile")]
    BluetoothDirectProfile,
    #[error("non-Bluetooth local-mesh peers require a direct profile")]
    MissingDirectProfile,
    #[error("CBOR encoding failed")]
    Encode,
    #[error("CBOR decoding failed")]
    Decode,
    #[error("trailing bytes after local-mesh peer")]
    TrailingBytes,
    #[error("local-mesh peer is not canonically encoded")]
    NonCanonicalEncoding,
}

fn validate_parts(
    transport: LocalMeshTransportKind,
    direct_profile: Option<DirectProfileConfig>,
) -> Result<(), LocalMeshPeerError> {
    match (transport, direct_profile) {
        (LocalMeshTransportKind::Bluetooth, Some(_)) => {
            Err(LocalMeshPeerError::BluetoothDirectProfile)
        }
        (LocalMeshTransportKind::Bluetooth, None) | (_, Some(_)) => Ok(()),
        (_, None) => Err(LocalMeshPeerError::MissingDirectProfile),
    }
}

#[cfg(test)]
mod tests {
    use super::{LOCAL_MESH_PEER_SCHEMA_VERSION, LocalMeshPeer, LocalMeshPeerError};
    use crate::{DirectProfileConfig, LocalMeshTransportKind};
    use arachne_core::IdentityPublicKey;

    fn identity() -> IdentityPublicKey {
        IdentityPublicKey::from_bytes([
            0xd7, 0x5a, 0x98, 0x01, 0x82, 0xb1, 0x0a, 0xb7, 0xd5, 0x4b, 0xfe, 0xd3, 0xc9, 0x64,
            0x07, 0x3a, 0x0e, 0xe1, 0x72, 0xf3, 0xda, 0xa6, 0x23, 0x25, 0xaf, 0x02, 0x1a, 0x68,
            0xf7, 0x07, 0x51, 0x1a,
        ])
        .unwrap()
    }

    #[test]
    fn encodes_a_versioned_label_free_direct_peer() {
        let profile = DirectProfileConfig::new("192.0.2.1:4444".parse().unwrap()).unwrap();
        let peer = LocalMeshPeer::new(
            LocalMeshTransportKind::WifiDirect,
            identity(),
            Some(profile),
        )
        .unwrap();
        let encoded = peer.encode().unwrap();
        assert_eq!(encoded[1], LOCAL_MESH_PEER_SCHEMA_VERSION);
        assert_eq!(LocalMeshPeer::decode(&encoded).unwrap(), peer);
        assert!(
            !encoded
                .windows(b"password".len())
                .any(|window| window == b"password")
        );
    }

    #[test]
    fn rejects_route_shapes_that_cannot_be_authenticated() {
        let profile = DirectProfileConfig::new("192.0.2.1:4444".parse().unwrap()).unwrap();
        assert_eq!(
            LocalMeshPeer::new(LocalMeshTransportKind::Bluetooth, identity(), Some(profile))
                .unwrap_err(),
            LocalMeshPeerError::BluetoothDirectProfile
        );
        assert_eq!(
            LocalMeshPeer::new(LocalMeshTransportKind::WifiHotspot, identity(), None).unwrap_err(),
            LocalMeshPeerError::MissingDirectProfile
        );
        assert_eq!(
            LocalMeshPeer::decode(&[0x84, 0x02, 0x03, 0x40, 0xf6]).unwrap_err(),
            LocalMeshPeerError::UnsupportedSchemaVersion(2)
        );
    }
}
