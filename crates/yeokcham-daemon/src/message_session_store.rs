use std::path::Path;

use yeokcham_core::OsKeystore;
use yeokcham_protocol::{MessagePayload, RatchetMessageEnvelope, RatchetMessageSession};

use crate::{EncryptedStateStore, StateDocument, StateDocumentError, StateStoreError};

pub struct MessageSessionStore {
    state: EncryptedStateStore,
}

impl MessageSessionStore {
    pub fn open<K: OsKeystore>(
        path: &Path,
        keystore: &mut K,
    ) -> Result<Self, MessageSessionStoreError> {
        Ok(Self {
            state: EncryptedStateStore::open(path, keystore)?,
        })
    }

    pub fn load(&self) -> Result<Option<RatchetMessageSession>, MessageSessionStoreError> {
        self.state
            .load()?
            .map(|document| RatchetMessageSession::decode(document.as_bytes()))
            .transpose()
            .map_err(MessageSessionStoreError::Session)
    }

    pub fn replace(
        &mut self,
        session: &RatchetMessageSession,
    ) -> Result<(), MessageSessionStoreError> {
        let encoded = session
            .encode()
            .map_err(MessageSessionStoreError::Session)?;
        let document =
            StateDocument::from_zeroizing(encoded).map_err(MessageSessionStoreError::Document)?;
        self.state.replace(&document)?;
        Ok(())
    }

    pub fn encrypt(
        &mut self,
        payload: &MessagePayload,
    ) -> Result<RatchetMessageEnvelope, MessageSessionStoreError> {
        let mut session = self.load_required()?;
        let envelope = session
            .encrypt(payload)
            .map_err(MessageSessionStoreError::Session)?;
        self.replace(&session)?;
        Ok(envelope)
    }

    pub fn decrypt(
        &mut self,
        envelope: &RatchetMessageEnvelope,
    ) -> Result<MessagePayload, MessageSessionStoreError> {
        let mut session = self.load_required()?;
        let payload = session
            .decrypt(envelope)
            .map_err(MessageSessionStoreError::Session)?;
        self.replace(&session)?;
        Ok(payload)
    }

    fn load_required(&self) -> Result<RatchetMessageSession, MessageSessionStoreError> {
        self.load()?
            .ok_or(MessageSessionStoreError::SessionUnavailable)
    }
}

#[derive(Debug, thiserror::Error)]
pub enum MessageSessionStoreError {
    #[error("encrypted state-store operation failed: {0}")]
    StateStore(#[from] StateStoreError),
    #[error("ratchet-message session document is invalid: {0}")]
    Session(#[source] yeokcham_protocol::RatchetMessageError),
    #[error("ratchet-message session document violates storage bounds: {0}")]
    Document(#[source] StateDocumentError),
    #[error("ratchet-message session has not been initialized")]
    SessionUnavailable,
}

#[cfg(test)]
mod tests {
    use std::{collections::BTreeMap, convert::Infallible, fs, path::PathBuf};

    use yeokcham_core::{
        IdentityKeypair, KeystoreEntryName, KeystoreSecret, OsKeystore, X25519IdentityKeypair,
    };
    use yeokcham_protocol::{
        MessageContentType, MessagePayload, RatchetMessageSession, SignedPrekey, X3dhPrekeyBundle,
        X25519IdentityBinding, initiate_x3dh,
    };

    use super::MessageSessionStore;

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

    fn path() -> PathBuf {
        std::env::temp_dir().join(format!(
            "yeokcham-message-session-store-{}.sqlite",
            std::process::id()
        ))
    }

    fn session() -> RatchetMessageSession {
        let initiator_signing = IdentityKeypair::generate().unwrap();
        let initiator_exchange = X25519IdentityKeypair::generate().unwrap();
        let initiator_binding =
            X25519IdentityBinding::create(&initiator_signing, &initiator_exchange).unwrap();
        let responder_signing = IdentityKeypair::generate().unwrap();
        let responder_exchange = X25519IdentityKeypair::generate().unwrap();
        let signed_prekey = SignedPrekey::generate(&responder_signing).unwrap();
        let bundle = X3dhPrekeyBundle::create(
            &responder_signing,
            &responder_exchange,
            signed_prekey.public(),
            Vec::new(),
        )
        .unwrap();
        let (_, x3dh) = initiate_x3dh(&initiator_exchange, initiator_binding, &bundle).unwrap();
        RatchetMessageSession::initiate(&x3dh, *signed_prekey.public().prekey()).unwrap()
    }

    #[test]
    fn persists_encrypted_ratchet_message_session() {
        let path = path();
        let mut keystore = MemoryKeystore::default();
        let session = session();
        let mut store = MessageSessionStore::open(&path, &mut keystore).unwrap();
        store.replace(&session).unwrap();
        store
            .encrypt(&MessagePayload::new(MessageContentType::TextUtf8, b"first".to_vec()).unwrap())
            .unwrap();
        drop(store);
        let mut restored = MessageSessionStore::open(&path, &mut keystore)
            .unwrap()
            .load()
            .unwrap()
            .unwrap();
        assert!(
            restored
                .encrypt(
                    &MessagePayload::new(MessageContentType::TextUtf8, b"second".to_vec()).unwrap()
                )
                .is_ok()
        );
        fs::remove_file(path).unwrap();
    }
}
