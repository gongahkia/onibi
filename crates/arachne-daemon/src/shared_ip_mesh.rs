use std::{
    fmt::Write as _,
    fs,
    io::Write as _,
    net::{IpAddr, SocketAddr},
    path::{Path, PathBuf},
    sync::Arc,
    time::{Duration, Instant},
};

use arachne_core::{
    ED25519_PUBLIC_KEY_BYTES, IdentityKeypair, IdentityPublicKey, KeystoreEntryName,
    KeystoreSecret, KeystoreSecretError, OsKeystore,
};
use arachne_protocol::{DirectProfileConfig, LocalMeshPeer, LocalMeshTransportKind};
use quinn::{
    ClientConfig, ServerConfig,
    crypto::rustls::QuicClientConfig,
    rustls::{
        self,
        client::danger::{HandshakeSignatureValid, ServerCertVerified, ServerCertVerifier},
        crypto::CryptoProvider,
        pki_types::{CertificateDer, PrivatePkcs8KeyDer, ServerName, UnixTime},
    },
};
use rcgen::generate_simple_self_signed;
use sha2::{Digest, Sha256};
use zeroize::Zeroizing;

use crate::{
    AuthenticatedLocalMeshSession, ClientStateDirectory, ClientStateDirectoryError,
    DirectConnection, DirectConnectionAttempts, DirectTransport, DirectTransportError, LanPeer,
    LanPeerDiscovery, LanPeerDiscoveryError, LocalMeshSessionError, LocalTransportAvailability,
};

pub const SHARED_IP_MESH_CONFIG_VERSION: u8 = 1;
pub const MAX_SHARED_IP_MESH_CONFIG_BYTES: usize = 16 * 1024;
pub const MAX_SHARED_IP_MESH_PEERS: usize = 64;
pub const SHARED_IP_MESH_TLS_KEY_ENTRY: &str = "arachne_shared_ip_mesh_tls_v1";
pub const SHARED_IP_MESH_TLS_PIN_BYTES: usize = 32;
pub const SHARED_IP_MESH_SERVER_NAME: &str = "arachne.local";

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct SharedIpMeshCertificatePin([u8; SHARED_IP_MESH_TLS_PIN_BYTES]);

impl SharedIpMeshCertificatePin {
    #[must_use]
    pub fn from_certificate_der(certificate_der: &[u8]) -> Self {
        let digest = Sha256::digest(certificate_der);
        let mut value = [0; SHARED_IP_MESH_TLS_PIN_BYTES];
        value.copy_from_slice(&digest);
        Self(value)
    }

    pub fn decode(encoded: &str) -> Result<Self, SharedIpMeshError> {
        if encoded.len() != SHARED_IP_MESH_TLS_PIN_BYTES * 2
            || !encoded
                .bytes()
                .all(|byte| byte.is_ascii_digit() || (byte.is_ascii_lowercase() && byte <= b'f'))
        {
            return Err(SharedIpMeshError::InvalidCertificatePin);
        }
        let mut output = [0; SHARED_IP_MESH_TLS_PIN_BYTES];
        for (index, pair) in encoded.as_bytes().chunks_exact(2).enumerate() {
            output[index] = (hex_nibble(pair[0])? << 4) | hex_nibble(pair[1])?;
        }
        Ok(Self(output))
    }

    #[must_use]
    pub const fn as_bytes(&self) -> &[u8; SHARED_IP_MESH_TLS_PIN_BYTES] {
        &self.0
    }

