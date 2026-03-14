use crate::domain::{AppState, CURRENT_APP_SCHEMA_VERSION};
use anyhow::{bail, Context, Result};
use serde_json::{json, Value};
use std::env;
use std::fs;
use std::fs::OpenOptions;
use std::io::ErrorKind;
use std::path::{Path, PathBuf};
use std::thread;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

const BACKUP_RETENTION: usize = 10;
const LOCK_ATTEMPTS: usize = 5;
const LOCK_RETRY_DELAY_MS: u64 = 20;
const STALE_LOCK_AFTER_SECS: u64 = 30;

pub trait Storage {
    fn init(&self) -> Result<PathBuf>;
    fn load(&self) -> Result<AppState>;
    fn save(&self, state: &AppState) -> Result<()>;
    fn data_file(&self) -> PathBuf;
    fn root_dir(&self) -> PathBuf;
    fn backup_dir(&self) -> PathBuf;
    fn lock_file(&self) -> PathBuf;
    fn export_to(&self, output: &Path) -> Result<PathBuf>;
    fn create_backup_snapshot(&self) -> Result<PathBuf>;
}

#[derive(Debug, Clone)]
pub struct JsonFileStorage {
    root: PathBuf,
}

impl JsonFileStorage {
    pub fn from_env() -> Result<Self> {
        Ok(Self {
            root: resolve_data_root()?,
        })
    }

    pub fn at(root: PathBuf) -> Self {
        Self { root }
    }

    fn ensure_parent_dir(path: &Path) -> Result<()> {
        if let Some(parent) = path.parent() {
            fs::create_dir_all(parent)
                .with_context(|| format!("failed to create data directory {}", parent.display()))?;
        }

        Ok(())
    }

    fn write_atomic(&self, path: &Path, contents: &str) -> Result<()> {
        let temp_file = path.with_extension("json.tmp");
        Self::ensure_parent_dir(&temp_file)?;
        fs::write(&temp_file, format!("{contents}\n"))
            .with_context(|| format!("failed to write {}", temp_file.display()))?;
        if path.exists() {
            fs::remove_file(path)
                .with_context(|| format!("failed to replace {}", path.display()))?;
        }
        fs::rename(&temp_file, path).with_context(|| {
            format!(
                "failed to move {} into place at {}",
                temp_file.display(),
                path.display()
            )
        })?;

        Ok(())
    }

    fn ensure_default_data_file(&self) -> Result<PathBuf> {
        let data_file = self.data_file();
        Self::ensure_parent_dir(&data_file)?;
        fs::create_dir_all(self.backup_dir())
            .with_context(|| format!("failed to create {}", self.backup_dir().display()))?;
        fs::create_dir_all(self.corrupt_dir())
            .with_context(|| format!("failed to create {}", self.corrupt_dir().display()))?;

        if !data_file.exists() {
            let contents = serde_json::to_string_pretty(&AppState::default())
                .context("failed to serialize default Kelp state")?;
            self.write_atomic(&data_file, &contents)?;
        }

        Ok(data_file)
    }

    fn corrupt_dir(&self) -> PathBuf {
        self.root.join("corrupt")
    }

    fn acquire_write_lock(&self) -> Result<StorageLock> {
        let lock_file = self.lock_file();
        Self::ensure_parent_dir(&lock_file)?;

        for attempt in 0..LOCK_ATTEMPTS {
            match OpenOptions::new()
                .write(true)
                .create_new(true)
                .open(&lock_file)
            {
                Ok(mut file) => {
                    use std::io::Write;
                    let _ = writeln!(file, "pid={}", std::process::id());
                    let _ = writeln!(file, "created_at={}", unix_timestamp());
                    return Ok(StorageLock { path: lock_file });
                }
                Err(error) if error.kind() == ErrorKind::AlreadyExists => {
                    if self.lock_is_stale(&lock_file)? {
                        let _ = fs::remove_file(&lock_file);
                        continue;
                    }

                    if attempt + 1 == LOCK_ATTEMPTS {
                        bail!(
                            "storage is locked by another process: {}",
                            lock_file.display()
                        );
                    }

                    thread::sleep(Duration::from_millis(LOCK_RETRY_DELAY_MS));
                }
                Err(error) => {
                    return Err(error)
                        .with_context(|| format!("failed to acquire {}", lock_file.display()));
                }
            }
        }

        bail!("failed to acquire storage lock {}", lock_file.display())
    }

