use std::{
    collections::BTreeMap,
    fs,
    path::PathBuf,
    sync::{
        Arc,
        atomic::{AtomicU64, Ordering},
    },
    time::Duration,
};

use yeokcham_core::{IdentityKeypair, KeystoreEntryName, KeystoreSecret, OsKeystore};
use yeokcham_protocol::{
    ContactInvitation, IdentityRotation, QrVerificationPayload, SafetyNumberFingerprint,
};
use yeokcham_sdk::{
    CancellationToken, LocalDaemonEndpoint, MAX_SDK_ASYNC_DEADLINE, MAX_SDK_EVENT_BUFFER_CAPACITY,
    RuntimeMode, SdkAsyncPolicy, SdkAsyncPolicyError, SdkClient, SdkClientBuilder, SdkClientError,
    SdkConfig, SdkConfigError, SdkContactError, SdkContactStatus, SdkDeliveryProfileKind,
    SdkDeliveryProfilePolicy, SdkDeliveryProfilePolicyError, SdkDeliveryStatus,
    SdkDirectIpDisclosureAcknowledgement, SdkError, SdkEvent, SdkEventEnvelope, SdkEventStream,
    SdkEventStreamError, SdkIdentityError, SdkIdentityInitialization, SdkIdentityManager,
    SdkLocalMeshPolicy, SdkLocalMeshTransportKind, SdkMessageEnvelope, SdkMessageEnvelopeError,
    SdkMessageError, SdkMessageExpiry, SdkMessageExpiryError, SdkMessageIdentifier,
    SdkMessageSendRequest, TransportAvailability, TransportCapability, TransportCapabilityError,
    TransportCapabilityMatrix, TransportKind,
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
    let event = events.try_next().unwrap().unwrap();
    assert_eq!(event.sequence(), 2);
    assert_eq!(event.event(), SdkEvent::ClientStopped);
    assert!(!client.is_running());
    fs::remove_dir_all(state_directory).unwrap();
}

