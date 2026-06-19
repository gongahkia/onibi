use std::fmt::{Display, Formatter};
use std::fs::{self, OpenOptions};
use std::io::{self, BufRead, BufReader, Write};
use std::panic::{self, PanicHookInfo};
use std::path::{Path, PathBuf};
use std::sync::Mutex;
use std::time::{SystemTime, UNIX_EPOCH};

use std::collections::BTreeMap;

use base64ct::{Base64UrlUnpadded, Encoding};
use ed25519_dalek::{Signature, Signer, VerifyingKey};
use serde::{Deserialize, Serialize};
use serde_json::{Map, Value};
use sha2::{Digest, Sha256};
#[cfg(unix)]
use std::os::unix::fs::{OpenOptionsExt, PermissionsExt};
use tracing::field::{Field, Visit};
use tracing::{Event, Subscriber};
use tracing_subscriber::layer::{Context, Layer, SubscriberExt};

mod approval;
mod chunking;
mod doctor;
mod eval;
mod evidence;
mod http;
mod index;
mod keys;
mod nmap;
mod nuclei;
mod policy;
mod scope;
mod selfcheck;
mod synthesis;
mod thermal;
mod wire;
mod zap;

pub use approval::{
    approve_operator_token, decision_after_approval, read_approval_record,
    request_operator_approval, ApprovalError, ApprovalRecord, ApprovalStatus, APPROVALS_DIR,
    DEFAULT_APPROVAL_TTL_SECONDS,
};
pub use chunking::{
    canonical_chunk_path, chunk_id_for, chunk_markdown, chunk_pdf_sidecar, chunk_plain_text,
    default_chunking_config, deterministic_token_windows, validate_ingest_source, ChunkingConfig,
    ContentChunk, IngestRefusal, DEFAULT_CHUNK_OVERLAP_TOKENS, DEFAULT_CHUNK_TARGET_TOKENS,
};
pub use doctor::{run_doctor, DoctorCheck, DoctorReport};
pub use eval::{
    default_gold_fixture_dir, gold_chunk_id_lines, run_gold_eval, run_synthesis_eval,
    GoldEvalCaseResult, GoldEvalError, GoldEvalReport, GoldEvalStatus, SynthesisEvalCaseResult,
    SynthesisEvalReport, DEFAULT_GOLD_TOP_K, GOLD_FIXTURE_DIR,
};
pub use evidence::{
    normalize_nuclei_jsonl, normalize_nuclei_jsonl_file, write_nuclei_findings_document,
    EvidenceAffectedInstance, EvidenceFindingsDocument, EvidenceNormalizeError, EvidenceSnippet,
    EvidenceSourceReference, NormalizedEvidenceFinding, EVIDENCE_FINDINGS_SCHEMA_VERSION,
};
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
    identity_key_paths, load_identity_key, load_identity_public_metadata,
    load_or_generate_identity_key, verifying_key_from_metadata, IdentityKey, IdentityKeyError,
    IdentityKeyMetadata, DEFAULT_KEY_LABEL, PRIVATE_KEY_FILE, PUBLIC_KEY_FILE,
};
pub use nmap::{
    probe_pinned_nmap_version, NmapVersion, NmapVersionError, PINNED_NMAP_DEBIAN_PACKAGE_VERSION,
    PINNED_NMAP_RUNTIME_VERSION,
};
pub use nuclei::{
    enforce_nuclei_templates_pin, NucleiTemplatesError, NucleiTemplatesPin,
    PINNED_NUCLEI_TEMPLATES_REVISION,
};
pub use policy::{
    apply_policy_push, appsec_agent_baseline_rule, evaluate_and_audit_local_policy,
    evaluate_and_audit_local_policy_with_mode, evaluate_local_policy,
    evaluate_local_policy_with_mode, policy_sha256, recognize_appsec_agent_baseline_decision,
    PiLocalPolicyDecision, PiLocalPolicyRequest, PiPolicyAction, PiPolicyDecision, PiPolicyGate,
    PiPolicyMode, PiPolicyRule, PiPolicyVocabularyError, PolicyDeliveryError, PolicyPushPayload,
    PolicyPushReceipt, PolicyPushTrustEntry, PolicyTrustState, StoredPolicyPack,
    TrustedControlPlaneKey, APPSEC_AGENT_BASELINE_PACK_ID, APPSEC_AGENT_BASELINE_RULES,
    APPSEC_AGENT_BASELINE_VERSION, CURRENT_POLICY_FILE, KELP_PI_DENY_OUTBOUND_NETWORK_RULE_ID,
    KELP_PI_REVIEW_FILE_MUTATION_RULE_ID, KELP_PI_REVIEW_SYNTHESIS_RULE_ID,
};
pub use scope::{
    active_scope_path, apply_scope_set, ensure_targets_in_scope, load_active_scope,
    unix_millis_now, ScopeError, ScopeMatch, ScopeSetPayload, ScopeSetReceipt, ScopeTarget,
    ScopeTargetType, StoredScope, CURRENT_SCOPE_FILE,
};
pub use selfcheck::{
    run_selfcheck, selfcheck_report_payload, validate_selfcheck_target, SelfcheckCheck,
    SelfcheckReport, SelfcheckStatus, SelfcheckTargetError,
};
pub use synthesis::{synthesize_with_citation_guard, SynthesisAnswer, SynthesisError};
pub use thermal::{
    evaluate_scan_thermal_guard, evaluate_scan_thermal_guard_with_limit, ThermalScanDecision,
    ThermalScanGuard, ThermalStatus, DEFAULT_SCAN_THERMAL_MAX_CELSIUS, THERMAL_TEMP_PATH_ENV,
    THROTTLED_PATH_ENV,
};
pub use wire::{
    canonical_unsigned_envelope_bytes, sign_envelope, verify_envelope, EnvelopeError,
    PiEnvelopeKind, PiEnvelopeSender, PiWireEnvelope, UnsignedPiWireEnvelope,
};
pub use zap::{
    evaluate_zap_guard, ZapDecision, ZapGuard, ZapStatus, ZAP_MEMINFO_PATH_ENV, ZAP_MODEL_PATH_ENV,
    ZAP_PI_MIN_RAM_BYTES,
};

