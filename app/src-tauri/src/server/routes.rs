use super::{pairing, AppState};
use crate::{
    adapters,
    approval::store::now_millis,
    protocol::{
        ApiError, Approval, ApprovalDecisionBody, ApprovalDecisionResponse, ApprovalRequestBody,
        Decision, PairRequest, PairResponse, PtyOutputBody, RunEventBody, ServerMessage,
        PROTOCOL_VERSION,
    },
    transport::{lan, TransportSnapshot},
};
use anyhow::Result;
use axum::{
    body::Body,
    extract::{Path, State},
    http::{header, HeaderValue, StatusCode},
    response::{IntoResponse, Response},
    Json,
};
use serde_json::{json, Value};
use tokio::time;
use ulid::Ulid;

type ApiResult<T> = Result<Json<T>, (StatusCode, Json<ApiError>)>;

pub async fn healthz() -> Json<Value> {
    Json(json!({"ok": true, "protocol_version": PROTOCOL_VERSION}))
}

pub async fn approval_request(
    State(state): State<AppState>,
    Json(body): Json<ApprovalRequestBody>,
) -> ApiResult<ApprovalDecisionResponse> {
    validate_version(body.protocol_version.as_deref())?;
    wait_for_approval_decision(&state, body)
        .await
        .map(Json)
        .map_err(internal_error)
}

pub async fn approval_pending(State(state): State<AppState>) -> ApiResult<Vec<Approval>> {
    state.store.list_pending().map(Json).map_err(internal_error)
}

pub async fn approval_decide(
    State(state): State<AppState>,
    Path(approval_id): Path<String>,
    Json(mut body): Json<ApprovalDecisionBody>,
) -> ApiResult<ApprovalDecisionResponse> {
    if body.by.is_none() {
        body.by = Some("desktop".to_string());
    }
    let decided = state
        .store
        .decide(&approval_id, &body)
        .map_err(internal_error)?;
    if !decided {
        return Err((
            StatusCode::CONFLICT,
            Json(ApiError::new("approval already decided or not found")),
        ));
    }

    let response = ApprovalDecisionResponse {
        protocol_version: PROTOCOL_VERSION.to_string(),
        approval_id: approval_id.clone(),
        decision: body.decision,
        updated_input: body.updated_input.clone(),
        reason: body.reason.clone(),
    };
    let _ = state.pending.resolve(&approval_id, response.clone()).await;

    let machine_id = state
        .store
        .get_approval(&approval_id)
        .ok()
        .flatten()
        .map(|approval| approval.machine_id)
        .unwrap_or_else(|| state.machine_id.clone());
    state.broadcast(ServerMessage::ApprovalResolved {
        protocol_version: PROTOCOL_VERSION.to_string(),
        approval_id,
        machine_id,
        decision: body.decision,
        by: body.by,
        reason: body.reason,
    });

    Ok(Json(response))
}

pub async fn run_event(
    State(state): State<AppState>,
    Json(body): Json<RunEventBody>,
) -> ApiResult<Value> {
    validate_version(body.protocol_version.as_deref())?;
    let machine_id = body.machine_id.unwrap_or_else(|| state.machine_id.clone());
    state
        .store
        .insert_run_event(&machine_id, &body.session_id, &body.kind, &body.payload)
        .map_err(internal_error)?;
    state.broadcast(ServerMessage::RunEvent {
        protocol_version: PROTOCOL_VERSION.to_string(),
        machine_id,
        session_id: body.session_id,
        kind: body.kind,
        payload: body.payload,
    });
    Ok(Json(
        json!({"ok": true, "protocol_version": PROTOCOL_VERSION}),
    ))
}

pub async fn pty_output(
    State(state): State<AppState>,
    Json(body): Json<PtyOutputBody>,
) -> ApiResult<Value> {
    validate_version(body.protocol_version.as_deref())?;
    let machine_id = body.machine_id.unwrap_or_else(|| state.machine_id.clone());
    state.append_pty_output(&body.session_id, &body.data).await;
    state.broadcast(ServerMessage::PtyOutput {
        protocol_version: PROTOCOL_VERSION.to_string(),
        machine_id,
        session_id: body.session_id,
        data: body.data,
    });
    Ok(Json(
        json!({"ok": true, "protocol_version": PROTOCOL_VERSION}),
    ))
}

