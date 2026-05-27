use super::AppState;
use axum::{
    body::Body,
    extract::State,
    http::{header, Request, StatusCode},
    middleware::Next,
    response::Response,
};

pub async fn require_bearer(
    State(state): State<AppState>,
    req: Request<Body>,
    next: Next,
) -> Result<Response, StatusCode> {
    if authorized(&state, &req) {
        Ok(next.run(req).await)
    } else {
        Err(StatusCode::UNAUTHORIZED)
    }
}

fn authorized(state: &AppState, req: &Request<Body>) -> bool {
    if let Some(value) = req.headers().get(header::AUTHORIZATION) {
        if let Ok(value) = value.to_str() {
            return value == format!("Bearer {}", state.token);
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
        .is_some_and(|token| token == state.token)
}
