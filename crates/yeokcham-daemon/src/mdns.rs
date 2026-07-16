use std::net::{IpAddr, SocketAddr};

use mdns_sd::{Receiver, ResolvedService, ServiceDaemon, ServiceEvent, ServiceInfo};
use yeokcham_core::{ED25519_PUBLIC_KEY_BYTES, IdentityPublicKey};
use yeokcham_protocol::{DirectProfileConfig, DirectProfileConfigError, IdentityIdentifier};

pub const LAN_MDNS_SERVICE_TYPE: &str = "_yeokcham._udp.local.";
const IDENTITY_PROPERTY: &str = "identity";
const IDENTITY_HEX_BYTES: usize = ED25519_PUBLIC_KEY_BYTES * 2;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct LanPeer {
    identity: IdentityPublicKey,
    endpoints: Vec<DirectProfileConfig>,
}

impl LanPeer {
    #[must_use]
    pub const fn identity(&self) -> IdentityPublicKey {
        self.identity
    }

    #[must_use]
    pub fn endpoints(&self) -> &[DirectProfileConfig] {
        &self.endpoints
    }
}

pub struct LanPeerDiscovery {
    daemon: ServiceDaemon,
}

impl LanPeerDiscovery {
    pub fn start() -> Result<Self, LanPeerDiscoveryError> {
        let daemon = ServiceDaemon::new().map_err(LanPeerDiscoveryError::Mdns)?;
        Ok(Self { daemon })
    }

    pub fn advertise(
        &self,
        identity: IdentityPublicKey,
        endpoint: DirectProfileConfig,
    ) -> Result<String, LanPeerDiscoveryError> {
        reject_scoped_ipv6(endpoint.endpoint().ip())?;
        let identity_hex = encode_identity(identity);
        let identifier = IdentityIdentifier::derive(&identity);
        let instance = format!("yeokcham-{}", encode_hex(&identifier.as_bytes()[..8]));
        let hostname = format!("{instance}.local.");
        let properties = [(IDENTITY_PROPERTY, identity_hex.as_str())];
        let service = ServiceInfo::new(
            LAN_MDNS_SERVICE_TYPE,
            &instance,
            &hostname,
            endpoint.endpoint().ip(),
            endpoint.endpoint().port(),
            &properties[..],
        )
        .map_err(LanPeerDiscoveryError::Mdns)?;
        let fullname = service.get_fullname().to_owned();
        self.daemon
            .register(service)
            .map_err(LanPeerDiscoveryError::Mdns)?;
        Ok(fullname)
    }

    pub fn browse(&self) -> Result<Receiver<ServiceEvent>, LanPeerDiscoveryError> {
        self.daemon
            .browse(LAN_MDNS_SERVICE_TYPE)
            .map_err(LanPeerDiscoveryError::Mdns)
    }

    pub fn resolve(service: &ResolvedService) -> Result<LanPeer, LanPeerDiscoveryError> {
        if !service.is_valid() || service.ty_domain != LAN_MDNS_SERVICE_TYPE {
            return Err(LanPeerDiscoveryError::InvalidService);
        }
        resolve_parts(
            service.get_property_val_str(IDENTITY_PROPERTY),
            service.port,
            service
                .get_addresses()
                .iter()
                .map(mdns_sd::ScopedIp::to_ip_addr),
        )
    }

    pub fn unadvertise(&self, fullname: &str) -> Result<(), LanPeerDiscoveryError> {
        self.daemon
            .unregister(fullname)
            .map_err(LanPeerDiscoveryError::Mdns)?;
        Ok(())
    }

    pub fn shutdown(&self) -> Result<(), LanPeerDiscoveryError> {
        self.daemon
            .shutdown()
            .map_err(LanPeerDiscoveryError::Mdns)?;
        Ok(())
    }
}

