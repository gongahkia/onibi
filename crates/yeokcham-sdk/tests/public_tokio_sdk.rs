use std::{
    collections::BTreeMap,
    fs,
    path::PathBuf,
    sync::atomic::{AtomicU64, Ordering},
};

use yeokcham_core::{KeystoreEntryName, KeystoreSecret, OsKeystore};
use yeokcham_sdk::{
    LocalDaemonEndpoint, MAX_SDK_EVENT_BUFFER_CAPACITY, RuntimeMode, SdkClient, SdkClientBuilder,
    SdkClientError, SdkConfig, SdkConfigError, SdkEvent, SdkIdentityError,
    SdkIdentityInitialization, SdkIdentityManager,
};

static NEXT_TEST_PATH: AtomicU64 = AtomicU64::new(0);

fn state_directory() -> PathBuf {
    let number = NEXT_TEST_PATH.fetch_add(1, Ordering::Relaxed);
    std::env::temp_dir().join(format!(
        "yeokcham-public-tokio-sdk-{}-{number}",
        std::process::id()
    ))
}

#[derive(Default)]
struct MemoryKeystore {
    entries: BTreeMap<String, Vec<u8>>,
    corrupt_load: bool,
    fail_store: bool,
}

#[derive(Debug, thiserror::Error)]
#[error("memory keystore operation failed")]
struct MemoryKeystoreError;

impl OsKeystore for MemoryKeystore {
    type Error = MemoryKeystoreError;

    fn load(&self, entry: &KeystoreEntryName) -> Result<Option<KeystoreSecret>, Self::Error> {
        if self.corrupt_load {
            return Ok(Some(KeystoreSecret::new(vec![0]).unwrap()));
        }
        Ok(self
            .entries
            .get(entry.as_str())
            .map(|secret| KeystoreSecret::new(secret.clone()).unwrap()))
    }

    fn store(
        &mut self,
        entry: &KeystoreEntryName,
        secret: &KeystoreSecret,
    ) -> Result<(), Self::Error> {
        if self.fail_store {
            return Err(MemoryKeystoreError);
        }
        self.entries
            .insert(entry.as_str().to_owned(), secret.as_bytes().to_vec());
        Ok(())
    }

    fn delete(&mut self, entry: &KeystoreEntryName) -> Result<(), Self::Error> {
        self.entries.remove(entry.as_str());
        Ok(())
    }
}

#[test]
fn embedded_sdk_lifecycle_is_available_through_the_public_crate() {
    let state_directory = state_directory();
    let config = SdkConfig::new(state_directory.clone(), RuntimeMode::Embedded, 1).unwrap();
    let mut client = SdkClient::start(&config).unwrap();
    let mut events = client.subscribe();

    assert!(client.is_running());
    client.shutdown().unwrap();
    let event = events.try_recv().unwrap();
    assert_eq!(event.sequence(), 2);
    assert_eq!(event.event(), SdkEvent::ClientStopped);
    assert!(!client.is_running());
    fs::remove_dir_all(state_directory).unwrap();
}

#[test]
fn daemon_mode_fails_closed_before_the_transport_client_exists() {
    let state_directory = state_directory();
    let endpoint = LocalDaemonEndpoint::new("/tmp/yeokcham.sock".to_owned()).unwrap();
    let config = SdkConfig::new(state_directory.clone(), RuntimeMode::Daemon(endpoint), 1).unwrap();

    assert!(matches!(
        SdkClient::start(&config),
        Err(SdkClientError::DaemonModeUnavailable)
    ));
    assert!(!state_directory.exists());
}

#[test]
fn rejects_event_capacity_above_the_public_limit() {
    let state_directory = state_directory();
    assert_eq!(
        SdkConfig::new(
            state_directory,
            RuntimeMode::Embedded,
            MAX_SDK_EVENT_BUFFER_CAPACITY + 1,
        ),
        Err(SdkConfigError::InvalidEventBufferCapacity)
    );
}

#[test]
fn typed_builder_requires_explicit_runtime_and_capacity_to_produce_configuration() {
    let state_directory = state_directory();
    let config = SdkClientBuilder::new(state_directory.clone())
        .event_buffer_capacity(1)
        .embedded()
        .build()
        .unwrap();
    assert_eq!(config.state_directory(), state_directory);
    assert_eq!(config.runtime_mode(), &RuntimeMode::Embedded);
    assert_eq!(config.event_buffer_capacity(), 1);
}

#[test]
fn typed_builder_preserves_validation_at_the_build_boundary() {
    assert_eq!(
        SdkClientBuilder::new(state_directory())
            .daemon(LocalDaemonEndpoint::new("/tmp/yeokcham.sock".to_owned()).unwrap())
            .event_buffer_capacity(0)
            .build(),
        Err(SdkConfigError::InvalidEventBufferCapacity)
    );
}

#[test]
fn identity_manager_exposes_one_public_identity_without_exposing_secret_state() {
    let mut manager = SdkIdentityManager::new(MemoryKeystore::default());
    let created = manager.create_or_load().unwrap();
    let loaded = manager.load().unwrap();

    assert_eq!(created.initialization(), SdkIdentityInitialization::Created);
    assert_eq!(loaded.initialization(), SdkIdentityInitialization::Loaded);
    assert_eq!(created.public_key(), loaded.public_key());
    assert_eq!(
        manager.create().unwrap_err(),
        SdkIdentityError::AlreadyInitialized
    );
    assert_eq!(manager.into_inner().entries.len(), 1);
}

#[test]
fn identity_manager_fails_closed_for_missing_corrupt_and_unwritable_state() {
    let manager = SdkIdentityManager::new(MemoryKeystore::default());
    assert_eq!(
        manager.load().unwrap_err(),
        SdkIdentityError::NotInitialized
    );

    let corrupt = MemoryKeystore {
        corrupt_load: true,
        ..MemoryKeystore::default()
    };
    let manager = SdkIdentityManager::new(corrupt);
    assert_eq!(
        manager.load().unwrap_err(),
        SdkIdentityError::InvalidStoredIdentity
    );

    let mut manager = SdkIdentityManager::new(MemoryKeystore {
        fail_store: true,
        ..MemoryKeystore::default()
    });
    assert_eq!(manager.create().unwrap_err(), SdkIdentityError::Keystore);
    assert!(manager.into_inner().entries.is_empty());
}

#[tokio::test]
async fn async_sdk_lifecycle_runs_without_blocking_the_tokio_caller() {
    let state_directory = state_directory();
    let config = SdkClientBuilder::new(state_directory.clone())
        .embedded()
        .event_buffer_capacity(1)
        .build()
        .unwrap();
    let client = SdkClient::start_async(&config).await.unwrap();
    let mut events = client.subscribe();

    client.shutdown_async().await.unwrap();
    let event = events.try_recv().unwrap();
    assert_eq!(event.sequence(), 2);
    assert_eq!(event.event(), SdkEvent::ClientStopped);
    fs::remove_dir_all(state_directory).unwrap();
}

#[tokio::test]
async fn async_start_rejects_daemon_mode_without_creating_state() {
    let state_directory = state_directory();
    let config = SdkClientBuilder::new(state_directory.clone())
        .daemon(LocalDaemonEndpoint::new("/tmp/yeokcham.sock".to_owned()).unwrap())
        .event_buffer_capacity(1)
        .build()
        .unwrap();

    assert!(matches!(
        SdkClient::start_async(&config).await,
        Err(SdkClientError::DaemonModeUnavailable)
    ));
    assert!(!state_directory.exists());
}
