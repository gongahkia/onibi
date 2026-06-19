use std::collections::{BTreeMap, BTreeSet};
use std::fs;
use std::path::Path;

use rusqlite::Connection;
use serde::Serialize;
use serde_json::{json, Value};

use crate::{apply_index_schema, index_db_path};

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct SelfcheckReport {
    pub ok: bool,
    pub data_dir: String,
    pub stale_index: bool,
    pub checks: Vec<SelfcheckCheck>,
    pub warnings: Vec<String>,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct SelfcheckCheck {
    pub id: String,
    pub status: SelfcheckStatus,
    pub message: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub details: Option<BTreeMap<String, Value>>,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
pub enum SelfcheckStatus {
    Pass,
    Warn,
    Fail,
}

pub fn run_selfcheck(data_dir: &Path) -> SelfcheckReport {
    let checks = vec![stale_index_check(data_dir)];
    let ok = checks
        .iter()
        .all(|check| !matches!(check.status, SelfcheckStatus::Fail));
    let stale_index = checks
        .iter()
        .any(|check| check.id == "stale-index" && check.status == SelfcheckStatus::Warn);
    let warnings = checks
        .iter()
        .filter(|check| matches!(check.status, SelfcheckStatus::Warn))
        .map(|check| format!("{}: {}", check.id, check.message))
        .collect();

    SelfcheckReport {
        ok,
        data_dir: data_dir.display().to_string(),
        stale_index,
        checks,
        warnings,
    }
}

fn stale_index_check(data_dir: &Path) -> SelfcheckCheck {
    match unindexed_corpus_files(data_dir) {
        Ok(unindexed) if unindexed.is_empty() => {
            pass("stale-index", "all corpus files are indexed")
        }
        Ok(unindexed) => {
            let mut details = BTreeMap::new();
            details.insert("unindexed_sources".to_string(), json!(unindexed));
            warn_with_details(
                "stale-index",
                "corpus contains source files missing from the index",
                details,
            )
        }
        Err(error) => fail("stale-index", format!("stale-index probe failed: {error}")),
    }
}

fn unindexed_corpus_files(data_dir: &Path) -> Result<Vec<String>, String> {
    let corpus_dir = data_dir.join("corpus");
    let mut corpus_files = Vec::new();
    collect_regular_files(&corpus_dir, &corpus_dir, &mut corpus_files)?;
    let indexed = indexed_source_paths(data_dir)?;
    Ok(corpus_files
        .into_iter()
        .filter(|path| !indexed.contains(path))
        .collect())
}

fn collect_regular_files(root: &Path, current: &Path, out: &mut Vec<String>) -> Result<(), String> {
    let entries =
        fs::read_dir(current).map_err(|error| format!("{}: {error}", current.display()))?;
    for entry in entries {
        let entry = entry.map_err(|error| format!("{}: {error}", current.display()))?;
        let path = entry.path();
        let file_type = entry
            .file_type()
            .map_err(|error| format!("{}: {error}", path.display()))?;
        if file_type.is_dir() {
            collect_regular_files(root, &path, out)?;
        } else if file_type.is_file() {
            out.push(relative_source_path(root, &path)?);
        }
    }
    out.sort();
    Ok(())
}

fn indexed_source_paths(data_dir: &Path) -> Result<BTreeSet<String>, String> {
    let db_path = index_db_path(data_dir);
    let parent = db_path
        .parent()
        .ok_or_else(|| format!("index db has no parent: {}", db_path.display()))?;
    fs::create_dir_all(parent).map_err(|error| format!("{}: {error}", parent.display()))?;
    let connection =
        Connection::open(&db_path).map_err(|error| format!("{}: {error}", db_path.display()))?;
    apply_index_schema(&connection).map_err(|error| format!("apply index schema: {error}"))?;
    let mut statement = connection
        .prepare("SELECT path FROM source_files")
        .map_err(|error| format!("query source_files: {error}"))?;
    let rows = statement
        .query_map([], |row| row.get::<_, String>(0))
        .map_err(|error| format!("query source_files: {error}"))?;
    rows.collect::<Result<BTreeSet<_>, _>>()
        .map_err(|error| format!("read source_files: {error}"))
}

fn relative_source_path(root: &Path, path: &Path) -> Result<String, String> {
    let relative = path
        .strip_prefix(root)
        .map_err(|error| format!("{}: {error}", path.display()))?;
    Ok(relative.to_string_lossy().replace('\\', "/"))
}

fn pass(id: &str, message: impl Into<String>) -> SelfcheckCheck {
    SelfcheckCheck {
        id: id.to_string(),
        status: SelfcheckStatus::Pass,
        message: message.into(),
        details: None,
    }
}

fn warn_with_details(
    id: &str,
    message: impl Into<String>,
    details: BTreeMap<String, Value>,
) -> SelfcheckCheck {
    SelfcheckCheck {
        id: id.to_string(),
        status: SelfcheckStatus::Warn,
        message: message.into(),
        details: Some(details),
    }
}

fn fail(id: &str, message: impl Into<String>) -> SelfcheckCheck {
    SelfcheckCheck {
        id: id.to_string(),
        status: SelfcheckStatus::Fail,
        message: message.into(),
        details: None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::REQUIRED_DATA_DIRS;
    use rusqlite::params;
    use std::path::PathBuf;
    use std::time::{SystemTime, UNIX_EPOCH};

    #[test]
    fn selfcheck_warns_when_corpus_file_has_never_been_ingested() {
        let root = temp_root("stale");
        create_layout(&root);
        fs::write(root.join("corpus").join("guide.txt"), "admin login").expect("write corpus");

        let report = run_selfcheck(&root);

        assert!(report.ok);
        assert!(report.stale_index);
        assert!(report
            .warnings
            .iter()
            .any(|warning| warning.contains("stale-index")));
        assert_eq!(
            report.checks[0]
                .details
                .as_ref()
                .expect("details")
                .get("unindexed_sources"),
            Some(&json!(["guide.txt"]))
        );
        fs::remove_dir_all(root).ok();
    }

    #[test]
    fn selfcheck_passes_when_corpus_file_is_indexed() {
        let root = temp_root("fresh");
        create_layout(&root);
        fs::write(root.join("corpus").join("guide.txt"), "admin login").expect("write corpus");
        let connection = Connection::open(index_db_path(&root)).expect("open index");
        apply_index_schema(&connection).expect("schema");
        connection
            .execute(
                "INSERT INTO source_files (path, content_hash, mtime_unix_nanos, size_bytes, chunk_count, ingested_at) VALUES (?1, ?2, ?3, ?4, ?5, ?6)",
                params!["guide.txt", "hash", 1_i64, 11_i64, 1_i64, "2026-06-19T00:00:00Z"],
            )
            .expect("insert source");

        let report = run_selfcheck(&root);

        assert!(report.ok);
        assert!(!report.stale_index);
        assert!(report.warnings.is_empty());
        assert_eq!(report.checks[0].status, SelfcheckStatus::Pass);
        fs::remove_dir_all(root).ok();
    }

    fn create_layout(root: &Path) {
        fs::create_dir_all(root).expect("create root");
        for name in REQUIRED_DATA_DIRS {
            fs::create_dir_all(root.join(name)).expect("create child");
        }
    }

    fn temp_root(name: &str) -> PathBuf {
        let nonce = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("time after epoch")
            .as_nanos();
        std::env::temp_dir().join(format!(
            "kelp-pi-selfcheck-{name}-{}-{nonce}",
            std::process::id()
        ))
    }
}
