#![cfg(unix)]

use std::{
    collections::BTreeMap,
    convert::Infallible,
    fs,
    path::PathBuf,
    sync::atomic::{AtomicU64, Ordering},
};

use arachne_core::{KeystoreEntryName, KeystoreSecret, OsKeystore};
use arachne_daemon::{
    DaemonLocalAuth, DaemonLocalAuthToken, DaemonRuntime, DaemonServer,
    LOCAL_AUTH_TOKEN_METADATA_KEY,
};
use arachne_daemon_api::v1::{
    ExportIdentityRecoveryRequest, GetIdentityRequest, IdentityInitialization,
    ImportIdentityRecoveryRequest, daemon_service_client::DaemonServiceClient,
};
use arachne_protocol::{IDENTITY_EXPORT_BYTES, ProtocolVersion};
use arachne_sdk::{
    SdkIdentityError, SdkIdentityInitialization, SdkIdentityManager, SdkRecoveryArchive,
    SdkRecoveryArchiveError, SdkRecoveryError, SdkRecoveryPassphrase,
};
use hyper_util::rt::TokioIo;
use tokio::{net::UnixStream, sync::oneshot};
use tonic::{
    Code, Request,
    transport::{Channel, Endpoint},
};
use tower::service_fn;

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

fn state_directory() -> PathBuf {
    let number = NEXT_TEST_PATH.fetch_add(1, Ordering::Relaxed);
    PathBuf::from("/tmp").join(format!("ycsc-{}-{number}", std::process::id()))
}

fn authenticated_request<T>(value: T, token: &DaemonLocalAuthToken) -> Request<T> {
    let mut request = Request::new(value);
    request
        .metadata_mut()
        .insert_bin(LOCAL_AUTH_TOKEN_METADATA_KEY, token.metadata_value());
    request
}

async fn daemon_client(socket_path: PathBuf) -> DaemonServiceClient<Channel> {
    let channel = Endpoint::from_static("http://[::]:50051")
        .connect_with_connector(service_fn(move |_| {
            let socket_path = socket_path.clone();
            async move { UnixStream::connect(socket_path).await.map(TokioIo::new) }
        }))
        .await
        .unwrap();
    DaemonServiceClient::new(channel)
}

async fn assert_daemon_recovery_failures(
    client: &mut DaemonServiceClient<Channel>,
    token: &DaemonLocalAuthToken,
    archive: &[u8],
    passphrase: &[u8],
    invalid_passphrase: &[u8],
) {
    assert_eq!(
        client
            .export_identity_recovery(authenticated_request(
                ExportIdentityRecoveryRequest {
                    passphrase: passphrase.to_vec(),
                },
                token,
            ))
            .await
            .unwrap_err()
            .code(),
        Code::NotFound
    );
    assert_eq!(
        client
            .import_identity_recovery(authenticated_request(
                ImportIdentityRecoveryRequest {
                    archive: vec![0; IDENTITY_EXPORT_BYTES - 1],
                    passphrase: passphrase.to_vec(),
                },
                token,
            ))
            .await
            .unwrap_err()
            .code(),
        Code::InvalidArgument
    );
    assert_eq!(
        client
            .import_identity_recovery(authenticated_request(
                ImportIdentityRecoveryRequest {
                    archive: archive.to_vec(),
                    passphrase: invalid_passphrase.to_vec(),
                },
                token,
            ))
            .await
            .unwrap_err()
            .code(),
        Code::InvalidArgument
    );
}

async fn import_and_export_daemon_recovery(
    client: &mut DaemonServiceClient<Channel>,
    token: &DaemonLocalAuthToken,
    archive: &[u8],
    public_key: &[u8],
    passphrase: &[u8],
) -> Vec<u8> {
    let imported = client
        .import_identity_recovery(authenticated_request(
            ImportIdentityRecoveryRequest {
                archive: archive.to_vec(),
                passphrase: passphrase.to_vec(),
            },
            token,
        ))
        .await
        .unwrap()
        .into_inner();
    assert_eq!(imported.public_key, public_key);
    assert_eq!(
        IdentityInitialization::try_from(imported.initialization),
        Ok(IdentityInitialization::Recovered)
    );
    assert_eq!(
        client
            .import_identity_recovery(authenticated_request(
                ImportIdentityRecoveryRequest {
                    archive: archive.to_vec(),
                    passphrase: passphrase.to_vec(),
                },
                token,
            ))
            .await
            .unwrap_err()
            .code(),
        Code::AlreadyExists
    );
    let loaded = client
        .get_identity(authenticated_request(GetIdentityRequest {}, token))
        .await
        .unwrap()
        .into_inner();
    assert_eq!(loaded.public_key, public_key);
    assert_eq!(
        IdentityInitialization::try_from(loaded.initialization),
        Ok(IdentityInitialization::Loaded)
    );
    let archive = client
        .export_identity_recovery(authenticated_request(
            ExportIdentityRecoveryRequest {
                passphrase: passphrase.to_vec(),
            },
            token,
        ))
        .await
        .unwrap()
        .into_inner()
        .archive;
    assert_eq!(archive.len(), IDENTITY_EXPORT_BYTES);
    archive
}

