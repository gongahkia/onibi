use std::{
    fs,
    net::SocketAddr,
    path::PathBuf,
    sync::atomic::{AtomicU64, Ordering},
    time::Duration,
};

use tokio::{sync::oneshot, time};
use tonic::Code;
use yeokcham_core::RelaySigningKeypair;
use yeokcham_protocol::{
    EncryptedMessageEnvelope, MAILBOX_CAPABILITY_TOKEN_BYTES, MAILBOX_IDENTIFIER_BYTES,
    MailboxCapability,
};
use yeokcham_relay::{MailboxQuota, RelayRetentionPolicy, RelayServer, SelfHostedRelayConfig};
use yeokcham_relay_api::v1::{
    RegisterMailboxRequest, RetrieveEnvelopesRequest, StoreEnvelopeRequest,
    relay_service_client::RelayServiceClient,
};

static NEXT_DATABASE: AtomicU64 = AtomicU64::new(0);

#[tokio::test]
async fn recovers_durable_maildrop_state_after_a_grpc_transport_fault() {
    let address = available_loopback_address();
    let database_path = database_path();
    let configuration = SelfHostedRelayConfig::new(
        address,
        database_path.clone(),
        MailboxQuota::new(1024).unwrap(),
        RelayRetentionPolicy::new(60).unwrap(),
    )
    .unwrap();
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
    let (first_shutdown, first_server) = start_relay(&configuration).await;
    let mut first_client = relay_client(address).await;

    first_client
        .register_mailbox(RegisterMailboxRequest {
            mailbox_capability: capability.clone(),
        })
        .await
        .unwrap();
    first_client
        .store_envelope(StoreEnvelopeRequest {
            mailbox_capability: capability.clone(),
            envelope: envelope.clone(),
        })
        .await
        .unwrap();
    first_server.abort();
    assert!(first_server.await.unwrap_err().is_cancelled());
    drop(first_client);
    assert!(
        RelayServiceClient::connect(format!("http://{address}"))
            .await
            .is_err()
    );
    drop(first_shutdown);

    let (second_shutdown, second_server) = start_relay(&configuration).await;
    let mut recovered_client = relay_client(address).await;
    let retrieved = recovered_client
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
        recovered_client
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
    drop(recovered_client);
    second_shutdown.send(()).unwrap();
    assert!(second_server.await.unwrap().is_ok());
    fs::remove_file(database_path).unwrap();
}

async fn start_relay(
    configuration: &SelfHostedRelayConfig,
) -> (
    oneshot::Sender<()>,
    tokio::task::JoinHandle<Result<(), yeokcham_relay::RelayServerError>>,
) {
    let mut attempts = 0;
    let server = loop {
        match RelayServer::bind(configuration, RelaySigningKeypair::generate().unwrap()).await {
            Ok(server) => break server,
            Err(_) if attempts < 9 => {
                attempts += 1;
                time::sleep(Duration::from_millis(10)).await;
            }
            Err(error) => panic!("relay server did not recover: {error}"),
        }
    };
    let (shutdown_sender, shutdown_receiver) = oneshot::channel();
    let server = tokio::spawn(server.serve_until(async move {
        let _ = shutdown_receiver.await;
    }));
    (shutdown_sender, server)
}

async fn relay_client(address: SocketAddr) -> RelayServiceClient<tonic::transport::Channel> {
    RelayServiceClient::connect(format!("http://{address}"))
        .await
        .unwrap()
}

fn available_loopback_address() -> SocketAddr {
    let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
    listener.local_addr().unwrap()
}

fn database_path() -> PathBuf {
    let number = NEXT_DATABASE.fetch_add(1, Ordering::Relaxed);
    std::env::temp_dir().join(format!(
        "yeokcham-relay-grpc-recovery-{}-{number}.sqlite",
        std::process::id()
    ))
}
