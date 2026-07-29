use std::{
    collections::BTreeMap,
    convert::Infallible,
    fs,
    path::PathBuf,
    sync::atomic::{AtomicU64, Ordering},
};

use arachne_core::{IdentityKeypair, KeystoreEntryName, KeystoreSecret, OsKeystore};
use arachne_daemon::{
    DeliveryState, MAX_MESSAGE_EXPIRY_SECONDS, MessageExpiry, SenderOutbox, SenderOutboxError,
};
use arachne_protocol::{DeliveryAcknowledgement, EncryptedMessageEnvelope};

static NEXT_TEST_PATH: AtomicU64 = AtomicU64::new(0);

#[derive(Default)]
struct MemoryKeystore(BTreeMap<String, Vec<u8>>);

impl OsKeystore for MemoryKeystore {
    type Error = Infallible;

    fn load(&self, entry: &KeystoreEntryName) -> Result<Option<KeystoreSecret>, Self::Error> {
        Ok(self
            .0
            .get(entry.as_str())
            .map(|secret| KeystoreSecret::new(secret.clone()).unwrap()))
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

fn database_path() -> PathBuf {
    let number = NEXT_TEST_PATH.fetch_add(1, Ordering::Relaxed);
    std::env::temp_dir()
        .join(format!(
            "arachne-outbox-lifecycle-pipeline-{}-{number}",
            std::process::id()
        ))
        .join("outbox.sqlite")
}

fn envelope(value: u8) -> EncryptedMessageEnvelope {
    EncryptedMessageEnvelope::new(vec![0xa1, value], vec![0xb2, value]).unwrap()
}

#[test]
fn persists_delivered_expired_and_pending_outbox_lifecycle_states() {
    let path = database_path();
    fs::create_dir(path.parent().unwrap()).unwrap();
    let mut keystore = MemoryKeystore::default();
    let delivered_recipient = IdentityKeypair::generate().unwrap();
    let expired_recipient = IdentityKeypair::generate().unwrap();
    let pending_recipient = IdentityKeypair::generate().unwrap();
    let pending_envelope = envelope(3);
    let (delivered_identifier, expired_identifier, pending_identifier) = {
        let mut outbox = SenderOutbox::open(&path, &mut keystore).unwrap();
        outbox
            .enqueue(
                delivered_recipient.public_key(),
                envelope(1),
                MessageExpiry::new(100, 60).unwrap(),
            )
            .unwrap();
        outbox
            .enqueue(
                expired_recipient.public_key(),
                envelope(2),
                MessageExpiry::new(200, 1).unwrap(),
            )
            .unwrap();
        outbox
            .enqueue(
                pending_recipient.public_key(),
                pending_envelope.clone(),
                MessageExpiry::new(300, 60).unwrap(),
            )
            .unwrap();
        let delivered_identifier = outbox.messages()[0].identifier();
        let expired_identifier = outbox.messages()[1].identifier();
        let pending_identifier = outbox.messages()[2].identifier();
        let acknowledgement =
            DeliveryAcknowledgement::create(&delivered_recipient, delivered_identifier, 101)
                .unwrap();
        outbox.acknowledge_delivery(&acknowledgement).unwrap();
        assert_eq!(
            outbox.expire_due_deliveries(201).unwrap()[0].identifier(),
            expired_identifier
        );
        (delivered_identifier, expired_identifier, pending_identifier)
    };
    let outbox = SenderOutbox::open(&path, &mut keystore).unwrap();
    assert_eq!(outbox.messages().len(), 1);
    assert_eq!(outbox.next().unwrap().identifier(), pending_identifier);
    assert_eq!(outbox.next().unwrap().envelope(), &pending_envelope);
    assert_eq!(
        outbox.delivery_state(delivered_identifier),
        Some(DeliveryState::Delivered)
    );
    assert_eq!(
        outbox.delivery_state(expired_identifier),
        Some(DeliveryState::Expired)
    );
    assert_eq!(
        outbox.delivery_state(pending_identifier),
        Some(DeliveryState::Queued)
    );
    drop(outbox);
    fs::remove_dir_all(path.parent().unwrap()).unwrap();
}

#[test]
fn rejects_an_untrusted_acknowledgement_without_removing_the_message() {
    let path = database_path();
    fs::create_dir(path.parent().unwrap()).unwrap();
    let mut keystore = MemoryKeystore::default();
    let recipient = IdentityKeypair::generate().unwrap();
    let untrusted = IdentityKeypair::generate().unwrap();
    let identifier = {
        let mut outbox = SenderOutbox::open(&path, &mut keystore).unwrap();
        outbox
            .enqueue(
                recipient.public_key(),
                envelope(1),
                MessageExpiry::new(100, 60).unwrap(),
            )
            .unwrap();
        let identifier = outbox.next().unwrap().identifier();
        let acknowledgement = DeliveryAcknowledgement::create(&untrusted, identifier, 101).unwrap();
        assert!(matches!(
            outbox.acknowledge_delivery(&acknowledgement),
            Err(SenderOutboxError::Acknowledgement(_))
        ));
        assert_eq!(outbox.next().unwrap().identifier(), identifier);
        identifier
    };
    let outbox = SenderOutbox::open(&path, &mut keystore).unwrap();
    assert_eq!(outbox.next().unwrap().identifier(), identifier);
    assert_eq!(
        outbox.delivery_state(identifier),
        Some(DeliveryState::Queued)
    );
    drop(outbox);
    fs::remove_dir_all(path.parent().unwrap()).unwrap();
}

#[test]
fn expires_a_message_at_the_maximum_supported_ttl_boundary() {
    let path = database_path();
    fs::create_dir(path.parent().unwrap()).unwrap();
    let mut keystore = MemoryKeystore::default();
    let recipient = IdentityKeypair::generate().unwrap();
    let created_at = 100_u64;
    let expiry = MessageExpiry::new(created_at, MAX_MESSAGE_EXPIRY_SECONDS).unwrap();
    let identifier = {
        let mut outbox = SenderOutbox::open(&path, &mut keystore).unwrap();
        outbox
            .enqueue(recipient.public_key(), envelope(1), expiry)
            .unwrap();
        let identifier = outbox.next().unwrap().identifier();
        assert!(
            outbox
                .expire_due_deliveries(expiry.expires_at() - 1)
                .unwrap()
                .is_empty()
        );
        assert_eq!(
            outbox.expire_due_deliveries(expiry.expires_at()).unwrap()[0].identifier(),
            identifier
        );
        identifier
    };
    let outbox = SenderOutbox::open(&path, &mut keystore).unwrap();
    assert_eq!(
        outbox.delivery_state(identifier),
        Some(DeliveryState::Expired)
    );
    drop(outbox);
    fs::remove_dir_all(path.parent().unwrap()).unwrap();
}
