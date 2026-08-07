use std::{
    fs::{self, File, OpenOptions},
    io::{self, Write},
    path::{Component, Path, PathBuf},
    sync::{Arc, Mutex},
    time::{Duration, SystemTime},
};

use tracing_subscriber::{EnvFilter, fmt::MakeWriter, prelude::*};

pub const LOG_DIRECTORY_NAME: &str = "logs";

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct LogFile {
    name: String,
    bytes: u64,
}

impl LogFile {
    #[must_use]
    pub fn name(&self) -> &str {
        &self.name
    }

    #[must_use]
    pub const fn bytes(&self) -> u64 {
        self.bytes
    }
}

pub fn log_directory(root: &Path) -> Result<PathBuf, LogFileError> {
    validate_root(root)?;
    Ok(root.join(LOG_DIRECTORY_NAME))
}

pub fn initialize_file_tracing(root: &Path, component: &str) -> Result<(), LogFileError> {
    let path = log_path(root, component)?;
    let parent = path.parent().ok_or(LogFileError::InvalidRoot)?;
    fs::create_dir_all(parent).map_err(LogFileError::CreateDirectory)?;
    let file = OpenOptions::new()
        .create(true)
        .append(true)
        .open(path)
        .map_err(LogFileError::Open)?;
    let filter = EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("info"));
    let subscriber = tracing_subscriber::registry().with(filter).with(
        tracing_subscriber::fmt::layer()
            .json()
            .with_ansi(false)
            .with_current_span(true)
            .with_span_list(true)
            .with_writer(SharedFileWriter(Arc::new(Mutex::new(file)))),
    );
    let _ = tracing::subscriber::set_global_default(subscriber);
    Ok(())
}

pub fn list_log_files(root: &Path) -> Result<Vec<LogFile>, LogFileError> {
    let directory = log_directory(root)?;
    let entries = match fs::read_dir(directory) {
        Ok(entries) => entries,
        Err(error) if error.kind() == io::ErrorKind::NotFound => return Ok(Vec::new()),
        Err(error) => return Err(LogFileError::ReadDirectory(error)),
    };
    let mut files = Vec::new();
    for entry in entries {
        let entry = entry.map_err(LogFileError::ReadDirectory)?;
        let name = entry.file_name();
        let Some(name) = name.to_str() else {
            continue;
        };
        if !is_managed_log_name(name) {
            continue;
        }
        let metadata = fs::symlink_metadata(entry.path()).map_err(LogFileError::Metadata)?;
        if metadata.file_type().is_file() {
            files.push(LogFile {
                name: name.to_owned(),
                bytes: metadata.len(),
            });
        }
    }
    files.sort_by(|left, right| left.name.cmp(&right.name));
    Ok(files)
}

pub fn prune_log_files(
    root: &Path,
    older_than: Duration,
    now: SystemTime,
) -> Result<usize, LogFileError> {
    let directory = log_directory(root)?;
    let entries = match fs::read_dir(directory) {
        Ok(entries) => entries,
        Err(error) if error.kind() == io::ErrorKind::NotFound => return Ok(0),
        Err(error) => return Err(LogFileError::ReadDirectory(error)),
    };
    let mut pruned = 0;
    for entry in entries {
        let entry = entry.map_err(LogFileError::ReadDirectory)?;
        let name = entry.file_name();
        let Some(name) = name.to_str() else {
            continue;
        };
        if !is_managed_log_name(name) {
            continue;
        }
        let path = entry.path();
        let metadata = fs::symlink_metadata(&path).map_err(LogFileError::Metadata)?;
        if !metadata.file_type().is_file() {
            continue;
        }
        let modified = metadata.modified().map_err(LogFileError::Metadata)?;
        if now.duration_since(modified).unwrap_or(Duration::ZERO) >= older_than {
            fs::remove_file(path).map_err(LogFileError::Remove)?;
            pruned += 1;
        }
    }
    Ok(pruned)
}

fn log_path(root: &Path, component: &str) -> Result<PathBuf, LogFileError> {
    if component.is_empty()
        || !component
            .bytes()
            .all(|byte| byte.is_ascii_lowercase() || byte.is_ascii_digit() || byte == b'-')
    {
        return Err(LogFileError::InvalidComponent);
    }
    Ok(log_directory(root)?.join(format!("arachne-{component}.jsonl")))
}

