use super::{auth, pairing, AppState};
use crate::{
    adapters,
    approval::store::{now_millis, ApprovalHistoryFilter},
    checkpointing,
    orchestration::AgentStatus,
    policy,
    protocol::{
        ApiError, Approval, ApprovalDecisionBody, ApprovalDecisionResponse, ApprovalRequestBody,
        CheckpointDiff, CheckpointRecord, CheckpointRestoreBody, ClientScope, Decision,
        DesktopCommandBlock, DesktopCommandResponse, DesktopPaneSplitBody, DesktopRemoteSshBody,
        DesktopSessionInputBody, DesktopSessionLaunchBody, DesktopSnapshotBody,
        DesktopWorktreeOpenBody, PairRequest, PairResponse, PtyOutputBody, RunEvent,
        RunEventBody, ServerMessage, PROTOCOL_VERSION,
    },
    pty::TrustMode,
    push, secret,
    transport::{lan, TransportSnapshot},
};
use anyhow::Result;
use async_trait::async_trait;
use axum::{
    body::Body,
    extract::{rejection::JsonRejection, Extension, FromRequest, Path, Query, Request, State},
    http::{header, HeaderValue, StatusCode},
    response::{IntoResponse, Response},
    Json,
};
use serde::Deserialize;
use serde_json::{json, Value};
use tokio::time;
use ulid::Ulid;

type ApiResult<T> = Result<Json<T>, (StatusCode, Json<ApiError>)>;

pub struct ApiJson<T>(pub T);

#[async_trait]
impl<S, T> FromRequest<S> for ApiJson<T>
where
    Json<T>: FromRequest<S, Rejection = JsonRejection>,
    S: Send + Sync,
{
    type Rejection = (StatusCode, Json<ApiError>);

    async fn from_request(req: Request, state: &S) -> Result<Self, Self::Rejection> {
        Json::<T>::from_request(req, state)
            .await
            .map(|Json(value)| Self(value))
            .map_err(|rejection| {
                (
                    StatusCode::BAD_REQUEST,
                    Json(ApiError::new(rejection.body_text())),
                )
            })
    }
}

#[derive(Debug, Deserialize)]
pub struct CommandBlockQuery {
    #[serde(default, rename = "sessionId")]
    session_id: Option<String>,
    #[serde(default)]
    limit: Option<usize>,
}

#[derive(Debug, Deserialize)]
pub struct ApprovalHistoryQuery {
    #[serde(default)]
    agent: Option<String>,
    #[serde(default)]
    tool: Option<String>,
    #[serde(default)]
    decision: Option<Decision>,
    #[serde(default)]
    from: Option<i64>,
    #[serde(default)]
    to: Option<i64>,
    #[serde(default)]
    limit: Option<usize>,
}

#[derive(Debug, Deserialize)]
pub struct CheckpointListQuery {
    #[serde(default)]
    limit: Option<usize>,
}

pub async fn healthz() -> Json<Value> {
    Json(json!({"ok": true, "protocol_version": PROTOCOL_VERSION}))
}

