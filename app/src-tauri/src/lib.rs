#[cfg(feature = "gui")]
pub mod config;
#[cfg(feature = "gui")]
pub mod fonts;
#[cfg(feature = "gui")]
pub mod fs;
#[cfg(feature = "gui")]
pub mod git;
#[cfg(feature = "gui")]
mod orchestration;
#[cfg(feature = "gui")]
pub mod pty;
#[cfg(feature = "gui")]
pub mod review;
#[cfg(feature = "gui")]
pub mod secret;
#[cfg(feature = "gui")]
pub mod util;

#[cfg(feature = "gui")]
use base64::{engine::general_purpose::STANDARD, Engine as _};
#[cfg(feature = "gui")]
use fonts::list_font_families;
#[cfg(feature = "gui")]
use fs::{
    fs_create_dir, fs_create_file, fs_create_file_with_contents, fs_delete_path,
    fs_detect_terminal_configs, fs_list_dir, fs_move_path, fs_read_file, fs_read_ghostty_config,
    fs_read_preview_file, fs_rename_path, fs_resolve_binary, fs_search_workspace,
    fs_workspace_info, fs_write_file,
};
#[cfg(feature = "gui")]
use git::{
    git_clone_repository, git_commit, git_create_worktree, git_diff_file, git_discard_paths,
    git_remove_worktree, git_stage_paths, git_status, git_sync, git_unstage_paths, git_worktrees,
};
#[cfg(feature = "gui")]
use pty::{PtyId, PtySpawnRequest};
#[cfg(feature = "gui")]
use review::{
    agent_review_accept, agent_review_diff, agent_review_note_human_write, agent_review_records,
    agent_review_reject, agent_review_start, agent_review_stop, AgentReviewManager,
};
#[cfg(feature = "gui")]
use serde::{Deserialize, Serialize};
#[cfg(feature = "gui")]
use serde_json::{json, Value};
#[cfg(feature = "gui")]
use std::{
    collections::HashSet,
    sync::{Arc, Mutex, OnceLock},
};
#[cfg(feature = "gui")]
use tauri::{Emitter, Manager};
#[cfg(feature = "gui")]
use tracing_subscriber::EnvFilter;

#[cfg(feature = "gui")]
#[derive(Clone, Serialize)]
#[serde(tag = "type", rename_all = "lowercase")]
enum PtyWireEvent {
    Data {
        data: String,
        offset: u64,
    },
    Exit {
        code: u32,
        signal: Option<String>,
    },
    Notification {
        source: String,
        title: String,
        body: Option<String>,
        urgency: Option<String>,
    },
}

#[cfg(feature = "gui")]
#[derive(Clone, Serialize)]
#[serde(rename_all = "camelCase")]
struct PtyReplay {
    data: String,
    start_offset: u64,
    end_offset: u64,
}

#[cfg(feature = "gui")]
#[derive(Clone, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
struct PtySessionRestart {
    command: String,
    args: Vec<String>,
    cwd: Option<String>,
    env: Vec<(String, String)>,
    #[serde(default)]
    remote: Option<pty::RemoteSessionMetadata>,
}

#[cfg(feature = "gui")]
#[derive(Clone, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
struct PtyProviderResume {
    command: String,
    args: Vec<String>,
    source: Option<String>,
}

#[cfg(feature = "gui")]
#[derive(Clone, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
struct PtyProviderSession {
    agent: String,
    provider_session_id: Option<String>,
    conversation_id: Option<String>,
    resume: Option<PtyProviderResume>,
    updated_at: i64,
}

#[cfg(feature = "gui")]
#[derive(Clone, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
struct PtySessionMetadata {
    id: String,
    pane_id: String,
    name: Option<String>,
    agent: Option<String>,
    workspace_id: Option<String>,
    cwd: Option<String>,
    title: Option<String>,
    status: String,
    lifecycle: String,
    rows: u16,
    cols: u16,
    created_at: i64,
    updated_at: i64,
    #[serde(default)]
    process_id: Option<u32>,
    stopped_at: Option<i64>,
    exit_code: Option<u32>,
    exit_signal: Option<String>,
    restart: Option<PtySessionRestart>,
    provider: Option<PtyProviderSession>,
    #[serde(default)]
    remote: Option<pty::RemoteSessionMetadata>,
}

