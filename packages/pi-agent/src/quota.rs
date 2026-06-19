use std::fmt::{Display, Formatter};
use std::fs;
use std::io;
use std::path::{Path, PathBuf};

#[cfg(unix)]
use std::ffi::CString;
#[cfg(unix)]
use std::os::unix::ffi::OsStrExt;

use serde::Deserialize;

use crate::GIB;

pub const DEFAULT_MIN_FREE_BYTES: u64 = GIB;
pub const DEFAULT_AGENT_CONFIG_PATH: &str = "/etc/kelp-pi/agent.json";

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum StorageQuotaScope {
    Ingest,
    Scan,
    Upload,
}

impl StorageQuotaScope {
    pub fn as_str(self) -> &'static str {
        match self {
            StorageQuotaScope::Ingest => "ingest",
            StorageQuotaScope::Scan => "scan",
            StorageQuotaScope::Upload => "upload",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct StorageQuotaConfig {
    pub min_free_bytes: u64,
}

impl Default for StorageQuotaConfig {
    fn default() -> Self {
        Self {
            min_free_bytes: DEFAULT_MIN_FREE_BYTES,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct StorageQuotaCheck {
    pub scope: StorageQuotaScope,
    pub path: PathBuf,
    pub available_bytes: u64,
    pub min_free_bytes: u64,
}

#[derive(Debug)]
pub enum StorageQuotaError {
    Io {
        path: PathBuf,
        source: io::Error,
    },
    ConfigJson {
        path: PathBuf,
        source: serde_json::Error,
    },
    InvalidConfig {
        path: PathBuf,
        reason: String,
    },
    BelowFloor(StorageQuotaCheck),
}

#[derive(Debug, Deserialize)]
struct AgentConfigFile {
    quotas: Option<QuotaOverrides>,
}

#[derive(Debug, Deserialize)]
struct QuotaOverrides {
    min_free_bytes: Option<u64>,
}

pub fn load_storage_quota_config(path: &Path) -> Result<StorageQuotaConfig, StorageQuotaError> {
    let content = match fs::read_to_string(path) {
        Ok(content) => content,
        Err(error) if error.kind() == io::ErrorKind::NotFound => {
            return Ok(StorageQuotaConfig::default())
        }
        Err(source) => {
            return Err(StorageQuotaError::Io {
                path: path.to_path_buf(),
                source,
            })
        }
    };
    let parsed: AgentConfigFile =
        serde_json::from_str(&content).map_err(|source| StorageQuotaError::ConfigJson {
            path: path.to_path_buf(),
            source,
        })?;
    let mut config = StorageQuotaConfig::default();
    if let Some(quotas) = parsed.quotas {
        if let Some(min_free_bytes) = quotas.min_free_bytes {
            if min_free_bytes == 0 {
                return Err(StorageQuotaError::InvalidConfig {
                    path: path.to_path_buf(),
                    reason: "quotas.min_free_bytes must be positive".to_string(),
                });
            }
            config.min_free_bytes = min_free_bytes;
        }
    }
    Ok(config)
}

pub fn enforce_storage_quota(
    path: &Path,
    scope: StorageQuotaScope,
    min_free_bytes: u64,
) -> Result<StorageQuotaCheck, StorageQuotaError> {
    let available_bytes = free_disk_bytes(path).map_err(|source| StorageQuotaError::Io {
        path: path.to_path_buf(),
        source,
    })?;
    evaluate_storage_quota(path, scope, min_free_bytes, available_bytes)
}

pub fn evaluate_storage_quota(
    path: &Path,
    scope: StorageQuotaScope,
    min_free_bytes: u64,
    available_bytes: u64,
) -> Result<StorageQuotaCheck, StorageQuotaError> {
    let check = StorageQuotaCheck {
        scope,
        path: path.to_path_buf(),
        available_bytes,
        min_free_bytes,
    };
    if available_bytes < min_free_bytes {
        Err(StorageQuotaError::BelowFloor(check))
    } else {
        Ok(check)
    }
}

#[cfg(unix)]
pub fn free_disk_bytes(path: &Path) -> io::Result<u64> {
    let raw_path = CString::new(path.as_os_str().as_bytes())
        .map_err(|_| io::Error::new(io::ErrorKind::InvalidInput, "path contains NUL byte"))?;
    let mut stat = std::mem::MaybeUninit::<libc::statvfs>::uninit();
    let result = unsafe { libc::statvfs(raw_path.as_ptr(), stat.as_mut_ptr()) };
    if result != 0 {
        return Err(io::Error::last_os_error());
    }
    let stat = unsafe { stat.assume_init() };
    Ok(u64::from(stat.f_bavail).saturating_mul(stat.f_frsize))
}

#[cfg(not(unix))]
pub fn free_disk_bytes(_path: &Path) -> io::Result<u64> {
    Err(io::Error::new(
        io::ErrorKind::Unsupported,
        "statvfs is unavailable on this platform",
    ))
}

impl Display for StorageQuotaError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            StorageQuotaError::Io { path, source } => {
                write!(
                    formatter,
                    "storage quota probe failed at {}: {source}",
                    path.display()
                )
            }
            StorageQuotaError::ConfigJson { path, source } => {
                write!(formatter, "{} is not valid JSON: {source}", path.display())
            }
            StorageQuotaError::InvalidConfig { path, reason } => {
                write!(formatter, "{} is invalid: {reason}", path.display())
            }
            StorageQuotaError::BelowFloor(check) => write!(
                formatter,
                "{} refused: free disk {} bytes below configured floor {} bytes at {}",
                check.scope.as_str(),
                check.available_bytes,
                check.min_free_bytes,
                check.path.display()
            ),
        }
    }
}

impl std::error::Error for StorageQuotaError {}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::{SystemTime, UNIX_EPOCH};

    #[test]
    fn storage_quota_refuses_when_available_below_floor() {
        let error = evaluate_storage_quota(
            Path::new("/var/lib/kelp-pi"),
            StorageQuotaScope::Scan,
            1024,
            1000,
        )
        .expect_err("below floor refused");

        assert!(matches!(error, StorageQuotaError::BelowFloor(_)));
        assert!(error.to_string().contains("scan refused"));
    }

    #[test]
    fn storage_quota_accepts_when_available_meets_floor() {
        let check = evaluate_storage_quota(
            Path::new("/var/lib/kelp-pi"),
            StorageQuotaScope::Upload,
            1024,
            1024,
        )
        .expect("floor met");

        assert_eq!(check.scope, StorageQuotaScope::Upload);
        assert_eq!(check.available_bytes, 1024);
    }

    #[test]
    fn quota_config_loads_min_free_override() {
        let root = temp_root("quota-config");
        fs::create_dir_all(&root).expect("create temp");
        let path = root.join("agent.json");
        fs::write(&path, r#"{"quotas":{"min_free_bytes":2048}}"#).expect("write config");

        let config = load_storage_quota_config(&path).expect("load config");

        assert_eq!(config.min_free_bytes, 2048);
        fs::remove_dir_all(root).ok();
    }

    fn temp_root(name: &str) -> PathBuf {
        let nonce = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("time after epoch")
            .as_nanos();
        std::env::temp_dir().join(format!("kelp-pi-{name}-{}-{nonce}", std::process::id()))
    }
}
