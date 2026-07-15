use std::net::SocketAddr;

use quinn::{ClientConfig, Connection, Endpoint, ServerConfig, VarInt};
use yeokcham_protocol::DirectProfileConfig;

pub struct DirectTransport {
    endpoint: Endpoint,
}

pub struct DirectConnection {
    connection: Connection,
}

impl DirectTransport {
    pub fn bind(
        bind_address: SocketAddr,
        server_config: Option<ServerConfig>,
    ) -> Result<Self, DirectTransportError> {
        let endpoint = match server_config {
            Some(server_config) => Endpoint::server(server_config, bind_address),
            None => Endpoint::client(bind_address),
        }
        .map_err(DirectTransportError::Bind)?;
        Ok(Self { endpoint })
    }

    pub fn local_address(&self) -> Result<SocketAddr, DirectTransportError> {
        self.endpoint
            .local_addr()
            .map_err(DirectTransportError::LocalAddress)
    }

    pub async fn connect(
        &self,
        profile: DirectProfileConfig,
        client_config: ClientConfig,
        server_name: &str,
    ) -> Result<DirectConnection, DirectTransportError> {
        if server_name.is_empty() {
            return Err(DirectTransportError::EmptyServerName);
        }
        let connecting = self
            .endpoint
            .connect_with(client_config, profile.endpoint(), server_name)
            .map_err(DirectTransportError::Connect)?;
        let connection = connecting.await.map_err(DirectTransportError::Handshake)?;
        Ok(DirectConnection { connection })
    }

    pub async fn accept(&self) -> Result<DirectConnection, DirectTransportError> {
        let incoming = self
            .endpoint
            .accept()
            .await
            .ok_or(DirectTransportError::Shutdown)?;
        let connection = incoming.await.map_err(DirectTransportError::Handshake)?;
        Ok(DirectConnection { connection })
    }

    pub fn shutdown(&self) {
        self.endpoint.close(VarInt::from_u32(0), b"shutdown");
    }

    pub async fn wait_idle(&self) {
        self.endpoint.wait_idle().await;
    }
}

impl DirectConnection {
    #[must_use]
    pub const fn connection(&self) -> &Connection {
        &self.connection
    }

    #[must_use]
    pub fn remote_address(&self) -> SocketAddr {
        self.connection.remote_address()
    }

    pub fn close(&self) {
        self.connection.close(VarInt::from_u32(0), b"shutdown");
    }
}

#[derive(Debug, thiserror::Error)]
pub enum DirectTransportError {
    #[error("failed to bind direct QUIC endpoint: {0}")]
    Bind(#[source] std::io::Error),
    #[error("failed to read direct QUIC endpoint address: {0}")]
    LocalAddress(#[source] std::io::Error),
    #[error("direct QUIC server name is empty")]
    EmptyServerName,
    #[error("failed to start direct QUIC connection: {0}")]
    Connect(#[source] quinn::ConnectError),
    #[error("direct QUIC TLS handshake failed: {0}")]
    Handshake(#[source] quinn::ConnectionError),
    #[error("direct QUIC endpoint is shut down")]
    Shutdown,
}

#[cfg(test)]
mod tests {
    use std::sync::Arc;

    use quinn::{
        ClientConfig, ServerConfig, rustls::RootCertStore, rustls::pki_types::PrivatePkcs8KeyDer,
    };
    use rcgen::generate_simple_self_signed;
    use yeokcham_protocol::DirectProfileConfig;

    use super::{DirectTransport, DirectTransportError};

    fn server_config() -> (
        ServerConfig,
        quinn::rustls::pki_types::CertificateDer<'static>,
    ) {
        let certificate = generate_simple_self_signed(vec!["localhost".to_owned()]).unwrap();
        let certificate_der = certificate.cert.der().clone();
        let private_key = PrivatePkcs8KeyDer::from(certificate.signing_key.serialize_der());
        let server_config =
            ServerConfig::with_single_cert(vec![certificate_der.clone()], private_key.into())
                .unwrap();
        (server_config, certificate_der)
    }

    fn client_config(
        certificate: quinn::rustls::pki_types::CertificateDer<'static>,
    ) -> ClientConfig {
        let mut roots = RootCertStore::empty();
        roots.add(certificate).unwrap();
        ClientConfig::with_root_certificates(Arc::new(roots)).unwrap()
    }

    #[tokio::test]
    async fn connects_only_with_an_explicitly_trusted_server_certificate() {
        let (server_tls_config, certificate) = server_config();
        let server =
            DirectTransport::bind("127.0.0.1:0".parse().unwrap(), Some(server_tls_config)).unwrap();
        let client = DirectTransport::bind("127.0.0.1:0".parse().unwrap(), None).unwrap();
        let profile = DirectProfileConfig::new(server.local_address().unwrap()).unwrap();

        let (accepted, connected) = tokio::join!(
            server.accept(),
            client.connect(profile, client_config(certificate), "localhost")
        );
        let accepted = accepted.unwrap();
        let connected = connected.unwrap();

        assert_eq!(accepted.remote_address(), client.local_address().unwrap());
        assert_eq!(connected.remote_address(), server.local_address().unwrap());
        assert!(matches!(
            client
                .connect(profile, client_config(server_config().1), "")
                .await,
            Err(DirectTransportError::EmptyServerName)
        ));
        connected.close();
        client.shutdown();
        server.shutdown();
        client.wait_idle().await;
        server.wait_idle().await;
    }
}
