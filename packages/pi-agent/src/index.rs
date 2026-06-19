use rusqlite::Connection;

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
}
