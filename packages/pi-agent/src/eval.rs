use std::collections::BTreeSet;
use std::fmt::{Display, Formatter};
use std::fs;
use std::io;
use std::path::{Path, PathBuf};

use rusqlite::Connection;
use serde::{Deserialize, Serialize};
use serde_json::Value;

use crate::{
    answer_query, chunk_markdown, chunk_plain_text, default_chunking_config, ingest_source_chunks,
    normalize_nuclei_jsonl_file, synthesize_with_citation_guard, ContentChunk,
    EvidenceFindingsDocument, EvidenceNormalizeError, SourceFileMetadata, SynthesisError,
    PINNED_NMAP_RUNTIME_VERSION, PINNED_NUCLEI_TEMPLATES_REVISION,
};

pub const GOLD_FIXTURE_DIR: &str = "fixtures/gold";
pub const SCANNER_STABILITY_FIXTURE_DIR: &str = "fixtures/scanner-stability";
pub const DEFAULT_GOLD_TOP_K: usize = 8;

#[derive(Debug, Serialize, PartialEq, Eq)]
pub struct GoldEvalReport {
    pub fixture_dir: String,
    pub cases: usize,
    pub passed: usize,
    pub failed: usize,
    pub results: Vec<GoldEvalCaseResult>,
}

#[derive(Debug, Serialize, PartialEq, Eq)]
pub struct GoldEvalCaseResult {
    pub id: String,
    pub status: GoldEvalStatus,
    pub question: String,
    pub retrieval_query: String,
    pub expected_chunk_ids: Vec<String>,
    pub citation_chunk_ids: Vec<String>,
    pub missing_chunk_ids: Vec<String>,
    pub expected_no_answer: bool,
    pub got_no_answer: bool,
}

#[derive(Debug, Serialize, PartialEq, Eq)]
pub struct SynthesisEvalReport {
    pub fixture_dir: String,
    pub cases: usize,
    pub passed: usize,
    pub failed: usize,
    pub results: Vec<SynthesisEvalCaseResult>,
}

#[derive(Debug, Serialize, PartialEq, Eq)]
pub struct ScannerStabilityReport {
    pub fixture_dir: String,
    pub target: String,
    pub nmap_runtime_version: String,
    pub nuclei_templates_revision: String,
    pub findings: usize,
    pub passed: bool,
    pub failures: Vec<String>,
    pub fingerprint: Vec<String>,
}

#[derive(Debug, Serialize, PartialEq, Eq)]
pub struct SynthesisEvalCaseResult {
    pub id: String,
    pub status: GoldEvalStatus,
    pub question: String,
    pub expected_no_answer: bool,
    pub got_no_answer: bool,
    pub generated_text: Option<String>,
    pub citation_chunk_ids: Vec<String>,
    pub missing_chunk_ids: Vec<String>,
    pub missing_answer_terms: Vec<String>,
    pub error: Option<String>,
}

#[derive(Debug, Serialize, PartialEq, Eq)]
#[serde(rename_all = "kebab-case")]
pub enum GoldEvalStatus {
    Pass,
    Fail,
}

#[derive(Debug)]
pub enum GoldEvalError {
    Io(io::Error),
    Json(serde_json::Error),
    Sqlite(rusqlite::Error),
    UnsupportedCorpusFile(String),
}

#[derive(Debug)]
pub enum ScannerStabilityError {
    Io(io::Error),
    Json(serde_json::Error),
    Normalize(EvidenceNormalizeError),
}

#[derive(Debug, Deserialize)]
struct GoldQaSet {
    corpus: Vec<GoldCorpusFile>,
    cases: Vec<GoldQaCase>,
}

#[derive(Debug, Deserialize)]
struct GoldCorpusFile {
    path: String,
    file: String,
}

#[derive(Debug, Deserialize)]
struct GoldQaCase {
    id: String,
    question: String,
    expected_chunk_ids: Vec<String>,
    #[serde(default)]
    expected_answer_contains: Vec<String>,
    #[serde(default)]
    expected_no_answer: bool,
}

#[derive(Debug, Deserialize)]
struct ScannerStabilityCase {
    schema_version: u32,
    target: String,
    nmap_runtime_version: String,
    nuclei_templates_revision: String,
    nuclei_input: String,
    raw_path: String,
    expected_fingerprint: String,
}

impl GoldEvalReport {
    pub fn ok(&self) -> bool {
        self.failed == 0
    }
}

impl SynthesisEvalReport {
    pub fn ok(&self) -> bool {
        self.failed == 0
    }
}

