use std::fmt::{Display, Formatter};
use std::fs::{self, OpenOptions};
use std::io::{self, Write};
use std::path::{Path, PathBuf};
use std::time::{SystemTime, UNIX_EPOCH};

use ed25519_dalek::pkcs8::{DecodePrivateKey, EncodePrivateKey, EncodePublicKey};
use ed25519_dalek::{SigningKey, VerifyingKey};
use rand_core::OsRng;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};

#[cfg(unix)]
use std::os::unix::fs::{OpenOptionsExt, PermissionsExt};

pub const PRIVATE_KEY_FILE: &str = "pi-ed25519.key.json";
pub const PUBLIC_KEY_FILE: &str = "pi-ed25519.pub.json";
pub const DEFAULT_KEY_LABEL: &str = "kelp-pi";
const KEY_SCHEMA_VERSION: u32 = 1;
const KEY_ALGORITHM: &str = "Ed25519";

#[derive(Debug)]
pub enum IdentityKeyError {
    Io(io::Error),
    Json(serde_json::Error),
    Invalid(String),
    Crypto(String),
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct IdentityKeyMetadata {
    pub schema_version: u32,
    pub key_id: String,
    pub algorithm: String,
    pub label: String,
    pub created_at_unix_ms: u64,
    pub rotation: u32,
    pub public_key_raw_hex: String,
    pub public_key_pkcs8_der_hex: String,
}

#[derive(Debug)]
pub struct IdentityKey {
    pub signing_key: SigningKey,
    pub metadata: IdentityKeyMetadata,
}

#[derive(Debug, Serialize, Deserialize)]
struct PrivateKeyFile {
    #[serde(flatten)]
    metadata: IdentityKeyMetadata,
    private_key_pkcs8_der_hex: String,
}

pub fn identity_key_paths(key_dir: &Path) -> (PathBuf, PathBuf) {
    (
        key_dir.join(PRIVATE_KEY_FILE),
        key_dir.join(PUBLIC_KEY_FILE),
    )
}

pub fn load_or_generate_identity_key(
    key_dir: &Path,
    label: &str,
) -> Result<IdentityKey, IdentityKeyError> {
    let (private_path, public_path) = identity_key_paths(key_dir);

    if private_path.exists() {
        let identity = load_identity_key(key_dir)?;
        ensure_public_key_file(&public_path, &identity.metadata)?;
        return Ok(identity);
    }

    if public_path.exists() {
        return Err(IdentityKeyError::Invalid(format!(
            "{} exists without {}",
            public_path.display(),
            private_path.display()
        )));
    }

    fs::create_dir_all(key_dir)?;
    generate_identity_key(key_dir, label)
}

pub fn load_identity_key(key_dir: &Path) -> Result<IdentityKey, IdentityKeyError> {
    let (private_path, public_path) = identity_key_paths(key_dir);
    let private_file: PrivateKeyFile = serde_json::from_slice(&fs::read(&private_path)?)?;
    validate_metadata_shape(&private_file.metadata)?;
    let private_der = decode_hex(&private_file.private_key_pkcs8_der_hex)?;
    let signing_key = SigningKey::from_pkcs8_der(&private_der)
        .map_err(|error| IdentityKeyError::Crypto(error.to_string()))?;
    let expected = metadata_for_signing_key(
        &signing_key,
        &private_file.metadata.label,
        private_file.metadata.created_at_unix_ms,
        private_file.metadata.rotation,
    )?;

    if private_file.metadata != expected {
        return Err(IdentityKeyError::Invalid(
            "private key metadata does not match key material".to_string(),
        ));
    }

    ensure_public_key_file(&public_path, &private_file.metadata)?;

    Ok(IdentityKey {
        signing_key,
        metadata: private_file.metadata,
    })
}

pub fn load_identity_public_metadata(
    key_dir: &Path,
) -> Result<IdentityKeyMetadata, IdentityKeyError> {
    let (_, public_path) = identity_key_paths(key_dir);
    let metadata: IdentityKeyMetadata = serde_json::from_slice(&fs::read(&public_path)?)?;
    validate_metadata_shape(&metadata)?;
    Ok(metadata)
}

pub fn verifying_key_from_metadata(
    metadata: &IdentityKeyMetadata,
) -> Result<VerifyingKey, IdentityKeyError> {
    let public_key = decode_hex(&metadata.public_key_raw_hex)?;
    VerifyingKey::try_from(public_key.as_slice())
        .map_err(|error| IdentityKeyError::Crypto(error.to_string()))
}

fn generate_identity_key(key_dir: &Path, label: &str) -> Result<IdentityKey, IdentityKeyError> {
    let mut rng = OsRng;
    let signing_key = SigningKey::generate(&mut rng);
    let metadata = metadata_for_signing_key(&signing_key, label, unix_millis(), 0)?;
    let private_der = signing_key
        .to_pkcs8_der()
        .map_err(|error| IdentityKeyError::Crypto(error.to_string()))?;
    let private_file = PrivateKeyFile {
        metadata: metadata.clone(),
        private_key_pkcs8_der_hex: encode_hex(private_der.as_bytes()),
    };
    let (_, public_path) = identity_key_paths(key_dir);

    write_json_new(&key_dir.join(PRIVATE_KEY_FILE), &private_file, 0o600)?;
    write_json_new(&public_path, &metadata, 0o644)?;

    Ok(IdentityKey {
        signing_key,
        metadata,
    })
}

fn ensure_public_key_file(
    public_path: &Path,
    metadata: &IdentityKeyMetadata,
) -> Result<(), IdentityKeyError> {
    match fs::read(public_path) {
        Ok(bytes) => {
            let existing: IdentityKeyMetadata = serde_json::from_slice(&bytes)?;
            validate_metadata_shape(&existing)?;
            if existing == *metadata {
                Ok(())
            } else {
                Err(IdentityKeyError::Invalid(format!(
                    "{} does not match private key metadata",
                    public_path.display()
                )))
            }
        }
        Err(error) if error.kind() == io::ErrorKind::NotFound => {
            write_json_new(public_path, metadata, 0o644)
        }
        Err(error) => Err(IdentityKeyError::Io(error)),
    }
}

fn metadata_for_signing_key(
    signing_key: &SigningKey,
    label: &str,
    created_at_unix_ms: u64,
    rotation: u32,
) -> Result<IdentityKeyMetadata, IdentityKeyError> {
    let verifying_key = signing_key.verifying_key();
    let public_key_raw = verifying_key.as_bytes();
    let public_key_der = verifying_key
        .to_public_key_der()
        .map_err(|error| IdentityKeyError::Crypto(error.to_string()))?;

    Ok(IdentityKeyMetadata {
        schema_version: KEY_SCHEMA_VERSION,
        key_id: key_id_for_public_key(public_key_raw),
        algorithm: KEY_ALGORITHM.to_string(),
        label: label.to_string(),
        created_at_unix_ms,
        rotation,
        public_key_raw_hex: encode_hex(public_key_raw),
        public_key_pkcs8_der_hex: encode_hex(public_key_der.as_bytes()),
    })
}

fn validate_metadata_shape(metadata: &IdentityKeyMetadata) -> Result<(), IdentityKeyError> {
    if metadata.schema_version != KEY_SCHEMA_VERSION {
        return Err(IdentityKeyError::Invalid(format!(
            "unsupported key schema version {}",
            metadata.schema_version
        )));
    }
    if metadata.algorithm != KEY_ALGORITHM {
        return Err(IdentityKeyError::Invalid(format!(
            "unsupported key algorithm {}",
            metadata.algorithm
        )));
    }
    Ok(())
}

fn key_id_for_public_key(public_key: &[u8]) -> String {
    let digest = Sha256::digest(public_key);
    format!("sha256:{}", encode_hex(digest))
}

fn write_json_new<T: Serialize>(path: &Path, value: &T, mode: u32) -> Result<(), IdentityKeyError> {
    let mut options = OpenOptions::new();
    options.write(true).create_new(true);
    #[cfg(unix)]
    options.mode(mode);
    let mut file = options.open(path)?;
    file.write_all(&serde_json::to_vec_pretty(value)?)?;
    file.write_all(b"\n")?;
    file.sync_all()?;
    #[cfg(unix)]
    fs::set_permissions(path, fs::Permissions::from_mode(mode))?;
    Ok(())
}

fn encode_hex(bytes: impl AsRef<[u8]>) -> String {
    const HEX: &[u8; 16] = b"0123456789abcdef";
    let bytes = bytes.as_ref();
    let mut encoded = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        encoded.push(HEX[(byte >> 4) as usize] as char);
        encoded.push(HEX[(byte & 0x0f) as usize] as char);
    }
    encoded
}

