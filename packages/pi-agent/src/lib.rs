use std::fmt::{Display, Formatter};
use std::fs::{self, OpenOptions};
use std::io::{self, BufRead, BufReader, Write};
use std::panic::{self, PanicHookInfo};
use std::path::{Path, PathBuf};
use std::sync::Mutex;
use std::time::{SystemTime, UNIX_EPOCH};

use std::collections::BTreeMap;

use serde_json::{Map, Value};
#[cfg(unix)]
use std::os::unix::fs::PermissionsExt;
use tracing::field::{Field, Visit};
use tracing::{Event, Subscriber};
use tracing_subscriber::layer::{Context, Layer, SubscriberExt};

mod chunking;
mod doctor;
mod http;
mod index;
mod keys;
mod selfcheck;
mod wire;

pub use chunking::{
    canonical_chunk_path, chunk_id_for, chunk_markdown, chunk_pdf_sidecar, chunk_plain_text,
    default_chunking_config, deterministic_token_windows, validate_ingest_source, ChunkingConfig,
    ContentChunk, IngestRefusal, DEFAULT_CHUNK_OVERLAP_TOKENS, DEFAULT_CHUNK_TARGET_TOKENS,
};
pub use doctor::{run_doctor, DoctorCheck, DoctorReport};
pub use http::{
    ask_bind_is_loopback, ask_router, AskHttpState, DEFAULT_ASK_BIND, DEFAULT_ASK_MAX_CONCURRENT,
    DEFAULT_ASK_RATE_LIMIT_PER_MINUTE,
};
pub use index::{
    answer_query, apply_index_schema, index_db_path, ingest_source_chunks, search_chunks,
    AskResponse, Citation, NoAnswer, RetrievedChunk, SourceFileMetadata, SourceIngestOutcome,
    DEFAULT_NO_ANSWER_THRESHOLD,
};
pub use keys::{
    identity_key_paths, load_identity_key, load_or_generate_identity_key, IdentityKey,
    IdentityKeyError, IdentityKeyMetadata, DEFAULT_KEY_LABEL, PRIVATE_KEY_FILE, PUBLIC_KEY_FILE,
};
pub use selfcheck::{run_selfcheck, SelfcheckCheck, SelfcheckReport, SelfcheckStatus};
pub use wire::{
    canonical_unsigned_envelope_bytes, sign_envelope, verify_envelope, EnvelopeError,
    PiEnvelopeKind, PiEnvelopeSender, PiWireEnvelope, UnsignedPiWireEnvelope,
};

pub const DEFAULT_DATA_DIR: &str = "/var/lib/kelp-pi";
pub const AUDIT_LOG_FILE: &str = "agent.jsonl";
pub const AUDIT_LOG_GENESIS_HASH: &str =
    "0000000000000000000000000000000000000000000000000000000000000000";
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
    Verify(AuditLogVerifyError),
    Subscriber(tracing::subscriber::SetGlobalDefaultError),
}

type PanicHook = Box<dyn Fn(&PanicHookInfo<'_>) + Sync + Send + 'static>;

pub struct PanicAuditHookGuard {
    previous: Option<PanicHook>,
}

#[derive(Debug)]
pub enum AuditLogVerifyError {
    Io(io::Error),
    InvalidJson {
        line: usize,
        source: serde_json::Error,
    },
    InvalidRecord {
        line: usize,
        reason: String,
    },
    PrevHashMismatch {
        line: usize,
        expected: String,
        found: String,
    },
    HashMismatch {
        line: usize,
        expected: String,
        found: String,
    },
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AuditLogVerification {
    pub entries: usize,
    pub head_hash: String,
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
            AuditTracingError::Verify(error) => write!(formatter, "{error}"),
            AuditTracingError::Subscriber(error) => write!(formatter, "{error}"),
        }
    }
}

impl Display for AuditLogVerifyError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            AuditLogVerifyError::Io(error) => write!(formatter, "{error}"),
            AuditLogVerifyError::InvalidJson { line, source } => {
                write!(formatter, "line {line}: invalid JSON: {source}")
            }
            AuditLogVerifyError::InvalidRecord { line, reason } => {
                write!(formatter, "line {line}: invalid audit record: {reason}")
            }
            AuditLogVerifyError::PrevHashMismatch {
                line,
                expected,
                found,
            } => write!(
                formatter,
                "line {line}: prev_hash mismatch: expected {expected}, found {found}"
            ),
            AuditLogVerifyError::HashMismatch {
                line,
                expected,
                found,
            } => write!(
                formatter,
                "line {line}: hash mismatch: expected {expected}, found {found}"
            ),
        }
    }
}