pub const DEFAULT_DATA_DIR: &str = "/var/lib/kelp-pi";
pub const AUDIT_LOG_FILE: &str = "agent.jsonl";
pub const AUDIT_SEGMENT_MANIFEST_SCHEMA_VERSION: u32 = 1;
pub const AUDIT_SEGMENT_MANIFEST_SUFFIX: &str = ".manifest.json";
pub const FIRMWARE_UPDATE_MANIFEST_FILE: &str = "manifest.json";
pub const FIRMWARE_UPDATE_MANIFEST_SCHEMA_VERSION: u32 = 1;
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

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct WipeReport {
    pub data_dir: String,
    pub files_zeroed: usize,
    pub bytes_zeroed: u64,
    pub entries_removed: usize,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct FirmwareUpdateManifest {
    pub schema_version: u32,
    pub update_id: String,
    pub version: String,
    pub created_at_unix_ms: u64,
    pub payload_file: String,
    pub payload_blake3: String,
    pub signer_key_id: String,
    pub signature: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct FirmwareUpdateReceipt {
    pub update_id: String,
    pub version: String,
    pub staged_dir: String,
    pub payload_file: String,
    pub payload_blake3: String,
    pub signer_key_id: String,
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
    Chain(AuditLogChainVerifyError),
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

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct AuditLogSegmentManifest {
    pub schema_version: u32,
    pub created_at_unix_ms: u64,
    pub segment_file: String,
    pub entries: usize,
    pub prev_hash: String,
    pub head_hash: String,
    pub segment_blake3: String,
    pub signer_key_id: String,
    pub signature: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AuditLogRotation {
    pub active_log_path: PathBuf,
    pub segment_path: PathBuf,
    pub manifest_path: PathBuf,
    pub manifest: AuditLogSegmentManifest,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AuditLogChainVerification {
    pub segments: usize,
    pub segment_entries: usize,
    pub active_entries: usize,
    pub entries: usize,
    pub head_hash: String,
}

#[derive(Debug)]
pub enum AuditLogChainVerifyError {
    Io(io::Error),
    Json {
        path: PathBuf,
        source: serde_json::Error,
    },
    Log {
        path: PathBuf,
        source: AuditLogVerifyError,
    },
    Identity(IdentityKeyError),
    InvalidManifest {
        path: PathBuf,
        reason: String,
    },
    Signature {
        path: PathBuf,
        reason: String,
    },
}

#[derive(Debug)]
pub enum AuditLogRotateError {
    Io(io::Error),
    Json(serde_json::Error),
    Chain(AuditLogChainVerifyError),
    Identity(IdentityKeyError),
    EmptyActiveLog,
    Signature(String),
}

#[derive(Debug)]
pub enum WipeError {
    Io { path: PathBuf, source: io::Error },
    UnsafePath(PathBuf),
}

#[derive(Debug)]
pub enum FirmwareUpdateError {
    Io {
        path: PathBuf,
        source: io::Error,
    },
    Json {
        path: PathBuf,
        source: serde_json::Error,
    },
    InvalidManifest {
        path: PathBuf,
        reason: String,
    },
    Signature {
        path: PathBuf,
        reason: String,
    },
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
            AuditTracingError::Chain(error) => write!(formatter, "{error}"),
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

impl Display for AuditLogChainVerifyError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            AuditLogChainVerifyError::Io(error) => write!(formatter, "{error}"),
            AuditLogChainVerifyError::Json { path, source } => {
                write!(
                    formatter,
                    "{}: invalid manifest JSON: {source}",
                    path.display()
                )
            }
            AuditLogChainVerifyError::Log { path, source } => {
                write!(formatter, "{}: {source}", path.display())
            }
            AuditLogChainVerifyError::Identity(error) => write!(formatter, "{error}"),
            AuditLogChainVerifyError::InvalidManifest { path, reason } => {
                write!(formatter, "{}: invalid manifest: {reason}", path.display())
            }
            AuditLogChainVerifyError::Signature { path, reason } => {
                write!(
                    formatter,
                    "{}: invalid manifest signature: {reason}",
                    path.display()
                )
            }
        }
    }
}

impl Display for AuditLogRotateError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            AuditLogRotateError::Io(error) => write!(formatter, "{error}"),
            AuditLogRotateError::Json(error) => write!(formatter, "{error}"),
            AuditLogRotateError::Chain(error) => write!(formatter, "{error}"),
            AuditLogRotateError::Identity(error) => write!(formatter, "{error}"),
            AuditLogRotateError::EmptyActiveLog => {
                write!(formatter, "active audit log has no entries to rotate")
            }
            AuditLogRotateError::Signature(reason) => write!(formatter, "{reason}"),
        }
    }
}

