use crate::domain::AppState;
use anyhow::{Context, Result};
use std::env;
use std::fs;
use std::path::{Path, PathBuf};
use std::time::{SystemTime, UNIX_EPOCH};

pub trait Storage {
    fn init(&self) -> Result<PathBuf>;
    fn load(&self) -> Result<AppState>;
    fn save(&self, state: &AppState) -> Result<()>;
    fn data_file(&self) -> PathBuf;
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
}

impl Storage for JsonFileStorage {
    fn init(&self) -> Result<PathBuf> {
        let data_file = self.data_file();
        Self::ensure_parent_dir(&data_file)?;

        if !data_file.exists() {
            let contents = serde_json::to_string_pretty(&AppState::default())
                .context("failed to serialize default Kelp state")?;
            fs::write(&data_file, format!("{contents}\n"))
                .with_context(|| format!("failed to create {}", data_file.display()))?;
        }

        Ok(data_file)
    }

    fn load(&self) -> Result<AppState> {
        let data_file = self.init()?;
        let contents = fs::read_to_string(&data_file)
            .with_context(|| format!("failed to read {}", data_file.display()))?;

        if contents.trim().is_empty() {
            return Ok(AppState::default());
        }

        serde_json::from_str(&contents)
            .with_context(|| format!("failed to parse {}", data_file.display()))
    }

    fn save(&self, state: &AppState) -> Result<()> {
        let data_file = self.init()?;
        let temp_file = data_file.with_extension("json.tmp");
        let contents =
            serde_json::to_string_pretty(state).context("failed to serialize Kelp state")?;

        fs::write(&temp_file, format!("{contents}\n"))
            .with_context(|| format!("failed to write {}", temp_file.display()))?;
        if data_file.exists() {
            fs::remove_file(&data_file)
                .with_context(|| format!("failed to replace {}", data_file.display()))?;
        }
        fs::rename(&temp_file, &data_file).with_context(|| {
            format!(
                "failed to move {} into place at {}",
                temp_file.display(),
                data_file.display()
            )
        })?;

        Ok(())
    }

    fn data_file(&self) -> PathBuf {
        self.root.join("data.json")
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

#[cfg(test)]
mod tests {
    use super::*;
    use crate::domain::{NewTask, Priority};
    use chrono::NaiveDate;

    fn date(value: &str) -> NaiveDate {
        NaiveDate::parse_from_str(value, "%Y-%m-%d").expect("date fixture should be valid")
    }

    fn temp_root() -> PathBuf {
        let nanos = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("system time should be after the unix epoch")
            .as_nanos();
        env::temp_dir().join(format!("kelp-storage-test-{}-{nanos}", std::process::id()))
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
}
