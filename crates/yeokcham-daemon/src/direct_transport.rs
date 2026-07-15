use std::net::SocketAddr;

use quinn::{ClientConfig, Connection, Endpoint, ServerConfig, VarInt};
use yeokcham_core::{IdentityKeypair, IdentityPublicKey};
use yeokcham_protocol::{DIRECT_PEER_PROOF_BYTES, DirectPeerProof, DirectProfileConfig};

const DIRECT_PEER_AUTH_LABEL: &[u8] = b"yeokcham/v1/direct-peer-authentication";
const DIRECT_PEER_AUTH_BINDING_BYTES: usize = 32;

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

    pub async fn authenticate_initiator(
        &self,
        local_identity: &IdentityKeypair,
        expected_peer: &IdentityPublicKey,
    ) -> Result<(), DirectTransportError> {
        let binding = self.connection_binding()?;
        let proof = DirectPeerProof::create(local_identity, &binding)
            .map_err(DirectTransportError::InvalidPeerProof)?;
        let (mut send, mut receive) = self
            .connection
            .open_bi()
            .await
            .map_err(DirectTransportError::OpenAuthenticationStream)?;
        send.write_all(
            &proof
                .encode()
                .map_err(DirectTransportError::InvalidPeerProof)?,
        )
        .await
        .map_err(DirectTransportError::WriteAuthenticationStream)?;
        send.finish()
            .map_err(DirectTransportError::FinishAuthenticationStream)?;
        self.verify_peer_proof(
            &receive
                .read_to_end(DIRECT_PEER_PROOF_BYTES)
                .await
                .map_err(DirectTransportError::ReadAuthenticationStream)?,
            &binding,
            expected_peer,
        )
    }

    pub async fn authenticate_responder(
        &self,
        local_identity: &IdentityKeypair,
        expected_peer: &IdentityPublicKey,
    ) -> Result<(), DirectTransportError> {
        let binding = self.connection_binding()?;
        let (mut send, mut receive) = self
            .connection
            .accept_bi()
            .await
            .map_err(DirectTransportError::OpenAuthenticationStream)?;
        let encoded = receive
            .read_to_end(DIRECT_PEER_PROOF_BYTES)
            .await
            .map_err(DirectTransportError::ReadAuthenticationStream)?;
        self.verify_peer_proof(&encoded, &binding, expected_peer)?;
        let proof = DirectPeerProof::create(local_identity, &binding)
            .map_err(DirectTransportError::InvalidPeerProof)?;
        send.write_all(
            &proof
                .encode()
                .map_err(DirectTransportError::InvalidPeerProof)?,
        )
        .await
        .map_err(DirectTransportError::WriteAuthenticationStream)?;
        send.finish()
            .map_err(DirectTransportError::FinishAuthenticationStream)?;
        Ok(())
    }

    fn connection_binding(
        &self,
    ) -> Result<[u8; DIRECT_PEER_AUTH_BINDING_BYTES], DirectTransportError> {
        let mut binding = [0; DIRECT_PEER_AUTH_BINDING_BYTES];
        self.connection
            .export_keying_material(&mut binding, DIRECT_PEER_AUTH_LABEL, b"")
            .map_err(|_| DirectTransportError::ConnectionBinding)?;
        Ok(binding)
    }

    fn verify_peer_proof(
        &self,
        encoded: &[u8],
        binding: &[u8; DIRECT_PEER_AUTH_BINDING_BYTES],
        expected_peer: &IdentityPublicKey,
    ) -> Result<(), DirectTransportError> {
        let proof =
            DirectPeerProof::decode(encoded).map_err(DirectTransportError::InvalidPeerProof)?;
        if proof.identity() != expected_peer {
            return Err(DirectTransportError::UnexpectedPeerIdentity);
        }
        proof
            .verify(binding)
            .map_err(DirectTransportError::InvalidPeerProof)
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
    #[error("failed to open direct peer-authentication stream: {0}")]
    OpenAuthenticationStream(#[source] quinn::ConnectionError),
    #[error("failed to write direct peer-authentication stream: {0}")]
    WriteAuthenticationStream(#[source] quinn::WriteError),
    #[error("failed to finish direct peer-authentication stream: {0}")]
    FinishAuthenticationStream(#[source] quinn::ClosedStream),
    #[error("failed to read direct peer-authentication stream: {0}")]
    ReadAuthenticationStream(#[source] quinn::ReadToEndError),
    #[error("failed to derive direct peer-authentication connection binding")]
    ConnectionBinding,
    #[error("direct peer proof is invalid: {0}")]
    InvalidPeerProof(#[source] yeokcham_protocol::DirectPeerProofError),
    #[error("direct peer proof does not match the expected identity")]
    UnexpectedPeerIdentity,
}

#[cfg(test)]
mod tests {
    use std::sync::Arc;

    use quinn::{
        ClientConfig, ServerConfig, rustls::RootCertStore, rustls::pki_types::PrivatePkcs8KeyDer,
    };
    use rcgen::generate_simple_self_signed;
    use yeokcham_core::IdentityKeypair;
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
        let server_identity = IdentityKeypair::generate().unwrap();
        let client_identity = IdentityKeypair::generate().unwrap();
        let server_public = server_identity.public_key();
        let client_public = client_identity.public_key();
        let (server_authentication, client_authentication) = tokio::join!(
            accepted.authenticate_responder(&server_identity, &client_public),
            connected.authenticate_initiator(&client_identity, &server_public)
        );

        assert_eq!(accepted.remote_address(), client.local_address().unwrap());
        assert_eq!(connected.remote_address(), server.local_address().unwrap());
        assert!(server_authentication.is_ok());
        assert!(client_authentication.is_ok());
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
