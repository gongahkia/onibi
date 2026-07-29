use std::task::{Context, Poll};

use hyper_util::rt::TokioIo;
use tokio::net::TcpStream;
use tokio_socks::tcp::Socks5Stream;
use tonic::codegen::{BoxFuture, Service, http::Uri};

use crate::{ExternalTorRuntime, ExternalTorRuntimeError, TorSocksTarget};

#[derive(Clone, Debug)]
pub struct TorSocksTonicConnector {
    runtime: ExternalTorRuntime,
    target: TorSocksTarget,
}

impl TorSocksTonicConnector {
    #[must_use]
    pub fn new(runtime: ExternalTorRuntime, target: TorSocksTarget) -> Self {
        Self { runtime, target }
    }

    #[must_use]
    pub const fn runtime(&self) -> ExternalTorRuntime {
        self.runtime
    }

    #[must_use]
    pub fn target(&self) -> &TorSocksTarget {
        &self.target
    }

    pub async fn connect(
        &self,
        uri: &Uri,
    ) -> Result<TokioIo<Socks5Stream<TcpStream>>, TorSocksTonicConnectorError> {
        if uri.scheme_str() != Some("https") {
            return Err(TorSocksTonicConnectorError::InvalidScheme);
        }
        if uri.host() != Some(self.target.hostname()) || uri.port_u16() != Some(self.target.port())
        {
            return Err(TorSocksTonicConnectorError::TargetMismatch);
        }
        self.runtime
            .connect(&self.target)
            .await
            .map(TokioIo::new)
            .map_err(TorSocksTonicConnectorError::Runtime)
    }
}

impl Service<Uri> for TorSocksTonicConnector {
    type Response = TokioIo<Socks5Stream<TcpStream>>;
    type Error = TorSocksTonicConnectorError;
    type Future = BoxFuture<Self::Response, Self::Error>;

    fn poll_ready(&mut self, _context: &mut Context<'_>) -> Poll<Result<(), Self::Error>> {
        Poll::Ready(Ok(()))
    }

    fn call(&mut self, uri: Uri) -> Self::Future {
        let connector = self.clone();
        Box::pin(async move { connector.connect(&uri).await })
    }
}

#[derive(Debug, thiserror::Error)]
pub enum TorSocksTonicConnectorError {
    #[error("Tor Tonic endpoint must use HTTPS")]
    InvalidScheme,
    #[error("Tor Tonic endpoint does not match the configured onion target")]
    TargetMismatch,
    #[error("Tor Tonic SOCKS connection failed")]
    Runtime(#[source] ExternalTorRuntimeError),
}

#[cfg(test)]
mod tests {
    use std::{io::ErrorKind, time::Duration};

    use arachne_daemon_api::v1::{
        StartClientRequest, daemon_service_client::DaemonServiceClient,
        daemon_service_server::DaemonServiceServer,
    };
    use arachne_protocol::ProtocolVersion;
    use tokio::{
        io::{AsyncReadExt, AsyncWriteExt},
        net::{TcpListener, TcpStream},
        sync::oneshot,
    };
    use tokio_stream::wrappers::TcpListenerStream;
    use tonic::{
        Request,
        transport::{Identity, Server, ServerTlsConfig},
    };

    use super::{TorSocksTonicConnector, TorSocksTonicConnectorError};
    use crate::{
        DaemonGrpcService, ExternalTorRuntimeConfig, RelayTlsEndpoint, RelayTlsPin, TorSocksTarget,
    };

    const ONION: &str = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.onion";
    const OTHER_ONION: &str = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.onion";