impl std::error::Error for AuditTracingError {}
impl std::error::Error for AuditLogVerifyError {}

impl From<io::Error> for AuditTracingError {
    fn from(error: io::Error) -> Self {
        AuditTracingError::Io(error)
    }
}

impl From<AuditLogVerifyError> for AuditTracingError {
    fn from(error: AuditLogVerifyError) -> Self {
        AuditTracingError::Verify(error)
    }
}

impl From<io::Error> for AuditLogVerifyError {
    fn from(error: io::Error) -> Self {
        AuditLogVerifyError::Io(error)
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
    let head_hash = load_audit_log_head(&audit_log_path(root))?;
    let file = OpenOptions::new()
        .create(true)
        .append(true)
        .open(audit_log_path(root))?;
    let subscriber =
        tracing_subscriber::registry().with(AuditJsonLayer::with_prev_hash(file, head_hash));
    tracing::subscriber::set_global_default(subscriber).map_err(AuditTracingError::Subscriber)
}

pub fn verify_audit_log(path: &Path) -> Result<AuditLogVerification, AuditLogVerifyError> {
    let file = fs::File::open(path)?;
    let mut expected_prev_hash = AUDIT_LOG_GENESIS_HASH.to_string();
    let mut entries = 0;

    for (index, line) in BufReader::new(file).lines().enumerate() {
        let line_number = index + 1;
        let line = line?;
        if line.trim().is_empty() {
            continue;
        }
        let value: Value =
            serde_json::from_str(&line).map_err(|source| AuditLogVerifyError::InvalidJson {
                line: line_number,
                source,
            })?;
        let Value::Object(record) = value else {
            return Err(AuditLogVerifyError::InvalidRecord {
                line: line_number,
                reason: "record is not a JSON object".to_string(),
            });
        };
        let prev_hash = required_text_field(&record, line_number, "prev_hash")
            .map(|value| value.to_string())?;
        let found_hash =
            required_text_field(&record, line_number, "hash").map(|value| value.to_string())?;

        if prev_hash != expected_prev_hash {
            return Err(AuditLogVerifyError::PrevHashMismatch {
                line: line_number,
                expected: expected_prev_hash,
                found: prev_hash,
            });
        }

        let expected_hash = audit_record_hash(&record);
        if found_hash != expected_hash {
            return Err(AuditLogVerifyError::HashMismatch {
                line: line_number,
                expected: expected_hash,
                found: found_hash,
            });
        }

        expected_prev_hash = found_hash;
        entries += 1;
    }

    Ok(AuditLogVerification {
        entries,
        head_hash: expected_prev_hash,
    })
}

pub fn install_panic_audit_hook() -> PanicAuditHookGuard {
    let previous = panic::take_hook();
    panic::set_hook(Box::new(|info| {
        let (file, line) = info
            .location()
            .map(|location| (location.file(), location.line()))
            .unwrap_or(("<unknown>", 0));
        tracing::error!(
            event = "panic",
            msg = "agent panic",
            msg_id = "agent-panic",
            panic_payload = panic_payload(info),
            panic_file = file,
            panic_line = u64::from(line)
        );
    }));
    PanicAuditHookGuard {
        previous: Some(previous),
    }
}

fn load_audit_log_head(path: &Path) -> Result<String, AuditLogVerifyError> {
    if !path.exists() {
        return Ok(AUDIT_LOG_GENESIS_HASH.to_string());
    }
    verify_audit_log(path).map(|verification| verification.head_hash)
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

fn panic_payload(info: &PanicHookInfo<'_>) -> String {
    if let Some(message) = info.payload().downcast_ref::<&str>() {
        return (*message).to_string();
    }
    if let Some(message) = info.payload().downcast_ref::<String>() {
        return message.clone();
    }
    "<non-string panic payload>".to_string()
}

impl Drop for PanicAuditHookGuard {
    fn drop(&mut self) {
        if let Some(previous) = self.previous.take() {
            panic::set_hook(previous);
        }
    }
}

pub struct AuditJsonLayer<W> {
    state: Mutex<AuditJsonState<W>>,
}

struct AuditJsonState<W> {
    writer: W,
    prev_hash: String,
}

impl<W> AuditJsonLayer<W> {
    pub fn new(writer: W) -> Self {
        Self::with_prev_hash(writer, AUDIT_LOG_GENESIS_HASH.to_string())
    }

    pub fn with_prev_hash(writer: W, prev_hash: String) -> Self {
        Self {
            state: Mutex::new(AuditJsonState { writer, prev_hash }),
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

        if let Ok(mut state) = self.state.lock() {
            record.insert(
                "prev_hash".to_string(),
                Value::String(state.prev_hash.clone()),
            );
            let hash = audit_record_hash(&record);
            record.insert("hash".to_string(), Value::String(hash.clone()));
            let wrote_record = serde_json::to_writer(&mut state.writer, &Value::Object(record))
                .is_ok()
                && state.writer.write_all(b"\n").is_ok()
                && state.writer.flush().is_ok();
            if wrote_record {
                state.prev_hash = hash;
            }
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

fn required_text_field<'a>(
    record: &'a Map<String, Value>,
    line: usize,
    name: &str,
) -> Result<&'a str, AuditLogVerifyError> {
    record
        .get(name)
        .and_then(Value::as_str)
        .ok_or_else(|| AuditLogVerifyError::InvalidRecord {
            line,
            reason: format!("{name} missing or non-string"),
        })
}

fn audit_record_hash(record: &Map<String, Value>) -> String {
    let mut canonical = BTreeMap::new();
    for (key, value) in record {
        if key != "hash" {
            canonical.insert(key, value);
        }
    }
    let encoded = serde_json::to_vec(&canonical).expect("serialize audit record");
    blake3::hash(&encoded).to_hex().to_string()
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
        assert_eq!(parsed["prev_hash"], AUDIT_LOG_GENESIS_HASH);
        assert!(parsed["hash"].as_str().is_some());

        fs::remove_dir_all(root).expect("cleanup");
    }

    #[test]
    fn audit_log_verifier_detects_tampering() {
        let root = temp_root("audit-chain");
        create_layout(&root);
        let path = audit_log_path(&root);
        let file = OpenOptions::new()
            .create(true)
            .append(true)
            .open(&path)
            .expect("open audit log");
        let subscriber = tracing_subscriber::registry().with(AuditJsonLayer::new(file));

        tracing::subscriber::with_default(subscriber, || {
            tracing::info!(
                event = "agent.first",
                msg = "first message",
                msg_id = "msg-1"
            );
            tracing::info!(
                event = "agent.second",
                msg = "second message",
                msg_id = "msg-2"
            );
        });

        let verification = verify_audit_log(&path).expect("valid audit chain");
        assert_eq!(verification.entries, 2);
        assert_ne!(verification.head_hash, AUDIT_LOG_GENESIS_HASH);

        let tampered = fs::read_to_string(&path)
            .expect("read audit log")
            .replace("second message", "tampered message");
        fs::write(&path, tampered).expect("write tampered audit log");

        let error = verify_audit_log(&path).expect_err("tampering rejected");
        assert!(matches!(
            error,
            AuditLogVerifyError::HashMismatch { line: 2, .. }
        ));

        fs::remove_dir_all(root).expect("cleanup");
    }

    #[test]
    fn panic_hook_writes_verifiable_panic_entry() {
        let root = temp_root("panic-hook");
        create_layout(&root);
        init_audit_tracing(&root).expect("init audit tracing");
        let hook = install_panic_audit_hook();

        let result = std::panic::catch_unwind(|| {
            panic!("induced panic");
        });

        drop(hook);
        assert!(result.is_err());

        let verification = verify_audit_log(&audit_log_path(&root)).expect("verify audit log");
        assert_eq!(verification.entries, 1);

        let content = fs::read_to_string(audit_log_path(&root)).expect("read audit log");
        let entry: Value =
            serde_json::from_str(content.lines().next().expect("panic entry")).expect("parse JSON");
        assert_eq!(entry["event"], "panic");
        assert_eq!(entry["msg_id"], "agent-panic");

        fs::remove_dir_all(root).expect("cleanup");
    }
}
