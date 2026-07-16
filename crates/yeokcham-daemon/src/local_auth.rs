use std::{
    fmt,
    sync::{Arc, Mutex},
};

use getrandom::{SysRng, rand_core::TryRng};
use subtle::ConstantTimeEq;
use tonic::{
    Request, Status,
    metadata::{Binary, MetadataMap, MetadataValue},
    service::Interceptor,
};
use zeroize::Zeroizing;

pub const LOCAL_AUTH_TOKEN_BYTES: usize = 32;
pub const LOCAL_AUTH_TOKEN_METADATA_KEY: &str = "x-yeokcham-local-auth-bin";
pub const LOCAL_AUTH_TOKEN_VERSION: u8 = 1;
pub const MAX_LOCAL_AUTH_METADATA_BYTES: usize = 64;

#[derive(Clone)]
pub struct DaemonLocalAuth {
    token: Arc<Mutex<Option<Zeroizing<[u8; LOCAL_AUTH_TOKEN_BYTES]>>>>,
}

pub struct DaemonLocalAuthToken(Zeroizing<[u8; LOCAL_AUTH_TOKEN_BYTES]>);

#[derive(Clone, Debug)]
pub struct DaemonLocalAuthInterceptor {
    auth: DaemonLocalAuth,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, thiserror::Error)]
pub enum DaemonLocalAuthError {
    #[error("local authentication failed")]
    Unauthorized,
    #[error("local authentication is unavailable")]
    State,
    #[error("local authentication token could not be generated")]
    Randomness,
}

impl DaemonLocalAuth {
    pub fn initialize() -> Result<(Self, DaemonLocalAuthToken), DaemonLocalAuthError> {
        let token = generate_token()?;
        let active_token = Zeroizing::new(*token.as_bytes());
        Ok((
            Self {
                token: Arc::new(Mutex::new(Some(active_token))),
            },
            token,
        ))
    }

    pub fn authorize(&self, presented: &[u8]) -> Result<(), DaemonLocalAuthError> {
        if presented.len() != LOCAL_AUTH_TOKEN_BYTES
            || presented.first() != Some(&LOCAL_AUTH_TOKEN_VERSION)
        {
            return Err(DaemonLocalAuthError::Unauthorized);
        }
        let authorized = {
            let token = self.token.lock().map_err(|_| DaemonLocalAuthError::State)?;
            let authorized = token.as_ref().is_some_and(|active| {
                let active: &[u8] = active.as_ref();
                bool::from(active.ct_eq(presented))
            });
            drop(token);
            authorized
        };
        authorized
            .then_some(())
            .ok_or(DaemonLocalAuthError::Unauthorized)
    }

    pub fn rotate(&self) -> Result<DaemonLocalAuthToken, DaemonLocalAuthError> {
        let token = generate_token()?;
        {
            let mut active = self.token.lock().map_err(|_| DaemonLocalAuthError::State)?;
            if active.is_none() {
                return Err(DaemonLocalAuthError::State);
            }
            *active = Some(Zeroizing::new(*token.as_bytes()));
        }
        Ok(token)
    }

    pub fn revoke(&self) -> Result<(), DaemonLocalAuthError> {
        let revoked = {
            let mut active = self.token.lock().map_err(|_| DaemonLocalAuthError::State)?;
            active.take().is_some()
        };
        revoked.then_some(()).ok_or(DaemonLocalAuthError::State)
    }

    #[must_use]
    pub fn interceptor(&self) -> DaemonLocalAuthInterceptor {
        DaemonLocalAuthInterceptor { auth: self.clone() }
    }

    fn authorize_metadata(&self, metadata: &MetadataMap) -> Result<(), DaemonLocalAuthError> {
        let mut values = metadata.get_all_bin(LOCAL_AUTH_TOKEN_METADATA_KEY).iter();
        let Some(value) = values.next() else {
            return Err(DaemonLocalAuthError::Unauthorized);
        };
        if values.next().is_some() || value.as_encoded_bytes().len() > MAX_LOCAL_AUTH_METADATA_BYTES
        {
            return Err(DaemonLocalAuthError::Unauthorized);
        }
        let presented = value
            .to_bytes()
            .map_err(|_| DaemonLocalAuthError::Unauthorized)?;
        self.authorize(presented.as_ref())
    }
}

impl DaemonLocalAuthToken {
    #[must_use]
    pub fn as_bytes(&self) -> &[u8; LOCAL_AUTH_TOKEN_BYTES] {
        &self.0
    }

    #[must_use]
    pub fn metadata_value(&self) -> MetadataValue<Binary> {
        let mut value = MetadataValue::from_bytes(self.as_bytes());
        value.set_sensitive(true);
        value
    }
}

impl Interceptor for DaemonLocalAuthInterceptor {
    fn call(&mut self, request: Request<()>) -> Result<Request<()>, Status> {
        self.auth
            .authorize_metadata(request.metadata())
            .map_err(|error| match error {
                DaemonLocalAuthError::Unauthorized => {
                    Status::unauthenticated("local authentication failed")
                }
                DaemonLocalAuthError::State | DaemonLocalAuthError::Randomness => {
                    Status::unavailable("local authentication unavailable")
                }
            })?;
        Ok(request)
    }
}

