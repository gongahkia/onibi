use notify::{Config, Event, RecommendedWatcher, RecursiveMode, Watcher};
use serde::Serialize;
use sha2::{Digest, Sha256};
use std::{
    collections::HashMap,
    fs,
    path::{Component, Path, PathBuf},
    process::Command,
    sync::Arc,
    time::{SystemTime, UNIX_EPOCH},
};

const MAX_REVIEW_FILE_BYTES: u64 = 2 * 1024 * 1024;
const MAX_SNAPSHOT_FILES: usize = 20_000;

#[derive(Default)]
pub struct AgentReviewManager {
    inner: Arc<parking_lot::Mutex<ReviewInner>>,
}

#[derive(Default)]
struct ReviewInner {
    sessions: HashMap<String, ReviewSession>,
    records: HashMap<String, AgentReviewRecord>,
}

struct ReviewSession {
    root: PathBuf,
    baseline: HashMap<String, Option<Vec<u8>>>,
    human_write_skips: HashMap<String, u8>,
    _watcher: RecommendedWatcher,
}

#[derive(Clone)]
struct AgentReviewRecord {
    id: String,
    session_id: String,
    root: PathBuf,
    path: String,
    full_path: PathBuf,
    baseline: Option<Vec<u8>>,
    baseline_hash: Option<String>,
    current_hash: Option<String>,
    recorded_at: u128,
}

#[derive(Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct AgentReviewMetadata {
    id: String,
    session_id: String,
    path: String,
    full_path: PathBuf,
    status: String,
    recorded_at: u128,
}

#[derive(Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct AgentReviewDiff {
    id: String,
    path: String,
    old_label: String,
    new_label: String,
    old_text: Option<String>,
    new_text: Option<String>,
    binary: bool,
}

fn now_millis() -> u128 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|duration| duration.as_millis())
        .unwrap_or(0)
}

fn canonicalize_existing(path: &Path) -> Result<PathBuf, String> {
    fs::canonicalize(path).map_err(|err| format!("failed to resolve {}: {err}", path.display()))
}

fn relative_path(root: &Path, path: &Path) -> Option<String> {
    let relative = path.strip_prefix(root).ok()?;
    if relative.components().any(|component| {
        matches!(
            component,
            Component::ParentDir | Component::RootDir | Component::Prefix(_)
        )
    }) {
        return None;
    }
    Some(relative.to_string_lossy().to_string())
}

fn path_inside(root: &Path, path: &Path) -> Result<(PathBuf, String), String> {
    let root = canonicalize_existing(root)?;
    let path = if path.exists() {
        canonicalize_existing(path)?
    } else if path.is_absolute() {
        path.to_path_buf()
    } else {
        root.join(path)
    };
    if !path.starts_with(&root) {
        return Err(format!(
            "path {} escapes workspace {}",
            path.display(),
            root.display()
        ));
    }
    let Some(relative) = relative_path(&root, &path) else {
        return Err("path escapes workspace".to_string());
    };
    Ok((path, relative))
}

fn read_trackable_file(path: &Path) -> Option<Vec<u8>> {
    let metadata = fs::metadata(path).ok()?;
    if !metadata.is_file() || metadata.len() > MAX_REVIEW_FILE_BYTES {
        return None;
    }
    fs::read(path).ok()
}

fn hash_bytes(bytes: &[u8]) -> String {
    hex::encode(Sha256::digest(bytes))
}

fn text_from_bytes(bytes: Option<Vec<u8>>) -> (Option<String>, bool) {
    let Some(bytes) = bytes else {
        return (None, false);
    };
    if bytes.iter().any(|byte| *byte == 0) {
        return (None, true);
    }
    match String::from_utf8(bytes) {
        Ok(text) => (Some(text), false),
        Err(_) => (None, true),
    }
}

fn record_key(root: &Path, relative: &str) -> String {
    format!("{}::{relative}", root.display())
}

fn git_visible_paths(root: &Path) -> Option<Vec<String>> {
    let output = Command::new("git")
        .arg("-C")
        .arg(root)
        .args([
            "ls-files",
            "-z",
            "--cached",
            "--others",
            "--exclude-standard",
        ])
        .output()
        .ok()?;
    if !output.status.success() {
        return None;
    }
    Some(
        output
            .stdout
            .split(|byte| *byte == 0)
            .filter(|path| !path.is_empty())
            .take(MAX_SNAPSHOT_FILES)
            .map(|path| String::from_utf8_lossy(path).to_string())
            .collect(),
    )
}

fn should_skip_dir(path: &Path) -> bool {
    matches!(
        path.file_name().and_then(|name| name.to_str()),
        Some(
            ".git"
                | "node_modules"
                | "target"
                | "dist"
                | "build"
                | ".next"
                | ".turbo"
                | "coverage"
                | "DerivedData"
        )
    )
}

