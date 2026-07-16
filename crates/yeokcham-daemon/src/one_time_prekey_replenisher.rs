use std::{collections::BTreeSet, error::Error, path::Path};

use minicbor::{Decoder, Encoder};
use yeokcham_core::{
    OneTimePrekeyId, OneTimePrekeyIdError, OneTimePrekeyStore, OneTimePrekeyStoreError, OsKeystore,
    X25519Prekey, X25519PrekeyError,
};
use yeokcham_protocol::{MAX_ONE_TIME_PREKEYS, OneTimePrekeyPublic};

use crate::{EncryptedStateStore, StateDocument, StateDocumentError, StateStoreError};

pub const ONE_TIME_PREKEY_INVENTORY_SCHEMA_VERSION: u8 = 1;
const INVENTORY_FIELDS: u64 = 3;

pub struct OneTimePrekeyReplenisher<K> {
    state: EncryptedStateStore,
    prekeys: OneTimePrekeyStore<K>,
    next_identifier: u64,
    available: BTreeSet<OneTimePrekeyId>,
}

impl<K> OneTimePrekeyReplenisher<K>
where
    K: OsKeystore,
{
    pub fn open(path: &Path, keystore: K) -> Result<Self, OneTimePrekeyReplenisherError<K::Error>> {
        let mut keystore = keystore;
        let state = EncryptedStateStore::open(path, &mut keystore)
            .map_err(OneTimePrekeyReplenisherError::StateStore)?;
        let (next_identifier, available) = match state
            .load()
            .map_err(OneTimePrekeyReplenisherError::StateStore)?
        {
            Some(document) => decode_inventory(document.as_bytes())
                .map_err(OneTimePrekeyReplenisherError::Inventory)?,
            None => (1, BTreeSet::new()),
        };
        Ok(Self {
            state,
            prekeys: OneTimePrekeyStore::new(keystore),
            next_identifier,
            available,
        })
    }

    pub fn replenish(
        &mut self,
        target: usize,
    ) -> Result<Vec<OneTimePrekeyId>, OneTimePrekeyReplenisherError<K::Error>> {
        if target > MAX_ONE_TIME_PREKEYS {
            return Err(OneTimePrekeyReplenisherError::TargetTooLarge);
        }
        let deficit = target.saturating_sub(self.available.len());
        let identifiers = self.reserve_identifiers(deficit)?;
        if identifiers.is_empty() {
            return Ok(identifiers);
        }
        self.persist()?;
        for identifier in &identifiers {
            let prekey = X25519Prekey::generate().map_err(OneTimePrekeyReplenisherError::Prekey)?;
            self.prekeys
                .store(*identifier, prekey)
                .map_err(OneTimePrekeyReplenisherError::Keystore)?;
        }
        self.available.extend(identifiers.iter().copied());
        self.persist()?;
        Ok(identifiers)
    }

    #[must_use]
    pub fn available(&self) -> impl ExactSizeIterator<Item = OneTimePrekeyId> + '_ {
        self.available.iter().copied()
    }

    pub fn available_public(
        &self,
    ) -> Result<Vec<OneTimePrekeyPublic>, OneTimePrekeyReplenisherError<K::Error>> {
        self.available
            .iter()
            .map(|identifier| {
                self.prekeys
                    .public(*identifier)
                    .map(|prekey| OneTimePrekeyPublic::from_public(*identifier, prekey))
                    .map_err(OneTimePrekeyReplenisherError::Keystore)
            })
            .collect()
    }

    #[must_use]
    pub fn into_keystore(self) -> K {
        self.prekeys.into_inner()
    }

    fn reserve_identifiers(
        &mut self,
        count: usize,
    ) -> Result<Vec<OneTimePrekeyId>, OneTimePrekeyReplenisherError<K::Error>> {
        let mut identifiers = Vec::with_capacity(count);
        for _ in 0..count {
            let identifier = OneTimePrekeyId::new(self.next_identifier)
                .map_err(OneTimePrekeyReplenisherError::Identifier)?;
            self.next_identifier = self
                .next_identifier
                .checked_add(1)
                .ok_or(OneTimePrekeyReplenisherError::IdentifierExhausted)?;
            identifiers.push(identifier);
        }
        Ok(identifiers)
    }

    fn persist(&mut self) -> Result<(), OneTimePrekeyReplenisherError<K::Error>> {
        let encoded = encode_inventory(self.next_identifier, &self.available)
            .map_err(OneTimePrekeyReplenisherError::Inventory)?;
        let document =
            StateDocument::new(encoded).map_err(OneTimePrekeyReplenisherError::Document)?;
        self.state
            .replace(&document)
            .map_err(OneTimePrekeyReplenisherError::StateStore)
    }
}

