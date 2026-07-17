#![forbid(unsafe_code)]

#[allow(clippy::all, clippy::pedantic, clippy::nursery)]
pub mod v1 {
    include!("generated/yeokcham.daemon.v1.rs");
}

#[cfg(test)]
mod tests {
    use super::v1::{
        ContactResponse, ContactStatus, ContactVerificationMethod, DeliveryStatus,
        GetDeliveryStatusRequest, GetDeliveryStatusResponse, GetStatusResponse,
        IdentityInitialization, IdentityResponse, SendMessageRequest, SendMessageResponse,
        StartClientResponse, VerifyContactQrRequest, VerifyContactSafetyNumberRequest,
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
}