fn scan_paths(base: &Path, dir: &Path, paths: &mut Vec<String>) {
    if paths.len() >= MAX_SNAPSHOT_FILES {
        return;
    }
    let Ok(entries) = fs::read_dir(dir) else {
        return;
    };
    for entry in entries.flatten() {
        let path = entry.path();
        if should_skip_dir(&path) {
            continue;
        }
        let Ok(metadata) = entry.metadata() else {
            continue;
        };
        if metadata.is_dir() {
            scan_paths(base, &path, paths);
        } else if metadata.is_file() {
            if let Some(relative) = path.parent().and_then(|_| {
                path.strip_prefix(base)
                    .ok()
                    .map(|relative| relative.to_string_lossy().to_string())
            }) {
                paths.push(relative);
            }
        }
        if paths.len() >= MAX_SNAPSHOT_FILES {
            break;
        }
    }
}

fn snapshot_baseline(root: &Path) -> HashMap<String, Option<Vec<u8>>> {
    let mut paths = git_visible_paths(root).unwrap_or_else(|| {
        let mut paths = Vec::new();
        scan_paths(root, root, &mut paths);
        paths
    });
    paths.sort();
    paths.dedup();
    paths
        .into_iter()
        .map(|relative| {
            let path = root.join(&relative);
            let bytes = read_trackable_file(&path);
            (relative, bytes)
        })
        .collect()
}

impl AgentReviewManager {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn start(&self, session_id: String, root: PathBuf) -> Result<(), String> {
        let root = canonicalize_existing(&root)?;
        let baseline = snapshot_baseline(&root);
        let inner = self.inner.clone();
        let callback_root = root.clone();
        let callback_session_id = session_id.clone();
        let mut watcher = RecommendedWatcher::new(
            move |result: Result<Event, notify::Error>| {
                if let Ok(event) = result {
                    AgentReviewManager::record_event_paths(
                        &inner,
                        &callback_session_id,
                        &callback_root,
                        event.paths,
                    );
                }
            },
            Config::default(),
        )
        .map_err(|err| err.to_string())?;
        watcher
            .watch(&root, RecursiveMode::Recursive)
            .map_err(|err| err.to_string())?;

        self.inner.lock().sessions.insert(
            session_id,
            ReviewSession {
                root,
                baseline,
                human_write_skips: HashMap::new(),
                _watcher: watcher,
            },
        );
        Ok(())
    }

    pub fn stop(&self, session_id: &str) {
        self.inner.lock().sessions.remove(session_id);
    }

    pub fn note_human_write(&self, root: PathBuf, path: PathBuf) -> Result<(), String> {
        let root = canonicalize_existing(&root)?;
        let (_, relative) = path_inside(&root, &path)?;
        let mut inner = self.inner.lock();
        for session in inner
            .sessions
            .values_mut()
            .filter(|session| session.root == root)
        {
            session.human_write_skips.insert(relative.clone(), 8);
        }
        Ok(())
    }

    pub fn records(&self, root: PathBuf) -> Result<Vec<AgentReviewMetadata>, String> {
        let root = canonicalize_existing(&root)?;
        let mut records: Vec<_> = self
            .inner
            .lock()
            .records
            .values()
            .filter(|record| record.root == root)
            .map(|record| {
                let status = match (&record.baseline_hash, &record.current_hash) {
                    (None, Some(_)) => "added",
                    (Some(_), None) => "deleted",
                    _ => "modified",
                }
                .to_string();
                AgentReviewMetadata {
                    id: record.id.clone(),
                    session_id: record.session_id.clone(),
                    path: record.path.clone(),
                    full_path: record.full_path.clone(),
                    status,
                    recorded_at: record.recorded_at,
                }
            })
            .collect();
        records.sort_by(|a, b| a.path.cmp(&b.path));
        Ok(records)
    }

    pub fn diff(&self, root: PathBuf, path: PathBuf) -> Result<AgentReviewDiff, String> {
        let root = canonicalize_existing(&root)?;
        let (_, relative) = path_inside(&root, &path)?;
        let key = record_key(&root, &relative);
        let record = self
            .inner
            .lock()
            .records
            .get(&key)
            .cloned()
            .ok_or_else(|| "agent review record not found".to_string())?;
        let current = read_trackable_file(&record.full_path);
        let (old_text, old_binary) = text_from_bytes(record.baseline.clone());
        let (new_text, new_binary) = text_from_bytes(current);
        Ok(AgentReviewDiff {
            id: record.id,
            path: record.path,
            old_label: "Before agent".to_string(),
            new_label: "Current file".to_string(),
            old_text,
            new_text,
            binary: old_binary || new_binary,
        })
    }

    pub fn accept(&self, root: PathBuf, path: PathBuf) -> Result<(), String> {
        let root = canonicalize_existing(&root)?;
        let (_, relative) = path_inside(&root, &path)?;
        self.inner
            .lock()
            .records
            .remove(&record_key(&root, &relative));
        Ok(())
    }

