use yeokcham_core::{
    ED25519_SIGNATURE_BYTES, IdentityKeypair, KeystoreEntryName, KeystoreSecret,
    KeystoreSecretError, OsKeystore, X25519_PREKEY_SERIALIZED_BYTES, X25519Prekey,
    X25519PrekeySerializationError,
};
use yeokcham_protocol::{
    SignedPrekey, SignedPrekeyError, SignedPrekeyPublic, SignedPrekeyValidationError,
};

pub const SIGNED_PREKEY_KEY_ENTRY: &str = "yeokcham_signed_prekey_v1";
const SIGNED_PREKEY_SERIALIZATION_VERSION: u8 = 1;
const SIGNED_PREKEY_SERIALIZED_BYTES: usize =
    1 + 8 + X25519_PREKEY_SERIALIZED_BYTES + ED25519_SIGNATURE_BYTES;

pub struct SignedPrekeyLifecycle {
    signed_prekey: SignedPrekey,
}

impl SignedPrekeyLifecycle {
    pub fn create<K: OsKeystore>(
        identity: &IdentityKeypair,
        keystore: &mut K,
    ) -> Result<Self, SignedPrekeyLifecycleError> {
        let entry = signed_prekey_entry()?;
        if keystore
            .load(&entry)
            .map_err(|_| SignedPrekeyLifecycleError::Keystore)?
            .is_some()
        {
            return Err(SignedPrekeyLifecycleError::AlreadyInitialized);
        }
        let signed_prekey =
            SignedPrekey::generate(identity).map_err(SignedPrekeyLifecycleError::SignedPrekey)?;
        persist(keystore, &entry, &signed_prekey)?;
        Ok(Self { signed_prekey })
    }

    pub fn load<K: OsKeystore>(
        identity: &IdentityKeypair,
        keystore: &K,
    ) -> Result<Self, SignedPrekeyLifecycleError> {
        let entry = signed_prekey_entry()?;
        let secret = keystore
            .load(&entry)
            .map_err(|_| SignedPrekeyLifecycleError::Keystore)?
            .ok_or(SignedPrekeyLifecycleError::NotInitialized)?;
        let signed_prekey = deserialize(identity, secret.as_bytes())?;
        Ok(Self { signed_prekey })
    }

    pub fn create_or_load<K: OsKeystore>(
        identity: &IdentityKeypair,
        keystore: &mut K,
    ) -> Result<(Self, SignedPrekeyInitialization), SignedPrekeyLifecycleError> {
        match Self::load(identity, keystore) {
            Ok(signed_prekey) => Ok((signed_prekey, SignedPrekeyInitialization::Loaded)),
            Err(SignedPrekeyLifecycleError::NotInitialized) => {
                let signed_prekey = Self::create(identity, keystore)?;
                Ok((signed_prekey, SignedPrekeyInitialization::Created))
            }
            Err(error) => Err(error),
        }
    }

    pub fn rotate<K: OsKeystore>(
        &mut self,
        identity: &IdentityKeypair,
        keystore: &mut K,
    ) -> Result<(), SignedPrekeyLifecycleError> {
        let entry = signed_prekey_entry()?;
        let mut replacement = SignedPrekey::from_parts(
            &identity.public_key(),
            self.signed_prekey.generation(),
            X25519Prekey::deserialize(self.signed_prekey.prekey().serialize().as_ref())
                .map_err(SignedPrekeyLifecycleError::Prekey)?,
            *self.signed_prekey.public().signature(),
        )
        .map_err(SignedPrekeyLifecycleError::InvalidStoredPrekey)?;
        replacement
            .rotate(identity)
            .map_err(SignedPrekeyLifecycleError::SignedPrekey)?;
        persist(keystore, &entry, &replacement)?;
        self.signed_prekey = replacement;
        Ok(())
    }

