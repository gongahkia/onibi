use anyhow::{bail, Context, Result};
use clap::ValueEnum;
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use std::env;
use std::fs;
use std::path::{Path, PathBuf};
use std::time::{SystemTime, UNIX_EPOCH};

const CURRENT_CONFIG_SCHEMA_VERSION: u32 = 1;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, ValueEnum, Default)]
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
    legacy_data_root: Option<PathBuf>,
}

impl JsonConfigStore {
    pub fn at(root: PathBuf) -> Self {
        Self {
            root,
            legacy_data_root: None,
        }
    }

    pub fn from_env() -> Result<Self> {
        Ok(Self {
            root: resolve_config_root(None)?,
            legacy_data_root: None,
        })
    }

    pub fn from_env_with_data_root(
        data_root: &Path,
        colocate_with_data_root: bool,
    ) -> Result<Self> {
        let root = resolve_config_root(colocate_with_data_root.then_some(data_root))?;
        let legacy_data_root =
            (!colocate_with_data_root && root != data_root).then(|| data_root.to_path_buf());

        Ok(Self {
            root,
            legacy_data_root,
        })
    }

    pub fn config_file(&self) -> PathBuf {
        self.root.join("config.json")
    }

    pub fn init(&self) -> Result<PathBuf> {
        self.migrate_legacy_config_if_needed()?;
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

    fn migrate_legacy_config_if_needed(&self) -> Result<()> {
        let Some(legacy_root) = &self.legacy_data_root else {
            return Ok(());
        };

        let target = self.config_file();
        let legacy = legacy_root.join("config.json");
        if target == legacy || target.exists() || !legacy.exists() {
            return Ok(());
        }

        if let Some(parent) = target.parent() {
            fs::create_dir_all(parent)
                .with_context(|| format!("failed to create {}", parent.display()))?;
        }

        fs::rename(&legacy, &target).with_context(|| {
            format!(
                "failed to migrate legacy config {} to {}",
                legacy.display(),
                target.display()
            )
        })?;

        Ok(())
    }

    pub fn load(&self) -> Result<AppConfig> {
        let file = self.init()?;
        let contents = fs::read_to_string(&file)
            .with_context(|| format!("failed to read {}", file.display()))?;
        if contents.trim().is_empty() {
            return Ok(AppConfig::default());
        }

        match serde_json::from_str::<Value>(&contents) {
            Ok(mut value) => {
                migrate_config_value(&mut value)?;
                serde_json::from_value::<AppConfig>(value)
                    .with_context(|| format!("failed to parse {}", file.display()))
            }
            Err(error) => self.recover_corrupt_config(error),
        }
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

    fn corrupt_dir(&self) -> PathBuf {
        self.root.join("corrupt")
    }

    fn recover_corrupt_config(&self, parse_error: serde_json::Error) -> Result<AppConfig> {
        let file = self.config_file();
        let corrupt_file = self
            .corrupt_dir()
            .join(format!("config-corrupt-{}.json", unix_timestamp()));
        if let Some(parent) = corrupt_file.parent() {
            fs::create_dir_all(parent)
                .with_context(|| format!("failed to create {}", parent.display()))?;
        }
        fs::rename(&file, &corrupt_file).with_context(|| {
            format!(
                "failed to move corrupt config {} into quarantine {}",
                file.display(),
                corrupt_file.display()
            )
        })?;

        let default_config = AppConfig::default();
        self.save(&default_config)?;
        Err(parse_error)
            .with_context(|| format!("failed to parse {}", file.display()))
            .context(format!(
                "corrupt config moved to {}; a default config was written",
                corrupt_file.display()
            ))
    }
}

fn current_config_schema_version() -> u32 {
    CURRENT_CONFIG_SCHEMA_VERSION
}

fn default_upcoming_days() -> i64 {
    7
}

fn unix_timestamp() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .expect("system time should be after the unix epoch")
        .as_secs()
}

fn migrate_config_value(value: &mut Value) -> Result<()> {
    let schema_version = value
        .get("schema_version")
        .and_then(Value::as_u64)
        .unwrap_or(0) as u32;

    match schema_version {
        0 | 1 => add_missing_config_fields(value),
        other if other > CURRENT_CONFIG_SCHEMA_VERSION => {
            bail!("config schema version {other} is newer than this build supports");
        }
        _ => {}
    }

    if let Some(object) = value.as_object_mut() {
        object.insert(
            "schema_version".to_string(),
            Value::Number(CURRENT_CONFIG_SCHEMA_VERSION.into()),
        );
    } else {
        bail!("config must be represented as a JSON object");
    }

    Ok(())
}

fn add_missing_config_fields(value: &mut Value) {
    if let Some(object) = value.as_object_mut() {
        object
            .entry("default_upcoming_days".to_string())
            .or_insert_with(|| json!(default_upcoming_days()));
        object
            .entry("default_task_sort".to_string())
            .or_insert_with(|| json!(TaskSortKey::default()));
        object
            .entry("default_json_output".to_string())
            .or_insert_with(|| json!(false));
    }
}

fn resolve_config_root(data_root_hint: Option<&Path>) -> Result<PathBuf> {
    if let Some(path) = env::var_os("KELP_CONFIG_DIR") {
        return Ok(PathBuf::from(path));
    }

    if let Some(data_root) = data_root_hint {
        return Ok(data_root.to_path_buf());
    }

    if let Some(path) = env::var_os("KELP_DATA_DIR") {
        return Ok(PathBuf::from(path));
    }

    if let Some(path) = env::var_os("XDG_CONFIG_HOME") {
        return Ok(PathBuf::from(path).join("kelp"));
    }

    if let Some(home) = env::var_os("HOME") {
        return Ok(PathBuf::from(home).join(".config").join("kelp"));
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

    #[test]
    fn load_migrates_legacy_config_values() {
        let root = temp_root();
        let store = JsonConfigStore::at(root.clone());
        fs::create_dir_all(&root).expect("config root should be created");
        fs::write(
            root.join("config.json"),
            "{\n  \"default_upcoming_days\": 9\n}\n",
        )
        .expect("legacy config should be written");

        let config = store.load().expect("config should load");

        assert_eq!(config.schema_version, CURRENT_CONFIG_SCHEMA_VERSION);
        assert_eq!(config.default_upcoming_days, 9);
        assert_eq!(config.default_task_sort, TaskSortKey::Due);
        assert!(!config.default_json_output);

        fs::remove_dir_all(root).expect("cleanup should succeed");
    }

    #[test]
    fn load_quarantines_corrupt_config_and_reports_an_error() {
        let root = temp_root();
        let store = JsonConfigStore::at(root.clone());
        fs::create_dir_all(&root).expect("config root should be created");
        fs::write(root.join("config.json"), "{not-valid-json")
            .expect("corrupt config should be written");

        let error = store
            .load()
            .expect_err("corrupt config should return an error after recovery");

        assert!(error.to_string().contains("corrupt config moved"));
        assert!(root.join("config.json").exists());
        assert!(fs::read_dir(root.join("corrupt"))
            .expect("corrupt directory should exist")
            .next()
            .is_some());

        fs::remove_dir_all(root).expect("cleanup should succeed");
    }
}
