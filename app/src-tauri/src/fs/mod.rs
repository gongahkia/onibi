use serde::Serialize;
use std::{
    env,
    fs::{self, OpenOptions},
    io::Write,
    path::{Path, PathBuf},
};

const MAX_EDITABLE_FILE_BYTES: u64 = 2 * 1024 * 1024;
const MAX_PREVIEW_FILE_BYTES: u64 = 64 * 1024 * 1024;
const MAX_SEARCH_FILE_BYTES: u64 = 2 * 1024 * 1024;
const MAX_SEARCH_RESULTS: usize = 300;

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

#[derive(Debug, Clone, Serialize)]
pub struct TerminalConfigCandidate {
    source: String,
    label: String,
    path: PathBuf,
    content: String,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct WorkspaceSearchResult {
    path: PathBuf,
    line: usize,
    column: usize,
    preview: String,
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
pub async fn fs_read_preview_file(root: PathBuf, path: PathBuf) -> Result<Vec<u8>, String> {
    let (_, path) = ensure_inside_workspace(&root, &path)?;
    let metadata = fs::metadata(&path).map_err(|err| err.to_string())?;
    if !metadata.is_file() {
        return Err(format!("{} is not a file", path.display()));
    }
    if metadata.len() > MAX_PREVIEW_FILE_BYTES {
        return Err(format!(
            "file too large: {} bytes exceeds {} bytes",
            metadata.len(),
            MAX_PREVIEW_FILE_BYTES
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
pub async fn fs_create_file_with_contents(
    root: PathBuf,
    parent: PathBuf,
    name: String,
    data: Vec<u8>,
) -> Result<FsEntry, String> {
    if data.len() as u64 > MAX_EDITABLE_FILE_BYTES {
        return Err(format!(
            "file too large: {} bytes exceeds {} bytes",
            data.len(),
            MAX_EDITABLE_FILE_BYTES
        ));
    }
    let (root, path) = child_path_inside_workspace(&root, &parent, &name)?;
    let mut file = OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(&path)
        .map_err(|err| err.to_string())?;
    file.write_all(&data).map_err(|err| err.to_string())?;
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
pub async fn fs_move_path(
    root: PathBuf,
    path: PathBuf,
    destination: PathBuf,
) -> Result<FsEntry, String> {
    let (root, path) = ensure_inside_workspace(&root, &path)?;
    if path == root {
        return Err("cannot move the workspace root".to_string());
    }

    let destination = canonicalize_existing(&destination)?;
    if !destination.starts_with(&root) {
        return Err(format!(
            "path {} escapes workspace {}",
            destination.display(),
            root.display()
        ));
    }
    let destination_metadata = fs::metadata(&destination).map_err(|err| err.to_string())?;
    if !destination_metadata.is_dir() {
        return Err(format!("{} is not a directory", destination.display()));
    }

    let metadata = fs::metadata(&path).map_err(|err| err.to_string())?;
    if metadata.is_dir() && (destination == path || destination.starts_with(&path)) {
        return Err("cannot move a folder into itself".to_string());
    }

    let name = path
        .file_name()
        .ok_or_else(|| format!("{} has no file name", path.display()))?;
    let target = destination.join(name);
    if target == path {
        return Err("item is already in that folder".to_string());
    }
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

fn is_search_skipped_dir(path: &Path) -> bool {
    matches!(
        path.file_name().and_then(|name| name.to_str()),
        Some(
            ".git"
                | ".hg"
                | ".svn"
                | "node_modules"
                | "target"
                | "dist"
                | "build"
                | ".next"
                | ".turbo"
                | ".cache"
        )
    )
}

fn search_file(
    path: &Path,
    query_lower: &str,
    results: &mut Vec<WorkspaceSearchResult>,
) -> Result<(), String> {
    if results.len() >= MAX_SEARCH_RESULTS {
        return Ok(());
    }
    let metadata = match fs::metadata(path) {
        Ok(metadata) => metadata,
        Err(_) => return Ok(()),
    };
    if !metadata.is_file() || metadata.len() > MAX_SEARCH_FILE_BYTES {
        return Ok(());
    }
    let Ok(content) = fs::read_to_string(path) else {
        return Ok(());
    };
    for (line_index, line) in content.lines().enumerate() {
        let haystack = line.to_lowercase();
        let Some(column) = haystack.find(query_lower) else {
            continue;
        };
        let preview = if line.chars().count() > 240 {
            line.chars().take(240).collect()
        } else {
            line.to_string()
        };
        results.push(WorkspaceSearchResult {
            path: path.to_path_buf(),
            line: line_index + 1,
            column: column + 1,
            preview,
        });
        if results.len() >= MAX_SEARCH_RESULTS {
            break;
        }
    }
    Ok(())
}

fn search_dir(
    root: &Path,
    dir: &Path,
    query_lower: &str,
    results: &mut Vec<WorkspaceSearchResult>,
) -> Result<(), String> {
    if results.len() >= MAX_SEARCH_RESULTS || is_search_skipped_dir(dir) {
        return Ok(());
    }
    let entries = match fs::read_dir(dir) {
        Ok(entries) => entries,
        Err(_) => return Ok(()),
    };
    for entry in entries {
        if results.len() >= MAX_SEARCH_RESULTS {
            break;
        }
        let entry = match entry {
            Ok(entry) => entry,
            Err(_) => continue,
        };
        let path = entry.path();
        let Ok(canonical_path) = fs::canonicalize(&path) else {
            continue;
        };
        if !canonical_path.starts_with(root) {
            continue;
        }
        let Ok(metadata) = fs::metadata(&canonical_path) else {
            continue;
        };
        if metadata.is_dir() {
            search_dir(root, &canonical_path, query_lower, results)?;
        } else if metadata.is_file() {
            search_file(&canonical_path, query_lower, results)?;
        }
    }
    Ok(())
}

#[tauri::command]
pub async fn fs_search_workspace(
    root: PathBuf,
    query: String,
) -> Result<Vec<WorkspaceSearchResult>, String> {
    let root = canonicalize_existing(&root)?;
    if !root.is_dir() {
        return Err(format!("{} is not a directory", root.display()));
    }
    let query = query.trim();
    if query.is_empty() {
        return Ok(Vec::new());
    }
    let query_lower = query.to_lowercase();
    let mut results = Vec::new();
    search_dir(&root, &root, &query_lower, &mut results)?;
    results.sort_by(|left, right| {
        left.path
            .cmp(&right.path)
            .then_with(|| left.line.cmp(&right.line))
            .then_with(|| left.column.cmp(&right.column))
    });
    Ok(results)
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

fn push_terminal_config(
    configs: &mut Vec<TerminalConfigCandidate>,
    source: &str,
    label: &str,
    path: PathBuf,
) -> Result<(), String> {
    match fs::read_to_string(&path) {
        Ok(content) => configs.push(TerminalConfigCandidate {
            source: source.to_string(),
            label: label.to_string(),
            path,
            content,
        }),
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => {}
        Err(err) => return Err(format!("failed to read {}: {err}", path.display())),
    }
    Ok(())
}

fn push_terminal_config_dir(
    configs: &mut Vec<TerminalConfigCandidate>,
    source: &str,
    label: &str,
    dir: PathBuf,
    extensions: &[&str],
) -> Result<(), String> {
    let entries = match fs::read_dir(&dir) {
        Ok(entries) => entries,
        Err(err) if err.kind() == std::io::ErrorKind::NotFound => return Ok(()),
        Err(err) => return Err(format!("failed to read {}: {err}", dir.display())),
    };
    for entry in entries {
        let entry = entry.map_err(|err| err.to_string())?;
        let path = entry.path();
        if !path.is_file() {
            continue;
        }
        let Some(extension) = path.extension().and_then(|value| value.to_str()) else {
            continue;
        };
        if !extensions
            .iter()
            .any(|candidate| extension.eq_ignore_ascii_case(candidate))
        {
            continue;
        }
        let item_label = path
            .file_stem()
            .and_then(|name| name.to_str())
            .map(|name| format!("{label}: {name}"))
            .unwrap_or_else(|| label.to_string());
        push_terminal_config(configs, source, &item_label, path)?;
    }
    Ok(())
}

fn push_terminal_plist_config(
    configs: &mut Vec<TerminalConfigCandidate>,
    source: &str,
    label: &str,
    path: PathBuf,
) -> Result<(), String> {
    if !path.is_file() {
        return Ok(());
    }
    match plist::Value::from_file(&path) {
        Ok(value) => {
            let content = serde_json::to_string(&value)
                .map_err(|err| format!("failed to serialize {}: {err}", path.display()))?;
            configs.push(TerminalConfigCandidate {
                source: source.to_string(),
                label: label.to_string(),
                path,
                content,
            });
        }
        Err(err) => return Err(format!("failed to read {}: {err}", path.display())),
    }
    Ok(())
}

#[tauri::command]
pub async fn fs_detect_terminal_configs() -> Result<Vec<TerminalConfigCandidate>, String> {
    let mut configs = Vec::new();
    if let Some(home) = env::var_os("HOME") {
        let home = PathBuf::from(home);
        push_terminal_config(
            &mut configs,
            "ghostty",
            "Ghostty",
            home.join(".config/ghostty/config"),
        )?;
        push_terminal_config(
            &mut configs,
            "alacritty",
            "Alacritty",
            home.join(".config/alacritty/alacritty.toml"),
        )?;
        push_terminal_config(
            &mut configs,
            "alacritty",
            "Alacritty legacy YAML",
            home.join(".config/alacritty/alacritty.yml"),
        )?;
        push_terminal_config(
            &mut configs,
            "wezterm",
            "WezTerm",
            home.join(".wezterm.lua"),
        )?;
        push_terminal_config(
            &mut configs,
            "wezterm",
            "WezTerm",
            home.join(".config/wezterm/wezterm.lua"),
        )?;
        push_terminal_config(
            &mut configs,
            "kitty",
            "kitty",
            home.join(".config/kitty/kitty.conf"),
        )?;
        push_terminal_config(
            &mut configs,
            "tmux",
            "tmux",
            home.join(".tmux.conf"),
        )?;
        push_terminal_config(
            &mut configs,
            "tmux",
            "tmux",
            home.join(".config/tmux/tmux.conf"),
        )?;
        push_terminal_config(
            &mut configs,
            "zellij",
            "Zellij",
            home.join(".config/zellij/config.kdl"),
        )?;
        push_terminal_config_dir(
            &mut configs,
            "warp",
            "Warp theme",
            home.join(".warp/themes"),
            &["yaml", "yml"],
        )?;
        push_terminal_config(
            &mut configs,
            "rio",
            "Rio",
            home.join(".config/rio/config.toml"),
        )?;
        push_terminal_config(
            &mut configs,
            "tabby",
            "Tabby",
            home.join(".config/tabby/config.yaml"),
        )?;
        push_terminal_config(
            &mut configs,
            "tabby",
            "Tabby",
            home.join("Library/Application Support/tabby/config.yaml"),
        )?;
        push_terminal_config(
            &mut configs,
            "hyper",
            "Hyper",
            home.join(".hyper.js"),
        )?;
        push_terminal_config(
            &mut configs,
            "contour",
            "Contour",
            home.join(".config/contour/contour.yml"),
        )?;
        push_terminal_config(
            &mut configs,
            "foot",
            "foot",
            home.join(".config/foot/foot.ini"),
        )?;
        push_terminal_config_dir(
            &mut configs,
            "konsole",
            "Konsole profile",
            home.join(".local/share/konsole"),
            &["profile"],
        )?;
        push_terminal_config(
            &mut configs,
            "xfce-terminal",
            "Xfce Terminal",
            home.join(".config/xfce4/terminal/terminalrc"),
        )?;
        push_terminal_config(
            &mut configs,
            "muxy",
            "muxy",
            home.join(".config/muxy/config.toml"),
        )?;
        push_terminal_config(
            &mut configs,
            "cmux",
            "cmux",
            home.join(".config/cmux/config.toml"),
        )?;
        push_terminal_config_dir(
            &mut configs,
            "iterm2",
            "iTerm2 dynamic profile",
            home.join("Library/Application Support/iTerm2/DynamicProfiles"),
            &["json"],
        )?;
        push_terminal_plist_config(
            &mut configs,
            "iterm2",
            "iTerm2 Preferences",
            home.join("Library/Preferences/com.googlecode.iterm2.plist"),
        )?;
        push_terminal_plist_config(
            &mut configs,
            "terminal-app",
            "Terminal.app Preferences",
            home.join("Library/Preferences/com.apple.Terminal.plist"),
        )?;
    }
    if let Some(local_app_data) = env::var_os("LOCALAPPDATA") {
        let local_app_data = PathBuf::from(local_app_data);
        push_terminal_config(
            &mut configs,
            "windows-terminal",
            "Windows Terminal",
            local_app_data
                .join("Packages")
                .join("Microsoft.WindowsTerminal_8wekyb3d8bbwe")
                .join("LocalState/settings.json"),
        )?;
        push_terminal_config(
            &mut configs,
            "windows-terminal",
            "Windows Terminal Preview",
            local_app_data
                .join("Packages")
                .join("Microsoft.WindowsTerminalPreview_8wekyb3d8bbwe")
                .join("LocalState/settings.json"),
        )?;
    }
    Ok(configs)
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
    async fn create_file_with_contents_writes_new_file() {
        let root = temp_workspace();
        let created = fs_create_file_with_contents(
            root.path().to_path_buf(),
            root.path().to_path_buf(),
            ".env.example".to_string(),
            b"API_KEY=\n".to_vec(),
        )
        .await
        .unwrap();

        assert_eq!(created.name, ".env.example");
        assert_eq!(fs::read_to_string(root.path().join(".env.example")).unwrap(), "API_KEY=\n");
    }

    #[tokio::test]
    async fn create_file_with_contents_rejects_existing_file() {
        let root = temp_workspace();
        fs::write(root.path().join(".env.example"), b"before").unwrap();

        let result = fs_create_file_with_contents(
            root.path().to_path_buf(),
            root.path().to_path_buf(),
            ".env.example".to_string(),
            b"after".to_vec(),
        )
        .await;

        assert!(result.unwrap_err().contains("already exists"));
        assert_eq!(fs::read(root.path().join(".env.example")).unwrap(), b"before");
    }

    #[tokio::test]
    async fn search_workspace_skips_heavy_dirs_and_returns_matches() {
        let root = temp_workspace();
        let src = root.path().join("src");
        let ignored = root.path().join("node_modules");
        fs::create_dir(&src).unwrap();
        fs::create_dir(&ignored).unwrap();
        fs::write(src.join("main.rs"), "fn main() {\n  println!(\"Onibi\");\n}\n").unwrap();
        fs::write(ignored.join("copy.rs"), "Onibi should not appear\n").unwrap();

        let results = fs_search_workspace(root.path().to_path_buf(), "onibi".to_string())
            .await
            .unwrap();

        assert_eq!(results.len(), 1);
        assert_eq!(results[0].line, 2);
        assert!(results[0].path.ends_with("src/main.rs"));
        assert!(results[0].preview.contains("Onibi"));
    }

    #[tokio::test]
    async fn delete_rejects_workspace_root() {
        let root = temp_workspace();
        let result = fs_delete_path(root.path().to_path_buf(), root.path().to_path_buf()).await;
        assert!(result.unwrap_err().contains("cannot delete"));
    }

    #[tokio::test]
    async fn move_workspace_file_into_folder() {
        let root = temp_workspace();
        let dir = root.path().join("src");
        let file = root.path().join("note.txt");
        fs::create_dir(&dir).unwrap();
        fs::write(&file, b"note").unwrap();

        let moved = fs_move_path(root.path().to_path_buf(), file, dir.clone())
            .await
            .unwrap();

        assert_eq!(moved.name, "note.txt");
        assert_eq!(moved.path, fs::canonicalize(dir.join("note.txt")).unwrap());
        assert!(dir.join("note.txt").exists());
    }

    #[tokio::test]
    async fn move_rejects_destination_escape() {
        let root = temp_workspace();
        let outside = tempdir().unwrap();
        let file = root.path().join("note.txt");
        fs::write(&file, b"note").unwrap();

        let result = fs_move_path(
            root.path().to_path_buf(),
            file,
            outside.path().to_path_buf(),
        )
        .await;

        assert!(result.unwrap_err().contains("escapes workspace"));
    }

    #[tokio::test]
    async fn move_rejects_folder_into_descendant() {
        let root = temp_workspace();
        let dir = root.path().join("src");
        let child = dir.join("child");
        fs::create_dir(&dir).unwrap();
        fs::create_dir(&child).unwrap();

        let result = fs_move_path(root.path().to_path_buf(), dir, child).await;

        assert!(result
            .unwrap_err()
            .contains("cannot move a folder into itself"));
    }

    #[tokio::test]
    async fn move_rejects_workspace_root() {
        let root = temp_workspace();
        let child = root.path().join("child");
        fs::create_dir(&child).unwrap();

        let result =
            fs_move_path(root.path().to_path_buf(), root.path().to_path_buf(), child).await;

        assert!(result.unwrap_err().contains("cannot move"));
    }
}