impl Display for WipeError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            WipeError::Io { path, source } => write!(formatter, "{}: {source}", path.display()),
            WipeError::UnsafePath(path) => {
                write!(formatter, "{} is not safe to wipe", path.display())
            }
        }
    }
}

impl Display for FirmwareUpdateError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            FirmwareUpdateError::Io { path, source } => {
                write!(formatter, "{}: {source}", path.display())
            }
            FirmwareUpdateError::Json { path, source } => {
                write!(formatter, "{}: invalid JSON: {source}", path.display())
            }
            FirmwareUpdateError::InvalidManifest { path, reason } => {
                write!(
                    formatter,
                    "{}: invalid firmware update manifest: {reason}",
                    path.display()
                )
            }
            FirmwareUpdateError::Signature { path, reason } => {
                write!(
                    formatter,
                    "{}: invalid firmware update signature: {reason}",
                    path.display()
                )
            }
        }
    }
}

impl std::error::Error for AuditTracingError {}
impl std::error::Error for AuditLogVerifyError {}
impl std::error::Error for AuditLogChainVerifyError {}
impl std::error::Error for AuditLogRotateError {}
impl std::error::Error for WipeError {}
impl std::error::Error for FirmwareUpdateError {}

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

impl From<AuditLogChainVerifyError> for AuditTracingError {
    fn from(error: AuditLogChainVerifyError) -> Self {
        AuditTracingError::Chain(error)
    }
}

impl From<io::Error> for AuditLogVerifyError {
    fn from(error: io::Error) -> Self {
        AuditLogVerifyError::Io(error)
    }
}

impl From<io::Error> for AuditLogChainVerifyError {
    fn from(error: io::Error) -> Self {
        AuditLogChainVerifyError::Io(error)
    }
}

impl From<IdentityKeyError> for AuditLogChainVerifyError {
    fn from(error: IdentityKeyError) -> Self {
        AuditLogChainVerifyError::Identity(error)
    }
}

impl From<io::Error> for AuditLogRotateError {
    fn from(error: io::Error) -> Self {
        AuditLogRotateError::Io(error)
    }
}

impl From<serde_json::Error> for AuditLogRotateError {
    fn from(error: serde_json::Error) -> Self {
        AuditLogRotateError::Json(error)
    }
}

impl From<AuditLogChainVerifyError> for AuditLogRotateError {
    fn from(error: AuditLogChainVerifyError) -> Self {
        AuditLogRotateError::Chain(error)
    }
}