#[tokio::test]
async fn embedded_and_daemon_recovery_public_boundaries_are_compatible() {
    let passphrase = b"embedded daemon compatibility passphrase".to_vec();
    let invalid_passphrase = b"embedded daemon invalid passphrase".to_vec();
    let sdk_passphrase = SdkRecoveryPassphrase::new(passphrase.clone()).unwrap();
    let invalid_sdk_passphrase = SdkRecoveryPassphrase::new(invalid_passphrase.clone()).unwrap();
    let mut embedded_source = SdkIdentityManager::new(MemoryKeystore::default());

    assert_eq!(
        embedded_source
            .export_recovery(&sdk_passphrase)
            .unwrap_err(),
        SdkRecoveryError::Identity(SdkIdentityError::NotInitialized)
    );
    assert_eq!(
        SdkRecoveryArchive::from_bytes(vec![0; IDENTITY_EXPORT_BYTES - 1]).unwrap_err(),
        SdkRecoveryArchiveError::InvalidLength
    );

    let source_identity = embedded_source.create().unwrap();
    assert_eq!(
        source_identity.initialization(),
        SdkIdentityInitialization::Created
    );
    assert_eq!(
        embedded_source.create(),
        Err(SdkIdentityError::AlreadyInitialized)
    );
    let source_public_key = source_identity.public_key().as_bytes().to_vec();
    let embedded_archive = embedded_source.export_recovery(&sdk_passphrase).unwrap();
    assert_eq!(embedded_archive.as_bytes().len(), IDENTITY_EXPORT_BYTES);
    let embedded_archive_bytes = embedded_archive.as_bytes().to_vec();

    let mut embedded_target = SdkIdentityManager::new(MemoryKeystore::default());
    assert_eq!(
        embedded_target.import_recovery(&embedded_archive, &invalid_sdk_passphrase),
        Err(SdkRecoveryError::InvalidArchive)
    );

    let state_directory = state_directory();
    let mut runtime = DaemonRuntime::start(ProtocolVersion::INITIAL, &state_directory).unwrap();
    let (auth, token) = DaemonLocalAuth::initialize().unwrap();
    let server =
        DaemonServer::bind_with_identity_keystore(&runtime, auth, MemoryKeystore::default())
            .unwrap();
    let socket_path = server.socket_path().to_path_buf();
    let (shutdown_sender, shutdown_receiver) = oneshot::channel();
    let server = server.serve_until(async move {
        let _ = shutdown_receiver.await;
    });
    let client = async {
        let mut client = daemon_client(socket_path.clone()).await;
        assert_daemon_recovery_failures(
            &mut client,
            &token,
            &embedded_archive_bytes,
            &passphrase,
            &invalid_passphrase,
        )
        .await;
        let archive = import_and_export_daemon_recovery(
            &mut client,
            &token,
            &embedded_archive_bytes,
            &source_public_key,
            &passphrase,
        )
        .await;
        shutdown_sender.send(()).unwrap();
        archive
    };
    let (server, daemon_archive) = tokio::join!(server, client);
    assert!(server.is_ok());
    assert!(!socket_path.exists());

    let daemon_archive = SdkRecoveryArchive::from_bytes(daemon_archive).unwrap();
    let recovered = embedded_target
        .import_recovery(&daemon_archive, &sdk_passphrase)
        .unwrap();
    assert_eq!(
        recovered.public_key().as_bytes(),
        source_public_key.as_slice()
    );
    assert_eq!(
        recovered.initialization(),
        SdkIdentityInitialization::Recovered
    );
    runtime.shutdown().unwrap();
    fs::remove_dir_all(state_directory).unwrap();
}
