#![forbid(unsafe_code)]

#[allow(clippy::all, clippy::pedantic, clippy::nursery)]
pub mod v1 {
    include!("generated/yeokcham.relay.v1.rs");
}

#[cfg(test)]
mod tests {
    use prost::Message;
    use tokio::net::TcpListener;
    use tokio_stream::wrappers::TcpListenerStream;
    use tonic::{Request, Response, Status, transport::Server};

    use super::v1::{
        AcknowledgeEnvelopeRequest, AcknowledgeEnvelopeResponse, DownloadAttachmentChunkRequest,
        DownloadAttachmentChunkResponse, GetMailboxQuotaRequest, GetMailboxQuotaResponse,
        RegisterMailboxRequest, RegisterMailboxResponse, RelayEnvelope, RetrieveEnvelopesRequest,
        RetrieveEnvelopesResponse, StoreEnvelopeRequest, StoreEnvelopeResponse,
        StoreEnvelopeWithReceiptResponse, UploadAttachmentChunkRequest,
        UploadAttachmentChunkResponse,
        relay_service_client::RelayServiceClient,
        relay_service_server::{RelayService, RelayServiceServer},
    };

    struct ContractService;

    #[tonic::async_trait]
    impl RelayService for ContractService {
        async fn register_mailbox(
            &self,
            request: Request<RegisterMailboxRequest>,
        ) -> Result<Response<RegisterMailboxResponse>, Status> {
            if request.into_inner().mailbox_capability.is_empty() {
                return Err(Status::invalid_argument("mailbox capability is required"));
            }
            Ok(Response::new(RegisterMailboxResponse {}))
        }

        async fn store_envelope(
            &self,
            request: Request<StoreEnvelopeRequest>,
        ) -> Result<Response<StoreEnvelopeResponse>, Status> {
            if request.into_inner().envelope.is_empty() {
                return Err(Status::invalid_argument("envelope is required"));
            }
            Ok(Response::new(StoreEnvelopeResponse { sequence: u64::MAX }))
        }

        async fn retrieve_envelopes(
            &self,
            request: Request<RetrieveEnvelopesRequest>,
        ) -> Result<Response<RetrieveEnvelopesResponse>, Status> {
            let request = request.into_inner();
            if request.limit == 0 || request.limit > 128 {
                return Err(Status::invalid_argument("retrieval limit is invalid"));
            }
            Ok(Response::new(RetrieveEnvelopesResponse {
                envelopes: vec![],
            }))
        }

        async fn acknowledge_envelope(
            &self,
            _request: Request<AcknowledgeEnvelopeRequest>,
        ) -> Result<Response<AcknowledgeEnvelopeResponse>, Status> {
            Err(Status::unimplemented("contract test"))
        }

        async fn upload_attachment_chunk(
            &self,
            _request: Request<UploadAttachmentChunkRequest>,
        ) -> Result<Response<UploadAttachmentChunkResponse>, Status> {
            Err(Status::unimplemented("contract test"))
        }

        async fn download_attachment_chunk(
            &self,
            _request: Request<DownloadAttachmentChunkRequest>,
        ) -> Result<Response<DownloadAttachmentChunkResponse>, Status> {
            Err(Status::unimplemented("contract test"))
        }

        async fn get_mailbox_quota(
            &self,
            _request: Request<GetMailboxQuotaRequest>,
        ) -> Result<Response<GetMailboxQuotaResponse>, Status> {
            Err(Status::unimplemented("contract test"))
        }

        async fn store_envelope_with_receipt(
            &self,
            _request: Request<StoreEnvelopeRequest>,
        ) -> Result<Response<StoreEnvelopeWithReceiptResponse>, Status> {
            Err(Status::unimplemented("contract test"))
        }
    }