    fn lock_is_stale(&self, path: &Path) -> Result<bool> {
        let metadata = fs::metadata(path)
            .with_context(|| format!("failed to inspect {}", path.display()))?;
        let modified = metadata.modified().with_context(|| {
            format!("failed to inspect the lock timestamp for {}", path.display())
        })?;
        let age = SystemTime::now()
            .duration_since(modified)
            .unwrap_or_else(|_| Duration::from_secs(0));

        Ok(age >= Duration::from_secs(STALE_LOCK_AFTER_SECS))
    }

    fn write_state_file(&self, state: &AppState) -> Result<()> {
        let contents =
            serde_json::to_string_pretty(state).context("failed to serialize Kelp state")?;
        self.write_atomic(&self.data_file(), &contents)
    }

    fn backup_file_name() -> String {
        format!("data-{}.json", unique_suffix())
    }

    fn snapshot_current_data(&self) -> Result<Option<PathBuf>> {
        let data_file = self.data_file();
        if !data_file.exists() {
            return Ok(None);
        }

        let backup_file = self.backup_dir().join(Self::backup_file_name());
        Self::ensure_parent_dir(&backup_file)?;
        fs::copy(&data_file, &backup_file).with_context(|| {
            format!(
                "failed to create backup snapshot {} from {}",
                backup_file.display(),
                data_file.display()
            )
        })?;
        self.prune_old_backups()?;

        Ok(Some(backup_file))
    }

    fn prune_old_backups(&self) -> Result<()> {
        let mut backups = self.list_backups()?;
        backups.sort();

        if backups.len() <= BACKUP_RETENTION {
            return Ok(());
        }

        let obsolete_count = backups.len() - BACKUP_RETENTION;
        for obsolete in backups.into_iter().take(obsolete_count) {
            fs::remove_file(&obsolete)
                .with_context(|| format!("failed to prune {}", obsolete.display()))?;
        }

        Ok(())
    }

    fn list_backups(&self) -> Result<Vec<PathBuf>> {
        let backup_dir = self.backup_dir();
        if !backup_dir.exists() {
            return Ok(Vec::new());
        }

        let mut backups = fs::read_dir(&backup_dir)
            .with_context(|| format!("failed to read {}", backup_dir.display()))?
            .collect::<std::result::Result<Vec<_>, _>>()
            .with_context(|| format!("failed to enumerate {}", backup_dir.display()))?
            .into_iter()
            .map(|entry| entry.path())
            .filter(|path| path.extension().and_then(|value| value.to_str()) == Some("json"))
            .collect::<Vec<_>>();
        backups.sort();
        Ok(backups)
    }

    fn recover_from_backup(&self, parse_error: serde_json::Error) -> Result<AppState> {
        let data_file = self.data_file();
        let corrupt_file = self
            .corrupt_dir()
            .join(format!("data-corrupt-{}.json", unix_timestamp()));
        Self::ensure_parent_dir(&corrupt_file)?;
        fs::rename(&data_file, &corrupt_file).with_context(|| {
            format!(
                "failed to move corrupt data file {} into quarantine {}",
                data_file.display(),
                corrupt_file.display()
            )
        })?;

        let mut backups = self.list_backups()?;
        backups.reverse();
        for backup in backups {
            let contents = fs::read_to_string(&backup)
                .with_context(|| format!("failed to read backup {}", backup.display()))?;
            if let Ok(state) = parse_state_contents(&contents, &backup) {
                self.write_state_file(&state)?;
                return Ok(state);
            }
        }

        Err(parse_error)
            .with_context(|| format!("failed to parse {}", data_file.display()))
            .context(format!(
                "no valid backup was available; corrupt data moved to {}",
                corrupt_file.display()
            ))
    }
}