pub async fn approval_request(
    State(state): State<AppState>,
    ApiJson(body): ApiJson<ApprovalRequestBody>,
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

pub async fn approval_history(
    State(state): State<AppState>,
    Query(query): Query<ApprovalHistoryQuery>,
) -> ApiResult<Vec<Approval>> {
    state
        .store
        .list_approvals(query.into_filter())
        .map(Json)
        .map_err(internal_error)
}

pub async fn approval_history_export(
    State(state): State<AppState>,
    Query(query): Query<ApprovalHistoryQuery>,
) -> Result<Response, (StatusCode, Json<ApiError>)> {
    let approvals = state
        .store
        .list_approvals(query.into_filter())
        .map_err(internal_error)?;
    let mut body = String::new();
    for approval in approvals {
        let line = serde_json::to_string(&approval)
            .map_err(|error| internal_error(anyhow::Error::new(error)))?;
        body.push_str(&line);
        body.push('\n');
    }
    let mut response = Body::from(body).into_response();
    response.headers_mut().insert(
        header::CONTENT_TYPE,
        HeaderValue::from_static("application/x-ndjson; charset=utf-8"),
    );
    Ok(response)
}

pub async fn checkpoints_list(
    State(state): State<AppState>,
    Query(query): Query<CheckpointListQuery>,
) -> ApiResult<Vec<CheckpointRecord>> {
    state
        .store
        .list_checkpoints(query.limit.unwrap_or(500))
        .map(Json)
        .map_err(internal_error)
}

pub async fn checkpoint_diff(
    State(state): State<AppState>,
    Path(approval_id): Path<String>,
) -> ApiResult<CheckpointDiff> {
    let checkpoint = state
        .store
        .get_checkpoint(&approval_id)
        .map_err(internal_error)?
        .ok_or_else(|| not_found("checkpoint not found"))?;
    checkpointing::diff(&checkpoint)
        .map(Json)
        .map_err(internal_error)
}

pub async fn checkpoint_restore(
    State(state): State<AppState>,
    Path(approval_id): Path<String>,
    ApiJson(body): ApiJson<CheckpointRestoreBody>,
) -> ApiResult<Value> {
    validate_version(body.protocol_version.as_deref())?;
    let checkpoint = state
        .store
        .get_checkpoint(&approval_id)
        .map_err(internal_error)?
        .ok_or_else(|| not_found("checkpoint not found"))?;
    checkpointing::restore(&checkpoint).map_err(internal_error)?;
    Ok(Json(json!({
        "ok": true,
        "protocol_version": PROTOCOL_VERSION,
        "approvalId": approval_id,
    })))
}

pub async fn approval_decide(
    State(state): State<AppState>,
    Path(approval_id): Path<String>,
    ApiJson(mut body): ApiJson<ApprovalDecisionBody>,
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
    if let Ok(Some(approval)) = state.store.get_approval(&approval_id) {
        state
            .orchestration
            .set_status(&approval.session_id, AgentStatus::Working)
            .await;
    }
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

pub async fn emergency_stop(State(state): State<AppState>) -> ApiResult<Value> {
    let stopped_sessions = state.orchestration.stop_all_running_sessions().await;
    let pending = state.store.list_pending().map_err(internal_error)?;
    let mut denied_approvals = 0usize;
    for approval in pending {
        let body = ApprovalDecisionBody {
            decision: Decision::Deny,
            updated_input: None,
            reason: Some("stopped by mobile kill switch".to_string()),
            by: Some("kill-switch".to_string()),
        };
        let decided = state
            .store
            .decide(&approval.approval_id, &body)
            .map_err(internal_error)?;
        if !decided {
            continue;
        }
        denied_approvals += 1;
        let response = ApprovalDecisionResponse {
            protocol_version: PROTOCOL_VERSION.to_string(),
            approval_id: approval.approval_id.clone(),
            decision: Decision::Deny,
            updated_input: None,
            reason: body.reason.clone(),
        };
        let _ = state.pending.resolve(&approval.approval_id, response).await;
        state.broadcast(ServerMessage::ApprovalResolved {
            protocol_version: PROTOCOL_VERSION.to_string(),
            approval_id: approval.approval_id,
            machine_id: approval.machine_id,
            decision: Decision::Deny,
            by: body.by,
            reason: body.reason,
        });
    }
    Ok(Json(json!({
        "ok": true,
        "stoppedSessions": stopped_sessions,
        "deniedApprovals": denied_approvals,
    })))
}

pub async fn run_event(
    State(state): State<AppState>,
    ApiJson(body): ApiJson<RunEventBody>,
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

pub async fn run_recent(State(state): State<AppState>) -> ApiResult<Vec<RunEvent>> {
    state
        .store
        .list_recent_run_events(50)
        .map(Json)
        .map_err(internal_error)
}

pub async fn pty_output(
    State(state): State<AppState>,
    ApiJson(body): ApiJson<PtyOutputBody>,
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

pub async fn desktop_state(
    State(state): State<AppState>,
    ApiJson(mut body): ApiJson<DesktopSnapshotBody>,
) -> ApiResult<Value> {
    validate_version(body.protocol_version.as_deref())?;
    body.protocol_version = Some(PROTOCOL_VERSION.to_string());
    state.set_desktop_snapshot(body).await;
    Ok(Json(
        json!({"ok": true, "protocol_version": PROTOCOL_VERSION}),
    ))
}

pub async fn desktop_sessions(State(state): State<AppState>) -> ApiResult<Value> {
    let snapshot = state.desktop_snapshot().await;
    Ok(Json(json!({
        "protocol_version": PROTOCOL_VERSION,
        "sessions": snapshot.sessions,
        "arrangements": snapshot.arrangements,
        "updatedAt": snapshot.updated_at,
    })))
}

pub async fn desktop_attention(State(state): State<AppState>) -> ApiResult<Value> {
    let snapshot = state.desktop_snapshot().await;
    let sessions = snapshot
        .sessions
        .into_iter()
        .filter(|session| {
            matches!(
                session.attention.as_str(),
                "needs-approval" | "triggered" | "failed" | "exited"
            )
        })
        .collect::<Vec<_>>();
    Ok(Json(json!({
        "protocol_version": PROTOCOL_VERSION,
        "attentionCount": sessions.len(),
        "sessions": sessions,
    })))
}

pub async fn desktop_command_block(
    State(state): State<AppState>,
    ApiJson(mut block): ApiJson<DesktopCommandBlock>,
) -> ApiResult<Value> {
    validate_version(block.protocol_version.as_deref())?;
    block.protocol_version = Some(PROTOCOL_VERSION.to_string());
    state
        .store
        .upsert_command_block(&block)
        .map_err(internal_error)?;
    Ok(Json(json!({
        "ok": true,
        "protocol_version": PROTOCOL_VERSION,
        "id": block.id,
    })))
}

pub async fn desktop_command_blocks(
    State(state): State<AppState>,
    Query(query): Query<CommandBlockQuery>,
) -> ApiResult<Vec<DesktopCommandBlock>> {
    state
        .store
        .list_command_blocks(query.session_id.as_deref(), query.limit.unwrap_or(100))
        .map(Json)
        .map_err(internal_error)
}

pub async fn desktop_session_launch(
    State(state): State<AppState>,
    ApiJson(body): ApiJson<DesktopSessionLaunchBody>,
) -> ApiResult<DesktopCommandResponse> {
    validate_version(body.protocol_version.as_deref())?;
    Ok(Json(broadcast_desktop_command(
        &state,
        "session-launch",
        json!({
            "agent": body.agent,
            "workspace": body.workspace,
            "prompt": body.prompt,
            "cwd": body.cwd,
            "worktreeStrategy": body.worktree_strategy,
        }),
    )))
}

pub async fn desktop_remote_ssh(
    State(state): State<AppState>,
    ApiJson(body): ApiJson<DesktopRemoteSshBody>,
) -> ApiResult<DesktopCommandResponse> {
    validate_version(body.protocol_version.as_deref())?;
    let keybindings = match body.keybindings.as_deref() {
        Some("remote") => "remote",
        _ => "local",
    };
    Ok(Json(broadcast_desktop_command(
        &state,
        "remote-ssh-launch",
        json!({
            "target": body.target,
            "workspace": body.workspace,
            "cwd": body.cwd,
            "name": body.name,
            "keybindings": keybindings,
        }),
    )))
}

pub async fn desktop_session_input(
    State(state): State<AppState>,
    Path(session_id): Path<String>,
    ApiJson(body): ApiJson<DesktopSessionInputBody>,
) -> ApiResult<DesktopCommandResponse> {
    validate_version(body.protocol_version.as_deref())?;
    Ok(Json(broadcast_desktop_command(
        &state,
        "session-input",
        json!({
            "sessionId": session_id,
            "text": body.text,
        }),
    )))
}

pub async fn desktop_worktree_open(
    State(state): State<AppState>,
    ApiJson(body): ApiJson<DesktopWorktreeOpenBody>,
) -> ApiResult<DesktopCommandResponse> {
    validate_version(body.protocol_version.as_deref())?;
    Ok(Json(broadcast_desktop_command(
        &state,
        "worktree-open",
        json!({
            "path": body.path,
            "agent": body.agent,
            "prompt": body.prompt,
        }),
    )))
}

pub async fn desktop_session_focus(
    State(state): State<AppState>,
    Path(session_id): Path<String>,
) -> ApiResult<DesktopCommandResponse> {
    Ok(Json(broadcast_desktop_command(
        &state,
        "session-focus",
        json!({ "sessionId": session_id }),
    )))
}

pub async fn desktop_arrangement_restore(
    State(state): State<AppState>,
    Path(arrangement_id): Path<String>,
) -> ApiResult<DesktopCommandResponse> {
    Ok(Json(broadcast_desktop_command(
        &state,
        "arrangement-restore",
        json!({ "arrangementId": arrangement_id }),
    )))
}

pub async fn desktop_pane_split(
    State(state): State<AppState>,
    Path(pane_id): Path<String>,
    ApiJson(body): ApiJson<DesktopPaneSplitBody>,
) -> ApiResult<DesktopCommandResponse> {
    validate_version(body.protocol_version.as_deref())?;
    let direction = if body.direction == "horizontal" {
        "horizontal"
    } else {
        "vertical"
    };
    Ok(Json(broadcast_desktop_command(
        &state,
        "pane-split",
        json!({
            "paneId": pane_id,
            "direction": direction,
        }),
    )))
}

pub async fn desktop_pane_focus(
    State(state): State<AppState>,
    Path(pane_id): Path<String>,
) -> ApiResult<DesktopCommandResponse> {
    Ok(Json(broadcast_desktop_command(
        &state,
        "pane-focus",
        json!({ "paneId": pane_id }),
    )))
}

pub async fn desktop_pane_maximize(
    State(state): State<AppState>,
    Path(pane_id): Path<String>,
) -> ApiResult<DesktopCommandResponse> {
    Ok(Json(broadcast_desktop_command(
        &state,
        "pane-maximize",
        json!({ "paneId": pane_id }),
    )))
}

fn broadcast_desktop_command(
    state: &AppState,
    kind: &str,
    payload: Value,
) -> DesktopCommandResponse {
    let command_id = Ulid::new().to_string();
    state.broadcast(ServerMessage::DesktopCommand {
        protocol_version: PROTOCOL_VERSION.to_string(),
        machine_id: state.machine_id.clone(),
        command_id: command_id.clone(),
        kind: kind.to_string(),
        payload,
    });
    DesktopCommandResponse {
        ok: true,
        protocol_version: PROTOCOL_VERSION.to_string(),
        command_id,
    }
}

pub async fn pair(
    State(state): State<AppState>,
    Extension(auth): Extension<auth::AuthScope>,
    ApiJson(body): ApiJson<PairRequest>,
) -> ApiResult<PairResponse> {
    let device_id = Ulid::new().to_string();
    if auth.scope == ClientScope::ReadOnly
        && !state
            .store
            .consume_spectator_token(&auth.token, &device_id)
            .map_err(internal_error)?
    {
        return Err((
            StatusCode::FORBIDDEN,
            Json(ApiError::new(
                "spectator pairing token has already been used",
            )),
        ));
    }
    state
        .store
        .insert_device(
            &device_id,
            &body.device_label,
            body.push_subscription.as_ref(),
            auth.scope,
        )
        .map_err(internal_error)?;
    Ok(Json(PairResponse {
        protocol_version: PROTOCOL_VERSION.to_string(),
        device_id,
        machine_id: state.machine_id,
        scope: auth.scope,
    }))
}

pub async fn spectator_pairing_payload(State(state): State<AppState>) -> ApiResult<Value> {
    let token = secret::generate_token();
    state
        .store
        .insert_spectator_token(&token)
        .map_err(internal_error)?;
    let mut payload = state.transports.pairing_payload().await;
    payload.token = token;
    payload.scope = ClientScope::ReadOnly;
    serde_json::to_value(payload)
        .map(Json)
        .map_err(|err| internal_error(err.into()))
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

pub async fn status(State(state): State<AppState>) -> ApiResult<Value> {
    let pending_approvals = state.store.list_pending().map_err(internal_error)?.len();
    let runtime_config = state.runtime_config().await;
    let config_path = state
        .config_path()
        .map(|path| path.display().to_string())
        .unwrap_or_else(|_| "~/.config/onibi/config.toml".to_string());
    let orchestration = state.orchestration.summary().await;
    Ok(Json(json!({
        "ok": true,
        "protocol_version": PROTOCOL_VERSION,
        "version": env!("CARGO_PKG_VERSION"),
        "machine_id": state.machine_id.clone(),
        "uptimeSecs": state.uptime_secs(),
        "configPath": config_path,
        "runtimeConfig": runtime_config,
        "pending_approvals": pending_approvals,
        "orchestration": orchestration,
        "transports": state.transports.status_snapshot().await,
    })))
}

pub async fn config_status(State(state): State<AppState>) -> ApiResult<Value> {
    let runtime_config = state.runtime_config().await;
    let validation = crate::config::validate().map_err(internal_error)?;
    Ok(Json(json!({
        "ok": true,
        "protocol_version": PROTOCOL_VERSION,
        "path": validation.path,
        "exists": validation.exists,
        "runtimeConfig": runtime_config,
        "fileRuntimeConfig": validation.runtime,
        "reloadableFields": [
            "server.approval_timeout_secs",
            "server.pty_ring_limit",
            "checkpointing.enabled"
        ],
        "restartRequiredFields": ["server.port"],
        "clientManagedFields": ["ui", "terminal", "keybindings", "workspaces"],
        "policyValidation": policy::validate(),
    })))
}

pub async fn config_reload(State(state): State<AppState>) -> ApiResult<Value> {
    let runtime_config = state
        .reload_runtime_config()
        .await
        .map_err(internal_error)?;
    Ok(Json(json!({
        "ok": true,
        "protocol_version": PROTOCOL_VERSION,
        "runtimeConfig": runtime_config,
        "appliedFields": [
            "server.approval_timeout_secs",
            "server.pty_ring_limit",
            "checkpointing.enabled"
        ],
        "restartRequiredFields": ["server.port"],
        "clientManagedFields": ["ui", "terminal", "keybindings", "workspaces"],
    })))
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
    ApiJson(payload): ApiJson<Value>,
) -> Result<Json<Value>, (StatusCode, Json<ApiError>)> {
    adapters::claude_code::handle_http_hook(&state, payload)
        .await
        .map(Json)
        .map_err(internal_error)
}

pub async fn codex_hook(
    State(state): State<AppState>,
    ApiJson(payload): ApiJson<Value>,
) -> Result<Json<Value>, (StatusCode, Json<ApiError>)> {
    adapters::codex::handle_http_hook(&state, payload)
        .await
        .map(Json)
        .map_err(internal_error)
}

pub async fn provider_event(
    State(state): State<AppState>,
    Path(agent): Path<String>,
    ApiJson(payload): ApiJson<Value>,
) -> ApiResult<Value> {
    let ingest = adapters::normalize_provider_event(&agent, payload).map_err(internal_error)?;
    validate_version(ingest.body.protocol_version.as_deref())?;
    let machine_id = ingest
        .body
        .machine_id
        .clone()
        .unwrap_or_else(|| state.machine_id.clone());
    state
        .store
        .insert_run_event(
            &machine_id,
            &ingest.run_session_id,
            &ingest.event_kind,
            &ingest.payload,
        )
        .map_err(internal_error)?;
    state.broadcast(ServerMessage::RunEvent {
        protocol_version: PROTOCOL_VERSION.to_string(),
        machine_id,
        session_id: ingest.run_session_id.clone(),
        kind: ingest.event_kind.clone(),
        payload: ingest.payload.clone(),
    });
    let session = state
        .orchestration
        .apply_provider_event(ingest.update)
        .await;
    checkpoint_post_if_enabled(&state, &ingest.run_session_id, &ingest.event_kind).await;
    Ok(Json(json!({
        "ok": true,
        "protocol_version": PROTOCOL_VERSION,
        "eventKind": ingest.event_kind,
        "sessionId": ingest.run_session_id,
        "correlated": session.is_some(),
        "session": session,
    })))
}

async fn checkpoint_pre_if_enabled(state: &AppState, approval: &Approval) {
    if !state.checkpointing_enabled().await {
        return;
    }
    match checkpointing::snapshot_pre(approval) {
        Ok(record) => {
            if let Err(error) = state.store.upsert_checkpoint_pre(&record) {
                tracing::warn!(%error, approval_id = approval.approval_id, "store checkpoint pre failed");
            }
        }
        Err(error) => {
            tracing::debug!(%error, approval_id = approval.approval_id, "checkpoint pre skipped");
        }
    }
}

async fn checkpoint_post_if_enabled(state: &AppState, session_id: &str, event_kind: &str) {
    if !state.checkpointing_enabled().await || !is_post_tool_event(event_kind) {
        return;
    }
    let record = match state.store.latest_checkpoint_without_post(session_id) {
        Ok(Some(record)) => record,
        Ok(None) => return,
        Err(error) => {
            tracing::warn!(%error, session_id, "load pending checkpoint failed");
            return;
        }
    };
    match checkpointing::snapshot_post(&record) {
        Ok(post_ref) => {
            if let Err(error) = state
                .store
                .mark_checkpoint_post(&record.approval_id, &post_ref)
            {
                tracing::warn!(%error, approval_id = record.approval_id, "store checkpoint post failed");
            }
        }
        Err(error) => {
            let message = error.to_string();
            let _ = state
                .store
                .mark_checkpoint_error(&record.approval_id, &message);
            tracing::debug!(%message, approval_id = record.approval_id, "checkpoint post skipped");
        }
    }
}

fn is_post_tool_event(event_kind: &str) -> bool {
    event_kind.contains("posttooluse")
        || event_kind.contains("tool.execute.after")
        || event_kind.contains("tool.executeafter")
}

pub async fn wait_for_approval_decision(
    state: &AppState,
    body: ApprovalRequestBody,
) -> Result<ApprovalDecisionResponse> {
    let approval_id = Ulid::new().to_string();
    let mut approval = Approval {
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

    let safe_mode = state
        .orchestration
        .session_safe_mode(&approval.session_id)
        .await;
    let trust_mode = state
        .orchestration
        .session_trust_mode(&approval.session_id)
        .await;
    if trust_mode == TrustMode::FullAccess && !safe_mode {
        let reason = "auto-allowed by session trust mode".to_string();
        approval.decision = Some(Decision::Allow);
        approval.reason = Some(reason.clone());
        approval.decided_by = Some("session-trust-mode".to_string());
        approval.decided_at = Some(now_millis());
        approval.metadata = with_trust_metadata(approval.metadata.take(), "full-access");
        state.store.insert_approval(&approval)?;
        checkpoint_pre_if_enabled(state, &approval).await;
        state
            .orchestration
            .set_status(&approval.session_id, AgentStatus::Working)
            .await;
        state.broadcast(ServerMessage::ApprovalResolved {
            protocol_version: PROTOCOL_VERSION.to_string(),
            approval_id: approval_id.clone(),
            machine_id: approval.machine_id.clone(),
            decision: Decision::Allow,
            by: Some("session-trust-mode".to_string()),
            reason: Some(reason.clone()),
        });
        return Ok(ApprovalDecisionResponse {
            protocol_version: PROTOCOL_VERSION.to_string(),
            approval_id,
            decision: Decision::Allow,
            updated_input: None,
            reason: Some(reason),
        });
    }

    let policy_evaluation = if safe_mode {
        Some(policy::evaluate_safe_mode(&approval))
    } else {
        policy::evaluate(&approval)?
    };
    if let Some(evaluation) = policy_evaluation {
        let source = if safe_mode { "safe-mode" } else { "manual" };
        approval.metadata = with_policy_metadata(approval.metadata.take(), &evaluation, source);
        let response_decision = if trust_mode == TrustMode::ApprovalRequired
            && !safe_mode
            && evaluation.decision == policy::PolicyDecision::AutoAllow
        {
            None
        } else {
            evaluation.response_decision()
        };
        if let Some(decision) = response_decision {
            let reason = evaluation.reason();
            approval.decision = Some(decision);
            approval.reason = Some(reason.clone());
            approval.decided_by = Some("policy".to_string());
            approval.decided_at = Some(now_millis());
            approval.metadata = with_policy_metadata(approval.metadata.take(), &evaluation, source);
            state.store.insert_approval(&approval)?;
            checkpoint_pre_if_enabled(state, &approval).await;
            state
                .orchestration
                .set_status(
                    &approval.session_id,
                    if decision == Decision::Allow {
                        AgentStatus::Working
                    } else {
                        AgentStatus::Done
                    },
                )
                .await;
            state.broadcast(ServerMessage::ApprovalResolved {
                protocol_version: PROTOCOL_VERSION.to_string(),
                approval_id: approval_id.clone(),
                machine_id: approval.machine_id.clone(),
                decision,
                by: Some("policy".to_string()),
                reason: Some(reason.clone()),
            });
            return Ok(ApprovalDecisionResponse {
                protocol_version: PROTOCOL_VERSION.to_string(),
                approval_id,
                decision,
                updated_input: None,
                reason: Some(reason),
            });
        }
    }

    state.store.insert_approval(&approval)?;
    checkpoint_pre_if_enabled(state, &approval).await;
    let rx = state.pending.insert(approval_id.clone()).await;
    state
        .orchestration
        .set_status(&approval.session_id, AgentStatus::Blocked)
        .await;
    let timeout = state.approval_timeout().await;
    let expires_at = approval
        .created_at
        .saturating_add(timeout.as_millis().min(i64::MAX as u128) as i64);
    state.broadcast(ServerMessage::approval_pending(&approval, Some(expires_at)));
    tokio::spawn(push::fanout_approval_pending(
        state.store.clone(),
        state.vapid.clone(),
        approval.clone(),
    ));

    match time::timeout(timeout, rx).await {
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
            state
                .orchestration
                .set_status(&approval.session_id, AgentStatus::Done)
                .await;
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

fn with_policy_metadata(
    metadata: Option<Value>,
    evaluation: &policy::PolicyEvaluation,
    source: &str,
) -> Option<Value> {
    let mut root = match metadata {
        Some(Value::Object(map)) => map,
        Some(value) => {
            let mut map = serde_json::Map::new();
            map.insert("original".to_string(), value);
            map
        }
        None => serde_json::Map::new(),
    };
    root.insert(
        "onibi_policy".to_string(),
        json!({
            "name": evaluation.rule_name,
            "decision": evaluation.decision,
            "source": source,
        }),
    );
    Some(Value::Object(root))
}

fn with_trust_metadata(metadata: Option<Value>, mode: &str) -> Option<Value> {
    let mut root = match metadata {
        Some(Value::Object(map)) => map,
        Some(value) => {
            let mut map = serde_json::Map::new();
            map.insert("original".to_string(), value);
            map
        }
        None => serde_json::Map::new(),
    };
    root.insert(
        "onibi_trust_mode".to_string(),
        json!({
            "mode": mode,
            "decision": "auto-allow",
            "source": "session-trust-mode",
        }),
    );
    Some(Value::Object(root))
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
    #[cfg(debug_assertions)]
    let message = error.to_string();
    #[cfg(not(debug_assertions))]
    let message = {
        tracing::warn!(%error, "request failed");
        "internal server error".to_string()
    };

    (
        StatusCode::INTERNAL_SERVER_ERROR,
        Json(ApiError::new(message)),
    )
}

fn not_found(message: &str) -> (StatusCode, Json<ApiError>) {
    (StatusCode::NOT_FOUND, Json(ApiError::new(message)))
}

impl ApprovalHistoryQuery {
    fn into_filter(self) -> ApprovalHistoryFilter {
        ApprovalHistoryFilter {
            agent: self.agent.filter(|value| !value.trim().is_empty()),
            tool: self.tool.filter(|value| !value.trim().is_empty()),
            decision: self.decision,
            from: self.from,
            to: self.to,
            limit: self.limit.unwrap_or(200),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{
        approval::store::ApprovalStore,
        orchestration::{SessionInfo, SessionLifecycle},
        server::router,
    };
    use axum::{
        body::{to_bytes, Body},
        http::{Request, StatusCode},
    };
    use serde_json::json;
    use tempfile::tempdir;
    use tower::ServiceExt;

    fn route_test_session(id: &str, trust_mode: TrustMode) -> SessionInfo {
        SessionInfo {
            id: id.to_string(),
            pane_id: id.to_string(),
            name: None,
            agent: Some("claude-code".to_string()),
            workspace_id: Some("workspace:/repo".to_string()),
            cwd: Some("/repo".to_string()),
            title: Some("Claude Code".to_string()),
            status: AgentStatus::Working,
            lifecycle: SessionLifecycle::Running,
            rows: 24,
            cols: 80,
            created_at: now_millis(),
            updated_at: now_millis(),
            restart: None,
            safe_mode: false,
            trust_mode,
            provider: None,
            remote: None,
            process_id: None,
            stopped_at: None,
            exit_code: None,
            exit_signal: None,
        }
    }

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

    #[tokio::test]
    async fn full_access_session_auto_allows_and_audits() {
        let dir = tempdir().unwrap();
        let store = ApprovalStore::open(dir.path().join("onibi.db")).unwrap();
        let state = AppState::for_tests(store.clone());
        state
            .orchestration
            .upsert_session_for_test(route_test_session("session-full", TrustMode::FullAccess))
            .await;
        let app = router(state);

        let response = app
            .oneshot(
                Request::builder()
                    .method("POST")
                    .uri("/v1/approval/request")
                    .header("authorization", "Bearer test-token")
                    .header("content-type", "application/json")
                    .body(Body::from(
                        json!({
                            "protocol_version": "1.0",
                            "session_id": "session-full",
                            "agent": "claude-code",
                            "tool": "Bash",
                            "input": {"command": "rm -rf node_modules"},
                            "cwd": "/tmp/project"
                        })
                        .to_string(),
                    ))
                    .unwrap(),
            )
            .await
            .unwrap();
        assert_eq!(response.status(), StatusCode::OK);
        let bytes = to_bytes(response.into_body(), usize::MAX).await.unwrap();
        let decision: ApprovalDecisionResponse = serde_json::from_slice(&bytes).unwrap();
        assert_eq!(decision.decision, Decision::Allow);
        assert_eq!(
            decision.reason.as_deref(),
            Some("auto-allowed by session trust mode")
        );

        let history = store
            .list_approvals(ApprovalHistoryFilter {
                limit: 10,
                ..Default::default()
            })
            .unwrap();
        assert_eq!(history.len(), 1);
        assert_eq!(history[0].session_id, "session-full");
        assert_eq!(history[0].decision, Some(Decision::Allow));
        assert_eq!(history[0].decided_by.as_deref(), Some("session-trust-mode"));
        assert_eq!(
            history[0].metadata.as_ref().unwrap()["onibi_trust_mode"]["mode"],
            "full-access"
        );
    }

    #[tokio::test]
    async fn malformed_json_returns_protocol_error_envelope() {
        let dir = tempdir().unwrap();
        let store = ApprovalStore::open(dir.path().join("onibi.db")).unwrap();
        let app = router(AppState::for_tests(store));

        let response = app
            .oneshot(
                Request::builder()
                    .method("POST")
                    .uri("/v1/run/event")
                    .header("authorization", "Bearer test-token")
                    .header("content-type", "application/json")
                    .body(Body::from("{not-json"))
                    .unwrap(),
            )
            .await
            .unwrap();
        assert_eq!(response.status(), StatusCode::BAD_REQUEST);
        let bytes = to_bytes(response.into_body(), usize::MAX).await.unwrap();
        let value: Value = serde_json::from_slice(&bytes).unwrap();
        assert_eq!(value["protocol_version"], PROTOCOL_VERSION);
        assert!(value["error"].as_str().unwrap_or_default().contains("JSON"));
    }

    #[tokio::test]
    async fn emergency_stop_denies_pending_approvals() {
        let dir = tempdir().unwrap();
        let store = ApprovalStore::open(dir.path().join("onibi.db")).unwrap();
        store
            .insert_approval(&Approval {
                protocol_version: PROTOCOL_VERSION.to_string(),
                approval_id: "approval-1".to_string(),
                machine_id: "machine".to_string(),
                session_id: "session".to_string(),
                agent: "claude-code".to_string(),
                tool: "Bash".to_string(),
                input: json!({"command": "rm -rf tmp"}),
                cwd: "/repo".to_string(),
                metadata: None,
                decision: None,
                updated_input: None,
                reason: None,
                decided_by: None,
                created_at: now_millis(),
                decided_at: None,
            })
            .unwrap();
        let app = router(AppState::for_tests(store.clone()));

        let response = app
            .oneshot(
                Request::builder()
                    .method("POST")
                    .uri("/v1/emergency-stop")
                    .header("authorization", "Bearer test-token")
                    .body(Body::empty())
                    .unwrap(),
            )
            .await
            .unwrap();
        assert_eq!(response.status(), StatusCode::OK);
        let bytes = to_bytes(response.into_body(), usize::MAX).await.unwrap();
        let value: Value = serde_json::from_slice(&bytes).unwrap();
        assert_eq!(value["deniedApprovals"], 1);

        let approval = store.get_approval("approval-1").unwrap().unwrap();
        assert_eq!(approval.decision, Some(Decision::Deny));
        assert_eq!(approval.decided_by.as_deref(), Some("kill-switch"));
        assert_eq!(
            approval.reason.as_deref(),
            Some("stopped by mobile kill switch"),
        );
    }

    #[tokio::test]
    async fn explicit_protocol_mismatch_returns_upgrade_required() {
        let dir = tempdir().unwrap();
        let store = ApprovalStore::open(dir.path().join("onibi.db")).unwrap();
        let app = router(AppState::for_tests(store));

        let response = app
            .oneshot(
                Request::builder()
                    .method("POST")
                    .uri("/v1/run/event")
                    .header("authorization", "Bearer test-token")
                    .header("content-type", "application/json")
                    .body(Body::from(
                        json!({
                            "protocol_version": "2.0",
                            "session_id": "pty-1",
                            "kind": "started",
                            "payload": {}
                        })
                        .to_string(),
                    ))
                    .unwrap(),
            )
            .await
            .unwrap();
        assert_eq!(response.status(), StatusCode::UPGRADE_REQUIRED);
        let bytes = to_bytes(response.into_body(), usize::MAX).await.unwrap();
        let value: Value = serde_json::from_slice(&bytes).unwrap();
        assert_eq!(value["protocol_version"], PROTOCOL_VERSION);
        assert_eq!(value["error"], "protocol_version mismatch");
    }

    #[tokio::test]
    async fn approval_history_and_export_include_decisions() {
        let dir = tempdir().unwrap();
        let store = ApprovalStore::open(dir.path().join("onibi.db")).unwrap();
        let state = AppState::for_tests(store.clone());
        let app = router(state);

        let approval = Approval {
            protocol_version: PROTOCOL_VERSION.to_string(),
            approval_id: "approval-1".to_string(),
            machine_id: "machine".to_string(),
            session_id: "session".to_string(),
            agent: "claude-code".to_string(),
            tool: "Bash".to_string(),
            input: json!({"command": "rm -rf node_modules"}),
            cwd: "/repo".to_string(),
            metadata: None,
            decision: None,
            updated_input: None,
            reason: None,
            decided_by: None,
            created_at: now_millis(),
            decided_at: None,
        };
        store.insert_approval(&approval).unwrap();
        store
            .decide(
                "approval-1",
                &ApprovalDecisionBody {
                    decision: Decision::Deny,
                    updated_input: None,
                    reason: Some("too broad".to_string()),
                    by: Some("mobile".to_string()),
                },
            )
            .unwrap();

        let response = app
            .clone()
            .oneshot(
                Request::builder()
                    .method("GET")
                    .uri("/v1/approval/history?decision=deny")
                    .header("authorization", "Bearer test-token")
                    .body(Body::empty())
                    .unwrap(),
            )
            .await
            .unwrap();
        assert_eq!(response.status(), StatusCode::OK);
        let bytes = to_bytes(response.into_body(), usize::MAX).await.unwrap();
        let approvals: Vec<Approval> = serde_json::from_slice(&bytes).unwrap();
        assert_eq!(approvals.len(), 1);
        assert_eq!(approvals[0].reason.as_deref(), Some("too broad"));

        let response = app
            .oneshot(
                Request::builder()
                    .method("GET")
                    .uri("/v1/approval/history/export?decision=deny")
                    .header("authorization", "Bearer test-token")
                    .body(Body::empty())
                    .unwrap(),
            )
            .await
            .unwrap();
        assert_eq!(response.status(), StatusCode::OK);
        let bytes = to_bytes(response.into_body(), usize::MAX).await.unwrap();
        let body = String::from_utf8(bytes.to_vec()).unwrap();
        assert!(body.contains("\"approval_id\":\"approval-1\""));
        assert!(body.ends_with('\n'));
    }

    #[tokio::test]
    async fn desktop_state_round_trips_attention() {
        let dir = tempdir().unwrap();
        let store = ApprovalStore::open(dir.path().join("onibi.db")).unwrap();
        let app = router(AppState::for_tests(store));

        let response = app
            .clone()
            .oneshot(
                Request::builder()
                    .method("POST")
                    .uri("/v1/desktop/state")
                    .header("authorization", "Bearer test-token")
                    .header("content-type", "application/json")
                    .body(Body::from(
                        json!({
                            "protocol_version": "1.0",
                            "sessions": [
                                {
                                    "id": "pty-1",
                                    "title": "Codex",
                                    "agent": "codex",
                                    "workspaceId": "workspace:/repo",
                                    "status": "running",
                                    "attention": "failed",
                                    "previewUrl": "http://localhost:1420/"
                                }
                            ],
                            "arrangements": [{"id": "arrangement-1", "name": "Pairing"}],
                            "updatedAt": 1
                        })
                        .to_string(),
                    ))
                    .unwrap(),
            )
            .await
            .unwrap();
        assert_eq!(response.status(), StatusCode::OK);

        let response = app
            .oneshot(
                Request::builder()
                    .method("GET")
                    .uri("/v1/desktop/attention")
                    .header("authorization", "Bearer test-token")
                    .body(Body::empty())
                    .unwrap(),
            )
            .await
            .unwrap();
        assert_eq!(response.status(), StatusCode::OK);
        let bytes = to_bytes(response.into_body(), usize::MAX).await.unwrap();
        let value: Value = serde_json::from_slice(&bytes).unwrap();
        assert_eq!(value["attentionCount"], 1);
        assert_eq!(value["sessions"][0]["previewUrl"], "http://localhost:1420/");
    }

    #[tokio::test]
    async fn desktop_command_blocks_round_trip() {
        let dir = tempdir().unwrap();
        let store = ApprovalStore::open(dir.path().join("onibi.db")).unwrap();
        let app = router(AppState::for_tests(store));

        let response = app
            .clone()
            .oneshot(
                Request::builder()
                    .method("POST")
                    .uri("/v1/desktop/command-blocks")
                    .header("authorization", "Bearer test-token")
                    .header("content-type", "application/json")
                    .body(Body::from(
                        json!({
                            "protocol_version": "1.0",
                            "id": "cmd-1",
                            "sessionId": "pty-1",
                            "workspaceId": "workspace:/repo",
                            "agent": "codex",
                            "command": "pnpm test",
                            "cwd": "/repo",
                            "startedAt": 10,
                            "endedAt": 20,
                            "exitCode": 1,
                            "status": "failed",
                            "outputPreview": "failed",
                            "previewUrl": "http://localhost:1420/",
                            "changedFiles": ["src/main.ts"],
                            "attention": "failed",
                            "source": "shell-integration"
                        })
                        .to_string(),
                    ))
                    .unwrap(),
            )
            .await
            .unwrap();
        assert_eq!(response.status(), StatusCode::OK);

        let response = app
            .oneshot(
                Request::builder()
                    .method("GET")
                    .uri("/v1/desktop/command-blocks?sessionId=pty-1")
                    .header("authorization", "Bearer test-token")
                    .body(Body::empty())
                    .unwrap(),
            )
            .await
            .unwrap();
        assert_eq!(response.status(), StatusCode::OK);
        let bytes = to_bytes(response.into_body(), usize::MAX).await.unwrap();
        let blocks: Vec<DesktopCommandBlock> = serde_json::from_slice(&bytes).unwrap();
        assert_eq!(blocks.len(), 1);
        assert_eq!(blocks[0].command, "pnpm test");
        assert_eq!(blocks[0].changed_files, vec!["src/main.ts"]);
    }

    #[tokio::test]
    async fn checkpoint_list_returns_records() {
        let dir = tempdir().unwrap();
        let store = ApprovalStore::open(dir.path().join("onibi.db")).unwrap();
        store
            .upsert_checkpoint_pre(&CheckpointRecord {
                approval_id: "approval-1".to_string(),
                session_id: "pty-1".to_string(),
                cwd: "/repo".to_string(),
                pre_ref: "refs/onibi/turns/approval-1/pre".to_string(),
                post_ref: None,
                created_at: 10,
                updated_at: 10,
                error: None,
            })
            .unwrap();
        let app = router(AppState::for_tests(store));

        let response = app
            .oneshot(
                Request::builder()
                    .method("GET")
                    .uri("/v1/checkpoints/list")
                    .header("authorization", "Bearer test-token")
                    .body(Body::empty())
                    .unwrap(),
            )
            .await
            .unwrap();
        assert_eq!(response.status(), StatusCode::OK);
        let bytes = to_bytes(response.into_body(), usize::MAX).await.unwrap();
        let records: Vec<CheckpointRecord> = serde_json::from_slice(&bytes).unwrap();
        assert_eq!(records.len(), 1);
        assert_eq!(records[0].approval_id, "approval-1");
    }

    #[tokio::test]
    async fn status_includes_runtime_config_and_orchestration_summary() {
        let dir = tempdir().unwrap();
        let store = ApprovalStore::open(dir.path().join("onibi.db")).unwrap();
        let app = router(AppState::for_tests(store));

        let response = app
            .oneshot(
                Request::builder()
                    .method("GET")
                    .uri("/v1/status")
                    .header("authorization", "Bearer test-token")
                    .body(Body::empty())
                    .unwrap(),
            )
            .await
            .unwrap();
        assert_eq!(response.status(), StatusCode::OK);
        let bytes = to_bytes(response.into_body(), usize::MAX).await.unwrap();
        let value: Value = serde_json::from_slice(&bytes).unwrap();
        assert_eq!(value["runtimeConfig"]["approvalTimeoutSecs"], 5);
        assert_eq!(
            value["runtimeConfig"]["ptyRingLimit"],
            crate::config::DEFAULT_PTY_RING_LIMIT
        );
        assert_eq!(value["runtimeConfig"]["checkpointingEnabled"], false);
        assert!(value["uptimeSecs"].is_u64());
        assert!(value["configPath"].is_string());
        assert_eq!(value["orchestration"]["paneCount"], 0);
    }

    #[tokio::test]
    async fn spectator_pairing_is_read_only_and_one_time() {
        let dir = tempdir().unwrap();
        let store = ApprovalStore::open(dir.path().join("onibi.db")).unwrap();
        let app = router(AppState::for_tests(store));

        let response = app
            .clone()
            .oneshot(
                Request::builder()
                    .method("POST")
                    .uri("/v1/token/spectator")
                    .header("authorization", "Bearer test-token")
                    .body(Body::from("{}"))
                    .unwrap(),
            )
            .await
            .unwrap();
        assert_eq!(response.status(), StatusCode::OK);
        let body = to_bytes(response.into_body(), usize::MAX).await.unwrap();
        let payload: serde_json::Value = serde_json::from_slice(&body).unwrap();
        assert_eq!(payload["scope"], "read-only");
        let token = payload["token"].as_str().unwrap();

        let response = app
            .clone()
            .oneshot(
                Request::builder()
                    .method("POST")
                    .uri("/v1/pair")
                    .header("authorization", format!("Bearer {token}"))
                    .header("content-type", "application/json")
                    .body(Body::from(json!({"deviceLabel": "spectator"}).to_string()))
                    .unwrap(),
            )
            .await
            .unwrap();
        assert_eq!(response.status(), StatusCode::OK);
        let body = to_bytes(response.into_body(), usize::MAX).await.unwrap();
        let paired: serde_json::Value = serde_json::from_slice(&body).unwrap();
        assert_eq!(paired["scope"], "read-only");

        let response = app
            .clone()
            .oneshot(
                Request::builder()
                    .method("POST")
                    .uri("/v1/pair")
                    .header("authorization", format!("Bearer {token}"))
                    .header("content-type", "application/json")
                    .body(Body::from(json!({"deviceLabel": "spectator2"}).to_string()))
                    .unwrap(),
            )
            .await
            .unwrap();
        assert_eq!(response.status(), StatusCode::FORBIDDEN);

        let response = app
            .clone()
            .oneshot(
                Request::builder()
                    .method("GET")
                    .uri("/v1/approval/pending")
                    .header("authorization", format!("Bearer {token}"))
                    .body(Body::empty())
                    .unwrap(),
            )
            .await
            .unwrap();
        assert_eq!(response.status(), StatusCode::OK);

        let response = app
            .oneshot(
                Request::builder()
                    .method("POST")
                    .uri("/v1/emergency-stop")
                    .header("authorization", format!("Bearer {token}"))
                    .body(Body::from("{}"))
                    .unwrap(),
            )
            .await
            .unwrap();
        assert_eq!(response.status(), StatusCode::FORBIDDEN);
    }

    #[tokio::test]
    async fn config_reload_reports_runtime_fields() {
        let dir = tempdir().unwrap();
        let store = ApprovalStore::open(dir.path().join("onibi.db")).unwrap();
        let app = router(AppState::for_tests(store));

        let response = app
            .oneshot(
                Request::builder()
                    .method("POST")
                    .uri("/v1/config/reload")
                    .header("authorization", "Bearer test-token")
                    .header("content-type", "application/json")
                    .body(Body::from("{}"))
                    .unwrap(),
            )
            .await
            .unwrap();
        assert_eq!(response.status(), StatusCode::OK);
        let bytes = to_bytes(response.into_body(), usize::MAX).await.unwrap();
        let value: Value = serde_json::from_slice(&bytes).unwrap();
        assert!(value["runtimeConfig"]["approvalTimeoutSecs"].is_u64());
        assert!(value["runtimeConfig"]["ptyRingLimit"].is_u64());
        assert_eq!(value["appliedFields"][0], "server.approval_timeout_secs");
        assert_eq!(value["restartRequiredFields"][0], "server.port");
    }
}
