use std::fmt::{Display, Formatter};
use std::fs;
use std::io;
use std::path::Path;
use std::str::FromStr;

use ed25519_dalek::VerifyingKey;
use serde::{Deserialize, Serialize};
use serde_json::{Map, Value};
use sha2::{Digest, Sha256};

use crate::{verify_envelope, EnvelopeError, PiEnvelopeKind, PiEnvelopeSender, PiWireEnvelope};

pub const APPSEC_AGENT_BASELINE_PACK_ID: &str = "appsec-agent-baseline";
pub const APPSEC_AGENT_BASELINE_VERSION: &str = "1.0.0";
pub const CURRENT_POLICY_FILE: &str = "current-policy.json";

#[derive(Debug, Clone, Copy, Serialize, PartialEq, Eq)]
#[serde(rename_all = "kebab-case")]
pub enum PiPolicyAction {
    Allow,
    RequireApproval,
    Deny,
    LogOnly,
}

#[derive(Debug, Clone, Copy, Serialize, PartialEq, Eq)]
pub struct PiPolicyRule {
    pub id: &'static str,
    pub action: PiPolicyAction,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub approver_role: Option<&'static str>,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct PiPolicyDecision {
    pub action: PiPolicyAction,
    pub matched_rule_ids: Vec<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub approver_role: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PiPolicyVocabularyError {
    UnknownAction(String),
    UnknownRuleId(String),
}

#[derive(Debug)]
pub enum PolicyDeliveryError {
    Io(io::Error),
    Json(serde_json::Error),
    Envelope(EnvelopeError),
    InvalidEnvelope(String),
    UnknownSigner,
    HashMismatch { expected: String, found: String },
    InvalidKey(String),
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
pub enum PolicyTrustState {
    Trusted,
    Retiring,
    Revoked,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct TrustedControlPlaneKey {
    pub key_id: String,
    pub public_key_raw_hex: String,
    pub state: PolicyTrustState,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct PolicyPushPayload {
    pub policy_pack_id: String,
    pub policy_hash: String,
    pub trust_epoch: u64,
    pub issued_at: String,
    pub trust_list: Vec<PolicyPushTrustEntry>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub policy: Option<Value>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct PolicyPushTrustEntry {
    pub key_id: String,
    pub device_id: String,
    pub state: PolicyTrustState,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct StoredPolicyPack {
    pub msg_id: String,
    pub signer_key_id: String,
    pub payload: PolicyPushPayload,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct PolicyPushReceipt {
    pub policy_pack_id: String,
    pub signer_key_id: String,
    pub trust_epoch: u64,
    pub path: String,
}

pub const APPSEC_AGENT_BASELINE_RULES: &[PiPolicyRule] = &[
    PiPolicyRule {
        id: "appsec-agent-deny-destructive-shell",
        action: PiPolicyAction::Deny,
        approver_role: None,
    },
    PiPolicyRule {
        id: "appsec-agent-deny-secret-exfil",
        action: PiPolicyAction::Deny,
        approver_role: None,
    },
    PiPolicyRule {
        id: "appsec-agent-deny-exploit-execution",
        action: PiPolicyAction::Deny,
        approver_role: None,
    },
    PiPolicyRule {
        id: "appsec-agent-deny-persistence-lateral",
        action: PiPolicyAction::Deny,
        approver_role: None,
    },
    PiPolicyRule {
        id: "appsec-agent-review-active-scanner",
        action: PiPolicyAction::RequireApproval,
        approver_role: Some("appsec-reviewer"),
    },
    PiPolicyRule {
        id: "appsec-agent-review-container-run",
        action: PiPolicyAction::RequireApproval,
        approver_role: Some("appsec-reviewer"),
    },
    PiPolicyRule {
        id: "appsec-agent-log-docker-build",
        action: PiPolicyAction::LogOnly,
        approver_role: None,
    },
];

impl PiPolicyAction {
    pub fn as_str(self) -> &'static str {
        match self {
            PiPolicyAction::Allow => "allow",
            PiPolicyAction::RequireApproval => "require-approval",
            PiPolicyAction::Deny => "deny",
            PiPolicyAction::LogOnly => "log-only",
        }
    }
}

impl FromStr for PiPolicyAction {
    type Err = PiPolicyVocabularyError;

    fn from_str(value: &str) -> Result<Self, Self::Err> {
        match value {
            "allow" => Ok(PiPolicyAction::Allow),
            "require-approval" => Ok(PiPolicyAction::RequireApproval),
            "deny" => Ok(PiPolicyAction::Deny),
            "log-only" => Ok(PiPolicyAction::LogOnly),
            other => Err(PiPolicyVocabularyError::UnknownAction(other.to_string())),
        }
    }
}

impl Display for PiPolicyAction {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        formatter.write_str(self.as_str())
    }
}

impl Display for PiPolicyVocabularyError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            PiPolicyVocabularyError::UnknownAction(action) => {
                write!(formatter, "unknown policy action: {action}")
            }
            PiPolicyVocabularyError::UnknownRuleId(rule_id) => {
                write!(formatter, "unknown policy rule id: {rule_id}")
            }
        }
    }
}

impl std::error::Error for PiPolicyVocabularyError {}

impl Display for PolicyDeliveryError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            PolicyDeliveryError::Io(error) => write!(formatter, "{error}"),
            PolicyDeliveryError::Json(error) => write!(formatter, "{error}"),
            PolicyDeliveryError::Envelope(error) => write!(formatter, "{error}"),
            PolicyDeliveryError::InvalidEnvelope(message) => write!(formatter, "{message}"),
            PolicyDeliveryError::UnknownSigner => {
                write!(formatter, "policy.push signer is not trusted")
            }
            PolicyDeliveryError::HashMismatch { expected, found } => {
                write!(
                    formatter,
                    "policy hash mismatch: expected {expected}, found {found}"
                )
            }
            PolicyDeliveryError::InvalidKey(message) => write!(formatter, "invalid key: {message}"),
        }
    }
}

