use std::{future::Future, path::Path};

use tonic::transport::Server;
use yeokcham_core::OsKeystore;
use yeokcham_daemon_api::v1::daemon_service_server::DaemonServiceServer;

use crate::{
    DaemonEndpointConfig, DaemonGrpcService, DaemonGrpcServiceConfigurationError, DaemonLocalAuth,
    DaemonRuntime, DaemonUnixListener, DaemonUnixListenerError,
};

pub struct DaemonServer<'runtime> {
    listener: DaemonUnixListener<'runtime>,
    auth: DaemonLocalAuth,
    service: DaemonGrpcService,
}

#[derive(Debug, thiserror::Error)]
pub enum DaemonServerError {
    #[error("daemon server listener could not start")]
    Listener(#[from] DaemonUnixListenerError),
    #[error("daemon server identity configuration failed")]
    IdentityConfiguration(#[from] DaemonGrpcServiceConfigurationError),
    #[error("daemon server stopped with a transport error")]
    Transport(#[source] tonic::transport::Error),
}

impl<'runtime> DaemonServer<'runtime> {
    pub fn bind(
        runtime: &'runtime DaemonRuntime,
        auth: DaemonLocalAuth,
    ) -> Result<Self, DaemonServerError> {
        let listener = DaemonUnixListener::bind(runtime)?;
        let service = DaemonGrpcService::with_system_keystore(
            runtime.daemon().protocol_version(),
            runtime.state_directory().contacts_path(),
            runtime.state_directory().outbox_path(),
        )?;
        Ok(Self::new(listener, auth, service))
    }

    pub fn bind_configured(
        runtime: &'runtime DaemonRuntime,
        endpoint: &DaemonEndpointConfig,
        auth: DaemonLocalAuth,
    ) -> Result<Self, DaemonServerError> {
        let listener = DaemonUnixListener::bind_configured(runtime, endpoint)?;
        let service = DaemonGrpcService::with_system_keystore(
            runtime.daemon().protocol_version(),
            runtime.state_directory().contacts_path(),
            runtime.state_directory().outbox_path(),
        )?;
        Ok(Self::new(listener, auth, service))
    }

    pub fn bind_with_identity_keystore<K>(
        runtime: &'runtime DaemonRuntime,
        auth: DaemonLocalAuth,
        keystore: K,
    ) -> Result<Self, DaemonServerError>
    where
        K: OsKeystore + Send + 'static,
    {
        let listener = DaemonUnixListener::bind(runtime)?;
        let service = DaemonGrpcService::with_state_keystore(
            runtime.daemon().protocol_version(),
            runtime.state_directory().contacts_path(),
            runtime.state_directory().outbox_path(),
            keystore,
        );
        Ok(Self::new(listener, auth, service))
    }

    #[must_use]
    pub fn socket_path(&self) -> &Path {
        self.listener.socket_path()
    }

    pub async fn serve_until<F>(self, shutdown: F) -> Result<(), DaemonServerError>
    where
        F: Future<Output = ()>,
    {
        let incoming = self.listener.into_incoming()?;
        let service = DaemonServiceServer::with_interceptor(self.service, self.auth.interceptor());
        Server::builder()
            .add_service(service)
            .serve_with_incoming_shutdown(incoming, shutdown)
            .await
            .map_err(DaemonServerError::Transport)
    }

    const fn new(
        listener: DaemonUnixListener<'runtime>,
        auth: DaemonLocalAuth,
        service: DaemonGrpcService,
    ) -> Self {
        Self {
            listener,
            auth,
            service,
        }
    }
}

#[cfg(test)]
mod tests {
    use std::{
        collections::BTreeMap,
        convert::Infallible,
        fs,
        path::PathBuf,
        sync::atomic::{AtomicU64, Ordering},
    };

    use hyper_util::rt::TokioIo;
    use tokio::{net::UnixStream, sync::oneshot};
    use tonic::{
        Code, Request,
        transport::{Channel, Endpoint},
    };
    use tower::service_fn;
    use yeokcham_core::{
        IdentityKeypair, IdentityPublicKey, KeystoreEntryName, KeystoreSecret, OsKeystore,
    };
    use yeokcham_daemon_api::v1::{
        ApplyContactIdentityRotationRequest, ContactResponse, ContactStatus as RpcContactStatus,
        ContactVerificationMethod as RpcContactVerificationMethod, CreateIdentityRequest,
        CreateOrLoadIdentityRequest, DeliveryStatus as RpcDeliveryStatus, GetContactRequest,
        GetDeliveryStatusRequest, GetIdentityRequest, IdentityInitialization,
        ImportContactInvitationRequest, ListContactsRequest, RevokeContactRequest,
        SendMessageRequest, StartClientRequest, VerifyContactQrRequest,
        VerifyContactSafetyNumberRequest, daemon_service_client::DaemonServiceClient,
    };
    use yeokcham_protocol::{
        ContactInvitation, EncryptedMessageEnvelope, IdentityRotation, MESSAGE_IDENTIFIER_BYTES,
        MessageIdentifier, ProtocolVersion, QrVerificationPayload, SafetyNumberFingerprint,
    };

    use super::DaemonServer;
    use crate::{
        ClientIdentity, DaemonLocalAuth, DaemonLocalAuthToken, DaemonRuntime,
        LOCAL_AUTH_TOKEN_METADATA_KEY,
    };

    static NEXT_TEST_PATH: AtomicU64 = AtomicU64::new(0);

    fn state_directory() -> PathBuf {
        let number = NEXT_TEST_PATH.fetch_add(1, Ordering::Relaxed);
        PathBuf::from("/tmp").join(format!("ycsrv-{}-{number}", std::process::id()))
    }

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

    fn authenticated_request<T>(value: T, token: &DaemonLocalAuthToken) -> Request<T> {
        let mut request = Request::new(value);
        request
            .metadata_mut()
            .insert_bin(LOCAL_AUTH_TOKEN_METADATA_KEY, token.metadata_value());
        request
    }

    #[tokio::test]
    async fn serves_authenticated_client_start_and_shuts_down_cleanly() {
        let state_directory = state_directory();
        let mut runtime = DaemonRuntime::start(ProtocolVersion::INITIAL, &state_directory).unwrap();
        let (auth, token) = DaemonLocalAuth::initialize().unwrap();
        let server = DaemonServer::bind(&runtime, auth).unwrap();
        let socket_path = server.socket_path().to_path_buf();
        let (shutdown_sender, shutdown_receiver) = oneshot::channel();

        let server = server.serve_until(async move {
            let _ = shutdown_receiver.await;
        });
        let client = async {
            let connector_path = socket_path.clone();
            let channel = Endpoint::from_static("http://[::]:50051")
                .connect_with_connector(service_fn(move |_| {
                    let connector_path = connector_path.clone();
                    async move { UnixStream::connect(connector_path).await.map(TokioIo::new) }
                }))
                .await
                .unwrap();
            let mut client = DaemonServiceClient::new(channel);
            assert_eq!(
                client
                    .start_client(Request::new(StartClientRequest {}))
                    .await
                    .unwrap_err()
                    .code(),
                Code::Unauthenticated
            );
            let status = client
                .start_client(authenticated_request(StartClientRequest {}, &token))
                .await
                .unwrap()
                .into_inner();
            shutdown_sender.send(()).unwrap();
            status
        };
        let (server, status) = tokio::join!(server, client);
        assert!(server.is_ok());
        assert_eq!(status.api_major, u32::from(ProtocolVersion::INITIAL.get()));
        assert_eq!(status.api_minor, 0);
        assert!(status.running);
        assert!(!socket_path.exists());
        runtime.shutdown().unwrap();
        fs::remove_dir_all(state_directory).unwrap();
    }

    #[tokio::test]
    async fn serves_authenticated_identity_lifecycle_without_secret_material() {
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
            let connector_path = socket_path.clone();
            let channel = Endpoint::from_static("http://[::]:50051")
                .connect_with_connector(service_fn(move |_| {
                    let connector_path = connector_path.clone();
                    async move { UnixStream::connect(connector_path).await.map(TokioIo::new) }
                }))
                .await
                .unwrap();
            let mut client = DaemonServiceClient::new(channel);
            assert_eq!(
                client
                    .get_identity(Request::new(GetIdentityRequest {}))
                    .await
                    .unwrap_err()
                    .code(),
                Code::Unauthenticated
            );
            assert_eq!(
                client
                    .get_identity(authenticated_request(GetIdentityRequest {}, &token))
                    .await
                    .unwrap_err()
                    .code(),
                Code::NotFound
            );
            let created = client
                .create_identity(authenticated_request(CreateIdentityRequest {}, &token))
                .await
                .unwrap()
                .into_inner();
            assert_eq!(created.public_key.len(), 32);
            assert_eq!(
                IdentityInitialization::try_from(created.initialization),
                Ok(IdentityInitialization::Created)
            );
            assert_eq!(
                client
                    .create_identity(authenticated_request(CreateIdentityRequest {}, &token))
                    .await
                    .unwrap_err()
                    .code(),
                Code::AlreadyExists
            );
            let loaded = client
                .get_identity(authenticated_request(GetIdentityRequest {}, &token))
                .await
                .unwrap()
                .into_inner();
            assert_eq!(loaded.public_key, created.public_key);
            assert_eq!(
                IdentityInitialization::try_from(loaded.initialization),
                Ok(IdentityInitialization::Loaded)
            );
            let loaded_again = client
                .create_or_load_identity(authenticated_request(
                    CreateOrLoadIdentityRequest {},
                    &token,
                ))
                .await
                .unwrap()
                .into_inner();
            assert_eq!(loaded_again.public_key, created.public_key);
            assert_eq!(
                IdentityInitialization::try_from(loaded_again.initialization),
                Ok(IdentityInitialization::Loaded)
            );
            shutdown_sender.send(()).unwrap();
            created.public_key
        };
        let (server, public_key) = tokio::join!(server, client);
        assert!(server.is_ok());
        assert_eq!(public_key.len(), 32);
        assert!(!socket_path.exists());
        runtime.shutdown().unwrap();
        fs::remove_dir_all(state_directory).unwrap();
    }

    async fn contact_client(socket_path: PathBuf) -> DaemonServiceClient<Channel> {
        let channel = Endpoint::from_static("http://[::]:50051")
            .connect_with_connector(service_fn(move |_| {
                let socket_path = socket_path.clone();
                async move { UnixStream::connect(socket_path).await.map(TokioIo::new) }
            }))
            .await
            .unwrap();
        DaemonServiceClient::new(channel)
    }

    async fn import_and_load_pending_contact(
        client: &mut DaemonServiceClient<Channel>,
        token: &DaemonLocalAuthToken,
        invitation: &[u8],
        remote_identity: &IdentityPublicKey,
        expected_contact_count: usize,
    ) -> ContactResponse {
        assert_eq!(
            client
                .list_contacts(Request::new(ListContactsRequest {}))
                .await
                .unwrap_err()
                .code(),
            Code::Unauthenticated
        );
        assert_eq!(
            client
                .get_contact(authenticated_request(
                    GetContactRequest {
                        identity: vec![0; 31],
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
                .list_contacts(authenticated_request(ListContactsRequest {}, token))
                .await
                .unwrap()
                .into_inner()
                .contacts
                .len(),
            expected_contact_count
        );
        assert_eq!(
            client
                .import_contact_invitation(authenticated_request(
                    ImportContactInvitationRequest {
                        invitation: vec![0; invitation.len() - 1],
                    },
                    token,
                ))
                .await
                .unwrap_err()
                .code(),
            Code::InvalidArgument
        );
        let imported = client
            .import_contact_invitation(authenticated_request(
                ImportContactInvitationRequest {
                    invitation: invitation.to_vec(),
                },
                token,
            ))
            .await
            .unwrap()
            .into_inner();
        assert_eq!(imported.identity, remote_identity.as_bytes());
        assert_eq!(
            RpcContactStatus::try_from(imported.status),
            Ok(RpcContactStatus::Pending)
        );
        assert_eq!(
            RpcContactVerificationMethod::try_from(imported.verification_method),
            Ok(RpcContactVerificationMethod::Unspecified)
        );
        let duplicate = client
            .import_contact_invitation(authenticated_request(
                ImportContactInvitationRequest {
                    invitation: invitation.to_vec(),
                },
                token,
            ))
            .await
            .unwrap()
            .into_inner();
        assert_eq!(duplicate, imported);
        let loaded = client
            .get_contact(authenticated_request(
                GetContactRequest {
                    identity: remote_identity.as_bytes().to_vec(),
                },
                token,
            ))
            .await
            .unwrap()
            .into_inner()
            .contact
            .unwrap();
        assert_eq!(loaded, imported);
        imported
    }

    async fn reject_pending_rotation_and_revoke_contact(
        client: &mut DaemonServiceClient<Channel>,
        token: &DaemonLocalAuthToken,
        remote_identity: &IdentityPublicKey,
        rotation: Vec<u8>,
    ) {
        assert_eq!(
            client
                .apply_contact_identity_rotation(authenticated_request(
                    ApplyContactIdentityRotationRequest { rotation },
                    token,
                ))
                .await
                .unwrap_err()
                .code(),
            Code::FailedPrecondition
        );
        let revoked = client
            .revoke_contact(authenticated_request(
                RevokeContactRequest {
                    identity: remote_identity.as_bytes().to_vec(),
                },
                token,
            ))
            .await
            .unwrap()
            .into_inner();
        assert_eq!(
            RpcContactStatus::try_from(revoked.status),
            Ok(RpcContactStatus::Revoked)
        );
        assert_eq!(
            RpcContactVerificationMethod::try_from(revoked.verification_method),
            Ok(RpcContactVerificationMethod::Unspecified)
        );
        let contacts = client
            .list_contacts(authenticated_request(ListContactsRequest {}, token))
            .await
            .unwrap()
            .into_inner()
            .contacts;
        assert_eq!(contacts, vec![revoked]);
    }

    async fn verify_qr_contact(
        client: &mut DaemonServiceClient<Channel>,
        token: &DaemonLocalAuthToken,
        payload: &[u8],
        remote_identity: &IdentityPublicKey,
    ) -> ContactResponse {
        assert_eq!(
            client
                .verify_contact_qr(Request::new(VerifyContactQrRequest {
                    payload: payload.to_vec(),
                }))
                .await
                .unwrap_err()
                .code(),
            Code::Unauthenticated
        );
        assert_eq!(
            client
                .verify_contact_qr(authenticated_request(
                    VerifyContactQrRequest {
                        payload: vec![0; payload.len() - 1],
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
                .verify_contact_qr(authenticated_request(
                    VerifyContactQrRequest {
                        payload: vec![0; payload.len()],
                    },
                    token,
                ))
                .await
                .unwrap_err()
                .code(),
            Code::InvalidArgument
        );
        let verified = client
            .verify_contact_qr(authenticated_request(
                VerifyContactQrRequest {
                    payload: payload.to_vec(),
                },
                token,
            ))
            .await
            .unwrap()
            .into_inner();
        assert_eq!(verified.identity, remote_identity.as_bytes());
        assert_eq!(
            RpcContactStatus::try_from(verified.status),
            Ok(RpcContactStatus::Verified)
        );
        assert_eq!(
            RpcContactVerificationMethod::try_from(verified.verification_method),
            Ok(RpcContactVerificationMethod::Qr)
        );
        verified
    }

    async fn verify_safety_number_contact(
        client: &mut DaemonServiceClient<Channel>,
        token: &DaemonLocalAuthToken,
        remote_identity: &IdentityPublicKey,
        fingerprint: &[u8],
    ) -> ContactResponse {
        assert_eq!(
            client
                .verify_contact_safety_number(authenticated_request(
                    VerifyContactSafetyNumberRequest {
                        identity: vec![0; 31],
                        fingerprint: fingerprint.to_vec(),
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
                .verify_contact_safety_number(authenticated_request(
                    VerifyContactSafetyNumberRequest {
                        identity: remote_identity.as_bytes().to_vec(),
                        fingerprint: vec![0; fingerprint.len() - 1],
                    },
                    token,
                ))
                .await
                .unwrap_err()
                .code(),
            Code::InvalidArgument
        );
        let mut mismatch = fingerprint.to_vec();
        mismatch[0] ^= 1;
        let error = client
            .verify_contact_safety_number(authenticated_request(
                VerifyContactSafetyNumberRequest {
                    identity: remote_identity.as_bytes().to_vec(),
                    fingerprint: mismatch,
                },
                token,
            ))
            .await
            .unwrap_err();
        assert_eq!(error.code(), Code::Internal);
        assert_eq!(error.message(), "daemon contact operation failed");
        let verified = client
            .verify_contact_safety_number(authenticated_request(
                VerifyContactSafetyNumberRequest {
                    identity: remote_identity.as_bytes().to_vec(),
                    fingerprint: fingerprint.to_vec(),
                },
                token,
            ))
            .await
            .unwrap()
            .into_inner();
        assert_eq!(verified.identity, remote_identity.as_bytes());
        assert_eq!(
            RpcContactStatus::try_from(verified.status),
            Ok(RpcContactStatus::Verified)
        );
        assert_eq!(
            RpcContactVerificationMethod::try_from(verified.verification_method),
            Ok(RpcContactVerificationMethod::SafetyNumber)
        );
        verified
    }

    async fn assert_queued_delivery_status(
        client: &mut DaemonServiceClient<Channel>,
        token: &DaemonLocalAuthToken,
        message_identifier: &[u8],
    ) {
        assert_eq!(
            client
                .get_delivery_status(authenticated_request(
                    GetDeliveryStatusRequest {
                        message_identifier: vec![0; MESSAGE_IDENTIFIER_BYTES - 1],
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
                .get_delivery_status(authenticated_request(
                    GetDeliveryStatusRequest {
                        message_identifier: vec![0; MESSAGE_IDENTIFIER_BYTES],
                    },
                    token,
                ))
                .await
                .unwrap_err()
                .code(),
            Code::InvalidArgument
        );
        let unknown = MessageIdentifier::generate().unwrap();
        assert_eq!(
            client
                .get_delivery_status(authenticated_request(
                    GetDeliveryStatusRequest {
                        message_identifier: unknown.as_bytes().to_vec(),
                    },
                    token,
                ))
                .await
                .unwrap_err()
                .code(),
            Code::NotFound
        );
        let status = client
            .get_delivery_status(authenticated_request(
                GetDeliveryStatusRequest {
                    message_identifier: message_identifier.to_vec(),
                },
                token,
            ))
            .await
            .unwrap()
            .into_inner();
        assert_eq!(
            RpcDeliveryStatus::try_from(status.status),
            Ok(RpcDeliveryStatus::Queued)
        );
    }

    #[tokio::test]
    async fn serves_authenticated_contact_lifecycle_with_bounded_inputs() {
        let state_directory = state_directory();
        let mut runtime = DaemonRuntime::start(ProtocolVersion::INITIAL, &state_directory).unwrap();
        let mut keystore = MemoryKeystore::default();
        ClientIdentity::create(&mut keystore).unwrap();
        let remote = IdentityKeypair::generate().unwrap();
        let invitation = ContactInvitation::create(&remote)
            .unwrap()
            .encode()
            .unwrap();
        let replacement = IdentityKeypair::generate().unwrap();
        let rotation = IdentityRotation::create(&remote, replacement.public_key())
            .unwrap()
            .encode()
            .unwrap();
        let (auth, token) = DaemonLocalAuth::initialize().unwrap();
        let server = DaemonServer::bind_with_identity_keystore(&runtime, auth, keystore).unwrap();
        let socket_path = server.socket_path().to_path_buf();
        let (shutdown_sender, shutdown_receiver) = oneshot::channel();

        let server = server.serve_until(async move {
            let _ = shutdown_receiver.await;
        });
        let client = async {
            let mut client = contact_client(socket_path.clone()).await;
            let _ = import_and_load_pending_contact(
                &mut client,
                &token,
                &invitation,
                &remote.public_key(),
                0,
            )
            .await;
            reject_pending_rotation_and_revoke_contact(
                &mut client,
                &token,
                &remote.public_key(),
                rotation,
            )
            .await;
            shutdown_sender.send(()).unwrap();
        };
        let (server, ()) = tokio::join!(server, client);
        assert!(server.is_ok());
        assert!(!socket_path.exists());
        runtime.shutdown().unwrap();
        fs::remove_dir_all(state_directory).unwrap();
    }

    #[tokio::test]
    async fn serves_authenticated_contact_verification_with_bounded_inputs() {
        let state_directory = state_directory();
        let mut runtime = DaemonRuntime::start(ProtocolVersion::INITIAL, &state_directory).unwrap();
        let mut keystore = MemoryKeystore::default();
        let local_identity = ClientIdentity::create(&mut keystore).unwrap().public_key();
        let qr_remote = IdentityKeypair::generate().unwrap();
        let qr_invitation = ContactInvitation::create(&qr_remote)
            .unwrap()
            .encode()
            .unwrap();
        let qr_payload = QrVerificationPayload::new(local_identity, qr_remote.public_key())
            .unwrap()
            .encode()
            .unwrap();
        let safety_remote = IdentityKeypair::generate().unwrap();
        let safety_invitation = ContactInvitation::create(&safety_remote)
            .unwrap()
            .encode()
            .unwrap();
        let safety_fingerprint =
            SafetyNumberFingerprint::derive(&local_identity, &safety_remote.public_key()).unwrap();
        let (auth, token) = DaemonLocalAuth::initialize().unwrap();
        let server = DaemonServer::bind_with_identity_keystore(&runtime, auth, keystore).unwrap();
        let socket_path = server.socket_path().to_path_buf();
        let (shutdown_sender, shutdown_receiver) = oneshot::channel();

        let server = server.serve_until(async move {
            let _ = shutdown_receiver.await;
        });
        let client = async {
            let mut client = contact_client(socket_path.clone()).await;
            let _ = import_and_load_pending_contact(
                &mut client,
                &token,
                &qr_invitation,
                &qr_remote.public_key(),
                0,
            )
            .await;
            let verified_qr =
                verify_qr_contact(&mut client, &token, &qr_payload, &qr_remote.public_key()).await;
            let _ = import_and_load_pending_contact(
                &mut client,
                &token,
                &safety_invitation,
                &safety_remote.public_key(),
                1,
            )
            .await;
            let verified_safety = verify_safety_number_contact(
                &mut client,
                &token,
                &safety_remote.public_key(),
                safety_fingerprint.as_bytes(),
            )
            .await;
            let contacts = client
                .list_contacts(authenticated_request(ListContactsRequest {}, &token))
                .await
                .unwrap()
                .into_inner()
                .contacts;
            assert_eq!(contacts.len(), 2);
            assert!(contacts.contains(&verified_qr));
            assert!(contacts.contains(&verified_safety));
            shutdown_sender.send(()).unwrap();
        };
        let (server, ()) = tokio::join!(server, client);
        assert!(server.is_ok());
        assert!(!socket_path.exists());
        runtime.shutdown().unwrap();
        fs::remove_dir_all(state_directory).unwrap();
    }

    #[tokio::test]
    async fn queues_authenticated_messages_with_bounded_inputs() {
        let state_directory = state_directory();
        let mut runtime = DaemonRuntime::start(ProtocolVersion::INITIAL, &state_directory).unwrap();
        let recipient = IdentityKeypair::generate().unwrap();
        let envelope = EncryptedMessageEnvelope::new(vec![0xa1], vec![0xb2])
            .unwrap()
            .encode()
            .unwrap();
        let (auth, token) = DaemonLocalAuth::initialize().unwrap();
        let server =
            DaemonServer::bind_with_identity_keystore(&runtime, auth, MemoryKeystore::default())
                .unwrap();
        let socket_path = server.socket_path().to_path_buf();
        let outbox_path = runtime.state_directory().outbox_path();
        let (shutdown_sender, shutdown_receiver) = oneshot::channel();

        let server = server.serve_until(async move {
            let _ = shutdown_receiver.await;
        });
        let client = async {
            let mut client = contact_client(socket_path.clone()).await;
            let request = SendMessageRequest {
                recipient: recipient.public_key().as_bytes().to_vec(),
                envelope: envelope.clone(),
                created_at: 100,
                ttl_seconds: 60,
            };
            assert_eq!(
                client
                    .send_message(Request::new(request.clone()))
                    .await
                    .unwrap_err()
                    .code(),
                Code::Unauthenticated
            );
            assert_eq!(
                client
                    .send_message(authenticated_request(
                        SendMessageRequest {
                            recipient: vec![0; 31],
                            ..request.clone()
                        },
                        &token,
                    ))
                    .await
                    .unwrap_err()
                    .code(),
                Code::InvalidArgument
            );
            assert_eq!(
                client
                    .send_message(authenticated_request(
                        SendMessageRequest {
                            envelope: vec![0; envelope.len()],
                            ..request.clone()
                        },
                        &token,
                    ))
                    .await
                    .unwrap_err()
                    .code(),
                Code::InvalidArgument
            );
            assert_eq!(
                client
                    .send_message(authenticated_request(
                        SendMessageRequest {
                            ttl_seconds: 0,
                            ..request.clone()
                        },
                        &token,
                    ))
                    .await
                    .unwrap_err()
                    .code(),
                Code::InvalidArgument
            );
            let first = client
                .send_message(authenticated_request(request.clone(), &token))
                .await
                .unwrap()
                .into_inner();
            assert_queued_delivery_status(&mut client, &token, &first.message_identifier).await;
            let second = client
                .send_message(authenticated_request(request, &token))
                .await
                .unwrap()
                .into_inner();
            assert_eq!(first.message_identifier.len(), MESSAGE_IDENTIFIER_BYTES);
            assert_eq!(second.message_identifier.len(), MESSAGE_IDENTIFIER_BYTES);
            assert_ne!(first.message_identifier, second.message_identifier);
            shutdown_sender.send(()).unwrap();
        };
        let (server, ()) = tokio::join!(server, client);
        assert!(server.is_ok());
        assert!(outbox_path.is_file());
        assert!(!socket_path.exists());
        runtime.shutdown().unwrap();
        fs::remove_dir_all(state_directory).unwrap();
    }

    #[tokio::test]
    async fn maps_daemon_failures_to_authenticated_grpc_statuses() {
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
            let mut client = contact_client(socket_path.clone()).await;
            assert_eq!(
                client
                    .list_contacts(authenticated_request(ListContactsRequest {}, &token))
                    .await
                    .unwrap_err()
                    .code(),
                Code::FailedPrecondition
            );
            assert_eq!(
                client
                    .send_message(authenticated_request(
                        SendMessageRequest {
                            recipient: vec![0; 31],
                            envelope: vec![0xa1],
                            created_at: 100,
                            ttl_seconds: 60,
                        },
                        &token,
                    ))
                    .await
                    .unwrap_err()
                    .code(),
                Code::InvalidArgument
            );
            let identifier = MessageIdentifier::generate().unwrap();
            assert_eq!(
                client
                    .get_delivery_status(authenticated_request(
                        GetDeliveryStatusRequest {
                            message_identifier: identifier.as_bytes().to_vec(),
                        },
                        &token,
                    ))
                    .await
                    .unwrap_err()
                    .code(),
                Code::NotFound
            );
            shutdown_sender.send(()).unwrap();
        };
        let (server, ()) = tokio::join!(server, client);
        assert!(server.is_ok());
        assert!(!socket_path.exists());
        runtime.shutdown().unwrap();
        fs::remove_dir_all(state_directory).unwrap();
    }
}