impl ScannerStabilityReport {
    pub fn ok(&self) -> bool {
        self.passed
    }
}

impl Display for GoldEvalError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            GoldEvalError::Io(error) => write!(formatter, "{error}"),
            GoldEvalError::Json(error) => write!(formatter, "{error}"),
            GoldEvalError::Sqlite(error) => write!(formatter, "{error}"),
            GoldEvalError::UnsupportedCorpusFile(path) => {
                write!(formatter, "unsupported gold corpus file: {path}")
            }
        }
    }
}

impl std::error::Error for GoldEvalError {}

impl Display for ScannerStabilityError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            ScannerStabilityError::Io(error) => write!(formatter, "{error}"),
            ScannerStabilityError::Json(error) => write!(formatter, "{error}"),
            ScannerStabilityError::Normalize(error) => write!(formatter, "{error}"),
        }
    }
}

impl std::error::Error for ScannerStabilityError {}

impl From<io::Error> for GoldEvalError {
    fn from(error: io::Error) -> Self {
        GoldEvalError::Io(error)
    }
}

impl From<serde_json::Error> for GoldEvalError {
    fn from(error: serde_json::Error) -> Self {
        GoldEvalError::Json(error)
    }
}

impl From<rusqlite::Error> for GoldEvalError {
    fn from(error: rusqlite::Error) -> Self {
        GoldEvalError::Sqlite(error)
    }
}

impl From<io::Error> for ScannerStabilityError {
    fn from(error: io::Error) -> Self {
        ScannerStabilityError::Io(error)
    }
}

impl From<serde_json::Error> for ScannerStabilityError {
    fn from(error: serde_json::Error) -> Self {
        ScannerStabilityError::Json(error)
    }
}

impl From<EvidenceNormalizeError> for ScannerStabilityError {
    fn from(error: EvidenceNormalizeError) -> Self {
        ScannerStabilityError::Normalize(error)
    }
}

pub fn default_gold_fixture_dir() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR")).join(GOLD_FIXTURE_DIR)
}

pub fn default_scanner_stability_fixture_dir() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR")).join(SCANNER_STABILITY_FIXTURE_DIR)
}

pub fn run_gold_eval(
    fixture_dir: &Path,
    top_k: usize,
    no_answer_threshold: f64,
) -> Result<GoldEvalReport, GoldEvalError> {
    let gold: GoldQaSet = serde_json::from_slice(&fs::read(fixture_dir.join("gold-qa.json"))?)?;
    let mut connection = Connection::open_in_memory()?;

    for corpus in &gold.corpus {
        ingest_gold_corpus_file(&mut connection, fixture_dir, corpus)?;
    }

    let mut results = Vec::with_capacity(gold.cases.len());
    for case in gold.cases {
        let retrieval_query = retrieval_query_for_question(&case.question);
        let response = answer_query(
            &connection,
            &retrieval_query,
            top_k.max(1),
            no_answer_threshold,
        )?;
        let citation_chunk_ids: Vec<String> = response
            .citations
            .iter()
            .map(|citation| citation.chunk_id.clone())
            .collect();
        let citation_set: BTreeSet<&str> = citation_chunk_ids
            .iter()
            .map(|chunk_id| chunk_id.as_str())
            .collect();
        let missing_chunk_ids: Vec<String> = case
            .expected_chunk_ids
            .iter()
            .filter(|chunk_id| !citation_set.contains(chunk_id.as_str()))
            .cloned()
            .collect();
        let got_no_answer = response.no_answer.is_some();
        let passed = if case.expected_no_answer {
            got_no_answer && citation_chunk_ids.is_empty()
        } else {
            !got_no_answer && missing_chunk_ids.is_empty()
        };

        results.push(GoldEvalCaseResult {
            id: case.id,
            status: if passed {
                GoldEvalStatus::Pass
            } else {
                GoldEvalStatus::Fail
            },
            question: case.question,
            retrieval_query,
            expected_chunk_ids: case.expected_chunk_ids,
            citation_chunk_ids,
            missing_chunk_ids,
            expected_no_answer: case.expected_no_answer,
            got_no_answer,
        });
    }

    let passed = results
        .iter()
        .filter(|result| result.status == GoldEvalStatus::Pass)
        .count();
    let failed = results.len() - passed;
    Ok(GoldEvalReport {
        fixture_dir: fixture_dir.display().to_string(),
        cases: results.len(),
        passed,
        failed,
        results,
    })
}

