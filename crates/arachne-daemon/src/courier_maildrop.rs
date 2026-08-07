use arachne_protocol::{
    AttachmentChunkError, AttachmentIdentifier, CourierBundle, CourierBundleError,
    EncryptedAttachmentChunk, EncryptedMessageEnvelope, EncryptedMessageError,
    MailboxCapabilityError, courier_directory_fetch_signing_input,
};
use arachne_relay_api::v1::{
    AcknowledgeEnvelopeRequest, DownloadAttachmentChunkRequest, FetchCourierBundleRequest,
    PublishCourierBundleRequest, RetrieveEnvelopesRequest, StoreEnvelopeRequest,
    UploadAttachmentChunkRequest, relay_service_client::RelayServiceClient,
};
use sha2::{Digest, Sha256};
use tonic::{Request, Status, transport::Channel};

use crate::{
    ExternalTorRuntime, RelayTlsEndpoint, RelayTlsEndpointError, RelayTlsPin, TorSocksError,
    TorSocksTarget, TorSocksTonicConnector, TorSocksTonicConnectorError,
};

#[derive(Debug)]
pub struct FetchedCourierBundle {
    bundle: CourierBundle,
    leased_one_time_prekey: Option<arachne_core::OneTimePrekeyId>,
}

impl FetchedCourierBundle {
    #[must_use]
    pub const fn bundle(&self) -> &CourierBundle {
        &self.bundle
    }

    #[must_use]
    pub const fn leased_one_time_prekey(&self) -> Option<arachne_core::OneTimePrekeyId> {
        self.leased_one_time_prekey
    }

    #[must_use]
    pub fn into_parts(self) -> (CourierBundle, Option<arachne_core::OneTimePrekeyId>) {
        (self.bundle, self.leased_one_time_prekey)
    }
}

pub const MAX_COURIER_MAILDROP_RETRIEVAL: u32 = 128;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct RetrievedCourierEnvelope {
    sequence: u64,
    envelope: EncryptedMessageEnvelope,
}

impl RetrievedCourierEnvelope {
    #[must_use]
    pub const fn sequence(&self) -> u64 {
        self.sequence
    }

    #[must_use]
    pub const fn envelope(&self) -> &EncryptedMessageEnvelope {
        &self.envelope
    }
}

#[derive(Clone, Copy, Debug)]
pub struct CourierMaildropClient {
    runtime: ExternalTorRuntime,
}

impl CourierMaildropClient {
    #[must_use]
    pub const fn new(runtime: ExternalTorRuntime) -> Self {
        Self { runtime }
    }

    pub async fn store_envelope(
        &self,
        recipient: &CourierBundle,
        envelope: &EncryptedMessageEnvelope,
    ) -> Result<u64, CourierMaildropError> {
        let capability = recipient
            .relay_invitation()
            .mailbox_capability()
            .encode()
            .map_err(CourierMaildropError::MailboxCapability)?;
        let envelope = envelope.encode().map_err(CourierMaildropError::Envelope)?;
        let idempotency_key = Sha256::digest(&envelope).to_vec();
        let mut client = self.connect(recipient).await?;
        client
            .store_envelope(Request::new(StoreEnvelopeRequest {
                mailbox_capability: capability,
                envelope,
                idempotency_key,
            }))
            .await
            .map_err(CourierMaildropError::RelayStatus)
            .map(|response| response.into_inner().sequence)
    }

    pub async fn publish_courier_bundle(
        &self,
        local_bundle: &CourierBundle,
    ) -> Result<(), CourierMaildropError> {
        let bundle = local_bundle
            .encode()
            .map_err(CourierMaildropError::Bundle)?;
        let mut client = self.connect(local_bundle).await?;
        client
            .publish_courier_bundle(Request::new(PublishCourierBundleRequest { bundle }))
            .await
            .map_err(CourierMaildropError::RelayStatus)?;
        Ok(())
    }

