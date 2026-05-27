use serde::Serialize;
use std::{
    env,
    fs::{self, OpenOptions},
    io::Write,
    path::{Path, PathBuf},
};

const MAX_EDITABLE_FILE_BYTES: u64 = 2 * 1024 * 1024;

#[derive(Debug, Clone, Serialize)]
pub struct FsEntry {
    name: String,
    path: PathBuf,
    kind: String,
    size: u64,
}

#[derive(Debug, Clone, Serialize)]
pub struct WorkspaceInfo {
    path: PathBuf,
    name: String,
}

fn canonicalize_existing(path: &Path) -> Result<PathBuf, String> {
    fs::canonicalize(path).map_err(|err| format!("failed to resolve {}: {err}", path.display()))
}

fn ensure_inside_workspace(root: &Path, path: &Path) -> Result<(PathBuf, PathBuf), String> {
    let root = canonicalize_existing(root)?;
    let path = canonicalize_existing(path)?;
    if path.starts_with(&root) {
        Ok((root, path))
    } else {
        Err(format!(
            "path {} escapes workspace {}",
            path.display(),
            root.display()
        ))
    }
}

fn basename(path: &Path) -> String {
    path.file_name()
        .and_then(|name| name.to_str())
        .filter(|name| !name.is_empty())
        .map(ToOwned::to_owned)
        .unwrap_or_else(|| path.display().to_string())
}

#[tauri::command]
pub async fn fs_workspace_info(path: PathBuf) -> Result<WorkspaceInfo, String> {
    let path = canonicalize_existing(&path)?;
    let metadata = fs::metadata(&path).map_err(|err| err.to_string())?;
    if !metadata.is_dir() {
        return Err(format!("{} is not a directory", path.display()));
    }
    Ok(WorkspaceInfo {
        name: basename(&path),
        path,
    })
}

#[tauri::command]
pub async fn fs_list_dir(root: PathBuf, path: PathBuf) -> Result<Vec<FsEntry>, String> {
    let (root, path) = ensure_inside_workspace(&root, &path)?;
    let metadata = fs::metadata(&path).map_err(|err| err.to_string())?;
    if !metadata.is_dir() {
        return Err(format!("{} is not a directory", path.display()));
    }

    let mut entries = Vec::new();
    for entry in fs::read_dir(&path).map_err(|err| err.to_string())? {
        let entry = entry.map_err(|err| err.to_string())?;
        let entry_path = entry.path();
        let Ok(canonical_path) = fs::canonicalize(&entry_path) else {
            continue;
        };
        if !canonical_path.starts_with(&root) {
            continue;
        }
        let Ok(metadata) = fs::metadata(&canonical_path) else {
            continue;
        };
        let kind = if metadata.is_dir() {
            "dir"
        } else if metadata.is_file() {
            "file"
        } else {
            continue;
        };
        entries.push(FsEntry {
            name: basename(&entry_path),
            path: canonical_path,
            kind: kind.to_string(),
            size: metadata.len(),
        });
    }

    entries.sort_by(|a, b| match (a.kind.as_str(), b.kind.as_str()) {
        ("dir", "file") => std::cmp::Ordering::Less,
        ("file", "dir") => std::cmp::Ordering::Greater,
        _ => a.name.to_lowercase().cmp(&b.name.to_lowercase()),
    });
    Ok(entries)
}

#[tauri::command]
pub async fn fs_read_file(root: PathBuf, path: PathBuf) -> Result<Vec<u8>, String> {
    let (_, path) = ensure_inside_workspace(&root, &path)?;
    let metadata = fs::metadata(&path).map_err(|err| err.to_string())?;
    if !metadata.is_file() {
        return Err(format!("{} is not a file", path.display()));
    }
    if metadata.len() > MAX_EDITABLE_FILE_BYTES {
        return Err(format!(
            "file too large: {} bytes exceeds {} bytes",
            metadata.len(),
            MAX_EDITABLE_FILE_BYTES
        ));
    }
    fs::read(&path).map_err(|err| err.to_string())
}

