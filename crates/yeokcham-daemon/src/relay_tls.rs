use sha2::{Digest, Sha256};

pub const RELAY_TLS_PIN_BYTES: usize = 32;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RelayTlsPin([u8; RELAY_TLS_PIN_BYTES]);

impl RelayTlsPin {
    #[must_use]
    pub fn from_certificate_der(certificate_der: &[u8]) -> Self {
        let digest = Sha256::digest(certificate_der);
        let mut pin = [0; RELAY_TLS_PIN_BYTES];
        pin.copy_from_slice(&digest);
        Self(pin)
    }

    #[must_use]
    pub const fn as_bytes(&self) -> &[u8; RELAY_TLS_PIN_BYTES] {
        &self.0
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct RelayTlsEndpoint {
    server_name: String,
    port: u16,
    pin: RelayTlsPin,
}

impl RelayTlsEndpoint {
    pub fn new(
        server_name: String,
        port: u16,
        pin: RelayTlsPin,
    ) -> Result<Self, RelayTlsEndpointError> {
        if port == 0 {
            return Err(RelayTlsEndpointError::ZeroPort);
        }
        if !is_valid_server_name(&server_name) {
            return Err(RelayTlsEndpointError::InvalidServerName);
        }
        Ok(Self {
            server_name,
            port,
            pin,
        })
    }

    #[must_use]
    pub fn server_name(&self) -> &str {
        &self.server_name
    }

    #[must_use]
    pub const fn port(&self) -> u16 {
        self.port
    }

    #[must_use]
    pub const fn pin(&self) -> RelayTlsPin {
        self.pin
    }

    pub fn verify_presented_certificate(
        &self,
        certificate_der: &[u8],
    ) -> Result<(), RelayTlsEndpointError> {
        if RelayTlsPin::from_certificate_der(certificate_der) != self.pin {
            return Err(RelayTlsEndpointError::PinMismatch);
        }
        Ok(())
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum RelayTlsEndpointError {
    #[error("relay TLS endpoint port must be nonzero")]
    ZeroPort,
    #[error("relay TLS endpoint server name is invalid")]
    InvalidServerName,
    #[error("relay TLS endpoint certificate does not match its configured pin")]
    PinMismatch,
}

fn is_valid_server_name(server_name: &str) -> bool {
    !server_name.is_empty()
        && server_name.len() <= 253
        && !server_name.starts_with('.')
        && !server_name.ends_with('.')
        && server_name.bytes().all(|byte| {
            byte.is_ascii_lowercase() || byte.is_ascii_digit() || matches!(byte, b'.' | b'-')
        })
}

#[cfg(test)]
mod tests {
    use super::{RelayTlsEndpoint, RelayTlsEndpointError, RelayTlsPin};

    #[test]
    fn pins_the_exact_relay_leaf_certificate() {
        let certificate = b"relay leaf certificate";
        let pin = RelayTlsPin::from_certificate_der(certificate);
        let endpoint = RelayTlsEndpoint::new("relay.example".to_owned(), 443, pin).unwrap();
        assert_eq!(endpoint.server_name(), "relay.example");
        assert!(endpoint.verify_presented_certificate(certificate).is_ok());
        assert_eq!(
            endpoint.verify_presented_certificate(b"substituted certificate"),
            Err(RelayTlsEndpointError::PinMismatch)
        );
    }

    #[test]
    fn rejects_invalid_endpoint_configuration() {
        let pin = RelayTlsPin::from_certificate_der(b"certificate");
        assert_eq!(
            RelayTlsEndpoint::new("relay.example".to_owned(), 0, pin),
            Err(RelayTlsEndpointError::ZeroPort)
        );
        assert_eq!(
            RelayTlsEndpoint::new("Relay.Example".to_owned(), 443, pin),
            Err(RelayTlsEndpointError::InvalidServerName)
        );
    }
}
