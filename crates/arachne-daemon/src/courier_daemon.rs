use arachne_core::OsKeystore;
use arachne_protocol::{
    CourierAttachmentReferenceError, CourierFrame, DeliveryAcknowledgement,
    EncryptedMessageEnvelope, MessageContentType, MessagePayload, MessagePayloadError,
    RelayInvitation,
};

use crate::{
    AttachmentJournalStoreError, AttachmentSubmissionStore, AttachmentSubmissionStoreError,
    AttachmentTransferJournalStore, COURIER_ONE_TIME_PREKEY_REPLENISH_THRESHOLD,
    COURIER_ONE_TIME_PREKEY_TARGET, ClientIdentity, ClientIdentityError, ClientProfile,
    ClientProfileError, ClientStateDirectory, ContactStatus, ContactStore, ContactStoreError,
    CourierAttachmentJobError, CourierAttachmentJobState, CourierAttachmentJobStore,
    CourierBundleStore, CourierBundleStoreError, CourierCryptographer, CourierCryptographerError,
    CourierDaemonConfig, CourierMaildropClient, CourierMaildropError,
    CourierOneTimePrekeyInventory, CourierOneTimePrekeyInventoryError, CourierSessionStore,
    CourierSessionStoreError, SenderOutbox, SenderOutboxError,
};

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct CourierDaemonCycle {
    uploaded: u16,
    received: u16,
    acknowledged: u16,
    expired: u16,
    failed: u16,
    attachment_chunks: u16,
}

impl CourierDaemonCycle {
    #[must_use]
    pub const fn uploaded(self) -> u16 {
        self.uploaded
    }

    #[must_use]
    pub const fn received(self) -> u16 {
        self.received
    }

    #[must_use]
    pub const fn acknowledged(self) -> u16 {
        self.acknowledged
    }

    #[must_use]
    pub const fn expired(self) -> u16 {
        self.expired
    }

    #[must_use]
    pub const fn failed(self) -> u16 {
        self.failed
    }

    #[must_use]
    pub const fn attachment_chunks(self) -> u16 {
        self.attachment_chunks
    }
}

pub struct CourierDaemon {
    layout: ClientStateDirectory,
    identity: ClientIdentity,
    contacts: ContactStore,
    bundles: CourierBundleStore,
    sessions: CourierSessionStore,
    prekeys: CourierOneTimePrekeyInventory,
    attachment_jobs: CourierAttachmentJobStore,
    attachment_submissions: AttachmentSubmissionStore,
    outbox: SenderOutbox,
    cryptographer: CourierCryptographer,
    maildrop: CourierMaildropClient,
    local_bundle: arachne_protocol::CourierBundle,
    directory_published: bool,
}

impl CourierDaemon {
    pub fn start<K: OsKeystore>(
        config: &CourierDaemonConfig,
        keystore: &mut K,
        now_unix_seconds: u64,
    ) -> Result<Self, CourierDaemonError> {
        let layout = ClientStateDirectory::new(config.state_directory())
            .map_err(CourierDaemonError::StateDirectory)?;
        let profile =
            ClientProfile::open_or_create(&layout).map_err(CourierDaemonError::Profile)?;
        let identity = profile
            .load_identity(keystore)
            .map_err(CourierDaemonError::Identity)?;
        let contacts = ContactStore::open(&layout.contacts_path(), keystore)
            .map_err(CourierDaemonError::Contacts)?;
        let bundles = CourierBundleStore::open(&layout.courier_bundles_path(), keystore)
            .map_err(CourierDaemonError::Bundles)?;
        let local_bundle = bundles
            .bundle_for(&identity.public_key(), now_unix_seconds)
            .map_err(CourierDaemonError::Bundles)?
            .ok_or(CourierDaemonError::LocalBundleUnavailable)?;
        let sessions = CourierSessionStore::open(&layout.courier_sessions_path(), keystore)
            .map_err(CourierDaemonError::Sessions)?;
        let prekeys =
            CourierOneTimePrekeyInventory::open(&layout.courier_one_time_prekeys_path(), keystore)
                .map_err(CourierDaemonError::Prekeys)?;
        let attachment_jobs =
            CourierAttachmentJobStore::open(&layout.courier_attachment_jobs_path(), keystore)
                .map_err(CourierDaemonError::AttachmentJobs)?;
        let attachment_submissions = AttachmentSubmissionStore::new(layout.clone());
        let outbox = SenderOutbox::open(&layout.outbox_path(), keystore)
            .map_err(CourierDaemonError::Outbox)?;
        let cryptographer = CourierCryptographer::load_or_create_for_profile(
            identity.keypair(),
            profile.id(),
            keystore,
        )
        .map_err(CourierDaemonError::Cryptographer)?;
        Ok(Self {
            layout,
            identity,
            contacts,
            bundles,
            sessions,
            prekeys,
            attachment_jobs,
            attachment_submissions,
            outbox,
            cryptographer,
            maildrop: CourierMaildropClient::new(config.tor().runtime()),
            local_bundle,
            directory_published: false,
        })
    }

