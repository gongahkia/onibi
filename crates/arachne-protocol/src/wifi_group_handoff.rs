use std::fmt;

use minicbor::{Decoder, Encoder};
use zeroize::Zeroizing;

use crate::{DirectProfileConfig, DirectProfileConfigError, LocalMeshTransportKind};

pub const WIFI_GROUP_CONFIGURATION_SCHEMA_VERSION: u8 = 1;
pub const MAX_WIFI_GROUP_IDENTIFIER_BYTES: usize = 64;
pub const MAX_WIFI_GROUP_CREDENTIAL_BYTES: usize = 128;
const WIFI_GROUP_CONFIGURATION_FIELDS: u64 = 5;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum WifiGroupRole {
    Owner = 1,
    Client = 2,
}

impl TryFrom<u8> for WifiGroupRole {
    type Error = WifiGroupConfigurationError;

    fn try_from(value: u8) -> Result<Self, Self::Error> {
        match value {
            1 => Ok(Self::Owner),
            2 => Ok(Self::Client),
            _ => Err(WifiGroupConfigurationError::InvalidRole(value)),
        }
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct WifiGroupConfiguration {
    transport: LocalMeshTransportKind,
    role: WifiGroupRole,
    identifier: String,
    direct_profile: DirectProfileConfig,
}

impl WifiGroupConfiguration {
    pub fn new(
        transport: LocalMeshTransportKind,
        role: WifiGroupRole,
        identifier: String,
        direct_profile: DirectProfileConfig,
    ) -> Result<Self, WifiGroupConfigurationError> {
        if !matches!(
            transport,
            LocalMeshTransportKind::WifiDirect | LocalMeshTransportKind::WifiHotspot
        ) {
            return Err(WifiGroupConfigurationError::NonWifiTransport(transport));
        }
        validate_identifier(&identifier)?;
        Ok(Self {
            transport,
            role,
            identifier,
            direct_profile,
        })
    }

    #[must_use]
    pub const fn transport(&self) -> LocalMeshTransportKind {
        self.transport
    }

    #[must_use]
    pub const fn role(&self) -> WifiGroupRole {
        self.role
    }

    #[must_use]
    pub fn identifier(&self) -> &str {
        &self.identifier
    }

    #[must_use]
    pub const fn direct_profile(&self) -> DirectProfileConfig {
        self.direct_profile
    }

    pub fn encode(&self) -> Result<Vec<u8>, WifiGroupConfigurationError> {
        validate_identifier(&self.identifier)?;
        let direct_profile = self
            .direct_profile
            .encode()
            .map_err(WifiGroupConfigurationError::DirectProfile)?;
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(WIFI_GROUP_CONFIGURATION_FIELDS)
            .map_err(|_| WifiGroupConfigurationError::Encode)?
            .u8(WIFI_GROUP_CONFIGURATION_SCHEMA_VERSION)
            .map_err(|_| WifiGroupConfigurationError::Encode)?
            .u8(self.transport as u8)
            .map_err(|_| WifiGroupConfigurationError::Encode)?
            .u8(self.role as u8)
            .map_err(|_| WifiGroupConfigurationError::Encode)?
            .str(&self.identifier)
            .map_err(|_| WifiGroupConfigurationError::Encode)?
            .bytes(&direct_profile)
            .map_err(|_| WifiGroupConfigurationError::Encode)?;
        Ok(encoder.into_writer())
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, WifiGroupConfigurationError> {
        let mut decoder = Decoder::new(encoded);
        if decoder
            .array()
            .map_err(|_| WifiGroupConfigurationError::Decode)?
            != Some(WIFI_GROUP_CONFIGURATION_FIELDS)
        {
            return Err(WifiGroupConfigurationError::InvalidShape);
        }
        let version = decoder
            .u8()
            .map_err(|_| WifiGroupConfigurationError::Decode)?;
        if version != WIFI_GROUP_CONFIGURATION_SCHEMA_VERSION {
            return Err(WifiGroupConfigurationError::UnsupportedSchemaVersion(
                version,
            ));
        }
        let transport = match decoder
            .u8()
            .map_err(|_| WifiGroupConfigurationError::Decode)?
        {
            2 => LocalMeshTransportKind::WifiHotspot,
            3 => LocalMeshTransportKind::WifiDirect,
            value => return Err(WifiGroupConfigurationError::NonWifiTransportValue(value)),
        };
        let role = WifiGroupRole::try_from(
            decoder
                .u8()
                .map_err(|_| WifiGroupConfigurationError::Decode)?,
        )?;
        let identifier = decoder
            .str()
            .map_err(|_| WifiGroupConfigurationError::Decode)?
            .to_owned();
        let direct_profile = DirectProfileConfig::decode(
            decoder
                .bytes()
                .map_err(|_| WifiGroupConfigurationError::Decode)?,
        )
        .map_err(WifiGroupConfigurationError::DirectProfile)?;
        if decoder.position() != encoded.len() {
            return Err(WifiGroupConfigurationError::TrailingBytes);
        }
        let configuration = Self::new(transport, role, identifier, direct_profile)?;
        if configuration.encode()? != encoded {
            return Err(WifiGroupConfigurationError::NonCanonicalEncoding);
        }
        Ok(configuration)
    }
}

#[derive(Clone, Eq, PartialEq)]
pub struct WifiGroupCredential(Zeroizing<Vec<u8>>);

impl WifiGroupCredential {
    pub fn new(credential: Vec<u8>) -> Result<Self, WifiGroupCredentialError> {
        if credential.is_empty() {
            return Err(WifiGroupCredentialError::Empty);
        }
        if credential.len() > MAX_WIFI_GROUP_CREDENTIAL_BYTES {
            return Err(WifiGroupCredentialError::TooLarge);
        }
        Ok(Self(Zeroizing::new(credential)))
    }

    #[must_use]
    pub fn as_bytes(&self) -> &[u8] {
        &self.0
    }
}

impl fmt::Debug for WifiGroupCredential {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("WifiGroupCredential(REDACTED)")
    }
}

#[derive(Clone, Eq, PartialEq)]
pub struct WifiGroupHandoff {
    configuration: WifiGroupConfiguration,
    credential: WifiGroupCredential,
}

impl WifiGroupHandoff {
    #[must_use]
    pub const fn new(
        configuration: WifiGroupConfiguration,
        credential: WifiGroupCredential,
    ) -> Self {
        Self {
            configuration,
            credential,
        }
    }

    #[must_use]
    pub const fn configuration(&self) -> &WifiGroupConfiguration {
        &self.configuration
    }

    #[must_use]
    pub const fn credential(&self) -> &WifiGroupCredential {
        &self.credential
    }
}

impl fmt::Debug for WifiGroupHandoff {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("WifiGroupHandoff")
            .field("configuration", &self.configuration)
            .field("credential", &"REDACTED")
            .finish()
    }
}

#[derive(Clone, Eq, PartialEq)]
pub struct WifiGroupBootstrap {
    transport: LocalMeshTransportKind,
    identifier: String,
    credential: WifiGroupCredential,
}

impl WifiGroupBootstrap {
    pub fn new(
        transport: LocalMeshTransportKind,
        identifier: String,
        credential: WifiGroupCredential,
    ) -> Result<Self, WifiGroupConfigurationError> {
        if !matches!(
            transport,
            LocalMeshTransportKind::WifiDirect | LocalMeshTransportKind::WifiHotspot
        ) {
            return Err(WifiGroupConfigurationError::NonWifiTransport(transport));
        }
        validate_identifier(&identifier)?;
        Ok(Self {
            transport,
            identifier,
            credential,
        })
    }