#[cfg(feature = "gui")]
#[derive(Clone, Deserialize, Serialize)]
#[serde(rename_all = "camelCase")]
struct PtyAttachResult {
    ok: bool,
    attached: bool,
    relaunched: bool,
    previous_session_id: Option<String>,
    id: String,
    session_id: String,
    pane_id: String,
    session: PtySessionMetadata,
}

#[cfg(feature = "gui")]
#[derive(Clone, Serialize)]
#[serde(rename_all = "camelCase")]
struct ApprovalServerConfig {
    port: u16,
    token: String,
}

#[cfg(feature = "gui")]
static RELAYED_PTY_IDS: OnceLock<Mutex<HashSet<String>>> = OnceLock::new();

#[cfg(feature = "gui")]
#[tauri::command]
async fn approval_server_config() -> Result<ApprovalServerConfig, String> {
    let token = secret::load_or_create_token().map_err(|err| err.to_string())?;
    let port = config::load()
        .map(|config| config.server_port())
        .unwrap_or(config::DEFAULT_PORT);
    Ok(ApprovalServerConfig {
        port,
        token: token.token,
    })
}

#[cfg(feature = "gui")]
#[tauri::command]
async fn onibi_read_config_toml() -> Result<Option<String>, String> {
    config::read_raw().map_err(|err| err.to_string())
}

#[cfg(feature = "gui")]
#[tauri::command]
async fn onibi_write_config_toml(toml: String) -> Result<(), String> {
    config::write_raw(&toml).map_err(|err| err.to_string())
}

#[cfg(feature = "gui")]
#[tauri::command]
async fn onibi_default_config_toml() -> Result<String, String> {
    Ok(config::default_config_toml())
}

#[cfg(feature = "gui")]
#[tauri::command]
async fn pty_spawn(window: tauri::Window, req: PtySpawnRequest) -> Result<PtyId, String> {
    let payload = serde_json::to_value(&req).map_err(|err| err.to_string())?;
    let response = orchestration::client::request("pty.spawn", payload)
        .await
        .map_err(|err| err.to_string())?;
    let id: PtyId = response
        .get("id")
        .and_then(Value::as_str)
        .ok_or_else(|| "daemon did not return a PTY id".to_string())?
        .parse()
        .map_err(|err| format!("parse daemon PTY id: {err}"))?;
    ensure_orchestration_relay(window, id).await?;
    Ok(id)
}

#[cfg(feature = "gui")]
async fn ensure_orchestration_relay(window: tauri::Window, id: PtyId) -> Result<(), String> {
    let id_string = id.to_string();
    let inserted = {
        let relayed = RELAYED_PTY_IDS.get_or_init(|| Mutex::new(HashSet::new()));
        let mut relayed = relayed.lock().map_err(|err| err.to_string())?;
        relayed.insert(id_string.clone())
    };
    if !inserted {
        return Ok(());
    }
    if let Err(error) = relay_orchestration_events(window, id).await {
        if let Some(relayed) = RELAYED_PTY_IDS.get() {
            if let Ok(mut relayed) = relayed.lock() {
                relayed.remove(&id_string);
            }
        }
        return Err(error);
    }
    Ok(())
}

