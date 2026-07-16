use yeokcham_protocol::MessageIdentifier;

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
    sequence: u64,
    event: SdkEvent,
}

impl SdkEventEnvelope {
    pub fn new(sequence: u64, event: SdkEvent) -> Result<Self, SdkEventError> {
        if sequence == 0 {
            return Err(SdkEventError::ZeroSequence);
        }
        Ok(Self { sequence, event })
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
    #[error("SDK event sequence must be nonzero")]
    ZeroSequence,
}

#[cfg(test)]
mod tests {
    use yeokcham_protocol::MessageIdentifier;

    use super::{SdkEvent, SdkEventEnvelope, SdkEventError};

    #[test]
    fn event_envelopes_are_ordered_and_reject_zero_sequences() {
        let identifier = MessageIdentifier::generate().unwrap();
        let event = SdkEventEnvelope::new(1, SdkEvent::MessageQueued(identifier)).unwrap();
        assert_eq!(event.sequence(), 1);
        assert_eq!(event.event(), SdkEvent::MessageQueued(identifier));
        assert_eq!(
            SdkEventEnvelope::new(0, SdkEvent::ClientStarted),
            Err(SdkEventError::ZeroSequence)
        );
    }
}
