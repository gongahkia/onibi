use std::net::{IpAddr, SocketAddr};

use igd_next::{
    PortMappingProtocol, SearchOptions,
    aio::{
        Gateway,
        tokio::{Tokio, search_gateway},
    },
};

pub const MAX_UPNP_LEASE_SECONDS: u32 = 3_600;
const MAPPING_DESCRIPTION: &str = "arachne direct QUIC";

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
        self.create_with_search_options(SearchOptions::default())
            .await
    }

    pub async fn create_with_search_options(
        self,
        search_options: SearchOptions,
    ) -> Result<UpnpMapping, UpnpMappingError> {
        let gateway = search_gateway(search_options)
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
    use std::time::Duration;

    use super::{MAX_UPNP_LEASE_SECONDS, UpnpMappingError, UpnpMappingRequest};
    use igd_next::SearchOptions;
    use tokio::{
        io::{AsyncReadExt, AsyncWriteExt},
        net::{TcpListener, TcpStream, UdpSocket},
    };

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

    #[tokio::test]
    async fn creates_and_removes_a_mapping_through_a_upnp_gateway() {
        let (search_options, ssdp_responder, http_server) = start_upnp_gateway().await;
        let mapping = UpnpMappingRequest::new("127.0.0.1:4242".parse().unwrap(), 4242, 600)
            .unwrap()
            .create_with_search_options(search_options)
            .await
            .unwrap();

        assert_eq!(mapping.external_port(), 4242);
        mapping.remove().await.unwrap();
        ssdp_responder.await.unwrap();
        http_server.await.unwrap();
    }

    #[tokio::test]
    async fn reports_gateway_discovery_failure() {
        let unavailable_gateway = UdpSocket::bind("127.0.0.1:0").await.unwrap();
        let unavailable_gateway = unavailable_gateway.local_addr().unwrap();
        let result = UpnpMappingRequest::new("127.0.0.1:4242".parse().unwrap(), 4242, 600)
            .unwrap()
            .create_with_search_options(search_options(unavailable_gateway))
            .await;

        assert!(matches!(result, Err(UpnpMappingError::Discovery(_))));
    }

    async fn start_upnp_gateway() -> (
        SearchOptions,
        tokio::task::JoinHandle<()>,
        tokio::task::JoinHandle<()>,
    ) {
        let http_listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let http_endpoint = http_listener.local_addr().unwrap();
        let ssdp_socket = UdpSocket::bind("127.0.0.1:0").await.unwrap();
        let ssdp_endpoint = ssdp_socket.local_addr().unwrap();
        let ssdp_responder = tokio::spawn(async move {
            let mut request = [0_u8; 1_500];
            let (length, peer) = ssdp_socket.recv_from(&mut request).await.unwrap();
            let request = std::str::from_utf8(&request[..length]).unwrap();
            assert!(request.starts_with("M-SEARCH * HTTP/1.1"));
            let response =
                format!("HTTP/1.1 200 OK\r\nLOCATION: http://{http_endpoint}/root.xml\r\n\r\n");
            ssdp_socket
                .send_to(response.as_bytes(), peer)
                .await
                .unwrap();
        });
        let http_server = tokio::spawn(async move {
            let (root, root_stream) = read_http_request(&http_listener).await;
            assert!(root.starts_with("GET /root.xml HTTP/1.1"));
            write_http_response(root_stream, device_description()).await;

            let (schema, schema_stream) = read_http_request(&http_listener).await;
            assert!(schema.starts_with("GET /schema.xml HTTP/1.1"));
            write_http_response(schema_stream, service_schema()).await;

            let (add, add_stream) = read_http_request(&http_listener).await;
            assert!(add.starts_with("POST /control HTTP/1.1"));
            assert!(add.contains("AddPortMapping"));
            assert!(add.contains("<NewExternalPort>4242</NewExternalPort>"));
            assert!(add.contains("<NewInternalClient>127.0.0.1</NewInternalClient>"));
            assert!(add.contains("<NewInternalPort>4242</NewInternalPort>"));
            assert!(add.contains("<NewProtocol>UDP</NewProtocol>"));
            write_http_response(add_stream, &soap_response("AddPortMappingResponse")).await;

            let (remove, remove_stream) = read_http_request(&http_listener).await;
            assert!(remove.starts_with("POST /control HTTP/1.1"));
            assert!(remove.contains("DeletePortMapping"));
            assert!(remove.contains("<NewExternalPort>4242</NewExternalPort>"));
            assert!(remove.contains("<NewProtocol>UDP</NewProtocol>"));
            write_http_response(remove_stream, &soap_response("DeletePortMappingResponse")).await;
        });
        (search_options(ssdp_endpoint), ssdp_responder, http_server)
    }

    fn search_options(broadcast_address: std::net::SocketAddr) -> SearchOptions {
        SearchOptions {
            bind_addr: "127.0.0.1:0".parse().unwrap(),
            broadcast_address,
            timeout: Some(Duration::from_millis(250)),
            single_search_timeout: Some(Duration::from_millis(25)),
        }
    }

    async fn read_http_request(listener: &TcpListener) -> (String, TcpStream) {
        let (mut stream, _) = listener.accept().await.unwrap();
        let mut request = Vec::new();
        let mut buffer = [0_u8; 1_024];
        loop {
            let length = stream.read(&mut buffer).await.unwrap();
            assert_ne!(length, 0);
            request.extend_from_slice(&buffer[..length]);
            if let Some(header_end) = header_end(&request) {
                let headers = std::str::from_utf8(&request[..header_end]).unwrap();
                let content_length = headers
                    .lines()
                    .find_map(|line| {
                        let (name, value) = line.split_once(':')?;
                        name.eq_ignore_ascii_case("content-length")
                            .then(|| value.trim().parse::<usize>().unwrap())
                    })
                    .unwrap_or(0);
                if request.len() >= header_end + content_length {
                    return (String::from_utf8(request).unwrap(), stream);
                }
            }
        }
    }

    async fn write_http_response(mut stream: TcpStream, body: &str) {
        let response = format!(
            "HTTP/1.1 200 OK\r\ncontent-length: {}\r\nconnection: close\r\n\r\n{body}",
            body.len()
        );
        stream.write_all(response.as_bytes()).await.unwrap();
    }

    fn header_end(request: &[u8]) -> Option<usize> {
        request
            .windows(4)
            .position(|window| window == b"\r\n\r\n")
            .map(|index| index + 4)
    }

    fn device_description() -> &'static str {
        "<?xml version=\"1.0\"?><root><device><serviceList><service><serviceType>urn:schemas-upnp-org:service:WANIPConnection:1</serviceType><SCPDURL>/schema.xml</SCPDURL><controlURL>/control</controlURL></service></serviceList></device></root>"
    }

    fn service_schema() -> &'static str {
        "<?xml version=\"1.0\"?><scpd><actionList><action><name>AddPortMapping</name><argumentList><argument><name>NewRemoteHost</name><direction>in</direction></argument><argument><name>NewExternalPort</name><direction>in</direction></argument><argument><name>NewProtocol</name><direction>in</direction></argument><argument><name>NewInternalPort</name><direction>in</direction></argument><argument><name>NewInternalClient</name><direction>in</direction></argument><argument><name>NewEnabled</name><direction>in</direction></argument><argument><name>NewPortMappingDescription</name><direction>in</direction></argument><argument><name>NewLeaseDuration</name><direction>in</direction></argument></argumentList></action><action><name>DeletePortMapping</name><argumentList><argument><name>NewRemoteHost</name><direction>in</direction></argument><argument><name>NewExternalPort</name><direction>in</direction></argument><argument><name>NewProtocol</name><direction>in</direction></argument></argumentList></action></actionList></scpd>"
    }

    fn soap_response(action: &str) -> String {
        format!(
            "<?xml version=\"1.0\"?><s:Envelope xmlns:s=\"http://schemas.xmlsoap.org/soap/envelope/\"><s:Body><u:{action} xmlns:u=\"urn:schemas-upnp-org:service:WANIPConnection:1\"/></s:Body></s:Envelope>"
        )
    }
}
