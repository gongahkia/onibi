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
    use tokio::net::UdpSocket;

    const MAGIC_COOKIE: [u8; 4] = [0x21, 0x12, 0xa4, 0x42];

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

    #[tokio::test]
    async fn discovers_an_external_address_through_a_stun_server() {
        let external = "198.51.100.7:52345".parse().unwrap();
        let (endpoint, responder) = start_stun_server(external).await;
        let servers = StunServers::new(vec![StunServer::new(endpoint).unwrap()]).unwrap();

        assert_eq!(
            servers
                .discover_external_address(local_bind())
                .await
                .unwrap(),
            external
        );
        responder.await.unwrap();
    }

    #[tokio::test]
    async fn skips_an_invalid_stun_mapping_and_uses_the_next_server() {
        let (invalid_endpoint, invalid_responder) =
            start_stun_server("0.0.0.0:3478".parse().unwrap()).await;
        let external = "198.51.100.8:52346".parse().unwrap();
        let (valid_endpoint, valid_responder) = start_stun_server(external).await;
        let servers = StunServers::new(vec![
            StunServer::new(invalid_endpoint).unwrap(),
            StunServer::new(valid_endpoint).unwrap(),
        ])
        .unwrap();

        assert_eq!(
            servers
                .discover_external_address(local_bind())
                .await
                .unwrap(),
            external
        );
        invalid_responder.await.unwrap();
        valid_responder.await.unwrap();
    }

    #[tokio::test]
    async fn rejects_invalid_stun_mappings_when_no_server_succeeds() {
        let (endpoint, responder) = start_stun_server("0.0.0.0:3478".parse().unwrap()).await;
        let servers = StunServers::new(vec![StunServer::new(endpoint).unwrap()]).unwrap();

        assert!(matches!(
            servers.discover_external_address(local_bind()).await,
            Err(StunServerError::DiscoveryFailed)
        ));
        responder.await.unwrap();
    }

    async fn start_stun_server(
        external: std::net::SocketAddr,
    ) -> (std::net::SocketAddr, tokio::task::JoinHandle<()>) {
        let socket = UdpSocket::bind("127.0.0.1:0").await.unwrap();
        let endpoint = socket.local_addr().unwrap();
        let responder = tokio::spawn(async move {
            let mut request = [0_u8; 256];
            let (length, peer) = socket.recv_from(&mut request).await.unwrap();
            let response = binding_success_response(&request[..length], external);
            socket.send_to(&response, peer).await.unwrap();
        });
        (endpoint, responder)
    }

    fn local_bind() -> std::net::SocketAddr {
        std::net::UdpSocket::bind("127.0.0.1:0")
            .unwrap()
            .local_addr()
            .unwrap()
    }

    fn binding_success_response(request: &[u8], external: std::net::SocketAddr) -> Vec<u8> {
        assert!(request.len() >= 20);
        let port = external.port();
        let external = match external.ip() {
            std::net::IpAddr::V4(address) => address.octets(),
            std::net::IpAddr::V6(_) => panic!("test STUN server requires IPv4"),
        };
        let mut response = vec![0; 32];
        response[..2].copy_from_slice(&[0x01, 0x01]);
        response[2..4].copy_from_slice(&12_u16.to_be_bytes());
        response[4..8].copy_from_slice(&MAGIC_COOKIE);
        response[8..20].copy_from_slice(&request[8..20]);
        response[20..22].copy_from_slice(&[0x00, 0x20]);
        response[22..24].copy_from_slice(&8_u16.to_be_bytes());
        response[25] = 0x01;
        response[26..28].copy_from_slice(&(port ^ 0x2112).to_be_bytes());
        for (index, octet) in external.iter().enumerate() {
            response[28 + index] = octet ^ MAGIC_COOKIE[index];
        }
        response
    }
}
