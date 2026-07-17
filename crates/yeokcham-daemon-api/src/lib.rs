#![forbid(unsafe_code)]

#[allow(clippy::all, clippy::pedantic, clippy::nursery)]
pub mod v1 {
    include!("generated/yeokcham.daemon.v1.rs");
}

#[cfg(test)]
mod tests {
    use super::v1::{
        AttachmentTransferResponse, ContactResponse, ContactStatus, ContactVerificationMethod,
        DaemonEvent, DaemonEventKind, DeliveryProfileKind, DeliveryProfileResponse, DeliveryStatus,
        ExportIdentityRecoveryRequest, ExportIdentityRecoveryResponse,
        GetAttachmentTransferRequest, GetDeliveryStatusRequest, GetDeliveryStatusResponse,
        GetStatusResponse, IdentityInitialization, IdentityResponse, ImportIdentityRecoveryRequest,
        LocalMeshTransportKind, QueueAttachmentRequest, SelectDeliveryProfileRequest,
        SendMessageRequest, SendMessageResponse, ShutdownDaemonResponse, StartClientResponse,
        VerifyContactQrRequest, VerifyContactSafetyNumberRequest,
        queue_attachment_request::Record as AttachmentRecord,
    };

    #[test]
    fn generated_status_contract_preserves_every_field() {
        let response = GetStatusResponse {
            api_major: 0,
            api_minor: 1,
            running: true,
        };
        assert_eq!(response.api_major, 0);
        assert_eq!(response.api_minor, 1);
        assert!(response.running);
    }

    #[test]
    fn generated_client_start_contract_preserves_every_field() {
        let response = StartClientResponse {
            api_major: 0,
            api_minor: 1,
            running: true,
        };
        assert_eq!(response.api_major, 0);
        assert_eq!(response.api_minor, 1);
        assert!(response.running);
    }

    #[test]
    fn generated_shutdown_contract_preserves_every_field() {
        let response = ShutdownDaemonResponse { running: false };
        assert!(!response.running);
    }

    #[test]
    fn generated_identity_contract_preserves_every_field() {
        let response = IdentityResponse {
            public_key: vec![1; 32],
            initialization: IdentityInitialization::Created.into(),
        };
        assert_eq!(response.public_key, vec![1; 32]);
        assert_eq!(
            IdentityInitialization::try_from(response.initialization),
            Ok(IdentityInitialization::Created)
        );
    }

    #[test]
    fn generated_identity_recovery_contract_preserves_every_field() {
        let export = ExportIdentityRecoveryRequest {
            passphrase: vec![1; 3],
        };
        let archive = ExportIdentityRecoveryResponse {
            archive: vec![2; 122],
        };
        let import = ImportIdentityRecoveryRequest {
            archive: archive.archive.clone(),
            passphrase: export.passphrase.clone(),
        };
        assert_eq!(export.passphrase, vec![1; 3]);
        assert_eq!(archive.archive, vec![2; 122]);
        assert_eq!(import.archive, archive.archive);
        assert_eq!(import.passphrase, export.passphrase);
    }

    #[test]
    fn generated_attachment_contract_preserves_every_field() {
        let manifest = QueueAttachmentRequest {
            record: Some(AttachmentRecord::Manifest(vec![1; 5])),
        };
        let chunk = QueueAttachmentRequest {
            record: Some(AttachmentRecord::Chunk(vec![2; 6])),
        };
        let request = GetAttachmentTransferRequest {
            attachment_identifier: vec![3; 16],
        };
        let response = AttachmentTransferResponse {
            attachment_identifier: request.attachment_identifier,
            chunk_count: 4,
            complete: false,
            next_pending_index: 2,
        };
        assert_eq!(
            manifest.record,
            Some(AttachmentRecord::Manifest(vec![1; 5]))
        );
        assert_eq!(chunk.record, Some(AttachmentRecord::Chunk(vec![2; 6])));
        assert_eq!(response.attachment_identifier, vec![3; 16]);
        assert_eq!(response.chunk_count, 4);
        assert!(!response.complete);
        assert_eq!(response.next_pending_index, 2);
    }