    #[test]
    fn generated_relay_messages_preserve_every_field() {
        let capability = vec![0x83, 1, 0x50, 0x11];
        let envelope = vec![0x82, 0x41, 1, 0x41, 2];
        let store = StoreEnvelopeRequest {
            mailbox_capability: capability.clone(),
            envelope: envelope.clone(),
        };
        let decoded = StoreEnvelopeRequest::decode(store.encode_to_vec().as_slice()).unwrap();
        assert_eq!(decoded.mailbox_capability, capability);
        assert_eq!(decoded.envelope, envelope);

        let retrieval = RetrieveEnvelopesRequest {
            mailbox_capability: vec![3; 53],
            after_sequence: Some(u64::MAX),
            limit: 128,
        };
        assert_eq!(retrieval.after_sequence, Some(u64::MAX));
        assert_eq!(retrieval.limit, 128);
        let relay_envelope = RelayEnvelope {
            sequence: u64::MAX,
            envelope: vec![4; 5],
            received_at: u64::MAX - 1,
            expires_at: u64::MAX,
        };
        assert_eq!(relay_envelope.sequence, u64::MAX);
        assert_eq!(relay_envelope.expires_at, u64::MAX);

        let acknowledgement = AcknowledgeEnvelopeRequest {
            mailbox_capability: vec![5; 53],
            sequence: 6,
        };
        assert_eq!(acknowledgement.sequence, 6);
        let upload = UploadAttachmentChunkRequest {
            mailbox_capability: vec![7; 53],
            chunk: vec![8; 65_640],
        };
        assert_eq!(upload.chunk.len(), 65_640);
        let download = DownloadAttachmentChunkRequest {
            mailbox_capability: vec![9; 53],
            attachment_identifier: vec![10; 16],
            chunk_index: u32::MAX,
        };
        assert_eq!(download.attachment_identifier, vec![10; 16]);
        assert_eq!(download.chunk_index, u32::MAX);
        let quota = GetMailboxQuotaResponse {
            capacity_bytes: u64::MAX,
            used_bytes: u64::MAX - 1,
            remaining_bytes: 1,
        };
        assert_eq!(quota.remaining_bytes, 1);
        let receipt = StoreEnvelopeWithReceiptResponse {
            receipt: vec![11; 128],
        };
        assert_eq!(receipt.receipt.len(), 128);
    }

    #[tokio::test]
    async fn generated_relay_service_exercises_success_failure_and_boundary_requests() {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let address = listener.local_addr().unwrap();
        let server = tokio::spawn(async move {
            Server::builder()
                .add_service(RelayServiceServer::new(ContractService))
                .serve_with_incoming(TcpListenerStream::new(listener))
                .await
                .unwrap();
        });
        let mut client = RelayServiceClient::connect(format!("http://{address}"))
            .await
            .unwrap();

        let stored = client
            .store_envelope(StoreEnvelopeRequest {
                mailbox_capability: vec![1; 53],
                envelope: vec![2],
            })
            .await
            .unwrap()
            .into_inner();
        assert_eq!(stored.sequence, u64::MAX);
        let retrieved = client
            .retrieve_envelopes(RetrieveEnvelopesRequest {
                mailbox_capability: vec![1; 53],
                after_sequence: Some(u64::MAX),
                limit: 128,
            })
            .await
            .unwrap()
            .into_inner();
        assert!(retrieved.envelopes.is_empty());

        let empty_envelope = client
            .store_envelope(StoreEnvelopeRequest {
                mailbox_capability: vec![1; 53],
                envelope: vec![],
            })
            .await
            .unwrap_err();
        assert_eq!(empty_envelope.code(), tonic::Code::InvalidArgument);
        let oversized_limit = client
            .retrieve_envelopes(RetrieveEnvelopesRequest {
                mailbox_capability: vec![1; 53],
                after_sequence: None,
                limit: 129,
            })
            .await
            .unwrap_err();
        assert_eq!(oversized_limit.code(), tonic::Code::InvalidArgument);

        server.abort();
        let _ = server.await;
    }
}
