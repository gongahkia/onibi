use axum::body::Bytes;
use axum::extract::{ConnectInfo, State};
use axum::http::StatusCode;
use axum::response::{IntoResponse, Response};
use axum::routing::post;
use axum::{Json, Router};
use rusqlite::Connection;
use serde::Deserialize;
use serde_json::json;
use std::collections::{HashMap, VecDeque};
use std::net::{IpAddr, SocketAddr};
use std::path::PathBuf;
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use crate::{answer_query, apply_index_schema, AskResponse};

pub const DEFAULT_ASK_BIND: &str = "127.0.0.1:8765";
pub const DEFAULT_ASK_MAX_CONCURRENT: usize = 8;
pub const DEFAULT_ASK_RATE_LIMIT_PER_MINUTE: usize = 60;

#[derive(Clone, Debug)]
pub struct AskHttpState {
    pub db_path: PathBuf,
    pub top_k: usize,
    pub no_answer_threshold: f64,
    pub max_concurrent: usize,
    pub rate_limit_per_minute: usize,
    in_flight: Arc<Mutex<usize>>,
    rate_windows: Arc<Mutex<HashMap<IpAddr, VecDeque<Instant>>>>,
}

#[derive(Debug, Deserialize)]
struct AskHttpRequest {
    q: String,
    top_k: Option<usize>,
    no_answer_threshold: Option<f64>,
}

impl AskHttpState {
    pub fn new(db_path: PathBuf, top_k: usize, no_answer_threshold: f64) -> Self {
        Self::with_limits(
            db_path,
            top_k,
            no_answer_threshold,
            DEFAULT_ASK_MAX_CONCURRENT,
            DEFAULT_ASK_RATE_LIMIT_PER_MINUTE,
        )
    }

    pub fn with_limits(
        db_path: PathBuf,
        top_k: usize,
        no_answer_threshold: f64,
        max_concurrent: usize,
        rate_limit_per_minute: usize,
    ) -> Self {
        Self {
            db_path,
            top_k: top_k.max(1),
            no_answer_threshold,
            max_concurrent: max_concurrent.max(1),
            rate_limit_per_minute: rate_limit_per_minute.max(1),
            in_flight: Arc::new(Mutex::new(0)),
            rate_windows: Arc::new(Mutex::new(HashMap::new())),
        }
    }

    fn try_acquire_concurrency(&self) -> Option<AskConcurrencyPermit> {
        let mut in_flight = self
            .in_flight
            .lock()
            .expect("ask concurrency lock poisoned");
        if *in_flight >= self.max_concurrent {
            return None;
        }
        *in_flight += 1;
        Some(AskConcurrencyPermit {
            in_flight: Arc::clone(&self.in_flight),
        })
    }

    fn accept_rate(&self, ip: IpAddr, now: Instant) -> bool {
        let mut windows = self.rate_windows.lock().expect("ask rate lock poisoned");
        let window = windows.entry(ip).or_default();
        let cutoff = now.checked_sub(Duration::from_secs(60)).unwrap_or(now);
        while window.front().is_some_and(|seen| *seen <= cutoff) {
            window.pop_front();
        }
        if window.len() >= self.rate_limit_per_minute {
            return false;
        }
        window.push_back(now);
        true
    }
}

struct AskConcurrencyPermit {
    in_flight: Arc<Mutex<usize>>,
}

impl Drop for AskConcurrencyPermit {
    fn drop(&mut self) {
        let mut in_flight = self
            .in_flight
            .lock()
            .expect("ask concurrency lock poisoned");
        *in_flight = in_flight.saturating_sub(1);
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

async fn ask_handler(
    ConnectInfo(remote_addr): ConnectInfo<SocketAddr>,
    State(state): State<Arc<AskHttpState>>,
    body: Bytes,
) -> Response {
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
    if !state.accept_rate(remote_addr.ip(), Instant::now()) {
        return json_error(StatusCode::TOO_MANY_REQUESTS, "rate limit exceeded");
    }
    let Some(_permit) = state.try_acquire_concurrency() else {
        return json_error(StatusCode::TOO_MANY_REQUESTS, "concurrency limit exceeded");
    };

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

    #[test]
    fn ask_http_state_enforces_rate_limit_per_ip() {
        let state = AskHttpState::with_limits(
            PathBuf::from("chunks.sqlite3"),
            5,
            crate::DEFAULT_NO_ANSWER_THRESHOLD,
            8,
            1,
        );
        let ip = "127.0.0.1".parse::<IpAddr>().expect("parse ip");
        let now = Instant::now();

        assert!(state.accept_rate(ip, now));
        assert!(!state.accept_rate(ip, now));
        assert!(state.accept_rate(ip, now + Duration::from_secs(61)));
    }

    #[test]
    fn ask_http_state_enforces_concurrency_cap() {
        let state = AskHttpState::with_limits(
            PathBuf::from("chunks.sqlite3"),
            5,
            crate::DEFAULT_NO_ANSWER_THRESHOLD,
            1,
            60,
        );

        let permit = state.try_acquire_concurrency().expect("first permit");
        assert!(state.try_acquire_concurrency().is_none());
        drop(permit);
        assert!(state.try_acquire_concurrency().is_some());
    }
}
