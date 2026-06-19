use std::collections::BTreeMap;
use std::fmt::{Display, Formatter};
use std::fs;
use std::io;
use std::path::{Path, PathBuf};

use base64ct::{Base64, Encoding};
use ed25519_dalek::pkcs8::spki::der::pem::LineEnding;
use ed25519_dalek::pkcs8::EncodePublicKey;
use ed25519_dalek::{Signer, SigningKey};
use serde::Serialize;
use serde_json::{json, Map, Value};
use sha2::{Digest, Sha256};

use crate::{
    audit_log_path, load_or_generate_identity_key, verify_audit_log_chain, IdentityKeyError,
    DEFAULT_KEY_LABEL,
};

pub const PI_AUDIT_BUNDLE_DIR: &str = "audit-bundle";
pub const PI_BUNDLE_RESULT_FILE: &str = "result.json";
pub const PI_BUNDLE_COMPATIBILITY_FILE: &str = "compatibility.json";
pub const PI_BUNDLE_POLICY_FILE: &str = "policy-decisions.json";
pub const PI_BUNDLE_REDACTION_FILE: &str = "redaction-report.json";
pub const PI_BUNDLE_FINDINGS_FILE: &str = "findings.sarif";
pub const PI_BUNDLE_INDEX_FILE: &str = "index.html";
pub const PI_BUNDLE_AUDIT_LOG_FILE: &str = "audit-log.jsonl";
pub const PI_BUNDLE_AUDIT_CHAIN_FILE: &str = "audit-chain.json";
pub const PI_BUNDLE_NORMALIZED_FINDINGS_FILE: &str = "normalized-findings.json";
pub const PI_BUNDLE_MANIFEST_FILE: &str = "manifest.json";
pub const PI_BUNDLE_MANIFEST_SIG_FILE: &str = "manifest.sig";
pub const PI_BUNDLE_MANIFEST_PUB_FILE: &str = "manifest.pub.json";
pub const PI_BUNDLE_ATTESTATION_FILE: &str = "attestation.json";
pub const PI_BUNDLE_ATTESTATION_SIG_FILE: &str = "attestation.sig";

