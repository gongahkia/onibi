use std::fmt::{Display, Formatter};
use std::fs::{self, OpenOptions};
use std::io::{self, Write};
use std::path::{Path, PathBuf};
use std::sync::Mutex;
use std::time::{SystemTime, UNIX_EPOCH};

use serde_json::{Map, Value};
#[cfg(unix)]
use std::os::unix::fs::PermissionsExt;
use tracing::field::{Field, Visit};
use tracing::{Event, Subscriber};
use tracing_subscriber::layer::{Context, Layer, SubscriberExt};

pub const DEFAULT_DATA_DIR: &str = "/var/lib/kelp-pi";
pub const AUDIT_LOG_FILE: &str = "agent.jsonl";
pub const REQUIRED_DATA_DIRS: [&str; 8] = [
    "corpus", "evidence", "bundles", "index", "audit", "keys", "policy", "scope",
];
pub const GIB: u64 = 1024 * 1024 * 1024;
pub const DEFAULT_QUOTAS: QuotaDefaults = QuotaDefaults {
    corpus_bytes: 20 * GIB,
    uploads_bytes: 8 * GIB,
    index_bytes: 8 * GIB,
    audit_log_bytes: GIB,
};

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct QuotaDefaults {
    pub corpus_bytes: u64,
    pub uploads_bytes: u64,
    pub index_bytes: u64,
    pub audit_log_bytes: u64,
}

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

#[derive(Debug)]
pub enum AuditTracingError {
    Io(io::Error),
    Subscriber(tracing::subscriber::SetGlobalDefaultError),
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

impl Display for AuditTracingError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            AuditTracingError::Io(error) => write!(formatter, "{error}"),
            AuditTracingError::Subscriber(error) => write!(formatter, "{error}"),
        }
    }
}

impl std::error::Error for AuditTracingError {}

impl From<io::Error> for AuditTracingError {
    fn from(error: io::Error) -> Self {
        AuditTracingError::Io(error)
    }
}

pub fn required_data_paths(root: &Path) -> Vec<PathBuf> {
    let mut paths = Vec::with_capacity(REQUIRED_DATA_DIRS.len() + 1);
    paths.push(root.to_path_buf());
    paths.extend(REQUIRED_DATA_DIRS.iter().map(|name| root.join(name)));
    paths
}

pub fn audit_log_path(root: &Path) -> PathBuf {
    root.join("audit").join(AUDIT_LOG_FILE)
}

pub fn init_audit_tracing(root: &Path) -> Result<(), AuditTracingError> {
    let file = OpenOptions::new()
        .create(true)
        .append(true)
        .open(audit_log_path(root))?;
    let subscriber = tracing_subscriber::registry().with(AuditJsonLayer::new(file));
    tracing::subscriber::set_global_default(subscriber).map_err(AuditTracingError::Subscriber)
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

pub struct AuditJsonLayer<W> {
    writer: Mutex<W>,
}

impl<W> AuditJsonLayer<W> {
    pub fn new(writer: W) -> Self {
        Self {
            writer: Mutex::new(writer),
        }
    }
}

impl<S, W> Layer<S> for AuditJsonLayer<W>
where
    S: Subscriber,
    W: Write + Send + 'static,
{
    fn on_event(&self, event: &Event<'_>, _context: Context<'_, S>) {
        let metadata = event.metadata();
        let mut visitor = JsonFieldVisitor::default();
        event.record(&mut visitor);
        let mut fields = visitor.fields;
        let event_name =
            take_text_field(&mut fields, "event").unwrap_or_else(|| metadata.name().to_string());
        let message = fields
            .remove("msg")
            .or_else(|| fields.remove("message"))
            .unwrap_or_else(|| Value::String(event_name.clone()));
        let msg_id = fields.remove("msg_id");
        let mut record = Map::new();
        record.insert("ts".to_string(), Value::from(unix_millis()));
        record.insert(
            "level".to_string(),
            Value::String(metadata.level().to_string()),
        );
        record.insert("event".to_string(), Value::String(event_name));
        record.insert("msg".to_string(), message);
        if let Some(value) = msg_id {
            record.insert("msg_id".to_string(), value);
        }
        for (key, value) in fields {
            record.insert(key, value);
        }

        if let Ok(mut writer) = self.writer.lock() {
            let _ = serde_json::to_writer(&mut *writer, &Value::Object(record));
            let _ = writer.write_all(b"\n");
            let _ = writer.flush();
        }
    }
}

#[derive(Default)]
struct JsonFieldVisitor {
    fields: Map<String, Value>,
}

impl JsonFieldVisitor {
    fn insert(&mut self, field: &Field, value: Value) {
        self.fields.insert(field.name().to_string(), value);
    }
}

impl Visit for JsonFieldVisitor {
    fn record_bool(&mut self, field: &Field, value: bool) {
        self.insert(field, Value::Bool(value));
    }

    fn record_i64(&mut self, field: &Field, value: i64) {
        self.insert(field, Value::from(value));
    }

    fn record_u64(&mut self, field: &Field, value: u64) {
        self.insert(field, Value::from(value));
    }

    fn record_str(&mut self, field: &Field, value: &str) {
        self.insert(field, Value::String(value.to_string()));
    }

    fn record_debug(&mut self, field: &Field, value: &dyn std::fmt::Debug) {
        self.insert(field, Value::String(format!("{value:?}")));
    }
}

fn take_text_field(fields: &mut Map<String, Value>, name: &str) -> Option<String> {
    fields.remove(name).map(|value| match value {
        Value::String(text) => text,
        other => other.to_string(),
    })
}

fn unix_millis() -> u64 {
    let millis = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|duration| duration.as_millis())
        .unwrap_or(0);
    u64::try_from(millis).unwrap_or(u64::MAX)
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

    #[test]
    fn audit_json_layer_writes_parseable_required_fields() {
        let root = temp_root("audit-json");
        create_layout(&root);
        let file = OpenOptions::new()
            .create(true)
            .append(true)
            .open(audit_log_path(&root))
            .expect("open audit log");
        let subscriber = tracing_subscriber::registry().with(AuditJsonLayer::new(file));

        tracing::subscriber::with_default(subscriber, || {
            tracing::info!(
                event = "agent.test",
                msg = "test message",
                msg_id = "msg-1",
                corpus_bytes = 42_u64
            );
        });

        let content = fs::read_to_string(audit_log_path(&root)).expect("read audit log");
        let line = content.lines().next().expect("audit line");
        let parsed: Value = serde_json::from_str(line).expect("parse audit JSON");

        assert!(parsed["ts"].as_u64().is_some());
        assert_eq!(parsed["level"], "INFO");
        assert_eq!(parsed["event"], "agent.test");
        assert_eq!(parsed["msg"], "test message");
        assert_eq!(parsed["msg_id"], "msg-1");
        assert_eq!(parsed["corpus_bytes"], 42);

        fs::remove_dir_all(root).expect("cleanup");
    }
}
