use std::{
    future::Future,
    io,
    net::SocketAddr,
    sync::{Arc, Mutex},
    time::{SystemTime, UNIX_EPOCH},
};

use tokio::net::TcpListener;
use tokio_stream::wrappers::TcpListenerStream;
use tonic::{Request, Response, Status, transport::Server};
use yeokcham_relay_api::v1::{
    self,
    relay_service_server::{RelayService, RelayServiceServer},
};

use crate::{
    MailboxCapability, MailboxQuota, RelayDatabase, RelayDatabaseError, SelfHostedRelayConfig,
};

pub struct RelayServer {
    listener: TcpListener,
    service: RelayGrpcService,
}

#[derive(Debug, thiserror::Error)]
pub enum RelayServerError {
    #[error("relay server database could not start")]
    Database(#[from] RelayDatabaseError),
    #[error("relay server listener could not start")]
    Listener(#[source] io::Error),
    #[error("relay server stopped with a transport error")]
    Transport(#[source] tonic::transport::Error),
}

impl RelayServer {
    pub async fn bind(config: &SelfHostedRelayConfig) -> Result<Self, RelayServerError> {
        let database = RelayDatabase::open(config.database_path())?;
        TcpListener::bind(config.listen_address())
            .await
            .map(|listener| Self::from_listener(listener, database, config.mailbox_quota()))
            .map_err(RelayServerError::Listener)
    }

    pub fn local_addr(&self) -> Result<SocketAddr, RelayServerError> {
        self.listener
            .local_addr()
            .map_err(RelayServerError::Listener)
    }

    pub async fn serve_until<F>(self, shutdown: F) -> Result<(), RelayServerError>
    where
        F: Future<Output = ()>,
    {
        let Self { listener, service } = self;
        Server::builder()
            .add_service(RelayServiceServer::new(service))
            .serve_with_incoming_shutdown(TcpListenerStream::new(listener), shutdown)
            .await
            .map_err(RelayServerError::Transport)
    }

    fn from_listener(listener: TcpListener, database: RelayDatabase, quota: MailboxQuota) -> Self {
        Self {
            listener,
            service: RelayGrpcService::new(database, quota),
        }
    }
}

struct RelayGrpcService {
    database: Arc<Mutex<RelayDatabase>>,
    quota: MailboxQuota,
}

impl RelayGrpcService {
    fn new(database: RelayDatabase, quota: MailboxQuota) -> Self {
        Self {
            database: Arc::new(Mutex::new(database)),
            quota,
        }
    }
}

fn unavailable<T>() -> Result<Response<T>, Status> {
    Err(Status::unimplemented("relay RPC is not implemented"))
}

#[tonic::async_trait]
impl RelayService for RelayGrpcService {
    async fn register_mailbox(
        &self,
        request: Request<v1::RegisterMailboxRequest>,
    ) -> Result<Response<v1::RegisterMailboxResponse>, Status> {
        let capability = MailboxCapability::decode(&request.into_inner().mailbox_capability)
            .map_err(|_| Status::invalid_argument("mailbox capability is invalid"))?;
        let created_at = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map_err(|_| Status::internal("relay clock is unavailable"))?
            .as_secs();
        let mut database = self
            .database
            .lock()
            .map_err(|_| Status::internal("relay database is unavailable"))?;
        match database.register_mailbox(&capability, self.quota, created_at) {
            Ok(()) => Ok(Response::new(v1::RegisterMailboxResponse {})),
            Err(RelayDatabaseError::MailboxAlreadyRegistered) => {
                Err(Status::already_exists("mailbox is already registered"))
            }
            Err(_) => Err(Status::internal("mailbox registration failed")),
        }
    }

    async fn store_envelope(
        &self,
        _request: Request<v1::StoreEnvelopeRequest>,
    ) -> Result<Response<v1::StoreEnvelopeResponse>, Status> {
        unavailable()
    }

    async fn retrieve_envelopes(
        &self,
        _request: Request<v1::RetrieveEnvelopesRequest>,
    ) -> Result<Response<v1::RetrieveEnvelopesResponse>, Status> {
        unavailable()
    }

    async fn acknowledge_envelope(
        &self,
        _request: Request<v1::AcknowledgeEnvelopeRequest>,
    ) -> Result<Response<v1::AcknowledgeEnvelopeResponse>, Status> {
        unavailable()
    }

    async fn upload_attachment_chunk(
        &self,
        _request: Request<v1::UploadAttachmentChunkRequest>,
    ) -> Result<Response<v1::UploadAttachmentChunkResponse>, Status> {
        unavailable()
    }

    async fn download_attachment_chunk(
        &self,
        _request: Request<v1::DownloadAttachmentChunkRequest>,
    ) -> Result<Response<v1::DownloadAttachmentChunkResponse>, Status> {
        unavailable()
    }

    async fn get_mailbox_quota(
        &self,
        _request: Request<v1::GetMailboxQuotaRequest>,
    ) -> Result<Response<v1::GetMailboxQuotaResponse>, Status> {
        unavailable()
    }

    async fn store_envelope_with_receipt(
        &self,
        _request: Request<v1::StoreEnvelopeRequest>,
    ) -> Result<Response<v1::StoreEnvelopeWithReceiptResponse>, Status> {
        unavailable()
    }
}

#[cfg(test)]
mod tests {
    use std::{
        path::PathBuf,
        sync::atomic::{AtomicU64, Ordering},
    };

    use rusqlite::Connection;
    use tokio::{net::TcpListener, sync::oneshot};
    use tonic::Code;
    use yeokcham_protocol::{
        MAILBOX_CAPABILITY_TOKEN_BYTES, MAILBOX_IDENTIFIER_BYTES, MailboxCapability,
    };
    use yeokcham_relay_api::v1::{
        RegisterMailboxRequest, StoreEnvelopeRequest, relay_service_client::RelayServiceClient,
    };

    use super::{RelayServer, RelayServerError};
    use crate::{MailboxQuota, RelayDatabase, RelayRetentionPolicy, SelfHostedRelayConfig};

    static NEXT_TEST_PATH: AtomicU64 = AtomicU64::new(0);

    #[tokio::test]
    async fn registers_canonical_mailboxes_over_the_generated_relay_contract() {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let address = listener.local_addr().unwrap();
        let server = RelayServer::from_listener(
            listener,
            RelayDatabase::from_connection(Connection::open_in_memory().unwrap()).unwrap(),
            MailboxQuota::new(1024).unwrap(),
        );
        let (shutdown_sender, shutdown_receiver) = oneshot::channel();
        let server = server.serve_until(async move {
            let _ = shutdown_receiver.await;
        });
        let client = async {
            let mut client = RelayServiceClient::connect(format!("http://{address}"))
                .await
                .unwrap();
            let capability = MailboxCapability::new(
                [0x11; MAILBOX_IDENTIFIER_BYTES],
                [0x22; MAILBOX_CAPABILITY_TOKEN_BYTES],
            )
            .unwrap()
            .encode()
            .unwrap();
            client
                .register_mailbox(RegisterMailboxRequest {
                    mailbox_capability: capability.clone(),
                })
                .await
                .unwrap();
            assert_eq!(
                client
                    .register_mailbox(RegisterMailboxRequest {
                        mailbox_capability: capability,
                    })
                    .await
                    .unwrap_err()
                    .code(),
                Code::AlreadyExists
            );
            assert_eq!(
                client
                    .register_mailbox(RegisterMailboxRequest {
                        mailbox_capability: vec![0],
                    })
                    .await
                    .unwrap_err()
                    .code(),
                Code::InvalidArgument
            );
            assert_eq!(
                client
                    .store_envelope(StoreEnvelopeRequest {
                        mailbox_capability: vec![1; 53],
                        envelope: vec![2],
                    })
                    .await
                    .unwrap_err()
                    .code(),
                Code::Unimplemented
            );
            shutdown_sender.send(()).unwrap();
        };
        let (server, ()) = tokio::join!(server, client);
        assert!(server.is_ok());
        assert!(TcpListener::bind(address).await.is_ok());
    }

    #[tokio::test]
    async fn fails_before_listening_when_the_database_cannot_open() {
        let number = NEXT_TEST_PATH.fetch_add(1, Ordering::Relaxed);
        let path = PathBuf::from("/tmp")
            .join(format!("yc-relay-missing-{}-{number}", std::process::id()))
            .join("relay.sqlite");
        let config = SelfHostedRelayConfig::new(
            "127.0.0.1:1".parse().unwrap(),
            path,
            MailboxQuota::new(1024).unwrap(),
            RelayRetentionPolicy::new(60).unwrap(),
        )
        .unwrap();

        assert!(matches!(
            RelayServer::bind(&config).await,
            Err(RelayServerError::Database(_))
        ));
    }
}
