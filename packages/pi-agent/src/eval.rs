use std::collections::BTreeSet;
use std::fmt::{Display, Formatter};
use std::fs;
use std::io;
use std::path::{Path, PathBuf};

use rusqlite::Connection;
use serde::{Deserialize, Serialize};

use crate::{
    answer_query, chunk_markdown, chunk_plain_text, default_chunking_config, ingest_source_chunks,
    SourceFileMetadata,
};

pub const GOLD_FIXTURE_DIR: &str = "fixtures/gold";
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
    expected_no_answer: bool,
}

impl GoldEvalReport {
    pub fn ok(&self) -> bool {
        self.failed == 0
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

pub fn default_gold_fixture_dir() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR")).join(GOLD_FIXTURE_DIR)
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

fn ingest_gold_corpus_file(
    connection: &mut Connection,
    fixture_dir: &Path,
    corpus: &GoldCorpusFile,
) -> Result<(), GoldEvalError> {
    let file_path = fixture_dir.join(&corpus.file);
    let content = fs::read_to_string(&file_path)?;
    let chunks = if corpus.path.ends_with(".md") {
        chunk_markdown(&corpus.path, &content, default_chunking_config())
    } else if corpus.path.ends_with(".txt") {
        chunk_plain_text(&corpus.path, &content, default_chunking_config())
    } else {
        return Err(GoldEvalError::UnsupportedCorpusFile(corpus.path.clone()));
    };
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
}