impl From<IdentityKeyError> for AuditLogRotateError {
    fn from(error: IdentityKeyError) -> Self {
        AuditLogRotateError::Identity(error)
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
    let head_hash = load_audit_log_head(root)?;
    let file = OpenOptions::new()
        .create(true)
        .append(true)
        .open(audit_log_path(root))?;
    let subscriber =
        tracing_subscriber::registry().with(AuditJsonLayer::with_prev_hash(file, head_hash));
    tracing::subscriber::set_global_default(subscriber).map_err(AuditTracingError::Subscriber)
}

pub fn rotate_audit_log(
    root: &Path,
    key_dir: &Path,
) -> Result<AuditLogRotation, AuditLogRotateError> {
    let chain = verify_audit_log_chain_state(root, key_dir, true)?;
    if chain.active_entries == 0 {
        return Err(AuditLogRotateError::EmptyActiveLog);
    }

    let active_log_path = audit_log_path(root);
    let audit_dir = root.join("audit");
    fs::create_dir_all(&audit_dir)?;
    let active_bytes = fs::read(&active_log_path)?;
    let segment_hash = blake3::hash(&active_bytes).to_hex().to_string();
    let created_at_unix_ms = unix_millis();
    let head_prefix = chain.head_hash.get(0..16).unwrap_or(&chain.head_hash);
    let segment_file = format!("agent-{created_at_unix_ms}-{head_prefix}.jsonl");
    let segment_path = audit_dir.join(&segment_file);
    let manifest_path = audit_dir.join(format!("{segment_file}{AUDIT_SEGMENT_MANIFEST_SUFFIX}"));
    let identity = load_or_generate_identity_key(key_dir, DEFAULT_KEY_LABEL)?;
    let mut manifest = AuditLogSegmentManifest {
        schema_version: AUDIT_SEGMENT_MANIFEST_SCHEMA_VERSION,
        created_at_unix_ms,
        segment_file,
        entries: chain.active_entries,
        prev_hash: chain.active_start_hash,
        head_hash: chain.head_hash.clone(),
        segment_blake3: segment_hash,
        signer_key_id: identity.metadata.key_id.clone(),
        signature: String::new(),
    };
    manifest.signature = sign_audit_segment_manifest(&manifest, &identity.signing_key)?;
    let tmp_manifest_path =
        manifest_path.with_extension(format!("manifest.json.tmp.{}", std::process::id()));
    write_json_new(&tmp_manifest_path, &manifest, 0o644)?;
    fs::rename(&active_log_path, &segment_path)?;
    fs::rename(&tmp_manifest_path, &manifest_path)?;
    write_new_empty_file(&active_log_path, 0o600)?;

    Ok(AuditLogRotation {
        active_log_path,
        segment_path,
        manifest_path,
        manifest,
    })
}

pub fn verify_audit_log_chain(
    root: &Path,
    key_dir: &Path,
) -> Result<AuditLogChainVerification, AuditLogChainVerifyError> {
    let state = verify_audit_log_chain_state(root, key_dir, false)?;
    Ok(AuditLogChainVerification {
        segments: state.segments,
        segment_entries: state.segment_entries,
        active_entries: state.active_entries,
        entries: state.segment_entries + state.active_entries,
        head_hash: state.head_hash,
    })
}

pub fn verify_audit_log(path: &Path) -> Result<AuditLogVerification, AuditLogVerifyError> {
    verify_audit_log_from(path, AUDIT_LOG_GENESIS_HASH.to_string())
}

fn verify_audit_log_from(
    path: &Path,
    initial_prev_hash: String,
) -> Result<AuditLogVerification, AuditLogVerifyError> {
    let file = fs::File::open(path)?;
    let mut expected_prev_hash = initial_prev_hash;
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

#[derive(Debug, Clone, PartialEq, Eq)]
struct AuditLogChainState {
    segments: usize,
    segment_entries: usize,
    active_entries: usize,
    active_start_hash: String,
    head_hash: String,
}

fn load_audit_log_head(root: &Path) -> Result<String, AuditLogChainVerifyError> {
    verify_audit_log_chain_state(root, &root.join("keys"), true).map(|state| state.head_hash)
}

fn verify_audit_log_chain_state(
    root: &Path,
    key_dir: &Path,
    active_missing_ok: bool,
) -> Result<AuditLogChainState, AuditLogChainVerifyError> {
    let manifest_paths = audit_segment_manifest_paths(root)?;
    let active_log_path = audit_log_path(root);

    if manifest_paths.is_empty() {
        if !active_log_path.exists() {
            if active_missing_ok {
                return Ok(AuditLogChainState {
                    segments: 0,
                    segment_entries: 0,
                    active_entries: 0,
                    active_start_hash: AUDIT_LOG_GENESIS_HASH.to_string(),
                    head_hash: AUDIT_LOG_GENESIS_HASH.to_string(),
                });
            }
            return Err(AuditLogChainVerifyError::Io(io::Error::new(
                io::ErrorKind::NotFound,
                format!("{} not found", active_log_path.display()),
            )));
        }
        let active = verify_audit_log_from(&active_log_path, AUDIT_LOG_GENESIS_HASH.to_string())
            .map_err(|source| AuditLogChainVerifyError::Log {
                path: active_log_path,
                source,
            })?;
        return Ok(AuditLogChainState {
            segments: 0,
            segment_entries: 0,
            active_entries: active.entries,
            active_start_hash: AUDIT_LOG_GENESIS_HASH.to_string(),
            head_hash: active.head_hash,
        });
    }

    let public_metadata = load_identity_public_metadata(key_dir)?;
    let verifying_key = verifying_key_from_metadata(&public_metadata)?;
    let mut expected_prev_hash = AUDIT_LOG_GENESIS_HASH.to_string();
    let mut segment_entries = 0;

    for manifest_path in &manifest_paths {
        let manifest = read_audit_segment_manifest(manifest_path)?;
        verify_audit_segment_manifest_signature(manifest_path, &manifest, &verifying_key)?;
        if manifest.schema_version != AUDIT_SEGMENT_MANIFEST_SCHEMA_VERSION {
            return Err(AuditLogChainVerifyError::InvalidManifest {
                path: manifest_path.clone(),
                reason: format!("unsupported schema version {}", manifest.schema_version),
            });
        }
        if manifest.signer_key_id != public_metadata.key_id {
            return Err(AuditLogChainVerifyError::InvalidManifest {
                path: manifest_path.clone(),
                reason: "signer key id does not match public key".to_string(),
            });
        }
        if manifest.prev_hash != expected_prev_hash {
            return Err(AuditLogChainVerifyError::InvalidManifest {
                path: manifest_path.clone(),
                reason: format!(
                    "prev_hash mismatch: expected {}, found {}",
                    expected_prev_hash, manifest.prev_hash
                ),
            });
        }
        let segment_path = root.join("audit").join(&manifest.segment_file);
        let segment_bytes = fs::read(&segment_path)?;
        let segment_hash = blake3::hash(&segment_bytes).to_hex().to_string();
        if segment_hash != manifest.segment_blake3 {
            return Err(AuditLogChainVerifyError::InvalidManifest {
                path: manifest_path.clone(),
                reason: "segment_blake3 does not match segment file".to_string(),
            });
        }
        let segment =
            verify_audit_log_from(&segment_path, expected_prev_hash.clone()).map_err(|source| {
                AuditLogChainVerifyError::Log {
                    path: segment_path.clone(),
                    source,
                }
            })?;
        if segment.entries != manifest.entries || segment.head_hash != manifest.head_hash {
            return Err(AuditLogChainVerifyError::InvalidManifest {
                path: manifest_path.clone(),
                reason: "manifest entry count or head hash does not match segment".to_string(),
            });
        }
        expected_prev_hash = manifest.head_hash;
        segment_entries += manifest.entries;
    }

    let active_start_hash = expected_prev_hash.clone();
    let active = if active_log_path.exists() {
        verify_audit_log_from(&active_log_path, expected_prev_hash).map_err(|source| {
            AuditLogChainVerifyError::Log {
                path: active_log_path.clone(),
                source,
            }
        })?
    } else {
        AuditLogVerification {
            entries: 0,
            head_hash: active_start_hash.clone(),
        }
    };

    Ok(AuditLogChainState {
        segments: manifest_paths.len(),
        segment_entries,
        active_entries: active.entries,
        active_start_hash,
        head_hash: active.head_hash,
    })
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

pub fn verify_and_stage_firmware_update(
    root: &Path,
    bundle_dir: &Path,
    trusted_key: &VerifyingKey,
) -> Result<FirmwareUpdateReceipt, FirmwareUpdateError> {
    let manifest_path = bundle_dir.join(FIRMWARE_UPDATE_MANIFEST_FILE);
    let manifest: FirmwareUpdateManifest =
        serde_json::from_slice(&fs::read(&manifest_path).map_err(|source| {
            FirmwareUpdateError::Io {
                path: manifest_path.clone(),
                source,
            }
        })?)
        .map_err(|source| FirmwareUpdateError::Json {
            path: manifest_path.clone(),
            source,
        })?;
    validate_firmware_manifest_shape(&manifest_path, &manifest, trusted_key)?;
    verify_firmware_manifest_signature(&manifest_path, &manifest, trusted_key)?;

    let payload_rel = safe_relative_path(&manifest.payload_file).ok_or_else(|| {
        FirmwareUpdateError::InvalidManifest {
            path: manifest_path.clone(),
            reason: "payload_file must be a relative path without '..'".to_string(),
        }
    })?;
    let payload_path = bundle_dir.join(&payload_rel);
    let payload_bytes = fs::read(&payload_path).map_err(|source| FirmwareUpdateError::Io {
        path: payload_path.clone(),
        source,
    })?;
    let payload_hash = blake3::hash(&payload_bytes).to_hex().to_string();
    if payload_hash != manifest.payload_blake3 {
        return Err(FirmwareUpdateError::InvalidManifest {
            path: manifest_path,
            reason: format!(
                "payload_blake3 mismatch: expected {}, found {}",
                manifest.payload_blake3, payload_hash
            ),
        });
    }

    let staged_dir = root.join("updates").join(&manifest.update_id);
    if staged_dir.exists() {
        return Err(FirmwareUpdateError::InvalidManifest {
            path: staged_dir,
            reason: "update is already staged".to_string(),
        });
    }
    let staged_payload = staged_dir.join(&payload_rel);
    if let Some(parent) = staged_payload.parent() {
        fs::create_dir_all(parent).map_err(|source| FirmwareUpdateError::Io {
            path: parent.to_path_buf(),
            source,
        })?;
    }
    fs::write(&staged_payload, payload_bytes).map_err(|source| FirmwareUpdateError::Io {
        path: staged_payload.clone(),
        source,
    })?;
    fs::write(
        staged_dir.join(FIRMWARE_UPDATE_MANIFEST_FILE),
        serde_json::to_vec_pretty(&manifest).map_err(|source| FirmwareUpdateError::Json {
            path: staged_dir.join(FIRMWARE_UPDATE_MANIFEST_FILE),
            source,
        })?,
    )
    .map_err(|source| FirmwareUpdateError::Io {
        path: staged_dir.join(FIRMWARE_UPDATE_MANIFEST_FILE),
        source,
    })?;

    let receipt = FirmwareUpdateReceipt {
        update_id: manifest.update_id,
        version: manifest.version,
        staged_dir: staged_dir.display().to_string(),
        payload_file: payload_rel.display().to_string(),
        payload_blake3: manifest.payload_blake3,
        signer_key_id: manifest.signer_key_id,
    };
    let receipt_path = staged_dir.join("receipt.json");
    fs::write(
        &receipt_path,
        serde_json::to_vec_pretty(&receipt).map_err(|source| FirmwareUpdateError::Json {
            path: receipt_path.clone(),
            source,
        })?,
    )
    .map_err(|source| FirmwareUpdateError::Io {
        path: receipt_path,
        source,
    })?;
    Ok(receipt)
}

fn validate_firmware_manifest_shape(
    path: &Path,
    manifest: &FirmwareUpdateManifest,
    trusted_key: &VerifyingKey,
) -> Result<(), FirmwareUpdateError> {
    if manifest.schema_version != FIRMWARE_UPDATE_MANIFEST_SCHEMA_VERSION {
        return Err(FirmwareUpdateError::InvalidManifest {
            path: path.to_path_buf(),
            reason: format!("unsupported schema version {}", manifest.schema_version),
        });
    }
    if !is_safe_update_id(&manifest.update_id) {
        return Err(FirmwareUpdateError::InvalidManifest {
            path: path.to_path_buf(),
            reason: "update_id must contain only ASCII letters, digits, '.', '_', or '-'"
                .to_string(),
        });
    }
    if manifest.version.trim().is_empty() {
        return Err(FirmwareUpdateError::InvalidManifest {
            path: path.to_path_buf(),
            reason: "version is required".to_string(),
        });
    }
    if manifest.payload_blake3.len() != 64
        || !manifest
            .payload_blake3
            .bytes()
            .all(|byte| byte.is_ascii_hexdigit())
    {
        return Err(FirmwareUpdateError::InvalidManifest {
            path: path.to_path_buf(),
            reason: "payload_blake3 must be 64 hex characters".to_string(),
        });
    }
    let key_id = key_id_for_verifying_key(trusted_key);
    if manifest.signer_key_id != key_id {
        return Err(FirmwareUpdateError::InvalidManifest {
            path: path.to_path_buf(),
            reason: "signer_key_id does not match trusted key".to_string(),
        });
    }
    Ok(())
}

fn verify_firmware_manifest_signature(
    path: &Path,
    manifest: &FirmwareUpdateManifest,
    trusted_key: &VerifyingKey,
) -> Result<(), FirmwareUpdateError> {
    let canonical = canonical_firmware_update_manifest_bytes(manifest).map_err(|source| {
        FirmwareUpdateError::Json {
            path: path.to_path_buf(),
            source,
        }
    })?;
    let signature_bytes = Base64UrlUnpadded::decode_vec(&manifest.signature).map_err(|error| {
        FirmwareUpdateError::Signature {
            path: path.to_path_buf(),
            reason: error.to_string(),
        }
    })?;
    let signature = Signature::try_from(signature_bytes.as_slice()).map_err(|error| {
        FirmwareUpdateError::Signature {
            path: path.to_path_buf(),
            reason: error.to_string(),
        }
    })?;
    trusted_key
        .verify_strict(&canonical, &signature)
        .map_err(|error| FirmwareUpdateError::Signature {
            path: path.to_path_buf(),
            reason: error.to_string(),
        })
}

fn canonical_firmware_update_manifest_bytes(
    manifest: &FirmwareUpdateManifest,
) -> Result<Vec<u8>, serde_json::Error> {
    let mut value = serde_json::to_value(manifest)?;
    if let Value::Object(map) = &mut value {
        map.remove("signature");
    }
    serde_json::to_vec(&canonical_json_value(value))
}

fn key_id_for_verifying_key(key: &VerifyingKey) -> String {
    let digest = Sha256::digest(key.as_bytes());
    format!("sha256:{}", encode_hex(digest))
}

fn encode_hex(bytes: impl AsRef<[u8]>) -> String {
    const HEX: &[u8; 16] = b"0123456789abcdef";
    let bytes = bytes.as_ref();
    let mut encoded = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        encoded.push(HEX[(byte >> 4) as usize] as char);
        encoded.push(HEX[(byte & 0x0f) as usize] as char);
    }
    encoded
}

fn safe_relative_path(path: &str) -> Option<PathBuf> {
    let path = Path::new(path);
    if path.as_os_str().is_empty() || path.is_absolute() {
        return None;
    }
    let mut normalized = PathBuf::new();
    for component in path.components() {
        match component {
            std::path::Component::Normal(part) => normalized.push(part),
            _ => return None,
        }
    }
    Some(normalized)
}

fn is_safe_update_id(value: &str) -> bool {
    !value.is_empty()
        && value
            .bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'.' | b'_' | b'-'))
}