    #[must_use]
    pub fn public(&self) -> SignedPrekeyPublic {
        self.signed_prekey.public()
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum SignedPrekeyInitialization {
    Created,
    Loaded,
}

#[derive(Debug, thiserror::Error)]
pub enum SignedPrekeyLifecycleError {
    #[error("signed-prekey key entry is invalid")]
    InvalidKeyEntry,
    #[error("signed prekey already exists")]
    AlreadyInitialized,
    #[error("signed prekey does not exist")]
    NotInitialized,
    #[error("signed prekey lifecycle generation failed")]
    SignedPrekey(#[source] SignedPrekeyError),
    #[error("signed prekey stored material is invalid")]
    InvalidStoredPrekey(#[source] SignedPrekeyValidationError),
    #[error("signed prekey private material is invalid")]
    Prekey(#[source] X25519PrekeySerializationError),
    #[error("signed prekey serialized material is invalid")]
    InvalidSerialization,
    #[error("signed prekey secret cannot be stored")]
    KeystoreSecret(#[source] KeystoreSecretError),
    #[error("signed prekey keystore operation failed")]
    Keystore,
}

fn signed_prekey_entry() -> Result<KeystoreEntryName, SignedPrekeyLifecycleError> {
    KeystoreEntryName::new(SIGNED_PREKEY_KEY_ENTRY.to_owned())
        .map_err(|_| SignedPrekeyLifecycleError::InvalidKeyEntry)
}

fn persist<K: OsKeystore>(
    keystore: &mut K,
    entry: &KeystoreEntryName,
    signed_prekey: &SignedPrekey,
) -> Result<(), SignedPrekeyLifecycleError> {
    let secret = KeystoreSecret::new(serialize(signed_prekey)?)
        .map_err(SignedPrekeyLifecycleError::KeystoreSecret)?;
    keystore
        .store(entry, &secret)
        .map_err(|_| SignedPrekeyLifecycleError::Keystore)
}

fn serialize(signed_prekey: &SignedPrekey) -> Result<Vec<u8>, SignedPrekeyLifecycleError> {
    let public = signed_prekey.public();
    let serialized_prekey = signed_prekey.prekey().serialize();
    let mut encoded = Vec::with_capacity(SIGNED_PREKEY_SERIALIZED_BYTES);
    encoded.push(SIGNED_PREKEY_SERIALIZATION_VERSION);
    encoded.extend_from_slice(&public.generation().to_be_bytes());
    encoded.extend_from_slice(serialized_prekey.as_ref());
    encoded.extend_from_slice(public.signature());
    if encoded.len() != SIGNED_PREKEY_SERIALIZED_BYTES {
        return Err(SignedPrekeyLifecycleError::InvalidSerialization);
    }
    Ok(encoded)
}

fn deserialize(
    identity: &IdentityKeypair,
    encoded: &[u8],
) -> Result<SignedPrekey, SignedPrekeyLifecycleError> {
    if encoded.len() != SIGNED_PREKEY_SERIALIZED_BYTES
        || encoded.first() != Some(&SIGNED_PREKEY_SERIALIZATION_VERSION)
    {
        return Err(SignedPrekeyLifecycleError::InvalidSerialization);
    }
    let generation = u64::from_be_bytes(
        encoded[1..9]
            .try_into()
            .map_err(|_| SignedPrekeyLifecycleError::InvalidSerialization)?,
    );
    let prekey_end = 9 + X25519_PREKEY_SERIALIZED_BYTES;
    let prekey = X25519Prekey::deserialize(&encoded[9..prekey_end])
        .map_err(SignedPrekeyLifecycleError::Prekey)?;
    let signature: [u8; ED25519_SIGNATURE_BYTES] = encoded[prekey_end..]
        .try_into()
        .map_err(|_| SignedPrekeyLifecycleError::InvalidSerialization)?;
    SignedPrekey::from_parts(&identity.public_key(), generation, prekey, signature)
        .map_err(SignedPrekeyLifecycleError::InvalidStoredPrekey)
}