#[tauri::command]
pub async fn fs_write_file(root: PathBuf, path: PathBuf, data: Vec<u8>) -> Result<(), String> {
    let (_, path) = ensure_inside_workspace(&root, &path)?;
    let metadata = fs::metadata(&path).map_err(|err| err.to_string())?;
    if !metadata.is_file() {
        return Err(format!("{} is not a file", path.display()));
    }
    if data.len() as u64 > MAX_EDITABLE_FILE_BYTES {
        return Err(format!(
            "file too large: {} bytes exceeds {} bytes",
            data.len(),
            MAX_EDITABLE_FILE_BYTES
        ));
    }
    let mut file = OpenOptions::new()
        .write(true)
        .truncate(true)
        .open(&path)
        .map_err(|err| err.to_string())?;
    file.write_all(&data).map_err(|err| err.to_string())
}

#[tauri::command]
pub async fn fs_resolve_binary(command: String) -> Result<Option<PathBuf>, String> {
    let command = command.trim();
    if command.is_empty() {
        return Ok(None);
    }
    let candidate = PathBuf::from(command);
    if candidate.components().count() > 1 {
        return Ok(candidate.is_file().then_some(candidate));
    }

    let Some(paths) = env::var_os("PATH") else {
        return Ok(None);
    };
    for dir in env::split_paths(&paths) {
        let candidate = dir.join(command);
        if candidate.is_file() {
            return Ok(Some(candidate));
        }
    }
    Ok(None)
}

#[tauri::command]
pub async fn fs_read_ghostty_config() -> Result<Option<String>, String> {
    let Some(home) = env::var_os("HOME") else {
        return Ok(None);
    };
    let path = PathBuf::from(home).join(".config/ghostty/config");
    match fs::read_to_string(path) {
        Ok(config) => Ok(Some(config)),
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => Ok(None),
        Err(err) => Err(err.to_string()),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::{SystemTime, UNIX_EPOCH};

    fn temp_workspace() -> PathBuf {
        let mut path = env::temp_dir();
        let nonce = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_nanos();
        path.push(format!("onibi-fs-test-{nonce}"));
        fs::create_dir_all(&path).unwrap();
        path
    }

    #[tokio::test]
    async fn list_dir_rejects_parent_escape() {
        let root = temp_workspace();
        let outside = root.parent().unwrap().to_path_buf();
        let result = fs_list_dir(root.clone(), outside).await;
        assert!(result.unwrap_err().contains("escapes workspace"));
        fs::remove_dir_all(root).unwrap();
    }

    #[tokio::test]
    async fn read_file_rejects_parent_escape() {
        let root = temp_workspace();
        let outside = root.parent().unwrap().join("onibi-outside.txt");
        fs::write(&outside, b"nope").unwrap();
        let result = fs_read_file(root.clone(), outside.clone()).await;
        assert!(result.unwrap_err().contains("escapes workspace"));
        fs::remove_file(outside).unwrap();
        fs::remove_dir_all(root).unwrap();
    }

    #[tokio::test]
    async fn write_file_rejects_symlink_escape() {
        let root = temp_workspace();
        let outside = root.parent().unwrap().join("onibi-outside-write.txt");
        let link = root.join("link.txt");
        fs::write(&outside, b"before").unwrap();
        #[cfg(unix)]
        std::os::unix::fs::symlink(&outside, &link).unwrap();

        #[cfg(unix)]
        {
            let result = fs_write_file(root.clone(), link, b"after".to_vec()).await;
            assert!(result.unwrap_err().contains("escapes workspace"));
            assert_eq!(fs::read(&outside).unwrap(), b"before");
        }

        fs::remove_file(outside).unwrap();
        fs::remove_dir_all(root).unwrap();
    }

    #[tokio::test]
    async fn write_file_updates_existing_workspace_file() {
        let root = temp_workspace();
        let file = root.join("note.txt");
        fs::write(&file, b"before").unwrap();
        fs_write_file(root.clone(), file.clone(), b"after".to_vec())
            .await
            .unwrap();
        assert_eq!(fs::read(file).unwrap(), b"after");
        fs::remove_dir_all(root).unwrap();
    }
}
