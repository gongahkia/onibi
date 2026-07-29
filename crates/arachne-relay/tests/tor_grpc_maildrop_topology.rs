use std::{
    fs,
    io::ErrorKind,
    net::SocketAddr,
    path::PathBuf,
    sync::atomic::{AtomicU64, Ordering},
    time::Duration,
};

use arachne_core::RelaySigningKeypair;
use arachne_daemon::{
    ExternalTorRuntimeConfig, RelayTlsEndpoint, RelayTlsPin, TorSocksTarget, TorSocksTonicConnector,
};
use arachne_protocol::{
    EncryptedMessageEnvelope, MAILBOX_CAPABILITY_TOKEN_BYTES, MAILBOX_IDENTIFIER_BYTES,
    MailboxCapability,
};
use arachne_relay::{MailboxQuota, RelayRetentionPolicy, RelayServer, SelfHostedRelayConfig};
use arachne_relay_api::v1::{
    RegisterMailboxRequest, RetrieveEnvelopesRequest, StoreEnvelopeRequest,
    relay_service_client::RelayServiceClient,
};
use tokio::{
    io::{AsyncReadExt, AsyncWriteExt, copy_bidirectional},
    net::{TcpListener, TcpStream},
    sync::oneshot,
};
use tonic::{
    Code,
    transport::{Channel, Identity, ServerTlsConfig},
};

const ONION: &str = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.onion";
const MAILDROP_PORT: u16 = 4444;

static NEXT_DATABASE: AtomicU64 = AtomicU64::new(0);

#[tokio::test]
async fn tor_socks_grpc_topology_reaches_the_real_relay_maildrop() {
    let certificate = rcgen::generate_simple_self_signed(vec!["relay.example".to_owned()]).unwrap();
    let relay_address = available_loopback_address();
    let database_path = database_path();
    let configuration = SelfHostedRelayConfig::new(
        relay_address,
        database_path.clone(),
        MailboxQuota::new(1024).unwrap(),
        RelayRetentionPolicy::new(60).unwrap(),
    )
    .unwrap();
    let relay = RelayServer::bind(&configuration, RelaySigningKeypair::generate().unwrap())
        .await
        .unwrap()
        .with_tls_config(ServerTlsConfig::new().identity(Identity::from_pem(
            certificate.cert.pem(),
            certificate.signing_key.serialize_pem(),
        )));
    let endpoint = RelayTlsEndpoint::new(
        ONION.to_owned(),
        MAILDROP_PORT,
        RelayTlsPin::from_certificate_der(certificate.cert.der().as_ref()),
    )
    .unwrap();
    let socks_listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let runtime =
        ExternalTorRuntimeConfig::new(socks_listener.local_addr().unwrap(), Duration::from_secs(1))
            .unwrap()
            .runtime();
    let connector = TorSocksTonicConnector::new(
        runtime,
        TorSocksTarget::new(ONION.to_owned(), MAILDROP_PORT).unwrap(),
    );
    let (shutdown_sender, shutdown_receiver) = oneshot::channel();
    let relay = relay.serve_until(async move {
        let _ = shutdown_receiver.await;
    });
    let socks = tokio::spawn(forward_socks_connection(socks_listener, relay_address));
    let client = async {
        let channel = endpoint
            .tonic_endpoint()
            .unwrap()
            .connect_with_connector(connector)
            .await
            .unwrap();
        let mut client = RelayServiceClient::new(channel);
        exercise_relay_maildrop(&mut client).await;
        drop(client);
        shutdown_sender.send(()).unwrap();
    };
    let (relay, ()) = tokio::join!(relay, client);
    assert!(relay.is_ok());
    socks.await.unwrap();
    fs::remove_file(database_path).unwrap();
}

async fn exercise_relay_maildrop(client: &mut RelayServiceClient<Channel>) {
    let capability = MailboxCapability::new(
        [0x11; MAILBOX_IDENTIFIER_BYTES],
        [0x22; MAILBOX_CAPABILITY_TOKEN_BYTES],
    )
    .unwrap()
    .encode()
    .unwrap();
    let envelope = EncryptedMessageEnvelope::new(vec![0xa1], vec![0xb2, 0xc3])
        .unwrap()
        .encode()
        .unwrap();

    assert_eq!(
        client
            .store_envelope(StoreEnvelopeRequest {
                mailbox_capability: capability.clone(),
                envelope: envelope.clone(),
            })
            .await
            .unwrap_err()
            .code(),
        Code::PermissionDenied
    );
    client
        .register_mailbox(RegisterMailboxRequest {
            mailbox_capability: capability.clone(),
        })
        .await
        .unwrap();
    assert_eq!(
        client
            .store_envelope(StoreEnvelopeRequest {
                mailbox_capability: capability.clone(),
                envelope: envelope.clone(),
            })
            .await
            .unwrap()
            .into_inner()
            .sequence,
        0
    );
    let retrieved = client
        .retrieve_envelopes(RetrieveEnvelopesRequest {
            mailbox_capability: capability.clone(),
            after_sequence: None,
            limit: 1,
        })
        .await
        .unwrap()
        .into_inner();
    assert_eq!(retrieved.envelopes.len(), 1);
    assert_eq!(retrieved.envelopes[0].sequence, 0);
    assert_eq!(retrieved.envelopes[0].envelope, envelope);
    assert_eq!(
        client
            .retrieve_envelopes(RetrieveEnvelopesRequest {
                mailbox_capability: capability,
                after_sequence: None,
                limit: 0,
            })
            .await
            .unwrap_err()
            .code(),
        Code::InvalidArgument
    );
}

fn available_loopback_address() -> SocketAddr {
    let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
    listener.local_addr().unwrap()
}

fn database_path() -> PathBuf {
    let number = NEXT_DATABASE.fetch_add(1, Ordering::Relaxed);
    std::env::temp_dir().join(format!(
        "arachne-tor-grpc-maildrop-{}-{number}.sqlite",
        std::process::id()
    ))
}

async fn forward_socks_connection(listener: TcpListener, relay_address: SocketAddr) {
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
    assert_eq!(u16::from_be_bytes(port), MAILDROP_PORT);
    let mut relay = TcpStream::connect(relay_address).await.unwrap();
    client
        .write_all(&[5, 0, 0, 1, 0, 0, 0, 0, 0, 0])
        .await
        .unwrap();
    if let Err(error) = copy_bidirectional(&mut client, &mut relay).await {
        assert!(matches!(
            error.kind(),
            ErrorKind::BrokenPipe | ErrorKind::ConnectionReset
        ));
    }
}