impl std::error::Error for PolicyDeliveryError {}

impl From<io::Error> for PolicyDeliveryError {
    fn from(error: io::Error) -> Self {
        PolicyDeliveryError::Io(error)
    }
}

impl From<serde_json::Error> for PolicyDeliveryError {
    fn from(error: serde_json::Error) -> Self {
        PolicyDeliveryError::Json(error)
    }
}

impl From<EnvelopeError> for PolicyDeliveryError {
    fn from(error: EnvelopeError) -> Self {
        PolicyDeliveryError::Envelope(error)
    }
}

pub fn appsec_agent_baseline_rule(id: &str) -> Option<&'static PiPolicyRule> {
    APPSEC_AGENT_BASELINE_RULES
        .iter()
        .find(|rule| rule.id == id)
}

pub fn recognize_appsec_agent_baseline_decision(
    action: &str,
    matched_rule_ids: &[&str],
) -> Result<PiPolicyDecision, PiPolicyVocabularyError> {
    let action = PiPolicyAction::from_str(action)?;
    let mut approver_role = None;

    for rule_id in matched_rule_ids {
        let Some(rule) = appsec_agent_baseline_rule(rule_id) else {
            return Err(PiPolicyVocabularyError::UnknownRuleId(
                (*rule_id).to_string(),
            ));
        };
        if approver_role.is_none() {
            approver_role = rule.approver_role.map(str::to_string);
        }
    }

    Ok(PiPolicyDecision {
        action,
        matched_rule_ids: matched_rule_ids
            .iter()
            .map(|rule_id| (*rule_id).to_string())
            .collect(),
        approver_role,
    })
}

