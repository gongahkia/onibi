use std::net::Ipv4Addr;

use natpmp::{NatpmpAsync, Protocol, Response, new_tokio_natpmp, new_tokio_natpmp_with};
use tokio::net::UdpSocket;

pub const MAX_NAT_PMP_LEASE_SECONDS: u32 = 3_600;

pub struct NatPmpMappingRequest {
    internal_port: u16,
    external_port: u16,
    lease_seconds: u32,
}

pub struct NatPmpMapping {
    gateway: Option<Ipv4Addr>,
    internal_port: u16,
    external_port: u16,
}

impl NatPmpMappingRequest {
    pub fn new(
        internal_port: u16,
        external_port: u16,
        lease_seconds: u32,
    ) -> Result<Self, NatPmpMappingError> {
        if internal_port == 0 {
            return Err(NatPmpMappingError::ZeroInternalPort);
        }
        if external_port == 0 {
            return Err(NatPmpMappingError::ZeroExternalPort);
        }
        if !(1..=MAX_NAT_PMP_LEASE_SECONDS).contains(&lease_seconds) {
            return Err(NatPmpMappingError::InvalidLease);
        }
        Ok(Self {
            internal_port,
            external_port,
            lease_seconds,
        })
    }

    pub async fn create(self) -> Result<NatPmpMapping, NatPmpMappingError> {
        self.create_with_gateway_option(None).await
    }

    pub async fn create_with_gateway(
        self,
        gateway: Ipv4Addr,
    ) -> Result<NatPmpMapping, NatPmpMappingError> {
        self.create_with_gateway_option(Some(gateway)).await
    }

    async fn create_with_gateway_option(
        self,
        gateway: Option<Ipv4Addr>,
    ) -> Result<NatPmpMapping, NatPmpMappingError> {
        let client = nat_pmp_client(gateway).await?;
        client
            .send_port_mapping_request(
                Protocol::UDP,
                self.internal_port,
                self.external_port,
                self.lease_seconds,
            )
            .await
            .map_err(NatPmpMappingError::Client)?;
        verify_response(
            client
                .read_response_or_retry()
                .await
                .map_err(NatPmpMappingError::Client)?,
            self.internal_port,
            self.external_port,
            true,
        )?;
        Ok(NatPmpMapping {
            gateway,
            internal_port: self.internal_port,
            external_port: self.external_port,
        })
    }
}

impl NatPmpMapping {
    #[must_use]
    pub const fn external_port(&self) -> u16 {
        self.external_port
    }

    pub async fn remove(&self) -> Result<(), NatPmpMappingError> {
        let client = nat_pmp_client(self.gateway).await?;
        client
            .send_port_mapping_request(Protocol::UDP, self.internal_port, self.external_port, 0)
            .await
            .map_err(NatPmpMappingError::Client)?;
        verify_response(
            client
                .read_response_or_retry()
                .await
                .map_err(NatPmpMappingError::Client)?,
            self.internal_port,
            self.external_port,
            false,
        )
    }
}

async fn nat_pmp_client(
    gateway: Option<Ipv4Addr>,
) -> Result<NatpmpAsync<UdpSocket>, NatPmpMappingError> {
    let client = match gateway {
        Some(gateway) => new_tokio_natpmp_with(gateway).await,
        None => new_tokio_natpmp().await,
    };
    client.map_err(NatPmpMappingError::Client)
}

