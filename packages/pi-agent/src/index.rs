use rusqlite::Connection;
use serde::Serialize;
use std::path::{Path, PathBuf};

pub const DEFAULT_NO_ANSWER_THRESHOLD: f64 = 0.000001;

#[derive(Debug, Clone, Serialize, PartialEq)]
pub struct AskResponse {
    pub query: String,
    pub top_k: usize,
    pub no_answer: Option<NoAnswer>,
    pub citations: Vec<Citation>,
    pub results: Vec<RetrievedChunk>,
}

#[derive(Debug, Clone, Serialize, PartialEq)]
pub struct NoAnswer {
    pub reason: String,
    pub threshold: f64,
    pub max_score: Option<f64>,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct Citation {
    pub path: String,
    pub heading_path: Vec<String>,
    pub chunk_id: String,
    pub start_byte: usize,
    pub end_byte: usize,
}

#[derive(Debug, Clone, Serialize, PartialEq)]
pub struct RetrievedChunk {
    pub chunk_id: String,
    pub path: String,
    pub heading_path: Vec<String>,
    pub start_byte: usize,
    pub end_byte: usize,
    pub score: f64,
    pub content: String,
}

pub fn index_db_path(data_dir: &Path) -> PathBuf {
    data_dir.join("index").join("chunks.sqlite3")
}

pub fn apply_index_schema(connection: &Connection) -> rusqlite::Result<()> {
    connection.execute_batch(
        r#"
        CREATE TABLE IF NOT EXISTS chunks (
          id TEXT PRIMARY KEY,
          path TEXT NOT NULL,
          heading_path TEXT NOT NULL,
          start_byte INTEGER NOT NULL CHECK (start_byte >= 0),
          end_byte INTEGER NOT NULL CHECK (end_byte >= start_byte),
          content_hash TEXT NOT NULL,
          content TEXT NOT NULL,
          ingested_at TEXT NOT NULL
        );

        CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(
          content,
          content='chunks',
          content_rowid='rowid'
        );
        "#,
    )
}

pub fn search_chunks(
    connection: &Connection,
    query: &str,
    top_k: usize,
) -> rusqlite::Result<Vec<RetrievedChunk>> {
    let mut statement = connection.prepare(
        r#"
        SELECT
          chunks.id,
          chunks.path,
          chunks.heading_path,
          chunks.start_byte,
          chunks.end_byte,
          -bm25(chunks_fts) AS score,
          chunks.content
        FROM chunks_fts
        JOIN chunks ON chunks_fts.rowid = chunks.rowid
        WHERE chunks_fts MATCH ?1
        ORDER BY score DESC
        LIMIT ?2
        "#,
    )?;
    let rows = statement.query_map((query, top_k.max(1) as i64), |row| {
        let heading_path_text: String = row.get(2)?;
        let heading_path = serde_json::from_str(&heading_path_text).unwrap_or_default();
        Ok(RetrievedChunk {
            chunk_id: row.get(0)?,
            path: row.get(1)?,
            heading_path,
            start_byte: row.get::<_, i64>(3)? as usize,
            end_byte: row.get::<_, i64>(4)? as usize,
            score: row.get(5)?,
            content: row.get(6)?,
        })
    })?;

    rows.collect()
}

pub fn citation_for_chunk(chunk: &RetrievedChunk) -> Citation {
    Citation {
        path: chunk.path.clone(),
        heading_path: chunk.heading_path.clone(),
        chunk_id: chunk.chunk_id.clone(),
        start_byte: chunk.start_byte,
        end_byte: chunk.end_byte,
    }
}

pub fn answer_query(
    connection: &Connection,
    query: &str,
    top_k: usize,
    no_answer_threshold: f64,
) -> rusqlite::Result<AskResponse> {
    let results = search_chunks(connection, query, top_k)?;
    let max_score = results
        .iter()
        .map(|result| result.score)
        .max_by(|left, right| left.total_cmp(right));
    let no_answer = match max_score {
        None => Some(NoAnswer {
            reason: "no matching chunks".to_string(),
            threshold: no_answer_threshold,
            max_score: None,
        }),
        Some(score) if score < no_answer_threshold => Some(NoAnswer {
            reason: "max score below threshold".to_string(),
            threshold: no_answer_threshold,
            max_score: Some(score),
        }),
        Some(_) => None,
    };

    let citations = if no_answer.is_some() {
        Vec::new()
    } else {
        results.iter().map(citation_for_chunk).collect()
    };

    Ok(AskResponse {
        query: query.to_string(),
        top_k: top_k.max(1),
        citations,
        results: if no_answer.is_some() {
            Vec::new()
        } else {
            results
        },
        no_answer,
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use rusqlite::params;

    #[test]
    fn index_schema_migration_is_idempotent_and_fts5_backed() {
        let connection = Connection::open_in_memory().expect("open sqlite");

        apply_index_schema(&connection).expect("apply schema once");
        apply_index_schema(&connection).expect("apply schema twice");

        connection
            .execute(
                "INSERT INTO chunks (id, path, heading_path, start_byte, end_byte, content_hash, content, ingested_at) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8)",
                params![
                    "chunk-1",
                    "docs/guide.md",
                    "[\"Guide\"]",
                    0_i64,
                    11_i64,
                    "hash-1",
                    "admin login",
                    "2026-06-19T00:00:00Z"
                ],
            )
            .expect("insert chunk");
        let rowid = connection.last_insert_rowid();
        connection
            .execute(
                "INSERT INTO chunks_fts (rowid, content) VALUES (?1, ?2)",
                params![rowid, "admin login"],
            )
            .expect("insert fts row");

        let matches: i64 = connection
            .query_row(
                "SELECT count(*) FROM chunks_fts WHERE chunks_fts MATCH 'admin'",
                [],
                |row| row.get(0),
            )
            .expect("query fts");
        assert_eq!(matches, 1);
    }

    #[test]
    fn search_chunks_returns_top_k_bm25_scores() {
        let connection = Connection::open_in_memory().expect("open sqlite");
        apply_index_schema(&connection).expect("apply schema");

        for (id, content) in [
            ("chunk-1", "admin login portal"),
            ("chunk-2", "admin password reset"),
            ("chunk-3", "billing export"),
        ] {
            connection
                .execute(
                    "INSERT INTO chunks (id, path, heading_path, start_byte, end_byte, content_hash, content, ingested_at) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8)",
                    params![
                        id,
                        "docs/guide.md",
                        "[\"Guide\"]",
                        0_i64,
                        content.len() as i64,
                        format!("hash-{id}"),
                        content,
                        "2026-06-19T00:00:00Z"
                    ],
                )
                .expect("insert chunk");
            let rowid = connection.last_insert_rowid();
            connection
                .execute(
                    "INSERT INTO chunks_fts (rowid, content) VALUES (?1, ?2)",
                    params![rowid, content],
                )
                .expect("insert fts row");
        }

        let results = search_chunks(&connection, "admin", 2).expect("search chunks");

        assert_eq!(results.len(), 2);
        assert!(results.iter().all(|result| result.score.is_finite()));
        assert!(results
            .iter()
            .all(|result| result.content.contains("admin")));
    }

    #[test]
    fn answer_query_returns_no_answer_for_unrelated_query() {
        let connection = Connection::open_in_memory().expect("open sqlite");
        apply_index_schema(&connection).expect("apply schema");
        connection
            .execute(
                "INSERT INTO chunks (id, path, heading_path, start_byte, end_byte, content_hash, content, ingested_at) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8)",
                params![
                    "chunk-1",
                    "docs/guide.md",
                    "[]",
                    0_i64,
                    18_i64,
                    "hash-1",
                    "admin login portal",
                    "2026-06-19T00:00:00Z"
                ],
            )
            .expect("insert chunk");
        let rowid = connection.last_insert_rowid();
        connection
            .execute(
                "INSERT INTO chunks_fts (rowid, content) VALUES (?1, ?2)",
                params![rowid, "admin login portal"],
            )
            .expect("insert fts row");

        let response = answer_query(&connection, "unrelated", 5, DEFAULT_NO_ANSWER_THRESHOLD)
            .expect("answer query");

        assert!(response.results.is_empty());
        assert_eq!(
            response.no_answer.expect("no answer").reason,
            "no matching chunks"
        );
        assert!(response.citations.is_empty());
    }

    #[test]
    fn answer_query_returns_top_level_citations_for_results() {
        let connection = Connection::open_in_memory().expect("open sqlite");
        apply_index_schema(&connection).expect("apply schema");
        connection
            .execute(
                "INSERT INTO chunks (id, path, heading_path, start_byte, end_byte, content_hash, content, ingested_at) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8)",
                params![
                    "chunk-1",
                    "docs/guide.md",
                    "[\"Guide\", \"Login\"]",
                    7_i64,
                    25_i64,
                    "hash-1",
                    "admin login portal",
                    "2026-06-19T00:00:00Z"
                ],
            )
            .expect("insert chunk");
        let rowid = connection.last_insert_rowid();
        connection
            .execute(
                "INSERT INTO chunks_fts (rowid, content) VALUES (?1, ?2)",
                params![rowid, "admin login portal"],
            )
            .expect("insert fts row");

        let response = answer_query(&connection, "admin", 5, DEFAULT_NO_ANSWER_THRESHOLD)
            .expect("answer query");

        assert!(response.no_answer.is_none());
        assert_eq!(
            response.citations,
            vec![Citation {
                path: "docs/guide.md".to_string(),
                heading_path: vec!["Guide".to_string(), "Login".to_string()],
                chunk_id: "chunk-1".to_string(),
                start_byte: 7,
                end_byte: 25,
            }]
        );
    }
}
