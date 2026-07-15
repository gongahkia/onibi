use minicbor::{Decoder, Encoder, data::Type};
use sha2::Sha256;
use yeokcham_core::{X25519Prekey, X25519PrekeyPublicKey};
use zeroize::Zeroizing;

use crate::{CryptoDomain, X3dhSession};

pub const DOUBLE_RATCHET_STATE_SCHEMA_VERSION: u8 = 2;
pub const RATCHET_KEY_BYTES: usize = 32;
pub const MAX_SKIPPED_MESSAGE_KEYS: usize = 1_000;
pub const MAX_RETIRED_RATCHET_KEYS: usize = 32;
const STATE_FIELDS_V1: u64 = 9;
const STATE_FIELDS: u64 = 11;
const SKIPPED_KEY_FIELDS: u64 = 3;
const ROOT_DERIVATION_OUTPUT_BYTES: usize = 64;
const CHAIN_DERIVATION_OUTPUT_BYTES: usize = 64;

pub struct DoubleRatchetState {
    root_key: Zeroizing<[u8; RATCHET_KEY_BYTES]>,
    sending_chain: Option<Zeroizing<[u8; RATCHET_KEY_BYTES]>>,
    receiving_chain: Option<Zeroizing<[u8; RATCHET_KEY_BYTES]>>,
    local_ratchet: X25519Prekey,
    remote_ratchet: X25519PrekeyPublicKey,
    sending_count: u32,
    receiving_count: u32,
    previous_sending_count: u32,
    skipped_message_keys: Vec<SkippedMessageKey>,
    retired_remote_ratchets: Vec<[u8; RATCHET_KEY_BYTES]>,
}

struct SkippedMessageKey {
    remote_ratchet: [u8; RATCHET_KEY_BYTES],
    message_number: u32,
    key: Zeroizing<[u8; RATCHET_KEY_BYTES]>,
}

impl DoubleRatchetState {
    pub fn from_x3dh(
        session: &X3dhSession,
        remote_ratchet: X25519PrekeyPublicKey,
    ) -> Result<Self, DoubleRatchetError> {
        Self::initialize(*session.root_key(), remote_ratchet)
    }

    pub fn initialize(
        root_key: [u8; RATCHET_KEY_BYTES],
        remote_ratchet: X25519PrekeyPublicKey,
    ) -> Result<Self, DoubleRatchetError> {
        if root_key.iter().all(|byte| *byte == 0) {
            return Err(DoubleRatchetError::InvalidRootKey);
        }
        let local_ratchet = X25519Prekey::generate().map_err(|_| DoubleRatchetError::Randomness)?;
        let (root_key, sending_chain) = derive_root(&root_key, &local_ratchet, &remote_ratchet)?;
        Ok(Self {
            root_key,
            sending_chain: Some(sending_chain),
            receiving_chain: None,
            local_ratchet,
            remote_ratchet,
            sending_count: 0,
            receiving_count: 0,
            previous_sending_count: 0,
            skipped_message_keys: Vec::new(),
            retired_remote_ratchets: Vec::new(),
        })
    }

    pub fn ratchet_receive(
        &mut self,
        remote_ratchet: X25519PrekeyPublicKey,
    ) -> Result<(), DoubleRatchetError> {
        self.advance_ratchet(remote_ratchet)
    }

    pub fn receive_key(
        &mut self,
        remote_ratchet: X25519PrekeyPublicKey,
        previous_sending_count: u32,
        message_number: u32,
    ) -> Result<Zeroizing<[u8; RATCHET_KEY_BYTES]>, DoubleRatchetError> {
        if let Some(position) = self.skipped_message_keys.iter().position(|skipped| {
            skipped.remote_ratchet == *remote_ratchet.as_bytes()
                && skipped.message_number == message_number
        }) {
            return Ok(self.skipped_message_keys.remove(position).key);
        }
        if remote_ratchet == self.remote_ratchet && message_number < self.receiving_count {
            return Err(DoubleRatchetError::ReplayedMessage);
        }
        if remote_ratchet != self.remote_ratchet {
            if self
                .retired_remote_ratchets
                .contains(remote_ratchet.as_bytes())
            {
                return Err(DoubleRatchetError::ReplayedMessage);
            }
            self.skip_receiving_keys(previous_sending_count)?;
            self.advance_ratchet(remote_ratchet)?;
        }
        self.skip_receiving_keys(message_number)?;
        self.next_receiving_key()
    }