#[test]
fn rust_sdk_concurrency_contract_supports_shared_subscriptions_and_moved_streams() {
    fn assert_send_and_sync<T: Send + Sync>() {}
    fn assert_send<T: Send>() {}

    assert_send_and_sync::<SdkConfig>();
    assert_send_and_sync::<SdkClient>();
    assert_send_and_sync::<SdkEventEnvelope>();
    assert_send::<SdkEventStream>();

    let state_directory = state_directory();
    let config = SdkConfig::new(state_directory.clone(), RuntimeMode::Embedded, 8).unwrap();
    let client = SdkClient::start(&config).unwrap();
    let stream = client.subscribe();
    let result = std::thread::spawn(move || {
        let mut stream = stream;
        stream.try_next()
    })
    .join()
    .unwrap();
    assert_eq!(result, Ok(None));
    let client = Arc::new(client);
    std::thread::scope(|scope| {
        for _ in 0..2 {
            let client = Arc::clone(&client);
            scope.spawn(move || {
                let mut events = client.subscribe();
                assert_eq!(events.try_next(), Ok(None));
            });
        }
    });
    let Ok(mut client) = Arc::try_unwrap(client) else {
        panic!("all SDK client references must be released");
    };
    client.shutdown().unwrap();
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
fn sdk_client_maps_engine_lifecycle_failures_to_typed_public_errors() {
    let state_directory = state_directory();
    let config = SdkConfig::new(state_directory.clone(), RuntimeMode::Embedded, 8).unwrap();
    let mut first = SdkClient::start(&config).unwrap();
    let Err(error) = SdkClient::start(&config) else {
        panic!("second client startup must fail");
    };
    assert_eq!(error, SdkClientError::AlreadyRunning);
    assert_eq!(
        SdkError::from(error),
        SdkError::Client(SdkClientError::AlreadyRunning)
    );
    first.shutdown().unwrap();
    assert_eq!(first.shutdown(), Err(SdkClientError::NotRunning));
    std::fs::remove_dir_all(state_directory).unwrap();
}

#[test]
fn rejects_event_capacity_above_the_public_limit() {
    let state_directory = state_directory();
    assert_eq!(
        SdkConfig::new(
            state_directory.clone(),
            RuntimeMode::Embedded,
            MAX_SDK_EVENT_BUFFER_CAPACITY + 1,
        ),
        Err(SdkConfigError::InvalidEventBufferCapacity)
    );
    assert!(!state_directory.exists());
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
fn transport_capability_matrix_is_complete_at_the_public_boundary() {
    let matrix = TransportCapabilityMatrix::new(vec![
        TransportCapability::new(TransportKind::Lan, TransportAvailability::Available).unwrap(),
        TransportCapability::new(TransportKind::Direct, TransportAvailability::Available).unwrap(),
        TransportCapability::new(
            TransportKind::WifiDirect,
            TransportAvailability::Unsupported,
        )
        .unwrap(),
        TransportCapability::new(
            TransportKind::WifiHotspot,
            TransportAvailability::PermissionDenied,
        )
        .unwrap(),
        TransportCapability::new(TransportKind::Bluetooth, TransportAvailability::Unavailable)
            .unwrap(),
        TransportCapability::new(TransportKind::TorMaildrop, TransportAvailability::Available)
            .unwrap(),
    ])
    .unwrap();
    assert_eq!(
        matrix.capability(TransportKind::WifiHotspot).availability(),
        TransportAvailability::PermissionDenied
    );
    assert_eq!(
        TransportCapabilityMatrix::new(vec![
            TransportCapability::new(TransportKind::TorMaildrop, TransportAvailability::Available)
                .unwrap(),
        ]),
        Err(TransportCapabilityError::IncompleteMatrix)
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

#[test]
fn contact_manager_imports_lists_and_revokes_contacts_under_the_sdk_runtime_lock() {
    let state_directory = state_directory();
    let config = SdkConfig::new(state_directory.clone(), RuntimeMode::Embedded, 1).unwrap();
    let mut identities = SdkIdentityManager::new(MemoryKeystore::default());
    identities.create_or_load().unwrap();
    let mut client = SdkClient::start(&config).unwrap();
    let remote = IdentityKeypair::generate().unwrap();
    let invitation = ContactInvitation::create(&remote)
        .unwrap()
        .encode()
        .unwrap();
    let replacement = IdentityKeypair::generate().unwrap();
    let unknown = IdentityKeypair::generate().unwrap();
    let unknown_rotation = IdentityRotation::create(&unknown, replacement.public_key())
        .unwrap()
        .encode()
        .unwrap();

    {
        let mut contacts = client.contact_manager(&mut identities).unwrap();
        let imported = contacts.import_invitation(&invitation).unwrap();
        assert_eq!(imported.identity(), remote.public_key());
        assert_eq!(imported.status(), SdkContactStatus::Pending);
        assert_eq!(contacts.contact(&remote.public_key()), Some(imported));
        assert_eq!(contacts.contacts().collect::<Vec<_>>(), vec![imported]);
        assert_eq!(
            contacts.apply_rotation(&unknown_rotation).unwrap_err(),
            SdkContactError::UnknownContact
        );
        assert_eq!(
            contacts.revoke(&remote.public_key()).unwrap().status(),
            SdkContactStatus::Revoked
        );
    }
    client.shutdown().unwrap();

    let mut restarted = SdkClient::start(&config).unwrap();
    {
        let contacts = restarted.contact_manager(&mut identities).unwrap();
        assert_eq!(contacts.contacts().count(), 1);
        assert_eq!(
            contacts.contact(&remote.public_key()).unwrap().status(),
            SdkContactStatus::Revoked
        );
    }
    restarted.shutdown().unwrap();
    fs::remove_dir_all(state_directory).unwrap();
}

#[test]
fn contact_manager_fails_closed_before_opening_state_for_missing_identity_or_bad_invitation() {
    let state_directory = state_directory();
    let config = SdkConfig::new(state_directory.clone(), RuntimeMode::Embedded, 1).unwrap();
    let mut client = SdkClient::start(&config).unwrap();
    let mut empty_identities = SdkIdentityManager::new(MemoryKeystore::default());

    assert!(matches!(
        client.contact_manager(&mut empty_identities),
        Err(SdkContactError::Identity(SdkIdentityError::NotInitialized))
    ));
    assert!(!state_directory.join("yeokcham-contacts.sqlite").exists());

    let mut identities = SdkIdentityManager::new(MemoryKeystore::default());
    identities.create_or_load().unwrap();
    let mut contacts = client.contact_manager(&mut identities).unwrap();
    assert_eq!(
        contacts.import_invitation(&[0xa1]).unwrap_err(),
        SdkContactError::InvalidInvitation
    );
    assert_eq!(contacts.contacts().count(), 0);
    drop(contacts);
    client.shutdown().unwrap();
    fs::remove_dir_all(state_directory).unwrap();
}

#[test]
fn contact_manager_verifies_qr_and_safety_numbers_at_the_public_boundary() {
    let state_directory = state_directory();
    let config = SdkConfig::new(state_directory.clone(), RuntimeMode::Embedded, 1).unwrap();
    let mut identities = SdkIdentityManager::new(MemoryKeystore::default());
    let local = identities.create_or_load().unwrap().public_key();
    let mut client = SdkClient::start(&config).unwrap();
    let qr_remote = IdentityKeypair::generate().unwrap();
    let safety_remote = IdentityKeypair::generate().unwrap();
    let qr_invitation = ContactInvitation::create(&qr_remote)
        .unwrap()
        .encode()
        .unwrap();
    let safety_invitation = ContactInvitation::create(&safety_remote)
        .unwrap()
        .encode()
        .unwrap();
    let qr_payload = QrVerificationPayload::new(local, qr_remote.public_key())
        .unwrap()
        .encode()
        .unwrap();
    let safety_number = SafetyNumberFingerprint::derive(&local, &safety_remote.public_key())
        .unwrap()
        .as_bytes()
        .to_owned();

    {
        let mut contacts = client.contact_manager(&mut identities).unwrap();
        contacts.import_invitation(&qr_invitation).unwrap();
        contacts.import_invitation(&safety_invitation).unwrap();
        let qr = contacts.verify_qr(&qr_payload).unwrap();
        let safety = contacts
            .verify_safety_number(&safety_remote.public_key(), &safety_number)
            .unwrap();

        assert_eq!(qr.status(), SdkContactStatus::Verified);
        assert_eq!(
            qr.verification_method(),
            Some(yeokcham_sdk::SdkContactVerificationMethod::Qr)
        );
        assert_eq!(safety.status(), SdkContactStatus::Verified);
        assert_eq!(
            safety.verification_method(),
            Some(yeokcham_sdk::SdkContactVerificationMethod::SafetyNumber)
        );
    }
    client.shutdown().unwrap();
    fs::remove_dir_all(state_directory).unwrap();
}

#[test]
fn contact_manager_rejects_invalid_verification_without_changing_pending_contact() {
    let state_directory = state_directory();
    let config = SdkConfig::new(state_directory.clone(), RuntimeMode::Embedded, 1).unwrap();
    let mut identities = SdkIdentityManager::new(MemoryKeystore::default());
    identities.create_or_load().unwrap();
    let mut client = SdkClient::start(&config).unwrap();
    let remote = IdentityKeypair::generate().unwrap();
    let invitation = ContactInvitation::create(&remote)
        .unwrap()
        .encode()
        .unwrap();

    {
        let mut contacts = client.contact_manager(&mut identities).unwrap();
        contacts.import_invitation(&invitation).unwrap();
        assert_eq!(
            contacts.verify_qr(&[0xa1]).unwrap_err(),
            SdkContactError::InvalidVerification
        );
        assert_eq!(
            contacts
                .verify_safety_number(&remote.public_key(), &[0; 31])
                .unwrap_err(),
            SdkContactError::InvalidVerification
        );
        assert_eq!(
            contacts
                .verify_safety_number(&remote.public_key(), &[0; 32])
                .unwrap_err(),
            SdkContactError::InvalidVerification
        );
        assert_eq!(
            contacts.contact(&remote.public_key()).unwrap().status(),
            SdkContactStatus::Pending
        );
    }
    client.shutdown().unwrap();
    fs::remove_dir_all(state_directory).unwrap();
}

#[test]
fn delivery_profile_policy_requires_explicit_privacy_and_transport_selection() {
    let local_mesh = SdkLocalMeshPolicy::new(&[SdkLocalMeshTransportKind::Lan]).unwrap();
    let policy = SdkDeliveryProfilePolicy::new(true, false, Some(local_mesh)).unwrap();
    let direct = policy
        .select_direct(SdkDirectIpDisclosureAcknowledgement::acknowledge())
        .unwrap();
    let local = policy
        .select_local_mesh(SdkLocalMeshTransportKind::Lan)
        .unwrap();

    assert_eq!(direct.kind(), SdkDeliveryProfileKind::Direct);
    assert!(direct.has_direct_ip_disclosure_warning());
    assert_eq!(local.kind(), SdkDeliveryProfileKind::LocalMesh);
    assert!(!local.has_direct_ip_disclosure_warning());
    assert_eq!(
        policy
            .select_local_mesh(SdkLocalMeshTransportKind::Bluetooth)
            .unwrap_err(),
        SdkDeliveryProfilePolicyError::LocalMeshTransportDisallowed
    );
    assert_eq!(
        policy.select_tor_maildrop().unwrap_err(),
        SdkDeliveryProfilePolicyError::TorMaildropDisallowed
    );
}

#[test]
fn delivery_profile_policy_rejects_empty_and_silent_profile_fallbacks() {
    assert_eq!(
        SdkLocalMeshPolicy::new(&[]).unwrap_err(),
        SdkDeliveryProfilePolicyError::NoAllowedLocalMeshTransports
    );
    assert_eq!(
        SdkLocalMeshPolicy::new(&[
            SdkLocalMeshTransportKind::Lan,
            SdkLocalMeshTransportKind::WifiHotspot,
            SdkLocalMeshTransportKind::WifiDirect,
            SdkLocalMeshTransportKind::Bluetooth,
            SdkLocalMeshTransportKind::Lan,
        ])
        .unwrap_err(),
        SdkDeliveryProfilePolicyError::TooManyLocalMeshTransports
    );
    assert_eq!(
        SdkDeliveryProfilePolicy::new(false, false, None).unwrap_err(),
        SdkDeliveryProfilePolicyError::NoAllowedProfiles
    );
    let policy = SdkDeliveryProfilePolicy::new(true, true, None).unwrap();
    let direct = policy
        .select_direct(SdkDirectIpDisclosureAcknowledgement::acknowledge())
        .unwrap();
    let tor = policy.select_tor_maildrop().unwrap();

    assert_eq!(
        direct.validate_automatic_replacement(tor).unwrap_err(),
        SdkDeliveryProfilePolicyError::DirectToTorRequiresExplicitSelection
    );
    assert_eq!(
        policy
            .select_local_mesh(SdkLocalMeshTransportKind::Lan)
            .unwrap_err(),
        SdkDeliveryProfilePolicyError::LocalMeshDisallowed
    );
}

#[test]
fn sdk_message_send_queues_a_validated_envelope_and_emits_an_event() {
    let state_directory = state_directory();
    let config = SdkConfig::new(state_directory.clone(), RuntimeMode::Embedded, 1).unwrap();
    let mut identities = SdkIdentityManager::new(MemoryKeystore::default());
    identities.create_or_load().unwrap();
    let recipient = IdentityKeypair::generate().unwrap().public_key();
    let envelope = SdkMessageEnvelope::new(vec![0xa1], vec![0xb2]).unwrap();
    let expiry = SdkMessageExpiry::new(100, 60).unwrap();
    let mut client = SdkClient::start(&config).unwrap();
    let mut events = client.subscribe();

    let queued = client
        .send_message(
            &mut identities,
            SdkMessageSendRequest::new(recipient, envelope, expiry),
        )
        .unwrap();
    let event = events.try_next().unwrap().unwrap();

    assert_eq!(queued.recipient(), recipient);
    assert_eq!(queued.expiry(), expiry);
    match event.event() {
        SdkEvent::MessageQueued(identifier) => {
            assert_eq!(identifier.as_bytes(), queued.identifier().as_bytes());
        }
        event => panic!("unexpected SDK event: {event:?}"),
    }
    client.shutdown().unwrap();
    fs::remove_dir_all(state_directory).unwrap();
}

#[test]
fn sdk_message_send_rejects_invalid_input_and_missing_identity_before_outbox_creation() {
    assert_eq!(
        SdkMessageEnvelope::new(Vec::new(), vec![0xb2]).unwrap_err(),
        SdkMessageEnvelopeError::InvalidEnvelope
    );
    assert_eq!(
        SdkMessageExpiry::new(100, 0).unwrap_err(),
        SdkMessageExpiryError::InvalidTtl
    );
    assert_eq!(
        SdkMessageEnvelope::from_encoded(&[0x98, 0x02, 0x41, 0xa1, 0x41, 0xb2]).unwrap_err(),
        SdkMessageEnvelopeError::NonCanonicalEncoding
    );

    let state_directory = state_directory();
    let config = SdkConfig::new(state_directory.clone(), RuntimeMode::Embedded, 1).unwrap();
    let mut identities = SdkIdentityManager::new(MemoryKeystore::default());
    let recipient = IdentityKeypair::generate().unwrap().public_key();
    let request = SdkMessageSendRequest::new(
        recipient,
        SdkMessageEnvelope::new(vec![0xa1], vec![0xb2]).unwrap(),
        SdkMessageExpiry::new(100, 60).unwrap(),
    );
    let mut client = SdkClient::start(&config).unwrap();

    assert!(matches!(
        client.send_message(&mut identities, request),
        Err(SdkMessageError::Identity(SdkIdentityError::NotInitialized))
    ));
    assert!(!state_directory.join("yeokcham-outbox.sqlite").exists());
    client.shutdown().unwrap();
    fs::remove_dir_all(state_directory).unwrap();
}

#[test]
fn sdk_event_stream_reports_overflow_from_its_configured_bounded_buffer() {
    let state_directory = state_directory();
    let config = SdkConfig::new(state_directory.clone(), RuntimeMode::Embedded, 1).unwrap();
    let mut identities = SdkIdentityManager::new(MemoryKeystore::default());
    identities.create_or_load().unwrap();
    let recipient = IdentityKeypair::generate().unwrap().public_key();
    let request = SdkMessageSendRequest::new(
        recipient,
        SdkMessageEnvelope::new(vec![0xa1], vec![0xb2]).unwrap(),
        SdkMessageExpiry::new(100, 60).unwrap(),
    );
    let mut client = SdkClient::start(&config).unwrap();
    let mut events = client.subscribe();

    client.send_message(&mut identities, request).unwrap();
    client.shutdown().unwrap();

    assert_eq!(events.try_next(), Err(SdkEventStreamError::Lagged(1)));
    fs::remove_dir_all(state_directory).unwrap();
}

#[test]
fn sdk_delivery_status_reports_absent_and_queued_messages_without_creating_an_empty_outbox() {
    let state_directory = state_directory();
    let config = SdkConfig::new(state_directory.clone(), RuntimeMode::Embedded, 1).unwrap();
    let mut identities = SdkIdentityManager::new(MemoryKeystore::default());
    identities.create_or_load().unwrap();
    let mut client = SdkClient::start(&config).unwrap();
    let unknown = SdkMessageIdentifier::from_bytes([1; 16]).unwrap();

    assert_eq!(
        client.delivery_status(&mut identities, unknown).unwrap(),
        None
    );
    assert!(!state_directory.join("yeokcham-outbox.sqlite").exists());

    let recipient = IdentityKeypair::generate().unwrap().public_key();
    let queued = client
        .send_message(
            &mut identities,
            SdkMessageSendRequest::new(
                recipient,
                SdkMessageEnvelope::new(vec![0xa1], vec![0xb2]).unwrap(),
                SdkMessageExpiry::new(100, 60).unwrap(),
            ),
        )
        .unwrap();
    assert_eq!(
        client
            .delivery_status(&mut identities, queued.identifier())
            .unwrap(),
        Some(SdkDeliveryStatus::Queued)
    );
    client.shutdown().unwrap();
    fs::remove_dir_all(state_directory).unwrap();
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
    let event = events.try_next().unwrap().unwrap();
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

#[tokio::test]
async fn sdk_event_stream_applies_cancellation_and_deadline_policy() {
    let state_directory = state_directory();
    let config = SdkConfig::new(state_directory.clone(), RuntimeMode::Embedded, 1).unwrap();
    let mut client = SdkClient::start(&config).unwrap();
    let mut events = client.subscribe();
    let policy = SdkAsyncPolicy::new(Duration::from_millis(20)).unwrap();
    assert_eq!(
        SdkAsyncPolicy::new(Duration::ZERO),
        Err(SdkAsyncPolicyError::ZeroDeadline)
    );
    assert_eq!(
        SdkAsyncPolicy::new(MAX_SDK_ASYNC_DEADLINE + Duration::from_nanos(1)),
        Err(SdkAsyncPolicyError::DeadlineExceedsMaximum)
    );

    let cancelled = CancellationToken::new();
    cancelled.cancel();
    assert_eq!(
        events.next_with_policy(policy, &cancelled).await,
        Err(SdkEventStreamError::Cancelled)
    );

    let active = CancellationToken::new();
    assert_eq!(
        events.next_with_policy(policy, &active).await,
        Err(SdkEventStreamError::DeadlineExceeded)
    );

    let task_token = CancellationToken::new();
    let event_task =
        tokio::spawn(async move { events.next_with_policy(policy, &task_token).await });
    client.shutdown().unwrap();
    let event = event_task.await.unwrap().unwrap();
    assert_eq!(event.event(), SdkEvent::ClientStopped);
    fs::remove_dir_all(state_directory).unwrap();
}