#[cfg(feature = "gui")]
async fn relay_orchestration_events(window: tauri::Window, id: PtyId) -> Result<(), String> {
    let mut rx = orchestration::client::event_receiver(json!({"sessionId": id.to_string()}))
        .await
        .map_err(|err| err.to_string())?;
    tauri::async_runtime::spawn(async move {
        let id_string = id.to_string();
        while let Some(frame) = rx.recv().await {
            let Some(event) = frame.get("event") else {
                continue;
            };
            let Some(session_id) = event.get("session_id").and_then(Value::as_str) else {
                continue;
            };
            if session_id != id_string {
                continue;
            }
            match event.get("type").and_then(Value::as_str) {
                Some("pty-output") => {
                    let payload = PtyWireEvent::Data {
                        data: event
                            .get("data")
                            .and_then(Value::as_str)
                            .unwrap_or_default()
                            .to_string(),
                        offset: event.get("offset").and_then(Value::as_u64).unwrap_or(0),
                    };
                    if window.emit(&format!("pty:{id}"), payload).is_err() {
                        break;
                    }
                }
                Some("pty-exit") => {
                    let payload = PtyWireEvent::Exit {
                        code: event.get("code").and_then(Value::as_u64).unwrap_or(1) as u32,
                        signal: event
                            .get("signal")
                            .and_then(Value::as_str)
                            .map(ToOwned::to_owned),
                    };
                    let _ = window.emit(&format!("pty:{id}"), payload);
                    break;
                }
                Some("pty-notification") => {
                    let Some(notification) = event.get("notification") else {
                        continue;
                    };
                    let notice = pty_notification_from_value(notification);
                    if let Some(hook) = pty::notification_hook() {
                        hook(id.to_string(), notice.clone());
                    }
                    let payload = PtyWireEvent::Notification {
                        source: notification
                            .get("source")
                            .and_then(Value::as_str)
                            .unwrap_or("osc9")
                            .to_string(),
                        title: notification
                            .get("title")
                            .and_then(Value::as_str)
                            .unwrap_or_default()
                            .to_string(),
                        body: notification
                            .get("body")
                            .and_then(Value::as_str)
                            .map(ToOwned::to_owned),
                        urgency: notification
                            .get("urgency")
                            .and_then(Value::as_str)
                            .map(ToOwned::to_owned),
                    };
                    if window.emit(&format!("pty:{id}"), payload).is_err() {
                        break;
                    }
                }
                _ => {}
            }
        }
        if let Some(relayed) = RELAYED_PTY_IDS.get() {
            if let Ok(mut relayed) = relayed.lock() {
                relayed.remove(&id_string);
            }
        }
    });
    Ok(())
}

#[cfg(feature = "gui")]
fn pty_notification_from_value(value: &Value) -> pty::OscNotification {
    let source = match value
        .get("source")
        .and_then(Value::as_str)
        .unwrap_or("osc9")
    {
        "osc99" => pty::NotificationSource::Osc99,
        "osc777" => pty::NotificationSource::Osc777,
        _ => pty::NotificationSource::Osc9,
    };
    pty::OscNotification {
        source,
        title: value
            .get("title")
            .and_then(Value::as_str)
            .unwrap_or_default()
            .to_string(),
        body: value
            .get("body")
            .and_then(Value::as_str)
            .map(ToOwned::to_owned),
        urgency: value
            .get("urgency")
            .and_then(Value::as_str)
            .map(ToOwned::to_owned),
    }
}

#[cfg(feature = "gui")]
#[tauri::command]
async fn pty_write(id: PtyId, data: Vec<u8>) -> Result<(), String> {
    orchestration::client::request(
        "pty.write",
        json!({"id": id.to_string(), "data": STANDARD.encode(data)}),
    )
    .await
    .map(|_| ())
    .map_err(|err| err.to_string())
}

#[cfg(feature = "gui")]
#[tauri::command]
async fn pty_resize(id: PtyId, rows: u16, cols: u16) -> Result<(), String> {
    orchestration::client::request(
        "pty.resize",
        json!({"id": id.to_string(), "rows": rows, "cols": cols}),
    )
    .await
    .map(|_| ())
    .map_err(|err| err.to_string())
}

#[cfg(feature = "gui")]
#[tauri::command]
async fn pty_kill(id: PtyId) -> Result<(), String> {
    orchestration::client::request("pty.kill", json!({"id": id.to_string()}))
        .await
        .map(|_| ())
        .map_err(|err| err.to_string())
}

#[cfg(feature = "gui")]
#[tauri::command]
async fn pty_list() -> Result<Vec<PtyId>, String> {
    let response = orchestration::client::request("pty.list", json!({}))
        .await
        .map_err(|err| err.to_string())?;
    let sessions = response
        .get("sessions")
        .and_then(Value::as_array)
        .cloned()
        .unwrap_or_default();
    sessions
        .iter()
        .filter_map(|session| session.get("id").and_then(Value::as_str))
        .map(|id| {
            id.parse()
                .map_err(|err| format!("parse daemon PTY id: {err}"))
        })
        .collect()
}

