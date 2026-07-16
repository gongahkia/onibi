use std::{
    fs,
    path::PathBuf,
    sync::atomic::{AtomicU64, Ordering},
};

use yeokcham_sdk::{
    LocalDaemonEndpoint, MAX_SDK_EVENT_BUFFER_CAPACITY, RuntimeMode, SdkClient, SdkClientBuilder,
    SdkClientError, SdkConfig, SdkConfigError, SdkEvent,
};

static NEXT_TEST_PATH: AtomicU64 = AtomicU64::new(0);

fn state_directory() -> PathBuf {
    let number = NEXT_TEST_PATH.fetch_add(1, Ordering::Relaxed);
    std::env::temp_dir().join(format!(
        "yeokcham-public-tokio-sdk-{}-{number}",
        std::process::id()
    ))
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
