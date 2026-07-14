use minicbor::{Decoder, Encoder, data::Type};
use sha2::Sha256;
use yeokcham_core::{X25519Prekey, X25519PrekeyPublicKey};
use zeroize::Zeroizing;

use crate::{CryptoDomain, X3dhSession};

pub const DOUBLE_RATCHET_STATE_SCHEMA_VERSION: u8 = 1;
pub const RATCHET_KEY_BYTES: usize = 32;
const STATE_FIELDS: u64 = 9;
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
        })
    }

    pub fn ratchet_receive(
        &mut self,
        remote_ratchet: X25519PrekeyPublicKey,
    ) -> Result<(), DoubleRatchetError> {
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
            .map_err(|_| DoubleRatchetError::Encode)?;
        Ok(Zeroizing::new(encoder.into_writer()))
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, DoubleRatchetError> {
        let mut decoder = Decoder::new(encoded);
        if decoder.array().map_err(|_| DoubleRatchetError::Decode)? != Some(STATE_FIELDS) {
            return Err(DoubleRatchetError::InvalidShape);
        }
        if decoder.u8().map_err(|_| DoubleRatchetError::Decode)?
            != DOUBLE_RATCHET_STATE_SCHEMA_VERSION
        {
            return Err(DoubleRatchetError::UnsupportedSchemaVersion);
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
        };
        if &*state.encode()? != encoded {
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

#[cfg(test)]
mod tests {
    use super::DoubleRatchetState;
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
}