pub fn wipe_data_dir(root: &Path) -> Result<WipeReport, WipeError> {
    let metadata = fs::symlink_metadata(root).map_err(|source| WipeError::Io {
        path: root.to_path_buf(),
        source,
    })?;
    let canonical = fs::canonicalize(root).map_err(|source| WipeError::Io {
        path: root.to_path_buf(),
        source,
    })?;
    if metadata.file_type().is_symlink() || !metadata.is_dir() || is_unsafe_wipe_path(&canonical) {
        return Err(WipeError::UnsafePath(root.to_path_buf()));
    }

    let mut report = WipeReport {
        data_dir: canonical.display().to_string(),
        files_zeroed: 0,
        bytes_zeroed: 0,
        entries_removed: 0,
    };
    wipe_entry(&canonical, &mut report)?;
    Ok(report)
}

fn wipe_entry(path: &Path, report: &mut WipeReport) -> Result<(), WipeError> {
    let metadata = fs::symlink_metadata(path).map_err(|source| WipeError::Io {
        path: path.to_path_buf(),
        source,
    })?;
    if metadata.is_dir() && !metadata.file_type().is_symlink() {
        let entries = fs::read_dir(path).map_err(|source| WipeError::Io {
            path: path.to_path_buf(),
            source,
        })?;
        for entry in entries {
            let entry = entry.map_err(|source| WipeError::Io {
                path: path.to_path_buf(),
                source,
            })?;
            wipe_entry(&entry.path(), report)?;
        }
        fs::remove_dir(path).map_err(|source| WipeError::Io {
            path: path.to_path_buf(),
            source,
        })?;
        report.entries_removed += 1;
        return Ok(());
    }

    if metadata.is_file() {
        zero_file(path, metadata.len(), report)?;
    }
    fs::remove_file(path).map_err(|source| WipeError::Io {
        path: path.to_path_buf(),
        source,
    })?;
    report.entries_removed += 1;
    Ok(())
}