    pub async fn fetch_courier_bundle(
        &self,
        directory: &CourierBundle,
        requester: &arachne_core::IdentityKeypair,
        recipient: &arachne_core::IdentityPublicKey,
    ) -> Result<FetchedCourierBundle, CourierMaildropError> {
        let signature = requester.sign(&courier_directory_fetch_signing_input(recipient));
        let mut client = self.connect(directory).await?;
        let response = client
            .fetch_courier_bundle(Request::new(FetchCourierBundleRequest {
                recipient_identity: recipient.as_bytes().to_vec(),
                requester_identity: requester.public_key().as_bytes().to_vec(),
                requester_signature: signature.to_vec(),
            }))
            .await
            .map_err(CourierMaildropError::RelayStatus)?
            .into_inner();
        let bundle =
            CourierBundle::decode(&response.bundle).map_err(CourierMaildropError::Bundle)?;
        if &bundle.encode().map_err(CourierMaildropError::Bundle)? != &response.bundle {
            return Err(CourierMaildropError::NonCanonicalBundle);
        }
        if bundle.publisher() != recipient {
            return Err(CourierMaildropError::BundleRecipientMismatch);
        }
        let leased_one_time_prekey = response
            .leased_one_time_prekey_identifier
            .map(arachne_core::OneTimePrekeyId::new)
            .transpose()
            .map_err(|_| CourierMaildropError::InvalidLeasedOneTimePrekey)?;
        if let Some(identifier) = leased_one_time_prekey
            && !bundle
                .prekey_bundle()
                .one_time_prekeys()
                .iter()
                .any(|prekey| prekey.identifier() == identifier)
        {
            return Err(CourierMaildropError::InvalidLeasedOneTimePrekey);
        }
        Ok(FetchedCourierBundle {
            bundle,
            leased_one_time_prekey,
        })
    }

    pub async fn retrieve_envelopes(
        &self,
        local_bundle: &CourierBundle,
        after_sequence: Option<u64>,
    ) -> Result<Vec<RetrievedCourierEnvelope>, CourierMaildropError> {
        let capability = local_bundle
            .relay_invitation()
            .mailbox_capability()
            .encode()
            .map_err(CourierMaildropError::MailboxCapability)?;
        let mut client = self.connect(local_bundle).await?;
        let response = client
            .retrieve_envelopes(Request::new(RetrieveEnvelopesRequest {
                mailbox_capability: capability,
                after_sequence,
                limit: MAX_COURIER_MAILDROP_RETRIEVAL,
            }))
            .await
            .map_err(CourierMaildropError::RelayStatus)?
            .into_inner();
        if response.envelopes.len() > MAX_COURIER_MAILDROP_RETRIEVAL as usize {
            return Err(CourierMaildropError::TooManyEnvelopes);
        }
        response
            .envelopes
            .into_iter()
            .map(|stored| {
                let envelope = EncryptedMessageEnvelope::decode(&stored.envelope)
                    .map_err(CourierMaildropError::Envelope)?;
                if envelope.encode().map_err(CourierMaildropError::Envelope)? != stored.envelope {
                    return Err(CourierMaildropError::NonCanonicalEnvelope);
                }
                Ok(RetrievedCourierEnvelope {
                    sequence: stored.sequence,
                    envelope,
                })
            })
            .collect()
    }

    pub async fn acknowledge_envelope(
        &self,
        local_bundle: &CourierBundle,
        sequence: u64,
    ) -> Result<(), CourierMaildropError> {
        let capability = local_bundle
            .relay_invitation()
            .mailbox_capability()
            .encode()
            .map_err(CourierMaildropError::MailboxCapability)?;
        let mut client = self.connect(local_bundle).await?;
        client
            .acknowledge_envelope(Request::new(AcknowledgeEnvelopeRequest {
                mailbox_capability: capability,
                sequence,
            }))
            .await
            .map_err(CourierMaildropError::RelayStatus)?;
        Ok(())
    }

    pub async fn upload_attachment_chunk(
        &self,
        recipient: &CourierBundle,
        chunk: &EncryptedAttachmentChunk,
    ) -> Result<bool, CourierMaildropError> {
        let capability = recipient
            .relay_invitation()
            .mailbox_capability()
            .encode()
            .map_err(CourierMaildropError::MailboxCapability)?;
        let chunk = chunk
            .encode()
            .map_err(CourierMaildropError::AttachmentChunk)?;
        let mut client = self.connect(recipient).await?;
        client
            .upload_attachment_chunk(Request::new(UploadAttachmentChunkRequest {
                mailbox_capability: capability,
                chunk,
            }))
            .await
            .map_err(CourierMaildropError::RelayStatus)
            .map(|response| response.into_inner().stored)
    }

