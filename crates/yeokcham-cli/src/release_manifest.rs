use std::{
    fs::{self, File},
    io::Read,
    path::Path,
};

use minicbor::{Decoder, Encoder};
use sha2::{Digest, Sha256};
use thiserror::Error;
use yeokcham_core::{ED25519_SIGNATURE_BYTES, IdentityKeypair, IdentityPublicKey};

pub const MAX_RELEASE_ARTIFACTS: usize = 64;
pub const MAX_RELEASE_MANIFEST_BYTES: usize = 32 * 1024;
const RELEASE_MANIFEST_SCHEMA_VERSION: u8 = 1;
const RELEASE_MANIFEST_FIELDS: u64 = 6;
const RELEASE_MANIFEST_SIGNING_FIELDS: u64 = 5;
const RELEASE_ARTIFACT_FIELDS: u64 = 3;
const RELEASE_ARTIFACT_NAME_BYTES: usize = 128;
const RELEASE_ARTIFACT_SHA256_BYTES: usize = 32;
const SIGNING_DOMAIN: &[u8] = b"yeokcham/v1/release-artifact-manifest";

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ReleaseArtifact {
    name: String,
    bytes: u64,
    sha256: [u8; RELEASE_ARTIFACT_SHA256_BYTES],
}

impl ReleaseArtifact {
    pub fn from_path(path: &Path) -> Result<Self, ReleaseManifestError> {
        let metadata = fs::symlink_metadata(path)?;
        if !metadata.file_type().is_file() {
            return Err(ReleaseManifestError::InvalidArtifactFile);
        }
        let name = path
            .file_name()
            .and_then(|name| name.to_str())
            .ok_or(ReleaseManifestError::InvalidArtifactName)?;
        let mut file = File::open(path)?;
        let mut hash = Sha256::new();
        let mut bytes = 0_u64;
        let mut buffer = vec![0_u8; 32 * 1024];
        loop {
            let read = file.read(&mut buffer)?;
            if read == 0 {
                break;
            }
            bytes = bytes
                .checked_add(u64::try_from(read).map_err(|_| ReleaseManifestError::Length)?)
                .ok_or(ReleaseManifestError::Length)?;
            hash.update(&buffer[..read]);
        }
        Self::from_parts(name.to_owned(), bytes, hash.finalize().into())
    }

    #[must_use]
    pub fn name(&self) -> &str {
        &self.name
    }

    #[must_use]
    pub const fn bytes(&self) -> u64 {
        self.bytes
    }

    #[must_use]
    pub const fn sha256(&self) -> &[u8; RELEASE_ARTIFACT_SHA256_BYTES] {
        &self.sha256
    }

