use std::net::{IpAddr, SocketAddr};

use igd_next::{
    PortMappingProtocol, SearchOptions,
    aio::{
        Gateway,
        tokio::{Tokio, search_gateway},
    },
};

pub const MAX_UPNP_LEASE_SECONDS: u32 = 3_600;
const MAPPING_DESCRIPTION: &str = "yeokcham direct QUIC";

pub struct UpnpMappingRequest {
    internal_endpoint: SocketAddr,
    external_port: u16,
    lease_seconds: u32,
}

pub struct UpnpMapping {
    gateway: Gateway<Tokio>,
    external_port: u16,
}

impl UpnpMappingRequest {
    pub fn new(
        internal_endpoint: SocketAddr,
        external_port: u16,
        lease_seconds: u32,
    ) -> Result<Self, UpnpMappingError> {
        validate_endpoint(internal_endpoint)?;
        if external_port == 0 {
            return Err(UpnpMappingError::ZeroExternalPort);
        }
        if !(1..=MAX_UPNP_LEASE_SECONDS).contains(&lease_seconds) {
            return Err(UpnpMappingError::InvalidLease);
        }
        Ok(Self {
            internal_endpoint,
            external_port,
            lease_seconds,
        })
    }

    pub async fn create(self) -> Result<UpnpMapping, UpnpMappingError> {
        let gateway = search_gateway(SearchOptions::default())
            .await
            .map_err(UpnpMappingError::Discovery)?;
        gateway
            .add_port(
                PortMappingProtocol::UDP,
                self.external_port,
                self.internal_endpoint,
                self.lease_seconds,
                MAPPING_DESCRIPTION,
            )
            .await
            .map_err(UpnpMappingError::Add)?;
        Ok(UpnpMapping {
            gateway,
            external_port: self.external_port,
        })
    }
}

impl UpnpMapping {
    #[must_use]
    pub const fn external_port(&self) -> u16 {
        self.external_port
    }

    pub async fn remove(&self) -> Result<(), UpnpMappingError> {
        self.gateway
            .remove_port(PortMappingProtocol::UDP, self.external_port)
            .await
            .map_err(UpnpMappingError::Remove)
    }
}

#[derive(Debug, thiserror::Error)]
pub enum UpnpMappingError {
    #[error("UPnP internal endpoint port must be nonzero")]
    ZeroInternalPort,
    #[error("UPnP internal endpoint must be a unicast address")]
    InvalidInternalAddress,
    #[error("UPnP external port must be nonzero")]
    ZeroExternalPort,
    #[error("UPnP lease must be between one second and {MAX_UPNP_LEASE_SECONDS} seconds")]
    InvalidLease,
    #[error("UPnP gateway discovery failed: {0}")]
    Discovery(#[source] igd_next::SearchError),
    #[error("UPnP port mapping creation failed: {0}")]
    Add(#[source] igd_next::AddPortError),
    #[error("UPnP port mapping removal failed: {0}")]
    Remove(#[source] igd_next::RemovePortError),
}

fn validate_endpoint(endpoint: SocketAddr) -> Result<(), UpnpMappingError> {
    if endpoint.port() == 0 {
        return Err(UpnpMappingError::ZeroInternalPort);
    }
    let address = endpoint.ip();
    if address.is_unspecified()
        || address.is_multicast()
        || matches!(address, IpAddr::V4(address) if address.is_broadcast())
    {
        return Err(UpnpMappingError::InvalidInternalAddress);
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::{MAX_UPNP_LEASE_SECONDS, UpnpMappingError, UpnpMappingRequest};

    #[test]
    fn validates_explicit_bounded_udp_mapping_requests() {
        assert!(UpnpMappingRequest::new("192.168.1.8:4242".parse().unwrap(), 4242, 600).is_ok());
        assert!(matches!(
            UpnpMappingRequest::new("192.168.1.8:0".parse().unwrap(), 4242, 600),
            Err(UpnpMappingError::ZeroInternalPort)
        ));
        assert!(matches!(
            UpnpMappingRequest::new("0.0.0.0:4242".parse().unwrap(), 4242, 600),
            Err(UpnpMappingError::InvalidInternalAddress)
        ));
        assert!(matches!(
            UpnpMappingRequest::new("192.168.1.8:4242".parse().unwrap(), 0, 600),
            Err(UpnpMappingError::ZeroExternalPort)
        ));
        assert!(matches!(
            UpnpMappingRequest::new("192.168.1.8:4242".parse().unwrap(), 4242, 0),
            Err(UpnpMappingError::InvalidLease)
        ));
        assert!(matches!(
            UpnpMappingRequest::new(
                "192.168.1.8:4242".parse().unwrap(),
                4242,
                MAX_UPNP_LEASE_SECONDS + 1
            ),
            Err(UpnpMappingError::InvalidLease)
        ));
    }
}