    #[test]
    fn generated_contact_contract_preserves_every_field() {
        let response = ContactResponse {
            identity: vec![1; 32],
            status: ContactStatus::Pending.into(),
            verification_method: ContactVerificationMethod::Unspecified.into(),
        };
        assert_eq!(response.identity, vec![1; 32]);
        assert_eq!(
            ContactStatus::try_from(response.status),
            Ok(ContactStatus::Pending)
        );
        assert_eq!(
            ContactVerificationMethod::try_from(response.verification_method),
            Ok(ContactVerificationMethod::Unspecified)
        );
    }

    #[test]
    fn generated_contact_verification_contract_preserves_every_field() {
        let qr = VerifyContactQrRequest {
            payload: vec![1; 70],
        };
        let safety_number = VerifyContactSafetyNumberRequest {
            identity: vec![2; 32],
            fingerprint: vec![3; 32],
        };
        assert_eq!(qr.payload, vec![1; 70]);
        assert_eq!(safety_number.identity, vec![2; 32]);
        assert_eq!(safety_number.fingerprint, vec![3; 32]);
    }

    #[test]
    fn generated_message_send_contract_preserves_every_field() {
        let request = SendMessageRequest {
            recipient: vec![1; 32],
            envelope: vec![2; 6],
            created_at: 3,
            ttl_seconds: 4,
        };
        let response = SendMessageResponse {
            message_identifier: vec![5; 16],
        };
        assert_eq!(request.recipient, vec![1; 32]);
        assert_eq!(request.envelope, vec![2; 6]);
        assert_eq!(request.created_at, 3);
        assert_eq!(request.ttl_seconds, 4);
        assert_eq!(response.message_identifier, vec![5; 16]);
    }

    #[test]
    fn generated_delivery_status_contract_preserves_every_field() {
        let request = GetDeliveryStatusRequest {
            message_identifier: vec![1; 16],
        };
        let response = GetDeliveryStatusResponse {
            status: DeliveryStatus::Queued.into(),
        };
        assert_eq!(request.message_identifier, vec![1; 16]);
        assert_eq!(
            DeliveryStatus::try_from(response.status),
            Ok(DeliveryStatus::Queued)
        );
    }

    #[test]
    fn generated_delivery_profile_contract_preserves_every_field() {
        let request = SelectDeliveryProfileRequest {
            direct_allowed: true,
            tor_maildrop_allowed: false,
            allowed_local_mesh_transports: vec![LocalMeshTransportKind::Lan.into()],
            kind: DeliveryProfileKind::Direct.into(),
            local_mesh_transport: LocalMeshTransportKind::Unspecified.into(),
            direct_ip_disclosure_acknowledged: true,
        };
        let response = DeliveryProfileResponse {
            kind: DeliveryProfileKind::Direct.into(),
            direct_ip_disclosure_warning: true,
        };
        assert!(request.direct_allowed);
        assert!(!request.tor_maildrop_allowed);
        assert_eq!(
            LocalMeshTransportKind::try_from(request.allowed_local_mesh_transports[0]),
            Ok(LocalMeshTransportKind::Lan)
        );
        assert_eq!(
            DeliveryProfileKind::try_from(response.kind),
            Ok(DeliveryProfileKind::Direct)
        );
        assert!(response.direct_ip_disclosure_warning);
    }

    #[test]
    fn generated_event_contract_preserves_every_field() {
        let event = DaemonEvent {
            version: 1,
            sequence: 2,
            kind: DaemonEventKind::MessageQueued.into(),
            message_identifier: vec![3; 16],
        };
        assert_eq!(event.version, 1);
        assert_eq!(event.sequence, 2);
        assert_eq!(
            DaemonEventKind::try_from(event.kind),
            Ok(DaemonEventKind::MessageQueued)
        );
        assert_eq!(event.message_identifier, vec![3; 16]);
    }
}
