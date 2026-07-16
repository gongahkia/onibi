use tokio::sync::broadcast;
use yeokcham_daemon::{DaemonLifecycleError, DaemonRuntime};
use yeokcham_protocol::ProtocolVersion;

use crate::{RuntimeMode, SdkConfig, SdkEvent, SdkEventEnvelope};

pub struct SdkClient {
    runtime: DaemonRuntime,
    events: broadcast::Sender<SdkEventEnvelope>,
    next_event_sequence: u64,
}

impl SdkClient {
    pub fn start(config: &SdkConfig) -> Result<Self, SdkClientError> {
        if !matches!(config.runtime_mode(), RuntimeMode::Embedded) {
            return Err(SdkClientError::DaemonModeUnavailable);
        }
        let runtime = DaemonRuntime::start(ProtocolVersion::INITIAL, config.state_directory())?;
        let (events, _) = broadcast::channel(config.event_buffer_capacity());
        let mut client = Self {
            runtime,
            events,
            next_event_sequence: 1,
        };
        client.emit(SdkEvent::ClientStarted)?;
        Ok(client)
    }

    #[must_use]
    pub fn subscribe(&self) -> broadcast::Receiver<SdkEventEnvelope> {
        self.events.subscribe()
    }

    #[must_use]
    pub const fn is_running(&self) -> bool {
        self.runtime.is_running()
    }

    pub fn shutdown(&mut self) -> Result<(), SdkClientError> {
        self.runtime.shutdown()?;
        self.emit(SdkEvent::ClientStopped)
    }

    fn emit(&mut self, event: SdkEvent) -> Result<(), SdkClientError> {
        let envelope = SdkEventEnvelope::new(self.next_event_sequence, event)
            .map_err(|_| SdkClientError::EventSequenceExhausted)?;
        self.next_event_sequence = self
            .next_event_sequence
            .checked_add(1)
            .ok_or(SdkClientError::EventSequenceExhausted)?;
        let _ = self.events.send(envelope);
        Ok(())
    }
}

#[derive(Debug, thiserror::Error)]
pub enum SdkClientError {
    #[error("daemon SDK mode is not available yet")]
    DaemonModeUnavailable,
    #[error("SDK client lifecycle operation failed")]
    Lifecycle(#[from] DaemonLifecycleError),
    #[error("SDK event sequence is exhausted")]
    EventSequenceExhausted,
}

#[cfg(test)]
mod tests {
    use std::{
        path::PathBuf,
        sync::atomic::{AtomicU64, Ordering},
    };

    use super::{SdkClient, SdkClientError};
    use crate::{LocalDaemonEndpoint, RuntimeMode, SdkConfig, SdkEvent};

    static NEXT_TEST_PATH: AtomicU64 = AtomicU64::new(0);

    fn state_directory() -> PathBuf {
        std::env::temp_dir().join(format!(
            "yeokcham-sdk-client-{}-{}",
            std::process::id(),
            NEXT_TEST_PATH.fetch_add(1, Ordering::Relaxed)
        ))
    }

    fn embedded_config(state_directory: PathBuf) -> SdkConfig {
        SdkConfig::new(state_directory, RuntimeMode::Embedded, 8).unwrap()
    }

    #[test]
    fn embedded_client_owns_its_state_directory_until_shutdown() {
        let state_directory = state_directory();
        let config = embedded_config(state_directory.clone());
        let mut client = SdkClient::start(&config).unwrap();
        assert!(client.is_running());
        let second_config = embedded_config(state_directory.clone());
        assert!(matches!(
            SdkClient::start(&second_config),
            Err(SdkClientError::Lifecycle(_))
        ));
        client.shutdown().unwrap();
        assert!(!client.is_running());
        let restarted_config = embedded_config(state_directory.clone());
        let restarted = SdkClient::start(&restarted_config).unwrap();
        drop(restarted);
        std::fs::remove_dir_all(state_directory).unwrap();
    }

    #[test]
    fn embedded_client_emits_lifecycle_events_to_active_subscribers() {
        let state_directory = state_directory();
        let config = embedded_config(state_directory.clone());
        let mut client = SdkClient::start(&config).unwrap();
        let mut events = client.subscribe();
        client.shutdown().unwrap();
        let event = events.try_recv().unwrap();
        assert_eq!(event.sequence(), 2);
        assert_eq!(event.event(), SdkEvent::ClientStopped);
        std::fs::remove_dir_all(state_directory).unwrap();
    }

    #[test]
    fn daemon_mode_is_rejected_before_the_grpc_client_exists() {
        let endpoint = LocalDaemonEndpoint::new("/tmp/yeokcham.sock".to_owned()).unwrap();
        let config = SdkConfig::new(state_directory(), RuntimeMode::Daemon(endpoint), 8).unwrap();
        assert!(matches!(
            SdkClient::start(&config),
            Err(SdkClientError::DaemonModeUnavailable)
        ));
    }
}