pub fn apply_policy_push(
    data_dir: &Path,
    envelope: &PiWireEnvelope,
    trusted_keys: &[TrustedControlPlaneKey],
) -> Result<PolicyPushReceipt, PolicyDeliveryError> {
    if envelope.sender != PiEnvelopeSender::Cp || envelope.kind != PiEnvelopeKind::PolicyPush {
        return Err(PolicyDeliveryError::InvalidEnvelope(
            "expected cp policy.push envelope".to_string(),
        ));
    }

    let signer_key_id = verify_with_trusted_key(envelope, trusted_keys)?;
    let payload: PolicyPushPayload =
        serde_json::from_value(Value::Object(envelope.payload.clone()))?;
    if let Some(policy) = &payload.policy {
        let found = policy_sha256(policy)?;
        if found != payload.policy_hash {
            return Err(PolicyDeliveryError::HashMismatch {
                expected: payload.policy_hash.clone(),
                found,
            });
        }
    }

    let policy_dir = data_dir.join("policy");
    fs::create_dir_all(&policy_dir)?;
    let path = policy_dir.join(CURRENT_POLICY_FILE);
    let stored = StoredPolicyPack {
        msg_id: envelope.msg_id.clone(),
        signer_key_id: signer_key_id.clone(),
        payload,
    };
    fs::write(&path, serde_json::to_vec_pretty(&stored)?)?;

    Ok(PolicyPushReceipt {
        policy_pack_id: stored.payload.policy_pack_id,
        signer_key_id,
        trust_epoch: stored.payload.trust_epoch,
        path: path.display().to_string(),
    })
}

pub fn policy_sha256(policy: &Value) -> Result<String, PolicyDeliveryError> {
    let canonical = canonicalize_json(policy.clone());
    let bytes = serde_json::to_vec(&canonical)?;
    Ok(format!("sha256:{}", encode_hex(Sha256::digest(bytes))))
}

fn verify_with_trusted_key(
    envelope: &PiWireEnvelope,
    trusted_keys: &[TrustedControlPlaneKey],
) -> Result<String, PolicyDeliveryError> {
    for trusted_key in trusted_keys
        .iter()
        .filter(|key| key.state == PolicyTrustState::Trusted)
    {
        let verifying_key = verifying_key_from_hex(&trusted_key.public_key_raw_hex)?;
        if verify_envelope(envelope, &verifying_key).is_ok() {
            return Ok(trusted_key.key_id.clone());
        }
    }
    Err(PolicyDeliveryError::UnknownSigner)
}

fn verifying_key_from_hex(hex: &str) -> Result<VerifyingKey, PolicyDeliveryError> {
    let bytes = decode_hex(hex).map_err(PolicyDeliveryError::InvalidKey)?;
    let raw: [u8; 32] = bytes
        .as_slice()
        .try_into()
        .map_err(|_| PolicyDeliveryError::InvalidKey("public key must be 32 bytes".to_string()))?;
    VerifyingKey::from_bytes(&raw)
        .map_err(|error| PolicyDeliveryError::InvalidKey(error.to_string()))
}