#[derive(Debug, thiserror::Error)]
pub enum OneTimePrekeyReplenisherError<E>
where
    E: Error + Send + Sync + 'static,
{
    #[error("encrypted state-store operation failed")]
    StateStore(#[source] StateStoreError),
    #[error("one-time prekey keystore operation failed")]
    Keystore(#[source] OneTimePrekeyStoreError<E>),
    #[error("one-time prekey generation failed")]
    Prekey(#[source] X25519PrekeyError),
    #[error("one-time prekey identifier is invalid")]
    Identifier(#[source] OneTimePrekeyIdError),
    #[error("one-time prekey identifier space is exhausted")]
    IdentifierExhausted,
    #[error("one-time prekey replenishment target exceeds the configured limit")]
    TargetTooLarge,
    #[error("one-time prekey inventory is invalid")]
    Inventory(#[source] InventoryError),
    #[error("one-time prekey inventory exceeds state-document bounds")]
    Document(#[source] StateDocumentError),
}

#[derive(Debug, thiserror::Error)]
pub enum InventoryError {
    #[error("inventory encoding failed")]
    Encode,
    #[error("inventory decoding failed")]
    Decode(#[source] minicbor::decode::Error),
    #[error("inventory has an invalid shape")]
    InvalidShape,
    #[error("inventory schema version is unsupported")]
    UnsupportedSchemaVersion,
    #[error("inventory has an invalid next identifier")]
    InvalidNextIdentifier,
    #[error("inventory contains too many prekeys")]
    TooManyPrekeys,
    #[error("inventory contains a duplicate prekey identifier")]
    DuplicateIdentifier,
    #[error("inventory identifier is invalid")]
    InvalidIdentifier,
    #[error("inventory is not canonical")]
    NonCanonical,
}

fn encode_inventory(
    next_identifier: u64,
    available: &BTreeSet<OneTimePrekeyId>,
) -> Result<Vec<u8>, InventoryError> {
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(INVENTORY_FIELDS)
        .map_err(|_| InventoryError::Encode)?
        .u8(ONE_TIME_PREKEY_INVENTORY_SCHEMA_VERSION)
        .map_err(|_| InventoryError::Encode)?
        .u64(next_identifier)
        .map_err(|_| InventoryError::Encode)?
        .array(u64::try_from(available.len()).map_err(|_| InventoryError::Encode)?)
        .map_err(|_| InventoryError::Encode)?;
    for identifier in available {
        encoder
            .u64(identifier.get())
            .map_err(|_| InventoryError::Encode)?;
    }
    Ok(encoder.into_writer())
}

fn decode_inventory(encoded: &[u8]) -> Result<(u64, BTreeSet<OneTimePrekeyId>), InventoryError> {
    let mut decoder = Decoder::new(encoded);
    if decoder.array().map_err(InventoryError::Decode)? != Some(INVENTORY_FIELDS) {
        return Err(InventoryError::InvalidShape);
    }
    if decoder.u8().map_err(InventoryError::Decode)? != ONE_TIME_PREKEY_INVENTORY_SCHEMA_VERSION {
        return Err(InventoryError::UnsupportedSchemaVersion);
    }
    let next_identifier = decoder.u64().map_err(InventoryError::Decode)?;
    if next_identifier == 0 {
        return Err(InventoryError::InvalidNextIdentifier);
    }
    let count = decoder
        .array()
        .map_err(InventoryError::Decode)?
        .ok_or(InventoryError::InvalidShape)?;
    let count = usize::try_from(count).map_err(|_| InventoryError::TooManyPrekeys)?;
    if count > MAX_ONE_TIME_PREKEYS {
        return Err(InventoryError::TooManyPrekeys);
    }
    let mut available = BTreeSet::new();
    for _ in 0..count {
        let identifier = OneTimePrekeyId::new(decoder.u64().map_err(InventoryError::Decode)?)
            .map_err(|_| InventoryError::InvalidIdentifier)?;
        if identifier.get() >= next_identifier {
            return Err(InventoryError::InvalidNextIdentifier);
        }
        if !available.insert(identifier) {
            return Err(InventoryError::DuplicateIdentifier);
        }
    }
    if decoder.position() != encoded.len()
        || encode_inventory(next_identifier, &available)? != encoded
    {
        return Err(InventoryError::NonCanonical);
    }
    Ok((next_identifier, available))
}
