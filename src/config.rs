use anyhow::{Context, Result};
use clap::ValueEnum;
use serde::{Deserialize, Serialize};
use std::env;
use std::fs;
use std::path::{Path, PathBuf};

const CURRENT_CONFIG_SCHEMA_VERSION: u32 = 1;

#[derive(
    Debug,
    Clone,
    Copy,
    PartialEq,
    Eq,
    Serialize,
    Deserialize,
    ValueEnum,
    Default,
)]
#[serde(rename_all = "snake_case")]
pub enum TaskSortKey {
    #[default]
    Due,
    Priority,
    Updated,
    Title,
}

impl std::fmt::Display for TaskSortKey {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let value = match self {
            Self::Due => "due",
            Self::Priority => "priority",
            Self::Updated => "updated",
            Self::Title => "title",
        };

        write!(f, "{value}")
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct AppConfig {
    #[serde(default = "current_config_schema_version")]
    pub schema_version: u32,
    #[serde(default = "default_upcoming_days")]
    pub default_upcoming_days: i64,
    #[serde(default)]
    pub default_task_sort: TaskSortKey,
    #[serde(default)]
    pub default_json_output: bool,
}

impl Default for AppConfig {
    fn default() -> Self {
        Self {
            schema_version: CURRENT_CONFIG_SCHEMA_VERSION,
            default_upcoming_days: default_upcoming_days(),
            default_task_sort: TaskSortKey::default(),
            default_json_output: false,
        }
    }
}

#[derive(Debug, Clone)]
pub struct JsonConfigStore {
    root: PathBuf,
}

impl JsonConfigStore {
    pub fn at(root: PathBuf) -> Self {
        Self { root }
    }

    pub fn from_env() -> Result<Self> {
        Ok(Self {
            root: resolve_config_root()?,
        })
    }

    pub fn config_file(&self) -> PathBuf {
        self.root.join("config.json")
    }

    pub fn init(&self) -> Result<PathBuf> {
        let file = self.config_file();
        if let Some(parent) = file.parent() {
            fs::create_dir_all(parent)
                .with_context(|| format!("failed to create {}", parent.display()))?;
        }
        if !file.exists() {
            self.save(&AppConfig::default())?;
        }

        Ok(file)
    }

    pub fn load(&self) -> Result<AppConfig> {
        let file = self.init()?;
        let contents = fs::read_to_string(&file)
            .with_context(|| format!("failed to read {}", file.display()))?;
        if contents.trim().is_empty() {
            return Ok(AppConfig::default());
        }

        let mut config: AppConfig = serde_json::from_str(&contents)
            .with_context(|| format!("failed to parse {}", file.display()))?;
        config.schema_version = CURRENT_CONFIG_SCHEMA_VERSION;
        Ok(config)
    }

    pub fn save(&self, config: &AppConfig) -> Result<()> {
        let file = self.config_file();
        if let Some(parent) = file.parent() {
            fs::create_dir_all(parent)
                .with_context(|| format!("failed to create {}", parent.display()))?;
        }

        let temp_file = file.with_extension("json.tmp");
        let mut config = config.clone();
        config.schema_version = CURRENT_CONFIG_SCHEMA_VERSION;
        let contents =
            serde_json::to_string_pretty(&config).context("failed to serialize Kelp config")?;
        fs::write(&temp_file, format!("{contents}\n"))
            .with_context(|| format!("failed to write {}", temp_file.display()))?;
        if file.exists() {
            fs::remove_file(&file)
                .with_context(|| format!("failed to replace {}", file.display()))?;
        }
        fs::rename(&temp_file, &file).with_context(|| {
            format!(
                "failed to move {} into place at {}",
                temp_file.display(),
                file.display()
            )
        })?;

        Ok(())
    }
}

fn current_config_schema_version() -> u32 {
    CURRENT_CONFIG_SCHEMA_VERSION
}

fn default_upcoming_days() -> i64 {
    7
}

fn resolve_config_root() -> Result<PathBuf> {
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
    use std::time::{SystemTime, UNIX_EPOCH};

    fn temp_root() -> PathBuf {
        let nanos = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("system time should be after the unix epoch")
            .as_nanos();
        env::temp_dir().join(format!("kelp-config-test-{}-{nanos}", std::process::id()))
    }

    #[test]
    fn init_creates_default_config() {
        let root = temp_root();
        let store = JsonConfigStore::at(root.clone());

        let file = store.init().expect("init should succeed");
        let config = store.load().expect("config should load");

        assert_eq!(file, root.join("config.json"));
        assert_eq!(config.default_upcoming_days, 7);
        assert_eq!(config.default_task_sort, TaskSortKey::Due);

        fs::remove_dir_all(root).expect("cleanup should succeed");
    }

    #[test]
    fn save_persists_updated_config() {
        let root = temp_root();
        let store = JsonConfigStore::at(root.clone());
        let config = AppConfig {
            default_upcoming_days: 14,
            default_task_sort: TaskSortKey::Priority,
            default_json_output: true,
            ..AppConfig::default()
        };

        store.save(&config).expect("save should succeed");
        let loaded = store.load().expect("config should load");

        assert_eq!(loaded.default_upcoming_days, 14);
        assert_eq!(loaded.default_task_sort, TaskSortKey::Priority);
        assert!(loaded.default_json_output);

        fs::remove_dir_all(root).expect("cleanup should succeed");
    }
}
