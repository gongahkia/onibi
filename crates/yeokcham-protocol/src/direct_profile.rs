use std::net::{IpAddr, Ipv4Addr, Ipv6Addr, SocketAddr, SocketAddrV4, SocketAddrV6};

use minicbor::{Decoder, Encoder};

pub const DIRECT_PROFILE_CONFIG_SCHEMA_VERSION: u8 = 1;
const DIRECT_PROFILE_CONFIG_FIELDS: u64 = 4;
const IPV4_ADDRESS_FAMILY: u8 = 4;
const IPV6_ADDRESS_FAMILY: u8 = 6;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct DirectProfileConfig {
    endpoint: SocketAddr,
}

impl DirectProfileConfig {
    pub fn new(endpoint: SocketAddr) -> Result<Self, DirectProfileConfigError> {
        validate_endpoint(endpoint)?;
        Ok(Self { endpoint })
    }

    #[must_use]
    pub const fn endpoint(self) -> SocketAddr {
        self.endpoint
    }

    pub fn encode(self) -> Result<Vec<u8>, DirectProfileConfigError> {
        validate_endpoint(self.endpoint)?;
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(DIRECT_PROFILE_CONFIG_FIELDS)
            .map_err(|_| DirectProfileConfigError::Encode)?
            .u8(DIRECT_PROFILE_CONFIG_SCHEMA_VERSION)
            .map_err(|_| DirectProfileConfigError::Encode)?;
        match self.endpoint {
            SocketAddr::V4(endpoint) => {
                let octets = endpoint.ip().octets();
                encoder
                    .u8(IPV4_ADDRESS_FAMILY)
                    .map_err(|_| DirectProfileConfigError::Encode)?
                    .bytes(&octets)
                    .map_err(|_| DirectProfileConfigError::Encode)?
                    .u16(endpoint.port())
                    .map_err(|_| DirectProfileConfigError::Encode)?;
            }
            SocketAddr::V6(endpoint) => {
                let octets = endpoint.ip().octets();
                encoder
                    .u8(IPV6_ADDRESS_FAMILY)
                    .map_err(|_| DirectProfileConfigError::Encode)?
                    .bytes(&octets)
                    .map_err(|_| DirectProfileConfigError::Encode)?
                    .u16(endpoint.port())
                    .map_err(|_| DirectProfileConfigError::Encode)?;
            }
        }
        Ok(encoder.into_writer())
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, DirectProfileConfigError> {
        let mut decoder = Decoder::new(encoded);
        if decoder
            .array()
            .map_err(|_| DirectProfileConfigError::Decode)?
            != Some(DIRECT_PROFILE_CONFIG_FIELDS)
        {
            return Err(DirectProfileConfigError::InvalidShape);
        }
        let schema_version = decoder.u8().map_err(|_| DirectProfileConfigError::Decode)?;
        if schema_version != DIRECT_PROFILE_CONFIG_SCHEMA_VERSION {
            return Err(DirectProfileConfigError::UnsupportedSchemaVersion(
                schema_version,
            ));
        }
        let address_family = decoder.u8().map_err(|_| DirectProfileConfigError::Decode)?;
        let endpoint = match address_family {
            IPV4_ADDRESS_FAMILY => {
                let octets: [u8; 4] = decoder
                    .bytes()
                    .map_err(|_| DirectProfileConfigError::Decode)?
                    .try_into()
                    .map_err(|_| DirectProfileConfigError::InvalidAddressLength)?;
                let port = decoder
                    .u16()
                    .map_err(|_| DirectProfileConfigError::Decode)?;
                SocketAddr::V4(SocketAddrV4::new(Ipv4Addr::from(octets), port))
            }
            IPV6_ADDRESS_FAMILY => {
                let octets: [u8; 16] = decoder
                    .bytes()
                    .map_err(|_| DirectProfileConfigError::Decode)?
                    .try_into()
                    .map_err(|_| DirectProfileConfigError::InvalidAddressLength)?;
                let port = decoder
                    .u16()
                    .map_err(|_| DirectProfileConfigError::Decode)?;
                SocketAddr::V6(SocketAddrV6::new(Ipv6Addr::from(octets), port, 0, 0))
            }
            _ => {
                return Err(DirectProfileConfigError::UnsupportedAddressFamily(
                    address_family,
                ));
            }
        };
        if decoder.position() != encoded.len() {
            return Err(DirectProfileConfigError::TrailingBytes);
        }
        Self::new(endpoint)
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum DirectProfileConfigError {
    #[error("unsupported direct-profile configuration schema version: {0}")]
    UnsupportedSchemaVersion(u8),
    #[error("unsupported direct endpoint address family: {0}")]
    UnsupportedAddressFamily(u8),
    #[error("direct endpoint address has an invalid length")]
    InvalidAddressLength,
    #[error("direct endpoint port must be nonzero")]
    ZeroPort,
    #[error("direct endpoint must not use an unspecified address")]
    UnspecifiedAddress,
    #[error("direct endpoint must not use a multicast address")]
    MulticastAddress,
    #[error("direct endpoint must not use the IPv4 broadcast address")]
    BroadcastAddress,
    #[error("direct IPv6 endpoint must not include flow or scope metadata")]
    UnsupportedIpv6Metadata,
    #[error("direct-profile configuration must be a four-element definite-length CBOR array")]
    InvalidShape,
    #[error("CBOR decoding failed")]
    Decode,
    #[error("CBOR encoding failed")]
    Encode,
    #[error("trailing bytes after direct-profile configuration")]
    TrailingBytes,
}

fn validate_endpoint(endpoint: SocketAddr) -> Result<(), DirectProfileConfigError> {
    if endpoint.port() == 0 {
        return Err(DirectProfileConfigError::ZeroPort);
    }
    let address = endpoint.ip();
    if address.is_unspecified() {
        return Err(DirectProfileConfigError::UnspecifiedAddress);
    }
    if address.is_multicast() {
        return Err(DirectProfileConfigError::MulticastAddress);
    }
    if matches!(address, IpAddr::V4(address) if address.is_broadcast()) {
        return Err(DirectProfileConfigError::BroadcastAddress);
    }
    if let SocketAddr::V6(endpoint) = endpoint
        && (endpoint.flowinfo() != 0 || endpoint.scope_id() != 0)
    {
        return Err(DirectProfileConfigError::UnsupportedIpv6Metadata);
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use std::net::{Ipv6Addr, SocketAddr, SocketAddrV6};

    use super::{
        DIRECT_PROFILE_CONFIG_SCHEMA_VERSION, DirectProfileConfig, DirectProfileConfigError,
    };

    fn ipv4_config() -> DirectProfileConfig {
        DirectProfileConfig::new("192.0.2.1:4444".parse().unwrap()).unwrap()
    }

    #[test]
    fn canonical_ip_endpoints_round_trip() {
        let ipv4 = ipv4_config();
        let expected_ipv4 = [0x84, 0x01, 0x04, 0x44, 192, 0, 2, 1, 0x19, 0x11, 0x5c];
        assert_eq!(ipv4.encode().unwrap(), expected_ipv4);
        assert_eq!(DirectProfileConfig::decode(&expected_ipv4).unwrap(), ipv4);

        let ipv6 = DirectProfileConfig::new("[2001:db8::1]:443".parse().unwrap()).unwrap();
        let mut expected_ipv6 = vec![0x84, 0x01, 0x06, 0x50];
        expected_ipv6.extend(Ipv6Addr::new(0x2001, 0x0db8, 0, 0, 0, 0, 0, 1).octets());
        expected_ipv6.extend([0x19, 0x01, 0xbb]);
        assert_eq!(ipv6.encode().unwrap(), expected_ipv6);
        assert_eq!(DirectProfileConfig::decode(&expected_ipv6).unwrap(), ipv6);
    }

    #[test]
    fn rejects_invalid_endpoints() {
        assert_eq!(
            DirectProfileConfig::new("192.0.2.1:0".parse().unwrap()).unwrap_err(),
            DirectProfileConfigError::ZeroPort
        );
        assert_eq!(
            DirectProfileConfig::new("0.0.0.0:443".parse().unwrap()).unwrap_err(),
            DirectProfileConfigError::UnspecifiedAddress
        );
        assert_eq!(
            DirectProfileConfig::new("224.0.0.1:443".parse().unwrap()).unwrap_err(),
            DirectProfileConfigError::MulticastAddress
        );
        assert_eq!(
            DirectProfileConfig::new("255.255.255.255:443".parse().unwrap()).unwrap_err(),
            DirectProfileConfigError::BroadcastAddress
        );
        assert_eq!(
            DirectProfileConfig::new(SocketAddr::V6(SocketAddrV6::new(
                Ipv6Addr::LOCALHOST,
                443,
                1,
                0,
            )))
            .unwrap_err(),
            DirectProfileConfigError::UnsupportedIpv6Metadata
        );
    }

    #[test]
    fn rejects_invalid_encodings() {
        assert_eq!(
            DirectProfileConfig::decode(&[0x84, 0x02, 0x04, 0x44, 192, 0, 2, 1, 0x19, 0x01, 0xbb])
                .unwrap_err(),
            DirectProfileConfigError::UnsupportedSchemaVersion(2)
        );
        assert_eq!(
            DirectProfileConfig::decode(&[0x84, DIRECT_PROFILE_CONFIG_SCHEMA_VERSION, 5])
                .unwrap_err(),
            DirectProfileConfigError::UnsupportedAddressFamily(5)
        );
        assert_eq!(
            DirectProfileConfig::decode(&[
                0x84,
                DIRECT_PROFILE_CONFIG_SCHEMA_VERSION,
                4,
                0x43,
                192,
                0,
                2,
                0x19,
                0x01,
                0xbb,
            ])
            .unwrap_err(),
            DirectProfileConfigError::InvalidAddressLength
        );
        assert_eq!(
            DirectProfileConfig::decode(&[0x9f, 0x01, 0x04, 0xff]).unwrap_err(),
            DirectProfileConfigError::InvalidShape
        );
        let mut encoded = ipv4_config().encode().unwrap();
        encoded.push(0);
        assert_eq!(
            DirectProfileConfig::decode(&encoded).unwrap_err(),
            DirectProfileConfigError::TrailingBytes
        );
    }
}