    fn advance_ratchet(
        &mut self,
        remote_ratchet: X25519PrekeyPublicKey,
    ) -> Result<(), DoubleRatchetError> {
        if remote_ratchet == self.remote_ratchet {
            if self.receiving_chain.is_some() {
                return Err(DoubleRatchetError::ReplayedMessage);
            }
        } else {
            if self
                .retired_remote_ratchets
                .contains(remote_ratchet.as_bytes())
            {
                return Err(DoubleRatchetError::ReplayedMessage);
            }
            if self.retired_remote_ratchets.len() == MAX_RETIRED_RATCHET_KEYS {
                return Err(DoubleRatchetError::TooManyRatchetSteps);
            }
            self.retired_remote_ratchets
                .push(*self.remote_ratchet.as_bytes());
        }
        let (root_key, receiving_chain) =
            derive_root(&self.root_key, &self.local_ratchet, &remote_ratchet)?;
        let local_ratchet = X25519Prekey::generate().map_err(|_| DoubleRatchetError::Randomness)?;
        let (root_key, sending_chain) = derive_root(&root_key, &local_ratchet, &remote_ratchet)?;
        self.previous_sending_count = self.sending_count;
        self.sending_count = 0;
        self.receiving_count = 0;
        self.root_key = root_key;
        self.sending_chain = Some(sending_chain);
        self.receiving_chain = Some(receiving_chain);
        self.local_ratchet = local_ratchet;
        self.remote_ratchet = remote_ratchet;
        Ok(())
    }

    fn skip_receiving_keys(&mut self, until: u32) -> Result<(), DoubleRatchetError> {
        if until <= self.receiving_count {
            return Ok(());
        }
        let missing = usize::try_from(until - self.receiving_count)
            .map_err(|_| DoubleRatchetError::TooManySkippedMessages)?;
        if missing > MAX_SKIPPED_MESSAGE_KEYS - self.skipped_message_keys.len() {
            return Err(DoubleRatchetError::TooManySkippedMessages);
        }
        let remote_ratchet = *self.remote_ratchet.as_bytes();
        while self.receiving_count < until {
            let message_number = self.receiving_count;
            let key = self.next_receiving_key()?;
            self.skipped_message_keys.push(SkippedMessageKey {
                remote_ratchet,
                message_number,
                key,
            });
        }
        Ok(())
    }

    pub fn next_sending_key(
        &mut self,
    ) -> Result<Zeroizing<[u8; RATCHET_KEY_BYTES]>, DoubleRatchetError> {
        let chain = self
            .sending_chain
            .as_mut()
            .ok_or(DoubleRatchetError::MissingSendingChain)?;
        let key = advance_chain(chain)?;
        self.sending_count = self
            .sending_count
            .checked_add(1)
            .ok_or(DoubleRatchetError::CounterExhausted)?;
        Ok(key)
    }

    pub fn next_receiving_key(
        &mut self,
    ) -> Result<Zeroizing<[u8; RATCHET_KEY_BYTES]>, DoubleRatchetError> {
        let chain = self
            .receiving_chain
            .as_mut()
            .ok_or(DoubleRatchetError::MissingReceivingChain)?;
        let key = advance_chain(chain)?;
        self.receiving_count = self
            .receiving_count
            .checked_add(1)
            .ok_or(DoubleRatchetError::CounterExhausted)?;
        Ok(key)
    }