pub async fn pair(
    State(state): State<AppState>,
    Json(body): Json<PairRequest>,
) -> ApiResult<PairResponse> {
    let device_id = Ulid::new().to_string();
    state
        .store
        .insert_device(
            &device_id,
            &body.device_label,
            body.push_subscription.as_ref(),
        )
        .map_err(internal_error)?;
    Ok(Json(PairResponse {
        protocol_version: PROTOCOL_VERSION.to_string(),
        device_id,
        machine_id: state.machine_id,
    }))
}

pub async fn qr(State(state): State<AppState>) -> Result<Response, (StatusCode, Json<ApiError>)> {
    let payload = state.transports.pairing_payload().await;
    let payload = serde_json::to_string(&payload)
        .map_err(|error| internal_error(anyhow::Error::new(error)))?;
    let png = pairing::qr_png(&payload).map_err(internal_error)?;
    let mut response = Body::from(png).into_response();
    response
        .headers_mut()
        .insert(header::CONTENT_TYPE, HeaderValue::from_static("image/png"));
    Ok(response)
}

pub async fn transport_status(State(state): State<AppState>) -> ApiResult<Vec<TransportSnapshot>> {
    Ok(Json(state.transports.status_snapshot().await))
}

pub async fn transport_enable(
    State(state): State<AppState>,
    Path(name): Path<String>,
) -> ApiResult<TransportSnapshot> {
    state
        .transports
        .enable(&name)
        .await
        .map(Json)
        .map_err(internal_error)
}

pub async fn transport_disable(
    State(state): State<AppState>,
    Path(name): Path<String>,
) -> ApiResult<Value> {
    state
        .transports
        .disable(&name)
        .await
        .map_err(internal_error)?;
    Ok(Json(json!({"ok": true, "transport": name})))
}

pub async fn lan_cert() -> Result<Response, (StatusCode, Json<ApiError>)> {
    let pem = lan::read_cert_pem().map_err(internal_error)?;
    let mut response = Body::from(pem).into_response();
    response.headers_mut().insert(
        header::CONTENT_TYPE,
        HeaderValue::from_static("application/x-pem-file"),
    );
    Ok(response)
}

pub async fn lan_cert_qr() -> Result<Response, (StatusCode, Json<ApiError>)> {
    let pem = lan::read_cert_pem().map_err(internal_error)?;
    let png = pairing::qr_png(&pem).map_err(internal_error)?;
    let mut response = Body::from(png).into_response();
    response
        .headers_mut()
        .insert(header::CONTENT_TYPE, HeaderValue::from_static("image/png"));
    Ok(response)
}

pub async fn claude_code_hook(
    State(state): State<AppState>,
    Json(payload): Json<Value>,
) -> Result<Json<Value>, (StatusCode, Json<ApiError>)> {
    adapters::claude_code::handle_http_hook(&state, payload)
        .await
        .map(Json)
        .map_err(internal_error)
}

pub async fn codex_hook(
    State(state): State<AppState>,
    Json(payload): Json<Value>,
) -> Result<Json<Value>, (StatusCode, Json<ApiError>)> {
    adapters::codex::handle_http_hook(&state, payload)
        .await
        .map(Json)
        .map_err(internal_error)
}

pub async fn wait_for_approval_decision(
    state: &AppState,
    body: ApprovalRequestBody,
) -> Result<ApprovalDecisionResponse> {
    let approval_id = Ulid::new().to_string();
    let approval = Approval {
        protocol_version: PROTOCOL_VERSION.to_string(),
        approval_id: approval_id.clone(),
        machine_id: body.machine_id.unwrap_or_else(|| state.machine_id.clone()),
        session_id: body.session_id.unwrap_or_else(|| Ulid::new().to_string()),
        agent: body.agent,
        tool: body.tool,
        input: body.input,
        cwd: body.cwd,
        metadata: body.metadata,
        decision: None,
        updated_input: None,
        reason: None,
        decided_by: None,
        created_at: now_millis(),
        decided_at: None,
    };

    state.store.insert_approval(&approval)?;
    let rx = state.pending.insert(approval_id.clone()).await;
    state.broadcast(ServerMessage::from(&approval));

    match time::timeout(state.approval_timeout, rx).await {
        Ok(Ok(response)) => Ok(response),
        Ok(Err(_)) => Ok(ApprovalDecisionResponse {
            protocol_version: PROTOCOL_VERSION.to_string(),
            approval_id,
            decision: Decision::Deny,
            updated_input: None,
            reason: Some("approval waiter closed".to_string()),
        }),
        Err(_) => {
            state.pending.remove(&approval_id).await;
            let body = ApprovalDecisionBody {
                decision: Decision::Deny,
                updated_input: None,
                reason: Some("timeout".to_string()),
                by: Some("system".to_string()),
            };
            let _ = state.store.decide(&approval_id, &body)?;
            let response = ApprovalDecisionResponse::denied_timeout(approval_id.clone());
            state.broadcast(ServerMessage::ApprovalResolved {
                protocol_version: PROTOCOL_VERSION.to_string(),
                approval_id,
                machine_id: state.machine_id.clone(),
                decision: Decision::Deny,
                by: Some("system".to_string()),
                reason: Some("timeout".to_string()),
            });
            Ok(response)
        }
    }
}

