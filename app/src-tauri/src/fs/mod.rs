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

fn validate_child_name(name: &str) -> Result<&str, String> {
    let name = name.trim();
    if name.is_empty() || name == "." || name == ".." || name.contains('/') || name.contains('\\') {
        return Err("name must be a single file or folder name".to_string());
    }
    Ok(name)
}

fn child_path_inside_workspace(
    root: &Path,
    parent: &Path,
    name: &str,
) -> Result<(PathBuf, PathBuf), String> {
    let root = canonicalize_existing(root)?;
    let parent = canonicalize_existing(parent)?;
    if !parent.starts_with(&root) {
        return Err(format!(
            "path {} escapes workspace {}",
            parent.display(),
            root.display()
        ));
    }
    let target = parent.join(validate_child_name(name)?);
    if target.exists() {
        return Err(format!("{} already exists", target.display()));
    }
    Ok((root, target))
}

fn basename(path: &Path) -> String {
    path.file_name()
        .and_then(|name| name.to_str())
        .filter(|name| !name.is_empty())
        .map(ToOwned::to_owned)
        .unwrap_or_else(|| path.display().to_string())
}

fn fs_entry_for_path(root: &Path, path: &Path) -> Result<FsEntry, String> {
    let path = canonicalize_existing(path)?;
    if !path.starts_with(root) {
        return Err(format!(
            "path {} escapes workspace {}",
            path.display(),
            root.display()
        ));
    }
    let metadata = fs::metadata(&path).map_err(|err| err.to_string())?;
    let kind = if metadata.is_dir() {
        "dir"
    } else if metadata.is_file() {
        "file"
    } else {
        return Err(format!("{} is not a file or directory", path.display()));
    };
    Ok(FsEntry {
        name: basename(&path),
        path,
        kind: kind.to_string(),
        size: metadata.len(),
    })
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
pub async fn fs_create_file(
    root: PathBuf,
    parent: PathBuf,
    name: String,
) -> Result<FsEntry, String> {
    let (root, path) = child_path_inside_workspace(&root, &parent, &name)?;
    OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(&path)
        .map_err(|err| err.to_string())?;
    fs_entry_for_path(&root, &path)
}

#[tauri::command]
pub async fn fs_create_dir(
    root: PathBuf,
    parent: PathBuf,
    name: String,
) -> Result<FsEntry, String> {
    let (root, path) = child_path_inside_workspace(&root, &parent, &name)?;
    fs::create_dir(&path).map_err(|err| err.to_string())?;
    fs_entry_for_path(&root, &path)
}

#[tauri::command]
pub async fn fs_rename_path(root: PathBuf, path: PathBuf, name: String) -> Result<FsEntry, String> {
    let (root, path) = ensure_inside_workspace(&root, &path)?;
    let parent = path
        .parent()
        .ok_or_else(|| format!("{} has no parent directory", path.display()))?;
    let target = parent.join(validate_child_name(&name)?);
    if target.exists() {
        return Err(format!("{} already exists", target.display()));
    }
    fs::rename(&path, &target).map_err(|err| err.to_string())?;
    fs_entry_for_path(&root, &target)
}

#[tauri::command]
pub async fn fs_delete_path(root: PathBuf, path: PathBuf) -> Result<(), String> {
    let (root, path) = ensure_inside_workspace(&root, &path)?;
    if path == root {
        return Err("cannot delete the workspace root".to_string());
    }
    let metadata = fs::metadata(&path).map_err(|err| err.to_string())?;
    if metadata.is_dir() {
        fs::remove_dir_all(&path).map_err(|err| err.to_string())
    } else if metadata.is_file() {
        fs::remove_file(&path).map_err(|err| err.to_string())
    } else {
        Err(format!("{} is not a file or directory", path.display()))
    }
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
    use tempfile::{tempdir, TempDir};

    fn temp_workspace() -> TempDir {
        tempdir().unwrap()
    }

    #[tokio::test]
    async fn list_dir_rejects_parent_escape() {
        let root = temp_workspace();
        let outside = tempdir().unwrap();
        let result = fs_list_dir(root.path().to_path_buf(), outside.path().to_path_buf()).await;
        assert!(result.unwrap_err().contains("escapes workspace"));
    }

    #[tokio::test]
    async fn read_file_rejects_parent_escape() {
        let root = temp_workspace();
        let outside_dir = tempdir().unwrap();
        let outside = outside_dir.path().join("onibi-outside.txt");
        fs::write(&outside, b"nope").unwrap();
        let result = fs_read_file(root.path().to_path_buf(), outside.clone()).await;
        assert!(result.unwrap_err().contains("escapes workspace"));
    }

    #[tokio::test]
    async fn write_file_rejects_symlink_escape() {
        let root = temp_workspace();
        let outside_dir = tempdir().unwrap();
        let outside = outside_dir.path().join("onibi-outside-write.txt");
        let link = root.path().join("link.txt");
        fs::write(&outside, b"before").unwrap();
        #[cfg(unix)]
        std::os::unix::fs::symlink(&outside, &link).unwrap();

        #[cfg(unix)]
        {
            let result = fs_write_file(root.path().to_path_buf(), link, b"after".to_vec()).await;
            assert!(result.unwrap_err().contains("escapes workspace"));
            assert_eq!(fs::read(&outside).unwrap(), b"before");
        }
    }

    #[tokio::test]
    async fn write_file_updates_existing_workspace_file() {
        let root = temp_workspace();
        let file = root.path().join("note.txt");
        fs::write(&file, b"before").unwrap();
        fs_write_file(root.path().to_path_buf(), file.clone(), b"after".to_vec())
            .await
            .unwrap();
        assert_eq!(fs::read(file).unwrap(), b"after");
    }

    #[tokio::test]
    async fn create_file_rejects_parent_escape() {
        let root = temp_workspace();
        let outside = tempdir().unwrap();
        let result = fs_create_file(
            root.path().to_path_buf(),
            outside.path().to_path_buf(),
            "note.txt".to_string(),
        )
        .await;
        assert!(result.unwrap_err().contains("escapes workspace"));
    }

    #[tokio::test]
    async fn create_dir_rejects_nested_name() {
        let root = temp_workspace();
        let result = fs_create_dir(
            root.path().to_path_buf(),
            root.path().to_path_buf(),
            "../bad".to_string(),
        )
        .await;
        assert!(result.unwrap_err().contains("single file or folder name"));
    }

    #[tokio::test]
    async fn create_rename_and_delete_workspace_file() {
        let root = temp_workspace();
        let created = fs_create_file(
            root.path().to_path_buf(),
            root.path().to_path_buf(),
            "note.txt".to_string(),
        )
        .await
        .unwrap();
        assert_eq!(created.name, "note.txt");

        let renamed = fs_rename_path(
            root.path().to_path_buf(),
            created.path,
            "renamed.txt".to_string(),
        )
        .await
        .unwrap();
        assert_eq!(renamed.name, "renamed.txt");

        fs_delete_path(root.path().to_path_buf(), renamed.path)
            .await
            .unwrap();
        assert!(!root.path().join("renamed.txt").exists());
    }

    #[tokio::test]
    async fn delete_rejects_workspace_root() {
        let root = temp_workspace();
        let result = fs_delete_path(root.path().to_path_buf(), root.path().to_path_buf()).await;
        assert!(result.unwrap_err().contains("cannot delete"));
    }
}
