use std::fmt::{Display, Formatter};
use std::fs;
use std::path::{Path, PathBuf};

#[cfg(unix)]
use std::os::unix::fs::PermissionsExt;

pub const DEFAULT_DATA_DIR: &str = "/var/lib/kelp-pi";
pub const REQUIRED_DATA_DIRS: [&str; 8] = [
    "corpus", "evidence", "bundles", "index", "audit", "keys", "policy", "scope",
];

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum DataDirIssueKind {
    Missing,
    NotDirectory,
    WorldWritable,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DataDirIssue {
    pub path: PathBuf,
    pub kind: DataDirIssueKind,
}

impl Display for DataDirIssue {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        let reason = match self.kind {
            DataDirIssueKind::Missing => "missing",
            DataDirIssueKind::NotDirectory => "not a directory",
            DataDirIssueKind::WorldWritable => "world-writable",
        };
        write!(formatter, "{}: {}", self.path.display(), reason)
    }
}

pub fn required_data_paths(root: &Path) -> Vec<PathBuf> {
    let mut paths = Vec::with_capacity(REQUIRED_DATA_DIRS.len() + 1);
    paths.push(root.to_path_buf());
    paths.extend(REQUIRED_DATA_DIRS.iter().map(|name| root.join(name)));
    paths
}

pub fn validate_data_dir(root: &Path) -> Result<(), Vec<DataDirIssue>> {
    let mut issues = Vec::new();

    for path in required_data_paths(root) {
        match fs::metadata(&path) {
            Ok(metadata) if !metadata.is_dir() => issues.push(DataDirIssue {
                path,
                kind: DataDirIssueKind::NotDirectory,
            }),
            Ok(metadata) if is_world_writable(&metadata) => issues.push(DataDirIssue {
                path,
                kind: DataDirIssueKind::WorldWritable,
            }),
            Ok(_) => {}
            Err(error) if error.kind() == std::io::ErrorKind::NotFound => {
                issues.push(DataDirIssue {
                    path,
                    kind: DataDirIssueKind::Missing,
                })
            }
            Err(_) => issues.push(DataDirIssue {
                path,
                kind: DataDirIssueKind::Missing,
            }),
        }
    }

    if issues.is_empty() {
        Ok(())
    } else {
        Err(issues)
    }
}

fn is_world_writable(metadata: &fs::Metadata) -> bool {
    #[cfg(unix)]
    {
        metadata.permissions().mode() & 0o002 != 0
    }
    #[cfg(not(unix))]
    {
        let _ = metadata;
        false
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::{SystemTime, UNIX_EPOCH};

    #[cfg(unix)]
    use std::os::unix::fs::PermissionsExt;

    fn temp_root(name: &str) -> PathBuf {
        let nonce = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("clock before epoch")
            .as_nanos();
        std::env::temp_dir().join(format!(
            "kelp-pi-agent-{name}-{}-{nonce}",
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
    fn accepts_non_world_writable_layout() {
        let root = temp_root("valid");
        create_layout(&root);

        validate_data_dir(&root).expect("valid data dir");

        fs::remove_dir_all(root).expect("cleanup");
    }

    #[cfg(unix)]
    #[test]
    fn rejects_world_writable_required_path() {
        let root = temp_root("world-writable");
        create_layout(&root);
        fs::set_permissions(root.join("corpus"), fs::Permissions::from_mode(0o777)).expect("chmod");

        let issues = validate_data_dir(&root).expect_err("world-writable dir rejected");
        assert!(issues.iter().any(|issue| {
            issue.path == root.join("corpus") && issue.kind == DataDirIssueKind::WorldWritable
        }));

        fs::remove_dir_all(root).expect("cleanup");
    }
}