    fn from_parts(
        name: String,
        bytes: u64,
        sha256: [u8; RELEASE_ARTIFACT_SHA256_BYTES],
    ) -> Result<Self, ReleaseManifestError> {
        if !is_canonical_artifact_name(&name) {
            return Err(ReleaseManifestError::InvalidArtifactName);
        }
        Ok(Self {
            name,
            bytes,
            sha256,
        })
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SignedReleaseArtifactManifest {
    source_revision: String,
    source_date_epoch: u64,
    artifacts: Vec<ReleaseArtifact>,
    signing_public_key: IdentityPublicKey,
    signature: [u8; ED25519_SIGNATURE_BYTES],
}

impl SignedReleaseArtifactManifest {
    pub fn load(path: &Path) -> Result<Self, ReleaseManifestError> {
        let metadata = fs::symlink_metadata(path)?;
        if !metadata.file_type().is_file() {
            return Err(ReleaseManifestError::InvalidManifestFile);
        }
        let length = usize::try_from(metadata.len()).map_err(|_| ReleaseManifestError::TooLarge)?;
        if length > MAX_RELEASE_MANIFEST_BYTES {
            return Err(ReleaseManifestError::TooLarge);
        }
        let encoded = fs::read(path)?;
        if encoded.len() > MAX_RELEASE_MANIFEST_BYTES {
            return Err(ReleaseManifestError::TooLarge);
        }
        Self::decode(&encoded)
    }

    pub fn sign(
        source_revision: &str,
        source_date_epoch: u64,
        mut artifacts: Vec<ReleaseArtifact>,
        signing_key: &IdentityKeypair,
    ) -> Result<Self, ReleaseManifestError> {
        artifacts.sort_unstable_by(|left, right| left.name.cmp(&right.name));
        let signing_public_key = signing_key.public_key();
        let mut manifest = Self {
            source_revision: source_revision.to_owned(),
            source_date_epoch,
            artifacts,
            signing_public_key,
            signature: [0; ED25519_SIGNATURE_BYTES],
        };
        manifest.validate()?;
        manifest.signature = signing_key.sign(&manifest.signing_input()?);
        Ok(manifest)
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, ReleaseManifestError> {
        if encoded.len() > MAX_RELEASE_MANIFEST_BYTES {
            return Err(ReleaseManifestError::TooLarge);
        }
        let mut decoder = Decoder::new(encoded);
        expect_array(&mut decoder, RELEASE_MANIFEST_FIELDS)?;
        let version = decoder
            .u8()
            .map_err(|_| ReleaseManifestError::InvalidEncoding)?;
        if version != RELEASE_MANIFEST_SCHEMA_VERSION {
            return Err(ReleaseManifestError::UnsupportedSchemaVersion(version));
        }
        let source_revision = decoder
            .str()
            .map_err(|_| ReleaseManifestError::InvalidEncoding)?
            .to_owned();
        let source_date_epoch = decoder
            .u64()
            .map_err(|_| ReleaseManifestError::InvalidEncoding)?;
        let artifacts = decode_artifacts(&mut decoder)?;
        let signing_public_key = IdentityPublicKey::from_bytes(decode_array(
            decoder
                .bytes()
                .map_err(|_| ReleaseManifestError::InvalidEncoding)?,
        )?)
        .map_err(|_| ReleaseManifestError::InvalidPublicKey)?;
        let signature = decode_array(
            decoder
                .bytes()
                .map_err(|_| ReleaseManifestError::InvalidEncoding)?,
        )?;
        if decoder.position() != encoded.len() {
            return Err(ReleaseManifestError::TrailingBytes);
        }
        let manifest = Self {
            source_revision,
            source_date_epoch,
            artifacts,
            signing_public_key,
            signature,
        };
        manifest.validate()?;
        if manifest.encode()? != encoded {
            return Err(ReleaseManifestError::NonCanonicalEncoding);
        }
        Ok(manifest)
    }

    pub fn encode(&self) -> Result<Vec<u8>, ReleaseManifestError> {
        self.validate()?;
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(RELEASE_MANIFEST_FIELDS)
            .map_err(|_| ReleaseManifestError::Encode)?
            .u8(RELEASE_MANIFEST_SCHEMA_VERSION)
            .map_err(|_| ReleaseManifestError::Encode)?;
        encode_signing_fields(&mut encoder, self)?;
        encoder
            .bytes(&self.signature)
            .map_err(|_| ReleaseManifestError::Encode)?;
        let serialized = encoder.into_writer();
        if serialized.len() > MAX_RELEASE_MANIFEST_BYTES {
            return Err(ReleaseManifestError::TooLarge);
        }
        Ok(serialized)
    }

    pub fn verify(
        &self,
        trusted_public_key: &IdentityPublicKey,
    ) -> Result<(), ReleaseManifestError> {
        self.validate()?;
        if &self.signing_public_key != trusted_public_key {
            return Err(ReleaseManifestError::UntrustedPublicKey);
        }
        trusted_public_key
            .verify(&self.signing_input()?, &self.signature)
            .map_err(|_| ReleaseManifestError::InvalidSignature)
    }

    pub fn verify_artifacts(&self, directory: &Path) -> Result<(), ReleaseManifestError> {
        let metadata = fs::symlink_metadata(directory)?;
        if !metadata.file_type().is_dir() {
            return Err(ReleaseManifestError::InvalidArtifactDirectory);
        }
        for expected in &self.artifacts {
            let actual = ReleaseArtifact::from_path(&directory.join(expected.name()))?;
            if actual != *expected {
                return Err(ReleaseManifestError::ArtifactMismatch);
            }
        }
        Ok(())
    }

    #[must_use]
    pub fn source_revision(&self) -> &str {
        &self.source_revision
    }

    #[must_use]
    pub const fn source_date_epoch(&self) -> u64 {
        self.source_date_epoch
    }

    #[must_use]
    pub fn artifacts(&self) -> &[ReleaseArtifact] {
        &self.artifacts
    }

    #[must_use]
    pub const fn signing_public_key(&self) -> &IdentityPublicKey {
        &self.signing_public_key
    }

    fn signing_input(&self) -> Result<Vec<u8>, ReleaseManifestError> {
        let mut input = Vec::with_capacity(SIGNING_DOMAIN.len() + MAX_RELEASE_MANIFEST_BYTES);
        input.extend_from_slice(SIGNING_DOMAIN);
        let mut encoder = Encoder::new(input);
        encoder
            .array(RELEASE_MANIFEST_SIGNING_FIELDS)
            .map_err(|_| ReleaseManifestError::Encode)?
            .u8(RELEASE_MANIFEST_SCHEMA_VERSION)
            .map_err(|_| ReleaseManifestError::Encode)?;
        encode_signing_fields(&mut encoder, self)?;
        Ok(encoder.into_writer())
    }

    fn validate(&self) -> Result<(), ReleaseManifestError> {
        if !is_canonical_revision(&self.source_revision) {
            return Err(ReleaseManifestError::InvalidSourceRevision);
        }
        if self.artifacts.is_empty() || self.artifacts.len() > MAX_RELEASE_ARTIFACTS {
            return Err(ReleaseManifestError::InvalidArtifactCount);
        }
        for pair in self.artifacts.windows(2) {
            if pair[0].name >= pair[1].name {
                return Err(ReleaseManifestError::NonCanonicalArtifacts);
            }
        }
        Ok(())
    }
}

#[derive(Debug, Error)]
pub enum ReleaseManifestError {
    #[error("release artifact manifest I/O failed")]
    Io(#[from] std::io::Error),
    #[error("release artifact manifest artifact file is invalid")]
    InvalidArtifactFile,
    #[error("release artifact manifest file is invalid")]
    InvalidManifestFile,
    #[error("release artifact manifest artifact directory is invalid")]
    InvalidArtifactDirectory,
    #[error("release artifact manifest artifact name is invalid")]
    InvalidArtifactName,
    #[error("release artifact manifest source revision is invalid")]
    InvalidSourceRevision,
    #[error("release artifact manifest artifact count is invalid")]
    InvalidArtifactCount,
    #[error("release artifact manifest length overflowed")]
    Length,
    #[error("release artifact manifest encoding is invalid")]
    InvalidEncoding,
    #[error("release artifact manifest public key is invalid")]
    InvalidPublicKey,
    #[error("release artifact manifest schema version is unsupported: {0}")]
    UnsupportedSchemaVersion(u8),
    #[error("release artifact manifest has trailing bytes")]
    TrailingBytes,
    #[error("release artifact manifest is not canonically encoded")]
    NonCanonicalEncoding,
    #[error("release artifact manifest artifacts are not in canonical order")]
    NonCanonicalArtifacts,
    #[error("release artifact manifest exceeds the configured limit")]
    TooLarge,
    #[error("release artifact manifest could not be encoded")]
    Encode,
    #[error("release artifact manifest signing key is not trusted")]
    UntrustedPublicKey,
    #[error("release artifact manifest signature is invalid")]
    InvalidSignature,
    #[error("release artifact does not match its manifest entry")]
    ArtifactMismatch,
}

fn encode_signing_fields(
    encoder: &mut Encoder<Vec<u8>>,
    manifest: &SignedReleaseArtifactManifest,
) -> Result<(), ReleaseManifestError> {
    encoder
        .str(&manifest.source_revision)
        .map_err(|_| ReleaseManifestError::Encode)?
        .u64(manifest.source_date_epoch)
        .map_err(|_| ReleaseManifestError::Encode)?
        .array(u64::try_from(manifest.artifacts.len()).map_err(|_| ReleaseManifestError::Encode)?)
        .map_err(|_| ReleaseManifestError::Encode)?;
    for artifact in &manifest.artifacts {
        encoder
            .array(RELEASE_ARTIFACT_FIELDS)
            .map_err(|_| ReleaseManifestError::Encode)?
            .str(artifact.name())
            .map_err(|_| ReleaseManifestError::Encode)?
            .u64(artifact.bytes())
            .map_err(|_| ReleaseManifestError::Encode)?
            .bytes(artifact.sha256())
            .map_err(|_| ReleaseManifestError::Encode)?;
    }
    encoder
        .bytes(manifest.signing_public_key.as_bytes())
        .map_err(|_| ReleaseManifestError::Encode)?;
    Ok(())
}

fn decode_artifacts(
    decoder: &mut Decoder<'_>,
) -> Result<Vec<ReleaseArtifact>, ReleaseManifestError> {
    let count = decoder
        .array()
        .map_err(|_| ReleaseManifestError::InvalidEncoding)?
        .ok_or(ReleaseManifestError::InvalidEncoding)?;
    let count = usize::try_from(count).map_err(|_| ReleaseManifestError::InvalidArtifactCount)?;
    if count == 0 || count > MAX_RELEASE_ARTIFACTS {
        return Err(ReleaseManifestError::InvalidArtifactCount);
    }
    let mut artifacts = Vec::with_capacity(count);
    for _ in 0..count {
        expect_array(decoder, RELEASE_ARTIFACT_FIELDS)?;
        let name = decoder
            .str()
            .map_err(|_| ReleaseManifestError::InvalidEncoding)?
            .to_owned();
        let bytes = decoder
            .u64()
            .map_err(|_| ReleaseManifestError::InvalidEncoding)?;
        let sha256 = decode_array(
            decoder
                .bytes()
                .map_err(|_| ReleaseManifestError::InvalidEncoding)?,
        )?;
        artifacts.push(ReleaseArtifact::from_parts(name, bytes, sha256)?);
    }
    Ok(artifacts)
}

fn expect_array(decoder: &mut Decoder<'_>, fields: u64) -> Result<(), ReleaseManifestError> {
    if decoder
        .array()
        .map_err(|_| ReleaseManifestError::InvalidEncoding)?
        != Some(fields)
    {
        return Err(ReleaseManifestError::InvalidEncoding);
    }
    Ok(())
}

fn decode_array<const N: usize>(encoded: &[u8]) -> Result<[u8; N], ReleaseManifestError> {
    encoded
        .try_into()
        .map_err(|_| ReleaseManifestError::InvalidEncoding)
}

fn is_canonical_revision(revision: &str) -> bool {
    matches!(revision.len(), 40 | 64)
        && revision
            .bytes()
            .all(|byte| byte.is_ascii_digit() || matches!(byte, b'a'..=b'f'))
}

fn is_canonical_artifact_name(name: &str) -> bool {
    !name.is_empty()
        && name.len() <= RELEASE_ARTIFACT_NAME_BYTES
        && name != "."
        && name != ".."
        && name.bytes().all(|byte| {
            byte.is_ascii_lowercase() || byte.is_ascii_digit() || matches!(byte, b'.' | b'_' | b'-')
        })
}

#[cfg(test)]
mod tests {
    use std::{fs, path::PathBuf};

    use yeokcham_core::IdentityKeypair;

    use super::{
        ReleaseArtifact, ReleaseManifestError, SignedReleaseArtifactManifest,
        is_canonical_artifact_name,
    };

    const REVISION: &str = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";

    fn artifact(name: &str, bytes: u64, hash: u8) -> ReleaseArtifact {
        ReleaseArtifact::from_parts(name.to_owned(), bytes, [hash; 32]).unwrap()
    }

    #[test]
    fn signs_canonical_manifest_and_pins_the_expected_public_key() {
        let signer = IdentityKeypair::generate().unwrap();
        let unrelated = IdentityKeypair::generate().unwrap();
        let manifest = SignedReleaseArtifactManifest::sign(
            REVISION,
            123,
            vec![
                artifact("release-metadata.json", 9, 2),
                artifact("yeokcham", 8, 1),
            ],
            &signer,
        )
        .unwrap();
        let encoded = manifest.encode().unwrap();
        let decoded = SignedReleaseArtifactManifest::decode(&encoded).unwrap();

        assert_eq!(decoded, manifest);
        assert_eq!(decoded.artifacts()[0].name(), "release-metadata.json");
        assert!(decoded.verify(&signer.public_key()).is_ok());
        assert!(matches!(
            decoded.verify(&unrelated.public_key()),
            Err(ReleaseManifestError::UntrustedPublicKey)
        ));
    }

    #[test]
    fn rejects_tampered_and_noncanonical_manifests() {
        let signer = IdentityKeypair::generate().unwrap();
        let manifest = SignedReleaseArtifactManifest::sign(
            REVISION,
            123,
            vec![artifact("yeokcham", 8, 1)],
            &signer,
        )
        .unwrap();
        let mut tampered = manifest.encode().unwrap();
        let last = tampered.len() - 1;
        tampered[last] ^= 1;
        let tampered = SignedReleaseArtifactManifest::decode(&tampered).unwrap();
        assert!(matches!(
            tampered.verify(&signer.public_key()),
            Err(ReleaseManifestError::InvalidSignature)
        ));
        let mut trailing = manifest.encode().unwrap();
        trailing.push(0);
        assert!(matches!(
            SignedReleaseArtifactManifest::decode(&trailing),
            Err(ReleaseManifestError::TrailingBytes)
        ));
    }

    #[test]
    fn verifies_regular_artifacts_and_rejects_tampering() {
        let root =
            std::env::temp_dir().join(format!("yeokcham-release-manifest-{}", std::process::id()));
        let _ = fs::remove_dir_all(&root);
        fs::create_dir(&root).unwrap();
        let path = root.join("yeokcham");
        fs::write(&path, b"release").unwrap();
        let artifact = ReleaseArtifact::from_path(&path).unwrap();
        let signer = IdentityKeypair::generate().unwrap();
        let manifest =
            SignedReleaseArtifactManifest::sign(REVISION, 123, vec![artifact], &signer).unwrap();

        assert!(manifest.verify_artifacts(&root).is_ok());
        fs::write(&path, b"tampered").unwrap();
        assert!(matches!(
            manifest.verify_artifacts(&root),
            Err(ReleaseManifestError::ArtifactMismatch)
        ));
        fs::remove_dir_all(root).unwrap();
    }

    #[test]
    fn rejects_unsafe_artifact_names() {
        for name in ["../yeokcham", "yeokcham\\x", "Yeokcham", "yeok/cham"] {
            assert!(!is_canonical_artifact_name(name));
        }
        assert!(is_canonical_artifact_name("yeokcham-linux-x86_64"));
        assert!(ReleaseArtifact::from_path(&PathBuf::from("../yeokcham")).is_err());
    }
}
