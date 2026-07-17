use tokio::sync::broadcast;
use tokio_util::sync::CancellationToken;
use yeokcham_protocol::MessageIdentifier;

use crate::SdkAsyncPolicy;

pub const SDK_EVENT_ENVELOPE_VERSION: u32 = 1;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum SdkEvent {
    ClientStarted,
    ClientStopped,
    MessageQueued(MessageIdentifier),
    MessageDelivered(MessageIdentifier),
    MessageDeliveryFailed(MessageIdentifier),
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct SdkEventEnvelope {
    version: u32,
    sequence: u64,
    event: SdkEvent,
}

impl SdkEventEnvelope {
    pub fn new(sequence: u64, event: SdkEvent) -> Result<Self, SdkEventError> {
        Self::with_version(SDK_EVENT_ENVELOPE_VERSION, sequence, event)
    }

    pub fn with_version(
        version: u32,
        sequence: u64,
        event: SdkEvent,
    ) -> Result<Self, SdkEventError> {
        if version != SDK_EVENT_ENVELOPE_VERSION {
            return Err(SdkEventError::UnsupportedVersion);
        }
        if sequence == 0 {
            return Err(SdkEventError::ZeroSequence);
        }
        Ok(Self {
            version,
            sequence,
            event,
        })
    }

    #[must_use]
    pub const fn version(self) -> u32 {
        self.version
    }

    #[must_use]
    pub const fn sequence(self) -> u64 {
        self.sequence
    }

    #[must_use]
    pub const fn event(self) -> SdkEvent {
        self.event
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, thiserror::Error)]
pub enum SdkEventError {
    #[error("SDK event envelope version is unsupported")]
    UnsupportedVersion,
    #[error("SDK event sequence must be nonzero")]
    ZeroSequence,
}

pub struct SdkEventStream {
    receiver: broadcast::Receiver<SdkEventEnvelope>,
}

impl SdkEventStream {
    pub(crate) const fn new(receiver: broadcast::Receiver<SdkEventEnvelope>) -> Self {
        Self { receiver }
    }

    pub async fn next(&mut self) -> Result<SdkEventEnvelope, SdkEventStreamError> {
        self.receiver
            .recv()
            .await
            .map_err(|error| map_receive_error(&error))
    }

    pub async fn next_with_policy(
        &mut self,
        policy: SdkAsyncPolicy,
        cancellation: &CancellationToken,
    ) -> Result<SdkEventEnvelope, SdkEventStreamError> {
        tokio::select! {
            biased;
            () = cancellation.cancelled() => Err(SdkEventStreamError::Cancelled),
            () = tokio::time::sleep(policy.deadline()) => Err(SdkEventStreamError::DeadlineExceeded),
            event = self.receiver.recv() => event.map_err(|error| map_receive_error(&error)),
        }
    }

    pub fn try_next(&mut self) -> Result<Option<SdkEventEnvelope>, SdkEventStreamError> {
        match self.receiver.try_recv() {
            Ok(event) => Ok(Some(event)),
            Err(broadcast::error::TryRecvError::Empty) => Ok(None),
            Err(broadcast::error::TryRecvError::Lagged(skipped)) => {
                Err(SdkEventStreamError::Lagged(skipped))
            }
            Err(broadcast::error::TryRecvError::Closed) => Err(SdkEventStreamError::Closed),
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, thiserror::Error)]
pub enum SdkEventStreamError {
    #[error("SDK event stream wait was cancelled")]
    Cancelled,
    #[error("SDK event stream wait exceeded its deadline")]
    DeadlineExceeded,
    #[error("SDK event stream lagged by {0} event(s)")]
    Lagged(u64),
    #[error("SDK event stream is closed")]
    Closed,
}

fn map_receive_error(error: &broadcast::error::RecvError) -> SdkEventStreamError {
    match error {
        broadcast::error::RecvError::Lagged(skipped) => SdkEventStreamError::Lagged(*skipped),
        broadcast::error::RecvError::Closed => SdkEventStreamError::Closed,
    }
}

#[cfg(test)]
mod tests {
    use yeokcham_protocol::MessageIdentifier;

    use super::{SDK_EVENT_ENVELOPE_VERSION, SdkEvent, SdkEventEnvelope, SdkEventError};

    #[test]
    fn event_envelopes_are_ordered_and_reject_zero_sequences() {
        let identifier = MessageIdentifier::generate().unwrap();
        let event = SdkEventEnvelope::new(1, SdkEvent::MessageQueued(identifier)).unwrap();
        assert_eq!(event.version(), SDK_EVENT_ENVELOPE_VERSION);
        assert_eq!(event.sequence(), 1);
        assert_eq!(event.event(), SdkEvent::MessageQueued(identifier));
        assert_eq!(
            SdkEventEnvelope::new(0, SdkEvent::ClientStarted),
            Err(SdkEventError::ZeroSequence)
        );
        assert_eq!(
            SdkEventEnvelope::with_version(0, 1, SdkEvent::ClientStarted),
            Err(SdkEventError::UnsupportedVersion)
        );
        assert_eq!(
            SdkEventEnvelope::with_version(
                SDK_EVENT_ENVELOPE_VERSION + 1,
                1,
                SdkEvent::ClientStarted
            ),
            Err(SdkEventError::UnsupportedVersion)
        );
    }
}
