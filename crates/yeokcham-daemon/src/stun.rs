use std::net::{IpAddr, SocketAddr};

use stunclient::StunClient;
use tokio::net::UdpSocket;

pub const MAX_STUN_SERVERS: usize = 3;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct StunServer {
    endpoint: SocketAddr,
}

pub struct StunServers {
    servers: Vec<StunServer>,
}

impl StunServer {
    pub fn new(endpoint: SocketAddr) -> Result<Self, StunServerError> {
        validate_endpoint(endpoint)?;
        Ok(Self { endpoint })
    }

    #[must_use]
    pub const fn endpoint(self) -> SocketAddr {
        self.endpoint
    }
}

impl StunServers {
    pub fn new(servers: Vec<StunServer>) -> Result<Self, StunServerError> {
        if servers.is_empty() {
            return Err(StunServerError::EmptyServerList);
        }
        if servers.len() > MAX_STUN_SERVERS {
            return Err(StunServerError::TooManyServers);
        }
        if servers.iter().enumerate().any(|(index, server)| {
            servers[..index]
                .iter()
                .any(|previous| previous.endpoint == server.endpoint)
        }) {
            return Err(StunServerError::DuplicateServer);
        }
        Ok(Self { servers })
    }

    #[must_use]
    pub fn servers(&self) -> &[StunServer] {
        &self.servers
    }

    pub async fn discover_external_address(
        &self,
        local_bind: SocketAddr,
    ) -> Result<SocketAddr, StunServerError> {
        validate_endpoint(local_bind)?;
        for server in &self.servers {
            let socket = UdpSocket::bind(local_bind)
                .await
                .map_err(StunServerError::Bind)?;
            let client = StunClient::new(server.endpoint);
            if let Ok(address) = client.query_external_address_async(&socket).await
                && validate_endpoint(address).is_ok()
            {
                return Ok(address);
            }
        }
        Err(StunServerError::DiscoveryFailed)
    }
}

#[derive(Debug, thiserror::Error)]
pub enum StunServerError {
    #[error("STUN endpoint port must be nonzero")]
    ZeroPort,
    #[error("STUN endpoint must be a unicast address")]
    InvalidAddress,
    #[error("STUN server list must not be empty")]
    EmptyServerList,
    #[error("STUN server list exceeds the {MAX_STUN_SERVERS}-server limit")]
    TooManyServers,
    #[error("STUN server list contains duplicates")]
    DuplicateServer,
    #[error("failed to bind STUN discovery socket: {0}")]
    Bind(#[source] std::io::Error),
    #[error("all configured STUN servers failed discovery")]
    DiscoveryFailed,
}

fn validate_endpoint(endpoint: SocketAddr) -> Result<(), StunServerError> {
    if endpoint.port() == 0 {
        return Err(StunServerError::ZeroPort);
    }
    let address = endpoint.ip();
    if address.is_unspecified()
        || address.is_multicast()
        || matches!(address, IpAddr::V4(address) if address.is_broadcast())
    {
        return Err(StunServerError::InvalidAddress);
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::{StunServer, StunServerError, StunServers};

    fn server(address: &str) -> StunServer {
        StunServer::new(address.parse().unwrap()).unwrap()
    }

    #[test]
    fn validates_optional_user_supplied_stun_servers() {
        let first = server("192.0.2.1:3478");
        let second = server("192.0.2.2:3478");
        assert_eq!(StunServers::new(vec![first]).unwrap().servers(), &[first]);
        assert!(matches!(
            StunServers::new(Vec::new()),
            Err(StunServerError::EmptyServerList)
        ));
        assert!(matches!(
            StunServers::new(vec![first, first]),
            Err(StunServerError::DuplicateServer)
        ));
        assert!(matches!(
            StunServers::new(vec![first, second, first]),
            Err(StunServerError::DuplicateServer)
        ));
        assert!(matches!(
            StunServers::new(vec![
                first,
                second,
                server("192.0.2.3:3478"),
                server("192.0.2.4:3478")
            ]),
            Err(StunServerError::TooManyServers)
        ));
        assert!(matches!(
            StunServer::new("0.0.0.0:3478".parse().unwrap()),
            Err(StunServerError::InvalidAddress)
        ));
    }
}