pub fn run_scanner_stability_eval(
    fixture_dir: &Path,
) -> Result<ScannerStabilityReport, ScannerStabilityError> {
    let case: ScannerStabilityCase =
        serde_json::from_slice(&fs::read(fixture_dir.join("case.json"))?)?;
    let first = normalize_nuclei_jsonl_file(&fixture_dir.join(&case.nuclei_input), &case.raw_path)?;
    let second =
        normalize_nuclei_jsonl_file(&fixture_dir.join(&case.nuclei_input), &case.raw_path)?;
    let first_value = canonical_scanner_stability_value(&first)?;
    let second_value = canonical_scanner_stability_value(&second)?;
    let fingerprint = structural_fingerprint(&first_value);
    let expected_fingerprint: Vec<String> =
        serde_json::from_slice(&fs::read(fixture_dir.join(&case.expected_fingerprint))?)?;
    let mut failures = Vec::new();

    if case.schema_version != 1 {
        failures.push(format!(
            "case schema_version mismatch: expected 1, found {}",
            case.schema_version
        ));
    }
    if case.nmap_runtime_version != PINNED_NMAP_RUNTIME_VERSION {
        failures.push(format!(
            "nmap runtime mismatch: expected {}, found {}",
            PINNED_NMAP_RUNTIME_VERSION, case.nmap_runtime_version
        ));
    }
    if case.nuclei_templates_revision != PINNED_NUCLEI_TEMPLATES_REVISION {
        failures.push(format!(
            "nuclei templates mismatch: expected {}, found {}",
            PINNED_NUCLEI_TEMPLATES_REVISION, case.nuclei_templates_revision
        ));
    }
    if first_value != second_value {
        failures.push(
            "normalized evidence changed across identical scanner inputs after timing normalization"
                .to_string(),
        );
    }
    if fingerprint != expected_fingerprint {
        failures.extend(fingerprint_diff(&fingerprint, &expected_fingerprint));
    }

    Ok(ScannerStabilityReport {
        fixture_dir: fixture_dir.display().to_string(),
        target: case.target,
        nmap_runtime_version: case.nmap_runtime_version,
        nuclei_templates_revision: case.nuclei_templates_revision,
        findings: first.findings.len(),
        passed: failures.is_empty(),
        failures,
        fingerprint,
    })
}

pub fn run_synthesis_eval(
    fixture_dir: &Path,
    top_k: usize,
    no_answer_threshold: f64,
) -> Result<SynthesisEvalReport, GoldEvalError> {
    let gold: GoldQaSet = serde_json::from_slice(&fs::read(fixture_dir.join("gold-qa.json"))?)?;
    let mut connection = Connection::open_in_memory()?;

    for corpus in &gold.corpus {
        ingest_gold_corpus_file(&mut connection, fixture_dir, corpus)?;
    }

    let mut results = Vec::with_capacity(gold.cases.len());
    for case in gold.cases {
        let retrieval_query = retrieval_query_for_question(&case.question);
        let response = answer_query(
            &connection,
            &retrieval_query,
            top_k.max(1),
            no_answer_threshold,
        )?;
        let got_no_answer = response.no_answer.is_some();
        let citation_chunk_ids: Vec<String> = response
            .citations
            .iter()
            .map(|citation| citation.chunk_id.clone())
            .collect();
        let citation_set: BTreeSet<&str> = citation_chunk_ids
            .iter()
            .map(|chunk_id| chunk_id.as_str())
            .collect();
        let missing_chunk_ids: Vec<String> = case
            .expected_chunk_ids
            .iter()
            .filter(|chunk_id| !citation_set.contains(chunk_id.as_str()))
            .cloned()
            .collect();
        let generated_seed = if case.expected_answer_contains.is_empty() {
            "cited answer".to_string()
        } else {
            case.expected_answer_contains.join("; ")
        };
        let citation_id = case
            .expected_chunk_ids
            .iter()
            .find(|chunk_id| citation_set.contains(chunk_id.as_str()))
            .or_else(|| citation_chunk_ids.first())
            .cloned()
            .unwrap_or_default();
        let synthesis = synthesize_with_citation_guard(&response, |_| {
            format!("{generated_seed} [{citation_id}].")
        });
        let (generated_text, missing_answer_terms, error) = match synthesis {
            Ok(answer) => {
                let missing_terms = case
                    .expected_answer_contains
                    .iter()
                    .filter(|term| !answer.text.contains(term.as_str()))
                    .cloned()
                    .collect();
                (Some(answer.text), missing_terms, None)
            }
            Err(error) => (None, case.expected_answer_contains.clone(), Some(error)),
        };
        let passed = if case.expected_no_answer {
            got_no_answer && matches!(error, Some(SynthesisError::NoAnswer))
        } else {
            !got_no_answer
                && missing_chunk_ids.is_empty()
                && missing_answer_terms.is_empty()
                && error.is_none()
        };

        results.push(SynthesisEvalCaseResult {
            id: case.id,
            status: if passed {
                GoldEvalStatus::Pass
            } else {
                GoldEvalStatus::Fail
            },
            question: case.question,
            expected_no_answer: case.expected_no_answer,
            got_no_answer,
            generated_text,
            citation_chunk_ids,
            missing_chunk_ids,
            missing_answer_terms,
            error: error.map(|error| error.to_string()),
        });
    }

    let passed = results
        .iter()
        .filter(|result| result.status == GoldEvalStatus::Pass)
        .count();
    let failed = results.len() - passed;
    Ok(SynthesisEvalReport {
        fixture_dir: fixture_dir.display().to_string(),
        cases: results.len(),
        passed,
        failed,
        results,
    })
}

