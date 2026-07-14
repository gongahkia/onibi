use minicbor::{Decoder, Encoder};

pub const LOCAL_MESH_PROFILE_CONFIG_SCHEMA_VERSION: u8 = 1;
const LOCAL_MESH_PROFILE_CONFIG_FIELDS: u64 = 2;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum LocalMeshTransportKind {
    Lan = 1,
    WifiHotspot = 2,
    WifiDirect = 3,
    Bluetooth = 4,
}

impl TryFrom<u8> for LocalMeshTransportKind {
    type Error = LocalMeshProfileConfigError;

    fn try_from(value: u8) -> Result<Self, Self::Error> {
        match value {
            1 => Ok(Self::Lan),
            2 => Ok(Self::WifiHotspot),
            3 => Ok(Self::WifiDirect),
            4 => Ok(Self::Bluetooth),
            _ => Err(LocalMeshProfileConfigError::UnknownTransportKind(value)),
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct LocalMeshProfileConfig {
    transport: LocalMeshTransportKind,
}

impl LocalMeshProfileConfig {
    #[must_use]
    pub const fn new(transport: LocalMeshTransportKind) -> Self {
        Self { transport }
    }

    #[must_use]
    pub const fn transport(self) -> LocalMeshTransportKind {
        self.transport
    }

    pub fn encode(self) -> Result<Vec<u8>, LocalMeshProfileConfigError> {
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(LOCAL_MESH_PROFILE_CONFIG_FIELDS)
            .map_err(|_| LocalMeshProfileConfigError::Encode)?
            .u8(LOCAL_MESH_PROFILE_CONFIG_SCHEMA_VERSION)
            .map_err(|_| LocalMeshProfileConfigError::Encode)?
            .u8(self.transport as u8)
            .map_err(|_| LocalMeshProfileConfigError::Encode)?;
        Ok(encoder.into_writer())
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, LocalMeshProfileConfigError> {
        let mut decoder = Decoder::new(encoded);
        if decoder
            .array()
            .map_err(|_| LocalMeshProfileConfigError::Decode)?
            != Some(LOCAL_MESH_PROFILE_CONFIG_FIELDS)
        {
            return Err(LocalMeshProfileConfigError::InvalidShape);
        }
        let schema_version = decoder
            .u8()
            .map_err(|_| LocalMeshProfileConfigError::Decode)?;
        if schema_version != LOCAL_MESH_PROFILE_CONFIG_SCHEMA_VERSION {
            return Err(LocalMeshProfileConfigError::UnsupportedSchemaVersion(
                schema_version,
            ));
        }
        let transport = LocalMeshTransportKind::try_from(
            decoder
                .u8()
                .map_err(|_| LocalMeshProfileConfigError::Decode)?,
        )?;
        if decoder.position() != encoded.len() {
            return Err(LocalMeshProfileConfigError::TrailingBytes);
        }
        Ok(Self::new(transport))
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum LocalMeshProfileConfigError {
    #[error("unsupported local-mesh-profile configuration schema version: {0}")]
    UnsupportedSchemaVersion(u8),
    #[error("unknown local-mesh transport kind: {0}")]
    UnknownTransportKind(u8),
    #[error("local-mesh-profile configuration must be a two-element definite-length CBOR array")]
    InvalidShape,
    #[error("CBOR decoding failed")]
    Decode,
    #[error("CBOR encoding failed")]
    Encode,
    #[error("trailing bytes after local-mesh-profile configuration")]
    TrailingBytes,
}

#[cfg(test)]
mod tests {
    use super::{
        LOCAL_MESH_PROFILE_CONFIG_SCHEMA_VERSION, LocalMeshProfileConfig,
        LocalMeshProfileConfigError, LocalMeshTransportKind,
    };

    #[test]
    fn canonical_transport_selectors_round_trip() {
        for (transport, encoded) in [
            (LocalMeshTransportKind::Lan, [0x82, 0x01, 0x01]),
            (LocalMeshTransportKind::WifiHotspot, [0x82, 0x01, 0x02]),
            (LocalMeshTransportKind::WifiDirect, [0x82, 0x01, 0x03]),
            (LocalMeshTransportKind::Bluetooth, [0x82, 0x01, 0x04]),
        ] {
            let config = LocalMeshProfileConfig::new(transport);
            assert_eq!(config.encode().unwrap(), encoded);
            assert_eq!(LocalMeshProfileConfig::decode(&encoded).unwrap(), config);
        }
    }

    #[test]
    fn rejects_invalid_encodings() {
        assert_eq!(
            LocalMeshProfileConfig::decode(&[0x82, 0x02, 0x01]).unwrap_err(),
            LocalMeshProfileConfigError::UnsupportedSchemaVersion(2)
        );
        assert_eq!(
            LocalMeshProfileConfig::decode(
                &[0x82, LOCAL_MESH_PROFILE_CONFIG_SCHEMA_VERSION, 0x05,]
            )
            .unwrap_err(),
            LocalMeshProfileConfigError::UnknownTransportKind(5)
        );
        assert_eq!(
            LocalMeshProfileConfig::decode(&[0x9f, 0x01, 0x01, 0xff]).unwrap_err(),
            LocalMeshProfileConfigError::InvalidShape
        );
        assert_eq!(
            LocalMeshProfileConfig::decode(&[0x82, 0x01, 0x01, 0]).unwrap_err(),
            LocalMeshProfileConfigError::TrailingBytes
        );
    }
}
