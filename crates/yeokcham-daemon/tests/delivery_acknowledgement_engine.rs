use std::{
    collections::BTreeMap,
    convert::Infallible,
    fs,
    future::{Future, ready},
    path::PathBuf,
    sync::atomic::{AtomicU64, Ordering},
    time::Duration,
};

use yeokcham_core::{IdentityKeypair, KeystoreEntryName, KeystoreSecret, OsKeystore};
use yeokcham_daemon::{
    BackgroundDeliveryScheduler, DeliverySchedulerOutcome, DeliveryState, DeliveryTransport,
    MessageExpiry, OutboxMessage, SenderOutbox,
};
use yeokcham_protocol::{DeliveryAcknowledgement, EncryptedMessageEnvelope, MessageIdentifier};

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

struct SigningTransport {
    recipient: IdentityKeypair,
    calls: usize,
}

impl DeliveryTransport for SigningTransport {
    type Error = Infallible;

    fn deliver(
        &mut self,
        message: &OutboxMessage,
    ) -> impl Future<Output = Result<DeliveryAcknowledgement, Self::Error>> + Send {
        self.calls += 1;
        ready(Ok(DeliveryAcknowledgement::create(
            &self.recipient,
            message.identifier(),
            101,
        )
        .unwrap()))
    }
}

struct MismatchedAcknowledgementTransport {
    recipient: IdentityKeypair,
    identifier: MessageIdentifier,
}

impl DeliveryTransport for MismatchedAcknowledgementTransport {
    type Error = Infallible;

    fn deliver(
        &mut self,
        _message: &OutboxMessage,
    ) -> impl Future<Output = Result<DeliveryAcknowledgement, Self::Error>> + Send {
        ready(Ok(DeliveryAcknowledgement::create(
            &self.recipient,
            self.identifier,
            101,
        )
        .unwrap()))
    }
}

fn database_path() -> PathBuf {
    let number = NEXT_TEST_PATH.fetch_add(1, Ordering::Relaxed);
    std::env::temp_dir()
        .join(format!(
            "yeokcham-delivery-acknowledgement-engine-{}-{number}",
            std::process::id()
        ))
        .join("outbox.sqlite")
}

fn envelope(value: u8) -> EncryptedMessageEnvelope {
    EncryptedMessageEnvelope::new(vec![0xa1, value], vec![0xb2, value]).unwrap()
}

#[tokio::test]
async fn persists_a_verified_acknowledgement_after_a_scheduler_cycle() {
    let path = database_path();
    fs::create_dir(path.parent().unwrap()).unwrap();
    let mut keystore = MemoryKeystore::default();
    let recipient = IdentityKeypair::generate().unwrap();
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
        let mut transport = SigningTransport {
            recipient,
            calls: 0,
        };
        let cycle = BackgroundDeliveryScheduler::new(Duration::from_secs(1))
            .unwrap()
            .run_cycle(&mut outbox, &mut transport, 101)
            .await
            .unwrap();
        assert_eq!(
            cycle.outcome(),
            DeliverySchedulerOutcome::Delivered(identifier)
        );
        assert_eq!(transport.calls, 1);
        identifier
    };
    let outbox = SenderOutbox::open(&path, &mut keystore).unwrap();
    assert!(outbox.messages().is_empty());
    assert_eq!(
        outbox.delivery_state(identifier),
        Some(DeliveryState::Delivered)
    );
    drop(outbox);
    fs::remove_dir_all(path.parent().unwrap()).unwrap();
}

#[tokio::test]
async fn rejects_an_acknowledgement_for_a_different_queued_message() {
    let path = database_path();
    fs::create_dir(path.parent().unwrap()).unwrap();
    let mut keystore = MemoryKeystore::default();
    let recipient = IdentityKeypair::generate().unwrap();
    let (first_identifier, second_identifier) = {
        let mut outbox = SenderOutbox::open(&path, &mut keystore).unwrap();
        for value in [1, 2] {
            outbox
                .enqueue(
                    recipient.public_key(),
                    envelope(value),
                    MessageExpiry::new(100, 60).unwrap(),
                )
                .unwrap();
        }
        let first_identifier = outbox.messages()[0].identifier();
        let second_identifier = outbox.messages()[1].identifier();
        let mut transport = MismatchedAcknowledgementTransport {
            recipient,
            identifier: second_identifier,
        };
        let cycle = BackgroundDeliveryScheduler::new(Duration::from_secs(1))
            .unwrap()
            .run_cycle(&mut outbox, &mut transport, 101)
            .await
            .unwrap();
        assert_eq!(
            cycle.outcome(),
            DeliverySchedulerOutcome::Retrying(first_identifier)
        );
        assert_eq!(outbox.messages().len(), 2);
        assert_eq!(
            outbox.delivery_state(first_identifier),
            Some(DeliveryState::Attempted)
        );
        assert_eq!(
            outbox.delivery_state(second_identifier),
            Some(DeliveryState::Queued)
        );
        (first_identifier, second_identifier)
    };
    let outbox = SenderOutbox::open(&path, &mut keystore).unwrap();
    assert_eq!(outbox.messages().len(), 2);
    assert_eq!(
        outbox.delivery_state(first_identifier),
        Some(DeliveryState::Attempted)
    );
    assert_eq!(
        outbox.delivery_state(second_identifier),
        Some(DeliveryState::Queued)
    );
    drop(outbox);
    fs::remove_dir_all(path.parent().unwrap()).unwrap();
}

#[tokio::test]
async fn expires_at_the_exact_deadline_without_attempting_delivery() {
    let path = database_path();
    fs::create_dir(path.parent().unwrap()).unwrap();
    let mut keystore = MemoryKeystore::default();
    let recipient = IdentityKeypair::generate().unwrap();
    let expiry = MessageExpiry::new(100, 1).unwrap();
    let identifier = {
        let mut outbox = SenderOutbox::open(&path, &mut keystore).unwrap();
        outbox
            .enqueue(recipient.public_key(), envelope(1), expiry)
            .unwrap();
        let identifier = outbox.next().unwrap().identifier();
        let mut transport = SigningTransport {
            recipient,
            calls: 0,
        };
        let cycle = BackgroundDeliveryScheduler::new(Duration::from_secs(1))
            .unwrap()
            .run_cycle(&mut outbox, &mut transport, expiry.expires_at())
            .await
            .unwrap();
        assert_eq!(cycle.expired(), 1);
        assert_eq!(cycle.outcome(), DeliverySchedulerOutcome::Idle);
        assert_eq!(transport.calls, 0);
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
