use std::fmt::{Display, Formatter};
use std::fs;
use std::io;
use std::path::{Path, PathBuf};
use std::time::{SystemTime, UNIX_EPOCH};

use rand_core::{OsRng, RngCore};
use serde::{Deserialize, Serialize};

use crate::{PiLocalPolicyDecision, PiPolicyAction};

pub const APPROVALS_DIR: &str = "approvals";
pub const DEFAULT_APPROVAL_TTL_SECONDS: u64 = 15 * 60;

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "kebab-case")]
pub enum ApprovalStatus {
    Pending,
    Approved,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct ApprovalRecord {
    pub token: String,
    pub scope_id: String,
    pub gate: String,
    pub required_action: String,
    pub matched_rule_ids: Vec<String>,
    pub reason: String,
    pub created_at_unix: u64,
    pub expires_at_unix: u64,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub approved_at_unix: Option<u64>,
    pub status: ApprovalStatus,
}

#[derive(Debug)]
pub enum ApprovalError {
    Io(io::Error),
    Json(serde_json::Error),
    NotFound(String),
    Pending(String),
    Expired {
        token: String,
        expires_at_unix: u64,
        now_unix: u64,
    },
    ScopeMismatch {
        token: String,
        expected: String,
        found: String,
    },
    NotRequired,
}

impl Display for ApprovalError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            ApprovalError::Io(error) => write!(formatter, "{error}"),
            ApprovalError::Json(error) => write!(formatter, "{error}"),
            ApprovalError::NotFound(token) => {
                write!(formatter, "approval token not found: {token}")
            }
            ApprovalError::Pending(token) => {
                write!(formatter, "approval token is pending: {token}")
            }
            ApprovalError::Expired {
                token,
                expires_at_unix,
                now_unix,
            } => write!(
                formatter,
                "approval token expired: {token} expired at {expires_at_unix}, now {now_unix}"
            ),
            ApprovalError::ScopeMismatch {
                token,
                expected,
                found,
            } => write!(
                formatter,
                "approval token scope mismatch: {token} expected {expected}, found {found}"
            ),
            ApprovalError::NotRequired => {
                write!(formatter, "policy decision does not require approval")
            }
        }
    }
}

impl std::error::Error for ApprovalError {}

impl From<io::Error> for ApprovalError {
    fn from(error: io::Error) -> Self {
        ApprovalError::Io(error)
    }
}

impl From<serde_json::Error> for ApprovalError {
    fn from(error: serde_json::Error) -> Self {
        ApprovalError::Json(error)
    }
}

pub fn request_operator_approval(
    data_dir: &Path,
    decision: &PiLocalPolicyDecision,
    scope_id: &str,
    ttl_seconds: u64,
) -> Result<ApprovalRecord, ApprovalError> {
    request_operator_approval_at(data_dir, decision, scope_id, ttl_seconds, now_unix())
}

fn request_operator_approval_at(
    data_dir: &Path,
    decision: &PiLocalPolicyDecision,
    scope_id: &str,
    ttl_seconds: u64,
    now: u64,
) -> Result<ApprovalRecord, ApprovalError> {
    if decision.action != PiPolicyAction::RequireApproval {
        return Err(ApprovalError::NotRequired);
    }

    let record = ApprovalRecord {
        token: generate_token(),
        scope_id: scope_id.to_string(),
        gate: decision.gate.as_str().to_string(),
        required_action: decision.action.as_str().to_string(),
        matched_rule_ids: decision.matched_rule_ids.clone(),
        reason: decision.reason.clone(),
        created_at_unix: now,
        expires_at_unix: now.saturating_add(ttl_seconds),
        approved_at_unix: None,
        status: ApprovalStatus::Pending,
    };
    write_approval_record(data_dir, &record)?;
    tracing::info!(
        event = "approval.requested",
        msg = "operator approval requested",
        msg_id = "approval-requested",
        token = record.token.as_str(),
        scope_id = record.scope_id.as_str(),
        gate = record.gate.as_str(),
        required_action = record.required_action.as_str(),
        expires_at_unix = record.expires_at_unix
    );
    Ok(record)
}

pub fn approve_operator_token(
    data_dir: &Path,
    token: &str,
) -> Result<ApprovalRecord, ApprovalError> {
    approve_operator_token_at(data_dir, token, now_unix())
}

fn approve_operator_token_at(
    data_dir: &Path,
    token: &str,
    now: u64,
) -> Result<ApprovalRecord, ApprovalError> {
    let mut record = read_approval_record(data_dir, token)?;
    reject_expired(&record, now)?;
    record.status = ApprovalStatus::Approved;
    record.approved_at_unix = Some(now);
    write_approval_record(data_dir, &record)?;
    tracing::info!(
        event = "approval.approved",
        msg = "operator approval accepted",
        msg_id = "approval-approved",
        token = record.token.as_str(),
        scope_id = record.scope_id.as_str(),
        gate = record.gate.as_str()
    );
    Ok(record)
}

pub fn decision_after_approval(
    data_dir: &Path,
    decision: &PiLocalPolicyDecision,
    scope_id: &str,
    token: &str,
) -> Result<PiLocalPolicyDecision, ApprovalError> {
    decision_after_approval_at(data_dir, decision, scope_id, token, now_unix())
}

