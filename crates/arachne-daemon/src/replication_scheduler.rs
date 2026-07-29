use std::collections::VecDeque;

use arachne_protocol::{RelayReplicaSelection, TorMaildropProfileConfig};

pub const MAX_REPLICA_WRITE_ATTEMPTS: u8 = 3;

#[derive(Clone, Copy)]
struct ReplicaTask {
    replica: TorMaildropProfileConfig,
    attempts: u8,
}

pub struct MaildropReplicationScheduler {
    pending: VecDeque<ReplicaTask>,
    in_flight: Option<ReplicaTask>,
}

impl MaildropReplicationScheduler {
    #[must_use]
    #[allow(clippy::needless_pass_by_value)]
    pub fn new(selection: RelayReplicaSelection) -> Self {
        Self {
            pending: selection
                .replicas()
                .iter()
                .copied()
                .map(|replica| ReplicaTask {
                    replica,
                    attempts: 0,
                })
                .collect(),
            in_flight: None,
        }
    }

    pub fn next_replica(
        &mut self,
    ) -> Result<Option<TorMaildropProfileConfig>, MaildropReplicationError> {
        if self.in_flight.is_some() {
            return Err(MaildropReplicationError::WriteInFlight);
        }
        let mut task = self.pending.pop_front();
        if let Some(task) = &mut task {
            task.attempts += 1;
        }
        self.in_flight = task;
        Ok(task.map(|task| task.replica))
    }

    pub fn complete(
        &mut self,
        replica: TorMaildropProfileConfig,
    ) -> Result<(), MaildropReplicationError> {
        if self.in_flight.map(|task| task.replica) != Some(replica) {
            return Err(MaildropReplicationError::UnexpectedReplica);
        }
        self.in_flight = None;
        Ok(())
    }

    pub fn fail(
        &mut self,
        replica: TorMaildropProfileConfig,
    ) -> Result<(), MaildropReplicationError> {
        let task = self
            .in_flight
            .ok_or(MaildropReplicationError::UnexpectedReplica)?;
        if task.replica != replica {
            return Err(MaildropReplicationError::UnexpectedReplica);
        }
        self.in_flight = None;
        if task.attempts >= MAX_REPLICA_WRITE_ATTEMPTS {
            return Err(MaildropReplicationError::AttemptsExhausted);
        }
        self.pending.push_front(task);
        Ok(())
    }

    #[must_use]
    pub fn is_complete(&self) -> bool {
        self.pending.is_empty() && self.in_flight.is_none()
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum MaildropReplicationError {
    #[error("a maildrop replica write is already in flight")]
    WriteInFlight,
    #[error("maildrop replica completion did not match the active write")]
    UnexpectedReplica,
    #[error("maildrop replica write attempts are exhausted")]
    AttemptsExhausted,
}

#[cfg(test)]
mod tests {
    use super::{
        MAX_REPLICA_WRITE_ATTEMPTS, MaildropReplicationError, MaildropReplicationScheduler,
    };
    use arachne_protocol::{
        DeliveryProfile, RelayReplicaSelection, TOR_ONION_SERVICE_PUBLIC_KEY_BYTES,
        TorMaildropProfileConfig,
    };

    fn replica(byte: u8) -> TorMaildropProfileConfig {
        TorMaildropProfileConfig::new([byte; TOR_ONION_SERVICE_PUBLIC_KEY_BYTES], 4444).unwrap()
    }

    #[test]
    fn schedules_each_explicit_replica_once_in_order() {
        let first = replica(0x11);
        let second = replica(0x22);
        let selection = RelayReplicaSelection::for_profile(
            DeliveryProfile::tor_maildrop(),
            vec![first, second],
        )
        .unwrap();
        let mut scheduler = MaildropReplicationScheduler::new(selection);

        assert_eq!(scheduler.next_replica(), Ok(Some(first)));
        assert_eq!(
            scheduler.next_replica(),
            Err(MaildropReplicationError::WriteInFlight)
        );
        assert_eq!(
            scheduler.complete(second),
            Err(MaildropReplicationError::UnexpectedReplica)
        );
        scheduler.complete(first).unwrap();
        assert_eq!(scheduler.next_replica(), Ok(Some(second)));
        scheduler.complete(second).unwrap();
        assert_eq!(scheduler.next_replica(), Ok(None));
        assert!(scheduler.is_complete());
    }

    #[test]
    fn retries_only_the_failed_replica_up_to_the_attempt_limit() {
        let first = replica(0x11);
        let selection =
            RelayReplicaSelection::for_profile(DeliveryProfile::tor_maildrop(), vec![first])
                .unwrap();
        let mut scheduler = MaildropReplicationScheduler::new(selection);

        for _ in 1..MAX_REPLICA_WRITE_ATTEMPTS {
            assert_eq!(scheduler.next_replica(), Ok(Some(first)));
            scheduler.fail(first).unwrap();
        }
        assert_eq!(scheduler.next_replica(), Ok(Some(first)));
        assert_eq!(
            scheduler.fail(first),
            Err(MaildropReplicationError::AttemptsExhausted)
        );
        assert!(scheduler.is_complete());
    }
}
