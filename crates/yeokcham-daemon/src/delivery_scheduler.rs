use std::{
    future::Future,
    time::{Duration, SystemTime, UNIX_EPOCH},
};

use tokio::{
    sync::watch,
    task::JoinHandle,
    time::{MissedTickBehavior, interval},
};
use yeokcham_protocol::{DeliveryAcknowledgement, MessageIdentifier};

use crate::{OutboxMessage, SenderOutbox, SenderOutboxError};

pub const MIN_DELIVERY_SCHEDULE_INTERVAL: Duration = Duration::from_secs(1);
pub const MAX_DELIVERY_SCHEDULE_INTERVAL: Duration = Duration::from_secs(3600);

pub trait DeliveryTransport: Send + 'static {
    type Error: Send + 'static;

    fn deliver(
        &mut self,
        message: &OutboxMessage,
    ) -> impl Future<Output = Result<DeliveryAcknowledgement, Self::Error>> + Send;
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum DeliverySchedulerOutcome {
    Idle,
    Retrying(MessageIdentifier),
    Delivered(MessageIdentifier),
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct DeliverySchedulerCycle {
    expired: usize,
    outcome: DeliverySchedulerOutcome,
}

impl DeliverySchedulerCycle {
    #[must_use]
    pub const fn expired(self) -> usize {
        self.expired
    }

    #[must_use]
    pub const fn outcome(self) -> DeliverySchedulerOutcome {
        self.outcome
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct BackgroundDeliveryScheduler {
    interval: Duration,
}

impl BackgroundDeliveryScheduler {
    pub fn new(interval: Duration) -> Result<Self, DeliverySchedulerError> {
        if !(MIN_DELIVERY_SCHEDULE_INTERVAL..=MAX_DELIVERY_SCHEDULE_INTERVAL).contains(&interval) {
            return Err(DeliverySchedulerError::InvalidInterval);
        }
        Ok(Self { interval })
    }

    #[must_use]
    pub const fn interval(self) -> Duration {
        self.interval
    }

    pub async fn run_cycle<T: DeliveryTransport>(
        &self,
        outbox: &mut SenderOutbox,
        transport: &mut T,
        now: u64,
    ) -> Result<DeliverySchedulerCycle, DeliverySchedulerError> {
        let expired = outbox.expire_due_deliveries(now)?.len();
        let Some(message) = outbox.next().cloned() else {
            return Ok(DeliverySchedulerCycle {
                expired,
                outcome: DeliverySchedulerOutcome::Idle,
            });
        };
        let identifier = message.identifier();
        let outcome = match transport.deliver(&message).await {
            Ok(acknowledgement) => match outbox.acknowledge_delivery(&acknowledgement) {
                Ok(_) => DeliverySchedulerOutcome::Delivered(identifier),
                Err(SenderOutboxError::Acknowledgement(_))
                | Err(SenderOutboxError::UnknownMessageIdentifier) => {
                    DeliverySchedulerOutcome::Retrying(identifier)
                }
                Err(error) => return Err(error.into()),
            },
            Err(_) => DeliverySchedulerOutcome::Retrying(identifier),
        };
        Ok(DeliverySchedulerCycle { expired, outcome })
    }

    pub fn spawn<T: DeliveryTransport>(
        self,
        mut outbox: SenderOutbox,
        mut transport: T,
        mut shutdown: watch::Receiver<bool>,
    ) -> JoinHandle<Result<(), DeliverySchedulerError>> {
        tokio::spawn(async move {
            if *shutdown.borrow() {
                return Ok(());
            }
            let mut ticker = interval(self.interval);
            ticker.set_missed_tick_behavior(MissedTickBehavior::Skip);
            loop {
                tokio::select! {
                    changed = shutdown.changed() => {
                        if changed.is_err() || *shutdown.borrow() {
                            return Ok(());
                        }
                    }
                    _ = ticker.tick() => {
                        self.run_cycle(&mut outbox, &mut transport, unix_seconds()?).await?;
                    }
                }
            }
        })
    }
}

#[derive(Debug, thiserror::Error)]
pub enum DeliverySchedulerError {
    #[error("delivery schedule interval is outside the supported bounds")]
    InvalidInterval,
    #[error("system clock is before the Unix epoch")]
    ClockBeforeUnixEpoch,
    #[error("sender outbox operation failed: {0}")]
    Outbox(#[from] SenderOutboxError),
}

fn unix_seconds() -> Result<u64, DeliverySchedulerError> {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|duration| duration.as_secs())
        .map_err(|_| DeliverySchedulerError::ClockBeforeUnixEpoch)
}

#[cfg(test)]
mod tests {
    use std::{
        collections::BTreeMap,
        convert::Infallible,
        future::ready,
        path::PathBuf,
        sync::{
            Arc,
            atomic::{AtomicUsize, Ordering},
        },
        time::{Duration, SystemTime, UNIX_EPOCH},
    };

    use tokio::{sync::watch, time::timeout};
    use yeokcham_core::{IdentityKeypair, KeystoreEntryName, KeystoreSecret, OsKeystore};
    use yeokcham_protocol::{DeliveryAcknowledgement, EncryptedMessageEnvelope};

    use super::{
        BackgroundDeliveryScheduler, DeliverySchedulerError, DeliverySchedulerOutcome,
        DeliveryTransport, MAX_DELIVERY_SCHEDULE_INTERVAL, MIN_DELIVERY_SCHEDULE_INTERVAL,
    };
    use crate::{DeliveryState, MessageExpiry, OutboxMessage, SenderOutbox};

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

    struct AcknowledgingTransport {
        recipient: IdentityKeypair,
    }

    impl DeliveryTransport for AcknowledgingTransport {
        type Error = Infallible;

        fn deliver(
            &mut self,
            message: &OutboxMessage,
        ) -> impl Future<Output = Result<DeliveryAcknowledgement, Self::Error>> + Send {
            ready(Ok(DeliveryAcknowledgement::create(
                &self.recipient,
                message.identifier(),
                101,
            )
            .unwrap()))
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

    struct InvalidAcknowledgementTransport {
        recipient: IdentityKeypair,
    }

    impl DeliveryTransport for InvalidAcknowledgementTransport {
        type Error = Infallible;

        fn deliver(
            &mut self,
            message: &OutboxMessage,
        ) -> impl Future<Output = Result<DeliveryAcknowledgement, Self::Error>> + Send {
            ready(Ok(DeliveryAcknowledgement::create(
                &self.recipient,
                message.identifier(),
                101,
            )
            .unwrap()))
        }
    }

    fn path(name: &str) -> PathBuf {
        std::env::temp_dir().join(format!(
            "yeokcham-delivery-scheduler-{name}-{}.sqlite",
            std::process::id()
        ))
    }

    fn envelope() -> EncryptedMessageEnvelope {
        EncryptedMessageEnvelope::new(vec![0xa1], vec![0xb2]).unwrap()
    }

    fn expiry() -> MessageExpiry {
        MessageExpiry::new(100, 60).unwrap()
    }

    #[test]
    fn validates_bounded_delivery_schedule_intervals() {
        assert!(matches!(
            BackgroundDeliveryScheduler::new(Duration::ZERO),
            Err(DeliverySchedulerError::InvalidInterval)
        ));
        assert!(matches!(
            BackgroundDeliveryScheduler::new(
                MAX_DELIVERY_SCHEDULE_INTERVAL + Duration::from_secs(1)
            ),
            Err(DeliverySchedulerError::InvalidInterval)
        ));
        assert_eq!(
            BackgroundDeliveryScheduler::new(MIN_DELIVERY_SCHEDULE_INTERVAL)
                .unwrap()
                .interval(),
            MIN_DELIVERY_SCHEDULE_INTERVAL
        );
    }

    #[tokio::test]
    async fn verifies_acknowledgements_before_marking_delivery_complete() {
        let path = path("acknowledgement");
        let mut keystore = MemoryKeystore::default();
        let recipient = IdentityKeypair::generate().unwrap();
        let mut outbox = SenderOutbox::open(&path, &mut keystore).unwrap();
        outbox
            .enqueue(recipient.public_key(), envelope(), expiry())
            .unwrap();
        let identifier = outbox.next().unwrap().identifier();
        let mut transport = AcknowledgingTransport { recipient };
        let scheduler = BackgroundDeliveryScheduler::new(Duration::from_secs(1)).unwrap();

        let cycle = scheduler
            .run_cycle(&mut outbox, &mut transport, 101)
            .await
            .unwrap();
        assert_eq!(cycle.expired(), 0);
        assert_eq!(
            cycle.outcome(),
            DeliverySchedulerOutcome::Delivered(identifier)
        );
        assert_eq!(
            outbox.delivery_state(identifier),
            Some(DeliveryState::Delivered)
        );
        std::fs::remove_file(path).unwrap();
    }

    #[tokio::test]
    async fn retains_failed_delivery_for_the_next_tick() {
        let path = path("retry");
        let mut keystore = MemoryKeystore::default();
        let recipient = IdentityKeypair::generate().unwrap().public_key();
        let mut outbox = SenderOutbox::open(&path, &mut keystore).unwrap();
        outbox.enqueue(recipient, envelope(), expiry()).unwrap();
        let identifier = outbox.next().unwrap().identifier();
        let calls = Arc::new(AtomicUsize::new(0));
        let mut transport = FailingTransport {
            calls: Arc::clone(&calls),
        };
        let scheduler = BackgroundDeliveryScheduler::new(Duration::from_secs(1)).unwrap();

        let cycle = scheduler
            .run_cycle(&mut outbox, &mut transport, 101)
            .await
            .unwrap();
        assert_eq!(cycle.expired(), 0);
        assert_eq!(
            cycle.outcome(),
            DeliverySchedulerOutcome::Retrying(identifier)
        );
        assert_eq!(calls.load(Ordering::Relaxed), 1);
        assert_eq!(
            outbox.delivery_state(identifier),
            Some(DeliveryState::Unknown)
        );
        std::fs::remove_file(path).unwrap();
    }

    #[tokio::test]
    async fn retains_messages_when_the_delivery_acknowledgement_is_untrusted() {
        let path = path("invalid-acknowledgement");
        let mut keystore = MemoryKeystore::default();
        let recipient = IdentityKeypair::generate().unwrap();
        let unrelated_recipient = IdentityKeypair::generate().unwrap();
        let mut outbox = SenderOutbox::open(&path, &mut keystore).unwrap();
        outbox
            .enqueue(recipient.public_key(), envelope(), expiry())
            .unwrap();
        let identifier = outbox.next().unwrap().identifier();
        let mut transport = InvalidAcknowledgementTransport {
            recipient: unrelated_recipient,
        };
        let scheduler = BackgroundDeliveryScheduler::new(Duration::from_secs(1)).unwrap();

        let cycle = scheduler
            .run_cycle(&mut outbox, &mut transport, 101)
            .await
            .unwrap();
        assert_eq!(
            cycle.outcome(),
            DeliverySchedulerOutcome::Retrying(identifier)
        );
        assert_eq!(
            outbox.delivery_state(identifier),
            Some(DeliveryState::Unknown)
        );
        std::fs::remove_file(path).unwrap();
    }

    #[tokio::test]
    async fn expires_messages_before_attempting_delivery() {
        let path = path("expiry");
        let mut keystore = MemoryKeystore::default();
        let recipient = IdentityKeypair::generate().unwrap().public_key();
        let mut outbox = SenderOutbox::open(&path, &mut keystore).unwrap();
        outbox.enqueue(recipient, envelope(), expiry()).unwrap();
        let identifier = outbox.next().unwrap().identifier();
        let calls = Arc::new(AtomicUsize::new(0));
        let mut transport = FailingTransport {
            calls: Arc::clone(&calls),
        };
        let scheduler = BackgroundDeliveryScheduler::new(Duration::from_secs(1)).unwrap();

        let cycle = scheduler
            .run_cycle(&mut outbox, &mut transport, 160)
            .await
            .unwrap();
        assert_eq!(cycle.expired(), 1);
        assert_eq!(cycle.outcome(), DeliverySchedulerOutcome::Idle);
        assert_eq!(calls.load(Ordering::Relaxed), 0);
        assert_eq!(
            outbox.delivery_state(identifier),
            Some(DeliveryState::Expired)
        );
        std::fs::remove_file(path).unwrap();
    }

    #[tokio::test]
    async fn background_task_attempts_delivery_and_stops_on_shutdown() {
        let path = path("background");
        let mut keystore = MemoryKeystore::default();
        let recipient = IdentityKeypair::generate().unwrap().public_key();
        let mut outbox = SenderOutbox::open(&path, &mut keystore).unwrap();
        let now = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_secs();
        outbox
            .enqueue(recipient, envelope(), MessageExpiry::new(now, 60).unwrap())
            .unwrap();
        let calls = Arc::new(AtomicUsize::new(0));
        let transport = FailingTransport {
            calls: Arc::clone(&calls),
        };
        let (shutdown_sender, shutdown_receiver) = watch::channel(false);
        let scheduler = BackgroundDeliveryScheduler::new(Duration::from_secs(1)).unwrap();
        let task = scheduler.spawn(outbox, transport, shutdown_receiver);

        timeout(Duration::from_secs(1), async {
            while calls.load(Ordering::Relaxed) == 0 {
                tokio::task::yield_now().await;
            }
        })
        .await
        .unwrap();
        shutdown_sender.send(true).unwrap();
        timeout(Duration::from_secs(1), task)
            .await
            .unwrap()
            .unwrap()
            .unwrap();
        std::fs::remove_file(path).unwrap();
    }
}