fn decision_after_approval_at(
    data_dir: &Path,
    decision: &PiLocalPolicyDecision,
    scope_id: &str,
    token: &str,
    now: u64,
) -> Result<PiLocalPolicyDecision, ApprovalError> {
    if decision.action != PiPolicyAction::RequireApproval {
        return Ok(decision.clone());
    }

    let record = read_approval_record(data_dir, token)?;
    reject_expired(&record, now)?;
    if record.scope_id != scope_id {
        return Err(ApprovalError::ScopeMismatch {
            token: token.to_string(),
            expected: record.scope_id,
            found: scope_id.to_string(),
        });
    }
    if record.status != ApprovalStatus::Approved {
        return Err(ApprovalError::Pending(token.to_string()));
    }

    let mut approved = decision.clone();
    approved.action = PiPolicyAction::Allow;
    approved.reason = format!("approved by operator token {}", record.token);
    approved.approver_role = None;
    Ok(approved)
}

pub fn read_approval_record(data_dir: &Path, token: &str) -> Result<ApprovalRecord, ApprovalError> {
    let path = approval_path(data_dir, token);
    if !path.exists() {
        return Err(ApprovalError::NotFound(token.to_string()));
    }
    Ok(serde_json::from_slice(&fs::read(path)?)?)
}

fn write_approval_record(data_dir: &Path, record: &ApprovalRecord) -> Result<(), ApprovalError> {
    let dir = data_dir.join(APPROVALS_DIR);
    fs::create_dir_all(&dir)?;
    fs::write(
        approval_path(data_dir, &record.token),
        serde_json::to_vec_pretty(record)?,
    )?;
    Ok(())
}

fn approval_path(data_dir: &Path, token: &str) -> PathBuf {
    data_dir.join(APPROVALS_DIR).join(format!("{token}.json"))
}

fn reject_expired(record: &ApprovalRecord, now: u64) -> Result<(), ApprovalError> {
    if now >= record.expires_at_unix {
        return Err(ApprovalError::Expired {
            token: record.token.clone(),
            expires_at_unix: record.expires_at_unix,
            now_unix: now,
        });
    }
    Ok(())
}

fn generate_token() -> String {
    let mut bytes = [0_u8; 16];
    OsRng.fill_bytes(&mut bytes);
    encode_hex(bytes)
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

fn now_unix() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_secs()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{evaluate_local_policy, PiLocalPolicyRequest, PiPolicyGate};
    use std::time::{SystemTime, UNIX_EPOCH};

    #[test]
    fn approval_blocks_until_operator_approves_token() {
        let root = temp_root("approval-flow");
        let decision = scanner_decision();
        let record =
            request_operator_approval(&root, &decision, "scope-a", DEFAULT_APPROVAL_TTL_SECONDS)
                .expect("request approval");

        let pending = decision_after_approval(&root, &decision, "scope-a", &record.token)
            .expect_err("pending");
        assert!(matches!(pending, ApprovalError::Pending(_)));

        approve_operator_token(&root, &record.token).expect("approve token");
        let approved = decision_after_approval(&root, &decision, "scope-a", &record.token)
            .expect("approved decision");

        assert_eq!(approved.action, PiPolicyAction::Allow);
        assert_eq!(
            approved.reason,
            format!("approved by operator token {}", record.token)
        );
        fs::remove_dir_all(root).ok();
    }

    #[test]
    fn expired_approval_token_is_rejected() {
        let root = temp_root("approval-expired");
        let decision = scanner_decision();
        let record =
            request_operator_approval_at(&root, &decision, "scope-a", 10, 100).expect("request");

        let approve_error =
            approve_operator_token_at(&root, &record.token, 110).expect_err("expired approve");
        assert!(matches!(approve_error, ApprovalError::Expired { .. }));

        approve_operator_token_at(&root, &record.token, 109).expect("approve before expiry");
        let decision_error =
            decision_after_approval_at(&root, &decision, "scope-a", &record.token, 110)
                .expect_err("expired decision");
        assert!(matches!(decision_error, ApprovalError::Expired { .. }));
        fs::remove_dir_all(root).ok();
    }

    #[test]
    fn approval_token_is_scope_bound() {
        let root = temp_root("approval-scope");
        let decision = scanner_decision();
        let record =
            request_operator_approval_at(&root, &decision, "scope-a", 10, 100).expect("request");
        approve_operator_token_at(&root, &record.token, 101).expect("approve");

        let error = decision_after_approval_at(&root, &decision, "scope-b", &record.token, 102)
            .expect_err("scope mismatch");

        assert!(matches!(error, ApprovalError::ScopeMismatch { .. }));
        fs::remove_dir_all(root).ok();
    }

    fn scanner_decision() -> PiLocalPolicyDecision {
        evaluate_local_policy(&PiLocalPolicyRequest {
            gate: PiPolicyGate::ScannerInvocation,
            command: Some("nuclei -u http://target.local".to_string()),
            path: None,
            host: None,
            mutating: false,
            allowed: true,
        })
    }

    fn temp_root(name: &str) -> PathBuf {
        let nonce = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("clock")
            .as_nanos();
        let root = std::env::temp_dir().join(format!("kelp-pi-{name}-{nonce}"));
        fs::create_dir_all(&root).expect("create temp root");
        root
    }
}
