use std::{
    collections::BTreeMap,
    convert::Infallible,
    fs,
    future::{Future, ready},
    path::PathBuf,
    sync::{
        Arc,
        atomic::{AtomicU64, AtomicUsize, Ordering},
    },
    time::Duration,
};

use minicbor::Encoder;
use yeokcham_core::{IdentityKeypair, KeystoreEntryName, KeystoreSecret, OsKeystore};
use yeokcham_daemon::{
    BackgroundDeliveryScheduler, DeliverySchedulerOutcome, DeliveryState, DeliveryTransport,
    EncryptedStateStore, MAX_DELIVERY_ATTEMPTS, MessageExpiry, OUTBOX_STATE_SCHEMA_VERSION,
    OutboxMessage, SenderOutbox, SenderOutboxError, StateDocument,
};
use yeokcham_protocol::{DeliveryAcknowledgement, EncryptedMessageEnvelope};

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

struct FailingTransport {
    calls: Arc<AtomicUsize>,
}

impl DeliveryTransport for FailingTransport {
    type Error = ();

    fn deliver(
        &mut self,
        _message: &OutboxMessage,
    ) -> impl Future<Output = Result<DeliveryAcknowledgement, Self::Error>> + Send {
        self.calls.fetch_add(1, Ordering::Relaxed);
        ready(Err(()))
    }
}

fn database_path() -> PathBuf {
    let number = NEXT_TEST_PATH.fetch_add(1, Ordering::Relaxed);
    std::env::temp_dir()
        .join(format!(
            "yeokcham-delivery-retry-scheduler-{}-{number}",
            std::process::id()
        ))
        .join("outbox.sqlite")
}

fn envelope(value: u8) -> EncryptedMessageEnvelope {
    EncryptedMessageEnvelope::new(vec![0xa1, value], vec![0xb2, value]).unwrap()
}

#[tokio::test]
async fn persists_bounded_attempts_then_advances_past_an_exhausted_message() {
    let path = database_path();
    fs::create_dir(path.parent().unwrap()).unwrap();
    let mut keystore = MemoryKeystore::default();
    let recipient = IdentityKeypair::generate().unwrap().public_key();
    let calls = Arc::new(AtomicUsize::new(0));
    let mut transport = FailingTransport {
        calls: Arc::clone(&calls),
    };
    let scheduler = BackgroundDeliveryScheduler::new(Duration::from_secs(1)).unwrap();
    let (first_identifier, second_identifier) = {
        let mut outbox = SenderOutbox::open(&path, &mut keystore).unwrap();
        outbox
            .enqueue(recipient, envelope(1), MessageExpiry::new(100, 60).unwrap())
            .unwrap();
        outbox
            .enqueue(recipient, envelope(2), MessageExpiry::new(100, 60).unwrap())
            .unwrap();
        let first_identifier = outbox.messages()[0].identifier();
        let second_identifier = outbox.messages()[1].identifier();
        let cycle = scheduler
            .run_cycle(&mut outbox, &mut transport, 101)
            .await
            .unwrap();
        assert_eq!(
            cycle.outcome(),
            DeliverySchedulerOutcome::Retrying(first_identifier)
        );
        assert_eq!(outbox.next().unwrap().delivery_attempts(), 1);
        (first_identifier, second_identifier)
    };
    let mut outbox = SenderOutbox::open(&path, &mut keystore).unwrap();
    assert_eq!(
        outbox.delivery_state(first_identifier),
        Some(DeliveryState::Attempted)
    );
    for attempt in 2..=MAX_DELIVERY_ATTEMPTS {
        let cycle = scheduler
            .run_cycle(&mut outbox, &mut transport, 101)
            .await
            .unwrap();
        let expected = if attempt == MAX_DELIVERY_ATTEMPTS {
            DeliverySchedulerOutcome::Failed(first_identifier)
        } else {
            DeliverySchedulerOutcome::Retrying(first_identifier)
        };
        assert_eq!(cycle.outcome(), expected);
    }
    assert_eq!(
        calls.load(Ordering::Relaxed),
        usize::from(MAX_DELIVERY_ATTEMPTS)
    );
    assert_eq!(
        outbox.delivery_state(first_identifier),
        Some(DeliveryState::Failed)
    );
    assert_eq!(outbox.next().unwrap().identifier(), second_identifier);
    let cycle = scheduler
        .run_cycle(&mut outbox, &mut transport, 101)
        .await
        .unwrap();
    assert_eq!(
        cycle.outcome(),
        DeliverySchedulerOutcome::Retrying(second_identifier)
    );
    assert_eq!(outbox.next().unwrap().delivery_attempts(), 1);
    drop(outbox);
    let outbox = SenderOutbox::open(&path, &mut keystore).unwrap();
    assert_eq!(
        outbox.delivery_state(first_identifier),
        Some(DeliveryState::Failed)
    );
    assert_eq!(outbox.next().unwrap().identifier(), second_identifier);
    assert_eq!(outbox.next().unwrap().delivery_attempts(), 1);
    drop(outbox);
    fs::remove_dir_all(path.parent().unwrap()).unwrap();
}