fn validate_version(version: Option<&str>) -> Result<(), (StatusCode, Json<ApiError>)> {
    if version.is_some_and(|version| version != PROTOCOL_VERSION) {
        Err((
            StatusCode::UPGRADE_REQUIRED,
            Json(ApiError::new("protocol_version mismatch")),
        ))
    } else {
        Ok(())
    }
}

fn internal_error(error: anyhow::Error) -> (StatusCode, Json<ApiError>) {
    (
        StatusCode::INTERNAL_SERVER_ERROR,
        Json(ApiError::new(error.to_string())),
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{approval::store::ApprovalStore, server::router};
    use axum::{
        body::{to_bytes, Body},
        http::{Request, StatusCode},
    };
    use serde_json::json;
    use tempfile::tempdir;
    use tower::ServiceExt;

    #[tokio::test]
    async fn approval_round_trip_blocks_until_decision() {
        let dir = tempdir().unwrap();
        let store = ApprovalStore::open(dir.path().join("onibi.db")).unwrap();
        let state = AppState::for_tests(store);
        let app = router(state.clone());

        let request = Request::builder()
            .method("POST")
            .uri("/v1/approval/request")
            .header("authorization", "Bearer test-token")
            .header("content-type", "application/json")
            .body(Body::from(
                json!({
                    "protocol_version": "1.0",
                    "agent": "claude-code",
                    "tool": "Bash",
                    "input": {"command": "rm -rf node_modules"},
                    "cwd": "/tmp/project"
                })
                .to_string(),
            ))
            .unwrap();

        let pending_app = app.clone();
        let response_task =
            tokio::spawn(async move { pending_app.oneshot(request).await.unwrap() });
        tokio::time::sleep(std::time::Duration::from_millis(50)).await;

        let pending_response = app
            .clone()
            .oneshot(
                Request::builder()
                    .method("GET")
                    .uri("/v1/approval/pending")
                    .header("authorization", "Bearer test-token")
                    .body(Body::empty())
                    .unwrap(),
            )
            .await
            .unwrap();
        assert_eq!(pending_response.status(), StatusCode::OK);
        let bytes = to_bytes(pending_response.into_body(), usize::MAX)
            .await
            .unwrap();
        let pending: Vec<Approval> = serde_json::from_slice(&bytes).unwrap();
        assert_eq!(pending.len(), 1);
        let id = pending[0].approval_id.clone();

        let decide_response = app
            .oneshot(
                Request::builder()
                    .method("POST")
                    .uri(format!("/v1/approval/{id}/decide"))
                    .header("authorization", "Bearer test-token")
                    .header("content-type", "application/json")
                    .body(Body::from(
                        json!({
                            "decision": "allow",
                            "updatedInput": {"command": "echo skipped"}
                        })
                        .to_string(),
                    ))
                    .unwrap(),
            )
            .await
            .unwrap();
        assert_eq!(decide_response.status(), StatusCode::OK);

        let response = response_task.await.unwrap();
        assert_eq!(response.status(), StatusCode::OK);
        let bytes = to_bytes(response.into_body(), usize::MAX).await.unwrap();
        let decision: ApprovalDecisionResponse = serde_json::from_slice(&bytes).unwrap();
        assert_eq!(decision.decision, Decision::Allow);
        assert_eq!(
            decision.updated_input,
            Some(json!({"command": "echo skipped"}))
        );
    }
}