    #[must_use]
    pub fn encode(self) -> String {
        encode_hex(&self.0)
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct SharedIpMeshPeer {
    identity: IdentityPublicKey,
    certificate_pin: SharedIpMeshCertificatePin,
}

impl SharedIpMeshPeer {
    #[must_use]
    pub const fn new(
        identity: IdentityPublicKey,
        certificate_pin: SharedIpMeshCertificatePin,
    ) -> Self {
        Self {
            identity,
            certificate_pin,
        }
    }

    #[must_use]
    pub const fn identity(self) -> IdentityPublicKey {
        self.identity
    }

    #[must_use]
    pub const fn certificate_pin(self) -> SharedIpMeshCertificatePin {
        self.certificate_pin
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum SharedIpMeshTransport {
    Lan,
    WifiHotspot,
}

impl SharedIpMeshTransport {
    pub fn parse(encoded: &str) -> Result<Self, SharedIpMeshError> {
        match encoded {
            "lan" => Ok(Self::Lan),
            "wifi_hotspot" => Ok(Self::WifiHotspot),
            _ => Err(SharedIpMeshError::UnsupportedTransport),
        }
    }

    #[must_use]
    pub const fn encode(self) -> &'static str {
        match self {
            Self::Lan => "lan",
            Self::WifiHotspot => "wifi_hotspot",
        }
    }

    #[must_use]
    pub const fn local_mesh_transport(self) -> LocalMeshTransportKind {
        match self {
            Self::Lan => LocalMeshTransportKind::Lan,
            Self::WifiHotspot => LocalMeshTransportKind::WifiHotspot,
        }
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SharedIpMeshConfig {
    state_directory: PathBuf,
    transport: SharedIpMeshTransport,
    listen_endpoint: SocketAddr,
    peers: Vec<SharedIpMeshPeer>,
}

impl SharedIpMeshConfig {
    pub fn new(
        state_directory: PathBuf,
        transport: SharedIpMeshTransport,
        listen_endpoint: SocketAddr,
    ) -> Result<Self, SharedIpMeshError> {
        validate_state_directory(&state_directory)?;
        validate_listen_endpoint(listen_endpoint)?;
        Ok(Self {
            state_directory,
            transport,
            listen_endpoint,
            peers: Vec::new(),
        })
    }

    pub fn load(path: &Path) -> Result<Self, SharedIpMeshError> {
        let source = fs::read(path).map_err(SharedIpMeshError::ConfigRead)?;
        if source.len() > MAX_SHARED_IP_MESH_CONFIG_BYTES {
            return Err(SharedIpMeshError::ConfigTooLarge);
        }
        let source = std::str::from_utf8(&source).map_err(|_| SharedIpMeshError::ConfigUtf8)?;
        Self::parse(source)
    }

    pub fn parse(source: &str) -> Result<Self, SharedIpMeshError> {
        if source.len() > MAX_SHARED_IP_MESH_CONFIG_BYTES {
            return Err(SharedIpMeshError::ConfigTooLarge);
        }
        let mut version = None;
        let mut state_directory = None;
        let mut transport = None;
        let mut listen_endpoint = None;
        let mut peers = Vec::new();
        for line in source.lines() {
            let line = line.trim();
            if line.is_empty() || line.starts_with('#') {
                continue;
            }
            let (key, value) = line
                .split_once('=')
                .ok_or(SharedIpMeshError::InvalidConfigLine)?;
            let key = key.trim();
            match key {
                "mesh_version" => {
                    if version.replace(parse_version(value.trim())?).is_some() {
                        return Err(SharedIpMeshError::DuplicateConfigSetting);
                    }
                }
                "state_directory" => {
                    let value = parse_string(value.trim())?;
                    if state_directory.replace(PathBuf::from(value)).is_some() {
                        return Err(SharedIpMeshError::DuplicateConfigSetting);
                    }
                }
                "transport" => {
                    let value = parse_string(value.trim())?;
                    if transport
                        .replace(SharedIpMeshTransport::parse(&value)?)
                        .is_some()
                    {
                        return Err(SharedIpMeshError::DuplicateConfigSetting);
                    }
                }
                "listen_endpoint" => {
                    let value = parse_string(value.trim())?;
                    let endpoint = value
                        .parse()
                        .map_err(|_| SharedIpMeshError::InvalidListenEndpoint)?;
                    if listen_endpoint.replace(endpoint).is_some() {
                        return Err(SharedIpMeshError::DuplicateConfigSetting);
                    }
                }
                "peer" => peers.push(parse_peer(&parse_string(value.trim())?)?),
                _ => return Err(SharedIpMeshError::UnknownConfigSetting),
            }
        }
        if version.ok_or(SharedIpMeshError::MissingConfigVersion)? != SHARED_IP_MESH_CONFIG_VERSION
        {
            return Err(SharedIpMeshError::UnsupportedConfigVersion);
        }
        let mut config = Self::new(
            state_directory.ok_or(SharedIpMeshError::MissingStateDirectory)?,
            transport.ok_or(SharedIpMeshError::MissingTransport)?,
            listen_endpoint.ok_or(SharedIpMeshError::MissingListenEndpoint)?,
        )?;
        for peer in peers {
            config.add_peer(peer)?;
        }
        Ok(config)
    }

    pub fn write_new(&self, path: &Path) -> Result<(), SharedIpMeshError> {
        let mut options = fs::OpenOptions::new();
        options.write(true).create_new(true);
        let mut file = options.open(path).map_err(SharedIpMeshError::ConfigWrite)?;
        file.write_all(self.render().as_bytes())
            .map_err(SharedIpMeshError::ConfigWrite)
    }

    pub fn save(&self, path: &Path) -> Result<(), SharedIpMeshError> {
        fs::write(path, self.render()).map_err(SharedIpMeshError::ConfigWrite)
    }

    pub fn add_peer(&mut self, peer: SharedIpMeshPeer) -> Result<(), SharedIpMeshError> {
        if self.peers.len() == MAX_SHARED_IP_MESH_PEERS {
            return Err(SharedIpMeshError::TooManyPeers);
        }
        if self
            .peers
            .iter()
            .any(|configured| configured.identity == peer.identity)
        {
            return Err(SharedIpMeshError::DuplicatePeer);
        }
        self.peers.push(peer);
        Ok(())
    }

    #[must_use]
    pub fn state_directory(&self) -> &Path {
        &self.state_directory
    }

    #[must_use]
    pub const fn transport(&self) -> SharedIpMeshTransport {
        self.transport
    }

    #[must_use]
    pub const fn listen_endpoint(&self) -> SocketAddr {
        self.listen_endpoint
    }

    #[must_use]
    pub fn peers(&self) -> &[SharedIpMeshPeer] {
        &self.peers
    }

    #[must_use]
    pub fn peer(&self, identity: IdentityPublicKey) -> Option<SharedIpMeshPeer> {
        self.peers
            .iter()
            .copied()
            .find(|peer| peer.identity == identity)
    }

    fn render(&self) -> String {
        let mut output = format!(
            "mesh_version = {SHARED_IP_MESH_CONFIG_VERSION}\nstate_directory = \"{}\"\ntransport = \"{}\"\nlisten_endpoint = \"{}\"\n",
            escape_string(&self.state_directory.to_string_lossy()),
            self.transport.encode(),
            self.listen_endpoint
        );
        for peer in &self.peers {
            writeln!(
                output,
                "peer = \"{}:{}\"",
                encode_hex(peer.identity.as_bytes()),
                peer.certificate_pin.encode()
            )
            .expect("writing to String cannot fail");
        }
        output
    }
}

pub struct SharedIpMeshTlsIdentity {
    certificate: CertificateDer<'static>,
    private_key: Zeroizing<Vec<u8>>,
}

impl SharedIpMeshTlsIdentity {
    pub fn create<K: OsKeystore>(
        state_directory: &ClientStateDirectory,
        keystore: &mut K,
    ) -> Result<Self, SharedIpMeshError> {
        let key_entry = mesh_tls_key_entry()?;
        if keystore
            .load(&key_entry)
            .map_err(|_| SharedIpMeshError::Keystore)?
            .is_some()
            || state_directory.shared_ip_mesh_certificate_path().exists()
        {
            return Err(SharedIpMeshError::TlsAlreadyInitialized);
        }
        fs::create_dir_all(state_directory.root())
            .map_err(SharedIpMeshError::StateDirectoryWrite)?;
        let certificate = generate_simple_self_signed(vec![SHARED_IP_MESH_SERVER_NAME.to_owned()])
            .map_err(|_| SharedIpMeshError::TlsGeneration)?;
        let certificate_der = certificate.cert.der().clone();
        let private_key = Zeroizing::new(certificate.signing_key.serialize_der());
        let secret =
            KeystoreSecret::new(private_key.to_vec()).map_err(SharedIpMeshError::KeystoreSecret)?;
        keystore
            .store(&key_entry, &secret)
            .map_err(|_| SharedIpMeshError::Keystore)?;
        if let Err(error) = fs::write(
            state_directory.shared_ip_mesh_certificate_path(),
            certificate_der.as_ref(),
        ) {
            let _ = keystore.delete(&key_entry);
            return Err(SharedIpMeshError::StateDirectoryWrite(error));
        }
        Ok(Self {
            certificate: certificate_der,
            private_key,
        })
    }

    pub fn load<K: OsKeystore>(
        state_directory: &ClientStateDirectory,
        keystore: &K,
    ) -> Result<Self, SharedIpMeshError> {
        let certificate = fs::read(state_directory.shared_ip_mesh_certificate_path())
            .map_err(SharedIpMeshError::TlsCertificateRead)?;
        if certificate.is_empty() {
            return Err(SharedIpMeshError::InvalidTlsCertificate);
        }
        let private_key = keystore
            .load(&mesh_tls_key_entry()?)
            .map_err(|_| SharedIpMeshError::Keystore)?
            .ok_or(SharedIpMeshError::TlsNotInitialized)?;
        Ok(Self {
            certificate: CertificateDer::from(certificate),
            private_key: Zeroizing::new(private_key.as_bytes().to_vec()),
        })
    }

    pub fn create_or_load<K: OsKeystore>(
        state_directory: &ClientStateDirectory,
        keystore: &mut K,
    ) -> Result<Self, SharedIpMeshError> {
        match Self::load(state_directory, keystore) {
            Ok(identity) => Ok(identity),
            Err(SharedIpMeshError::TlsCertificateRead(error))
                if error.kind() == std::io::ErrorKind::NotFound =>
            {
                Self::create(state_directory, keystore)
            }
            Err(SharedIpMeshError::TlsNotInitialized) => Self::create(state_directory, keystore),
            Err(error) => Err(error),
        }
    }

    #[must_use]
    pub fn certificate_pin(&self) -> SharedIpMeshCertificatePin {
        SharedIpMeshCertificatePin::from_certificate_der(self.certificate.as_ref())
    }

    pub fn server_config(&self) -> Result<ServerConfig, SharedIpMeshError> {
        ServerConfig::with_single_cert(
            vec![self.certificate.clone()],
            PrivatePkcs8KeyDer::from(self.private_key.to_vec()).into(),
        )
        .map_err(|_| SharedIpMeshError::TlsConfiguration)
    }

    pub fn client_config(
        &self,
        expected_pin: SharedIpMeshCertificatePin,
    ) -> Result<ClientConfig, SharedIpMeshError> {
        let crypto = rustls::ClientConfig::builder()
            .dangerous()
            .with_custom_certificate_verifier(Arc::new(PinnedMeshCertificateVerifier::new(
                expected_pin,
            )))
            .with_no_client_auth();
        let crypto =
            QuicClientConfig::try_from(crypto).map_err(|_| SharedIpMeshError::TlsConfiguration)?;
        Ok(ClientConfig::new(Arc::new(crypto)))
    }
}

pub struct SharedIpMeshEndpoint {
    config: SharedIpMeshConfig,
    transport: DirectTransport,
    discovery: LanPeerDiscovery,
    advertisement: String,
}

impl SharedIpMeshEndpoint {
    pub fn start(
        config: SharedIpMeshConfig,
        local_identity: IdentityPublicKey,
        tls_identity: &SharedIpMeshTlsIdentity,
    ) -> Result<Self, SharedIpMeshError> {
        let transport =
            DirectTransport::bind(config.listen_endpoint, Some(tls_identity.server_config()?))
                .map_err(SharedIpMeshError::Direct)?;
        let bound = transport
            .local_address()
            .map_err(SharedIpMeshError::Direct)?;
        if bound != config.listen_endpoint {
            return Err(SharedIpMeshError::BoundEndpointMismatch);
        }
        let discovery = LanPeerDiscovery::start().map_err(SharedIpMeshError::Discovery)?;
        let profile = DirectProfileConfig::new(bound).map_err(SharedIpMeshError::Profile)?;
        let advertisement = match discovery.advertise_for_transport(
            local_identity,
            profile,
            config.transport.local_mesh_transport(),
        ) {
            Ok(advertisement) => advertisement,
            Err(error) => {
                transport.shutdown();
                let _ = discovery.shutdown();
                return Err(SharedIpMeshError::Discovery(error));
            }
        };
        Ok(Self {
            config,
            transport,
            discovery,
            advertisement,
        })
    }

    #[must_use]
    pub fn config(&self) -> &SharedIpMeshConfig {
        &self.config
    }

    pub fn browse(&self) -> Result<mdns_sd::Receiver<mdns_sd::ServiceEvent>, SharedIpMeshError> {
        self.discovery
            .browse()
            .map_err(SharedIpMeshError::Discovery)
    }

    pub fn resolve(
        &self,
        service: &mdns_sd::ResolvedService,
    ) -> Result<LanPeer, SharedIpMeshError> {
        let peer = LanPeerDiscovery::resolve(service).map_err(SharedIpMeshError::Discovery)?;
        if peer.transport() != self.config.transport.local_mesh_transport() {
            return Err(SharedIpMeshError::TransportMismatch);
        }
        if self.config.peer(peer.identity()).is_none() {
            return Err(SharedIpMeshError::UnknownPeer);
        }
        Ok(peer)
    }

    pub fn discover_trusted_peer(
        &self,
        identity: IdentityPublicKey,
        timeout: Duration,
    ) -> Result<LanPeer, SharedIpMeshError> {
        if self.config.peer(identity).is_none() {
            return Err(SharedIpMeshError::UnknownPeer);
        }
        let events = self.browse()?;
        let deadline = Instant::now() + timeout;
        loop {
            let remaining = deadline.saturating_duration_since(Instant::now());
            if remaining.is_zero() {
                return Err(SharedIpMeshError::DiscoveryTimedOut);
            }
            let Ok(event) = events.recv_timeout(remaining) else {
                return Err(SharedIpMeshError::DiscoveryTimedOut);
            };
            let mdns_sd::ServiceEvent::ServiceResolved(service) = event else {
                continue;
            };
            let Ok(peer) = LanPeerDiscovery::resolve(&service) else {
                continue;
            };
            if peer.identity() == identity
                && peer.transport() == self.config.transport.local_mesh_transport()
            {
                return Ok(peer);
            }
        }
    }

    pub async fn connect(
        &self,
        peer: LanPeer,
        local_identity: &IdentityKeypair,
        tls_identity: &SharedIpMeshTlsIdentity,
    ) -> Result<SharedIpMeshConnection, SharedIpMeshError> {
        if peer.transport() != self.config.transport.local_mesh_transport() {
            return Err(SharedIpMeshError::TransportMismatch);
        }
        let trusted = self
            .config
            .peer(peer.identity())
            .ok_or(SharedIpMeshError::UnknownPeer)?;
        let mut last_error = None;
        for endpoint in peer.endpoints() {
            match self
                .connect_to(trusted, *endpoint, local_identity, tls_identity)
                .await
            {
                Ok(connection) => return Ok(connection),
                Err(error) => last_error = Some(error),
            }
        }
        Err(last_error.unwrap_or(SharedIpMeshError::Discovery(
            LanPeerDiscoveryError::InvalidService,
        )))
    }

    pub async fn connect_to(
        &self,
        trusted: SharedIpMeshPeer,
        endpoint: DirectProfileConfig,
        local_identity: &IdentityKeypair,
        tls_identity: &SharedIpMeshTlsIdentity,
    ) -> Result<SharedIpMeshConnection, SharedIpMeshError> {
        if self.config.peer(trusted.identity()) != Some(trusted) {
            return Err(SharedIpMeshError::UnknownPeer);
        }
        let session_peer = LocalMeshPeer::new(
            self.config.transport.local_mesh_transport(),
            trusted.identity(),
            Some(endpoint),
        )
        .map_err(SharedIpMeshError::Peer)?;
        let mut session = AuthenticatedLocalMeshSession::initiate(
            &self.transport,
            LocalTransportAvailability::Available,
            session_peer,
            local_identity,
            tls_identity.client_config(trusted.certificate_pin())?,
            SHARED_IP_MESH_SERVER_NAME,
            DirectConnectionAttempts::default(),
        )
        .await
        .map_err(SharedIpMeshError::Session)?;
        let connection = session
            .handoff_for(self.config.transport.local_mesh_transport())
            .map_err(SharedIpMeshError::Session)?
            .into_connection_for(self.config.transport.local_mesh_transport())
            .map_err(SharedIpMeshError::Session)?;
        Ok(SharedIpMeshConnection::new(session_peer, connection))
    }

    pub async fn accept(
        &self,
        local_identity: &IdentityKeypair,
    ) -> Result<SharedIpMeshConnection, SharedIpMeshError> {
        let connection = self
            .transport
            .accept()
            .await
            .map_err(SharedIpMeshError::Direct)?;
        let expected = self
            .config
            .peers()
            .iter()
            .map(|peer| peer.identity())
            .collect::<Vec<_>>();
        let identity = connection
            .authenticate_responder_for(local_identity, &expected)
            .await
            .map_err(SharedIpMeshError::Direct)?;
        let peer = LocalMeshPeer::new(
            self.config.transport.local_mesh_transport(),
            identity,
            Some(
                DirectProfileConfig::new(connection.remote_address())
                    .map_err(SharedIpMeshError::Profile)?,
            ),
        )
        .map_err(SharedIpMeshError::Peer)?;
        Ok(SharedIpMeshConnection::new(peer, connection))
    }

    pub fn shutdown(&self) -> Result<(), SharedIpMeshError> {
        self.discovery
            .unadvertise(&self.advertisement)
            .map_err(SharedIpMeshError::Discovery)?;
        self.discovery
            .shutdown()
            .map_err(SharedIpMeshError::Discovery)?;
        self.transport.shutdown();
        Ok(())
    }
}

pub struct SharedIpMeshConnection {
    peer: LocalMeshPeer,
    connection: Option<DirectConnection>,
}

impl SharedIpMeshConnection {
    const fn new(peer: LocalMeshPeer, connection: DirectConnection) -> Self {
        Self {
            peer,
            connection: Some(connection),
        }
    }

    #[must_use]
    pub const fn peer(&self) -> LocalMeshPeer {
        self.peer
    }

    #[must_use]
    pub fn connection(&self) -> &DirectConnection {
        self.connection
            .as_ref()
            .unwrap_or_else(|| unreachable!("shared-IP mesh connection was transferred"))
    }

    #[must_use]
    pub fn into_connection(mut self) -> DirectConnection {
        self.connection
            .take()
            .unwrap_or_else(|| unreachable!("shared-IP mesh connection was transferred"))
    }
}

impl Drop for SharedIpMeshConnection {
    fn drop(&mut self) {
        if let Some(connection) = self.connection.take() {
            connection.close();
        }
    }
}

#[derive(Debug, thiserror::Error)]
pub enum SharedIpMeshError {
    #[error("shared-IP mesh configuration could not be read")]
    ConfigRead(#[source] std::io::Error),
    #[error("shared-IP mesh configuration could not be written")]
    ConfigWrite(#[source] std::io::Error),
    #[error("shared-IP mesh configuration exceeds its size limit")]
    ConfigTooLarge,
    #[error("shared-IP mesh configuration is not UTF-8")]
    ConfigUtf8,
    #[error("shared-IP mesh configuration has an invalid line")]
    InvalidConfigLine,
    #[error("shared-IP mesh configuration has an invalid quoted string")]
    InvalidConfigString,
    #[error("shared-IP mesh configuration repeats a setting")]
    DuplicateConfigSetting,
    #[error("shared-IP mesh configuration has an unknown setting")]
    UnknownConfigSetting,
    #[error("shared-IP mesh configuration is missing its version")]
    MissingConfigVersion,
    #[error("shared-IP mesh configuration version is unsupported")]
    UnsupportedConfigVersion,
    #[error("shared-IP mesh configuration is missing its state directory")]
    MissingStateDirectory,
    #[error("shared-IP mesh configuration state directory is invalid")]
    InvalidStateDirectory(#[source] ClientStateDirectoryError),
    #[error("shared-IP mesh configuration is missing its transport")]
    MissingTransport,
    #[error("shared-IP mesh transport is unsupported")]
    UnsupportedTransport,
    #[error("shared-IP mesh configuration is missing its listen endpoint")]
    MissingListenEndpoint,
    #[error("shared-IP mesh listen endpoint is invalid")]
    InvalidListenEndpoint,
    #[error("shared-IP mesh listen endpoint must use a specific address and nonzero port")]
    UnusableListenEndpoint,
    #[error("shared-IP mesh peer is invalid")]
    InvalidPeer,
    #[error("shared-IP mesh certificate pin is invalid")]
    InvalidCertificatePin,
    #[error("shared-IP mesh peer is duplicated")]
    DuplicatePeer,
    #[error("shared-IP mesh peer limit is exceeded")]
    TooManyPeers,
    #[error("shared-IP mesh TLS key entry is invalid")]
    InvalidTlsKeyEntry,
    #[error("shared-IP mesh TLS identity already exists")]
    TlsAlreadyInitialized,
    #[error("shared-IP mesh TLS identity does not exist")]
    TlsNotInitialized,
    #[error("shared-IP mesh TLS certificate could not be read")]
    TlsCertificateRead(#[source] std::io::Error),
    #[error("shared-IP mesh TLS certificate is invalid")]
    InvalidTlsCertificate,
    #[error("shared-IP mesh TLS certificate generation failed")]
    TlsGeneration,
    #[error("shared-IP mesh TLS configuration failed")]
    TlsConfiguration,
    #[error("shared-IP mesh state directory could not be written")]
    StateDirectoryWrite(#[source] std::io::Error),
    #[error("shared-IP mesh secret cannot be stored")]
    KeystoreSecret(#[source] KeystoreSecretError),
    #[error("shared-IP mesh keystore operation failed")]
    Keystore,
    #[error("shared-IP mesh direct endpoint failed: {0}")]
    Direct(#[source] DirectTransportError),
    #[error("shared-IP mesh bound a different endpoint than configured")]
    BoundEndpointMismatch,
    #[error("shared-IP mesh discovery failed: {0}")]
    Discovery(#[source] LanPeerDiscoveryError),
    #[error("shared-IP mesh discovered a different transport than configured")]
    TransportMismatch,
    #[error("shared-IP mesh peer is not explicitly trusted")]
    UnknownPeer,
    #[error("shared-IP mesh peer discovery timed out")]
    DiscoveryTimedOut,
    #[error("shared-IP mesh peer is invalid: {0}")]
    Peer(#[source] arachne_protocol::LocalMeshPeerError),
    #[error("shared-IP mesh direct profile is invalid: {0}")]
    Profile(#[source] arachne_protocol::DirectProfileConfigError),
    #[error("shared-IP mesh session failed: {0}")]
    Session(#[source] LocalMeshSessionError),
}

#[derive(Debug)]
struct PinnedMeshCertificateVerifier {
    pin: SharedIpMeshCertificatePin,
    provider: Arc<CryptoProvider>,
}

impl PinnedMeshCertificateVerifier {
    fn new(pin: SharedIpMeshCertificatePin) -> Self {
        Self {
            pin,
            provider: Arc::new(rustls::crypto::ring::default_provider()),
        }
    }
}

impl ServerCertVerifier for PinnedMeshCertificateVerifier {
    fn verify_server_cert(
        &self,
        end_entity: &CertificateDer<'_>,
        _intermediates: &[CertificateDer<'_>],
        _server_name: &ServerName<'_>,
        _ocsp_response: &[u8],
        _now: UnixTime,
    ) -> Result<ServerCertVerified, rustls::Error> {
        if SharedIpMeshCertificatePin::from_certificate_der(end_entity.as_ref()) != self.pin {
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

fn validate_state_directory(path: &Path) -> Result<(), SharedIpMeshError> {
    ClientStateDirectory::new(path)
        .map(|_| ())
        .map_err(SharedIpMeshError::InvalidStateDirectory)
}

fn validate_listen_endpoint(endpoint: SocketAddr) -> Result<(), SharedIpMeshError> {
    if endpoint.port() == 0 || endpoint.ip().is_unspecified() || is_scoped_ipv6(endpoint.ip()) {
        return Err(SharedIpMeshError::UnusableListenEndpoint);
    }
    Ok(())
}

fn parse_version(value: &str) -> Result<u8, SharedIpMeshError> {
    if value.is_empty() || !value.bytes().all(|byte| byte.is_ascii_digit()) {
        return Err(SharedIpMeshError::UnsupportedConfigVersion);
    }
    value
        .parse()
        .map_err(|_| SharedIpMeshError::UnsupportedConfigVersion)
}

fn parse_string(value: &str) -> Result<String, SharedIpMeshError> {
    let value = value
        .strip_prefix('"')
        .and_then(|value| value.strip_suffix('"'))
        .ok_or(SharedIpMeshError::InvalidConfigString)?;
    let mut output = String::with_capacity(value.len());
    let mut characters = value.chars();
    while let Some(character) = characters.next() {
        if character == '"' {
            return Err(SharedIpMeshError::InvalidConfigString);
        }
        if character != '\\' {
            output.push(character);
            continue;
        }
        match characters.next() {
            Some('"') => output.push('"'),
            Some('\\') => output.push('\\'),
            _ => return Err(SharedIpMeshError::InvalidConfigString),
        }
    }
    Ok(output)
}

fn parse_peer(value: &str) -> Result<SharedIpMeshPeer, SharedIpMeshError> {
    let (identity, pin) = value
        .split_once(':')
        .ok_or(SharedIpMeshError::InvalidPeer)?;
    if identity.len() != ED25519_PUBLIC_KEY_BYTES * 2 {
        return Err(SharedIpMeshError::InvalidPeer);
    }
    let mut identity_bytes = [0; ED25519_PUBLIC_KEY_BYTES];
    for (index, pair) in identity.as_bytes().chunks_exact(2).enumerate() {
        identity_bytes[index] = (hex_nibble(pair[0])? << 4) | hex_nibble(pair[1])?;
    }
    let identity = IdentityPublicKey::from_bytes(identity_bytes)
        .map_err(|_| SharedIpMeshError::InvalidPeer)?;
    Ok(SharedIpMeshPeer::new(
        identity,
        SharedIpMeshCertificatePin::decode(pin)?,
    ))
}

fn mesh_tls_key_entry() -> Result<KeystoreEntryName, SharedIpMeshError> {
    KeystoreEntryName::new(SHARED_IP_MESH_TLS_KEY_ENTRY.to_owned())
        .map_err(|_| SharedIpMeshError::InvalidTlsKeyEntry)
}

fn escape_string(value: &str) -> String {
    value.replace('\\', "\\\\").replace('"', "\\\"")
}

fn is_scoped_ipv6(address: IpAddr) -> bool {
    matches!(address, IpAddr::V6(address) if address.is_unicast_link_local())
}

fn hex_nibble(byte: u8) -> Result<u8, SharedIpMeshError> {
    match byte {
        b'0'..=b'9' => Ok(byte - b'0'),
        b'a'..=b'f' => Ok(byte - b'a' + 10),
        _ => Err(SharedIpMeshError::InvalidPeer),
    }
}

fn encode_hex(bytes: &[u8]) -> String {
    const HEX: &[u8; 16] = b"0123456789abcdef";
    let mut output = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        output.push(char::from(HEX[usize::from(byte >> 4)]));
        output.push(char::from(HEX[usize::from(byte & 0x0f)]));
    }
    output
}

#[cfg(test)]
mod tests {
    use std::{
        collections::BTreeMap,
        convert::Infallible,
        net::SocketAddr,
        path::PathBuf,
        sync::atomic::{AtomicU64, Ordering},
    };

    use arachne_core::{KeystoreEntryName, KeystoreSecret, OsKeystore};

    use super::{
        SHARED_IP_MESH_CONFIG_VERSION, SharedIpMeshCertificatePin, SharedIpMeshConfig,
        SharedIpMeshError, SharedIpMeshPeer, SharedIpMeshTlsIdentity, SharedIpMeshTransport,
    };

    static NEXT_TEST_DIRECTORY: AtomicU64 = AtomicU64::new(0);

    #[derive(Default)]
    struct MemoryKeystore(BTreeMap<String, Vec<u8>>);

    impl OsKeystore for MemoryKeystore {
        type Error = Infallible;

        fn load(&self, entry: &KeystoreEntryName) -> Result<Option<KeystoreSecret>, Self::Error> {
            Ok(self
                .0
                .get(entry.as_str())
                .map(|value| KeystoreSecret::new(value.clone()).unwrap()))
        }

        fn store(
            &mut self,
            entry: &KeystoreEntryName,
            secret: &KeystoreSecret,
        ) -> Result<(), Self::Error> {
            self.0
                .insert(entry.as_str().to_owned(), secret.as_bytes().to_vec());
            Ok(())
        }

        fn delete(&mut self, entry: &KeystoreEntryName) -> Result<(), Self::Error> {
            self.0.remove(entry.as_str());
            Ok(())
        }
    }

    #[test]
    fn config_round_trips_canonically() {
        let path = PathBuf::from("/var/lib/arachne/alice");
        let mut config = SharedIpMeshConfig::new(
            path,
            SharedIpMeshTransport::Lan,
            "192.0.2.10:4242".parse::<SocketAddr>().unwrap(),
        )
        .unwrap();
        let identity = arachne_core::IdentityKeypair::generate()
            .unwrap()
            .public_key();
        config
            .add_peer(SharedIpMeshPeer::new(
                identity,
                SharedIpMeshCertificatePin::from_certificate_der(b"peer certificate"),
            ))
            .unwrap();
        let encoded = format!(
            "# shared-IP mesh\nmesh_version = {SHARED_IP_MESH_CONFIG_VERSION}\nstate_directory = \"/var/lib/arachne/alice\"\ntransport = \"lan\"\nlisten_endpoint = \"192.0.2.10:4242\"\npeer = \"{}:{}\"\n",
            super::encode_hex(identity.as_bytes()),
            SharedIpMeshCertificatePin::from_certificate_der(b"peer certificate").encode()
        );
        assert_eq!(SharedIpMeshConfig::parse(&encoded).unwrap(), config);
    }

    #[test]
    fn config_rejects_unsupported_transport_and_duplicate_peer() {
        let mut config = SharedIpMeshConfig::new(
            PathBuf::from("/var/lib/arachne/alice"),
            SharedIpMeshTransport::WifiHotspot,
            "192.0.2.10:4242".parse().unwrap(),
        )
        .unwrap();
        let peer = SharedIpMeshPeer::new(
            arachne_core::IdentityKeypair::generate()
                .unwrap()
                .public_key(),
            SharedIpMeshCertificatePin::from_certificate_der(b"peer certificate"),
        );
        config.add_peer(peer).unwrap();
        assert!(matches!(
            config.add_peer(peer),
            Err(SharedIpMeshError::DuplicatePeer)
        ));
        assert!(matches!(
            SharedIpMeshConfig::parse(
                "mesh_version = 1\nstate_directory = \"/var/lib/arachne/alice\"\ntransport = \"wifi_direct\"\nlisten_endpoint = \"192.0.2.10:4242\"\n"
            ),
            Err(SharedIpMeshError::UnsupportedTransport)
        ));
    }

    #[test]
    fn pin_requires_canonical_lowercase_hex() {
        assert!(matches!(
            SharedIpMeshCertificatePin::decode("A0"),
            Err(SharedIpMeshError::InvalidCertificatePin)
        ));
    }

    #[test]
    fn tls_identity_persists_its_private_key_only_in_the_keystore() {
        let directory = std::env::temp_dir().join(format!(
            "arachne-shared-ip-mesh-unit-{}-{}",
            std::process::id(),
            NEXT_TEST_DIRECTORY.fetch_add(1, Ordering::Relaxed)
        ));
        let layout = crate::ClientStateDirectory::new(&directory).unwrap();
        let mut keystore = MemoryKeystore::default();
        let created = SharedIpMeshTlsIdentity::create(&layout, &mut keystore).unwrap();
        let loaded = SharedIpMeshTlsIdentity::load(&layout, &keystore).unwrap();
        assert_eq!(created.certificate_pin(), loaded.certificate_pin());
        assert!(loaded.server_config().is_ok());
        let certificate = std::fs::read(layout.shared_ip_mesh_certificate_path()).unwrap();
        assert!(!certificate.is_empty());
        std::fs::remove_dir_all(directory).unwrap();
    }
}