impl Storage for JsonFileStorage {
    fn init(&self) -> Result<PathBuf> {
        self.ensure_default_data_file()
    }

    fn load(&self) -> Result<AppState> {
        let data_file = self.ensure_default_data_file()?;
        let contents = fs::read_to_string(&data_file)
            .with_context(|| format!("failed to read {}", data_file.display()))?;

        if contents.trim().is_empty() {
            return Ok(AppState::default());
        }

        match serde_json::from_str::<Value>(&contents) {
            Ok(_) => parse_state_contents(&contents, &data_file),
            Err(error) => self.recover_from_backup(error),
        }
    }

    fn save(&self, state: &AppState) -> Result<()> {
        self.ensure_default_data_file()?;
        let _lock = self.acquire_write_lock()?;
        self.write_state_file(state)?;
        let _ = self.snapshot_current_data()?;
        Ok(())
    }

    fn data_file(&self) -> PathBuf {
        self.root.join("data.json")
    }

    fn root_dir(&self) -> PathBuf {
        self.root.clone()
    }

    fn backup_dir(&self) -> PathBuf {
        self.root.join("backups")
    }

    fn lock_file(&self) -> PathBuf {
        self.root.join("data.lock")
    }

    fn export_to(&self, output: &Path) -> Result<PathBuf> {
        let data_file = self.ensure_default_data_file()?;
        Self::ensure_parent_dir(output)?;
        fs::copy(&data_file, output).with_context(|| {
            format!(
                "failed to export {} to {}",
                data_file.display(),
                output.display()
            )
        })?;
        Ok(output.to_path_buf())
    }

    fn create_backup_snapshot(&self) -> Result<PathBuf> {
        self.ensure_default_data_file()?;
        let _lock = self.acquire_write_lock()?;
        self.snapshot_current_data()?.ok_or_else(|| {
            anyhow::anyhow!(
                "failed to create a backup snapshot for {}",
                self.data_file().display()
            )
        })
    }
}

fn resolve_data_root() -> Result<PathBuf> {
    if let Some(path) = env::var_os("KELP_DATA_DIR") {
        return Ok(PathBuf::from(path));
    }

    if let Some(path) = env::var_os("XDG_DATA_HOME") {
        return Ok(PathBuf::from(path).join("kelp"));
    }

    if let Some(home) = env::var_os("HOME") {
        return Ok(PathBuf::from(home).join(".local").join("share").join("kelp"));
    }

    Ok(env::current_dir()
        .context("failed to determine the current working directory")?
        .join(".kelp"))
}

#[derive(Debug)]
struct StorageLock {
    path: PathBuf,
}

impl Drop for StorageLock {
    fn drop(&mut self) {
        let _ = fs::remove_file(&self.path);
    }
}

fn unix_timestamp() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .expect("system time should be after the unix epoch")
        .as_secs()
}

fn unique_suffix() -> u128 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .expect("system time should be after the unix epoch")
        .as_nanos()
}

fn migrate_state_value(value: &mut Value) -> Result<()> {
    let schema_version = value
        .get("schema_version")
        .and_then(Value::as_u64)
        .unwrap_or(1) as u32;

    if schema_version < 2 {
        add_missing_archived_fields(value);
    }
    if schema_version < 4 {
        add_missing_planner_fields(value);
    }
    if schema_version < 5 {
        add_missing_dependency_fields(value);
    }

    if schema_version > CURRENT_APP_SCHEMA_VERSION {
        bail!("app state schema version {schema_version} is newer than this build supports");
    }

    if let Some(object) = value.as_object_mut() {
        object.insert(
            "schema_version".to_string(),
            Value::Number(CURRENT_APP_SCHEMA_VERSION.into()),
        );
    } else {
        bail!("app state must be represented as a JSON object");
    }

    Ok(())
}

fn parse_state_contents(contents: &str, path: &Path) -> Result<AppState> {
    let mut value: Value = serde_json::from_str(contents)
        .with_context(|| format!("failed to parse {}", path.display()))?;
    migrate_state_value(&mut value)?;
    serde_json::from_value::<AppState>(value)
        .with_context(|| format!("failed to parse {}", path.display()))
}

