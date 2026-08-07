use std::sync::Arc;

use quinn::rustls::{
    self,
    client::danger::{HandshakeSignatureValid, ServerCertVerified, ServerCertVerifier},
    crypto::CryptoProvider,
    pki_types::{CertificateDer, ServerName, UnixTime},
};
use sha2::{Digest, Sha256};
use tonic::transport::{ClientTlsConfig, Endpoint};

pub const RELAY_TLS_PIN_BYTES: usize = 32;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct RelayTlsPin([u8; RELAY_TLS_PIN_BYTES]);

impl RelayTlsPin {
    #[must_use]
    pub const fn from_bytes(bytes: [u8; RELAY_TLS_PIN_BYTES]) -> Self {
        Self(bytes)
    }

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

    pub fn tonic_endpoint(&self) -> Result<Endpoint, RelayTlsEndpointError> {
        Endpoint::from_shared(format!("https://{}:{}", self.server_name, self.port))
            .map_err(|_| RelayTlsEndpointError::EndpointConfiguration)?
            .tls_config_with_verifier(
                ClientTlsConfig::new().domain_name(self.server_name.clone()),
                Arc::new(PinnedRelayCertificateVerifier::new(self.pin)),
            )
            .map_err(|_| RelayTlsEndpointError::TlsConfiguration)
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
    #[error("relay TLS endpoint could not be constructed")]
    EndpointConfiguration,
    #[error("relay TLS configuration could not be constructed")]
    TlsConfiguration,
}

fn is_valid_server_name(server_name: &str) -> bool {
    !server_name.is_empty()
        && server_name.len() <= 253
        && !server_name.starts_with('.')
        && !server_name.ends_with('.')
        && server_name.split('.').all(|label| {
            !label.is_empty()
                && label.len() <= 63
                && !label.starts_with('-')
                && !label.ends_with('-')
                && label
                    .bytes()
                    .all(|byte| byte.is_ascii_lowercase() || byte.is_ascii_digit() || byte == b'-')
        })
}

#[derive(Debug)]
struct PinnedRelayCertificateVerifier {
    pin: RelayTlsPin,
    provider: Arc<CryptoProvider>,
}

impl PinnedRelayCertificateVerifier {
    fn new(pin: RelayTlsPin) -> Self {
        Self {
            pin,
            provider: Arc::new(rustls::crypto::ring::default_provider()),
        }
    }
}

impl ServerCertVerifier for PinnedRelayCertificateVerifier {
    fn verify_server_cert(
        &self,
        end_entity: &CertificateDer<'_>,
        _intermediates: &[CertificateDer<'_>],
        _server_name: &ServerName<'_>,
        _ocsp_response: &[u8],
        _now: UnixTime,
    ) -> Result<ServerCertVerified, rustls::Error> {
        if RelayTlsPin::from_certificate_der(end_entity.as_ref()) != self.pin {
            return Err(rustls::CertificateError::UnknownIssuer.into());
        }
        Ok(ServerCertVerified::assertion())
    }

    fn verify_tls12_signature(
        &self,
        message: &[u8],
        cert: &CertificateDer<'_>,
        dss: &rustls::DigitallySignedStruct,
    ) -> Result<HandshakeSignatureValid, rustls::Error> {
        rustls::crypto::verify_tls12_signature(
            message,
            cert,
            dss,
            &self.provider.signature_verification_algorithms,
        )
    }

    fn verify_tls13_signature(
        &self,
        message: &[u8],
        cert: &CertificateDer<'_>,
        dss: &rustls::DigitallySignedStruct,
    ) -> Result<HandshakeSignatureValid, rustls::Error> {
        rustls::crypto::verify_tls13_signature(
            message,
            cert,
            dss,
            &self.provider.signature_verification_algorithms,
        )
    }

    fn supported_verify_schemes(&self) -> Vec<rustls::SignatureScheme> {
        self.provider
            .signature_verification_algorithms
            .supported_schemes()
    }
}

#[cfg(test)]
mod tests {
    use arachne_daemon_api::v1::{
        StartClientRequest, daemon_service_client::DaemonServiceClient,
        daemon_service_server::DaemonServiceServer,
    };
    use arachne_protocol::ProtocolVersion;
    use hyper_util::rt::TokioIo;
    use tokio::{
        net::{TcpListener, TcpStream},
        sync::oneshot,
    };
    use tokio_stream::wrappers::TcpListenerStream;
    use tonic::{
        Request,
        transport::{Identity, Server, ServerTlsConfig},
    };
    use tower::service_fn;

    use super::{RelayTlsEndpoint, RelayTlsEndpointError, RelayTlsPin};
    use crate::DaemonGrpcService;

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
        assert_eq!(
            RelayTlsEndpoint::new("relay..example".to_owned(), 443, pin),
            Err(RelayTlsEndpointError::InvalidServerName)
        );
        assert_eq!(
            RelayTlsEndpoint::new("-relay.example".to_owned(), 443, pin),
            Err(RelayTlsEndpointError::InvalidServerName)
        );
    }

    #[tokio::test]
    async fn configures_tonic_to_accept_only_the_pinned_tls_certificate() {
        let certificate =
            rcgen::generate_simple_self_signed(vec!["relay.example".to_owned()]).unwrap();
        let endpoint = RelayTlsEndpoint::new(
            "relay.example".to_owned(),
            443,
            RelayTlsPin::from_certificate_der(certificate.cert.der().as_ref()),
        )
        .unwrap();
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let address = listener.local_addr().unwrap();
        let identity = Identity::from_pem(
            certificate.cert.pem(),
            certificate.signing_key.serialize_pem(),
        );
        let (shutdown_sender, shutdown_receiver) = oneshot::channel();
        let server = Server::builder()
            .tls_config(ServerTlsConfig::new().identity(identity))
            .unwrap()
            .add_service(DaemonServiceServer::new(DaemonGrpcService::new(
                ProtocolVersion::INITIAL,
            )))
            .serve_with_incoming_shutdown(TcpListenerStream::new(listener), async move {
                let _ = shutdown_receiver.await;
            });
        let client = async {
            let channel = endpoint
                .tonic_endpoint()
                .unwrap()
                .connect_with_connector(service_fn(move |_| async move {
                    TcpStream::connect(address).await.map(TokioIo::new)
                }))
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
        let (server, response) = tokio::join!(server, client);
        assert!(server.is_ok());
        assert!(response.running);

        let substitute =
            rcgen::generate_simple_self_signed(vec!["relay.example".to_owned()]).unwrap();
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let address = listener.local_addr().unwrap();
        let identity = Identity::from_pem(
            substitute.cert.pem(),
            substitute.signing_key.serialize_pem(),
        );
        let (shutdown_sender, shutdown_receiver) = oneshot::channel();
        let server = Server::builder()
            .tls_config(ServerTlsConfig::new().identity(identity))
            .unwrap()
            .add_service(DaemonServiceServer::new(DaemonGrpcService::new(
                ProtocolVersion::INITIAL,
            )))
            .serve_with_incoming_shutdown(TcpListenerStream::new(listener), async move {
                let _ = shutdown_receiver.await;
            });

        let client = async {
            let result = endpoint
                .tonic_endpoint()
                .unwrap()
                .connect_with_connector(service_fn(move |_| async move {
                    TcpStream::connect(address).await.map(TokioIo::new)
                }))
                .await;
            shutdown_sender.send(()).unwrap();
            result
        };
        let (server, client) = tokio::join!(server, client);
        assert!(server.is_ok());
        assert!(client.is_err());
    }
}