fn decode_hex(text: &str) -> Result<Vec<u8>, IdentityKeyError> {
    if !text.len().is_multiple_of(2) {
        return Err(IdentityKeyError::Invalid("hex length is odd".to_string()));
    }
    let mut bytes = Vec::with_capacity(text.len() / 2);
    let mut chars = text.as_bytes().chunks_exact(2);
    for pair in &mut chars {
        let high = decode_hex_nibble(pair[0])?;
        let low = decode_hex_nibble(pair[1])?;
        bytes.push((high << 4) | low);
    }
    Ok(bytes)
}

fn decode_hex_nibble(byte: u8) -> Result<u8, IdentityKeyError> {
    match byte {
        b'0'..=b'9' => Ok(byte - b'0'),
        b'a'..=b'f' => Ok(byte - b'a' + 10),
        b'A'..=b'F' => Ok(byte - b'A' + 10),
        _ => Err(IdentityKeyError::Invalid("invalid hex byte".to_string())),
    }
}

fn unix_millis() -> u64 {
    let millis = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|duration| duration.as_millis())
        .unwrap_or(0);
    u64::try_from(millis).unwrap_or(u64::MAX)
}

impl Display for IdentityKeyError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            IdentityKeyError::Io(error) => write!(formatter, "{error}"),
            IdentityKeyError::Json(error) => write!(formatter, "{error}"),
            IdentityKeyError::Invalid(message) => write!(formatter, "{message}"),
            IdentityKeyError::Crypto(message) => write!(formatter, "{message}"),
        }
    }
}

