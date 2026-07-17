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
use yeokcham_core::RelaySigningKeypair;
use yeokcham_relay_api::v1::{
    self,
    relay_service_server::{RelayService, RelayServiceServer},
};

use crate::{
    AttachmentIdentifier, EncryptedAttachmentChunk, EncryptedMessageEnvelope, MailboxCapability,
    MailboxQuota, RelayDatabase, RelayDatabaseError, RelayRetentionPolicy, SelfHostedRelayConfig,
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
    pub async fn bind(
        config: &SelfHostedRelayConfig,
        relay: RelaySigningKeypair,
    ) -> Result<Self, RelayServerError> {
        let database = RelayDatabase::open(config.database_path())?;
        TcpListener::bind(config.listen_address())
            .await
            .map(|listener| {
                Self::from_listener(
                    listener,
                    database,
                    config.mailbox_quota(),
                    config.retention(),
                    relay,
                )
            })
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

    fn from_listener(
        listener: TcpListener,
        database: RelayDatabase,
        quota: MailboxQuota,
        retention: RelayRetentionPolicy,
        relay: RelaySigningKeypair,
    ) -> Self {
        Self {
            listener,
            service: RelayGrpcService::new(database, quota, retention, relay),
        }
    }
}

struct RelayGrpcService {
    database: Arc<Mutex<RelayDatabase>>,
    quota: MailboxQuota,
    retention: RelayRetentionPolicy,
    relay: RelaySigningKeypair,
}

impl RelayGrpcService {
    fn new(
        database: RelayDatabase,
        quota: MailboxQuota,
        retention: RelayRetentionPolicy,
        relay: RelaySigningKeypair,
    ) -> Self {
        Self {
            database: Arc::new(Mutex::new(database)),
            quota,
            retention,
            relay,
        }
    }
}

fn current_unix_seconds() -> Result<u64, Status> {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map_err(|_| Status::internal("relay clock is unavailable"))
        .map(|duration| duration.as_secs())
}

#[tonic::async_trait]
impl RelayService for RelayGrpcService {
    async fn register_mailbox(
        &self,
        request: Request<v1::RegisterMailboxRequest>,
    ) -> Result<Response<v1::RegisterMailboxResponse>, Status> {
        let capability = MailboxCapability::decode(&request.into_inner().mailbox_capability)
            .map_err(|_| Status::invalid_argument("mailbox capability is invalid"))?;
        let created_at = current_unix_seconds()?;
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
        request: Request<v1::StoreEnvelopeRequest>,
    ) -> Result<Response<v1::StoreEnvelopeResponse>, Status> {
        let request = request.into_inner();
        let capability = MailboxCapability::decode(&request.mailbox_capability)
            .map_err(|_| Status::invalid_argument("mailbox capability is invalid"))?;
        let envelope = EncryptedMessageEnvelope::decode(&request.envelope)
            .map_err(|_| Status::invalid_argument("envelope is invalid"))?;
        let received_at = current_unix_seconds()?;
        let mut database = self
            .database
            .lock()
            .map_err(|_| Status::internal("relay database is unavailable"))?;
        match database.insert_envelope(&capability, &envelope, received_at, self.retention) {
            Ok(sequence) => Ok(Response::new(v1::StoreEnvelopeResponse { sequence })),
            Err(RelayDatabaseError::InvalidCapability) => {
                Err(Status::permission_denied("mailbox capability is invalid"))
            }
            Err(RelayDatabaseError::QuotaExceeded) => {
                Err(Status::resource_exhausted("mailbox quota is exhausted"))
            }
            Err(_) => Err(Status::internal("envelope storage failed")),
        }
    }

    async fn retrieve_envelopes(
        &self,
        request: Request<v1::RetrieveEnvelopesRequest>,
    ) -> Result<Response<v1::RetrieveEnvelopesResponse>, Status> {
        let request = request.into_inner();
        let capability = MailboxCapability::decode(&request.mailbox_capability)
            .map_err(|_| Status::invalid_argument("mailbox capability is invalid"))?;
        let limit = u16::try_from(request.limit)
            .map_err(|_| Status::invalid_argument("retrieval limit is invalid"))?;
        let now = current_unix_seconds()?;
        let envelopes = {
            let mut database = self
                .database
                .lock()
                .map_err(|_| Status::internal("relay database is unavailable"))?;
            match database.retrieve_envelopes(&capability, request.after_sequence, now, limit) {
                Ok(envelopes) => envelopes,
                Err(RelayDatabaseError::InvalidCapability) => {
                    return Err(Status::permission_denied("mailbox capability is invalid"));
                }
                Err(
                    RelayDatabaseError::InvalidRetrievalLimit
                    | RelayDatabaseError::TimestampOutOfRange,
                ) => {
                    return Err(Status::invalid_argument("retrieval request is invalid"));
                }
                Err(_) => return Err(Status::internal("envelope retrieval failed")),
            }
        };
        let envelopes = envelopes
            .into_iter()
            .map(|envelope| {
                Ok(v1::RelayEnvelope {
                    sequence: envelope.sequence(),
                    envelope: envelope
                        .envelope()
                        .encode()
                        .map_err(|_| Status::internal("stored envelope is invalid"))?,
                    received_at: envelope.received_at(),
                    expires_at: envelope.expires_at(),
                })
            })
            .collect::<Result<Vec<_>, Status>>()?;
        Ok(Response::new(v1::RetrieveEnvelopesResponse { envelopes }))
    }

    async fn acknowledge_envelope(
        &self,
        request: Request<v1::AcknowledgeEnvelopeRequest>,
    ) -> Result<Response<v1::AcknowledgeEnvelopeResponse>, Status> {
        let request = request.into_inner();
        let capability = MailboxCapability::decode(&request.mailbox_capability)
            .map_err(|_| Status::invalid_argument("mailbox capability is invalid"))?;
        let mut database = self
            .database
            .lock()
            .map_err(|_| Status::internal("relay database is unavailable"))?;
        match database.acknowledge_envelope(&capability, request.sequence) {
            Ok(()) => Ok(Response::new(v1::AcknowledgeEnvelopeResponse {})),
            Err(RelayDatabaseError::InvalidCapability) => {
                Err(Status::permission_denied("mailbox capability is invalid"))
            }
            Err(RelayDatabaseError::UnknownEnvelope) => {
                Err(Status::not_found("mailbox envelope is unknown"))
            }
            Err(RelayDatabaseError::TimestampOutOfRange) => {
                Err(Status::invalid_argument("envelope sequence is invalid"))
            }
            Err(_) => Err(Status::internal("envelope acknowledgement failed")),
        }
    }

    async fn upload_attachment_chunk(
        &self,
        request: Request<v1::UploadAttachmentChunkRequest>,
    ) -> Result<Response<v1::UploadAttachmentChunkResponse>, Status> {
        let request = request.into_inner();
        let capability = MailboxCapability::decode(&request.mailbox_capability)
            .map_err(|_| Status::invalid_argument("mailbox capability is invalid"))?;
        let chunk = EncryptedAttachmentChunk::decode(&request.chunk)
            .map_err(|_| Status::invalid_argument("attachment chunk is invalid"))?;
        let received_at = current_unix_seconds()?;
        let mut database = self
            .database
            .lock()
            .map_err(|_| Status::internal("relay database is unavailable"))?;
        match database.store_attachment_chunk(&capability, &chunk, received_at, self.retention) {
            Ok(stored) => Ok(Response::new(v1::UploadAttachmentChunkResponse { stored })),
            Err(RelayDatabaseError::InvalidCapability) => {
                Err(Status::permission_denied("mailbox capability is invalid"))
            }
            Err(RelayDatabaseError::QuotaExceeded) => {
                Err(Status::resource_exhausted("mailbox quota is exhausted"))
            }
            Err(RelayDatabaseError::AttachmentChunkConflict) => Err(Status::already_exists(
                "attachment chunk conflicts with stored chunk",
            )),
            Err(
                RelayDatabaseError::InvalidAttachmentChunk
                | RelayDatabaseError::TimestampOutOfRange,
            ) => Err(Status::invalid_argument("attachment upload is invalid")),
            Err(_) => Err(Status::internal("attachment upload failed")),
        }
    }

    async fn download_attachment_chunk(
        &self,
        request: Request<v1::DownloadAttachmentChunkRequest>,
    ) -> Result<Response<v1::DownloadAttachmentChunkResponse>, Status> {
        let request = request.into_inner();
        let capability = MailboxCapability::decode(&request.mailbox_capability)
            .map_err(|_| Status::invalid_argument("mailbox capability is invalid"))?;
        let identifier = AttachmentIdentifier::from_bytes(
            request
                .attachment_identifier
                .as_slice()
                .try_into()
                .map_err(|_| Status::invalid_argument("attachment identifier is invalid"))?,
        )
        .map_err(|_| Status::invalid_argument("attachment identifier is invalid"))?;
        let now = current_unix_seconds()?;
        let chunk = {
            let mut database = self
                .database
                .lock()
                .map_err(|_| Status::internal("relay database is unavailable"))?;
            match database.retrieve_attachment_chunk(
                &capability,
                identifier,
                request.chunk_index,
                now,
            ) {
                Ok(chunk) => chunk,
                Err(RelayDatabaseError::InvalidCapability) => {
                    return Err(Status::permission_denied("mailbox capability is invalid"));
                }
                Err(RelayDatabaseError::UnknownAttachmentChunk) => {
                    return Err(Status::not_found("attachment chunk is unavailable"));
                }
                Err(RelayDatabaseError::TimestampOutOfRange) => {
                    return Err(Status::invalid_argument("attachment download is invalid"));
                }
                Err(_) => return Err(Status::internal("attachment download failed")),
            }
        };
        let chunk = chunk
            .encode()
            .map_err(|_| Status::internal("stored attachment chunk is invalid"))?;
        Ok(Response::new(v1::DownloadAttachmentChunkResponse { chunk }))
    }

    async fn get_mailbox_quota(
        &self,
        request: Request<v1::GetMailboxQuotaRequest>,
    ) -> Result<Response<v1::GetMailboxQuotaResponse>, Status> {
        let capability = MailboxCapability::decode(&request.into_inner().mailbox_capability)
            .map_err(|_| Status::invalid_argument("mailbox capability is invalid"))?;
        let database = self
            .database
            .lock()
            .map_err(|_| Status::internal("relay database is unavailable"))?;
        match database.mailbox_quota(&capability) {
            Ok(quota) => Ok(Response::new(v1::GetMailboxQuotaResponse {
                capacity_bytes: quota.quota().bytes(),
                used_bytes: quota.used_bytes(),
                remaining_bytes: quota.remaining_bytes(),
            })),
            Err(RelayDatabaseError::InvalidCapability) => {
                Err(Status::permission_denied("mailbox capability is invalid"))
            }
            Err(_) => Err(Status::internal("mailbox quota lookup failed")),
        }
    }

    async fn store_envelope_with_receipt(
        &self,
        request: Request<v1::StoreEnvelopeRequest>,
    ) -> Result<Response<v1::StoreEnvelopeWithReceiptResponse>, Status> {
        let request = request.into_inner();
        let capability = MailboxCapability::decode(&request.mailbox_capability)
            .map_err(|_| Status::invalid_argument("mailbox capability is invalid"))?;
        let envelope = EncryptedMessageEnvelope::decode(&request.envelope)
            .map_err(|_| Status::invalid_argument("envelope is invalid"))?;
        let received_at = current_unix_seconds()?;
        let receipt = {
            let mut database = self
                .database
                .lock()
                .map_err(|_| Status::internal("relay database is unavailable"))?;
            match database.insert_envelope_with_receipt(
                &capability,
                &envelope,
                received_at,
                self.retention,
                &self.relay,
            ) {
                Ok(receipt) => receipt,
                Err(RelayDatabaseError::InvalidCapability) => {
                    return Err(Status::permission_denied("mailbox capability is invalid"));
                }
                Err(RelayDatabaseError::QuotaExceeded) => {
                    return Err(Status::resource_exhausted("mailbox quota is exhausted"));
                }
                Err(RelayDatabaseError::TimestampOutOfRange) => {
                    return Err(Status::invalid_argument("envelope storage is invalid"));
                }
                Err(_) => return Err(Status::internal("receipt storage failed")),
            }
        };
        let receipt = receipt
            .encode()
            .map_err(|_| Status::internal("receipt encoding failed"))?;
        Ok(Response::new(v1::StoreEnvelopeWithReceiptResponse {
            receipt,
        }))
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
    use tonic::{Code, transport::Channel};
    use yeokcham_core::RelaySigningKeypair;
    use yeokcham_protocol::{
        ATTACHMENT_CHUNK_BYTES, AttachmentIdentifier, AttachmentKey, EncryptedAttachmentChunk,
        EncryptedMessageEnvelope, MAILBOX_CAPABILITY_TOKEN_BYTES, MAILBOX_IDENTIFIER_BYTES,
        MAX_ENCODED_ATTACHMENT_CHUNK_BYTES, MailboxCapability, RelayStorageReceipt,
    };
    use yeokcham_relay_api::v1::{
        AcknowledgeEnvelopeRequest, DownloadAttachmentChunkRequest, GetMailboxQuotaRequest,
        GetMailboxQuotaResponse, RegisterMailboxRequest, RetrieveEnvelopesRequest,
        StoreEnvelopeRequest, StoreEnvelopeWithReceiptResponse, UploadAttachmentChunkRequest,
        relay_service_client::RelayServiceClient,
    };

    use super::{RelayServer, RelayServerError};
    use crate::{MailboxQuota, RelayDatabase, RelayRetentionPolicy, SelfHostedRelayConfig};

    static NEXT_TEST_PATH: AtomicU64 = AtomicU64::new(0);

    #[tokio::test]
    async fn registers_and_stores_canonical_mailboxes_over_the_generated_relay_contract() {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let address = listener.local_addr().unwrap();
        let server = RelayServer::from_listener(
            listener,
            RelayDatabase::from_connection(Connection::open_in_memory().unwrap()).unwrap(),
            MailboxQuota::new(5).unwrap(),
            RelayRetentionPolicy::new(60).unwrap(),
            RelaySigningKeypair::generate().unwrap(),
        );
        let (shutdown_sender, shutdown_receiver) = oneshot::channel();
        let server = server.serve_until(async move {
            let _ = shutdown_receiver.await;
        });
        let client = async {
            let mut client = RelayServiceClient::connect(format!("http://{address}"))
                .await
                .unwrap();
            let capability = capability(0x11, 0x22);
            assert_registration_contract(&mut client, &capability).await;
            assert_envelope_storage_contract(&mut client, &capability).await;
            assert_quota_contract(&mut client, &capability).await;
            assert_retrieval_contract(&mut client, &capability).await;
            assert_acknowledgement_contract(&mut client, &capability).await;
            shutdown_sender.send(()).unwrap();
        };
        let (server, ()) = tokio::join!(server, client);
        assert!(server.is_ok());
        assert!(TcpListener::bind(address).await.is_ok());
    }

    fn capability(mailbox_byte: u8, token_byte: u8) -> Vec<u8> {
        MailboxCapability::new(
            [mailbox_byte; MAILBOX_IDENTIFIER_BYTES],
            [token_byte; MAILBOX_CAPABILITY_TOKEN_BYTES],
        )
        .unwrap()
        .encode()
        .unwrap()
    }

    async fn assert_registration_contract(
        client: &mut RelayServiceClient<Channel>,
        encoded_capability: &[u8],
    ) {
        client
            .register_mailbox(RegisterMailboxRequest {
                mailbox_capability: encoded_capability.to_vec(),
            })
            .await
            .unwrap();
        assert_eq!(
            client
                .register_mailbox(RegisterMailboxRequest {
                    mailbox_capability: encoded_capability.to_vec(),
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
    }

    async fn assert_envelope_storage_contract(
        client: &mut RelayServiceClient<Channel>,
        encoded_capability: &[u8],
    ) {
        let envelope = envelope();
        assert_eq!(
            client
                .store_envelope(StoreEnvelopeRequest {
                    mailbox_capability: encoded_capability.to_vec(),
                    envelope: envelope.clone(),
                })
                .await
                .unwrap()
                .into_inner()
                .sequence,
            0
        );
        assert_eq!(
            client
                .store_envelope(StoreEnvelopeRequest {
                    mailbox_capability: encoded_capability.to_vec(),
                    envelope: envelope.clone(),
                })
                .await
                .unwrap_err()
                .code(),
            Code::ResourceExhausted
        );
        assert_eq!(
            client
                .store_envelope(StoreEnvelopeRequest {
                    mailbox_capability: encoded_capability.to_vec(),
                    envelope: vec![0],
                })
                .await
                .unwrap_err()
                .code(),
            Code::InvalidArgument
        );
        assert_eq!(
            client
                .store_envelope(StoreEnvelopeRequest {
                    mailbox_capability: capability(0x33, 0x44),
                    envelope,
                })
                .await
                .unwrap_err()
                .code(),
            Code::PermissionDenied
        );
    }

    async fn assert_retrieval_contract(
        client: &mut RelayServiceClient<Channel>,
        encoded_capability: &[u8],
    ) {
        let retrieved = client
            .retrieve_envelopes(RetrieveEnvelopesRequest {
                mailbox_capability: encoded_capability.to_vec(),
                after_sequence: None,
                limit: 1,
            })
            .await
            .unwrap()
            .into_inner();
        assert_eq!(retrieved.envelopes.len(), 1);
        assert_eq!(retrieved.envelopes[0].sequence, 0);
        assert_eq!(retrieved.envelopes[0].envelope, envelope());
        assert!(
            client
                .retrieve_envelopes(RetrieveEnvelopesRequest {
                    mailbox_capability: encoded_capability.to_vec(),
                    after_sequence: Some(0),
                    limit: 1,
                })
                .await
                .unwrap()
                .into_inner()
                .envelopes
                .is_empty()
        );
        assert_eq!(
            client
                .retrieve_envelopes(RetrieveEnvelopesRequest {
                    mailbox_capability: encoded_capability.to_vec(),
                    after_sequence: None,
                    limit: 0,
                })
                .await
                .unwrap_err()
                .code(),
            Code::InvalidArgument
        );
        assert_eq!(
            client
                .retrieve_envelopes(RetrieveEnvelopesRequest {
                    mailbox_capability: capability(0x33, 0x44),
                    after_sequence: None,
                    limit: 1,
                })
                .await
                .unwrap_err()
                .code(),
            Code::PermissionDenied
        );
    }

    async fn assert_quota_contract(
        client: &mut RelayServiceClient<Channel>,
        encoded_capability: &[u8],
    ) {
        assert_eq!(
            client
                .get_mailbox_quota(GetMailboxQuotaRequest {
                    mailbox_capability: encoded_capability.to_vec(),
                })
                .await
                .unwrap()
                .into_inner(),
            GetMailboxQuotaResponse {
                capacity_bytes: 5,
                used_bytes: 5,
                remaining_bytes: 0,
            }
        );
        assert_eq!(
            client
                .get_mailbox_quota(GetMailboxQuotaRequest {
                    mailbox_capability: capability(0x33, 0x44),
                })
                .await
                .unwrap_err()
                .code(),
            Code::PermissionDenied
        );
        assert_eq!(
            client
                .get_mailbox_quota(GetMailboxQuotaRequest {
                    mailbox_capability: vec![0],
                })
                .await
                .unwrap_err()
                .code(),
            Code::InvalidArgument
        );
    }

    async fn assert_acknowledgement_contract(
        client: &mut RelayServiceClient<Channel>,
        encoded_capability: &[u8],
    ) {
        client
            .acknowledge_envelope(AcknowledgeEnvelopeRequest {
                mailbox_capability: encoded_capability.to_vec(),
                sequence: 0,
            })
            .await
            .unwrap();
        assert_eq!(
            client
                .acknowledge_envelope(AcknowledgeEnvelopeRequest {
                    mailbox_capability: encoded_capability.to_vec(),
                    sequence: 0,
                })
                .await
                .unwrap_err()
                .code(),
            Code::NotFound
        );
        assert_eq!(
            client
                .acknowledge_envelope(AcknowledgeEnvelopeRequest {
                    mailbox_capability: capability(0x33, 0x44),
                    sequence: 0,
                })
                .await
                .unwrap_err()
                .code(),
            Code::PermissionDenied
        );
        assert_eq!(
            client
                .store_envelope(StoreEnvelopeRequest {
                    mailbox_capability: encoded_capability.to_vec(),
                    envelope: envelope(),
                })
                .await
                .unwrap()
                .into_inner()
                .sequence,
            0
        );
    }

    fn envelope() -> Vec<u8> {
        EncryptedMessageEnvelope::new(vec![1], vec![2])
            .unwrap()
            .encode()
            .unwrap()
    }

    #[tokio::test]
    async fn uploads_canonical_attachment_chunks_over_the_generated_relay_contract() {
        let chunk = attachment_chunk(0, 0x55);
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let address = listener.local_addr().unwrap();
        let server = RelayServer::from_listener(
            listener,
            RelayDatabase::from_connection(Connection::open_in_memory().unwrap()).unwrap(),
            MailboxQuota::new(u64::try_from(chunk.len()).unwrap()).unwrap(),
            RelayRetentionPolicy::new(60).unwrap(),
            RelaySigningKeypair::generate().unwrap(),
        );
        let (shutdown_sender, shutdown_receiver) = oneshot::channel();
        let server = server.serve_until(async move {
            let _ = shutdown_receiver.await;
        });
        let client = async {
            let mut client = RelayServiceClient::connect(format!("http://{address}"))
                .await
                .unwrap();
            let capability = capability(0x11, 0x22);
            client
                .register_mailbox(RegisterMailboxRequest {
                    mailbox_capability: capability.clone(),
                })
                .await
                .unwrap();
            assert_upload_contract(&mut client, &capability, &chunk).await;
            shutdown_sender.send(()).unwrap();
        };
        let (server, ()) = tokio::join!(server, client);
        assert!(server.is_ok());
    }

    async fn assert_upload_contract(
        client: &mut RelayServiceClient<Channel>,
        encoded_capability: &[u8],
        chunk: &[u8],
    ) {
        assert!(
            client
                .upload_attachment_chunk(UploadAttachmentChunkRequest {
                    mailbox_capability: encoded_capability.to_vec(),
                    chunk: chunk.to_vec(),
                })
                .await
                .unwrap()
                .into_inner()
                .stored
        );
        assert!(
            !client
                .upload_attachment_chunk(UploadAttachmentChunkRequest {
                    mailbox_capability: encoded_capability.to_vec(),
                    chunk: chunk.to_vec(),
                })
                .await
                .unwrap()
                .into_inner()
                .stored
        );
        assert_eq!(
            client
                .upload_attachment_chunk(UploadAttachmentChunkRequest {
                    mailbox_capability: encoded_capability.to_vec(),
                    chunk: attachment_chunk(0, 0x66),
                })
                .await
                .unwrap_err()
                .code(),
            Code::AlreadyExists
        );
        assert_eq!(
            client
                .upload_attachment_chunk(UploadAttachmentChunkRequest {
                    mailbox_capability: encoded_capability.to_vec(),
                    chunk: attachment_chunk(1, 0x77),
                })
                .await
                .unwrap_err()
                .code(),
            Code::ResourceExhausted
        );
        assert_eq!(
            client
                .upload_attachment_chunk(UploadAttachmentChunkRequest {
                    mailbox_capability: capability(0x33, 0x44),
                    chunk: chunk.to_vec(),
                })
                .await
                .unwrap_err()
                .code(),
            Code::PermissionDenied
        );
        assert_eq!(
            client
                .upload_attachment_chunk(UploadAttachmentChunkRequest {
                    mailbox_capability: encoded_capability.to_vec(),
                    chunk: vec![0; MAX_ENCODED_ATTACHMENT_CHUNK_BYTES + 1],
                })
                .await
                .unwrap_err()
                .code(),
            Code::InvalidArgument
        );
    }

    fn attachment_chunk(index: u32, byte: u8) -> Vec<u8> {
        let identifier = AttachmentIdentifier::from_bytes([0x55; 16]).unwrap();
        let key = AttachmentKey::derive(&[0x44; 32], identifier)
            .unwrap()
            .derive_chunk_key(index)
            .unwrap();
        EncryptedAttachmentChunk::encrypt(
            identifier,
            index,
            &key,
            &vec![byte; ATTACHMENT_CHUNK_BYTES],
        )
        .unwrap()
        .encode()
        .unwrap()
    }

    #[tokio::test]
    async fn downloads_canonical_attachment_chunks_over_the_generated_relay_contract() {
        let chunk = attachment_chunk(0, 0x55);
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let address = listener.local_addr().unwrap();
        let server = RelayServer::from_listener(
            listener,
            RelayDatabase::from_connection(Connection::open_in_memory().unwrap()).unwrap(),
            MailboxQuota::new(u64::try_from(chunk.len()).unwrap()).unwrap(),
            RelayRetentionPolicy::new(60).unwrap(),
            RelaySigningKeypair::generate().unwrap(),
        );
        let (shutdown_sender, shutdown_receiver) = oneshot::channel();
        let server = server.serve_until(async move {
            let _ = shutdown_receiver.await;
        });
        let client = async {
            let mut client = RelayServiceClient::connect(format!("http://{address}"))
                .await
                .unwrap();
            let capability = capability(0x11, 0x22);
            client
                .register_mailbox(RegisterMailboxRequest {
                    mailbox_capability: capability.clone(),
                })
                .await
                .unwrap();
            client
                .upload_attachment_chunk(UploadAttachmentChunkRequest {
                    mailbox_capability: capability.clone(),
                    chunk: chunk.clone(),
                })
                .await
                .unwrap();
            assert_download_contract(&mut client, &capability, &chunk).await;
            shutdown_sender.send(()).unwrap();
        };
        let (server, ()) = tokio::join!(server, client);
        assert!(server.is_ok());
    }

    async fn assert_download_contract(
        client: &mut RelayServiceClient<Channel>,
        encoded_capability: &[u8],
        chunk: &[u8],
    ) {
        assert_eq!(
            client
                .download_attachment_chunk(DownloadAttachmentChunkRequest {
                    mailbox_capability: encoded_capability.to_vec(),
                    attachment_identifier: vec![0x55; 16],
                    chunk_index: 0,
                })
                .await
                .unwrap()
                .into_inner()
                .chunk,
            chunk
        );
        assert_eq!(
            client
                .download_attachment_chunk(DownloadAttachmentChunkRequest {
                    mailbox_capability: encoded_capability.to_vec(),
                    attachment_identifier: vec![0x55; 16],
                    chunk_index: u32::MAX,
                })
                .await
                .unwrap_err()
                .code(),
            Code::NotFound
        );
        assert_eq!(
            client
                .download_attachment_chunk(DownloadAttachmentChunkRequest {
                    mailbox_capability: capability(0x33, 0x44),
                    attachment_identifier: vec![0x55; 16],
                    chunk_index: 0,
                })
                .await
                .unwrap_err()
                .code(),
            Code::PermissionDenied
        );
        assert_eq!(
            client
                .download_attachment_chunk(DownloadAttachmentChunkRequest {
                    mailbox_capability: encoded_capability.to_vec(),
                    attachment_identifier: vec![0; 16],
                    chunk_index: 0,
                })
                .await
                .unwrap_err()
                .code(),
            Code::InvalidArgument
        );
    }

    #[tokio::test]
    async fn stores_envelopes_with_verified_receipts_over_the_generated_relay_contract() {
        let relay = RelaySigningKeypair::generate().unwrap();
        let relay_public_key = relay.public_key();
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let address = listener.local_addr().unwrap();
        let server = RelayServer::from_listener(
            listener,
            RelayDatabase::from_connection(Connection::open_in_memory().unwrap()).unwrap(),
            MailboxQuota::new(5).unwrap(),
            RelayRetentionPolicy::new(60).unwrap(),
            relay,
        );
        let (shutdown_sender, shutdown_receiver) = oneshot::channel();
        let server = server.serve_until(async move {
            let _ = shutdown_receiver.await;
        });
        let client = async {
            let mut client = RelayServiceClient::connect(format!("http://{address}"))
                .await
                .unwrap();
            let capability = capability(0x11, 0x22);
            client
                .register_mailbox(RegisterMailboxRequest {
                    mailbox_capability: capability.clone(),
                })
                .await
                .unwrap();
            assert_receipt_contract(&mut client, &capability, &relay_public_key).await;
            shutdown_sender.send(()).unwrap();
        };
        let (server, ()) = tokio::join!(server, client);
        assert!(server.is_ok());
    }

    async fn assert_receipt_contract(
        client: &mut RelayServiceClient<Channel>,
        encoded_capability: &[u8],
        relay_public_key: &yeokcham_core::RelayPublicKey,
    ) {
        let receipt = client
            .store_envelope_with_receipt(StoreEnvelopeRequest {
                mailbox_capability: encoded_capability.to_vec(),
                envelope: envelope(),
            })
            .await
            .unwrap()
            .into_inner();
        assert_receipt(&receipt, relay_public_key);
        assert_eq!(
            client
                .store_envelope_with_receipt(StoreEnvelopeRequest {
                    mailbox_capability: encoded_capability.to_vec(),
                    envelope: envelope(),
                })
                .await
                .unwrap_err()
                .code(),
            Code::ResourceExhausted
        );
        assert_eq!(
            client
                .store_envelope_with_receipt(StoreEnvelopeRequest {
                    mailbox_capability: capability(0x33, 0x44),
                    envelope: envelope(),
                })
                .await
                .unwrap_err()
                .code(),
            Code::PermissionDenied
        );
        assert_eq!(
            client
                .store_envelope_with_receipt(StoreEnvelopeRequest {
                    mailbox_capability: encoded_capability.to_vec(),
                    envelope: vec![0],
                })
                .await
                .unwrap_err()
                .code(),
            Code::InvalidArgument
        );
    }

    fn assert_receipt(
        response: &StoreEnvelopeWithReceiptResponse,
        relay_public_key: &yeokcham_core::RelayPublicKey,
    ) {
        let receipt = RelayStorageReceipt::decode(&response.receipt).unwrap();
        receipt.verify().unwrap();
        assert_eq!(receipt.relay(), relay_public_key);
        assert_eq!(receipt.mailbox_id(), &[0x11; MAILBOX_IDENTIFIER_BYTES]);
        assert_eq!(receipt.sequence(), 0);
        assert!(receipt.expires_at() > receipt.received_at());
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
            RelayServer::bind(&config, RelaySigningKeypair::generate().unwrap()).await,
            Err(RelayServerError::Database(_))
        ));
    }
}