    #[tokio::test]
    async fn rejects_nonmatching_tonic_endpoint_before_connecting() {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let runtime =
            ExternalTorRuntimeConfig::new(listener.local_addr().unwrap(), Duration::from_secs(1))
                .unwrap()
                .runtime();
        let connector = TorSocksTonicConnector::new(
            runtime,
            TorSocksTarget::new(ONION.to_owned(), 443).unwrap(),
        );
        assert!(matches!(
            connector
                .connect(
                    &"http://aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.onion:443"
                        .parse()
                        .unwrap()
                )
                .await,
            Err(TorSocksTonicConnectorError::InvalidScheme)
        ));
        assert!(matches!(
            connector
                .connect(&format!("https://{OTHER_ONION}:443").parse().unwrap())
                .await,
            Err(TorSocksTonicConnectorError::TargetMismatch)
        ));
        assert_eq!(connector.target().hostname(), ONION);
    }

    #[tokio::test]
    async fn carries_pinned_tls_tonic_grpc_through_the_onion_socks_connector() {
        let certificate =
            rcgen::generate_simple_self_signed(vec!["relay.example".to_owned()]).unwrap();
        let relay_endpoint = RelayTlsEndpoint::new(
            ONION.to_owned(),
            443,
            RelayTlsPin::from_certificate_der(certificate.cert.der().as_ref()),
        )
        .unwrap();
        let relay_listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let relay_address = relay_listener.local_addr().unwrap();
        let identity = Identity::from_pem(
            certificate.cert.pem(),
            certificate.signing_key.serialize_pem(),
        );
        let (shutdown_sender, shutdown_receiver) = oneshot::channel();
        let relay = Server::builder()
            .tls_config(ServerTlsConfig::new().identity(identity))
            .unwrap()
            .add_service(DaemonServiceServer::new(DaemonGrpcService::new(
                ProtocolVersion::INITIAL,
            )))
            .serve_with_incoming_shutdown(TcpListenerStream::new(relay_listener), async move {
                let _ = shutdown_receiver.await;
            });
        let socks_listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let runtime = ExternalTorRuntimeConfig::new(
            socks_listener.local_addr().unwrap(),
            Duration::from_secs(1),
        )
        .unwrap()
        .runtime();
        let connector = TorSocksTonicConnector::new(
            runtime,
            TorSocksTarget::new(ONION.to_owned(), relay_endpoint.port()).unwrap(),
        );
        let socks = tokio::spawn(forward_socks_connection(socks_listener, relay_address));
        let client = async {
            let channel = relay_endpoint
                .tonic_endpoint()
                .unwrap()
                .connect_with_connector(connector)
                .await
                .unwrap();
            let mut client = DaemonServiceClient::new(channel);
            let response = client
                .start_client(Request::new(StartClientRequest {}))
                .await
                .unwrap()
                .into_inner();
            shutdown_sender.send(()).unwrap();
            response
        };
        let (relay, response) = tokio::join!(relay, client);
        assert!(relay.is_ok());
        assert!(response.running);
        socks.await.unwrap();
    }

    async fn forward_socks_connection(listener: TcpListener, relay_address: std::net::SocketAddr) {
        let (mut client, _) = listener.accept().await.unwrap();
        let mut greeting = [0; 3];
        client.read_exact(&mut greeting).await.unwrap();
        assert_eq!(greeting, [5, 1, 0]);
        client.write_all(&[5, 0]).await.unwrap();
        let mut header = [0; 5];
        client.read_exact(&mut header).await.unwrap();
        assert_eq!(&header[..4], &[5, 1, 0, 3]);
        let mut hostname = vec![0; usize::from(header[4])];
        client.read_exact(&mut hostname).await.unwrap();
        let mut port = [0; 2];
        client.read_exact(&mut port).await.unwrap();
        assert_eq!(hostname, ONION.as_bytes());
        assert_eq!(u16::from_be_bytes(port), 443);
        let mut relay = TcpStream::connect(relay_address).await.unwrap();
        client
            .write_all(&[5, 0, 0, 1, 0, 0, 0, 0, 0, 0])
            .await
            .unwrap();
        if let Err(error) = tokio::io::copy_bidirectional(&mut client, &mut relay).await {
            assert_eq!(error.kind(), ErrorKind::BrokenPipe);
        }
    }
}