fn canonicalize_json(value: Value) -> Value {
    match value {
        Value::Array(values) => Value::Array(values.into_iter().map(canonicalize_json).collect()),
        Value::Object(map) => {
            let mut entries: Vec<_> = map.into_iter().collect();
            entries.sort_by(|left, right| left.0.cmp(&right.0));
            let mut sorted = Map::new();
            for (key, value) in entries {
                sorted.insert(key, canonicalize_json(value));
            }
            Value::Object(sorted)
        }
        other => other,
    }
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

fn decode_hex(text: &str) -> Result<Vec<u8>, String> {
    if !text.len().is_multiple_of(2) {
        return Err("hex length is odd".to_string());
    }
    let mut bytes = Vec::with_capacity(text.len() / 2);
    for pair in text.as_bytes().chunks_exact(2) {
        let high = decode_hex_nibble(pair[0])?;
        let low = decode_hex_nibble(pair[1])?;
        bytes.push((high << 4) | low);
    }
    Ok(bytes)
}

fn decode_hex_nibble(byte: u8) -> Result<u8, String> {
    match byte {
        b'0'..=b'9' => Ok(byte - b'0'),
        b'a'..=b'f' => Ok(byte - b'a' + 10),
        b'A'..=b'F' => Ok(byte - b'A' + 10),
        _ => Err("invalid hex byte".to_string()),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{
        sign_envelope, PiEnvelopeKind, PiEnvelopeSender, PiWireEnvelope, UnsignedPiWireEnvelope,
    };
    use ed25519_dalek::SigningKey;
    use rand_core::OsRng;
    use serde_json::{json, Value};
    use std::fs;
    use std::path::PathBuf;
    use std::time::{SystemTime, UNIX_EPOCH};

    #[test]
    fn policy_actions_match_typescript_vocabulary() {
        assert_eq!(PiPolicyAction::from_str("allow"), Ok(PiPolicyAction::Allow));
        assert_eq!(PiPolicyAction::from_str("deny"), Ok(PiPolicyAction::Deny));
        assert_eq!(
            PiPolicyAction::from_str("require-approval"),
            Ok(PiPolicyAction::RequireApproval)
        );
        assert_eq!(
            PiPolicyAction::from_str("log-only"),
            Ok(PiPolicyAction::LogOnly)
        );
        assert!(PiPolicyAction::from_str("approve").is_err());
    }

    #[test]
    fn appsec_agent_baseline_rule_ids_match_typescript_pack() {
        let rules: Vec<(&str, PiPolicyAction, Option<&str>)> = APPSEC_AGENT_BASELINE_RULES
            .iter()
            .map(|rule| (rule.id, rule.action, rule.approver_role))
            .collect();

        assert_eq!(
            rules,
            vec![
                (
                    "appsec-agent-deny-destructive-shell",
                    PiPolicyAction::Deny,
                    None,
                ),
                ("appsec-agent-deny-secret-exfil", PiPolicyAction::Deny, None),
                (
                    "appsec-agent-deny-exploit-execution",
                    PiPolicyAction::Deny,
                    None,
                ),
                (
                    "appsec-agent-deny-persistence-lateral",
                    PiPolicyAction::Deny,
                    None,
                ),
                (
                    "appsec-agent-review-active-scanner",
                    PiPolicyAction::RequireApproval,
                    Some("appsec-reviewer"),
                ),
                (
                    "appsec-agent-review-container-run",
                    PiPolicyAction::RequireApproval,
                    Some("appsec-reviewer"),
                ),
                (
                    "appsec-agent-log-docker-build",
                    PiPolicyAction::LogOnly,
                    None,
                ),
            ]
        );
    }

    #[test]
    fn appsec_agent_baseline_decisions_validate_known_rule_ids() {
        let decision = recognize_appsec_agent_baseline_decision(
            "require-approval",
            &["appsec-agent-review-active-scanner"],
        )
        .expect("recognize decision");

        assert_eq!(decision.action, PiPolicyAction::RequireApproval);
        assert_eq!(
            decision.matched_rule_ids,
            vec!["appsec-agent-review-active-scanner".to_string()]
        );
        assert_eq!(decision.approver_role, Some("appsec-reviewer".to_string()));
        assert!(recognize_appsec_agent_baseline_decision("deny", &["unknown"]).is_err());
    }

    #[test]
    fn policy_push_from_trusted_signer_is_persisted() {
        let root = temp_root("trusted-policy");
        let mut rng = OsRng;
        let signing_key = SigningKey::generate(&mut rng);
        let policy = json!({ "mode": "enforce" });
        let payload = policy_push_payload(policy);
        let envelope = signed_policy_push(&signing_key, payload);
        let trusted_key = trusted_control_plane_key(&signing_key, PolicyTrustState::Trusted);

        let receipt =
            apply_policy_push(&root, &envelope, &[trusted_key.clone()]).expect("apply policy push");

        assert_eq!(receipt.signer_key_id, trusted_key.key_id);
        assert_eq!(receipt.policy_pack_id, "appsec-agent-baseline@2026-06-19");
        let stored_path = root.join("policy").join(CURRENT_POLICY_FILE);
        let stored: StoredPolicyPack =
            serde_json::from_slice(&fs::read(stored_path).expect("read policy")).expect("policy");
        assert_eq!(stored.signer_key_id, trusted_key.key_id);
        assert_eq!(stored.payload.trust_epoch, 8);
        fs::remove_dir_all(root).ok();
    }

    #[test]
    fn policy_push_from_untrusted_signer_is_rejected() {
        let root = temp_root("untrusted-policy");
        let mut rng = OsRng;
        let trusted_signing_key = SigningKey::generate(&mut rng);
        let untrusted_signing_key = SigningKey::generate(&mut rng);
        let policy = json!({ "mode": "enforce" });
        let envelope = signed_policy_push(&untrusted_signing_key, policy_push_payload(policy));
        let trusted_key =
            trusted_control_plane_key(&trusted_signing_key, PolicyTrustState::Trusted);

        let error = apply_policy_push(&root, &envelope, &[trusted_key]).expect_err("reject");

        assert!(matches!(error, PolicyDeliveryError::UnknownSigner));
        assert!(!root.join("policy").join(CURRENT_POLICY_FILE).exists());
        fs::remove_dir_all(root).ok();
    }

    #[test]
    fn policy_push_with_hash_mismatch_is_rejected() {
        let root = temp_root("bad-policy-hash");
        let mut rng = OsRng;
        let signing_key = SigningKey::generate(&mut rng);
        let mut payload = policy_push_payload(json!({ "mode": "enforce" }));
        payload.policy_hash =
            "sha256:0000000000000000000000000000000000000000000000000000000000000000".to_string();
        let envelope = signed_policy_push(&signing_key, payload);
        let trusted_key = trusted_control_plane_key(&signing_key, PolicyTrustState::Trusted);

        let error = apply_policy_push(&root, &envelope, &[trusted_key]).expect_err("reject");

        assert!(matches!(error, PolicyDeliveryError::HashMismatch { .. }));
        fs::remove_dir_all(root).ok();
    }

    fn policy_push_payload(policy: Value) -> PolicyPushPayload {
        PolicyPushPayload {
            policy_pack_id: "appsec-agent-baseline@2026-06-19".to_string(),
            policy_hash: policy_sha256(&policy).expect("policy hash"),
            trust_epoch: 8,
            issued_at: "2026-06-19T06:00:00Z".to_string(),
            trust_list: vec![PolicyPushTrustEntry {
                key_id: "sha256:0000000000000000000000000000000000000000000000000000000000000000"
                    .to_string(),
                device_id: "pi-field-01".to_string(),
                state: PolicyTrustState::Trusted,
            }],
            policy: Some(policy),
        }
    }

    fn signed_policy_push(signing_key: &SigningKey, payload: PolicyPushPayload) -> PiWireEnvelope {
        let payload = serde_json::to_value(payload)
            .expect("payload value")
            .as_object()
            .expect("payload object")
            .clone();
        sign_envelope(
            UnsignedPiWireEnvelope {
                msg_id: "msg-policy-push-1".to_string(),
                ts: "2026-06-19T06:00:01Z".to_string(),
                sender: PiEnvelopeSender::Cp,
                kind: PiEnvelopeKind::PolicyPush,
                payload,
            },
            signing_key,
        )
        .expect("sign policy push")
    }

    fn trusted_control_plane_key(
        signing_key: &SigningKey,
        state: PolicyTrustState,
    ) -> TrustedControlPlaneKey {
        let public_key = signing_key.verifying_key();
        TrustedControlPlaneKey {
            key_id: format!(
                "sha256:{}",
                encode_hex(Sha256::digest(public_key.as_bytes()))
            ),
            public_key_raw_hex: encode_hex(public_key.as_bytes()),
            state,
        }
    }

    fn temp_root(name: &str) -> PathBuf {
        let nonce = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("time after epoch")
            .as_nanos();
        std::env::temp_dir().join(format!(
            "kelp-pi-policy-{name}-{}-{nonce}",
            std::process::id()
        ))
    }
}
