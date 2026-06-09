use super::AppState;
use crate::protocol::ClientScope;
use axum::{
    body::Body,
    extract::State,
    http::{header, Method, Request, StatusCode},
    middleware::Next,
    response::Response,
};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AuthScope {
    pub scope: ClientScope,
    pub token: String,
}

pub async fn require_bearer(
    State(state): State<AppState>,
    mut req: Request<Body>,
    next: Next,
) -> Result<Response, StatusCode> {
    if let Some(auth) = authorized_scope(&state, &req) {
        if auth.scope == ClientScope::ReadOnly && !read_only_route(req.method(), req.uri().path()) {
            return Err(StatusCode::FORBIDDEN);
        }
        req.extensions_mut().insert(auth);
        Ok(next.run(req).await)
    } else {
        Err(StatusCode::UNAUTHORIZED)
    }
}

fn authorized_scope(state: &AppState, req: &Request<Body>) -> Option<AuthScope> {
    if let Some(value) = req.headers().get(header::AUTHORIZATION) {
        if let Ok(value) = value.to_str() {
            if let Some(token) = value.strip_prefix("Bearer ") {
                return token_scope(state, token);
            }
        }
    }
    req.uri()
        .query()
        .and_then(|query| {
            query.split('&').find_map(|part| {
                let (key, value) = part.split_once('=')?;
                (key == "token").then_some(value)
            })
        })
        .and_then(|token| token_scope(state, token))
}

fn token_scope(state: &AppState, token: &str) -> Option<AuthScope> {
    if token == state.token() {
        return Some(AuthScope {
            scope: ClientScope::Full,
            token: token.to_string(),
        });
    }
    state
        .store
        .spectator_token_exists(token)
        .ok()
        .and_then(|exists| {
            exists.then(|| AuthScope {
                scope: ClientScope::ReadOnly,
                token: token.to_string(),
            })
        })
}

fn read_only_route(method: &Method, path: &str) -> bool {
    matches!(
        (method, path),
        (&Method::POST, "/v1/pair")
            | (&Method::GET, "/v1/approval/pending")
            | (&Method::GET, "/v1/approval/history")
            | (&Method::GET, "/v1/approval/history/export")
            | (&Method::GET, "/v1/run/recent")
            | (&Method::GET, "/v1/status")
            | (&Method::GET, "/v1/transport/status")
            | (&Method::GET, "/v1/realtime")
    )
}