impl fmt::Debug for DaemonLocalAuth {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("DaemonLocalAuth(REDACTED)")
    }
}

impl fmt::Debug for DaemonLocalAuthToken {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("DaemonLocalAuthToken(REDACTED)")
    }
}

fn generate_token() -> Result<DaemonLocalAuthToken, DaemonLocalAuthError> {
    let mut bytes = Zeroizing::new([0; LOCAL_AUTH_TOKEN_BYTES]);
    let mut random_source = SysRng;
    random_source
        .try_fill_bytes(bytes.as_mut())
        .map_err(|_| DaemonLocalAuthError::Randomness)?;
    if bytes.iter().all(|byte| *byte == 0) {
        return Err(DaemonLocalAuthError::Randomness);
    }
    bytes[0] = LOCAL_AUTH_TOKEN_VERSION;
    Ok(DaemonLocalAuthToken(bytes))
}

#[cfg(test)]
mod tests {
    use tonic::{Code, Request, metadata::MetadataValue, service::Interceptor};

    use super::{
        DaemonLocalAuth, DaemonLocalAuthError, LOCAL_AUTH_TOKEN_BYTES,
        LOCAL_AUTH_TOKEN_METADATA_KEY, LOCAL_AUTH_TOKEN_VERSION, MAX_LOCAL_AUTH_METADATA_BYTES,
    };

    #[test]
    fn issues_rotates_and_revokes_redacted_bearer_tokens() {
        let (auth, token) = DaemonLocalAuth::initialize().unwrap();
        assert_eq!(token.as_bytes().len(), LOCAL_AUTH_TOKEN_BYTES);
        assert_eq!(token.as_bytes()[0], LOCAL_AUTH_TOKEN_VERSION);
        assert!(token.as_bytes().iter().any(|byte| *byte != 0));
        assert_eq!(auth.authorize(token.as_bytes()), Ok(()));
        assert_eq!(format!("{auth:?}"), "DaemonLocalAuth(REDACTED)");
        assert_eq!(format!("{token:?}"), "DaemonLocalAuthToken(REDACTED)");

        let replacement = auth.rotate().unwrap();
        assert_eq!(
            auth.authorize(token.as_bytes()),
            Err(DaemonLocalAuthError::Unauthorized)
        );
        assert_eq!(auth.authorize(replacement.as_bytes()), Ok(()));

        auth.revoke().unwrap();
        assert_eq!(
            auth.authorize(replacement.as_bytes()),
            Err(DaemonLocalAuthError::Unauthorized)
        );
        assert_eq!(auth.revoke(), Err(DaemonLocalAuthError::State));
        assert!(matches!(auth.rotate(), Err(DaemonLocalAuthError::State)));
    }

    #[test]
    fn rejects_invalid_length_and_value_without_leaking_authentication_state() {
        let (auth, token) = DaemonLocalAuth::initialize().unwrap();
        for presented in [
            &[][..],
            &[0; LOCAL_AUTH_TOKEN_BYTES - 1][..],
            &[0; LOCAL_AUTH_TOKEN_BYTES][..],
            &[0; LOCAL_AUTH_TOKEN_BYTES + 1][..],
        ] {
            assert_eq!(
                auth.authorize(presented),
                Err(DaemonLocalAuthError::Unauthorized)
            );
        }
        let mut unsupported_version = *token.as_bytes();
        unsupported_version[0] = LOCAL_AUTH_TOKEN_VERSION + 1;
        assert_eq!(
            auth.authorize(&unsupported_version),
            Err(DaemonLocalAuthError::Unauthorized)
        );
        assert_eq!(auth.authorize(token.as_bytes()), Ok(()));
    }

    #[test]
    fn interceptor_accepts_one_bounded_sensitive_binary_token() {
        let (auth, token) = DaemonLocalAuth::initialize().unwrap();
        let mut interceptor = auth.interceptor();

        let mut request = Request::new(());
        let value = token.metadata_value();
        assert!(value.is_sensitive());
        request
            .metadata_mut()
            .insert_bin(LOCAL_AUTH_TOKEN_METADATA_KEY, value);
        assert!(interceptor.call(request).is_ok());

        let missing = interceptor.call(Request::new(())).unwrap_err();
        assert_eq!(missing.code(), Code::Unauthenticated);
        assert_eq!(missing.message(), "local authentication failed");

        let mut duplicate = Request::new(());
        duplicate
            .metadata_mut()
            .append_bin(LOCAL_AUTH_TOKEN_METADATA_KEY, token.metadata_value());
        duplicate
            .metadata_mut()
            .append_bin(LOCAL_AUTH_TOKEN_METADATA_KEY, token.metadata_value());
        assert_eq!(
            interceptor.call(duplicate).unwrap_err().code(),
            Code::Unauthenticated
        );

        let mut oversized = Request::new(());
        oversized.metadata_mut().insert_bin(
            LOCAL_AUTH_TOKEN_METADATA_KEY,
            MetadataValue::from_bytes(&[0; MAX_LOCAL_AUTH_METADATA_BYTES]),
        );
        assert_eq!(
            interceptor.call(oversized).unwrap_err().code(),
            Code::Unauthenticated
        );
    }
}
