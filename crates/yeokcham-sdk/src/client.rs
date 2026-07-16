use std::{fs, io, path::PathBuf};

use tokio::sync::broadcast;
use yeokcham_core::OsKeystore;
use yeokcham_daemon::{DaemonLifecycleError, DaemonRuntime, SenderOutbox};
use yeokcham_protocol::ProtocolVersion;

use crate::message::{map_delivery_state, map_outbox_error};
use crate::{
    RuntimeMode, SdkConfig, SdkContactError, SdkContactManager, SdkDeliveryStatus, SdkEvent,
    SdkEventEnvelope, SdkEventStream, SdkIdentityManager, SdkMessageError, SdkMessageIdentifier,
    SdkMessageSendRequest, SdkQueuedMessage,
};

pub struct SdkClient {
    runtime: DaemonRuntime,
    state_directory: PathBuf,
    events: broadcast::Sender<SdkEventEnvelope>,
    next_event_sequence: u64,
}

impl SdkClient {
    pub async fn start_async(config: &SdkConfig) -> Result<Self, SdkClientError> {
        let config = config.clone();
        tokio::task::spawn_blocking(move || Self::start(&config))
            .await
            .map_err(|_| SdkClientError::AsyncTask)?
    }

    pub fn start(config: &SdkConfig) -> Result<Self, SdkClientError> {
        config.validate()?;
        if !matches!(config.runtime_mode(), RuntimeMode::Embedded) {
            return Err(SdkClientError::DaemonModeUnavailable);
        }
        let runtime = DaemonRuntime::start(ProtocolVersion::INITIAL, config.state_directory())?;
        let (events, _) = broadcast::channel(config.event_buffer_capacity());
        let mut client = Self {
            runtime,
            state_directory: config.state_directory().to_path_buf(),
            events,
            next_event_sequence: 1,
        };
        client.emit(SdkEvent::ClientStarted)?;
        Ok(client)
    }

    #[must_use]
    pub fn subscribe(&self) -> SdkEventStream {
        SdkEventStream::new(self.events.subscribe())
    }

    #[must_use]
    pub const fn is_running(&self) -> bool {
        self.runtime.is_running()
    }

    pub fn shutdown(&mut self) -> Result<(), SdkClientError> {
        self.runtime.shutdown()?;
        self.emit(SdkEvent::ClientStopped)
    }

    pub fn contact_manager<K: OsKeystore>(
        &self,
        identity: &mut SdkIdentityManager<K>,
    ) -> Result<SdkContactManager<'_>, SdkContactError> {
        if !self.is_running() {
            return Err(SdkContactError::State);
        }
        SdkContactManager::open(self, identity)
    }

    pub fn send_message<K: OsKeystore>(
        &mut self,
        identity: &mut SdkIdentityManager<K>,
        request: SdkMessageSendRequest,
    ) -> Result<SdkQueuedMessage, SdkMessageError> {
        if !self.is_running() {
            return Err(SdkMessageError::ClientNotRunning);
        }
        if self.next_event_sequence == u64::MAX {
            return Err(SdkMessageError::EventSequenceExhausted);
        }
        let _ = identity.load()?;
        let mut outbox = SenderOutbox::open(&self.outbox_path(), identity.keystore_mut())
            .map_err(|error| map_outbox_error(&error))?;
        let (recipient, envelope, expiry) = request.into_parts();
        outbox
            .enqueue(recipient, envelope, expiry)
            .map_err(|error| map_outbox_error(&error))?;
        let message = outbox.messages().last().ok_or(SdkMessageError::State)?;
        let queued = SdkQueuedMessage::from(message);
        self.emit(SdkEvent::MessageQueued(message.identifier()))
            .map_err(|_| SdkMessageError::EventSequenceExhausted)?;
        Ok(queued)
    }

    pub fn delivery_status<K: OsKeystore>(
        &self,
        identity: &mut SdkIdentityManager<K>,
        identifier: SdkMessageIdentifier,
    ) -> Result<Option<SdkDeliveryStatus>, SdkMessageError> {
        if !self.is_running() {
            return Err(SdkMessageError::ClientNotRunning);
        }
        let _ = identity.load()?;
        let path = self.outbox_path();
        match fs::metadata(&path) {
            Ok(metadata) if metadata.is_file() => {}
            Err(error) if error.kind() == io::ErrorKind::NotFound => return Ok(None),
            Ok(_) | Err(_) => return Err(SdkMessageError::State),
        }
        let outbox = SenderOutbox::open(&path, identity.keystore_mut())
            .map_err(|error| map_outbox_error(&error))?;
        Ok(outbox
            .delivery_state(identifier.into_inner())
            .map(map_delivery_state))
    }

    pub async fn shutdown_async(mut self) -> Result<(), SdkClientError> {
        tokio::task::spawn_blocking(move || self.shutdown())
            .await
            .map_err(|_| SdkClientError::AsyncTask)?
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

    pub(crate) fn contacts_path(&self) -> PathBuf {
        self.state_directory
            .join(yeokcham_daemon::CONTACTS_DATABASE_FILE)
    }

    fn outbox_path(&self) -> PathBuf {
        self.state_directory
            .join(yeokcham_daemon::OUTBOX_DATABASE_FILE)
    }
}

#[derive(Debug, thiserror::Error)]
pub enum SdkClientError {
    #[error("SDK configuration is invalid")]
    Configuration(#[from] crate::SdkConfigError),
    #[error("daemon SDK mode is not available yet")]
    DaemonModeUnavailable,
    #[error("SDK client lifecycle operation failed")]
    Lifecycle(#[from] DaemonLifecycleError),
    #[error("SDK event sequence is exhausted")]
    EventSequenceExhausted,
    #[error("SDK async lifecycle task failed")]
    AsyncTask,
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
        let event = events.try_next().unwrap().unwrap();
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