#[derive(Debug, thiserror::Error)]
pub enum NatPmpMappingError {
    #[error("NAT-PMP internal port must be nonzero")]
    ZeroInternalPort,
    #[error("NAT-PMP external port must be nonzero")]
    ZeroExternalPort,
    #[error("NAT-PMP lease must be between one second and {MAX_NAT_PMP_LEASE_SECONDS} seconds")]
    InvalidLease,
    #[error("NAT-PMP client operation failed: {0}")]
    Client(#[source] natpmp::Error),
    #[error("NAT-PMP response is not a UDP mapping response")]
    UnexpectedResponse,
    #[error("NAT-PMP response ports do not match the requested mapping")]
    MismatchedResponse,
    #[error("NAT-PMP response has an invalid mapping lease")]
    InvalidResponseLease,
}

fn verify_response(
    response: Response,
    internal_port: u16,
    external_port: u16,
    mapping: bool,
) -> Result<(), NatPmpMappingError> {
    let Response::UDP(response) = response else {
        return Err(NatPmpMappingError::UnexpectedResponse);
    };
    if response.private_port() != internal_port || response.public_port() != external_port {
        return Err(NatPmpMappingError::MismatchedResponse);
    }
    let lease_seconds = response.lifetime().as_secs();
    if (mapping && !(1..=u64::from(MAX_NAT_PMP_LEASE_SECONDS)).contains(&lease_seconds))
        || (!mapping && lease_seconds != 0)
    {
        return Err(NatPmpMappingError::InvalidResponseLease);
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use std::{net::Ipv4Addr, sync::OnceLock};

    use super::{MAX_NAT_PMP_LEASE_SECONDS, NatPmpMappingError, NatPmpMappingRequest};
    use tokio::net::UdpSocket;

    const TEST_GATEWAY: Ipv4Addr = Ipv4Addr::LOCALHOST;
    static NAT_PMP_TEST_LOCK: OnceLock<tokio::sync::Mutex<()>> = OnceLock::new();

    #[test]
    fn validates_explicit_bounded_udp_mapping_requests() {
        assert!(NatPmpMappingRequest::new(4242, 4242, 600).is_ok());
        assert!(matches!(
            NatPmpMappingRequest::new(0, 4242, 600),
            Err(NatPmpMappingError::ZeroInternalPort)
        ));
        assert!(matches!(
            NatPmpMappingRequest::new(4242, 0, 600),
            Err(NatPmpMappingError::ZeroExternalPort)
        ));
        assert!(matches!(
            NatPmpMappingRequest::new(4242, 4242, 0),
            Err(NatPmpMappingError::InvalidLease)
        ));
        assert!(matches!(
            NatPmpMappingRequest::new(4242, 4242, MAX_NAT_PMP_LEASE_SECONDS + 1),
            Err(NatPmpMappingError::InvalidLease)
        ));
    }

    #[tokio::test]
    async fn creates_and_removes_a_mapping_through_a_nat_pmp_gateway() {
        let _guard = test_lock().lock().await;
        let gateway = start_nat_pmp_gateway(TEST_GATEWAY).await;
        let mapping = NatPmpMappingRequest::new(4242, 4242, 600)
            .unwrap()
            .create_with_gateway(TEST_GATEWAY)
            .await
            .unwrap();

        assert_eq!(mapping.external_port(), 4242);
        mapping.remove().await.unwrap();
        gateway.await.unwrap();
    }

    #[tokio::test]
    async fn rejects_non_udp_mapping_responses() {
        let _guard = test_lock().lock().await;
        let gateway = start_non_udp_response_gateway(TEST_GATEWAY).await;
        let result = NatPmpMappingRequest::new(4242, 4242, 600)
            .unwrap()
            .create_with_gateway(TEST_GATEWAY)
            .await;

        assert!(matches!(
            result,
            Err(NatPmpMappingError::UnexpectedResponse)
        ));
        gateway.await.unwrap();
    }

    fn test_lock() -> &'static tokio::sync::Mutex<()> {
        NAT_PMP_TEST_LOCK.get_or_init(tokio::sync::Mutex::default)
    }

    async fn start_nat_pmp_gateway(gateway: Ipv4Addr) -> tokio::task::JoinHandle<()> {
        let socket = UdpSocket::bind((gateway, natpmp::NATPMP_PORT))
            .await
            .unwrap();
        tokio::spawn(async move {
            for lifetime in [600, 0] {
                let (request, peer) = read_mapping_request(&socket).await;
                assert_eq!(mapping_lifetime(&request), lifetime);
                socket
                    .send_to(&mapping_response(&request, 0x81, lifetime), peer)
                    .await
                    .unwrap();
            }
        })
    }

    async fn start_non_udp_response_gateway(gateway: Ipv4Addr) -> tokio::task::JoinHandle<()> {
        let socket = UdpSocket::bind((gateway, natpmp::NATPMP_PORT))
            .await
            .unwrap();
        tokio::spawn(async move {
            let (request, peer) = read_mapping_request(&socket).await;
            socket
                .send_to(&mapping_response(&request, 0x82, 600), peer)
                .await
                .unwrap();
        })
    }

    async fn read_mapping_request(socket: &UdpSocket) -> ([u8; 12], std::net::SocketAddr) {
        let mut request = [0_u8; 12];
        let (length, peer) = socket.recv_from(&mut request).await.unwrap();
        assert_eq!(length, request.len());
        assert_eq!(request[0], 0);
        assert_eq!(request[1], 1);
        assert_eq!(request[2..4], [0, 0]);
        assert_eq!(u16::from_be_bytes([request[4], request[5]]), 4242);
        assert_eq!(u16::from_be_bytes([request[6], request[7]]), 4242);
        (request, peer)
    }

    fn mapping_lifetime(request: &[u8; 12]) -> u32 {
        u32::from_be_bytes([request[8], request[9], request[10], request[11]])
    }

    fn mapping_response(request: &[u8; 12], opcode: u8, lifetime: u32) -> [u8; 16] {
        let mut response = [0_u8; 16];
        response[1] = opcode;
        response[4..8].copy_from_slice(&1_u32.to_be_bytes());
        response[8..12].copy_from_slice(&request[4..8]);
        response[12..16].copy_from_slice(&lifetime.to_be_bytes());
        response
    }
}