fn add_missing_archived_fields(value: &mut Value) {
    if let Some(tasks) = value.get_mut("tasks").and_then(Value::as_array_mut) {
        for task in tasks {
            if let Some(object) = task.as_object_mut() {
                object
                    .entry("archived_on".to_string())
                    .or_insert_with(|| json!(null));
            }
        }
    }

    if let Some(projects) = value.get_mut("projects").and_then(Value::as_array_mut) {
        for project in projects {
            if let Some(object) = project.as_object_mut() {
                object
                    .entry("archived_on".to_string())
                    .or_insert_with(|| json!(null));
            }
        }
    }
}

fn add_missing_planner_fields(value: &mut Value) {
    if let Some(tasks) = value.get_mut("tasks").and_then(Value::as_array_mut) {
        for task in tasks {
            if let Some(object) = task.as_object_mut() {
                object
                    .entry("waiting_until".to_string())
                    .or_insert_with(|| json!(null));
                object
                    .entry("blocked_reason".to_string())
                    .or_insert_with(|| json!(null));
            }
        }
    }

    if let Some(projects) = value.get_mut("projects").and_then(Value::as_array_mut) {
        for project in projects {
            if let Some(object) = project.as_object_mut() {
                object
                    .entry("deadline".to_string())
                    .or_insert_with(|| json!(null));
            }
        }
    }
}

