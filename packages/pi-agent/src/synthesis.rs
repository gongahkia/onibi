use std::collections::BTreeSet;
use std::fmt::{Display, Formatter};

use serde::Serialize;

use crate::AskResponse;

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct SynthesisAnswer {
    pub text: String,
    pub citation_chunk_ids: Vec<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum SynthesisError {
    NoAnswer,
    CitationFreeSentence { sentence: String },
    UnknownCitation { chunk_id: String },
}

pub fn synthesize_with_citation_guard<F>(
    retrieval: &AskResponse,
    mut generate: F,
) -> Result<SynthesisAnswer, SynthesisError>
where
    F: FnMut(&AskResponse) -> String,
{
    if retrieval.no_answer.is_some() {
        return Err(SynthesisError::NoAnswer);
    }
    let allowed: BTreeSet<String> = retrieval
        .citations
        .iter()
        .map(|citation| citation.chunk_id.clone())
        .collect();
    let text = generate(retrieval);
    validate_generated_citations(&text, &allowed)?;
    let citation_chunk_ids = allowed
        .into_iter()
        .filter(|chunk_id| text.contains(&format!("[{chunk_id}]")))
        .collect();
    Ok(SynthesisAnswer {
        text,
        citation_chunk_ids,
    })
}

fn validate_generated_citations(
    text: &str,
    allowed: &BTreeSet<String>,
) -> Result<(), SynthesisError> {
    for sentence in generated_sentences(text) {
        let citations = bracket_citations(sentence);
        if citations.is_empty() {
            return Err(SynthesisError::CitationFreeSentence {
                sentence: sentence.to_string(),
            });
        }
        for chunk_id in citations {
            if !allowed.contains(&chunk_id) {
                return Err(SynthesisError::UnknownCitation { chunk_id });
            }
        }
    }
    Ok(())
}

fn generated_sentences(text: &str) -> Vec<&str> {
    let mut sentences = Vec::new();
    let mut start = 0;
    for (index, character) in text.char_indices() {
        if matches!(character, '.' | '!' | '?') {
            let next = text[index + character.len_utf8()..].chars().next();
            if next.is_none_or(char::is_whitespace) {
                let sentence = text[start..index].trim();
                if !sentence.is_empty() {
                    sentences.push(sentence);
                }
                start = index + character.len_utf8();
            }
        }
    }
    let sentence = text[start..].trim();
    if !sentence.is_empty() {
        sentences.push(sentence);
    }
    sentences
}

fn bracket_citations(sentence: &str) -> Vec<String> {
    let mut citations = Vec::new();
    let mut rest = sentence;
    while let Some(start) = rest.find('[') {
        let after_start = &rest[start + 1..];
        let Some(end) = after_start.find(']') else {
            break;
        };
        let chunk_id = after_start[..end].trim();
        if !chunk_id.is_empty() {
            citations.push(chunk_id.to_string());
        }
        rest = &after_start[end + 1..];
    }
    citations
}

impl Display for SynthesisError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            SynthesisError::NoAnswer => write!(formatter, "retrieval returned no-answer"),
            SynthesisError::CitationFreeSentence { sentence } => {
                write!(formatter, "generated sentence has no citation: {sentence}")
            }
            SynthesisError::UnknownCitation { chunk_id } => {
                write!(
                    formatter,
                    "generated sentence cites unknown chunk: {chunk_id}"
                )
            }
        }
    }
}

impl std::error::Error for SynthesisError {}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{AskResponse, Citation, NoAnswer, RetrievedChunk};

    #[test]
    fn citation_free_generation_is_rejected() {
        let retrieval = cited_retrieval();

        let error =
            synthesize_with_citation_guard(&retrieval, |_| "Admin login is exposed.".to_string())
                .expect_err("citation-free output rejected");

        assert!(matches!(error, SynthesisError::CitationFreeSentence { .. }));
    }

    #[test]
    fn every_generated_sentence_must_cite_retrieved_chunk() {
        let retrieval = cited_retrieval();

        let answer = synthesize_with_citation_guard(&retrieval, |_| {
            "Admin login is exposed [chunk-a]. Rotate the password [chunk-a].".to_string()
        })
        .expect("cited output accepted");

        assert_eq!(answer.citation_chunk_ids, vec!["chunk-a"]);
    }

    #[test]
    fn unknown_citations_are_rejected() {
        let retrieval = cited_retrieval();

        let error = synthesize_with_citation_guard(&retrieval, |_| {
            "Admin login is exposed [missing].".to_string()
        })
        .expect_err("unknown citation rejected");

        assert!(matches!(
            error,
            SynthesisError::UnknownCitation { chunk_id } if chunk_id == "missing"
        ));
    }

    #[test]
    fn no_answer_skips_generation() {
        let mut retrieval = cited_retrieval();
        retrieval.no_answer = Some(NoAnswer {
            reason: "no matching chunks".to_string(),
            threshold: 1.0,
            max_score: None,
        });
        retrieval.citations.clear();
        retrieval.results.clear();
        let mut called = false;

        let error = synthesize_with_citation_guard(&retrieval, |_| {
            called = true;
            "should not run [chunk-a].".to_string()
        })
        .expect_err("no-answer rejected");

        assert!(!called);
        assert_eq!(error, SynthesisError::NoAnswer);
    }

    fn cited_retrieval() -> AskResponse {
        AskResponse {
            query: "admin login".to_string(),
            top_k: 1,
            no_answer: None,
            citations: vec![Citation {
                path: "guide.md".to_string(),
                heading_path: vec!["Findings".to_string()],
                chunk_id: "chunk-a".to_string(),
                start_byte: 0,
                end_byte: 12,
            }],
            results: vec![RetrievedChunk {
                chunk_id: "chunk-a".to_string(),
                path: "guide.md".to_string(),
                heading_path: vec!["Findings".to_string()],
                start_byte: 0,
                end_byte: 12,
                score: 1.0,
                content: "Admin login".to_string(),
            }],
        }
    }
}