    #[must_use]
    pub const fn transport(&self) -> LocalMeshTransportKind {
        self.transport
    }

    #[must_use]
    pub const fn role(&self) -> WifiGroupRole {
        WifiGroupRole::Owner
    }

    #[must_use]
    pub fn identifier(&self) -> &str {
        &self.identifier
    }

    #[must_use]
    pub const fn credential(&self) -> &WifiGroupCredential {
        &self.credential
    }

    pub fn activate(
        &self,
        direct_profile: DirectProfileConfig,
    ) -> Result<WifiGroupHandoff, WifiGroupConfigurationError> {
        Ok(WifiGroupHandoff::new(
            WifiGroupConfiguration::new(
                self.transport,
                WifiGroupRole::Owner,
                self.identifier.clone(),
                direct_profile,
            )?,
            self.credential.clone(),
        ))
    }
}

impl fmt::Debug for WifiGroupBootstrap {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("WifiGroupBootstrap")
            .field("transport", &self.transport)
            .field("role", &WifiGroupRole::Owner)
            .field("identifier", &self.identifier)
            .field("credential", &"REDACTED")
            .finish()
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum WifiGroupConfigurationError {
    #[error("unsupported Wi-Fi group configuration schema version: {0}")]
    UnsupportedSchemaVersion(u8),
    #[error("Wi-Fi group configuration must be a five-element definite-length CBOR array")]
    InvalidShape,
    #[error("Wi-Fi group configuration requires Wi-Fi Direct or Wi-Fi hotspot, got {0:?}")]
    NonWifiTransport(LocalMeshTransportKind),
    #[error("Wi-Fi group configuration has a non-Wi-Fi transport value: {0}")]
    NonWifiTransportValue(u8),
    #[error("Wi-Fi group role is invalid: {0}")]
    InvalidRole(u8),
    #[error("Wi-Fi group identifier is invalid")]
    InvalidIdentifier,
    #[error("Wi-Fi group direct profile is invalid: {0}")]
    DirectProfile(#[source] DirectProfileConfigError),
    #[error("CBOR encoding failed")]
    Encode,
    #[error("CBOR decoding failed")]
    Decode,
    #[error("trailing bytes after Wi-Fi group configuration")]
    TrailingBytes,
    #[error("Wi-Fi group configuration is not canonically encoded")]
    NonCanonicalEncoding,
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum WifiGroupCredentialError {
    #[error("Wi-Fi group credential is required")]
    Empty,
    #[error("Wi-Fi group credential exceeds the configured limit")]
    TooLarge,
}

fn validate_identifier(identifier: &str) -> Result<(), WifiGroupConfigurationError> {
    if identifier.is_empty()
        || identifier.len() > MAX_WIFI_GROUP_IDENTIFIER_BYTES
        || identifier.bytes().any(|byte| !byte.is_ascii_graphic())
    {
        return Err(WifiGroupConfigurationError::InvalidIdentifier);
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::{
        WifiGroupBootstrap, WifiGroupConfiguration, WifiGroupConfigurationError,
        WifiGroupCredential, WifiGroupCredentialError, WifiGroupHandoff, WifiGroupRole,
    };
    use crate::{DirectProfileConfig, LocalMeshTransportKind};

    fn profile() -> DirectProfileConfig {
        DirectProfileConfig::new("192.0.2.1:4444".parse().unwrap()).unwrap()
    }

    #[test]
    fn encodes_a_versioned_group_configuration_without_credentials() {
        let configuration = WifiGroupConfiguration::new(
            LocalMeshTransportKind::WifiDirect,
            WifiGroupRole::Owner,
            "arachne-p2p".to_owned(),
            profile(),
        )
        .unwrap();
        let encoded = configuration.encode().unwrap();
        assert_eq!(
            WifiGroupConfiguration::decode(&encoded).unwrap(),
            configuration
        );
        assert!(
            !encoded
                .windows(b"secret".len())
                .any(|window| window == b"secret")
        );
    }

    #[test]
    fn rejects_non_wifi_or_invalid_group_configuration() {
        assert_eq!(
            WifiGroupConfiguration::new(
                LocalMeshTransportKind::Bluetooth,
                WifiGroupRole::Client,
                "arachne".to_owned(),
                profile(),
            )
            .unwrap_err(),
            WifiGroupConfigurationError::NonWifiTransport(LocalMeshTransportKind::Bluetooth)
        );
        assert_eq!(
            WifiGroupConfiguration::new(
                LocalMeshTransportKind::WifiHotspot,
                WifiGroupRole::Client,
                " ".to_owned(),
                profile(),
            )
            .unwrap_err(),
            WifiGroupConfigurationError::InvalidIdentifier
        );
    }

    #[test]
    fn redacts_credentials_from_handoff_debug_output() {
        let credential = WifiGroupCredential::new(b"secret-value".to_vec()).unwrap();
        assert_eq!(format!("{credential:?}"), "WifiGroupCredential(REDACTED)");
        assert_eq!(
            WifiGroupCredential::new(Vec::new()).unwrap_err(),
            WifiGroupCredentialError::Empty
        );
        let handoff = WifiGroupHandoff::new(
            WifiGroupConfiguration::new(
                LocalMeshTransportKind::WifiHotspot,
                WifiGroupRole::Owner,
                "arachne-hotspot".to_owned(),
                profile(),
            )
            .unwrap(),
            credential,
        );
        assert!(!format!("{handoff:?}").contains("secret-value"));
    }

    #[test]
    fn activates_owner_bootstrap_only_after_the_endpoint_is_known() {
        let bootstrap = WifiGroupBootstrap::new(
            LocalMeshTransportKind::WifiHotspot,
            "arachne-hotspot".to_owned(),
            WifiGroupCredential::new(b"secret-value".to_vec()).unwrap(),
        )
        .unwrap();
        let handoff = bootstrap.activate(profile()).unwrap();

        assert_eq!(bootstrap.role(), WifiGroupRole::Owner);
        assert_eq!(handoff.configuration().role(), WifiGroupRole::Owner);
        assert_eq!(handoff.configuration().identifier(), "arachne-hotspot");
        assert_eq!(handoff.configuration().direct_profile(), profile());
        assert!(!format!("{bootstrap:?}").contains("secret-value"));
        assert_eq!(
            WifiGroupBootstrap::new(
                LocalMeshTransportKind::Bluetooth,
                "arachne".to_owned(),
                WifiGroupCredential::new(b"secret-value".to_vec()).unwrap(),
            )
            .unwrap_err(),
            WifiGroupConfigurationError::NonWifiTransport(LocalMeshTransportKind::Bluetooth)
        );
    }
}