    pub async fn run_cycle(
        &mut self,
        now_unix_seconds: u64,
    ) -> Result<CourierDaemonCycle, CourierDaemonError> {
        if self.prekeys.available_count() < COURIER_ONE_TIME_PREKEY_REPLENISH_THRESHOLD {
            self.prekeys
                .replenish(COURIER_ONE_TIME_PREKEY_TARGET)
                .map_err(CourierDaemonError::Prekeys)?;
            self.refresh_local_bundle(now_unix_seconds)?;
        }
        self.local_bundle
            .validate(now_unix_seconds)
            .map_err(CourierDaemonError::LocalBundle)?;
        if !self.directory_published {
            self.maildrop
                .publish_courier_bundle(&self.local_bundle)
                .await
                .map_err(CourierDaemonError::Maildrop)?;
            self.prekeys
                .mark_unpublished_as_advertised()
                .map_err(CourierDaemonError::Prekeys)?;
            self.directory_published = true;
        }
        let mut cycle = CourierDaemonCycle::default();
        cycle.expired = u16::try_from(
            self.outbox
                .expire_due_deliveries(now_unix_seconds)
                .map_err(CourierDaemonError::Outbox)?
                .len(),
        )
        .unwrap_or(u16::MAX);
        for job in self
            .attachment_jobs
            .expire_due(now_unix_seconds)
            .map_err(CourierDaemonError::AttachmentJobs)?
        {
            self.attachment_submissions
                .delete(job.attachment_identifier())
                .map_err(CourierDaemonError::AttachmentSubmission)?;
            cycle.expired = cycle.expired.saturating_add(1);
        }
        cycle.attachment_chunks = self.process_attachment_jobs(now_unix_seconds).await?;

        for message in self.outbox.messages().to_vec() {
            if message.relay_uploaded() {
                continue;
            }
            let frame = CourierFrame::from_envelope(message.envelope())
                .map_err(CourierDaemonError::QueuedFrame)?;
            if frame.sender() != &self.identity.public_key()
                || frame.message_identifier() != Some(message.identifier())
            {
                return Err(CourierDaemonError::QueuedMessageMismatch);
            }
            self.require_verified_contact(message.recipient())?;
            let recipient = self
                .bundles
                .bundle_for(message.recipient(), now_unix_seconds)
                .map_err(CourierDaemonError::Bundles)?
                .ok_or(CourierDaemonError::MissingRecipientBundle)?;
            match self
                .maildrop
                .store_envelope(&recipient, message.envelope())
                .await
            {
                Ok(_) => {
                    self.outbox
                        .mark_relay_uploaded(message.identifier())
                        .map_err(CourierDaemonError::Outbox)?;
                    cycle.uploaded = cycle.uploaded.saturating_add(1);
                }
                Err(_) => match self.outbox.begin_delivery_attempt(message.identifier()) {
                    Ok(_) => {}
                    Err(SenderOutboxError::AttemptsExhausted) => {
                        self.outbox
                            .fail_delivery(message.identifier())
                            .map_err(CourierDaemonError::Outbox)?;
                        cycle.failed = cycle.failed.saturating_add(1);
                    }
                    Err(error) => return Err(CourierDaemonError::Outbox(error)),
                },
            }
        }

        let received = self
            .maildrop
            .retrieve_envelopes(&self.local_bundle, None)
            .await
            .map_err(CourierDaemonError::Maildrop)?;
        for stored in received {
            let delivered = self
                .process_incoming(stored.envelope(), now_unix_seconds)
                .await?;
            self.maildrop
                .acknowledge_envelope(&self.local_bundle, stored.sequence())
                .await
                .map_err(CourierDaemonError::Maildrop)?;
            cycle.received = cycle.received.saturating_add(1);
            if delivered {
                cycle.acknowledged = cycle.acknowledged.saturating_add(1);
            }
        }
        Ok(cycle)
    }