fn zero_file(path: &Path, len: u64, report: &mut WipeReport) -> Result<(), WipeError> {
    let mut file = OpenOptions::new()
        .write(true)
        .open(path)
        .map_err(|source| WipeError::Io {
            path: path.to_path_buf(),
            source,
        })?;
    let zeros = [0_u8; 8192];
    let mut remaining = len;
    while remaining > 0 {
        let chunk_len = remaining.min(zeros.len() as u64) as usize;
        file.write_all(&zeros[..chunk_len])
            .map_err(|source| WipeError::Io {
                path: path.to_path_buf(),
                source,
            })?;
        remaining -= chunk_len as u64;
    }
    file.sync_all().map_err(|source| WipeError::Io {
        path: path.to_path_buf(),
        source,
    })?;
    report.files_zeroed += 1;
    report.bytes_zeroed += len;
    Ok(())
}

fn is_unsafe_wipe_path(path: &Path) -> bool {
    path.parent().is_none() || path == Path::new("/")
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

fn audit_segment_manifest_paths(root: &Path) -> Result<Vec<PathBuf>, io::Error> {
    let audit_dir = root.join("audit");
    let mut paths = Vec::new();
    let entries = match fs::read_dir(&audit_dir) {
        Ok(entries) => entries,
        Err(error) if error.kind() == io::ErrorKind::NotFound => return Ok(paths),
        Err(error) => return Err(error),
    };
    for entry in entries {
        let entry = entry?;
        let path = entry.path();
        if path
            .file_name()
            .and_then(|name| name.to_str())
            .is_some_and(|name| name.ends_with(AUDIT_SEGMENT_MANIFEST_SUFFIX))
        {
            paths.push(path);
        }
    }
    paths.sort();
    Ok(paths)
}

fn read_audit_segment_manifest(
    path: &Path,
) -> Result<AuditLogSegmentManifest, AuditLogChainVerifyError> {
    serde_json::from_slice(&fs::read(path)?).map_err(|source| AuditLogChainVerifyError::Json {
        path: path.to_path_buf(),
        source,
    })
}

fn sign_audit_segment_manifest(
    manifest: &AuditLogSegmentManifest,
    signing_key: &ed25519_dalek::SigningKey,
) -> Result<String, AuditLogRotateError> {
    let canonical = canonical_audit_segment_manifest_bytes(manifest)?;
    let signature: Signature = signing_key.sign(&canonical);
    Ok(Base64UrlUnpadded::encode_string(&signature.to_bytes()))
}

fn verify_audit_segment_manifest_signature(
    path: &Path,
    manifest: &AuditLogSegmentManifest,
    verifying_key: &VerifyingKey,
) -> Result<(), AuditLogChainVerifyError> {
    let canonical = canonical_audit_segment_manifest_bytes(manifest).map_err(|source| {
        AuditLogChainVerifyError::Json {
            path: path.to_path_buf(),
            source,
        }
    })?;
    let signature_bytes = Base64UrlUnpadded::decode_vec(&manifest.signature).map_err(|error| {
        AuditLogChainVerifyError::Signature {
            path: path.to_path_buf(),
            reason: error.to_string(),
        }
    })?;
    let signature = Signature::try_from(signature_bytes.as_slice()).map_err(|error| {
        AuditLogChainVerifyError::Signature {
            path: path.to_path_buf(),
            reason: error.to_string(),
        }
    })?;
    verifying_key
        .verify_strict(&canonical, &signature)
        .map_err(|error| AuditLogChainVerifyError::Signature {
            path: path.to_path_buf(),
            reason: error.to_string(),
        })
}

fn canonical_audit_segment_manifest_bytes(
    manifest: &AuditLogSegmentManifest,
) -> Result<Vec<u8>, serde_json::Error> {
    let mut value = serde_json::to_value(manifest)?;
    if let Value::Object(map) = &mut value {
        map.remove("signature");
    }
    serde_json::to_vec(&canonical_json_value(value))
}

fn canonical_json_value(value: Value) -> Value {
    match value {
        Value::Array(values) => {
            Value::Array(values.into_iter().map(canonical_json_value).collect())
        }
        Value::Object(map) => {
            let mut entries: Vec<_> = map.into_iter().collect();
            entries.sort_by(|left, right| left.0.cmp(&right.0));
            let mut sorted = Map::new();
            for (key, value) in entries {
                sorted.insert(key, canonical_json_value(value));
            }
            Value::Object(sorted)
        }
        other => other,
    }
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

fn write_json_new<T: Serialize>(
    path: &Path,
    value: &T,
    mode: u32,
) -> Result<(), AuditLogRotateError> {
    let mut options = OpenOptions::new();
    options.write(true).create_new(true);
    #[cfg(unix)]
    options.mode(mode);
    let mut file = options.open(path)?;
    serde_json::to_writer_pretty(&mut file, value)?;
    file.write_all(b"\n")?;
    file.sync_all()?;
    Ok(())
}

fn write_new_empty_file(path: &Path, mode: u32) -> Result<(), io::Error> {
    let mut options = OpenOptions::new();
    options.write(true).create_new(true);
    #[cfg(unix)]
    options.mode(mode);
    let file = options.open(path)?;
    file.sync_all()
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
    use ed25519_dalek::{Signer, SigningKey};
    use rand_core::OsRng;
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

    fn signed_firmware_manifest(
        signing_key: &SigningKey,
        update_id: &str,
        payload_file: &str,
        payload_bytes: &[u8],
    ) -> FirmwareUpdateManifest {
        let mut manifest = FirmwareUpdateManifest {
            schema_version: FIRMWARE_UPDATE_MANIFEST_SCHEMA_VERSION,
            update_id: update_id.to_string(),
            version: "2026.06.19-test".to_string(),
            created_at_unix_ms: 1,
            payload_file: payload_file.to_string(),
            payload_blake3: blake3::hash(payload_bytes).to_hex().to_string(),
            signer_key_id: key_id_for_verifying_key(&signing_key.verifying_key()),
            signature: String::new(),
        };
        let canonical =
            canonical_firmware_update_manifest_bytes(&manifest).expect("canonical manifest");
        let signature = signing_key.sign(&canonical);
        manifest.signature = Base64UrlUnpadded::encode_string(&signature.to_bytes());
        manifest
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
    fn firmware_update_stages_only_after_signature_and_hash_verify() {
        let root = temp_root("firmware-ok");
        create_layout(&root);
        let bundle = root.join("bundle");
        fs::create_dir_all(&bundle).expect("create bundle");
        let payload = b"firmware payload";
        fs::write(bundle.join("payload.tar"), payload).expect("write payload");
        let mut rng = OsRng;
        let signing_key = SigningKey::generate(&mut rng);
        let manifest = signed_firmware_manifest(&signing_key, "update-1", "payload.tar", payload);
        fs::write(
            bundle.join(FIRMWARE_UPDATE_MANIFEST_FILE),
            serde_json::to_vec_pretty(&manifest).expect("serialize manifest"),
        )
        .expect("write manifest");

        let receipt =
            verify_and_stage_firmware_update(&root, &bundle, &signing_key.verifying_key())
                .expect("stage firmware update");

        assert_eq!(receipt.update_id, "update-1");
        assert_eq!(receipt.payload_blake3, manifest.payload_blake3);
        assert_eq!(
            fs::read(root.join("updates").join("update-1").join("payload.tar"))
                .expect("read staged payload"),
            payload
        );
        assert!(root
            .join("updates")
            .join("update-1")
            .join("receipt.json")
            .exists());

        fs::remove_dir_all(root).expect("cleanup");
    }

    #[test]
    fn firmware_update_rejects_wrong_key_and_unsigned_bundle() {
        let root = temp_root("firmware-refuse");
        create_layout(&root);
        let payload = b"firmware payload";
        let mut rng = OsRng;
        let signing_key = SigningKey::generate(&mut rng);
        let wrong_key = SigningKey::generate(&mut rng);

        let wrong_key_bundle = root.join("wrong-key");
        fs::create_dir_all(&wrong_key_bundle).expect("create wrong-key bundle");
        fs::write(wrong_key_bundle.join("payload.tar"), payload).expect("write payload");
        let manifest =
            signed_firmware_manifest(&signing_key, "update-wrong-key", "payload.tar", payload);
        fs::write(
            wrong_key_bundle.join(FIRMWARE_UPDATE_MANIFEST_FILE),
            serde_json::to_vec_pretty(&manifest).expect("serialize manifest"),
        )
        .expect("write manifest");
        let error =
            verify_and_stage_firmware_update(&root, &wrong_key_bundle, &wrong_key.verifying_key())
                .expect_err("wrong key refused");
        assert!(matches!(error, FirmwareUpdateError::InvalidManifest { .. }));

        let unsigned_bundle = root.join("unsigned");
        fs::create_dir_all(&unsigned_bundle).expect("create unsigned bundle");
        fs::write(unsigned_bundle.join("payload.tar"), payload).expect("write payload");
        let mut unsigned =
            signed_firmware_manifest(&signing_key, "update-unsigned", "payload.tar", payload);
        unsigned.signature.clear();
        fs::write(
            unsigned_bundle.join(FIRMWARE_UPDATE_MANIFEST_FILE),
            serde_json::to_vec_pretty(&unsigned).expect("serialize manifest"),
        )
        .expect("write manifest");
        let error =
            verify_and_stage_firmware_update(&root, &unsigned_bundle, &signing_key.verifying_key())
                .expect_err("unsigned bundle refused");
        assert!(matches!(error, FirmwareUpdateError::Signature { .. }));

        fs::remove_dir_all(root).expect("cleanup");
    }

    #[test]
    fn wipe_data_dir_zeros_files_and_forces_rebootstrap() {
        let root = temp_root("wipe");
        create_layout(&root);
        fs::write(root.join("corpus").join("notes.md"), b"engagement notes").expect("write file");
        fs::create_dir_all(root.join("evidence").join("uploads")).expect("create nested");
        fs::write(
            root.join("evidence").join("uploads").join("raw.bin"),
            b"scanner bytes",
        )
        .expect("write nested file");

        let report = wipe_data_dir(&root).expect("wipe data dir");

        assert_eq!(report.files_zeroed, 2);
        assert_eq!(report.bytes_zeroed, 29);
        assert!(!root.exists());
        assert!(validate_data_dir(&root).is_err());
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
    fn audit_log_rotation_writes_signed_segment_and_verifies_chain() {
        let root = temp_root("audit-rotation");
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

        let rotation = rotate_audit_log(&root, &root.join("keys")).expect("rotate audit log");
        assert!(rotation.segment_path.exists());
        assert!(rotation.manifest_path.exists());
        assert!(rotation.active_log_path.exists());
        assert_eq!(
            fs::read_to_string(&rotation.active_log_path).expect("read active"),
            ""
        );
        assert_eq!(rotation.manifest.entries, 2);
        assert_eq!(rotation.manifest.prev_hash, AUDIT_LOG_GENESIS_HASH);

        let chain = verify_audit_log_chain(&root, &root.join("keys")).expect("verify chain");
        assert_eq!(chain.segments, 1);
        assert_eq!(chain.segment_entries, 2);
        assert_eq!(chain.active_entries, 0);
        assert_eq!(chain.entries, 2);
        assert_eq!(chain.head_hash, rotation.manifest.head_hash);

        let file = OpenOptions::new()
            .append(true)
            .open(&rotation.active_log_path)
            .expect("open active");
        let subscriber = tracing_subscriber::registry().with(AuditJsonLayer::with_prev_hash(
            file,
            rotation.manifest.head_hash.clone(),
        ));
        tracing::subscriber::with_default(subscriber, || {
            tracing::info!(
                event = "agent.third",
                msg = "third message",
                msg_id = "msg-3"
            );
        });

        let chain = verify_audit_log_chain(&root, &root.join("keys")).expect("verify continued");
        assert_eq!(chain.segments, 1);
        assert_eq!(chain.segment_entries, 2);
        assert_eq!(chain.active_entries, 1);
        assert_eq!(chain.entries, 3);

        let tampered_segment = fs::read_to_string(&rotation.segment_path)
            .expect("read segment")
            .replace("second message", "tampered message");
        fs::write(&rotation.segment_path, tampered_segment).expect("tamper segment");
        assert!(verify_audit_log_chain(&root, &root.join("keys")).is_err());

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