fn canonical_scanner_stability_value(
    document: &EvidenceFindingsDocument,
) -> Result<Value, serde_json::Error> {
    let mut value = serde_json::to_value(document)?;
    normalize_timing_fields(&mut value);
    Ok(value)
}

fn normalize_timing_fields(value: &mut Value) {
    match value {
        Value::Array(values) => {
            for value in values {
                normalize_timing_fields(value);
            }
        }
        Value::Object(map) => {
            for (key, value) in map {
                if matches!(key.as_str(), "firstSeen" | "lastSeen") {
                    *value = Value::String("<timing>".to_string());
                } else {
                    normalize_timing_fields(value);
                }
            }
        }
        _ => {}
    }
}

fn structural_fingerprint(value: &Value) -> Vec<String> {
    let mut lines = Vec::new();
    collect_structural_fingerprint("$", value, &mut lines);
    lines.sort();
    lines.dedup();
    lines
}

fn collect_structural_fingerprint(path: &str, value: &Value, lines: &mut Vec<String>) {
    match value {
        Value::Null => lines.push(format!("{path}:null")),
        Value::Bool(_) => lines.push(format!("{path}:bool")),
        Value::Number(_) => lines.push(format!("{path}:number")),
        Value::String(_) => lines.push(format!("{path}:string")),
        Value::Array(values) => {
            lines.push(format!("{path}:array[{}]", values.len()));
            for (index, value) in values.iter().enumerate() {
                collect_structural_fingerprint(&format!("{path}[{index}]"), value, lines);
            }
        }
        Value::Object(map) => {
            let mut keys = map.keys().cloned().collect::<Vec<_>>();
            keys.sort();
            lines.push(format!("{path}:object{{{}}}", keys.join(",")));
            for key in keys {
                let child = map.get(&key).expect("key collected from map");
                collect_structural_fingerprint(
                    &format!("{path}{}", path_segment(&key)),
                    child,
                    lines,
                );
            }
        }
    }
}

fn path_segment(key: &str) -> String {
    if key
        .bytes()
        .all(|byte| byte.is_ascii_alphanumeric() || byte == b'_')
    {
        format!(".{key}")
    } else {
        format!(
            "[{}]",
            serde_json::to_string(key).expect("serialize JSON path segment")
        )
    }
}

fn fingerprint_diff(actual: &[String], expected: &[String]) -> Vec<String> {
    let actual_set = actual.iter().map(String::as_str).collect::<BTreeSet<_>>();
    let expected_set = expected.iter().map(String::as_str).collect::<BTreeSet<_>>();
    let missing = expected_set
        .difference(&actual_set)
        .copied()
        .collect::<Vec<_>>();
    let unexpected = actual_set
        .difference(&expected_set)
        .copied()
        .collect::<Vec<_>>();
    let mut failures = Vec::new();
    if !missing.is_empty() {
        failures.push(format!("fingerprint missing paths: {}", missing.join(", ")));
    }
    if !unexpected.is_empty() {
        failures.push(format!(
            "fingerprint unexpected paths: {}",
            unexpected.join(", ")
        ));
    }
    failures
}