    pub async fn download_attachment_chunk(
        &self,
        local_bundle: &CourierBundle,
        identifier: AttachmentIdentifier,
        index: u32,
    ) -> Result<EncryptedAttachmentChunk, CourierMaildropError> {
        let capability = local_bundle
            .relay_invitation()
            .mailbox_capability()
            .encode()
            .map_err(CourierMaildropError::MailboxCapability)?;
        let mut client = self.connect(local_bundle).await?;
        let response = client
            .download_attachment_chunk(Request::new(DownloadAttachmentChunkRequest {
                mailbox_capability: capability,
                attachment_identifier: identifier.as_bytes().to_vec(),
                chunk_index: index,
            }))
            .await
            .map_err(CourierMaildropError::RelayStatus)?
            .into_inner();
        let chunk = EncryptedAttachmentChunk::decode(&response.chunk)
            .map_err(CourierMaildropError::AttachmentChunk)?;
        if chunk
            .encode()
            .map_err(CourierMaildropError::AttachmentChunk)?
            != response.chunk
        {
            return Err(CourierMaildropError::NonCanonicalAttachmentChunk);
        }
        Ok(chunk)
    }

    async fn connect(
        &self,
        bundle: &CourierBundle,
    ) -> Result<RelayServiceClient<Channel>, CourierMaildropError> {
        let endpoint = bundle.relay_invitation().endpoint();
        let hostname = endpoint.onion_hostname();
        let target = TorSocksTarget::new(hostname.clone(), endpoint.virtual_port())
            .map_err(CourierMaildropError::Socks)?;
        let tls = RelayTlsEndpoint::new(
            hostname,
            endpoint.virtual_port(),
            RelayTlsPin::from_bytes(*bundle.relay_tls_pin()),
        )
        .map_err(CourierMaildropError::Tls)?;
        let connector = TorSocksTonicConnector::new(self.runtime, target);
        let channel = tls
            .tonic_endpoint()
            .map_err(CourierMaildropError::Tls)?
            .connect_with_connector(connector)
            .await
            .map_err(CourierMaildropError::Transport)?;
        Ok(RelayServiceClient::new(channel))
    }
}

#[derive(Debug, thiserror::Error)]
pub enum CourierMaildropError {
    #[error("courier maildrop SOCKS target is invalid")]
    Socks(#[source] TorSocksError),
    #[error("courier maildrop TLS endpoint is invalid")]
    Tls(#[source] RelayTlsEndpointError),
    #[error("courier maildrop capability is invalid")]
    MailboxCapability(#[source] MailboxCapabilityError),
    #[error("courier maildrop envelope is invalid")]
    Envelope(#[source] EncryptedMessageError),
    #[error("courier maildrop bundle is invalid")]
    Bundle(#[source] CourierBundleError),
    #[error("courier maildrop attachment chunk is invalid")]
    AttachmentChunk(#[source] AttachmentChunkError),
    #[error("courier maildrop relay response contains too many envelopes")]
    TooManyEnvelopes,
    #[error("courier maildrop relay response contains a noncanonical envelope")]
    NonCanonicalEnvelope,
    #[error("courier maildrop relay response contains a noncanonical bundle")]
    NonCanonicalBundle,
    #[error("courier maildrop bundle does not belong to the requested recipient")]
    BundleRecipientMismatch,
    #[error("courier maildrop response has an invalid leased one-time prekey")]
    InvalidLeasedOneTimePrekey,
    #[error("courier maildrop relay response contains a noncanonical attachment chunk")]
    NonCanonicalAttachmentChunk,
    #[error("courier maildrop SOCKS connector failed")]
    Connector(#[source] TorSocksTonicConnectorError),
    #[error("courier maildrop TLS transport failed")]
    Transport(#[source] tonic::transport::Error),
    #[error("courier maildrop relay request failed")]
    RelayStatus(#[source] Status),
}
