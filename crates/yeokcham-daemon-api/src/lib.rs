#![forbid(unsafe_code)]

#[allow(clippy::all, clippy::pedantic, clippy::nursery)]
pub mod v1 {
    include!("generated/yeokcham.daemon.v1.rs");
}

#[cfg(test)]
mod tests {
    use super::v1::{
        GetStatusResponse, IdentityInitialization, IdentityResponse, StartClientResponse,
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
}
