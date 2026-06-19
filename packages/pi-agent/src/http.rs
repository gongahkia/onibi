use axum::body::Bytes;
use axum::extract::State;
use axum::http::StatusCode;
use axum::response::{IntoResponse, Response};
use axum::routing::post;
use axum::{Json, Router};
use rusqlite::Connection;
use serde::Deserialize;
use serde_json::json;
use std::net::SocketAddr;
use std::path::PathBuf;
use std::sync::Arc;

use crate::{answer_query, apply_index_schema, AskResponse};

pub const DEFAULT_ASK_BIND: &str = "127.0.0.1:8765";

#[derive(Clone, Debug)]
pub struct AskHttpState {
    pub db_path: PathBuf,
    pub top_k: usize,
    pub no_answer_threshold: f64,
}

#[derive(Debug, Deserialize)]
struct AskHttpRequest {
    q: String,
    top_k: Option<usize>,
    no_answer_threshold: Option<f64>,
}

impl AskHttpState {
    pub fn new(db_path: PathBuf, top_k: usize, no_answer_threshold: f64) -> Self {
        Self {
            db_path,
            top_k: top_k.max(1),
            no_answer_threshold,
        }
    }
}

pub fn ask_router(state: AskHttpState) -> Router {
    Router::new()
        .route("/ask", post(ask_handler))
        .with_state(Arc::new(state))
}

pub fn ask_bind_is_loopback(addr: &SocketAddr) -> bool {
    addr.ip().is_loopback()
}

async fn ask_handler(State(state): State<Arc<AskHttpState>>, body: Bytes) -> Response {
    let request: AskHttpRequest = match serde_json::from_slice(&body) {
        Ok(request) => request,
        Err(error) => {
            return json_error(StatusCode::BAD_REQUEST, format!("invalid JSON: {error}"));
        }
    };
    let query = request.q.trim();
    if query.is_empty() {
        return json_error(StatusCode::BAD_REQUEST, "q is required");
    }
    let top_k = request.top_k.unwrap_or(state.top_k).max(1);
    let no_answer_threshold = request
        .no_answer_threshold
        .unwrap_or(state.no_answer_threshold);
    if !no_answer_threshold.is_finite() || no_answer_threshold < 0.0 {
        return json_error(
            StatusCode::BAD_REQUEST,
            "no_answer_threshold must be finite and non-negative",
        );
    }

    let db_path = state.db_path.clone();
    let query = query.to_string();
    let response = tokio::task::spawn_blocking(move || -> rusqlite::Result<AskResponse> {
        let connection = Connection::open(db_path)?;
        apply_index_schema(&connection)?;
        answer_query(&connection, &query, top_k, no_answer_threshold)
    })
    .await;

    match response {
        Ok(Ok(response)) => Json(response).into_response(),
        Ok(Err(error)) => json_error(StatusCode::INTERNAL_SERVER_ERROR, format!("{error}")),
        Err(error) => json_error(StatusCode::INTERNAL_SERVER_ERROR, format!("{error}")),
    }
}

fn json_error(status: StatusCode, message: impl Into<String>) -> Response {
    (status, Json(json!({ "error": message.into() }))).into_response()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn default_ask_bind_is_loopback() {
        let addr: SocketAddr = DEFAULT_ASK_BIND.parse().expect("parse bind");
        assert!(ask_bind_is_loopback(&addr));
    }

    #[test]
    fn ask_http_state_clamps_top_k() {
        let state = AskHttpState::new(
            PathBuf::from("chunks.sqlite3"),
            0,
            crate::DEFAULT_NO_ANSWER_THRESHOLD,
        );
        assert_eq!(state.top_k, 1);
    }
}