    pub fn encode(&self) -> Result<Zeroizing<Vec<u8>>, DoubleRatchetError> {
        let local_ratchet = self.local_ratchet.serialize();
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(STATE_FIELDS)
            .map_err(|_| DoubleRatchetError::Encode)?
            .u8(DOUBLE_RATCHET_STATE_SCHEMA_VERSION)
            .map_err(|_| DoubleRatchetError::Encode)?
            .bytes(&*self.root_key)
            .map_err(|_| DoubleRatchetError::Encode)?;
        encode_optional_key(&mut encoder, self.sending_chain.as_deref())?;
        encode_optional_key(&mut encoder, self.receiving_chain.as_deref())?;
        encoder
            .bytes(&*local_ratchet)
            .map_err(|_| DoubleRatchetError::Encode)?
            .bytes(self.remote_ratchet.as_bytes())
            .map_err(|_| DoubleRatchetError::Encode)?
            .u32(self.sending_count)
            .map_err(|_| DoubleRatchetError::Encode)?
            .u32(self.receiving_count)
            .map_err(|_| DoubleRatchetError::Encode)?
            .u32(self.previous_sending_count)
            .map_err(|_| DoubleRatchetError::Encode)?
            .array(
                u64::try_from(self.skipped_message_keys.len())
                    .map_err(|_| DoubleRatchetError::Encode)?,
            )
            .map_err(|_| DoubleRatchetError::Encode)?;
        let mut skipped_message_keys: Vec<&SkippedMessageKey> =
            self.skipped_message_keys.iter().collect();
        skipped_message_keys.sort_unstable_by(|left, right| {
            (left.remote_ratchet, left.message_number)
                .cmp(&(right.remote_ratchet, right.message_number))
        });
        for skipped in skipped_message_keys {
            encoder
                .array(SKIPPED_KEY_FIELDS)
                .map_err(|_| DoubleRatchetError::Encode)?
                .bytes(&skipped.remote_ratchet)
                .map_err(|_| DoubleRatchetError::Encode)?
                .u32(skipped.message_number)
                .map_err(|_| DoubleRatchetError::Encode)?
                .bytes(&*skipped.key)
                .map_err(|_| DoubleRatchetError::Encode)?;
        }
        encoder
            .array(
                u64::try_from(self.retired_remote_ratchets.len())
                    .map_err(|_| DoubleRatchetError::Encode)?,
            )
            .map_err(|_| DoubleRatchetError::Encode)?;
        let mut retired_remote_ratchets = self.retired_remote_ratchets.clone();
        retired_remote_ratchets.sort_unstable();
        for remote_ratchet in &retired_remote_ratchets {
            encoder
                .bytes(remote_ratchet)
                .map_err(|_| DoubleRatchetError::Encode)?;
        }
        Ok(Zeroizing::new(encoder.into_writer()))
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, DoubleRatchetError> {
        let mut decoder = Decoder::new(encoded);
        let fields = decoder.array().map_err(|_| DoubleRatchetError::Decode)?;
        let version = decoder.u8().map_err(|_| DoubleRatchetError::Decode)?;
        let expected_fields = match version {
            1 => STATE_FIELDS_V1,
            DOUBLE_RATCHET_STATE_SCHEMA_VERSION => STATE_FIELDS,
            _ => return Err(DoubleRatchetError::UnsupportedSchemaVersion),
        };
        if fields != Some(expected_fields) {
            return Err(DoubleRatchetError::InvalidShape);
        }
        let root_key = decode_key(decoder.bytes().map_err(|_| DoubleRatchetError::Decode)?)?;
        let sending_chain = decode_optional_key(&mut decoder)?;
        let receiving_chain = decode_optional_key(&mut decoder)?;
        let local_ratchet =
            X25519Prekey::deserialize(decoder.bytes().map_err(|_| DoubleRatchetError::Decode)?)
                .map_err(|_| DoubleRatchetError::InvalidLocalRatchet)?;
        let remote_ratchet =
            decode_public(decoder.bytes().map_err(|_| DoubleRatchetError::Decode)?)?;
        let sending_count = decoder.u32().map_err(|_| DoubleRatchetError::Decode)?;
        let receiving_count = decoder.u32().map_err(|_| DoubleRatchetError::Decode)?;
        let previous_sending_count = decoder.u32().map_err(|_| DoubleRatchetError::Decode)?;
        let (skipped_message_keys, retired_remote_ratchets) = if version == 1 {
            (Vec::new(), Vec::new())
        } else {
            (
                decode_skipped_message_keys(&mut decoder)?,
                decode_retired_remote_ratchets(&mut decoder)?,
            )
        };
        if decoder.position() != encoded.len() {
            return Err(DoubleRatchetError::TrailingBytes);
        }
        let state = Self {
            root_key: Zeroizing::new(root_key),
            sending_chain: sending_chain.map(Zeroizing::new),
            receiving_chain: receiving_chain.map(Zeroizing::new),
            local_ratchet,
            remote_ratchet,
            sending_count,
            receiving_count,
            previous_sending_count,
            skipped_message_keys,
            retired_remote_ratchets,
        };
        if state
            .retired_remote_ratchets
            .contains(state.remote_ratchet.as_bytes())
            || state.skipped_message_keys.iter().any(|skipped| {
                (skipped.remote_ratchet == *state.remote_ratchet.as_bytes()
                    && skipped.message_number >= state.receiving_count)
                    || (skipped.remote_ratchet != *state.remote_ratchet.as_bytes()
                        && !state
                            .retired_remote_ratchets
                            .contains(&skipped.remote_ratchet))
            })
        {
            return Err(DoubleRatchetError::NonCanonicalEncoding);
        }
        if version == DOUBLE_RATCHET_STATE_SCHEMA_VERSION && &*state.encode()? != encoded {
            return Err(DoubleRatchetError::NonCanonicalEncoding);
        }
        Ok(state)
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum DoubleRatchetError {
    #[error("operating-system random source failed")]
    Randomness,
    #[error("ratchet root key is invalid")]
    InvalidRootKey,
    #[error("ratchet peer key is invalid")]
    InvalidPeerKey,
    #[error("ratchet key agreement failed")]
    KeyAgreement,
    #[error("ratchet derivation failed")]
    KeyDerivation,
    #[error("ratchet sending chain is unavailable")]
    MissingSendingChain,
    #[error("ratchet receiving chain is unavailable")]
    MissingReceivingChain,
    #[error("ratchet counter is exhausted")]
    CounterExhausted,
    #[error("message has already been processed")]
    ReplayedMessage,
    #[error("too many skipped message keys")]
    TooManySkippedMessages,
    #[error("too many remote ratchet steps")]
    TooManyRatchetSteps,
    #[error("ratchet state encoding failed")]
    Encode,
    #[error("ratchet state decoding failed")]
    Decode,
    #[error("ratchet state has an invalid shape")]
    InvalidShape,
    #[error("ratchet state schema version is unsupported")]
    UnsupportedSchemaVersion,
    #[error("ratchet state contains an invalid local ratchet key")]
    InvalidLocalRatchet,
    #[error("ratchet state has trailing bytes")]
    TrailingBytes,
    #[error("ratchet state is not canonically encoded")]
    NonCanonicalEncoding,
}

fn derive_root(
    root: &[u8; RATCHET_KEY_BYTES],
    local: &X25519Prekey,
    remote: &X25519PrekeyPublicKey,
) -> Result<
    (
        Zeroizing<[u8; RATCHET_KEY_BYTES]>,
        Zeroizing<[u8; RATCHET_KEY_BYTES]>,
    ),
    DoubleRatchetError,
> {
    let shared = local
        .shared_secret(remote.as_bytes())
        .map_err(|_| DoubleRatchetError::KeyAgreement)?;
    let hkdf = hkdf::Hkdf::<Sha256>::new(Some(root), shared.as_ref());
    let mut output = [0; ROOT_DERIVATION_OUTPUT_BYTES];
    hkdf.expand(CryptoDomain::RatchetRootKey.context(), &mut output)
        .map_err(|_| DoubleRatchetError::KeyDerivation)?;
    let mut next_root = [0; RATCHET_KEY_BYTES];
    next_root.copy_from_slice(&output[..RATCHET_KEY_BYTES]);
    let mut chain = [0; RATCHET_KEY_BYTES];
    chain.copy_from_slice(&output[RATCHET_KEY_BYTES..]);
    Ok((Zeroizing::new(next_root), Zeroizing::new(chain)))
}

fn advance_chain(
    chain: &mut Zeroizing<[u8; RATCHET_KEY_BYTES]>,
) -> Result<Zeroizing<[u8; RATCHET_KEY_BYTES]>, DoubleRatchetError> {
    let hkdf = hkdf::Hkdf::<Sha256>::new(None, chain.as_ref());
    let mut output = [0; CHAIN_DERIVATION_OUTPUT_BYTES];
    hkdf.expand(CryptoDomain::RatchetChainKey.context(), &mut output)
        .map_err(|_| DoubleRatchetError::KeyDerivation)?;
    chain.copy_from_slice(&output[..RATCHET_KEY_BYTES]);
    let mut key = [0; RATCHET_KEY_BYTES];
    key.copy_from_slice(&output[RATCHET_KEY_BYTES..]);
    Ok(Zeroizing::new(key))
}

fn encode_optional_key(
    encoder: &mut Encoder<Vec<u8>>,
    key: Option<&[u8; RATCHET_KEY_BYTES]>,
) -> Result<(), DoubleRatchetError> {
    match key {
        Some(key) => encoder.bytes(key).map_err(|_| DoubleRatchetError::Encode)?,
        None => encoder.null().map_err(|_| DoubleRatchetError::Encode)?,
    };
    Ok(())
}
fn decode_optional_key(
    decoder: &mut Decoder<'_>,
) -> Result<Option<[u8; RATCHET_KEY_BYTES]>, DoubleRatchetError> {
    match decoder.datatype().map_err(|_| DoubleRatchetError::Decode)? {
        Type::Null => {
            decoder.null().map_err(|_| DoubleRatchetError::Decode)?;
            Ok(None)
        }
        Type::Bytes => {
            decode_key(decoder.bytes().map_err(|_| DoubleRatchetError::Decode)?).map(Some)
        }
        _ => Err(DoubleRatchetError::InvalidShape),
    }
}
fn decode_key(bytes: &[u8]) -> Result<[u8; RATCHET_KEY_BYTES], DoubleRatchetError> {
    let key: [u8; RATCHET_KEY_BYTES] = bytes
        .try_into()
        .map_err(|_| DoubleRatchetError::InvalidRootKey)?;
    if key.iter().all(|byte| *byte == 0) {
        return Err(DoubleRatchetError::InvalidRootKey);
    }
    Ok(key)
}
fn decode_public(bytes: &[u8]) -> Result<X25519PrekeyPublicKey, DoubleRatchetError> {
    let key: [u8; RATCHET_KEY_BYTES] = bytes
        .try_into()
        .map_err(|_| DoubleRatchetError::InvalidPeerKey)?;
    X25519PrekeyPublicKey::from_bytes(key).map_err(|_| DoubleRatchetError::InvalidPeerKey)
}

fn decode_skipped_message_keys(
    decoder: &mut Decoder<'_>,
) -> Result<Vec<SkippedMessageKey>, DoubleRatchetError> {
    let length = decoder.array().map_err(|_| DoubleRatchetError::Decode)?;
    let length = usize::try_from(length.ok_or(DoubleRatchetError::InvalidShape)?)
        .map_err(|_| DoubleRatchetError::InvalidShape)?;
    if length > MAX_SKIPPED_MESSAGE_KEYS {
        return Err(DoubleRatchetError::TooManySkippedMessages);
    }
    let mut skipped_message_keys = Vec::with_capacity(length);
    let mut previous = None;
    for _ in 0..length {
        if decoder.array().map_err(|_| DoubleRatchetError::Decode)? != Some(SKIPPED_KEY_FIELDS) {
            return Err(DoubleRatchetError::InvalidShape);
        }
        let remote_ratchet =
            *decode_public(decoder.bytes().map_err(|_| DoubleRatchetError::Decode)?)?.as_bytes();
        let message_number = decoder.u32().map_err(|_| DoubleRatchetError::Decode)?;
        let key = decode_key(decoder.bytes().map_err(|_| DoubleRatchetError::Decode)?)?;
        let current = (remote_ratchet, message_number);
        if previous.is_some_and(|previous| previous >= current) {
            return Err(DoubleRatchetError::NonCanonicalEncoding);
        }
        previous = Some(current);
        skipped_message_keys.push(SkippedMessageKey {
            remote_ratchet,
            message_number,
            key: Zeroizing::new(key),
        });
    }
    Ok(skipped_message_keys)
}

fn decode_retired_remote_ratchets(
    decoder: &mut Decoder<'_>,
) -> Result<Vec<[u8; RATCHET_KEY_BYTES]>, DoubleRatchetError> {
    let length = decoder.array().map_err(|_| DoubleRatchetError::Decode)?;
    let length = usize::try_from(length.ok_or(DoubleRatchetError::InvalidShape)?)
        .map_err(|_| DoubleRatchetError::InvalidShape)?;
    if length > MAX_RETIRED_RATCHET_KEYS {
        return Err(DoubleRatchetError::TooManyRatchetSteps);
    }
    let mut retired_remote_ratchets = Vec::with_capacity(length);
    let mut previous = None;
    for _ in 0..length {
        let remote_ratchet =
            *decode_public(decoder.bytes().map_err(|_| DoubleRatchetError::Decode)?)?.as_bytes();
        if previous.is_some_and(|previous| previous >= remote_ratchet) {
            return Err(DoubleRatchetError::NonCanonicalEncoding);
        }
        previous = Some(remote_ratchet);
        retired_remote_ratchets.push(remote_ratchet);
    }
    Ok(retired_remote_ratchets)
}

#[cfg(test)]
mod tests {
    use super::{DoubleRatchetError, DoubleRatchetState, MAX_SKIPPED_MESSAGE_KEYS};
    use yeokcham_core::X25519Prekey;
    #[test]
    fn advances_and_round_trips_durable_ratchet_state() {
        let peer = X25519Prekey::generate().unwrap();
        let mut state = DoubleRatchetState::initialize([1; 32], peer.public_key()).unwrap();
        let first = state.next_sending_key().unwrap();
        let encoded = state.encode().unwrap();
        let mut restored = DoubleRatchetState::decode(&encoded).unwrap();
        assert_ne!(first, restored.next_sending_key().unwrap());
        restored.ratchet_receive(peer.public_key()).unwrap();
        assert!(restored.next_receiving_key().is_ok());
    }

    #[test]
    fn receives_out_of_order_messages_once_and_persists_skipped_keys() {
        let peer = X25519Prekey::generate().unwrap();
        let peer_public = peer.public_key();
        let mut state = DoubleRatchetState::initialize([2; 32], peer_public).unwrap();
        state.ratchet_receive(peer_public).unwrap();

        let third = state.receive_key(peer_public, 0, 2).unwrap();
        let encoded = state.encode().unwrap();
        let mut restored = DoubleRatchetState::decode(&encoded).unwrap();
        let first = restored.receive_key(peer_public, 0, 0).unwrap();
        let second = restored.receive_key(peer_public, 0, 1).unwrap();

        assert_ne!(first, second);
        assert_ne!(second, third);
        assert_eq!(
            restored.receive_key(peer_public, 0, 1).unwrap_err(),
            DoubleRatchetError::ReplayedMessage
        );
    }

    #[test]
    fn rejects_unbounded_gaps_without_advancing_the_receive_chain() {
        let peer = X25519Prekey::generate().unwrap();
        let peer_public = peer.public_key();
        let mut state = DoubleRatchetState::initialize([3; 32], peer_public).unwrap();
        state.ratchet_receive(peer_public).unwrap();

        assert_eq!(
            state
                .receive_key(
                    peer_public,
                    0,
                    u32::try_from(MAX_SKIPPED_MESSAGE_KEYS + 1).unwrap(),
                )
                .unwrap_err(),
            DoubleRatchetError::TooManySkippedMessages
        );
        assert!(state.receive_key(peer_public, 0, 0).is_ok());
    }

    #[test]
    fn rejects_messages_from_retired_ratchets() {
        let first_peer = X25519Prekey::generate().unwrap();
        let first_public = first_peer.public_key();
        let second_public = X25519Prekey::generate().unwrap().public_key();
        let mut state = DoubleRatchetState::initialize([4; 32], first_public).unwrap();
        state.ratchet_receive(first_public).unwrap();
        assert!(state.receive_key(second_public, 0, 0).is_ok());

        assert_eq!(
            state.receive_key(first_public, 0, 0).unwrap_err(),
            DoubleRatchetError::ReplayedMessage
        );
    }
}