fn validate_root(root: &Path) -> Result<(), LogFileError> {
    if !root.is_absolute()
        || root.as_os_str().is_empty()
        || root.file_name().is_none()
        || root
            .components()
            .any(|component| matches!(component, Component::ParentDir))
    {
        return Err(LogFileError::InvalidRoot);
    }
    Ok(())
}

fn is_managed_log_name(name: &str) -> bool {
    name.starts_with("arachne-")
        && name
            .strip_prefix("arachne-")
            .and_then(|value| value.strip_suffix(".jsonl"))
            .is_some_and(|component| {
                !component.is_empty()
                    && component.bytes().all(|byte| {
                        byte.is_ascii_lowercase() || byte.is_ascii_digit() || byte == b'-'
                    })
            })
}

#[derive(Clone)]
struct SharedFileWriter(Arc<Mutex<File>>);

impl<'a> MakeWriter<'a> for SharedFileWriter {
    type Writer = LockedFileWriter;

    fn make_writer(&'a self) -> Self::Writer {
        LockedFileWriter(Arc::clone(&self.0))
    }
}

struct LockedFileWriter(Arc<Mutex<File>>);

impl Write for LockedFileWriter {
    fn write(&mut self, buffer: &[u8]) -> io::Result<usize> {
        self.0
            .lock()
            .map_err(|_| io::Error::other("log file lock is poisoned"))?
            .write(buffer)
    }

    fn flush(&mut self) -> io::Result<()> {
        self.0
            .lock()
            .map_err(|_| io::Error::other("log file lock is poisoned"))?
            .flush()
    }
}

#[derive(Debug, thiserror::Error)]
pub enum LogFileError {
    #[error("log root must be an absolute non-root path without parent traversal")]
    InvalidRoot,
    #[error("log component is invalid")]
    InvalidComponent,
    #[error("log directory could not be created")]
    CreateDirectory(#[source] io::Error),
    #[error("log file could not be opened")]
    Open(#[source] io::Error),
    #[error("log directory could not be read")]
    ReadDirectory(#[source] io::Error),
    #[error("log file metadata could not be read")]
    Metadata(#[source] io::Error),
    #[error("log file could not be removed")]
    Remove(#[source] io::Error),
}

#[cfg(test)]
mod tests {
    use std::{
        fs,
        path::PathBuf,
        sync::atomic::{AtomicU64, Ordering},
        time::{Duration, SystemTime},
    };

    use super::{LogFileError, list_log_files, log_directory, prune_log_files};

    static NEXT_ROOT: AtomicU64 = AtomicU64::new(0);

    fn root() -> PathBuf {
        std::env::temp_dir().join(format!(
            "arachne-log-files-{}-{}",
            std::process::id(),
            NEXT_ROOT.fetch_add(1, Ordering::Relaxed)
        ))
    }

    #[test]
    fn lists_only_regular_managed_log_files() {
        let root = root();
        let directory = log_directory(&root).unwrap();
        fs::create_dir_all(&directory).unwrap();
        fs::write(directory.join("arachne-daemon.jsonl"), b"{}\n").unwrap();
        fs::write(directory.join("notes.txt"), b"keep").unwrap();
        let files = list_log_files(&root).unwrap();
        assert_eq!(files.len(), 1);
        assert_eq!(files[0].name(), "arachne-daemon.jsonl");
        fs::remove_dir_all(root).unwrap();
    }

    #[test]
    fn prune_with_zero_age_removes_only_managed_files() {
        let root = root();
        let directory = log_directory(&root).unwrap();
        fs::create_dir_all(&directory).unwrap();
        let managed = directory.join("arachne-daemon.jsonl");
        let unmanaged = directory.join("notes.txt");
        fs::write(&managed, b"{}\n").unwrap();
        fs::write(&unmanaged, b"keep").unwrap();
        assert_eq!(
            prune_log_files(&root, Duration::ZERO, SystemTime::now()).unwrap(),
            1
        );
        assert!(!managed.exists());
        assert!(unmanaged.exists());
        fs::remove_dir_all(root).unwrap();
    }

    #[test]
    fn rejects_unsafe_log_roots() {
        assert!(matches!(
            log_directory(PathBuf::from("relative").as_path()),
            Err(LogFileError::InvalidRoot)
        ));
        assert!(matches!(
            log_directory(PathBuf::from("/state/../other").as_path()),
            Err(LogFileError::InvalidRoot)
        ));
    }
}
