use std::collections::BTreeMap;
use std::ffi::CString;
use std::io;
use std::path::Path;

use rusqlite::Connection;
use serde::Serialize;
use serde_json::{json, Value};

use crate::{load_identity_key, validate_data_dir};

#[cfg(unix)]
use std::os::unix::ffi::OsStrExt;

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct DoctorReport {
    pub ok: bool,
    pub data_dir: String,
    pub checks: Vec<DoctorCheck>,
    pub recommendations: Vec<String>,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct DoctorCheck {
    pub id: String,
    pub status: DoctorStatus,
    pub required: bool,
    pub message: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub details: Option<BTreeMap<String, Value>>,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
pub enum DoctorStatus {
    Pass,
    Fail,
}

pub fn run_doctor(data_dir: &Path) -> DoctorReport {
    let checks = vec![
        data_dir_check(data_dir),
        key_presence_check(data_dir),
        disk_space_check(data_dir),
        sqlite_version_check(),
        sqlite_fts5_check(),
    ];
    let ok = checks
        .iter()
        .all(|check| matches!(check.status, DoctorStatus::Pass));
    let recommendations = checks
        .iter()
        .filter(|check| matches!(check.status, DoctorStatus::Fail))
        .map(|check| format!("fix {}: {}", check.id, check.message))
        .collect();

    DoctorReport {
        ok,
        data_dir: data_dir.display().to_string(),
        checks,
        recommendations,
    }
}

fn data_dir_check(data_dir: &Path) -> DoctorCheck {
    match validate_data_dir(data_dir) {
        Ok(()) => pass(
            "data-dir",
            "data dir exists with non-world-writable required paths",
        ),
        Err(issues) => {
            let mut details = BTreeMap::new();
            details.insert(
                "issues".to_string(),
                Value::Array(
                    issues
                        .iter()
                        .map(|issue| Value::String(issue.to_string()))
                        .collect(),
                ),
            );
            fail_with_details("data-dir", "data dir preflight failed", details)
        }
    }
}

fn key_presence_check(data_dir: &Path) -> DoctorCheck {
    match load_identity_key(&data_dir.join("keys")) {
        Ok(identity) => {
            let mut details = BTreeMap::new();
            details.insert("key_id".to_string(), json!(identity.metadata.key_id));
            details.insert("label".to_string(), json!(identity.metadata.label));
            pass_with_details("key-presence", "Ed25519 identity key loads", details)
        }
        Err(error) => fail(
            "key-presence",
            format!("Ed25519 identity key missing or invalid: {error}"),
        ),
    }
}

fn disk_space_check(data_dir: &Path) -> DoctorCheck {
    match free_disk_bytes(data_dir) {
        Ok(bytes) if bytes > 0 => {
            let mut details = BTreeMap::new();
            details.insert("free_bytes".to_string(), json!(bytes));
            pass_with_details("disk-space", "disk space probe succeeded", details)
        }
        Ok(_) => fail(
            "disk-space",
            "disk space probe returned zero available bytes",
        ),
        Err(error) => fail("disk-space", format!("disk space probe failed: {error}")),
    }
}

fn sqlite_version_check() -> DoctorCheck {
    match sqlite_version() {
        Ok(version) => {
            let mut details = BTreeMap::new();
            details.insert("version".to_string(), json!(version));
            pass_with_details("sqlite-version", "SQLite version probe succeeded", details)
        }
        Err(error) => fail(
            "sqlite-version",
            format!("SQLite version probe failed: {error}"),
        ),
    }
}

fn sqlite_fts5_check() -> DoctorCheck {
    match sqlite_has_fts5() {
        Ok(()) => pass("sqlite-fts5", "SQLite FTS5 virtual table probe succeeded"),
        Err(error) => fail("sqlite-fts5", format!("SQLite FTS5 probe failed: {error}")),
    }
}

fn sqlite_version() -> rusqlite::Result<String> {
    let connection = Connection::open_in_memory()?;
    connection.query_row("SELECT sqlite_version()", [], |row| row.get(0))
}

fn sqlite_has_fts5() -> rusqlite::Result<()> {
    let connection = Connection::open_in_memory()?;
    connection.execute("CREATE VIRTUAL TABLE fts5_probe USING fts5(content)", [])?;
    Ok(())
}

#[cfg(unix)]
fn free_disk_bytes(path: &Path) -> io::Result<u64> {
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
fn free_disk_bytes(_path: &Path) -> io::Result<u64> {
    Err(io::Error::new(
        io::ErrorKind::Unsupported,
        "statvfs is unavailable on this platform",
    ))
}

fn pass(id: &str, message: impl Into<String>) -> DoctorCheck {
    DoctorCheck {
        id: id.to_string(),
        status: DoctorStatus::Pass,
        required: true,
        message: message.into(),
        details: None,
    }
}

fn pass_with_details(
    id: &str,
    message: impl Into<String>,
    details: BTreeMap<String, Value>,
) -> DoctorCheck {
    DoctorCheck {
        id: id.to_string(),
        status: DoctorStatus::Pass,
        required: true,
        message: message.into(),
        details: Some(details),
    }
}

fn fail(id: &str, message: impl Into<String>) -> DoctorCheck {
    DoctorCheck {
        id: id.to_string(),
        status: DoctorStatus::Fail,
        required: true,
        message: message.into(),
        details: None,
    }
}

fn fail_with_details(
    id: &str,
    message: impl Into<String>,
    details: BTreeMap<String, Value>,
) -> DoctorCheck {
    DoctorCheck {
        id: id.to_string(),
        status: DoctorStatus::Fail,
        required: true,
        message: message.into(),
        details: Some(details),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{load_or_generate_identity_key, REQUIRED_DATA_DIRS};
    use std::fs;
    use std::path::PathBuf;
    use std::time::{SystemTime, UNIX_EPOCH};

    fn temp_root(name: &str) -> PathBuf {
        let nonce = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("clock before epoch")
            .as_nanos();
        std::env::temp_dir().join(format!(
            "kelp-pi-agent-doctor-{name}-{}-{nonce}",
            std::process::id()
        ))
    }

    fn create_layout(root: &Path) {
        fs::create_dir_all(root).expect("create root");
        for name in REQUIRED_DATA_DIRS {
            fs::create_dir_all(root.join(name)).expect("create child");
        }
    }

    #[test]
    fn doctor_passes_with_valid_layout_key_and_sqlite_fts5() {
        let root = temp_root("valid");
        create_layout(&root);
        load_or_generate_identity_key(&root.join("keys"), "pi-a").expect("keygen");

        let report = run_doctor(&root);

        assert!(report.ok);
        assert!(report
            .checks
            .iter()
            .any(|check| check.id == "sqlite-fts5" && check.status == DoctorStatus::Pass));

        fs::remove_dir_all(root).expect("cleanup");
    }

    #[test]
    fn doctor_fails_without_key() {
        let root = temp_root("missing-key");
        create_layout(&root);

        let report = run_doctor(&root);

        assert!(!report.ok);
        assert!(report
            .checks
            .iter()
            .any(|check| check.id == "key-presence" && check.status == DoctorStatus::Fail));

        fs::remove_dir_all(root).expect("cleanup");
    }
}