fn add_missing_dependency_fields(value: &mut Value) {
    if let Some(tasks) = value.get_mut("tasks").and_then(Value::as_array_mut) {
        for task in tasks {
            if let Some(object) = task.as_object_mut() {
                object
                    .entry("depends_on".to_string())
                    .or_insert_with(|| json!([]));
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::domain::{NewTask, Priority};
    use chrono::NaiveDate;

    fn date(value: &str) -> NaiveDate {
        NaiveDate::parse_from_str(value, "%Y-%m-%d").expect("date fixture should be valid")
    }

    fn temp_root() -> PathBuf {
        env::temp_dir().join(format!("kelp-storage-test-{}", unique_suffix()))
    }

    #[test]
    fn init_creates_the_default_json_file() {
        let root = temp_root();
        let storage = JsonFileStorage::at(root.clone());

        let file = storage.init().expect("init should succeed");

        assert!(file.exists());
        assert_eq!(file, root.join("data.json"));
        fs::remove_dir_all(root).expect("temporary directory cleanup should succeed");
    }

    #[test]
    fn save_and_load_roundtrip_state() {
        let root = temp_root();
        let storage = JsonFileStorage::at(root.clone());
        let today = date("2026-03-14");
        let mut state = AppState::default();

        state
            .create_task(
                NewTask {
                    title: "Ship the rewrite".to_string(),
                    notes: Some("Keep storage deterministic".to_string()),
                    project_id: None,
                    priority: Priority::High,
                    tags: vec!["rust".to_string(), "cli".to_string()],
                    due_date: Some(today),
                    recurrence: None,
                    waiting_until: None,
                    blocked_reason: None,
                    depends_on: Vec::new(),
                },
                today,
            )
            .expect("task creation should succeed");

        storage.save(&state).expect("save should succeed");
        let loaded = storage.load().expect("load should succeed");

        assert_eq!(loaded.tasks.len(), 1);
        assert_eq!(loaded.tasks[0].title, "Ship the rewrite");

        fs::remove_dir_all(root).expect("temporary directory cleanup should succeed");
    }

    #[test]
    fn save_creates_backup_snapshots() {
        let root = temp_root();
        let storage = JsonFileStorage::at(root.clone());
        let state = AppState::default();

        storage.save(&state).expect("save should succeed");
        let backup = storage
            .create_backup_snapshot()
            .expect("backup snapshot should succeed");

        assert!(backup.exists());
        assert!(backup.starts_with(storage.backup_dir()));

        fs::remove_dir_all(root).expect("temporary directory cleanup should succeed");
    }

    #[test]
    fn load_recovers_from_the_latest_valid_backup() {
        let root = temp_root();
        let storage = JsonFileStorage::at(root.clone());
        let today = date("2026-03-14");
        let mut state = AppState::default();

        state
            .create_task(
                NewTask {
                    title: "Recover me".to_string(),
                    notes: None,
                    project_id: None,
                    priority: Priority::Medium,
                    tags: vec!["backup".to_string()],
                    due_date: Some(today),
                    recurrence: None,
                    waiting_until: None,
                    blocked_reason: None,
                    depends_on: Vec::new(),
                },
                today,
            )
            .expect("task creation should succeed");

        storage.save(&state).expect("save should succeed");
        fs::write(storage.data_file(), "{not-valid-json").expect("corrupt data should be written");

        let recovered = storage.load().expect("load should recover from backup");

        assert_eq!(recovered.tasks.len(), 1);
        assert_eq!(recovered.tasks[0].title, "Recover me");
        assert!(fs::read_dir(storage.corrupt_dir())
            .expect("corrupt dir should exist")
            .next()
            .is_some());

        fs::remove_dir_all(root).expect("temporary directory cleanup should succeed");
    }

    #[test]
    fn save_fails_when_a_fresh_lock_file_exists() {
        let root = temp_root();
        let storage = JsonFileStorage::at(root.clone());
        storage.init().expect("init should succeed");
        fs::write(storage.lock_file(), "held").expect("lock file should be created");

        let error = storage
            .save(&AppState::default())
            .expect_err("save should fail while the lock is held");

        assert!(error.to_string().contains("storage is locked"));
        fs::remove_dir_all(root).expect("temporary directory cleanup should succeed");
    }

    #[test]
    fn load_migrates_v1_state_to_the_current_schema() {
        let root = temp_root();
        let storage = JsonFileStorage::at(root.clone());
        storage.init().expect("init should succeed");
        fs::write(
            storage.data_file(),
            r#"{
  "schema_version": 1,
  "next_task_id": 2,
  "next_project_id": 2,
  "tasks": [
    {
      "id": 1,
      "title": "Old task",
      "notes": null,
      "project_id": null,
      "status": "todo",
      "priority": "medium",
      "tags": [],
      "due_date": "2026-03-14",
      "recurrence": null,
      "created_on": "2026-03-14",
      "updated_on": "2026-03-14",
      "completed_on": null
    }
  ],
  "projects": [
    {
      "id": 1,
      "name": "Old project",
      "description": null,
      "status": "active",
      "created_on": "2026-03-14",
      "updated_on": "2026-03-14"
    }
  ]
}
"#,
        )
        .expect("legacy state should be written");

        let loaded = storage.load().expect("load should migrate v1 state");

        assert_eq!(loaded.schema_version, CURRENT_APP_SCHEMA_VERSION);
        assert_eq!(loaded.tasks[0].archived_on, None);
        assert_eq!(loaded.tasks[0].waiting_until, None);
        assert_eq!(loaded.tasks[0].blocked_reason, None);
        assert!(loaded.tasks[0].depends_on.is_empty());
        assert_eq!(loaded.projects[0].archived_on, None);
        assert_eq!(loaded.projects[0].deadline, None);

        fs::remove_dir_all(root).expect("temporary directory cleanup should succeed");
    }

    #[test]
    fn load_rejects_future_schema_versions() {
        let root = temp_root();
        let storage = JsonFileStorage::at(root.clone());
        storage.init().expect("init should succeed");
        fs::write(
            storage.data_file(),
            format!(
                "{{\"schema_version\": {}, \"next_task_id\": 1, \"next_project_id\": 1, \"tasks\": [], \"projects\": []}}",
                CURRENT_APP_SCHEMA_VERSION + 1
            ),
        )
        .expect("future schema state should be written");

        let error = storage.load().expect_err("future schema should be rejected");

        assert!(error
            .to_string()
            .contains("newer than this build supports"));

        fs::remove_dir_all(root).expect("temporary directory cleanup should succeed");
    }
}