    pub fn inbox_messages(&self) -> impl Iterator<Item = &crate::CourierInboxMessage> {
        self.sessions.inbox_messages()
    }

    async fn process_incoming(
        &mut self,
        envelope: &EncryptedMessageEnvelope,
        now_unix_seconds: u64,
    ) -> Result<bool, CourierDaemonError> {
        let frame =
            CourierFrame::from_envelope(envelope).map_err(CourierDaemonError::IncomingFrame)?;
        self.require_verified_contact(frame.sender())?;
        match &frame {
            CourierFrame::Acknowledgement {
                sender,
                acknowledgement,
            } => {
                if sender != acknowledgement.recipient() {
                    return Err(CourierDaemonError::AcknowledgementSenderMismatch);
                }
                match self.outbox.acknowledge_delivery(acknowledgement) {
                    Ok(message) => {
                        if let Some(job) = self
                            .attachment_jobs
                            .remove_for_message(message.identifier())
                            .map_err(CourierDaemonError::AttachmentJobs)?
                        {
                            self.attachment_submissions
                                .delete(job.attachment_identifier())
                                .map_err(CourierDaemonError::AttachmentSubmission)?;
                        }
                        Ok(true)
                    }
                    Err(SenderOutboxError::UnknownMessageIdentifier) => Ok(false),
                    Err(error) => Err(CourierDaemonError::Outbox(error)),
                }
            }
            CourierFrame::Bootstrap {
                sender,
                message_identifier,
                ..
            }
            | CourierFrame::Ratchet {
                sender,
                message_identifier,
                ..
            } => {
                if !self.sessions.inbox_contains(*message_identifier) {
                    let one_time_prekey = match &frame {
                        CourierFrame::Bootstrap { initial, .. } => initial
                            .one_time_prekey()
                            .map(|identifier| {
                                self.prekeys
                                    .load(identifier)
                                    .ok_or(CourierDaemonError::MissingOneTimePrekey)
                            })
                            .transpose()?,
                        _ => None,
                    };
                    let (message, session) = self
                        .cryptographer
                        .decrypt_for_receive(&frame, &self.sessions, one_time_prekey)
                        .map_err(CourierDaemonError::Cryptographer)?
                        .ok_or(CourierDaemonError::IncomingFrameMissingPayload)?;
                    self.sessions
                        .commit_received(sender, &session, message, now_unix_seconds)
                        .map_err(CourierDaemonError::Sessions)?;
                    if let CourierFrame::Bootstrap { initial, .. } = &frame
                        && let Some(identifier) = initial.one_time_prekey()
                    {
                        self.prekeys
                            .take(identifier)
                            .map_err(CourierDaemonError::Prekeys)?;
                        if self.prekeys.available_count()
                            < COURIER_ONE_TIME_PREKEY_REPLENISH_THRESHOLD
                        {
                            self.prekeys
                                .replenish(COURIER_ONE_TIME_PREKEY_TARGET)
                                .map_err(CourierDaemonError::Prekeys)?;
                            self.refresh_local_bundle(now_unix_seconds)?;
                        }
                    }
                }
                self.send_delivery_acknowledgement(sender, *message_identifier, now_unix_seconds)
                    .await?;
                Ok(false)
            }
        }
    }

    async fn send_delivery_acknowledgement(
        &self,
        recipient: &arachne_core::IdentityPublicKey,
        message_identifier: arachne_protocol::MessageIdentifier,
        now_unix_seconds: u64,
    ) -> Result<(), CourierDaemonError> {
        let bundle = self
            .bundles
            .bundle_for(recipient, now_unix_seconds)
            .map_err(CourierDaemonError::Bundles)?
            .ok_or(CourierDaemonError::MissingRecipientBundle)?;
        let acknowledgement = DeliveryAcknowledgement::create(
            self.identity.keypair(),
            message_identifier,
            now_unix_seconds,
        )
        .map_err(CourierDaemonError::Acknowledgement)?;
        let envelope = CourierFrame::Acknowledgement {
            sender: self.identity.public_key(),
            acknowledgement,
        }
        .into_envelope()
        .map_err(CourierDaemonError::IncomingFrame)?;
        self.maildrop
            .store_envelope(&bundle, &envelope)
            .await
            .map_err(CourierDaemonError::Maildrop)?;
        Ok(())
    }