impl std::error::Error for IdentityKeyError {}

impl From<io::Error> for IdentityKeyError {
    fn from(error: io::Error) -> Self {
        IdentityKeyError::Io(error)
    }
}

impl From<serde_json::Error> for IdentityKeyError {
    fn from(error: serde_json::Error) -> Self {
        IdentityKeyError::Json(error)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use ed25519_dalek::Signer;
    use std::time::{SystemTime, UNIX_EPOCH};

    fn temp_root(name: &str) -> PathBuf {
        let nonce = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("clock before epoch")
            .as_nanos();
        std::env::temp_dir().join(format!(
            "kelp-pi-agent-key-{name}-{}-{nonce}",
            std::process::id()
        ))
    }

    #[test]
    fn keygen_persists_and_loads_idempotently() {
        let root = temp_root("idempotent");
        let key_dir = root.join("keys");

        let first = load_or_generate_identity_key(&key_dir, "pi-a").expect("generate key");
        let private_before =
            fs::read_to_string(key_dir.join(PRIVATE_KEY_FILE)).expect("read private key");
        let public_before =
            fs::read_to_string(key_dir.join(PUBLIC_KEY_FILE)).expect("read public key");
        let second = load_or_generate_identity_key(&key_dir, "pi-b").expect("load existing key");

        assert_eq!(first.metadata, second.metadata);
        assert_eq!(
            private_before,
            fs::read_to_string(key_dir.join(PRIVATE_KEY_FILE)).expect("reread private key")
        );
        assert_eq!(
            public_before,
            fs::read_to_string(key_dir.join(PUBLIC_KEY_FILE)).expect("reread public key")
        );
        assert!(first.metadata.key_id.starts_with("sha256:"));

        let message = b"kelp pi identity";
        let signature = second.signing_key.sign(message);
        second
            .signing_key
            .verifying_key()
            .verify_strict(message, &signature)
            .expect("loaded key verifies signature");

        #[cfg(unix)]
        {
            let private_mode = fs::metadata(key_dir.join(PRIVATE_KEY_FILE))
                .expect("private metadata")
                .permissions()
                .mode()
                & 0o777;
            let public_mode = fs::metadata(key_dir.join(PUBLIC_KEY_FILE))
                .expect("public metadata")
                .permissions()
                .mode()
                & 0o777;
            assert_eq!(private_mode, 0o600);
            assert_eq!(public_mode, 0o644);
        }

        fs::remove_dir_all(root).expect("cleanup");
    }
}
