use arachne_core::IdentityPublicKey;
use minicbor::{Decoder, Encoder};

pub const TOR_MAILDROP_PROFILE_CONFIG_SCHEMA_VERSION: u8 = 1;
pub const TOR_ONION_SERVICE_PUBLIC_KEY_BYTES: usize = 32;
const TOR_MAILDROP_PROFILE_CONFIG_FIELDS: u64 = 3;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct TorMaildropProfileConfig {
    onion_service_public_key: [u8; TOR_ONION_SERVICE_PUBLIC_KEY_BYTES],
    virtual_port: u16,
}

impl TorMaildropProfileConfig {
    pub fn new(
        onion_service_public_key: [u8; TOR_ONION_SERVICE_PUBLIC_KEY_BYTES],
        virtual_port: u16,
    ) -> Result<Self, TorMaildropProfileConfigError> {
        if virtual_port == 0 {
            return Err(TorMaildropProfileConfigError::ZeroVirtualPort);
        }
        IdentityPublicKey::from_bytes(onion_service_public_key)
            .map_err(|_| TorMaildropProfileConfigError::InvalidOnionServicePublicKey)?;
        Ok(Self {
            onion_service_public_key,
            virtual_port,
        })
    }

    #[must_use]
    pub const fn onion_service_public_key(self) -> [u8; TOR_ONION_SERVICE_PUBLIC_KEY_BYTES] {
        self.onion_service_public_key
    }

    #[must_use]
    pub const fn virtual_port(self) -> u16 {
        self.virtual_port
    }

    pub fn encode(self) -> Result<Vec<u8>, TorMaildropProfileConfigError> {
        if self.virtual_port == 0 {
            return Err(TorMaildropProfileConfigError::ZeroVirtualPort);
        }
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(TOR_MAILDROP_PROFILE_CONFIG_FIELDS)
            .map_err(|_| TorMaildropProfileConfigError::Encode)?
            .u8(TOR_MAILDROP_PROFILE_CONFIG_SCHEMA_VERSION)
            .map_err(|_| TorMaildropProfileConfigError::Encode)?
            .bytes(&self.onion_service_public_key)
            .map_err(|_| TorMaildropProfileConfigError::Encode)?
            .u16(self.virtual_port)
            .map_err(|_| TorMaildropProfileConfigError::Encode)?;
        Ok(encoder.into_writer())
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, TorMaildropProfileConfigError> {
        let mut decoder = Decoder::new(encoded);
        if decoder
            .array()
            .map_err(|_| TorMaildropProfileConfigError::Decode)?
            != Some(TOR_MAILDROP_PROFILE_CONFIG_FIELDS)
        {
            return Err(TorMaildropProfileConfigError::InvalidShape);
        }
        let schema_version = decoder
            .u8()
            .map_err(|_| TorMaildropProfileConfigError::Decode)?;
        if schema_version != TOR_MAILDROP_PROFILE_CONFIG_SCHEMA_VERSION {
            return Err(TorMaildropProfileConfigError::UnsupportedSchemaVersion(
                schema_version,
            ));
        }
        let onion_service_public_key = decoder
            .bytes()
            .map_err(|_| TorMaildropProfileConfigError::Decode)?
            .try_into()
            .map_err(|_| TorMaildropProfileConfigError::InvalidOnionServicePublicKeyLength)?;
        let virtual_port = decoder
            .u16()
            .map_err(|_| TorMaildropProfileConfigError::Decode)?;
        if decoder.position() != encoded.len() {
            return Err(TorMaildropProfileConfigError::TrailingBytes);
        }
        Self::new(onion_service_public_key, virtual_port)
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum TorMaildropProfileConfigError {
    #[error("unsupported Tor-maildrop-profile configuration schema version: {0}")]
    UnsupportedSchemaVersion(u8),
    #[error("onion-service public key has an invalid length")]
    InvalidOnionServicePublicKeyLength,
    #[error("onion-service public key is malformed or weak")]
    InvalidOnionServicePublicKey,
    #[error("Tor virtual port must be nonzero")]
    ZeroVirtualPort,
    #[error(
        "Tor-maildrop-profile configuration must be a three-element definite-length CBOR array"
    )]
    InvalidShape,
    #[error("CBOR decoding failed")]
    Decode,
    #[error("CBOR encoding failed")]
    Encode,
    #[error("trailing bytes after Tor-maildrop-profile configuration")]
    TrailingBytes,
}

#[cfg(test)]
mod tests {
    use super::{
        TOR_MAILDROP_PROFILE_CONFIG_SCHEMA_VERSION, TOR_ONION_SERVICE_PUBLIC_KEY_BYTES,
        TorMaildropProfileConfig, TorMaildropProfileConfigError,
    };

    fn config() -> TorMaildropProfileConfig {
        TorMaildropProfileConfig::new([0x11; TOR_ONION_SERVICE_PUBLIC_KEY_BYTES], 4444).unwrap()
    }

    #[test]
    fn canonical_config_round_trips() {
        let config = config();
        let mut expected = vec![0x83, 0x01, 0x58, 0x20];
        expected.extend([0x11; TOR_ONION_SERVICE_PUBLIC_KEY_BYTES]);
        expected.extend([0x19, 0x11, 0x5c]);
        assert_eq!(config.encode().unwrap(), expected);
        assert_eq!(TorMaildropProfileConfig::decode(&expected).unwrap(), config);
        assert_eq!(
            config.onion_service_public_key(),
            [0x11; TOR_ONION_SERVICE_PUBLIC_KEY_BYTES]
        );
        assert_eq!(config.virtual_port(), 4444);
    }

    #[test]
    fn rejects_zero_virtual_port() {
        assert_eq!(
            TorMaildropProfileConfig::new([0x11; TOR_ONION_SERVICE_PUBLIC_KEY_BYTES], 0)
                .unwrap_err(),
            TorMaildropProfileConfigError::ZeroVirtualPort
        );
        assert_eq!(
            TorMaildropProfileConfig::new([0; TOR_ONION_SERVICE_PUBLIC_KEY_BYTES], 443)
                .unwrap_err(),
            TorMaildropProfileConfigError::InvalidOnionServicePublicKey
        );
    }

    #[test]
    fn rejects_invalid_encodings() {
        assert_eq!(
            TorMaildropProfileConfig::decode(&[0x83, 0x02, 0x58, 0x20]).unwrap_err(),
            TorMaildropProfileConfigError::UnsupportedSchemaVersion(2)
        );
        let mut invalid_key_length =
            vec![0x83, TOR_MAILDROP_PROFILE_CONFIG_SCHEMA_VERSION, 0x58, 0x1f];
        invalid_key_length.extend([0x11; TOR_ONION_SERVICE_PUBLIC_KEY_BYTES - 1]);
        invalid_key_length.extend([0x19, 0x01, 0xbb]);
        assert_eq!(
            TorMaildropProfileConfig::decode(&invalid_key_length).unwrap_err(),
            TorMaildropProfileConfigError::InvalidOnionServicePublicKeyLength
        );
        assert_eq!(
            TorMaildropProfileConfig::decode(&[0x9f, 0x01, 0xff]).unwrap_err(),
            TorMaildropProfileConfigError::InvalidShape
        );
        let mut encoded = config().encode().unwrap();
        encoded.push(0);
        assert_eq!(
            TorMaildropProfileConfig::decode(&encoded).unwrap_err(),
            TorMaildropProfileConfigError::TrailingBytes
        );
    }
}