pub fn gold_chunk_id_lines(fixture_dir: &Path) -> Result<Vec<String>, GoldEvalError> {
    let gold: GoldQaSet = serde_json::from_slice(&fs::read(fixture_dir.join("gold-qa.json"))?)?;
    let mut lines = Vec::new();
    for corpus in &gold.corpus {
        for chunk in gold_corpus_chunks(fixture_dir, corpus)? {
            lines.push(format!(
                "{}\t{}\t{}",
                chunk.chunk_id,
                chunk.path,
                serde_json::to_string(&chunk.heading_path)?
            ));
        }
    }
    lines.sort();
    Ok(lines)
}

fn ingest_gold_corpus_file(
    connection: &mut Connection,
    fixture_dir: &Path,
    corpus: &GoldCorpusFile,
) -> Result<(), GoldEvalError> {
    let content = fs::read_to_string(fixture_dir.join(&corpus.file))?;
    let chunks = gold_chunks_for_content(corpus, &content)?;
    let metadata = SourceFileMetadata {
        path: corpus.path.clone(),
        content_hash: blake3::hash(content.as_bytes()).to_hex().to_string(),
        mtime_unix_nanos: 0,
        size_bytes: content.len() as i64,
        ingested_at: "1970-01-01T00:00:00Z".to_string(),
    };
    ingest_source_chunks(connection, &metadata, &chunks)?;
    Ok(())
}

fn gold_corpus_chunks(
    fixture_dir: &Path,
    corpus: &GoldCorpusFile,
) -> Result<Vec<ContentChunk>, GoldEvalError> {
    let content = fs::read_to_string(fixture_dir.join(&corpus.file))?;
    gold_chunks_for_content(corpus, &content)
}

fn gold_chunks_for_content(
    corpus: &GoldCorpusFile,
    content: &str,
) -> Result<Vec<ContentChunk>, GoldEvalError> {
    if corpus.path.ends_with(".md") {
        Ok(chunk_markdown(
            &corpus.path,
            content,
            default_chunking_config(),
        ))
    } else if corpus.path.ends_with(".txt") {
        Ok(chunk_plain_text(
            &corpus.path,
            content,
            default_chunking_config(),
        ))
    } else {
        Err(GoldEvalError::UnsupportedCorpusFile(corpus.path.clone()))
    }
}

fn retrieval_query_for_question(question: &str) -> String {
    let stopwords = [
        "about", "after", "does", "what", "when", "where", "which", "while", "with", "from",
        "that", "this", "into", "is", "are", "the", "for", "and", "its", "use", "uses",
    ];
    let mut terms = BTreeSet::new();
    for raw in question.split(|character: char| !character.is_ascii_alphanumeric()) {
        let term = raw.to_ascii_lowercase();
        if term.len() > 2 && !stopwords.contains(&term.as_str()) {
            terms.insert(term);
        }
    }
    if terms.is_empty() {
        question.to_string()
    } else {
        terms.into_iter().collect::<Vec<_>>().join(" OR ")
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::DEFAULT_NO_ANSWER_THRESHOLD;

    #[test]
    fn bundled_gold_eval_passes() {
        let report = run_gold_eval(
            &default_gold_fixture_dir(),
            DEFAULT_GOLD_TOP_K,
            DEFAULT_NO_ANSWER_THRESHOLD,
        )
        .expect("gold eval");

        assert!(report.ok());
        assert_eq!(report.cases, 34);
        assert_eq!(report.failed, 0);
    }

    #[test]
    fn bundled_gold_chunk_ids_match_manifest() {
        let expected = include_str!("../fixtures/gold/chunk-ids.txt")
            .lines()
            .map(str::to_string)
            .collect::<Vec<_>>();
        let actual = gold_chunk_id_lines(&default_gold_fixture_dir()).expect("chunk id lines");

        assert_eq!(actual, expected);
    }

    #[test]
    fn bundled_synthesis_eval_passes() {
        let report = run_synthesis_eval(
            &default_gold_fixture_dir(),
            DEFAULT_GOLD_TOP_K,
            DEFAULT_NO_ANSWER_THRESHOLD,
        )
        .expect("synthesis eval");

        assert!(report.ok());
        assert_eq!(report.cases, 34);
        assert_eq!(report.failed, 0);
    }

    #[test]
    fn bundled_scanner_stability_eval_passes() {
        let report = run_scanner_stability_eval(&default_scanner_stability_fixture_dir())
            .expect("scanner stability eval");

        assert!(
            report.ok(),
            "{}",
            serde_json::to_string_pretty(&report).expect("serialize report")
        );
        assert_eq!(report.findings, 2);
    }
}
