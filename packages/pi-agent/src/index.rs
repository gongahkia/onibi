use crate::chunking::ContentChunk;
use rusqlite::{params, Connection, OptionalExtension};
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

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SourceFileMetadata {
    pub path: String,
    pub content_hash: String,
    pub mtime_unix_nanos: i64,
    pub size_bytes: i64,
    pub ingested_at: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum SourceIngestOutcome {
    Unchanged { chunk_count: usize },
    Replaced { chunk_count: usize },
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

        CREATE TABLE IF NOT EXISTS source_files (
          path TEXT PRIMARY KEY,
          content_hash TEXT NOT NULL,
          mtime_unix_nanos INTEGER NOT NULL CHECK (mtime_unix_nanos >= 0),
          size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
          chunk_count INTEGER NOT NULL CHECK (chunk_count >= 0),
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

pub fn ingest_source_chunks(
    connection: &mut Connection,
    metadata: &SourceFileMetadata,
    chunks: &[ContentChunk],
) -> rusqlite::Result<SourceIngestOutcome> {
    apply_index_schema(connection)?;
    let existing = connection
        .query_row(
            "SELECT content_hash, mtime_unix_nanos, size_bytes, chunk_count FROM source_files WHERE path = ?1",
            params![metadata.path.as_str()],
            |row| {
                Ok((
                    row.get::<_, String>(0)?,
                    row.get::<_, i64>(1)?,
                    row.get::<_, i64>(2)?,
                    row.get::<_, i64>(3)? as usize,
                ))
            },
        )
        .optional()?;

    if let Some((content_hash, mtime_unix_nanos, size_bytes, chunk_count)) = existing {
        if content_hash == metadata.content_hash
            && mtime_unix_nanos == metadata.mtime_unix_nanos
            && size_bytes == metadata.size_bytes
        {
            return Ok(SourceIngestOutcome::Unchanged { chunk_count });
        }
    }

    let transaction = connection.transaction()?;
    transaction.execute(
        "DELETE FROM chunks WHERE path = ?1",
        params![metadata.path.as_str()],
    )?;

    for chunk in chunks {
        transaction.execute(
            "INSERT INTO chunks (id, path, heading_path, start_byte, end_byte, content_hash, content, ingested_at) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8)",
            params![
                chunk.chunk_id.as_str(),
                chunk.path.as_str(),
                serde_json::to_string(&chunk.heading_path).expect("serialize heading path"),
                chunk.start_byte as i64,
                chunk.end_byte as i64,
                chunk.chunk_hash.as_str(),
                chunk.content.as_str(),
                metadata.ingested_at.as_str(),
            ],
        )?;
    }

    transaction.execute(
        "INSERT INTO source_files (path, content_hash, mtime_unix_nanos, size_bytes, chunk_count, ingested_at) VALUES (?1, ?2, ?3, ?4, ?5, ?6) ON CONFLICT(path) DO UPDATE SET content_hash = excluded.content_hash, mtime_unix_nanos = excluded.mtime_unix_nanos, size_bytes = excluded.size_bytes, chunk_count = excluded.chunk_count, ingested_at = excluded.ingested_at",
        params![
            metadata.path.as_str(),
            metadata.content_hash.as_str(),
            metadata.mtime_unix_nanos,
            metadata.size_bytes,
            chunks.len() as i64,
            metadata.ingested_at.as_str(),
        ],
    )?;
    transaction.execute("INSERT INTO chunks_fts(chunks_fts) VALUES('rebuild')", [])?;
    transaction.commit()?;

    Ok(SourceIngestOutcome::Replaced {
        chunk_count: chunks.len(),
    })
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
    use crate::chunking::{chunk_plain_text, ChunkingConfig};
    use rusqlite::params;
    use std::fs;
    use std::time::{SystemTime, UNIX_EPOCH};

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

    #[test]
    fn ingest_source_chunks_replaces_changed_file_and_keeps_stable_chunk_ids() {
        let mut connection = Connection::open_in_memory().expect("open sqlite");
        let root = temp_root("reingest");
        let source_path = root.join("guide.txt");
        let logical_path = "docs/guide.txt";
        let config = ChunkingConfig {
            target_tokens: 2,
            overlap_tokens: 0,
            heading_aware: false,
        };

        fs::write(&source_path, "alpha beta\n\ngamma delta\n").expect("write source");
        let first_text = fs::read_to_string(&source_path).expect("read source");
        let first_chunks = chunk_plain_text(logical_path, &first_text, config);
        let first_metadata = source_metadata(&source_path, logical_path, "2026-06-19T00:00:00Z");

        let first_outcome = ingest_source_chunks(&mut connection, &first_metadata, &first_chunks)
            .expect("ingest first");
        assert_eq!(
            first_outcome,
            SourceIngestOutcome::Replaced { chunk_count: 2 }
        );
        let first_ids = chunk_ids_for_path(&connection, logical_path);

        fs::write(&source_path, "alpha beta\n\ngamma epsilon\n").expect("edit source");
        let second_text = fs::read_to_string(&source_path).expect("read edited source");
        let second_chunks = chunk_plain_text(logical_path, &second_text, config);
        let second_metadata = source_metadata(&source_path, logical_path, "2026-06-19T00:00:01Z");

        let second_outcome =
            ingest_source_chunks(&mut connection, &second_metadata, &second_chunks)
                .expect("ingest edited");
        assert_eq!(
            second_outcome,
            SourceIngestOutcome::Replaced { chunk_count: 2 }
        );
        let second_ids = chunk_ids_for_path(&connection, logical_path);

        assert_eq!(first_ids.len(), 2);
        assert_eq!(second_ids.len(), 2);
        assert_eq!(first_ids[0], second_ids[0]);
        assert_ne!(first_ids[1], second_ids[1]);
        assert!(search_chunks(&connection, "delta", 5)
            .expect("search old term")
            .is_empty());
        assert_eq!(
            search_chunks(&connection, "epsilon", 5)
                .expect("search new term")
                .len(),
            1
        );

        let unchanged_outcome =
            ingest_source_chunks(&mut connection, &second_metadata, &second_chunks)
                .expect("ingest unchanged");
        assert_eq!(
            unchanged_outcome,
            SourceIngestOutcome::Unchanged { chunk_count: 2 }
        );
        fs::remove_dir_all(root).ok();
    }

    fn chunk_ids_for_path(connection: &Connection, path: &str) -> Vec<String> {
        let mut statement = connection
            .prepare("SELECT id FROM chunks WHERE path = ?1 ORDER BY start_byte")
            .expect("prepare chunk id query");
        statement
            .query_map(params![path], |row| row.get::<_, String>(0))
            .expect("query chunk ids")
            .collect::<Result<Vec<_>, _>>()
            .expect("collect chunk ids")
    }

    fn source_metadata(path: &Path, logical_path: &str, ingested_at: &str) -> SourceFileMetadata {
        let bytes = fs::read(path).expect("read source bytes");
        let metadata = fs::metadata(path).expect("stat source");
        let modified = metadata
            .modified()
            .expect("source modified time")
            .duration_since(UNIX_EPOCH)
            .expect("mtime after epoch");
        SourceFileMetadata {
            path: logical_path.to_string(),
            content_hash: blake3::hash(&bytes).to_hex().to_string(),
            mtime_unix_nanos: modified.as_nanos() as i64,
            size_bytes: metadata.len() as i64,
            ingested_at: ingested_at.to_string(),
        }
    }

    fn temp_root(name: &str) -> PathBuf {
        let nonce = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("time after epoch")
            .as_nanos();
        let root = std::env::temp_dir().join(format!(
            "kelp-pi-index-{name}-{}-{nonce}",
            std::process::id()
        ));
        fs::create_dir_all(&root).expect("create temp root");
        root
    }
}