#[cfg(feature = "gui")]
#[tauri::command]
async fn pty_sessions(window: tauri::Window) -> Result<Vec<PtySessionMetadata>, String> {
    let response = orchestration::client::request("session.list", json!({}))
        .await
        .map_err(|err| err.to_string())?;
    let sessions = response
        .get("sessions")
        .and_then(Value::as_array)
        .cloned()
        .unwrap_or_default()
        .into_iter()
        .map(|value| {
            serde_json::from_value::<PtySessionMetadata>(value).map_err(|err| err.to_string())
        })
        .collect::<Result<Vec<_>, _>>()?;
    for session in sessions
        .iter()
        .filter(|session| session.lifecycle == "running")
    {
        let id: PtyId = session
            .id
            .parse()
            .map_err(|err| format!("parse daemon PTY id: {err}"))?;
        ensure_orchestration_relay(window.clone(), id).await?;
    }
    Ok(sessions)
}

#[cfg(feature = "gui")]
#[tauri::command]
async fn session_attach(window: tauri::Window, id: String) -> Result<PtyAttachResult, String> {
    let response = orchestration::client::request("session.attach", json!({"id": id}))
        .await
        .map_err(|err| err.to_string())?;
    let result: PtyAttachResult =
        serde_json::from_value(response).map_err(|err| err.to_string())?;
    let pty_id: PtyId = result
        .id
        .parse()
        .map_err(|err| format!("parse daemon PTY id: {err}"))?;
    ensure_orchestration_relay(window, pty_id).await?;
    Ok(result)
}

#[cfg(feature = "gui")]
#[tauri::command]
async fn pty_replay(id: PtyId) -> Result<Option<PtyReplay>, String> {
    let response = orchestration::client::request("pty.replay", json!({"id": id.to_string()}))
        .await
        .map_err(|err| err.to_string())?;
    let data = response
        .get("data")
        .and_then(Value::as_str)
        .unwrap_or_default()
        .to_string();
    if data.is_empty() {
        return Ok(None);
    }
    Ok(Some(PtyReplay {
        data,
        start_offset: response
            .get("startOffset")
            .and_then(Value::as_u64)
            .unwrap_or(0),
        end_offset: response
            .get("endOffset")
            .and_then(Value::as_u64)
            .unwrap_or(0),
    }))
}

#[cfg(feature = "gui")]
#[cfg_attr(all(feature = "gui", mobile), tauri::mobile_entry_point)]
pub fn run() {
    let _ = tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| "info,onibi=debug,app_lib=debug".into()),
        )
        .try_init();

    tauri::Builder::default()
        .plugin(tauri_plugin_single_instance::init(|app, _args, _cwd| {
            if let Some(window) = app.get_webview_window("main") {
                let _ = window.unminimize();
                let _ = window.show();
                let _ = window.set_focus();
            }
        }))
        .manage(Arc::new(AgentReviewManager::new()))
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_store::Builder::default().build())
        .invoke_handler(tauri::generate_handler![
            pty_spawn,
            pty_write,
            pty_resize,
            pty_kill,
            pty_list,
            pty_sessions,
            session_attach,
            pty_replay,
            approval_server_config,
            onibi_read_config_toml,
            onibi_write_config_toml,
            onibi_default_config_toml,
            fs_list_dir,
            fs_read_file,
            fs_read_preview_file,
            fs_search_workspace,
            fs_write_file,
            fs_create_file,
            fs_create_file_with_contents,
            fs_create_dir,
            fs_rename_path,
            fs_move_path,
            fs_delete_path,
            fs_workspace_info,
            fs_resolve_binary,
            fs_read_ghostty_config,
            fs_detect_terminal_configs,
            git_status,
            git_stage_paths,
            git_unstage_paths,
            git_discard_paths,
            git_commit,
            git_sync,
            git_diff_file,
            git_worktrees,
            git_create_worktree,
            git_remove_worktree,
            git_clone_repository,
            agent_review_start,
            agent_review_stop,
            agent_review_note_human_write,
            agent_review_records,
            agent_review_diff,
            agent_review_accept,
            agent_review_reject,
            list_font_families
        ])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}

#[cfg(not(feature = "gui"))]
pub fn run() {
    panic!("Onibi was built without the gui feature; use CLI commands or --headless");
}