#[derive(Debug)]
pub enum PiBundleError {
    Io(io::Error),
    Json(serde_json::Error),
    Identity(IdentityKeyError),
    AuditChain(crate::AuditLogChainVerifyError),
    Crypto(String),
    MissingFindings(PathBuf),
    MissingBundle(PathBuf),
    MissingBundleManifest(PathBuf),
    InvalidBundleId(String),
    UnsafeBundlePath(PathBuf),
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PiBundleAssembly {
    pub run_id: String,
    pub bundle_dir: PathBuf,
    pub files: Vec<String>,
    pub manifest: String,
    pub manifest_sha256: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct PiBundleTransfer {
    pub run_id: String,
    pub bundle_id: String,
    pub manifest_hash: String,
    pub size_bytes: u64,
    pub files: Vec<PiBundleTransferFile>,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct PiBundleTransferFile {
    pub path: String,
    pub size_bytes: u64,
    pub sha256: String,
    pub content_base64: String,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct AuditBundleManifest {
    schema_version: &'static str,
    run_id: String,
    generated_at: String,
    algorithm: &'static str,
    public_key_id: String,
    files: Vec<AuditBundleManifestFile>,
}

#[derive(Debug, Serialize)]
struct AuditBundleManifestFile {
    path: String,
    size: u64,
    sha256: String,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct AuditBundleAttestation {
    schema_version: &'static str,
    run_id: String,
    generated_at: String,
    policy_pack: String,
    signer: AuditBundleSigner,
    manifest: AuditBundleAttestationManifest,
    files: Vec<String>,
    evidence: AuditBundleEvidence,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct AuditBundleSigner {
    key_id: String,
    algorithm: &'static str,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct AuditBundleAttestationManifest {
    path: &'static str,
    sha256: String,
    signature_path: &'static str,
    public_key_path: &'static str,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct AuditBundleEvidence {
    governance_report: bool,
    controls: bool,
    sarif: bool,
    web_evidence: bool,
    evidence_workspace: bool,
    hook_events: bool,
    agent_run: bool,
}

pub fn assemble_pi_audit_bundle(
    data_dir: &Path,
    key_dir: &Path,
    workspace_dir: &Path,
    output_dir: &Path,
    run_id: &str,
) -> Result<PiBundleAssembly, PiBundleError> {
    fs::create_dir_all(output_dir)?;
    let generated_at = rfc3339_now();
    let findings_path = workspace_dir.join("normalized").join("findings.json");
    if !findings_path.exists() {
        return Err(PiBundleError::MissingFindings(findings_path));
    }
    let findings: Value = serde_json::from_slice(&fs::read(&findings_path)?)?;
    let audit_chain = verify_audit_log_chain(data_dir, key_dir)?;
    let audit_log = audit_log_path(data_dir);
    let identity = load_or_generate_identity_key(key_dir, DEFAULT_KEY_LABEL)?;
    let public_key_pem = identity
        .signing_key
        .verifying_key()
        .to_public_key_pem(LineEnding::LF)
        .map_err(|error| PiBundleError::Crypto(error.to_string()))?;
    let mut files = Vec::new();

    write_json_file(
        output_dir,
        PI_BUNDLE_RESULT_FILE,
        &json!({
            "schemaVersion": "kelpclaw.pi.bundle-result.v1",
            "runId": run_id,
            "ok": true,
            "status": "succeeded",
            "policyPack": "appsec-agent-baseline",
            "mode": "pi-field",
            "generatedAt": generated_at
        }),
        &mut files,
    )?;
    write_json_file(
        output_dir,
        PI_BUNDLE_COMPATIBILITY_FILE,
        &json!({
            "schemaVersion": "kelpclaw.pi.compatibility.v1",
            "ok": true,
            "target": "kelp-pi",
            "checks": [
                {
                    "id": "pi-bundle-layout",
                    "status": "pass",
                    "message": "Pi audit bundle layout matches KelpClaw verifier contract"
                }
            ]
        }),
        &mut files,
    )?;
    write_json_file(
        output_dir,
        PI_BUNDLE_POLICY_FILE,
        &json!({
            "schemaVersion": "kelpclaw.pi.policy-decisions.v1",
            "policyPack": "appsec-agent-baseline",
            "decisions": []
        }),
        &mut files,
    )?;
    write_json_file(
        output_dir,
        PI_BUNDLE_REDACTION_FILE,
        &json!({
            "schemaVersion": "1.0.0",
            "generatedAt": generated_at,
            "redacted": false,
            "filesScanned": 0,
            "findingCount": 0,
            "findings": []
        }),
        &mut files,
    )?;
    write_json_file(
        output_dir,
        PI_BUNDLE_FINDINGS_FILE,
        &findings_sarif(&findings),
        &mut files,
    )?;
    write_json_file(
        output_dir,
        PI_BUNDLE_AUDIT_CHAIN_FILE,
        &json!({
            "schemaVersion": "kelpclaw.pi.audit-chain.v1",
            "segments": audit_chain.segments,
            "segmentEntries": audit_chain.segment_entries,
            "activeEntries": audit_chain.active_entries,
            "entries": audit_chain.entries,
            "headHash": audit_chain.head_hash,
            "verifiedAgainst": identity.metadata.key_id
        }),
        &mut files,
    )?;
    copy_bundle_file(
        output_dir,
        PI_BUNDLE_NORMALIZED_FINDINGS_FILE,
        &findings_path,
        &mut files,
    )?;
    copy_bundle_file(output_dir, PI_BUNDLE_AUDIT_LOG_FILE, &audit_log, &mut files)?;
    write_text_file(
        output_dir,
        PI_BUNDLE_INDEX_FILE,
        &render_pi_bundle_html(run_id, &findings, &files, &audit_chain.head_hash),
        &mut files,
    )?;

    let manifest = AuditBundleManifest {
        schema_version: "1.0.0",
        run_id: run_id.to_string(),
        generated_at: generated_at.clone(),
        algorithm: "ed25519",
        public_key_id: identity.metadata.key_id.clone(),
        files: manifest_files(output_dir, &files)?,
    };
    let manifest_payload = stable_json_string(&manifest)?;
    let manifest_sig = sign_stable_payload(&identity.signing_key, manifest_payload.as_bytes());
    fs::write(output_dir.join(PI_BUNDLE_MANIFEST_FILE), manifest_payload)?;
    fs::write(
        output_dir.join(PI_BUNDLE_MANIFEST_SIG_FILE),
        format!("{manifest_sig}\n"),
    )?;
    write_json_value(
        &output_dir.join(PI_BUNDLE_MANIFEST_PUB_FILE),
        &json!({
            "keyId": identity.metadata.key_id,
            "algorithm": "ed25519",
            "publicKeyPem": public_key_pem
        }),
    )?;

    let manifest_hash = sha256_file(&output_dir.join(PI_BUNDLE_MANIFEST_FILE))?;
    let attested_files = files.clone();
    let attestation = AuditBundleAttestation {
        schema_version: "1.0.0",
        run_id: run_id.to_string(),
        generated_at,
        policy_pack: "appsec-agent-baseline".to_string(),
        signer: AuditBundleSigner {
            key_id: identity.metadata.key_id,
            algorithm: "ed25519",
        },
        manifest: AuditBundleAttestationManifest {
            path: PI_BUNDLE_MANIFEST_FILE,
            sha256: manifest_hash.clone(),
            signature_path: PI_BUNDLE_MANIFEST_SIG_FILE,
            public_key_path: PI_BUNDLE_MANIFEST_PUB_FILE,
        },
        files: attested_files,
        evidence: AuditBundleEvidence {
            governance_report: false,
            controls: false,
            sarif: true,
            web_evidence: false,
            evidence_workspace: true,
            hook_events: false,
            agent_run: false,
        },
    };
    let attestation_payload = stable_json_string(&attestation)?;
    let attestation_sig =
        sign_stable_payload(&identity.signing_key, attestation_payload.as_bytes());
    fs::write(
        output_dir.join(PI_BUNDLE_ATTESTATION_FILE),
        attestation_payload,
    )?;
    fs::write(
        output_dir.join(PI_BUNDLE_ATTESTATION_SIG_FILE),
        format!("{attestation_sig}\n"),
    )?;

    files.extend([
        PI_BUNDLE_MANIFEST_FILE.to_string(),
        PI_BUNDLE_MANIFEST_SIG_FILE.to_string(),
        PI_BUNDLE_MANIFEST_PUB_FILE.to_string(),
        PI_BUNDLE_ATTESTATION_FILE.to_string(),
        PI_BUNDLE_ATTESTATION_SIG_FILE.to_string(),
    ]);
    files.sort();

    Ok(PiBundleAssembly {
        run_id: run_id.to_string(),
        bundle_dir: output_dir.to_path_buf(),
        files,
        manifest: PI_BUNDLE_MANIFEST_FILE.to_string(),
        manifest_sha256: format!("sha256:{manifest_hash}"),
    })
}

pub fn load_pi_bundle_transfer(
    data_dir: &Path,
    bundle_id: &str,
    run_id: Option<&str>,
) -> Result<PiBundleTransfer, PiBundleError> {
    validate_bundle_lookup_id(bundle_id)?;
    if let Some(run_id) = run_id {
        validate_bundle_lookup_id(run_id)?;
    }
    let bundle_dir = staged_bundle_dir(data_dir, bundle_id, run_id)?;
    let manifest_path = bundle_dir.join(PI_BUNDLE_MANIFEST_FILE);
    if !manifest_path.exists() {
        return Err(PiBundleError::MissingBundleManifest(manifest_path));
    }
    let manifest: Value = serde_json::from_slice(&fs::read(&manifest_path)?)?;
    let resolved_run_id = run_id
        .map(str::to_string)
        .or_else(|| {
            manifest
                .get("runId")
                .and_then(Value::as_str)
                .map(str::to_string)
        })
        .unwrap_or_else(|| bundle_id.to_string());
    let mut paths = Vec::new();
    collect_bundle_file_paths(&bundle_dir, &bundle_dir, &mut paths)?;
    paths.sort();
    let mut size_bytes = 0_u64;
    let mut files = Vec::with_capacity(paths.len());
    for relative_path in paths {
        let absolute_path = bundle_dir.join(&relative_path);
        let bytes = fs::read(&absolute_path)?;
        size_bytes = size_bytes.saturating_add(bytes.len() as u64);
        files.push(PiBundleTransferFile {
            path: relative_path,
            size_bytes: bytes.len() as u64,
            sha256: format!("sha256:{}", encode_hex(Sha256::digest(&bytes))),
            content_base64: Base64::encode_string(&bytes),
        });
    }
    Ok(PiBundleTransfer {
        run_id: resolved_run_id,
        bundle_id: bundle_id.to_string(),
        manifest_hash: format!("sha256:{}", sha256_file(&manifest_path)?),
        size_bytes,
        files,
    })
}

fn staged_bundle_dir(
    data_dir: &Path,
    bundle_id: &str,
    run_id: Option<&str>,
) -> Result<PathBuf, PiBundleError> {
    let bundles_dir = data_dir.join("bundles");
    let bundle_path = bundles_dir.join(bundle_id);
    if bundle_path.is_dir() {
        return Ok(bundle_path);
    }
    if let Some(run_id) = run_id {
        let run_path = bundles_dir.join(run_id);
        if run_path.is_dir() {
            return Ok(run_path);
        }
    }
    Err(PiBundleError::MissingBundle(bundle_path))
}

fn validate_bundle_lookup_id(value: &str) -> Result<(), PiBundleError> {
    if value.is_empty()
        || value == "."
        || value == ".."
        || value.contains('/')
        || value.contains('\\')
    {
        Err(PiBundleError::InvalidBundleId(value.to_string()))
    } else {
        Ok(())
    }
}

fn collect_bundle_file_paths(
    root: &Path,
    dir: &Path,
    paths: &mut Vec<String>,
) -> Result<(), PiBundleError> {
    let mut entries = fs::read_dir(dir)?.collect::<Result<Vec<_>, _>>()?;
    entries.sort_by_key(|entry| entry.path());
    for entry in entries {
        let path = entry.path();
        let metadata = fs::symlink_metadata(&path)?;
        if metadata.file_type().is_symlink() {
            return Err(PiBundleError::UnsafeBundlePath(path));
        }
        if metadata.is_dir() {
            collect_bundle_file_paths(root, &path, paths)?;
            continue;
        }
        if !metadata.is_file() {
            return Err(PiBundleError::UnsafeBundlePath(path));
        }
        let relative_path = path
            .strip_prefix(root)
            .map_err(|_| PiBundleError::UnsafeBundlePath(path.clone()))?;
        paths.push(bundle_relative_path(relative_path)?);
    }
    Ok(())
}

fn bundle_relative_path(path: &Path) -> Result<String, PiBundleError> {
    let mut parts = Vec::new();
    for component in path.components() {
        match component {
            std::path::Component::Normal(value) => {
                let Some(part) = value.to_str() else {
                    return Err(PiBundleError::UnsafeBundlePath(path.to_path_buf()));
                };
                parts.push(part.to_string());
            }
            _ => return Err(PiBundleError::UnsafeBundlePath(path.to_path_buf())),
        }
    }
    if parts.is_empty() {
        Err(PiBundleError::UnsafeBundlePath(path.to_path_buf()))
    } else {
        Ok(parts.join("/"))
    }
}

fn write_json_file(
    bundle_dir: &Path,
    file: &str,
    value: &Value,
    files: &mut Vec<String>,
) -> Result<(), PiBundleError> {
    write_json_value(&bundle_dir.join(file), value)?;
    files.push(file.to_string());
    Ok(())
}

fn write_json_value(path: &Path, value: &Value) -> Result<(), PiBundleError> {
    fs::write(path, format!("{}\n", serde_json::to_string_pretty(value)?))?;
    Ok(())
}

fn write_text_file(
    bundle_dir: &Path,
    file: &str,
    content: &str,
    files: &mut Vec<String>,
) -> Result<(), PiBundleError> {
    fs::write(bundle_dir.join(file), content)?;
    files.push(file.to_string());
    Ok(())
}

fn copy_bundle_file(
    bundle_dir: &Path,
    file: &str,
    source: &Path,
    files: &mut Vec<String>,
) -> Result<(), PiBundleError> {
    fs::copy(source, bundle_dir.join(file))?;
    files.push(file.to_string());
    Ok(())
}

fn manifest_files(
    bundle_dir: &Path,
    files: &[String],
) -> Result<Vec<AuditBundleManifestFile>, PiBundleError> {
    let mut sorted = files.to_vec();
    sorted.sort();
    sorted
        .into_iter()
        .map(|file| {
            let path = bundle_dir.join(&file);
            Ok(AuditBundleManifestFile {
                path: file,
                size: fs::metadata(&path)?.len(),
                sha256: sha256_file(&path)?,
            })
        })
        .collect()
}

fn findings_sarif(findings: &Value) -> Value {
    let results = findings
        .get("findings")
        .and_then(Value::as_array)
        .map(|items| {
            items
                .iter()
                .map(|finding| {
                    let id = finding
                        .get("id")
                        .and_then(Value::as_str)
                        .unwrap_or("kelp-pi-finding");
                    let title = finding
                        .get("title")
                        .and_then(Value::as_str)
                        .unwrap_or("Kelp Pi finding");
                    let severity = finding
                        .get("severity")
                        .and_then(Value::as_str)
                        .unwrap_or("warning");
                    let asset = finding
                        .get("asset")
                        .and_then(Value::as_str)
                        .unwrap_or("kelp-pi-target");
                    json!({
                        "ruleId": id,
                        "level": sarif_level(severity),
                        "message": { "text": title },
                        "locations": [
                            {
                                "physicalLocation": {
                                    "artifactLocation": { "uri": asset }
                                }
                            }
                        ]
                    })
                })
                .collect::<Vec<_>>()
        })
        .unwrap_or_default();
    json!({
        "version": "2.1.0",
        "$schema": "https://json.schemastore.org/sarif-2.1.0.json",
        "runs": [
            {
                "tool": {
                    "driver": {
                        "name": "kelp-pi-agent",
                        "informationUri": "https://kelpclaw.dev/pi",
                        "rules": []
                    }
                },
                "results": results
            }
        ]
    })
}

fn sarif_level(severity: &str) -> &'static str {
    match severity {
        "critical" | "high" => "error",
        "medium" | "low" => "warning",
        _ => "note",
    }
}

fn render_pi_bundle_html(
    run_id: &str,
    findings: &Value,
    files: &[String],
    audit_head: &str,
) -> String {
    let findings_rows = findings
        .get("findings")
        .and_then(Value::as_array)
        .map(|items| {
            items
                .iter()
                .map(|finding| {
                    let title = finding.get("title").and_then(Value::as_str).unwrap_or("");
                    let severity = finding
                        .get("severity")
                        .and_then(Value::as_str)
                        .unwrap_or("");
                    let asset = finding.get("asset").and_then(Value::as_str).unwrap_or("");
                    format!(
                        "<tr><td>{}</td><td>{}</td><td>{}</td></tr>",
                        escape_html(severity),
                        escape_html(title),
                        escape_html(asset)
                    )
                })
                .collect::<Vec<_>>()
                .join("\n")
        })
        .filter(|rows| !rows.is_empty())
        .unwrap_or_else(|| "<tr><td colspan=\"3\">No findings.</td></tr>".to_string());
    let file_rows = files
        .iter()
        .map(|file| {
            format!(
                "<tr><td><a href=\"{}\">{}</a></td></tr>",
                escape_html(file),
                escape_html(file)
            )
        })
        .collect::<Vec<_>>()
        .join("\n");
    format!(
        "<!doctype html>
<html lang=\"en\">
<head>
  <meta charset=\"utf-8\">
  <title>Kelp Pi Audit Bundle</title>
  <style>body{{font-family:-apple-system,BlinkMacSystemFont,\"Segoe UI\",sans-serif;margin:0;color:#172033;background:#f7f9fb}}header{{background:#13293d;color:#fff;padding:24px 32px}}main{{max-width:1120px;margin:0 auto;padding:24px 32px}}section{{background:#fff;border:1px solid #d7dee8;border-radius:8px;padding:16px;margin:0 0 16px}}table{{width:100%;border-collapse:collapse}}td,th{{border-bottom:1px solid #e4e9f0;padding:8px;text-align:left}}code{{word-break:break-all}}</style>
</head>
<body>
  <header><h1>Kelp Pi Audit Bundle</h1><p>Run {}</p></header>
  <main>
    <section><h2>Findings</h2><table><thead><tr><th>Severity</th><th>Title</th><th>Asset</th></tr></thead><tbody>{}</tbody></table></section>
    <section><h2>Evidence Sources</h2><table><tbody>{}</tbody></table></section>
    <section><h2>Policy Decisions</h2><p>Policy pack: appsec-agent-baseline.</p></section>
    <section><h2>Chain Of Custody</h2><p>Audit head: <code>{}</code></p><p>Manifest and attestation are Ed25519 signed.</p></section>
  </main>
</body>
</html>
",
        escape_html(run_id),
        findings_rows,
        file_rows,
        escape_html(audit_head)
    )
}

fn stable_json_string<T: Serialize>(value: &T) -> Result<String, PiBundleError> {
    let value = serde_json::to_value(value)?;
    Ok(serde_json::to_string_pretty(&sort_json_value(value))?)
}

fn sort_json_value(value: Value) -> Value {
    match value {
        Value::Array(values) => Value::Array(values.into_iter().map(sort_json_value).collect()),
        Value::Object(map) => {
            let entries = map.into_iter().collect::<BTreeMap<_, _>>();
            let mut sorted = Map::new();
            for (key, value) in entries {
                sorted.insert(key, sort_json_value(value));
            }
            Value::Object(sorted)
        }
        other => other,
    }
}

fn sign_stable_payload(signing_key: &SigningKey, payload: &[u8]) -> String {
    Base64::encode_string(&signing_key.sign(payload).to_bytes())
}

fn sha256_file(path: &Path) -> Result<String, PiBundleError> {
    Ok(encode_hex(Sha256::digest(fs::read(path)?)))
}

fn encode_hex(bytes: impl AsRef<[u8]>) -> String {
    const HEX: &[u8; 16] = b"0123456789abcdef";
    let bytes = bytes.as_ref();
    let mut out = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        out.push(HEX[(byte >> 4) as usize] as char);
        out.push(HEX[(byte & 0x0f) as usize] as char);
    }
    out
}

fn escape_html(value: &str) -> String {
    value
        .replace('&', "&amp;")
        .replace('<', "&lt;")
        .replace('>', "&gt;")
        .replace('"', "&quot;")
        .replace('\'', "&#39;")
}

fn rfc3339_now() -> String {
    let seconds = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|duration| duration.as_secs())
        .unwrap_or(0);
    let datetime = time_from_unix_seconds(seconds);
    format!(
        "{:04}-{:02}-{:02}T{:02}:{:02}:{:02}Z",
        datetime.year,
        datetime.month,
        datetime.day,
        datetime.hour,
        datetime.minute,
        datetime.second
    )
}

struct DateTimeParts {
    year: i64,
    month: u32,
    day: u32,
    hour: u64,
    minute: u64,
    second: u64,
}

fn time_from_unix_seconds(seconds: u64) -> DateTimeParts {
    let day = seconds / 86_400;
    let seconds_of_day = seconds % 86_400;
    let (year, month, day) = civil_from_unix_day(day as i64);
    DateTimeParts {
        year,
        month,
        day,
        hour: seconds_of_day / 3600,
        minute: (seconds_of_day % 3600) / 60,
        second: seconds_of_day % 60,
    }
}

fn civil_from_unix_day(day: i64) -> (i64, u32, u32) {
    let day = day + 719_468;
    let era = if day >= 0 { day } else { day - 146_096 } / 146_097;
    let day_of_era = day - era * 146_097;
    let year_of_era =
        (day_of_era - day_of_era / 1_460 + day_of_era / 36_524 - day_of_era / 146_096) / 365;
    let year = year_of_era + era * 400;
    let day_of_year = day_of_era - (365 * year_of_era + year_of_era / 4 - year_of_era / 100);
    let month_prime = (5 * day_of_year + 2) / 153;
    let day_of_month = day_of_year - (153 * month_prime + 2) / 5 + 1;
    let month = month_prime + if month_prime < 10 { 3 } else { -9 };
    let year = year + if month <= 2 { 1 } else { 0 };
    (year, month as u32, day_of_month as u32)
}

impl Display for PiBundleError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            PiBundleError::Io(error) => write!(formatter, "{error}"),
            PiBundleError::Json(error) => write!(formatter, "{error}"),
            PiBundleError::Identity(error) => write!(formatter, "{error}"),
            PiBundleError::AuditChain(error) => write!(formatter, "{error}"),
            PiBundleError::Crypto(message) => write!(formatter, "{message}"),
            PiBundleError::MissingFindings(path) => {
                write!(formatter, "{} is missing", path.display())
            }
            PiBundleError::MissingBundle(path) => {
                write!(formatter, "bundle {} is missing", path.display())
            }
            PiBundleError::MissingBundleManifest(path) => {
                write!(formatter, "{} is missing", path.display())
            }
            PiBundleError::InvalidBundleId(value) => {
                write!(formatter, "bundle id is unsafe: {value}")
            }
            PiBundleError::UnsafeBundlePath(path) => {
                write!(formatter, "unsafe bundle path: {}", path.display())
            }
        }
    }
}

impl std::error::Error for PiBundleError {}

impl From<io::Error> for PiBundleError {
    fn from(error: io::Error) -> Self {
        PiBundleError::Io(error)
    }
}

impl From<serde_json::Error> for PiBundleError {
    fn from(error: serde_json::Error) -> Self {
        PiBundleError::Json(error)
    }
}

impl From<IdentityKeyError> for PiBundleError {
    fn from(error: IdentityKeyError) -> Self {
        PiBundleError::Identity(error)
    }
}

impl From<crate::AuditLogChainVerifyError> for PiBundleError {
    fn from(error: crate::AuditLogChainVerifyError) -> Self {
        PiBundleError::AuditChain(error)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{write_nuclei_findings_document, AuditJsonLayer};
    use std::fs::OpenOptions;
    use std::time::{SystemTime, UNIX_EPOCH};
    use tracing_subscriber::layer::SubscriberExt;

    #[test]
    fn pi_bundle_contains_verifier_contract_files() {
        let root = temp_root("bundle");
        let data_dir = root.join("data");
        let workspace = root.join("workspace");
        let bundle = root.join("audit-bundle");
        create_layout(&data_dir);
        fs::create_dir_all(workspace.join("raw")).expect("create raw");
        let raw = workspace.join("raw").join("nuclei.jsonl");
        fs::write(
            &raw,
            r#"{"template-id":"http-missing-security-headers","matched-at":"https://app.example.test","info":{"name":"Missing security header","severity":"medium"}}"#,
        )
        .expect("write nuclei");
        write_nuclei_findings_document(
            &raw,
            &workspace.join("normalized").join("findings.json"),
            "raw/nuclei.jsonl",
        )
        .expect("normalize");
        write_test_audit_entry(&data_dir);

        let assembly = assemble_pi_audit_bundle(
            &data_dir,
            &data_dir.join("keys"),
            &workspace,
            &bundle,
            "pi-run-1",
        )
        .expect("assemble bundle");

        for file in [
            PI_BUNDLE_INDEX_FILE,
            PI_BUNDLE_RESULT_FILE,
            PI_BUNDLE_COMPATIBILITY_FILE,
            PI_BUNDLE_POLICY_FILE,
            PI_BUNDLE_REDACTION_FILE,
            PI_BUNDLE_FINDINGS_FILE,
            PI_BUNDLE_AUDIT_LOG_FILE,
            PI_BUNDLE_AUDIT_CHAIN_FILE,
            PI_BUNDLE_MANIFEST_FILE,
            PI_BUNDLE_MANIFEST_SIG_FILE,
            PI_BUNDLE_MANIFEST_PUB_FILE,
            PI_BUNDLE_ATTESTATION_FILE,
            PI_BUNDLE_ATTESTATION_SIG_FILE,
        ] {
            assert!(bundle.join(file).exists(), "{file} exists");
        }
        assert_eq!(assembly.run_id, "pi-run-1");
        assert!(assembly
            .files
            .contains(&PI_BUNDLE_FINDINGS_FILE.to_string()));
        fs::remove_dir_all(root).ok();
    }

    #[test]
    fn stable_manifest_json_matches_typescript_style_shape() {
        let manifest = AuditBundleManifest {
            schema_version: "1.0.0",
            run_id: "run-1".to_string(),
            generated_at: "2026-06-19T00:00:00Z".to_string(),
            algorithm: "ed25519",
            public_key_id: "sha256:test".to_string(),
            files: vec![AuditBundleManifestFile {
                path: "result.json".to_string(),
                size: 2,
                sha256: "ab".to_string(),
            }],
        };

        assert_eq!(
            stable_json_string(&manifest).expect("stable json"),
            "{\n  \"algorithm\": \"ed25519\",\n  \"files\": [\n    {\n      \"path\": \"result.json\",\n      \"sha256\": \"ab\",\n      \"size\": 2\n    }\n  ],\n  \"generatedAt\": \"2026-06-19T00:00:00Z\",\n  \"publicKeyId\": \"sha256:test\",\n  \"runId\": \"run-1\",\n  \"schemaVersion\": \"1.0.0\"\n}"
        );
    }

    #[test]
    fn bundle_transfer_includes_complete_staged_bundle() {
        let root = temp_root("bundle-transfer");
        let data_dir = root.join("data");
        let workspace = root.join("workspace");
        let bundle = data_dir.join("bundles").join("bundle-1");
        create_layout(&data_dir);
        fs::create_dir_all(workspace.join("raw")).expect("create raw");
        let raw = workspace.join("raw").join("nuclei.jsonl");
        fs::write(
            &raw,
            r#"{"template-id":"http-missing-security-headers","matched-at":"https://app.example.test","info":{"name":"Missing security header","severity":"medium"}}"#,
        )
        .expect("write nuclei");
        write_nuclei_findings_document(
            &raw,
            &workspace.join("normalized").join("findings.json"),
            "raw/nuclei.jsonl",
        )
        .expect("normalize");
        write_test_audit_entry(&data_dir);
        assemble_pi_audit_bundle(
            &data_dir,
            &data_dir.join("keys"),
            &workspace,
            &bundle,
            "run-1",
        )
        .expect("assemble bundle");

        let transfer =
            load_pi_bundle_transfer(&data_dir, "bundle-1", Some("run-1")).expect("transfer");

        assert_eq!(transfer.bundle_id, "bundle-1");
        assert_eq!(transfer.run_id, "run-1");
        assert!(transfer.manifest_hash.starts_with("sha256:"));
        assert!(transfer.size_bytes > 0);
        assert!(transfer
            .files
            .iter()
            .any(|file| file.path == PI_BUNDLE_MANIFEST_FILE));
        assert!(transfer
            .files
            .iter()
            .any(|file| file.path == PI_BUNDLE_MANIFEST_SIG_FILE));
        assert!(transfer
            .files
            .iter()
            .all(|file| !file.content_base64.is_empty()));
        fs::remove_dir_all(root).ok();
    }

    fn write_test_audit_entry(data_dir: &Path) {
        fs::create_dir_all(data_dir.join("audit")).expect("create audit");
        let file = OpenOptions::new()
            .create(true)
            .append(true)
            .open(crate::audit_log_path(data_dir))
            .expect("open audit log");
        let subscriber = tracing_subscriber::registry().with(AuditJsonLayer::new(file));
        tracing::subscriber::with_default(subscriber, || {
            tracing::info!(
                event = "bundle.test",
                msg = "bundle test event",
                msg_id = "bundle-test"
            );
        });
    }

    fn create_layout(root: &Path) {
        fs::create_dir_all(root).expect("create root");
        for name in crate::REQUIRED_DATA_DIRS {
            fs::create_dir_all(root.join(name)).expect("create child");
        }
    }

    fn temp_root(name: &str) -> PathBuf {
        let nonce = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("time after epoch")
            .as_nanos();
        std::env::temp_dir().join(format!("kelp-pi-{name}-{}-{nonce}", std::process::id()))
    }
}
