use std::{
    collections::{BTreeMap, BTreeSet},
    path::Path,
};

use arachne_core::{
    IdentityKeypair, IdentityPublicKey, KeystoreEntryName, KeystoreSecret, OneTimePrekeyId,
    OsKeystore, X25519IdentityKeypair, X25519Prekey,
};
use arachne_protocol::{
    CourierBundle, CourierBundleError, CourierFrame, MAX_ONE_TIME_PREKEYS, MessageIdentifier,
    MessagePayload, MessagePayloadError, OneTimePrekeyPublic, RatchetMessageSession,
    RelayInvitation, X3dhPrekeyBundle, X25519IdentityBinding, initiate_x3dh,
    initiate_x3dh_with_one_time_prekey, respond_x3dh,
};
use minicbor::{Decoder, Encoder};

use crate::{
    ClientProfileId, EncryptedStateStore, SignedPrekeyLifecycle, SignedPrekeyLifecycleError,
    StateDocument, StateDocumentError, StateStoreError,
};

pub const COURIER_EXCHANGE_IDENTITY_KEY_ENTRY: &str = "arachne_courier_exchange_identity_v1";
pub const COURIER_BUNDLE_STATE_SCHEMA_VERSION: u8 = 1;
pub const COURIER_SESSION_STATE_SCHEMA_VERSION: u8 = 2;
pub const MAX_COURIER_CONTACTS: usize = 256;
pub const COURIER_ONE_TIME_PREKEY_TARGET: usize = MAX_ONE_TIME_PREKEYS;
pub const COURIER_ONE_TIME_PREKEY_REPLENISH_THRESHOLD: usize = 16;
const BUNDLE_STATE_FIELDS: u64 = 2;
const SESSION_STATE_FIELDS: u64 = 3;
const SESSION_FIELDS: u64 = 2;
const IDENTITY_BYTES: usize = 32;
const INBOX_MESSAGE_FIELDS: u64 = 4;
const PREKEY_INVENTORY_FIELDS: u64 = 3;
const PREKEY_INVENTORY_ENTRY_FIELDS: u64 = 3;
const PREKEY_INVENTORY_SCHEMA_VERSION: u8 = 2;

pub struct CourierOneTimePrekeyInventory {
    state: EncryptedStateStore,
    next_identifier: u64,
    available: BTreeMap<OneTimePrekeyId, X25519Prekey>,
    unpublished: BTreeSet<OneTimePrekeyId>,
}

impl CourierOneTimePrekeyInventory {
    pub fn open<K: OsKeystore>(
        path: &Path,
        keystore: &mut K,
    ) -> Result<Self, CourierOneTimePrekeyInventoryError> {
        let state = EncryptedStateStore::open(path, keystore)?;
        let (next_identifier, available, unpublished) = match state.load()? {
            Some(document) => decode_prekey_inventory(document.as_bytes())?,
            None => (1, BTreeMap::new(), BTreeSet::new()),
        };
        Ok(Self {
            state,
            next_identifier,
            available,
            unpublished,
        })
    }

    pub fn replenish(
        &mut self,
        target: usize,
    ) -> Result<Vec<OneTimePrekeyId>, CourierOneTimePrekeyInventoryError> {
        if target > MAX_ONE_TIME_PREKEYS {
            return Err(CourierOneTimePrekeyInventoryError::TargetTooLarge);
        }
        let prior_next_identifier = self.next_identifier;
        let count = target.saturating_sub(self.available.len());
        let mut added = Vec::with_capacity(count);
        for _ in 0..count {
            let identifier = OneTimePrekeyId::new(self.next_identifier)
                .map_err(|_| CourierOneTimePrekeyInventoryError::InvalidIdentifier)?;
            self.next_identifier = self
                .next_identifier
                .checked_add(1)
                .ok_or(CourierOneTimePrekeyInventoryError::IdentifierExhausted)?;
            let prekey = X25519Prekey::generate()
                .map_err(|_| CourierOneTimePrekeyInventoryError::PrekeyGeneration)?;
            self.available.insert(identifier, prekey);
            self.unpublished.insert(identifier);
            added.push(identifier);
        }
        if let Err(error) = self.persist() {
            for identifier in &added {
                self.available.remove(identifier);
                self.unpublished.remove(identifier);
            }
            self.next_identifier = prior_next_identifier;
            return Err(error);
        }
        Ok(added)
    }

    #[must_use]
    pub fn available_count(&self) -> usize {
        self.available.len()
    }

    #[must_use]
    pub fn unpublished_public(&self) -> Vec<OneTimePrekeyPublic> {
        self.available
            .iter()
            .filter(|(identifier, _)| self.unpublished.contains(identifier))
            .map(|(identifier, prekey)| OneTimePrekeyPublic::new(*identifier, prekey))
            .collect()
    }

    /// Marks the keys in the current signed bundle as advertised only after
    /// the relay has accepted that bundle. Advertised keys remain private
    /// locally for delayed bootstrap messages but are never offered again by
    /// a newer bundle.
    pub fn mark_unpublished_as_advertised(
        &mut self,
    ) -> Result<(), CourierOneTimePrekeyInventoryError> {
        if self.unpublished.is_empty() {
            return Ok(());
        }
        let previous = std::mem::take(&mut self.unpublished);
        if let Err(error) = self.persist() {
            self.unpublished = previous;
            return Err(error);
        }
        Ok(())
    }

    #[must_use]
    pub fn load(&self, identifier: OneTimePrekeyId) -> Option<&X25519Prekey> {
        self.available.get(&identifier)
    }

    pub fn take(
        &mut self,
        identifier: OneTimePrekeyId,
    ) -> Result<X25519Prekey, CourierOneTimePrekeyInventoryError> {
        let prekey = self
            .available
            .remove(&identifier)
            .ok_or(CourierOneTimePrekeyInventoryError::UnavailableIdentifier)?;
        let unpublished = self.unpublished.remove(&identifier);
        if let Err(error) = self.persist() {
            self.available.insert(identifier, prekey);
            if unpublished {
                self.unpublished.insert(identifier);
            }
            return Err(error);
        }
        Ok(prekey)
    }

    fn persist(&mut self) -> Result<(), CourierOneTimePrekeyInventoryError> {
        let document = StateDocument::new(encode_prekey_inventory(
            self.next_identifier,
            &self.available,
            &self.unpublished,
        )?)
        .map_err(CourierOneTimePrekeyInventoryError::Document)?;
        self.state.replace(&document)?;
        Ok(())
    }
}