    pub fn reject(&self, root: PathBuf, path: PathBuf) -> Result<(), String> {
        let root = canonicalize_existing(&root)?;
        let (_, relative) = path_inside(&root, &path)?;
        let key = record_key(&root, &relative);
        let record = self
            .inner
            .lock()
            .records
            .get(&key)
            .cloned()
            .ok_or_else(|| "agent review record not found".to_string())?;
        let current = read_trackable_file(&record.full_path);
        let current_hash = current.as_ref().map(|bytes| hash_bytes(bytes));
        if current_hash != record.current_hash {
            return Err("file changed after the agent edit; refusing to overwrite it".to_string());
        }
        if let Some(baseline) = record.baseline {
            if let Some(parent) = record.full_path.parent() {
                fs::create_dir_all(parent).map_err(|err| err.to_string())?;
            }
            fs::write(&record.full_path, baseline).map_err(|err| err.to_string())?;
        } else if record.full_path.exists() {
            fs::remove_file(&record.full_path).map_err(|err| err.to_string())?;
        }
        self.inner.lock().records.remove(&key);
        Ok(())
    }

    fn record_event_paths(
        inner: &Arc<parking_lot::Mutex<ReviewInner>>,
        session_id: &str,
        root: &Path,
        paths: Vec<PathBuf>,
    ) {
        for path in paths {
            let path = if path.exists() {
                match fs::canonicalize(&path) {
                    Ok(path) => path,
                    Err(_) => continue,
                }
            } else {
                path
            };
            if !path.starts_with(root) {
                continue;
            }
            if path.is_dir() {
                continue;
            }
            let Some(relative) = relative_path(root, &path) else {
                continue;
            };
            let mut guard = inner.lock();
            let Some((baseline, should_skip)) = guard.sessions.get_mut(session_id).map(|session| {
                let should_skip = if let Some(skips) = session.human_write_skips.get_mut(&relative)
                {
                    *skips = skips.saturating_sub(1);
                    let remove = *skips == 0;
                    if remove {
                        session.human_write_skips.remove(&relative);
                    }
                    true
                } else {
                    false
                };
                (
                    session.baseline.get(&relative).cloned().unwrap_or(None),
                    should_skip,
                )
            }) else {
                continue;
            };
            if should_skip {
                continue;
            }
            let baseline_hash = baseline.as_ref().map(|bytes| hash_bytes(bytes));
            let current = read_trackable_file(&path);
            let current_hash = current.as_ref().map(|bytes| hash_bytes(bytes));
            if baseline_hash == current_hash {
                guard.records.remove(&record_key(root, &relative));
                continue;
            }
            let key = record_key(root, &relative);
            guard.records.insert(
                key,
                AgentReviewRecord {
                    id: format!("{session_id}:{relative}"),
                    session_id: session_id.to_string(),
                    root: root.to_path_buf(),
                    path: relative,
                    full_path: path,
                    baseline,
                    baseline_hash,
                    current_hash,
                    recorded_at: now_millis(),
                },
            );
        }
    }
}

#[tauri::command]
pub async fn agent_review_start(
    state: tauri::State<'_, Arc<AgentReviewManager>>,
    session_id: String,
    root: PathBuf,
) -> Result<(), String> {
    state.start(session_id, root)
}

#[tauri::command]
pub async fn agent_review_stop(
    state: tauri::State<'_, Arc<AgentReviewManager>>,
    session_id: String,
) -> Result<(), String> {
    state.stop(&session_id);
    Ok(())
}

#[tauri::command]
pub async fn agent_review_note_human_write(
    state: tauri::State<'_, Arc<AgentReviewManager>>,
    root: PathBuf,
    path: PathBuf,
) -> Result<(), String> {
    state.note_human_write(root, path)
}

#[tauri::command]
pub async fn agent_review_records(
    state: tauri::State<'_, Arc<AgentReviewManager>>,
    root: PathBuf,
) -> Result<Vec<AgentReviewMetadata>, String> {
    state.records(root)
}

#[tauri::command]
pub async fn agent_review_diff(
    state: tauri::State<'_, Arc<AgentReviewManager>>,
    root: PathBuf,
    path: PathBuf,
) -> Result<AgentReviewDiff, String> {
    state.diff(root, path)
}

#[tauri::command]
pub async fn agent_review_accept(
    state: tauri::State<'_, Arc<AgentReviewManager>>,
    root: PathBuf,
    path: PathBuf,
) -> Result<(), String> {
    state.accept(root, path)
}

#[tauri::command]
pub async fn agent_review_reject(
    state: tauri::State<'_, Arc<AgentReviewManager>>,
    root: PathBuf,
    path: PathBuf,
) -> Result<(), String> {
    state.reject(root, path)
}
