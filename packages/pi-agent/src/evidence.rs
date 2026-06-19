use std::collections::BTreeSet;
use std::fmt::{Display, Formatter};
use std::fs;
use std::io;
use std::path::Path;
use std::time::{SystemTime, UNIX_EPOCH};

use serde::Serialize;
use serde_json::{Map, Value};
use sha2::{Digest, Sha256};

pub const EVIDENCE_FINDINGS_SCHEMA_VERSION: &str = "kelpclaw.evidence.findings.v1";

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct EvidenceFindingsDocument {
    #[serde(rename = "schemaVersion")]
    pub schema_version: &'static str,
    pub findings: Vec<NormalizedEvidenceFinding>,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct NormalizedEvidenceFinding {
    pub id: String,
    pub title: String,
    pub severity: String,
    pub confidence: String,
    pub status: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub description: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub remediation: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub asset: Option<String>,
    pub weakness_ids: Vec<String>,
    pub references: Vec<String>,
    pub tags: Vec<String>,
    pub evidence: Vec<EvidenceSnippet>,
    pub source_references: Vec<EvidenceSourceReference>,
    pub affected_instances: Vec<EvidenceAffectedInstance>,
    pub first_seen: String,
    pub last_seen: String,
    pub provenance: Map<String, Value>,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct EvidenceSnippet {
    pub kind: String,
    pub value: String,
    pub redacted: bool,
    pub locator: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct EvidenceSourceReference {
    pub tool: String,
    pub input_sha256: String,
    pub raw_path: String,
    pub locator: String,
    pub metadata: Map<String, Value>,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct EvidenceAffectedInstance {
    pub asset: String,
    pub metadata: Map<String, Value>,
}

#[derive(Debug)]
pub enum EvidenceNormalizeError {
    Io(io::Error),
    Json {
        line: usize,
        source: serde_json::Error,
    },
    InvalidRecord {
        line: usize,
        reason: String,
    },
    Empty,
}

pub fn normalize_nuclei_jsonl_file(
    input_path: &Path,
    raw_path: &str,
) -> Result<EvidenceFindingsDocument, EvidenceNormalizeError> {
    let content = fs::read_to_string(input_path)?;
    let input_sha256 = format!("sha256:{}", encode_hex(Sha256::digest(content.as_bytes())));
    normalize_nuclei_jsonl(&content, raw_path, &input_sha256)
}

pub fn write_nuclei_findings_document(
    input_path: &Path,
    output_path: &Path,
    raw_path: &str,
) -> Result<EvidenceFindingsDocument, EvidenceNormalizeError> {
    let document = normalize_nuclei_jsonl_file(input_path, raw_path)?;
    if let Some(parent) = output_path.parent() {
        fs::create_dir_all(parent)?;
    }
    fs::write(output_path, serde_json::to_vec_pretty(&document)?)?;
    Ok(document)
}

pub fn normalize_nuclei_jsonl(
    content: &str,
    raw_path: &str,
    input_sha256: &str,
) -> Result<EvidenceFindingsDocument, EvidenceNormalizeError> {
    let mut findings = Vec::new();
    for (index, raw_line) in content.lines().enumerate() {
        let line = raw_line.trim();
        if line.is_empty() {
            continue;
        }
        let line_number = index + 1;
        let value: Value =
            serde_json::from_str(line).map_err(|source| EvidenceNormalizeError::Json {
                line: line_number,
                source,
            })?;
        let record = value
            .as_object()
            .ok_or_else(|| EvidenceNormalizeError::InvalidRecord {
                line: line_number,
                reason: "nuclei line must be a JSON object".to_string(),
            })?;
        let info = record
            .get("info")
            .and_then(Value::as_object)
            .ok_or_else(|| EvidenceNormalizeError::InvalidRecord {
                line: line_number,
                reason: "nuclei info must be a JSON object".to_string(),
            })?;
        findings.push(nuclei_finding(
            record,
            info,
            line_number,
            raw_path,
            input_sha256,
        ));
    }
    if findings.is_empty() {
        return Err(EvidenceNormalizeError::Empty);
    }
    Ok(EvidenceFindingsDocument {
        schema_version: EVIDENCE_FINDINGS_SCHEMA_VERSION,
        findings,
    })
}

fn nuclei_finding(
    record: &Map<String, Value>,
    info: &Map<String, Value>,
    line_number: usize,
    raw_path: &str,
    input_sha256: &str,
) -> NormalizedEvidenceFinding {
    let template_id = string_field(record, "template-id")
        .or_else(|| string_field(record, "templateID"))
        .unwrap_or_else(|| format!("line-{line_number}"));
    let title = string_field(info, "name").unwrap_or_else(|| template_id.clone());
    let asset = string_field(record, "matched-at")
        .or_else(|| string_field(record, "host"))
        .or_else(|| string_field(record, "url"));
    let matcher_name = string_field(record, "matcher-name");
    let result_type = string_field(record, "type");
    let locator = format!("line {line_number}");
    let mut metadata = Map::new();
    metadata.insert("templateId".to_string(), Value::String(template_id.clone()));
    if let Some(matcher_name) = &matcher_name {
        metadata.insert(
            "matcherName".to_string(),
            Value::String(matcher_name.clone()),
        );
    }
    if let Some(result_type) = &result_type {
        metadata.insert("type".to_string(), Value::String(result_type.clone()));
    }
    let now = rfc3339_now();
    let mut tags = sorted_strings(
        ["nuclei", template_id.as_str()]
            .into_iter()
            .map(str::to_string),
    );
    tags.extend(string_array_or_csv_field(info, "tags"));
    tags.sort();
    tags.dedup();

    NormalizedEvidenceFinding {
        id: deterministic_evidence_id(&[
            "nuclei",
            template_id.as_str(),
            asset.as_deref().unwrap_or(""),
            matcher_name.as_deref().unwrap_or(""),
        ]),
        title: title.clone(),
        severity: scanner_severity(string_field(info, "severity").as_deref()),
        confidence: "tool-observed".to_string(),
        status: "open".to_string(),
        description: string_field(info, "description"),
        remediation: string_field(info, "remediation"),
        asset: asset.clone(),
        weakness_ids: cwe_ids(&string_array_or_csv_field(info, "classification")),
        references: sorted_strings(string_array_or_csv_field(info, "reference").into_iter()),
        tags,
        evidence: vec![EvidenceSnippet {
            kind: "nuclei-result".to_string(),
            value: if let Some(asset) = &asset {
                format!("{title} on {asset}")
            } else {
                title.clone()
            },
            redacted: false,
            locator: locator.clone(),
        }],
        source_references: vec![EvidenceSourceReference {
            tool: "nuclei".to_string(),
            input_sha256: input_sha256.to_string(),
            raw_path: raw_path.to_string(),
            locator: locator.clone(),
            metadata: metadata.clone(),
        }],
        affected_instances: vec![EvidenceAffectedInstance {
            asset: asset.unwrap_or_else(|| "unknown".to_string()),
            metadata: metadata.clone(),
        }],
        first_seen: now.clone(),
        last_seen: now,
        provenance: {
            let mut provenance = metadata;
            provenance.insert("tool".to_string(), Value::String("nuclei".to_string()));
            provenance
        },
    }
}

fn string_field(record: &Map<String, Value>, field: &str) -> Option<String> {
    record
        .get(field)
        .and_then(Value::as_str)
        .filter(|value| !value.is_empty())
        .map(str::to_string)
}

fn string_array_or_csv_field(record: &Map<String, Value>, field: &str) -> Vec<String> {
    match record.get(field) {
        Some(Value::Array(values)) => values
            .iter()
            .flat_map(|value| match value {
                Value::String(text) => split_list(text),
                Value::Number(number) => split_list(&number.to_string()),
                _ => Vec::new(),
            })
            .collect(),
        Some(Value::String(text)) => split_list(text),
        Some(Value::Number(number)) => split_list(&number.to_string()),
        Some(Value::Object(map)) => map
            .values()
            .flat_map(|value| match value {
                Value::String(text) => split_list(text),
                Value::Number(number) => split_list(&number.to_string()),
                _ => Vec::new(),
            })
            .collect(),
        _ => Vec::new(),
    }
}

fn split_list(value: &str) -> Vec<String> {
    value
        .split([',', ';', '\n'])
        .map(str::trim)
        .filter(|entry| !entry.is_empty())
        .map(str::to_string)
        .collect()
}

fn cwe_ids(values: &[String]) -> Vec<String> {
    let mut ids = BTreeSet::new();
    for value in values {
        for token in value.split(|ch: char| !ch.is_ascii_alphanumeric()) {
            let Some(id) = token.strip_prefix("CWE") else {
                continue;
            };
            let id = id.trim_start_matches(['-', '_']);
            if !id.is_empty() && id.bytes().all(|byte| byte.is_ascii_digit()) {
                ids.insert(format!("CWE-{id}"));
            }
        }
    }
    ids.into_iter().collect()
}

fn scanner_severity(value: Option<&str>) -> String {
    let normalized = value.unwrap_or_default().to_ascii_lowercase();
    if normalized.contains("critical") || normalized == "4" {
        "critical".to_string()
    } else if normalized.contains("high") || normalized == "3" {
        "high".to_string()
    } else if normalized.contains("medium") || normalized.contains("moderate") || normalized == "2"
    {
        "medium".to_string()
    } else if normalized.contains("low") || normalized == "1" {
        "low".to_string()
    } else {
        "info".to_string()
    }
}

fn sorted_strings(values: impl Iterator<Item = String>) -> Vec<String> {
    let mut values: Vec<_> = values.filter(|value| !value.is_empty()).collect();
    values.sort();
    values.dedup();
    values
}

fn deterministic_evidence_id(parts: &[&str]) -> String {
    let joined = parts
        .iter()
        .map(|part| part.trim().to_ascii_lowercase())
        .collect::<Vec<_>>()
        .join("\u{1f}");
    format!(
        "evidence:{}",
        &encode_hex(Sha256::digest(joined.as_bytes()))[..24]
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

fn rfc3339_now() -> String {
    let seconds = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|duration| duration.as_secs())
        .unwrap_or(0);
    let day = seconds / 86_400;
    let seconds_of_day = seconds % 86_400;
    let (year, month, day_of_month) = civil_from_unix_day(day as i64);
    let hour = seconds_of_day / 3600;
    let minute = (seconds_of_day % 3600) / 60;
    let second = seconds_of_day % 60;
    format!("{year:04}-{month:02}-{day_of_month:02}T{hour:02}:{minute:02}:{second:02}Z")
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

impl Display for EvidenceNormalizeError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            EvidenceNormalizeError::Io(error) => write!(formatter, "{error}"),
            EvidenceNormalizeError::Json { line, source } => {
                write!(formatter, "line {line}: {source}")
            }
            EvidenceNormalizeError::InvalidRecord { line, reason } => {
                write!(formatter, "line {line}: {reason}")
            }
            EvidenceNormalizeError::Empty => write!(formatter, "nuclei JSONL contained no records"),
        }
    }
}

impl std::error::Error for EvidenceNormalizeError {}

impl From<io::Error> for EvidenceNormalizeError {
    fn from(error: io::Error) -> Self {
        EvidenceNormalizeError::Io(error)
    }
}

impl From<serde_json::Error> for EvidenceNormalizeError {
    fn from(error: serde_json::Error) -> Self {
        EvidenceNormalizeError::Json {
            line: 0,
            source: error,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn normalizes_nuclei_jsonl_to_evidence_findings_document() {
        let line = json!({
            "template-id": "http-missing-security-headers",
            "matched-at": "https://app.example.test",
            "matcher-name": "header",
            "type": "http",
            "info": {
                "name": "Missing security header",
                "severity": "medium",
                "description": "The response is missing a security header.",
                "remediation": "Set the missing header.",
                "tags": "http,headers",
                "reference": ["https://example.test/header-hardening"]
            }
        });
        let document = normalize_nuclei_jsonl(&format!("{line}\n"), "nuclei.jsonl", "sha256:test")
            .expect("normalize nuclei");

        assert_eq!(document.schema_version, EVIDENCE_FINDINGS_SCHEMA_VERSION);
        assert_eq!(document.findings.len(), 1);
        let finding = &document.findings[0];
        assert_eq!(finding.title, "Missing security header");
        assert_eq!(finding.severity, "medium");
        assert_eq!(finding.asset.as_deref(), Some("https://app.example.test"));
        assert_eq!(finding.source_references[0].tool, "nuclei");
        assert!(finding.tags.contains(&"headers".to_string()));
    }
}