    fn require_verified_contact(
        &self,
        identity: &arachne_core::IdentityPublicKey,
    ) -> Result<(), CourierDaemonError> {
        match self.contacts.contact(identity) {
            Some(contact) if contact.status() == ContactStatus::Verified => Ok(()),
            _ => Err(CourierDaemonError::UnverifiedContact),
        }
    }

    fn refresh_local_bundle(&mut self, now_unix_seconds: u64) -> Result<(), CourierDaemonError> {
        let invitation = RelayInvitation::decode(
            &self
                .local_bundle
                .relay_invitation()
                .encode()
                .map_err(CourierDaemonError::LocalInvitation)?,
        )
        .map_err(|_| CourierDaemonError::LocalBundleCopy)?;
        let generation = self
            .local_bundle
            .generation()
            .checked_add(1)
            .ok_or(CourierDaemonError::BundleGenerationExhausted)?;
        let bundle = self
            .cryptographer
            .bundle_with_prekeys(
                self.identity.keypair(),
                invitation,
                *self.local_bundle.relay_tls_pin(),
                generation,
                self.prekeys.unpublished_public(),
            )
            .map_err(CourierDaemonError::Cryptographer)?;
        self.bundles
            .import(&bundle, now_unix_seconds)
            .map_err(CourierDaemonError::Bundles)?;
        self.local_bundle = bundle;
        self.directory_published = false;
        Ok(())
    }

    async fn process_attachment_jobs(
        &mut self,
        now_unix_seconds: u64,
    ) -> Result<u16, CourierDaemonError> {
        const CHUNKS_PER_CYCLE: usize = 64;
        let jobs = self.attachment_jobs.jobs().cloned().collect::<Vec<_>>();
        let mut uploaded = 0_u16;
        for job in jobs {
            if job.state() != CourierAttachmentJobState::Uploading
                || job.expiry().is_expired(now_unix_seconds)
            {
                continue;
            }
            self.require_verified_contact(job.recipient())?;
            let directory = self
                .bundles
                .bundle_for(job.recipient(), now_unix_seconds)
                .map_err(CourierDaemonError::Bundles)?
                .ok_or(CourierDaemonError::MissingRecipientBundle)?;
            let journal_store =
                AttachmentTransferJournalStore::open(&self.layout, job.attachment_identifier())
                    .map_err(CourierDaemonError::AttachmentJournal)?;
            let mut journal = journal_store
                .load()
                .map_err(CourierDaemonError::AttachmentJournal)?;
            for _ in 0..CHUNKS_PER_CYCLE {
                let Some(index) = journal.next_pending_index() else {
                    break;
                };
                let chunk = self
                    .attachment_submissions
                    .load_chunk(job.attachment_identifier(), index)
                    .map_err(CourierDaemonError::AttachmentSubmission)?;
                if self
                    .maildrop
                    .upload_attachment_chunk(&directory, &chunk)
                    .await
                    .is_err()
                {
                    break;
                }
                journal_store
                    .mark_uploaded(&mut journal, &chunk)
                    .map_err(CourierDaemonError::AttachmentJournal)?;
                uploaded = uploaded.saturating_add(1);
            }
            if !journal.is_complete() {
                continue;
            }
            let (recipient_bundle, selected_prekey) = if self.sessions.contains(job.recipient()) {
                (directory, None)
            } else {
                let fetched = self
                    .maildrop
                    .fetch_courier_bundle(&directory, self.identity.keypair(), job.recipient())
                    .await
                    .map_err(CourierDaemonError::Maildrop)?;
                self.bundles
                    .import(fetched.bundle(), now_unix_seconds)
                    .map_err(CourierDaemonError::Bundles)?;
                fetched.into_parts()
            };
            let reference = job
                .reference()
                .map_err(CourierDaemonError::AttachmentJobs)?;
            let payload = MessagePayload::new(
                MessageContentType::Binary,
                reference
                    .encode()
                    .map_err(CourierDaemonError::AttachmentReference)?,
            )
            .map_err(CourierDaemonError::AttachmentPayload)?;
            let frame = self
                .cryptographer
                .encrypt_with_identifier(
                    &self.identity.public_key(),
                    &recipient_bundle,
                    &mut self.sessions,
                    &payload,
                    job.message_identifier(),
                    selected_prekey,
                )
                .map_err(CourierDaemonError::Cryptographer)?;
            let envelope = frame
                .into_envelope()
                .map_err(CourierDaemonError::IncomingFrame)?;
            self.outbox
                .enqueue_with_identifier(
                    job.message_identifier(),
                    *job.recipient(),
                    envelope,
                    job.expiry(),
                )
                .map_err(CourierDaemonError::Outbox)?;
            self.attachment_jobs
                .mark_reference_queued(job.attachment_identifier())
                .map_err(CourierDaemonError::AttachmentJobs)?;
        }
        Ok(uploaded)
    }
}

