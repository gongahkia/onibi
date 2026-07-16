use std::{
    collections::{BTreeMap, BTreeSet},
    convert::Infallible,
    fs,
    path::PathBuf,
    sync::{
        Arc,
        atomic::{AtomicU64, Ordering},
    },
};

use tokio::sync::Mutex;
use yeokcham_core::{IdentityKeypair, KeystoreEntryName, KeystoreSecret, OsKeystore};
use yeokcham_daemon::{
    DeliveryState, InboxDeduplicationResult, MessageExpiry, RecipientInboxDeduplication,
    SenderOutbox,
};
use yeokcham_protocol::{DeliveryAcknowledgement, EncryptedMessageEnvelope};

const CONCURRENT_MESSAGES: u8 = 64;
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

fn database_path(name: &str) -> PathBuf {
    let number = NEXT_TEST_PATH.fetch_add(1, Ordering::Relaxed);
    std::env::temp_dir().join(format!(
        "yeokcham-concurrent-{name}-{}-{number}.sqlite",
        std::process::id()
    ))
}

fn envelope(index: u8) -> EncryptedMessageEnvelope {
    EncryptedMessageEnvelope::new(vec![0xa1, index], vec![0xb2, index]).unwrap()
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn concurrent_delivery_acknowledgements_and_duplicates_are_exactly_once() {
    let outbox_path = database_path("outbox");
    let inbox_path = database_path("inbox");
    let mut outbox_keystore = MemoryKeystore::default();
    let mut inbox_keystore = MemoryKeystore::default();
    let recipient = IdentityKeypair::generate().unwrap();
    let mut initial_outbox = SenderOutbox::open(&outbox_path, &mut outbox_keystore).unwrap();
    let mut envelopes = Vec::with_capacity(usize::from(CONCURRENT_MESSAGES));
    let mut acknowledgements = Vec::with_capacity(usize::from(CONCURRENT_MESSAGES));
    let mut identifiers = Vec::with_capacity(usize::from(CONCURRENT_MESSAGES));
    for index in 0..CONCURRENT_MESSAGES {
        let envelope = envelope(index);
        initial_outbox
            .enqueue(
                recipient.public_key(),
                envelope.clone(),
                MessageExpiry::new(100, 3_600).unwrap(),
            )
            .unwrap();
        let identifier = initial_outbox.messages().last().unwrap().identifier();
        acknowledgements
            .push(DeliveryAcknowledgement::create(&recipient, identifier, 101).unwrap());
        identifiers.push(identifier);
        envelopes.push(envelope);
    }
    let outbox = Arc::new(Mutex::new(initial_outbox));
    let inbox = Arc::new(Mutex::new(
        RecipientInboxDeduplication::open(&inbox_path, &mut inbox_keystore).unwrap(),
    ));
    let mut deliveries = Vec::with_capacity(usize::from(CONCURRENT_MESSAGES) * 2);
    for (envelope, acknowledgement) in envelopes.iter().cloned().zip(acknowledgements) {
        let inbox = Arc::clone(&inbox);
        let outbox = Arc::clone(&outbox);
        deliveries.push(tokio::spawn(async move {
            tokio::task::yield_now().await;
            let deduplication = { inbox.lock().await.record(&envelope).unwrap() };
            let identifier = {
                outbox
                    .lock()
                    .await
                    .acknowledge_delivery(&acknowledgement)
                    .unwrap()
                    .identifier()
            };
            (deduplication, Some(identifier))
        }));
    }
    for envelope in envelopes {
        let inbox = Arc::clone(&inbox);
        deliveries.push(tokio::spawn(async move {
            tokio::task::yield_now().await;
            let deduplication = inbox.lock().await.record(&envelope).unwrap();
            (deduplication, None)
        }));
    }

    let mut accepted = 0;
    let mut duplicates = 0;
    let mut delivered = BTreeSet::new();
    for delivery in deliveries {
        let (deduplication, identifier) = delivery.await.unwrap();
        match deduplication {
            InboxDeduplicationResult::Accepted => accepted += 1,
            InboxDeduplicationResult::Duplicate => duplicates += 1,
        }
        if let Some(identifier) = identifier {
            assert!(delivered.insert(identifier));
        }
    }
    assert_eq!(accepted, usize::from(CONCURRENT_MESSAGES));
    assert_eq!(duplicates, usize::from(CONCURRENT_MESSAGES));
    assert_eq!(delivered.len(), usize::from(CONCURRENT_MESSAGES));
    assert_eq!(inbox.lock().await.len(), usize::from(CONCURRENT_MESSAGES));
    let outbox_guard = outbox.lock().await;
    assert!(outbox_guard.messages().is_empty());
    for identifier in identifiers {
        assert_eq!(
            outbox_guard.delivery_state(identifier),
            Some(DeliveryState::Delivered)
        );
    }
    drop(outbox_guard);
    drop(inbox);
    drop(outbox);
    fs::remove_file(inbox_path).unwrap();
    fs::remove_file(outbox_path).unwrap();
}
