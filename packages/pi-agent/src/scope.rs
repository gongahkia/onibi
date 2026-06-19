use std::fmt::{Display, Formatter};
use std::fs;
use std::io;
use std::net::Ipv4Addr;
use std::path::{Path, PathBuf};
use std::time::{SystemTime, UNIX_EPOCH};

use ed25519_dalek::VerifyingKey;
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sha2::{Digest, Sha256};

use crate::{verify_envelope, EnvelopeError, PiEnvelopeKind, PiEnvelopeSender, PiWireEnvelope};

pub const CURRENT_SCOPE_FILE: &str = "current-scope.json";

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct ScopeTarget {
    #[serde(rename = "type")]
    pub target_type: ScopeTargetType,
    pub value: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub ports: Option<Vec<u16>>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
pub enum ScopeTargetType {
    Cidr,
    Host,
    Ip,
    Url,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct ScopeSetPayload {
    pub scope_id: String,
    pub issued_at: String,
    pub valid_from: String,
    pub valid_until: String,
    pub targets: Vec<ScopeTarget>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct StoredScope {
    pub schema_version: u32,
    pub msg_id: String,
    pub signer_key_id: String,
    pub accepted_at_unix_ms: u64,
    pub payload: ScopeSetPayload,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct ScopeSetReceipt {
    pub scope_id: String,
    pub signer_key_id: String,
    pub valid_from: String,
    pub valid_until: String,
    pub path: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ScopeMatch {
    pub scope_id: String,
    pub matched_target: ScopeTarget,
}

#[derive(Debug)]
pub enum ScopeError {
    Io(io::Error),
    Json(serde_json::Error),
    Envelope(EnvelopeError),
    InvalidEnvelope(String),
    InvalidScope(String),
    InvalidDateTime(String),
    InactiveScope(String),
    OutOfScope { target: String, reason: String },
}

pub fn apply_scope_set(
    data_dir: &Path,
    envelope: &PiWireEnvelope,
    trusted_cp_key: &VerifyingKey,
) -> Result<ScopeSetReceipt, ScopeError> {
    if envelope.sender != PiEnvelopeSender::Cp || envelope.kind != PiEnvelopeKind::ScopeSet {
        return Err(ScopeError::InvalidEnvelope(
            "expected cp scope.set envelope".to_string(),
        ));
    }
    verify_envelope(envelope, trusted_cp_key)?;
    let payload: ScopeSetPayload = serde_json::from_value(Value::Object(envelope.payload.clone()))?;
    validate_scope_payload(&payload)?;

    let signer_key_id = key_id_for_verifying_key(trusted_cp_key);
    let stored = StoredScope {
        schema_version: 1,
        msg_id: envelope.msg_id.clone(),
        signer_key_id: signer_key_id.clone(),
        accepted_at_unix_ms: unix_millis(),
        payload,
    };
    let scope_dir = data_dir.join("scope");
    fs::create_dir_all(scope_dir.join("history"))?;
    let current_path = active_scope_path(data_dir);
    write_scope_atomic(&current_path, &stored)?;
    let history_path = scope_dir
        .join("history")
        .join(format!("{}.json", stored.payload.scope_id));
    write_scope_atomic(&history_path, &stored)?;

    tracing::info!(
        event = "scope.set",
        msg = "scope accepted",
        msg_id = "scope-set-accepted",
        scope_id = stored.payload.scope_id.as_str(),
        signer_key_id = signer_key_id.as_str(),
        target_count = stored.payload.targets.len() as u64,
        valid_from = stored.payload.valid_from.as_str(),
        valid_until = stored.payload.valid_until.as_str()
    );

    Ok(ScopeSetReceipt {
        scope_id: stored.payload.scope_id,
        signer_key_id,
        valid_from: stored.payload.valid_from,
        valid_until: stored.payload.valid_until,
        path: current_path.display().to_string(),
    })
}

pub fn load_active_scope(data_dir: &Path) -> Result<StoredScope, ScopeError> {
    Ok(serde_json::from_slice(&fs::read(active_scope_path(
        data_dir,
    ))?)?)
}

pub fn ensure_targets_in_scope(
    scope: &StoredScope,
    targets: &[String],
    now_unix_ms: u64,
) -> Result<Vec<ScopeMatch>, ScopeError> {
    ensure_scope_active(scope, now_unix_ms)?;
    let mut matches = Vec::with_capacity(targets.len());
    for target in targets {
        let parsed = parse_target(target).ok_or_else(|| ScopeError::OutOfScope {
            target: target.clone(),
            reason: "target is empty, malformed, or unsupported".to_string(),
        })?;
        let Some(matched_target) = scope
            .payload
            .targets
            .iter()
            .find(|allowed| target_matches_scope(&parsed, allowed))
        else {
            return Err(ScopeError::OutOfScope {
                target: target.clone(),
                reason: format!("outside active scope {}", scope.payload.scope_id),
            });
        };
        matches.push(ScopeMatch {
            scope_id: scope.payload.scope_id.clone(),
            matched_target: matched_target.clone(),
        });
    }
    Ok(matches)
}

pub fn unix_millis_now() -> u64 {
    unix_millis()
}

pub fn active_scope_path(data_dir: &Path) -> PathBuf {
    data_dir.join("scope").join(CURRENT_SCOPE_FILE)
}

fn validate_scope_payload(payload: &ScopeSetPayload) -> Result<(), ScopeError> {
    if !is_safe_id(&payload.scope_id) {
        return Err(ScopeError::InvalidScope(
            "scope_id must contain only ASCII letters, digits, '.', '_', or '-'".to_string(),
        ));
    }
    if payload.targets.is_empty() {
        return Err(ScopeError::InvalidScope(
            "scope must include at least one target".to_string(),
        ));
    }
    let issued_at = parse_rfc3339_utc_ms(&payload.issued_at)?;
    let valid_from = parse_rfc3339_utc_ms(&payload.valid_from)?;
    let valid_until = parse_rfc3339_utc_ms(&payload.valid_until)?;
    if issued_at > valid_until {
        return Err(ScopeError::InvalidScope(
            "issued_at must be before valid_until".to_string(),
        ));
    }
    if valid_from >= valid_until {
        return Err(ScopeError::InvalidScope(
            "valid_from must be before valid_until".to_string(),
        ));
    }
    for target in &payload.targets {
        validate_scope_target(target)?;
    }
    Ok(())
}

fn validate_scope_target(target: &ScopeTarget) -> Result<(), ScopeError> {
    if target.value.trim().is_empty() {
        return Err(ScopeError::InvalidScope(
            "target value must not be empty".to_string(),
        ));
    }
    if target
        .ports
        .as_ref()
        .is_some_and(|ports| ports.is_empty() || ports.contains(&0))
    {
        return Err(ScopeError::InvalidScope(
            "target ports must be 1..65535".to_string(),
        ));
    }
    match target.target_type {
        ScopeTargetType::Cidr => {
            if parse_cidr(&target.value).is_none() {
                return Err(ScopeError::InvalidScope(format!(
                    "invalid CIDR target: {}",
                    target.value
                )));
            }
        }
        ScopeTargetType::Host | ScopeTargetType::Ip | ScopeTargetType::Url => {
            if parse_target(&target.value).is_none() {
                return Err(ScopeError::InvalidScope(format!(
                    "invalid target: {}",
                    target.value
                )));
            }
        }
    }
    Ok(())
}

fn ensure_scope_active(scope: &StoredScope, now_unix_ms: u64) -> Result<(), ScopeError> {
    let valid_from = parse_rfc3339_utc_ms(&scope.payload.valid_from)?;
    let valid_until = parse_rfc3339_utc_ms(&scope.payload.valid_until)?;
    if now_unix_ms < valid_from {
        return Err(ScopeError::InactiveScope(format!(
            "scope {} is not valid until {}",
            scope.payload.scope_id, scope.payload.valid_from
        )));
    }
    if now_unix_ms >= valid_until {
        return Err(ScopeError::InactiveScope(format!(
            "scope {} expired at {}",
            scope.payload.scope_id, scope.payload.valid_until
        )));
    }
    Ok(())
}

fn target_matches_scope(target: &ParsedTarget, scope_target: &ScopeTarget) -> bool {
    match scope_target.target_type {
        ScopeTargetType::Cidr => cidr_target_matches(target, scope_target),
        ScopeTargetType::Host => host_target_matches(target, scope_target),
        ScopeTargetType::Ip => ip_target_matches(target, scope_target),
        ScopeTargetType::Url => url_target_matches(target, scope_target),
    }
}

fn cidr_target_matches(target: &ParsedTarget, scope_target: &ScopeTarget) -> bool {
    let Ok(ip) = target.host.parse::<Ipv4Addr>() else {
        return false;
    };
    parse_cidr(&scope_target.value).is_some_and(|cidr| ipv4_in_cidr(ip, cidr))
        && ports_match(target.port, &scope_target.ports)
}

fn host_target_matches(target: &ParsedTarget, scope_target: &ScopeTarget) -> bool {
    let Some(allowed) = parse_target(&scope_target.value) else {
        return false;
    };
    target.host.eq_ignore_ascii_case(&allowed.host)
        && explicit_or_listed_port_matches(target.port, allowed.port, &scope_target.ports)
}

fn ip_target_matches(target: &ParsedTarget, scope_target: &ScopeTarget) -> bool {
    let Some(allowed) = parse_target(&scope_target.value) else {
        return false;
    };
    target.host.parse::<Ipv4Addr>().ok() == allowed.host.parse::<Ipv4Addr>().ok()
        && explicit_or_listed_port_matches(target.port, allowed.port, &scope_target.ports)
}

fn url_target_matches(target: &ParsedTarget, scope_target: &ScopeTarget) -> bool {
    let Some(allowed) = parse_target(&scope_target.value) else {
        return false;
    };
    if allowed.scheme.is_none() {
        return false;
    }
    if allowed.specific_url && target.normalized_url != allowed.normalized_url {
        return false;
    }
    target.scheme == allowed.scheme
        && target.host.eq_ignore_ascii_case(&allowed.host)
        && explicit_or_listed_port_matches(target.port, allowed.port, &scope_target.ports)
}

fn explicit_or_listed_port_matches(
    target_port: Option<u16>,
    value_port: Option<u16>,
    ports: &Option<Vec<u16>>,
) -> bool {
    if let Some(ports) = ports {
        return target_port.is_some_and(|port| ports.contains(&port));
    }
    value_port.is_none_or(|port| target_port == Some(port))
}

fn ports_match(target_port: Option<u16>, ports: &Option<Vec<u16>>) -> bool {
    ports
        .as_ref()
        .is_none_or(|ports| target_port.is_some_and(|port| ports.contains(&port)))
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct ParsedTarget {
    scheme: Option<String>,
    host: String,
    port: Option<u16>,
    normalized_url: Option<String>,
    specific_url: bool,
}

fn parse_target(target: &str) -> Option<ParsedTarget> {
    let trimmed = target.trim();
    if trimmed.is_empty() {
        return None;
    }
    let (scheme, rest) = if let Some((scheme, rest)) = trimmed.split_once("://") {
        if scheme.is_empty() {
            return None;
        }
        (Some(scheme.to_ascii_lowercase()), rest)
    } else {
        (None, trimmed)
    };
    let (authority, suffix) = split_authority(rest);
    let authority = authority.rsplit('@').next().unwrap_or(authority);
    if authority.is_empty() || authority.starts_with('[') {
        return None;
    }
    let mut host = authority;
    let mut port = default_port(scheme.as_deref());
    if authority.matches(':').count() == 1 {
        let (candidate_host, candidate_port) = authority.rsplit_once(':')?;
        if !candidate_port.is_empty() {
            let parsed_port = candidate_port.parse::<u16>().ok()?;
            if parsed_port == 0 {
                return None;
            }
            host = candidate_host;
            port = Some(parsed_port);
        }
    } else if authority.contains(':') {
        return None;
    }
    let host = host.trim().trim_end_matches('.').to_ascii_lowercase();
    if host.is_empty() {
        return None;
    }
    let normalized_url = scheme.as_ref().map(|scheme| {
        let port_text = match (port, default_port(Some(scheme.as_str()))) {
            (Some(port), Some(default)) if port == default => String::new(),
            (Some(port), _) => format!(":{port}"),
            (None, _) => String::new(),
        };
        format!("{scheme}://{host}{port_text}{suffix}")
    });
    let specific_url = scheme.is_some() && !matches!(suffix, "" | "/");
    Some(ParsedTarget {
        scheme,
        host,
        port,
        normalized_url,
        specific_url,
    })
}

fn split_authority(rest: &str) -> (&str, &str) {
    if let Some(index) = rest.find(['/', '?', '#']) {
        rest.split_at(index)
    } else {
        (rest, "")
    }
}

fn default_port(scheme: Option<&str>) -> Option<u16> {
    match scheme {
        Some("http") => Some(80),
        Some("https") => Some(443),
        _ => None,
    }
}

fn parse_cidr(value: &str) -> Option<(Ipv4Addr, u32)> {
    let (base, prefix) = value.split_once('/')?;
    let base = base.parse::<Ipv4Addr>().ok()?;
    let prefix = prefix.parse::<u32>().ok()?;
    if prefix > 32 {
        return None;
    }
    Some((base, prefix))
}

fn ipv4_in_cidr(ip: Ipv4Addr, cidr: (Ipv4Addr, u32)) -> bool {
    let (base, prefix) = cidr;
    let mask = if prefix == 0 {
        0
    } else {
        u32::MAX << (32 - prefix)
    };
    (u32::from(ip) & mask) == (u32::from(base) & mask)
}

fn write_scope_atomic(path: &Path, scope: &StoredScope) -> Result<(), ScopeError> {
    let tmp_path = path.with_extension("tmp");
    fs::write(&tmp_path, serde_json::to_vec_pretty(scope)?)?;
    fs::rename(tmp_path, path)?;
    Ok(())
}

fn key_id_for_verifying_key(verifying_key: &VerifyingKey) -> String {
    format!(
        "sha256:{}",
        encode_hex(Sha256::digest(verifying_key.as_bytes()))
    )
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

fn is_safe_id(value: &str) -> bool {
    !value.is_empty()
        && value
            .bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'.' | b'_' | b'-'))
}

fn parse_rfc3339_utc_ms(value: &str) -> Result<u64, ScopeError> {
    if value.len() != 20 || !value.ends_with('Z') {
        return Err(ScopeError::InvalidDateTime(format!(
            "{value}: expected YYYY-MM-DDTHH:MM:SSZ"
        )));
    }
    let bytes = value.as_bytes();
    if bytes[4] != b'-'
        || bytes[7] != b'-'
        || bytes[10] != b'T'
        || bytes[13] != b':'
        || bytes[16] != b':'
    {
        return Err(ScopeError::InvalidDateTime(format!(
            "{value}: expected YYYY-MM-DDTHH:MM:SSZ"
        )));
    }
    let year = parse_digits(&bytes[0..4]).ok_or_else(|| bad_datetime(value))? as i32;
    let month = parse_digits(&bytes[5..7]).ok_or_else(|| bad_datetime(value))?;
    let day = parse_digits(&bytes[8..10]).ok_or_else(|| bad_datetime(value))?;
    let hour = parse_digits(&bytes[11..13]).ok_or_else(|| bad_datetime(value))?;
    let minute = parse_digits(&bytes[14..16]).ok_or_else(|| bad_datetime(value))?;
    let second = parse_digits(&bytes[17..19]).ok_or_else(|| bad_datetime(value))?;
    if month == 0
        || month > 12
        || day == 0
        || day > days_in_month(year, month)
        || hour > 23
        || minute > 59
        || second > 59
    {
        return Err(bad_datetime(value));
    }
    let days = days_from_civil(year, month, day);
    if days < 0 {
        return Err(ScopeError::InvalidDateTime(format!(
            "{value}: dates before 1970-01-01T00:00:00Z are unsupported"
        )));
    }
    let seconds = days as u64 * 86_400 + hour as u64 * 3_600 + minute as u64 * 60 + second as u64;
    Ok(seconds * 1_000)
}

fn parse_digits(bytes: &[u8]) -> Option<u32> {
    let mut value = 0_u32;
    for byte in bytes {
        if !byte.is_ascii_digit() {
            return None;
        }
        value = value * 10 + u32::from(byte - b'0');
    }
    Some(value)
}

fn bad_datetime(value: &str) -> ScopeError {
    ScopeError::InvalidDateTime(format!("{value}: invalid UTC date-time"))
}

fn days_in_month(year: i32, month: u32) -> u32 {
    match month {
        1 | 3 | 5 | 7 | 8 | 10 | 12 => 31,
        4 | 6 | 9 | 11 => 30,
        2 if is_leap_year(year) => 29,
        2 => 28,
        _ => 0,
    }
}

fn is_leap_year(year: i32) -> bool {
    (year % 4 == 0 && year % 100 != 0) || year % 400 == 0
}

fn days_from_civil(year: i32, month: u32, day: u32) -> i64 {
    let year = i64::from(year) - i64::from(month <= 2);
    let era = if year >= 0 { year } else { year - 399 } / 400;
    let year_of_era = year - era * 400;
    let month = i64::from(month);
    let day = i64::from(day);
    let day_of_year = (153 * (month + if month > 2 { -3 } else { 9 }) + 2) / 5 + day - 1;
    let day_of_era = year_of_era * 365 + year_of_era / 4 - year_of_era / 100 + day_of_year;
    era * 146_097 + day_of_era - 719_468
}

fn unix_millis() -> u64 {
    let millis = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|duration| duration.as_millis())
        .unwrap_or(0);
    u64::try_from(millis).unwrap_or(u64::MAX)
}

impl Display for ScopeError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            ScopeError::Io(error) => write!(formatter, "{error}"),
            ScopeError::Json(error) => write!(formatter, "{error}"),
            ScopeError::Envelope(error) => write!(formatter, "{error}"),
            ScopeError::InvalidEnvelope(message) => write!(formatter, "{message}"),
            ScopeError::InvalidScope(message) => write!(formatter, "{message}"),
            ScopeError::InvalidDateTime(message) => write!(formatter, "{message}"),
            ScopeError::InactiveScope(message) => write!(formatter, "{message}"),
            ScopeError::OutOfScope { target, reason } => write!(formatter, "{target}: {reason}"),
        }
    }
}

impl std::error::Error for ScopeError {}

impl From<io::Error> for ScopeError {
    fn from(error: io::Error) -> Self {
        ScopeError::Io(error)
    }
}

impl From<serde_json::Error> for ScopeError {
    fn from(error: serde_json::Error) -> Self {
        ScopeError::Json(error)
    }
}

impl From<EnvelopeError> for ScopeError {
    fn from(error: EnvelopeError) -> Self {
        ScopeError::Envelope(error)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{sign_envelope, UnsignedPiWireEnvelope};
    use ed25519_dalek::SigningKey;
    use rand_core::OsRng;
    use serde_json::json;

    #[test]
    fn applies_signed_scope_and_matches_targets() {
        let root = temp_root("scope-ok");
        fs::create_dir_all(root.join("scope")).expect("scope dir");
        let mut rng = OsRng;
        let signing_key = SigningKey::generate(&mut rng);
        let payload = json!({
            "scope_id": "scope-a",
            "issued_at": "2026-06-19T00:00:00Z",
            "valid_from": "2026-06-19T00:00:00Z",
            "valid_until": "2026-06-20T00:00:00Z",
            "targets": [
                { "type": "cidr", "value": "192.0.2.0/29", "ports": [80, 443] },
                { "type": "host", "value": "app.example.test", "ports": [8080] }
            ]
        });
        let envelope = sign_envelope(
            UnsignedPiWireEnvelope {
                msg_id: "scope-msg-1".to_string(),
                ts: "2026-06-19T00:00:00Z".to_string(),
                sender: PiEnvelopeSender::Cp,
                kind: PiEnvelopeKind::ScopeSet,
                payload: payload.as_object().expect("object").clone(),
            },
            &signing_key,
        )
        .expect("sign");

        let receipt =
            apply_scope_set(&root, &envelope, &signing_key.verifying_key()).expect("apply");
        let stored = load_active_scope(&root).expect("load");
        let matches = ensure_targets_in_scope(
            &stored,
            &[
                "https://192.0.2.3".to_string(),
                "app.example.test:8080".to_string(),
            ],
            parse_rfc3339_utc_ms("2026-06-19T12:00:00Z").expect("time"),
        )
        .expect("targets in scope");

        assert_eq!(receipt.scope_id, "scope-a");
        assert_eq!(stored.payload.scope_id, "scope-a");
        assert_eq!(matches.len(), 2);
        assert!(root.join("scope/history/scope-a.json").exists());
        fs::remove_dir_all(root).ok();
    }

    #[test]
    fn rejects_out_of_scope_target_and_expired_scope() {
        let stored = StoredScope {
            schema_version: 1,
            msg_id: "scope-msg-1".to_string(),
            signer_key_id: "sha256:test".to_string(),
            accepted_at_unix_ms: 1,
            payload: ScopeSetPayload {
                scope_id: "scope-a".to_string(),
                issued_at: "2026-06-19T00:00:00Z".to_string(),
                valid_from: "2026-06-19T00:00:00Z".to_string(),
                valid_until: "2026-06-20T00:00:00Z".to_string(),
                targets: vec![ScopeTarget {
                    target_type: ScopeTargetType::Cidr,
                    value: "192.0.2.0/29".to_string(),
                    ports: Some(vec![443]),
                }],
            },
        };

        let out_of_scope = ensure_targets_in_scope(
            &stored,
            &["192.0.2.8:443".to_string()],
            parse_rfc3339_utc_ms("2026-06-19T12:00:00Z").expect("time"),
        )
        .expect_err("blocked");
        assert!(matches!(out_of_scope, ScopeError::OutOfScope { .. }));

        let expired = ensure_targets_in_scope(
            &stored,
            &["192.0.2.3:443".to_string()],
            parse_rfc3339_utc_ms("2026-06-20T00:00:00Z").expect("time"),
        )
        .expect_err("expired");
        assert!(matches!(expired, ScopeError::InactiveScope(_)));
    }

    #[test]
    fn rejects_unsigned_or_wrong_signer_scope_set() {
        let root = temp_root("scope-wrong-signer");
        fs::create_dir_all(root.join("scope")).expect("scope dir");
        let mut rng = OsRng;
        let signing_key = SigningKey::generate(&mut rng);
        let wrong_key = SigningKey::generate(&mut rng);
        let payload = json!({
            "scope_id": "scope-a",
            "issued_at": "2026-06-19T00:00:00Z",
            "valid_from": "2026-06-19T00:00:00Z",
            "valid_until": "2026-06-20T00:00:00Z",
            "targets": [{ "type": "host", "value": "app.example.test" }]
        });
        let envelope = sign_envelope(
            UnsignedPiWireEnvelope {
                msg_id: "scope-msg-1".to_string(),
                ts: "2026-06-19T00:00:00Z".to_string(),
                sender: PiEnvelopeSender::Cp,
                kind: PiEnvelopeKind::ScopeSet,
                payload: payload.as_object().expect("object").clone(),
            },
            &signing_key,
        )
        .expect("sign");

        let error =
            apply_scope_set(&root, &envelope, &wrong_key.verifying_key()).expect_err("wrong key");
        assert!(matches!(error, ScopeError::Envelope(_)));
        assert!(!active_scope_path(&root).exists());
        fs::remove_dir_all(root).ok();
    }

    fn temp_root(name: &str) -> PathBuf {
        let nonce = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("clock")
            .as_nanos();
        std::env::temp_dir().join(format!("kelp-pi-scope-{name}-{nonce}"))
    }
}
