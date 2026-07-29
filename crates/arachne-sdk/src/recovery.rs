use std::fmt;

use arachne_protocol::{
    IDENTITY_EXPORT_BYTES, IdentityExportPassphrase, IdentityExportPassphraseError,
    MAX_IDENTITY_EXPORT_PASSPHRASE_BYTES,
};

pub const MAX_SDK_RECOVERY_PASSPHRASE_BYTES: usize = MAX_IDENTITY_EXPORT_PASSPHRASE_BYTES;

pub struct SdkRecoveryPassphrase(IdentityExportPassphrase);

impl SdkRecoveryPassphrase {
    pub fn new(passphrase: Vec<u8>) -> Result<Self, SdkRecoveryPassphraseError> {
        IdentityExportPassphrase::new(passphrase)
            .map(Self)
            .map_err(SdkRecoveryPassphraseError::from)
    }

    pub(crate) const fn as_inner(&self) -> &IdentityExportPassphrase {
        &self.0
    }
}

impl fmt::Debug for SdkRecoveryPassphrase {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("SdkRecoveryPassphrase(REDACTED)")
    }
}

pub struct SdkRecoveryArchive(Vec<u8>);

impl SdkRecoveryArchive {
    pub fn from_bytes(bytes: Vec<u8>) -> Result<Self, SdkRecoveryArchiveError> {
        if bytes.len() != IDENTITY_EXPORT_BYTES {
            return Err(SdkRecoveryArchiveError::InvalidLength);
        }
        Ok(Self(bytes))
    }

    #[must_use]
    pub fn as_bytes(&self) -> &[u8] {
        &self.0
    }

    #[must_use]
    pub fn into_bytes(self) -> Vec<u8> {
        self.0
    }

    pub(crate) fn from_trusted_bytes(bytes: Vec<u8>) -> Self {
        debug_assert_eq!(bytes.len(), IDENTITY_EXPORT_BYTES);
        Self(bytes)
    }
}

impl fmt::Debug for SdkRecoveryArchive {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("SdkRecoveryArchive(REDACTED)")
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, thiserror::Error)]
pub enum SdkRecoveryPassphraseError {
    #[error("SDK recovery passphrase must not be empty")]
    Empty,
    #[error("SDK recovery passphrase exceeds the configured limit")]
    TooLong,
}

impl From<IdentityExportPassphraseError> for SdkRecoveryPassphraseError {
    fn from(error: IdentityExportPassphraseError) -> Self {
        match error {
            IdentityExportPassphraseError::Empty => Self::Empty,
            IdentityExportPassphraseError::TooLong => Self::TooLong,
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, thiserror::Error)]
pub enum SdkRecoveryArchiveError {
    #[error("SDK recovery archive has an invalid length")]
    InvalidLength,
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum SdkRecoveryError {
    #[error("SDK identity operation failed")]
    Identity(#[from] crate::SdkIdentityError),
    #[error("SDK recovery archive authentication failed")]
    InvalidArchive,
    #[error("SDK recovery export operation failed")]
    Operation,
}

#[cfg(test)]
mod tests {
    use super::{
        MAX_SDK_RECOVERY_PASSPHRASE_BYTES, SdkRecoveryArchive, SdkRecoveryArchiveError,
        SdkRecoveryPassphrase, SdkRecoveryPassphraseError,
    };

    #[test]
    fn validates_and_redacts_recovery_public_values() {
        let passphrase = SdkRecoveryPassphrase::new(b"recovery passphrase".to_vec()).unwrap();
        let archive =
            SdkRecoveryArchive::from_bytes(vec![0; arachne_protocol::IDENTITY_EXPORT_BYTES])
                .unwrap();

        assert_eq!(format!("{passphrase:?}"), "SdkRecoveryPassphrase(REDACTED)");
        assert_eq!(format!("{archive:?}"), "SdkRecoveryArchive(REDACTED)");
        assert_eq!(
            SdkRecoveryPassphrase::new(Vec::new()).unwrap_err(),
            SdkRecoveryPassphraseError::Empty
        );
        assert_eq!(
            SdkRecoveryPassphrase::new(vec![0; MAX_SDK_RECOVERY_PASSPHRASE_BYTES + 1]).unwrap_err(),
            SdkRecoveryPassphraseError::TooLong
        );
        assert_eq!(
            SdkRecoveryArchive::from_bytes(vec![0; 1]).unwrap_err(),
            SdkRecoveryArchiveError::InvalidLength
        );
    }
}