#[derive(Debug, thiserror::Error)]
pub enum CourierDaemonError {
    #[error("courier daemon state directory is invalid")]
    StateDirectory(#[source] crate::ClientStateDirectoryError),
    #[error("courier daemon identity is unavailable")]
    Identity(#[source] ClientIdentityError),
    #[error("courier daemon profile is unavailable")]
    Profile(#[source] ClientProfileError),
    #[error("courier daemon contact state is unavailable")]
    Contacts(#[source] ContactStoreError),
    #[error("courier daemon bundle state is unavailable")]
    Bundles(#[source] CourierBundleStoreError),
    #[error("courier daemon session state is unavailable")]
    Sessions(#[source] CourierSessionStoreError),
    #[error("courier daemon one-time-prekey inventory is unavailable")]
    Prekeys(#[source] CourierOneTimePrekeyInventoryError),
    #[error("courier daemon attachment job state is unavailable")]
    AttachmentJobs(#[source] CourierAttachmentJobError),
    #[error("courier daemon attachment staging is unavailable")]
    AttachmentSubmission(#[source] AttachmentSubmissionStoreError),
    #[error("courier daemon attachment transfer journal is unavailable")]
    AttachmentJournal(#[source] AttachmentJournalStoreError),
    #[error("courier daemon attachment reference is invalid")]
    AttachmentReference(#[source] CourierAttachmentReferenceError),
    #[error("courier daemon attachment payload is invalid")]
    AttachmentPayload(#[source] MessagePayloadError),
    #[error("courier daemon outbox state is unavailable")]
    Outbox(#[source] SenderOutboxError),
    #[error("courier daemon cryptographer is unavailable")]
    Cryptographer(#[source] CourierCryptographerError),
    #[error("courier daemon has no active local relay bundle")]
    LocalBundleUnavailable,
    #[error("courier daemon local relay bundle is invalid or expired")]
    LocalBundle(#[source] arachne_protocol::CourierBundleError),
    #[error("courier daemon local relay invitation is invalid")]
    LocalInvitation(#[source] arachne_protocol::RelayInvitationError),
    #[error("courier daemon local relay invitation could not be copied")]
    LocalBundleCopy,
    #[error("courier daemon bundle generation is exhausted")]
    BundleGenerationExhausted,
    #[error("courier daemon queued frame is invalid")]
    QueuedFrame(#[source] arachne_protocol::CourierFrameError),
    #[error("courier daemon queued message metadata does not match its frame")]
    QueuedMessageMismatch,
    #[error("courier daemon incoming frame is invalid")]
    IncomingFrame(#[source] arachne_protocol::CourierFrameError),
    #[error("courier daemon incoming frame had no message payload")]
    IncomingFrameMissingPayload,
    #[error("courier daemon delivery acknowledgement is invalid")]
    Acknowledgement(#[source] arachne_protocol::DeliveryAcknowledgementError),
    #[error("courier daemon acknowledgement sender does not match its signer")]
    AcknowledgementSenderMismatch,
    #[error("courier daemon requires a verified contact")]
    UnverifiedContact,
    #[error("courier daemon has no active relay bundle for a verified recipient")]
    MissingRecipientBundle,
    #[error("courier daemon bootstrap references an unavailable one-time prekey")]
    MissingOneTimePrekey,
    #[error("courier daemon relay request failed")]
    Maildrop(#[source] CourierMaildropError),
}