#[derive(Debug, thiserror::Error)]
pub enum CourierOneTimePrekeyInventoryError {
    #[error("courier one-time-prekey encrypted state operation failed")]
    State(#[from] StateStoreError),
    #[error("courier one-time-prekey state document is invalid")]
    Document(#[source] StateDocumentError),
    #[error("courier one-time-prekey inventory encoding failed")]
    Encode,
    #[error("courier one-time-prekey inventory decoding failed")]
    Decode,
    #[error("courier one-time-prekey inventory shape is invalid")]
    InvalidShape,
    #[error("courier one-time-prekey inventory schema is unsupported")]
    UnsupportedSchemaVersion,
    #[error("courier one-time-prekey inventory has an invalid identifier")]
    InvalidIdentifier,
    #[error("courier one-time-prekey inventory has duplicate identifiers")]
    DuplicateIdentifier,
    #[error("courier one-time-prekey inventory has too many entries")]
    TooManyPrekeys,
    #[error("courier one-time-prekey inventory is noncanonical")]
    NonCanonicalEncoding,
    #[error("courier one-time-prekey target exceeds the protocol maximum")]
    TargetTooLarge,
    #[error("courier one-time-prekey identifier space is exhausted")]
    IdentifierExhausted,
    #[error("courier one-time-prekey is unavailable")]
    UnavailableIdentifier,
    #[error("courier one-time-prekey generation failed")]
    PrekeyGeneration,
    #[error("courier one-time-prekey private material is invalid")]
    InvalidPrivateMaterial,
}

pub struct CourierBundleStore {
    state: EncryptedStateStore,
    bundles: BTreeMap<[u8; IDENTITY_BYTES], Vec<u8>>,
}

impl CourierBundleStore {
    pub fn open<K: OsKeystore>(
        path: &Path,
        keystore: &mut K,
    ) -> Result<Self, CourierBundleStoreError> {
        let state = EncryptedStateStore::open(path, keystore)?;
        let bundles = match state.load()? {
            Some(document) => decode_bundle_state(document.as_bytes())?,
            None => BTreeMap::new(),
        };
        Ok(Self { state, bundles })
    }

    pub fn import(
        &mut self,
        bundle: &CourierBundle,
        now_unix_seconds: u64,
    ) -> Result<(), CourierBundleStoreError> {
        bundle
            .validate(now_unix_seconds)
            .map_err(CourierBundleStoreError::Bundle)?;
        let encoded = bundle.encode().map_err(CourierBundleStoreError::Bundle)?;
        let key = *bundle.publisher().as_bytes();
        let previous = self.bundles.insert(key, encoded);
        if let Err(error) = self.persist() {
            match previous {
                Some(previous) => {
                    self.bundles.insert(key, previous);
                }
                None => {
                    self.bundles.remove(&key);
                }
            }
            return Err(error);
        }
        Ok(())
    }

    pub fn bundle_for(
        &self,
        identity: &IdentityPublicKey,
        now_unix_seconds: u64,
    ) -> Result<Option<CourierBundle>, CourierBundleStoreError> {
        self.bundles
            .get(identity.as_bytes())
            .map(|encoded| {
                let bundle =
                    CourierBundle::decode(encoded).map_err(CourierBundleStoreError::Bundle)?;
                bundle
                    .validate(now_unix_seconds)
                    .map_err(CourierBundleStoreError::Bundle)?;
                Ok(bundle)
            })
            .transpose()
    }

    fn persist(&mut self) -> Result<(), CourierBundleStoreError> {
        let document = StateDocument::new(encode_bundle_state(&self.bundles)?)
            .map_err(CourierBundleStoreError::Document)?;
        self.state.replace(&document)?;
        Ok(())
    }
}

pub struct CourierSessionStore {
    state: EncryptedStateStore,
    sessions: BTreeMap<[u8; IDENTITY_BYTES], Vec<u8>>,
    inbox: BTreeMap<[u8; 16], crate::CourierInboxMessage>,
}

impl CourierSessionStore {
    pub fn open<K: OsKeystore>(
        path: &Path,
        keystore: &mut K,
    ) -> Result<Self, CourierSessionStoreError> {
        let state = EncryptedStateStore::open(path, keystore)?;
        let (sessions, inbox) = match state.load()? {
            Some(document) => decode_session_state(document.as_bytes())?,
            None => (BTreeMap::new(), BTreeMap::new()),
        };
        Ok(Self {
            state,
            sessions,
            inbox,
        })
    }

    pub fn load(
        &self,
        identity: &IdentityPublicKey,
    ) -> Result<Option<RatchetMessageSession>, CourierSessionStoreError> {
        self.sessions
            .get(identity.as_bytes())
            .map(|encoded| {
                RatchetMessageSession::decode(encoded).map_err(CourierSessionStoreError::Session)
            })
            .transpose()
    }

    pub fn replace(
        &mut self,
        identity: &IdentityPublicKey,
        session: &RatchetMessageSession,
    ) -> Result<(), CourierSessionStoreError> {
        let encoded = session
            .encode()
            .map_err(CourierSessionStoreError::Session)?;
        let key = *identity.as_bytes();
        let previous = self.sessions.insert(key, encoded.to_vec());
        if let Err(error) = self.persist() {
            match previous {
                Some(previous) => {
                    self.sessions.insert(key, previous);
                }
                None => {
                    self.sessions.remove(&key);
                }
            }
            return Err(error);
        }
        Ok(())
    }

    #[must_use]
    pub fn inbox_messages(&self) -> impl Iterator<Item = &crate::CourierInboxMessage> {
        self.inbox.values()
    }

    #[must_use]
    pub fn inbox_message(
        &self,
        identifier: MessageIdentifier,
    ) -> Option<&crate::CourierInboxMessage> {
        self.inbox.get(identifier.as_bytes())
    }

    #[must_use]
    pub fn inbox_contains(&self, identifier: MessageIdentifier) -> bool {
        self.inbox.contains_key(identifier.as_bytes())
    }

    /// Atomically advances the ratchet and commits its accepted plaintext.
    pub fn commit_received(
        &mut self,
        identity: &IdentityPublicKey,
        session: &RatchetMessageSession,
        message: DecryptedCourierMessage,
        received_at: u64,
    ) -> Result<bool, CourierSessionStoreError> {
        let identifier = message.message_identifier();
        if self.inbox_contains(identifier) {
            return Ok(false);
        }
        if self.inbox.len() >= crate::MAX_COURIER_INBOX_MESSAGES {
            return Err(CourierSessionStoreError::InboxCapacityExceeded);
        }
        let encoded_session = session
            .encode()
            .map_err(CourierSessionStoreError::Session)?;
        let key = *identity.as_bytes();
        let prior_session = self.sessions.insert(key, encoded_session.to_vec());
        self.inbox.insert(
            *identifier.as_bytes(),
            crate::CourierInboxMessage::new(
                *message.sender(),
                identifier,
                received_at,
                message.payload().clone(),
            ),
        );
        if let Err(error) = self.persist() {
            self.inbox.remove(identifier.as_bytes());
            match prior_session {
                Some(previous) => {
                    self.sessions.insert(key, previous);
                }
                None => {
                    self.sessions.remove(&key);
                }
            }
            return Err(error);
        }
        Ok(true)
    }

    #[must_use]
    pub fn contains(&self, identity: &IdentityPublicKey) -> bool {
        self.sessions.contains_key(identity.as_bytes())
    }

    fn persist(&mut self) -> Result<(), CourierSessionStoreError> {
        let document = StateDocument::new(encode_session_state(&self.sessions, &self.inbox)?)
            .map_err(CourierSessionStoreError::Document)?;
        self.state.replace(&document)?;
        Ok(())
    }
}

pub struct CourierCryptographer {
    exchange_identity: X25519IdentityKeypair,
    binding: X25519IdentityBinding,
    signed_prekey: SignedPrekeyLifecycle,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct DecryptedCourierMessage {
    sender: IdentityPublicKey,
    message_identifier: MessageIdentifier,
    payload: MessagePayload,
}

impl DecryptedCourierMessage {
    #[must_use]
    pub const fn sender(&self) -> &IdentityPublicKey {
        &self.sender
    }

    #[must_use]
    pub const fn message_identifier(&self) -> MessageIdentifier {
        self.message_identifier
    }

    #[must_use]
    pub const fn payload(&self) -> &MessagePayload {
        &self.payload
    }
}

impl CourierCryptographer {
    pub fn load_or_create<K: OsKeystore>(
        identity: &IdentityKeypair,
        keystore: &mut K,
    ) -> Result<Self, CourierCryptographerError> {
        let exchange_identity = load_or_create_exchange_identity(keystore, None)?;
        let binding = X25519IdentityBinding::create(identity, &exchange_identity)
            .map_err(CourierCryptographerError::X3dh)?;
        let (signed_prekey, _) = SignedPrekeyLifecycle::create_or_load(identity, keystore)
            .map_err(CourierCryptographerError::SignedPrekey)?;
        Ok(Self {
            exchange_identity,
            binding,
            signed_prekey,
        })
    }

    pub fn load_or_create_for_profile<K: OsKeystore>(
        identity: &IdentityKeypair,
        profile: ClientProfileId,
        keystore: &mut K,
    ) -> Result<Self, CourierCryptographerError> {
        let exchange_identity = load_or_create_exchange_identity(keystore, Some(profile))?;
        let binding = X25519IdentityBinding::create(identity, &exchange_identity)
            .map_err(CourierCryptographerError::X3dh)?;
        let signed_entry = profile
            .keystore_entry("courier_signed_prekey")
            .map_err(|_| CourierCryptographerError::InvalidExchangeIdentityEntry)?;
        let (signed_prekey, _) =
            SignedPrekeyLifecycle::create_or_load_with_entry(identity, keystore, signed_entry)
                .map_err(CourierCryptographerError::SignedPrekey)?;
        Ok(Self {
            exchange_identity,
            binding,
            signed_prekey,
        })
    }

    pub fn bundle(
        &self,
        identity: &IdentityKeypair,
        relay_invitation: RelayInvitation,
        relay_tls_pin: [u8; 32],
    ) -> Result<CourierBundle, CourierCryptographerError> {
        self.bundle_with_generation(identity, relay_invitation, relay_tls_pin, 1)
    }

    pub fn bundle_with_generation(
        &self,
        identity: &IdentityKeypair,
        relay_invitation: RelayInvitation,
        relay_tls_pin: [u8; 32],
        generation: u64,
    ) -> Result<CourierBundle, CourierCryptographerError> {
        self.bundle_with_prekeys(
            identity,
            relay_invitation,
            relay_tls_pin,
            generation,
            Vec::new(),
        )
    }

    pub fn bundle_with_prekeys(
        &self,
        identity: &IdentityKeypair,
        relay_invitation: RelayInvitation,
        relay_tls_pin: [u8; 32],
        generation: u64,
        one_time_prekeys: Vec<OneTimePrekeyPublic>,
    ) -> Result<CourierBundle, CourierCryptographerError> {
        let prekey_bundle = X3dhPrekeyBundle::create(
            identity,
            &self.exchange_identity,
            self.signed_prekey.public(),
            one_time_prekeys,
        )
        .map_err(CourierCryptographerError::X3dh)?;
        CourierBundle::create(
            identity,
            prekey_bundle,
            relay_invitation,
            relay_tls_pin,
            generation,
        )
        .map_err(CourierCryptographerError::Bundle)
    }

    pub fn encrypt(
        &self,
        sender: &IdentityPublicKey,
        recipient_bundle: &CourierBundle,
        sessions: &mut CourierSessionStore,
        payload: &MessagePayload,
    ) -> Result<CourierFrame, CourierCryptographerError> {
        let identifier =
            MessageIdentifier::generate().map_err(|_| CourierCryptographerError::Identifier)?;
        self.encrypt_with_identifier_and_one_time_prekey(
            sender,
            recipient_bundle,
            sessions,
            payload,
            identifier,
            None,
        )
    }

    pub fn encrypt_with_one_time_prekey(
        &self,
        sender: &IdentityPublicKey,
        recipient_bundle: &CourierBundle,
        sessions: &mut CourierSessionStore,
        payload: &MessagePayload,
        selected_one_time_prekey: Option<arachne_core::OneTimePrekeyId>,
    ) -> Result<CourierFrame, CourierCryptographerError> {
        let identifier =
            MessageIdentifier::generate().map_err(|_| CourierCryptographerError::Identifier)?;
        self.encrypt_with_identifier_and_one_time_prekey(
            sender,
            recipient_bundle,
            sessions,
            payload,
            identifier,
            selected_one_time_prekey,
        )
    }

    pub fn encrypt_with_identifier(
        &self,
        sender: &IdentityPublicKey,
        recipient_bundle: &CourierBundle,
        sessions: &mut CourierSessionStore,
        payload: &MessagePayload,
        message_identifier: MessageIdentifier,
        selected_one_time_prekey: Option<arachne_core::OneTimePrekeyId>,
    ) -> Result<CourierFrame, CourierCryptographerError> {
        self.encrypt_with_identifier_and_one_time_prekey(
            sender,
            recipient_bundle,
            sessions,
            payload,
            message_identifier,
            selected_one_time_prekey,
        )
    }

    fn encrypt_with_identifier_and_one_time_prekey(
        &self,
        sender: &IdentityPublicKey,
        recipient_bundle: &CourierBundle,
        sessions: &mut CourierSessionStore,
        payload: &MessagePayload,
        message_identifier: MessageIdentifier,
        selected_one_time_prekey: Option<arachne_core::OneTimePrekeyId>,
    ) -> Result<CourierFrame, CourierCryptographerError> {
        if recipient_bundle.publisher() == sender {
            return Err(CourierCryptographerError::SelfRecipient);
        }
        let recipient = recipient_bundle.publisher();
        match sessions.load(recipient)? {
            Some(mut session) => {
                let message = session
                    .encrypt(payload)
                    .map_err(CourierCryptographerError::Ratchet)?;
                sessions.replace(recipient, &session)?;
                return Ok(CourierFrame::Ratchet {
                    sender: *sender,
                    message_identifier,
                    message,
                });
            }
            None => {
                let (initial, x3dh) = match selected_one_time_prekey {
                    Some(identifier) => initiate_x3dh_with_one_time_prekey(
                        &self.exchange_identity,
                        self.binding,
                        recipient_bundle.prekey_bundle(),
                        Some(identifier),
                    ),
                    None => initiate_x3dh(
                        &self.exchange_identity,
                        self.binding,
                        recipient_bundle.prekey_bundle(),
                    ),
                }
                .map_err(CourierCryptographerError::X3dh)?;
                let mut session = RatchetMessageSession::initiate(
                    &x3dh,
                    *recipient_bundle.prekey_bundle().signed_prekey().prekey(),
                )
                .map_err(CourierCryptographerError::Ratchet)?;
                let message = session
                    .encrypt(payload)
                    .map_err(CourierCryptographerError::Ratchet)?;
                sessions.replace(recipient, &session)?;
                return Ok(CourierFrame::Bootstrap {
                    sender: *sender,
                    message_identifier,
                    initial,
                    message,
                });
            }
        }
    }

    pub fn decrypt(
        &self,
        frame: &CourierFrame,
        sessions: &mut CourierSessionStore,
    ) -> Result<Option<DecryptedCourierMessage>, CourierCryptographerError> {
        match frame {
            CourierFrame::Bootstrap {
                sender,
                message_identifier,
                initial,
                message,
            } => {
                if sessions.contains(sender) {
                    return Err(CourierCryptographerError::SessionAlreadyInitialized);
                }
                if initial.initiator_binding().signing_identity() != sender {
                    return Err(CourierCryptographerError::SenderMismatch);
                }
                let x3dh = respond_x3dh(
                    &self.exchange_identity,
                    &self.binding,
                    self.signed_prekey.signed_prekey(),
                    None,
                    initial,
                )
                .map_err(CourierCryptographerError::X3dh)?;
                let mut session = RatchetMessageSession::respond(
                    &x3dh,
                    self.signed_prekey.signed_prekey().prekey(),
                    message.header().ratchet_public(),
                )
                .map_err(CourierCryptographerError::Ratchet)?;
                let payload = session
                    .decrypt(message)
                    .map_err(CourierCryptographerError::Ratchet)?;
                sessions.replace(sender, &session)?;
                Ok(Some(DecryptedCourierMessage {
                    sender: *sender,
                    message_identifier: *message_identifier,
                    payload,
                }))
            }
            CourierFrame::Ratchet {
                sender,
                message_identifier,
                message,
            } => {
                let mut session = sessions
                    .load(sender)?
                    .ok_or(CourierCryptographerError::SessionUnavailable)?;
                let payload = session
                    .decrypt(message)
                    .map_err(CourierCryptographerError::Ratchet)?;
                sessions.replace(sender, &session)?;
                Ok(Some(DecryptedCourierMessage {
                    sender: *sender,
                    message_identifier: *message_identifier,
                    payload,
                }))
            }
            CourierFrame::Acknowledgement { .. } => Ok(None),
        }
    }

    /// Decrypt without persisting state so the caller can atomically commit the
    /// resulting ratchet session with the accepted inbox record.
    pub fn decrypt_for_receive(
        &self,
        frame: &CourierFrame,
        sessions: &CourierSessionStore,
        one_time_prekey: Option<&X25519Prekey>,
    ) -> Result<Option<(DecryptedCourierMessage, RatchetMessageSession)>, CourierCryptographerError>
    {
        match frame {
            CourierFrame::Bootstrap {
                sender,
                message_identifier,
                initial,
                message,
            } => {
                if sessions.contains(sender) {
                    return Err(CourierCryptographerError::SessionAlreadyInitialized);
                }
                if initial.initiator_binding().signing_identity() != sender {
                    return Err(CourierCryptographerError::SenderMismatch);
                }
                let x3dh = respond_x3dh(
                    &self.exchange_identity,
                    &self.binding,
                    self.signed_prekey.signed_prekey(),
                    initial.one_time_prekey().zip(one_time_prekey),
                    initial,
                )
                .map_err(CourierCryptographerError::X3dh)?;
                let mut session = RatchetMessageSession::respond(
                    &x3dh,
                    self.signed_prekey.signed_prekey().prekey(),
                    message.header().ratchet_public(),
                )
                .map_err(CourierCryptographerError::Ratchet)?;
                let payload = session
                    .decrypt(message)
                    .map_err(CourierCryptographerError::Ratchet)?;
                Ok(Some((
                    DecryptedCourierMessage {
                        sender: *sender,
                        message_identifier: *message_identifier,
                        payload,
                    },
                    session,
                )))
            }
            CourierFrame::Ratchet {
                sender,
                message_identifier,
                message,
            } => {
                if one_time_prekey.is_some() {
                    return Err(CourierCryptographerError::UnexpectedOneTimePrekey);
                }
                let mut session = sessions
                    .load(sender)?
                    .ok_or(CourierCryptographerError::SessionUnavailable)?;
                let payload = session
                    .decrypt(message)
                    .map_err(CourierCryptographerError::Ratchet)?;
                Ok(Some((
                    DecryptedCourierMessage {
                        sender: *sender,
                        message_identifier: *message_identifier,
                        payload,
                    },
                    session,
                )))
            }
            CourierFrame::Acknowledgement { .. } => Ok(None),
        }
    }
}

#[derive(Debug, thiserror::Error)]
pub enum CourierBundleStoreError {
    #[error("courier bundle encrypted state operation failed")]
    State(#[from] StateStoreError),
    #[error("courier bundle state document is invalid")]
    Document(#[source] StateDocumentError),
    #[error("courier bundle is invalid")]
    Bundle(#[source] CourierBundleError),
    #[error("courier bundle state encoding failed")]
    Encode,
    #[error("courier bundle state decoding failed")]
    Decode,
    #[error("courier bundle state has an invalid shape")]
    InvalidShape,
    #[error("courier bundle state schema is unsupported")]
    UnsupportedSchemaVersion,
    #[error("courier bundle state has too many contacts")]
    TooManyContacts,
    #[error("courier bundle state has a duplicate contact")]
    DuplicateContact,
    #[error("courier bundle state has trailing bytes")]
    TrailingBytes,
    #[error("courier bundle state is not canonical")]
    NonCanonicalEncoding,
}

#[derive(Debug, thiserror::Error)]
pub enum CourierSessionStoreError {
    #[error("courier session encrypted state operation failed")]
    State(#[from] StateStoreError),
    #[error("courier session state document is invalid")]
    Document(#[source] StateDocumentError),
    #[error("courier ratchet session is invalid")]
    Session(#[source] arachne_protocol::RatchetMessageError),
    #[error("courier inbox payload is invalid")]
    Payload(#[source] MessagePayloadError),
    #[error("courier session state encoding failed")]
    Encode,
    #[error("courier session state decoding failed")]
    Decode,
    #[error("courier session state has an invalid shape")]
    InvalidShape,
    #[error("courier session state schema is unsupported")]
    UnsupportedSchemaVersion,
    #[error("courier session state has too many contacts")]
    TooManyContacts,
    #[error("courier session state has a duplicate contact")]
    DuplicateContact,
    #[error("courier session state has too many inbox messages")]
    TooManyInboxMessages,
    #[error("courier session state has a duplicate inbox message identifier")]
    DuplicateInboxIdentifier,
    #[error("courier session state has an invalid inbox sender")]
    InvalidInboxSender,
    #[error("courier session state has an invalid inbox message identifier")]
    InvalidInboxIdentifier,
    #[error("courier inbox capacity is exhausted")]
    InboxCapacityExceeded,
    #[error("courier session state has trailing bytes")]
    TrailingBytes,
    #[error("courier session state is not canonical")]
    NonCanonicalEncoding,
}

#[derive(Debug, thiserror::Error)]
pub enum CourierCryptographerError {
    #[error("courier exchange identity entry is invalid")]
    InvalidExchangeIdentityEntry,
    #[error("courier exchange identity keystore operation failed")]
    Keystore,
    #[error("courier exchange identity stored material is invalid")]
    ExchangeIdentity,
    #[error("courier exchange identity secret cannot be stored")]
    KeystoreSecret,
    #[error("courier X3DH operation failed")]
    X3dh(#[source] arachne_protocol::X3dhError),
    #[error("courier signed prekey operation failed")]
    SignedPrekey(#[source] SignedPrekeyLifecycleError),
    #[error("courier bundle operation failed")]
    Bundle(#[source] CourierBundleError),
    #[error("courier ratchet operation failed")]
    Ratchet(#[source] arachne_protocol::RatchetMessageError),
    #[error("courier session state operation failed")]
    Sessions(#[from] CourierSessionStoreError),
    #[error("courier message identifier could not be generated")]
    Identifier,
    #[error("courier cannot send to its own identity")]
    SelfRecipient,
    #[error("courier bootstrap sender identity does not match its X3DH binding")]
    SenderMismatch,
    #[error("courier session is already initialized")]
    SessionAlreadyInitialized,
    #[error("courier session is unavailable")]
    SessionUnavailable,
    #[error("courier one-time prekey was supplied for a non-bootstrap frame")]
    UnexpectedOneTimePrekey,
}

fn load_or_create_exchange_identity<K: OsKeystore>(
    keystore: &mut K,
    profile: Option<ClientProfileId>,
) -> Result<X25519IdentityKeypair, CourierCryptographerError> {
    let entry = match profile {
        Some(profile) => profile
            .keystore_entry("courier_exchange_identity")
            .map_err(|_| CourierCryptographerError::InvalidExchangeIdentityEntry)?,
        None => KeystoreEntryName::new(COURIER_EXCHANGE_IDENTITY_KEY_ENTRY.to_owned())
            .map_err(|_| CourierCryptographerError::InvalidExchangeIdentityEntry)?,
    };
    if let Some(secret) = keystore
        .load(&entry)
        .map_err(|_| CourierCryptographerError::Keystore)?
    {
        return X25519IdentityKeypair::deserialize(secret.as_bytes())
            .map_err(|_| CourierCryptographerError::ExchangeIdentity);
    }
    let exchange_identity = X25519IdentityKeypair::generate()
        .map_err(|_| CourierCryptographerError::ExchangeIdentity)?;
    let secret = KeystoreSecret::new(exchange_identity.serialize().to_vec())
        .map_err(|_| CourierCryptographerError::KeystoreSecret)?;
    keystore
        .store(&entry, &secret)
        .map_err(|_| CourierCryptographerError::Keystore)?;
    Ok(exchange_identity)
}

fn encode_prekey_inventory(
    next_identifier: u64,
    available: &BTreeMap<OneTimePrekeyId, X25519Prekey>,
    unpublished: &BTreeSet<OneTimePrekeyId>,
) -> Result<Vec<u8>, CourierOneTimePrekeyInventoryError> {
    if next_identifier == 0 {
        return Err(CourierOneTimePrekeyInventoryError::InvalidIdentifier);
    }
    if available.len() > MAX_ONE_TIME_PREKEYS {
        return Err(CourierOneTimePrekeyInventoryError::TooManyPrekeys);
    }
    let available_identifiers = available.keys().copied().collect::<BTreeSet<_>>();
    if !unpublished.is_subset(&available_identifiers) {
        return Err(CourierOneTimePrekeyInventoryError::InvalidIdentifier);
    }
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(PREKEY_INVENTORY_FIELDS)
        .map_err(|_| CourierOneTimePrekeyInventoryError::Encode)?
        .u8(PREKEY_INVENTORY_SCHEMA_VERSION)
        .map_err(|_| CourierOneTimePrekeyInventoryError::Encode)?
        .u64(next_identifier)
        .map_err(|_| CourierOneTimePrekeyInventoryError::Encode)?
        .array(
            u64::try_from(available.len())
                .map_err(|_| CourierOneTimePrekeyInventoryError::Encode)?,
        )
        .map_err(|_| CourierOneTimePrekeyInventoryError::Encode)?;
    for (identifier, prekey) in available {
        encoder
            .array(PREKEY_INVENTORY_ENTRY_FIELDS)
            .map_err(|_| CourierOneTimePrekeyInventoryError::Encode)?
            .u64(identifier.get())
            .map_err(|_| CourierOneTimePrekeyInventoryError::Encode)?
            .bytes(prekey.serialize().as_ref())
            .map_err(|_| CourierOneTimePrekeyInventoryError::Encode)?
            .bool(!unpublished.contains(identifier))
            .map_err(|_| CourierOneTimePrekeyInventoryError::Encode)?;
    }
    Ok(encoder.into_writer())
}

fn decode_prekey_inventory(
    encoded: &[u8],
) -> Result<
    (
        u64,
        BTreeMap<OneTimePrekeyId, X25519Prekey>,
        BTreeSet<OneTimePrekeyId>,
    ),
    CourierOneTimePrekeyInventoryError,
> {
    let mut decoder = Decoder::new(encoded);
    if decoder
        .array()
        .map_err(|_| CourierOneTimePrekeyInventoryError::Decode)?
        != Some(PREKEY_INVENTORY_FIELDS)
    {
        return Err(CourierOneTimePrekeyInventoryError::InvalidShape);
    }
    if decoder
        .u8()
        .map_err(|_| CourierOneTimePrekeyInventoryError::Decode)?
        != PREKEY_INVENTORY_SCHEMA_VERSION
    {
        return Err(CourierOneTimePrekeyInventoryError::UnsupportedSchemaVersion);
    }
    let next_identifier = decoder
        .u64()
        .map_err(|_| CourierOneTimePrekeyInventoryError::Decode)?;
    if next_identifier == 0 {
        return Err(CourierOneTimePrekeyInventoryError::InvalidIdentifier);
    }
    let count = decoder
        .array()
        .map_err(|_| CourierOneTimePrekeyInventoryError::Decode)?
        .ok_or(CourierOneTimePrekeyInventoryError::InvalidShape)?;
    let count =
        usize::try_from(count).map_err(|_| CourierOneTimePrekeyInventoryError::TooManyPrekeys)?;
    if count > MAX_ONE_TIME_PREKEYS {
        return Err(CourierOneTimePrekeyInventoryError::TooManyPrekeys);
    }
    let mut available = BTreeMap::new();
    let mut unpublished = BTreeSet::new();
    for _ in 0..count {
        if decoder
            .array()
            .map_err(|_| CourierOneTimePrekeyInventoryError::Decode)?
            != Some(PREKEY_INVENTORY_ENTRY_FIELDS)
        {
            return Err(CourierOneTimePrekeyInventoryError::InvalidShape);
        }
        let identifier = OneTimePrekeyId::new(
            decoder
                .u64()
                .map_err(|_| CourierOneTimePrekeyInventoryError::Decode)?,
        )
        .map_err(|_| CourierOneTimePrekeyInventoryError::InvalidIdentifier)?;
        if identifier.get() >= next_identifier {
            return Err(CourierOneTimePrekeyInventoryError::InvalidIdentifier);
        }
        let serialized = decoder
            .bytes()
            .map_err(|_| CourierOneTimePrekeyInventoryError::Decode)?;
        let prekey = X25519Prekey::deserialize(serialized)
            .map_err(|_| CourierOneTimePrekeyInventoryError::InvalidPrivateMaterial)?;
        if available.insert(identifier, prekey).is_some() {
            return Err(CourierOneTimePrekeyInventoryError::DuplicateIdentifier);
        }
        let advertised = decoder
            .bool()
            .map_err(|_| CourierOneTimePrekeyInventoryError::Decode)?;
        if !advertised {
            unpublished.insert(identifier);
        }
    }
    if decoder.position() != encoded.len()
        || encode_prekey_inventory(next_identifier, &available, &unpublished)? != encoded
    {
        return Err(CourierOneTimePrekeyInventoryError::NonCanonicalEncoding);
    }
    Ok((next_identifier, available, unpublished))
}

fn encode_bundle_state(
    bundles: &BTreeMap<[u8; IDENTITY_BYTES], Vec<u8>>,
) -> Result<Vec<u8>, CourierBundleStoreError> {
    if bundles.len() > MAX_COURIER_CONTACTS {
        return Err(CourierBundleStoreError::TooManyContacts);
    }
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(BUNDLE_STATE_FIELDS)
        .map_err(|_| CourierBundleStoreError::Encode)?
        .u8(COURIER_BUNDLE_STATE_SCHEMA_VERSION)
        .map_err(|_| CourierBundleStoreError::Encode)?
        .array(u64::try_from(bundles.len()).map_err(|_| CourierBundleStoreError::Encode)?)
        .map_err(|_| CourierBundleStoreError::Encode)?;
    for (identity, bundle) in bundles {
        encoder
            .array(SESSION_FIELDS)
            .map_err(|_| CourierBundleStoreError::Encode)?
            .bytes(identity)
            .map_err(|_| CourierBundleStoreError::Encode)?
            .bytes(bundle)
            .map_err(|_| CourierBundleStoreError::Encode)?;
    }
    Ok(encoder.into_writer())
}

fn decode_bundle_state(
    encoded: &[u8],
) -> Result<BTreeMap<[u8; IDENTITY_BYTES], Vec<u8>>, CourierBundleStoreError> {
    let mut decoder = Decoder::new(encoded);
    if decoder
        .array()
        .map_err(|_| CourierBundleStoreError::Decode)?
        != Some(BUNDLE_STATE_FIELDS)
    {
        return Err(CourierBundleStoreError::InvalidShape);
    }
    if decoder.u8().map_err(|_| CourierBundleStoreError::Decode)?
        != COURIER_BUNDLE_STATE_SCHEMA_VERSION
    {
        return Err(CourierBundleStoreError::UnsupportedSchemaVersion);
    }
    let count = decoder
        .array()
        .map_err(|_| CourierBundleStoreError::Decode)?
        .ok_or(CourierBundleStoreError::InvalidShape)?;
    let count = usize::try_from(count).map_err(|_| CourierBundleStoreError::TooManyContacts)?;
    if count > MAX_COURIER_CONTACTS {
        return Err(CourierBundleStoreError::TooManyContacts);
    }
    let mut bundles = BTreeMap::new();
    for _ in 0..count {
        if decoder
            .array()
            .map_err(|_| CourierBundleStoreError::Decode)?
            != Some(SESSION_FIELDS)
        {
            return Err(CourierBundleStoreError::InvalidShape);
        }
        let identity: [u8; IDENTITY_BYTES] = decoder
            .bytes()
            .map_err(|_| CourierBundleStoreError::Decode)?
            .try_into()
            .map_err(|_| CourierBundleStoreError::InvalidShape)?;
        let bundle = decoder
            .bytes()
            .map_err(|_| CourierBundleStoreError::Decode)?
            .to_vec();
        let parsed = CourierBundle::decode(&bundle).map_err(CourierBundleStoreError::Bundle)?;
        if parsed.publisher().as_bytes() != &identity || bundles.insert(identity, bundle).is_some()
        {
            return Err(CourierBundleStoreError::DuplicateContact);
        }
    }
    if decoder.position() != encoded.len() {
        return Err(CourierBundleStoreError::TrailingBytes);
    }
    if encode_bundle_state(&bundles)? != encoded {
        return Err(CourierBundleStoreError::NonCanonicalEncoding);
    }
    Ok(bundles)
}

fn encode_session_state(
    sessions: &BTreeMap<[u8; IDENTITY_BYTES], Vec<u8>>,
    inbox: &BTreeMap<[u8; 16], crate::CourierInboxMessage>,
) -> Result<Vec<u8>, CourierSessionStoreError> {
    if sessions.len() > MAX_COURIER_CONTACTS {
        return Err(CourierSessionStoreError::TooManyContacts);
    }
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(SESSION_STATE_FIELDS)
        .map_err(|_| CourierSessionStoreError::Encode)?
        .u8(COURIER_SESSION_STATE_SCHEMA_VERSION)
        .map_err(|_| CourierSessionStoreError::Encode)?
        .array(u64::try_from(sessions.len()).map_err(|_| CourierSessionStoreError::Encode)?)
        .map_err(|_| CourierSessionStoreError::Encode)?;
    for (identity, session) in sessions {
        encoder
            .array(SESSION_FIELDS)
            .map_err(|_| CourierSessionStoreError::Encode)?
            .bytes(identity)
            .map_err(|_| CourierSessionStoreError::Encode)?
            .bytes(session)
            .map_err(|_| CourierSessionStoreError::Encode)?;
    }
    encoder
        .array(u64::try_from(inbox.len()).map_err(|_| CourierSessionStoreError::Encode)?)
        .map_err(|_| CourierSessionStoreError::Encode)?;
    for message in inbox.values() {
        let payload = message
            .payload()
            .encode()
            .map_err(CourierSessionStoreError::Payload)?;
        encoder
            .array(INBOX_MESSAGE_FIELDS)
            .map_err(|_| CourierSessionStoreError::Encode)?
            .bytes(message.sender().as_bytes())
            .map_err(|_| CourierSessionStoreError::Encode)?
            .bytes(message.identifier().as_bytes())
            .map_err(|_| CourierSessionStoreError::Encode)?
            .u64(message.received_at())
            .map_err(|_| CourierSessionStoreError::Encode)?
            .bytes(&payload)
            .map_err(|_| CourierSessionStoreError::Encode)?;
    }
    Ok(encoder.into_writer())
}

fn decode_session_state(
    encoded: &[u8],
) -> Result<
    (
        BTreeMap<[u8; IDENTITY_BYTES], Vec<u8>>,
        BTreeMap<[u8; 16], crate::CourierInboxMessage>,
    ),
    CourierSessionStoreError,
> {
    let mut decoder = Decoder::new(encoded);
    if decoder
        .array()
        .map_err(|_| CourierSessionStoreError::Decode)?
        != Some(SESSION_STATE_FIELDS)
    {
        return Err(CourierSessionStoreError::InvalidShape);
    }
    if decoder.u8().map_err(|_| CourierSessionStoreError::Decode)?
        != COURIER_SESSION_STATE_SCHEMA_VERSION
    {
        return Err(CourierSessionStoreError::UnsupportedSchemaVersion);
    }
    let count = decoder
        .array()
        .map_err(|_| CourierSessionStoreError::Decode)?
        .ok_or(CourierSessionStoreError::InvalidShape)?;
    let count = usize::try_from(count).map_err(|_| CourierSessionStoreError::TooManyContacts)?;
    if count > MAX_COURIER_CONTACTS {
        return Err(CourierSessionStoreError::TooManyContacts);
    }
    let mut sessions = BTreeMap::new();
    for _ in 0..count {
        if decoder
            .array()
            .map_err(|_| CourierSessionStoreError::Decode)?
            != Some(SESSION_FIELDS)
        {
            return Err(CourierSessionStoreError::InvalidShape);
        }
        let identity: [u8; IDENTITY_BYTES] = decoder
            .bytes()
            .map_err(|_| CourierSessionStoreError::Decode)?
            .try_into()
            .map_err(|_| CourierSessionStoreError::InvalidShape)?;
        let session = decoder
            .bytes()
            .map_err(|_| CourierSessionStoreError::Decode)?
            .to_vec();
        RatchetMessageSession::decode(&session).map_err(CourierSessionStoreError::Session)?;
        if sessions.insert(identity, session).is_some() {
            return Err(CourierSessionStoreError::DuplicateContact);
        }
    }
    let message_count = decoder
        .array()
        .map_err(|_| CourierSessionStoreError::Decode)?
        .ok_or(CourierSessionStoreError::InvalidShape)?;
    let message_count = usize::try_from(message_count)
        .map_err(|_| CourierSessionStoreError::TooManyInboxMessages)?;
    if message_count > crate::MAX_COURIER_INBOX_MESSAGES {
        return Err(CourierSessionStoreError::TooManyInboxMessages);
    }
    let mut inbox = BTreeMap::new();
    for _ in 0..message_count {
        if decoder
            .array()
            .map_err(|_| CourierSessionStoreError::Decode)?
            != Some(INBOX_MESSAGE_FIELDS)
        {
            return Err(CourierSessionStoreError::InvalidShape);
        }
        let sender = IdentityPublicKey::from_bytes(
            decoder
                .bytes()
                .map_err(|_| CourierSessionStoreError::Decode)?
                .try_into()
                .map_err(|_| CourierSessionStoreError::InvalidInboxSender)?,
        )
        .map_err(|_| CourierSessionStoreError::InvalidInboxSender)?;
        let identifier = MessageIdentifier::from_bytes(
            decoder
                .bytes()
                .map_err(|_| CourierSessionStoreError::Decode)?
                .try_into()
                .map_err(|_| CourierSessionStoreError::InvalidInboxIdentifier)?,
        )
        .map_err(|_| CourierSessionStoreError::InvalidInboxIdentifier)?;
        let received_at = decoder
            .u64()
            .map_err(|_| CourierSessionStoreError::Decode)?;
        let payload = MessagePayload::decode(
            decoder
                .bytes()
                .map_err(|_| CourierSessionStoreError::Decode)?,
        )
        .map_err(CourierSessionStoreError::Payload)?;
        if inbox
            .insert(
                *identifier.as_bytes(),
                crate::CourierInboxMessage::new(sender, identifier, received_at, payload),
            )
            .is_some()
        {
            return Err(CourierSessionStoreError::DuplicateInboxIdentifier);
        }
    }
    if decoder.position() != encoded.len() {
        return Err(CourierSessionStoreError::TrailingBytes);
    }
    if encode_session_state(&sessions, &inbox)? != encoded {
        return Err(CourierSessionStoreError::NonCanonicalEncoding);
    }
    Ok((sessions, inbox))
}

#[cfg(test)]
mod tests {
    use std::{collections::BTreeMap, convert::Infallible, fs, path::PathBuf};

    use arachne_core::{
        IdentityKeypair, KeystoreEntryName, KeystoreSecret, OsKeystore, RelaySigningKeypair,
    };
    use arachne_protocol::{
        MAILBOX_CAPABILITY_TOKEN_BYTES, MAILBOX_IDENTIFIER_BYTES, MailboxCapability,
        MessageContentType, MessagePayload, RelayInvitation, TOR_ONION_SERVICE_PUBLIC_KEY_BYTES,
        TorMaildropProfileConfig,
    };

    use super::{CourierCryptographer, CourierOneTimePrekeyInventory, CourierSessionStore};

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

    fn path(name: &str) -> PathBuf {
        std::env::temp_dir().join(format!(
            "arachne-courier-{name}-{}.sqlite",
            std::process::id()
        ))
    }

    fn relay_invitation(recipient: &IdentityKeypair) -> RelayInvitation {
        RelayInvitation::create(
            &RelaySigningKeypair::generate().unwrap(),
            recipient.public_key(),
            TorMaildropProfileConfig::new([0x11; TOR_ONION_SERVICE_PUBLIC_KEY_BYTES], 443).unwrap(),
            MailboxCapability::new(
                [0x22; MAILBOX_IDENTIFIER_BYTES],
                [0x33; MAILBOX_CAPABILITY_TOKEN_BYTES],
            )
            .unwrap(),
            100,
            60,
        )
        .unwrap()
    }

    #[test]
    fn establishes_a_persistent_x3dh_ratchet_session_for_courier_text() {
        let alice = IdentityKeypair::generate().unwrap();
        let bob = IdentityKeypair::generate().unwrap();
        let mut alice_keystore = MemoryKeystore::default();
        let mut bob_keystore = MemoryKeystore::default();
        let alice_cryptographer =
            CourierCryptographer::load_or_create(&alice, &mut alice_keystore).unwrap();
        let bob_cryptographer =
            CourierCryptographer::load_or_create(&bob, &mut bob_keystore).unwrap();
        let bob_bundle = bob_cryptographer
            .bundle(&bob, relay_invitation(&bob), [0x44; 32])
            .unwrap();
        let alice_path = path("alice-session");
        let bob_path = path("bob-session");
        let _ = fs::remove_file(&alice_path);
        let _ = fs::remove_file(&bob_path);
        let mut alice_sessions =
            CourierSessionStore::open(&alice_path, &mut alice_keystore).unwrap();
        let mut bob_sessions = CourierSessionStore::open(&bob_path, &mut bob_keystore).unwrap();
        let payload =
            MessagePayload::new(MessageContentType::TextUtf8, b"hello over Tor".to_vec()).unwrap();
        let frame = alice_cryptographer
            .encrypt(
                &alice.public_key(),
                &bob_bundle,
                &mut alice_sessions,
                &payload,
            )
            .unwrap();
        let identifier = frame.message_identifier().unwrap();
        let encoded = frame.into_envelope().unwrap();
        let frame = arachne_protocol::CourierFrame::from_envelope(&encoded).unwrap();
        let received = bob_cryptographer
            .decrypt(&frame, &mut bob_sessions)
            .unwrap()
            .unwrap();
        assert_eq!(received.sender(), &alice.public_key());
        assert_eq!(received.message_identifier(), identifier);
        assert_eq!(received.payload(), &payload);
        fs::remove_file(alice_path).unwrap();
        fs::remove_file(bob_path).unwrap();
    }

    #[test]
    fn refreshes_only_new_one_time_prekeys_without_reoffering_advertised_keys() {
        let path = path("one-time-prekey-inventory");
        let _ = fs::remove_file(&path);
        let mut keystore = MemoryKeystore::default();
        let mut inventory = CourierOneTimePrekeyInventory::open(&path, &mut keystore).unwrap();
        let initial = inventory.replenish(2).unwrap();
        assert_eq!(inventory.unpublished_public().len(), 2);

        inventory.mark_unpublished_as_advertised().unwrap();
        assert!(inventory.unpublished_public().is_empty());
        assert!(inventory.load(initial[1]).is_some());

        inventory.take(initial[0]).unwrap();
        let fresh = inventory.replenish(2).unwrap();
        assert_eq!(fresh.len(), 1);
        let advertised = inventory
            .unpublished_public()
            .into_iter()
            .map(|prekey| prekey.identifier())
            .collect::<Vec<_>>();
        assert_eq!(advertised, fresh);
        assert!(inventory.load(initial[1]).is_some());

        drop(inventory);
        let reopened = CourierOneTimePrekeyInventory::open(&path, &mut keystore).unwrap();
        assert_eq!(
            reopened
                .unpublished_public()
                .into_iter()
                .map(|prekey| prekey.identifier())
                .collect::<Vec<_>>(),
            fresh
        );
        assert!(reopened.load(initial[1]).is_some());
        drop(reopened);
        fs::remove_file(path).unwrap();
    }
}
