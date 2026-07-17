use std::{future::Future, io, net::SocketAddr};

use tokio::net::TcpListener;
use tokio_stream::wrappers::TcpListenerStream;
use tonic::{Request, Response, Status, transport::Server};
use yeokcham_relay_api::v1::{
    self,
    relay_service_server::{RelayService, RelayServiceServer},
};

use crate::{RelayDatabase, RelayDatabaseError, SelfHostedRelayConfig};

pub struct RelayServer {
    listener: TcpListener,
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
        let _ = RelayDatabase::open(config.database_path())?;
        TcpListener::bind(config.listen_address())
            .await
            .map(Self::from_listener)
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
        Server::builder()
            .add_service(RelayServiceServer::new(RelayGrpcService))
            .serve_with_incoming_shutdown(TcpListenerStream::new(self.listener), shutdown)
            .await
            .map_err(RelayServerError::Transport)
    }

    const fn from_listener(listener: TcpListener) -> Self {
        Self { listener }
    }
}

struct RelayGrpcService;

fn unavailable<T>() -> Result<Response<T>, Status> {
    Err(Status::unimplemented("relay RPC is not implemented"))
}

#[tonic::async_trait]
impl RelayService for RelayGrpcService {
    async fn register_mailbox(
        &self,
        _request: Request<v1::RegisterMailboxRequest>,
    ) -> Result<Response<v1::RegisterMailboxResponse>, Status> {
        unavailable()
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

    use tokio::{net::TcpListener, sync::oneshot};
    use tonic::Code;
    use yeokcham_relay_api::v1::{StoreEnvelopeRequest, relay_service_client::RelayServiceClient};

    use super::{RelayServer, RelayServerError};
    use crate::{MailboxQuota, RelayRetentionPolicy, SelfHostedRelayConfig};

    static NEXT_TEST_PATH: AtomicU64 = AtomicU64::new(0);

    #[tokio::test]
    async fn serves_the_generated_relay_contract_and_releases_the_listener() {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let address = listener.local_addr().unwrap();
        let server = RelayServer::from_listener(listener);
        let (shutdown_sender, shutdown_receiver) = oneshot::channel();
        let server = server.serve_until(async move {
            let _ = shutdown_receiver.await;
        });
        let client = async {
            let mut client = RelayServiceClient::connect(format!("http://{address}"))
                .await
                .unwrap();
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