#[derive(Debug, thiserror::Error)]
pub enum LanPeerDiscoveryError {
    #[error("mDNS operation failed: {0}")]
    Mdns(#[source] mdns_sd::Error),
    #[error("resolved mDNS service is invalid")]
    InvalidService,
    #[error("resolved mDNS service is missing its identity")]
    MissingIdentity,
    #[error("resolved mDNS identity is not canonical lowercase hexadecimal")]
    InvalidIdentityEncoding,
    #[error("resolved mDNS identity public key is invalid: {0}")]
    InvalidIdentity(#[source] yeokcham_core::IdentityPublicKeyError),
    #[error(
        "IPv6 link-local endpoints require an interface scope and cannot be represented in direct profiles"
    )]
    UnsupportedIpv6Scope,
    #[error("resolved mDNS endpoint is invalid: {0}")]
    InvalidEndpoint(#[source] DirectProfileConfigError),
}

fn encode_identity(identity: IdentityPublicKey) -> String {
    encode_hex(identity.as_bytes())
}

fn decode_identity(encoded: &str) -> Result<IdentityPublicKey, LanPeerDiscoveryError> {
    if encoded.len() != IDENTITY_HEX_BYTES
        || !encoded
            .bytes()
            .all(|byte| byte.is_ascii_lowercase() || byte.is_ascii_digit())
    {
        return Err(LanPeerDiscoveryError::InvalidIdentityEncoding);
    }
    let mut bytes = [0; ED25519_PUBLIC_KEY_BYTES];
    for (index, chunk) in encoded.as_bytes().chunks_exact(2).enumerate() {
        bytes[index] = (hex_nibble(chunk[0])? << 4) | hex_nibble(chunk[1])?;
    }
    IdentityPublicKey::from_bytes(bytes).map_err(LanPeerDiscoveryError::InvalidIdentity)
}

fn resolve_parts(
    identity_text: Option<&str>,
    port: u16,
    addresses: impl Iterator<Item = IpAddr>,
) -> Result<LanPeer, LanPeerDiscoveryError> {
    let identity = identity_text
        .ok_or(LanPeerDiscoveryError::MissingIdentity)
        .and_then(decode_identity)?;
    let endpoints = addresses
        .filter(|address| !is_scoped_ipv6(*address))
        .map(|address| DirectProfileConfig::new(SocketAddr::new(address, port)))
        .collect::<Result<Vec<_>, _>>()
        .map_err(LanPeerDiscoveryError::InvalidEndpoint)?;
    if endpoints.is_empty() {
        return Err(LanPeerDiscoveryError::InvalidService);
    }
    Ok(LanPeer {
        identity,
        endpoints,
    })
}

fn reject_scoped_ipv6(address: IpAddr) -> Result<(), LanPeerDiscoveryError> {
    if is_scoped_ipv6(address) {
        return Err(LanPeerDiscoveryError::UnsupportedIpv6Scope);
    }
    Ok(())
}

fn is_scoped_ipv6(address: IpAddr) -> bool {
    matches!(address, IpAddr::V6(address) if address.is_unicast_link_local())
}

fn hex_nibble(byte: u8) -> Result<u8, LanPeerDiscoveryError> {
    match byte {
        b'0'..=b'9' => Ok(byte - b'0'),
        b'a'..=b'f' => Ok(byte - b'a' + 10),
        _ => Err(LanPeerDiscoveryError::InvalidIdentityEncoding),
    }
}

fn encode_hex(bytes: &[u8]) -> String {
    const HEX: &[u8; 16] = b"0123456789abcdef";
    let mut encoded = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        encoded.push(char::from(HEX[usize::from(byte >> 4)]));
        encoded.push(char::from(HEX[usize::from(byte & 0x0f)]));
    }
    encoded
}

#[cfg(test)]
mod tests {
    use std::net::{IpAddr, SocketAddr};

    use yeokcham_core::IdentityPublicKey;

    use super::{LanPeerDiscoveryError, decode_identity, encode_identity, resolve_parts};

    #[test]
    fn identity_txt_round_trips_canonically() {
        let identity = IdentityPublicKey::from_bytes([
            0xd7, 0x5a, 0x98, 0x01, 0x82, 0xb1, 0x0a, 0xb7, 0xd5, 0x4b, 0xfe, 0xd3, 0xc9, 0x64,
            0x07, 0x3a, 0x0e, 0xe1, 0x72, 0xf3, 0xda, 0xa6, 0x23, 0x25, 0xaf, 0x02, 0x1a, 0x68,
            0xf7, 0x07, 0x51, 0x1a,
        ])
        .unwrap();
        let encoded = encode_identity(identity);
        assert_eq!(
            encoded,
            "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a"
        );
        assert_eq!(decode_identity(&encoded).unwrap(), identity);
    }

    #[test]
    fn rejects_noncanonical_identity_txt() {
        assert!(matches!(
            decode_identity("D75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a"),
            Err(LanPeerDiscoveryError::InvalidIdentityEncoding)
        ));
        assert!(matches!(
            decode_identity("d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511"),
            Err(LanPeerDiscoveryError::InvalidIdentityEncoding)
        ));
    }

    #[test]
    fn resolver_requires_a_valid_identity_and_endpoint() {
        let identity = "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a";
        let peer = resolve_parts(
            Some(identity),
            4444,
            ["192.0.2.1".parse().unwrap()].into_iter(),
        )
        .unwrap();
        assert_eq!(
            peer.endpoints()[0].endpoint(),
            "192.0.2.1:4444".parse::<SocketAddr>().unwrap()
        );
        assert!(matches!(
            resolve_parts(None, 4444, [IpAddr::from([192, 0, 2, 1])].into_iter()),
            Err(LanPeerDiscoveryError::MissingIdentity)
        ));
        assert!(matches!(
            resolve_parts(
                Some(identity),
                0,
                [IpAddr::from([192, 0, 2, 1])].into_iter()
            ),
            Err(LanPeerDiscoveryError::InvalidEndpoint(_))
        ));
        assert!(matches!(
            resolve_parts(
                Some(identity),
                4444,
                ["fe80::1".parse().unwrap()].into_iter()
            ),
            Err(LanPeerDiscoveryError::InvalidService)
        ));
    }
}