#[test]
fn fails_closed_for_a_persisted_attempt_count_above_the_limit() {
    let path = database_path();
    fs::create_dir(path.parent().unwrap()).unwrap();
    let mut keystore = MemoryKeystore::default();
    let recipient = IdentityKeypair::generate().unwrap().public_key();
    let envelope = envelope(1);
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(3)
        .unwrap()
        .u8(OUTBOX_STATE_SCHEMA_VERSION)
        .unwrap()
        .array(1)
        .unwrap()
        .array(6)
        .unwrap()
        .bytes(&[1; 16])
        .unwrap()
        .bytes(recipient.as_bytes())
        .unwrap()
        .bytes(&envelope.encode().unwrap())
        .unwrap()
        .u64(100)
        .unwrap()
        .u32(60)
        .unwrap()
        .u8(MAX_DELIVERY_ATTEMPTS + 1)
        .unwrap()
        .array(0)
        .unwrap();
    EncryptedStateStore::open(&path, &mut keystore)
        .unwrap()
        .replace(&StateDocument::new(encoder.into_writer()).unwrap())
        .unwrap();
    assert!(matches!(
        SenderOutbox::open(&path, &mut keystore),
        Err(SenderOutboxError::InvalidDeliveryAttempts)
    ));
    fs::remove_dir_all(path.parent().unwrap()).unwrap();
}

#[test]
fn migrates_v4_pending_messages_with_zero_delivery_attempts() {
    let path = database_path();
    fs::create_dir(path.parent().unwrap()).unwrap();
    let mut keystore = MemoryKeystore::default();
    let recipient = IdentityKeypair::generate().unwrap().public_key();
    let envelope = envelope(1);
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(3)
        .unwrap()
        .u8(4)
        .unwrap()
        .array(1)
        .unwrap()
        .array(5)
        .unwrap()
        .bytes(&[1; 16])
        .unwrap()
        .bytes(recipient.as_bytes())
        .unwrap()
        .bytes(&envelope.encode().unwrap())
        .unwrap()
        .u64(100)
        .unwrap()
        .u32(60)
        .unwrap()
        .array(0)
        .unwrap();
    EncryptedStateStore::open(&path, &mut keystore)
        .unwrap()
        .replace(&StateDocument::new(encoder.into_writer()).unwrap())
        .unwrap();
    let outbox = SenderOutbox::open(&path, &mut keystore).unwrap();
    assert_eq!(outbox.messages().len(), 1);
    assert_eq!(outbox.next().unwrap().delivery_attempts(), 0);
    drop(outbox);
    let state = EncryptedStateStore::open(&path, &mut keystore).unwrap();
    assert_eq!(
        state.load().unwrap().unwrap().as_bytes()[1],
        OUTBOX_STATE_SCHEMA_VERSION
    );
    drop(state);
    fs::remove_dir_all(path.parent().unwrap()).unwrap();
}
