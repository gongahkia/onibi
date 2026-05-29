#[cfg(feature = "gui")]
pub mod fonts;
#[cfg(feature = "gui")]
pub mod fs;
#[cfg(feature = "gui")]
pub mod git;
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
    git_commit, git_create_worktree, git_diff_file, git_discard_paths, git_remove_worktree,
    git_stage_paths, git_status, git_sync, git_unstage_paths, git_worktrees,
};
#[cfg(feature = "gui")]
use pty::{PtyEvent, PtyId, PtyManager, PtySpawnRequest};
#[cfg(feature = "gui")]
use review::{
    agent_review_accept, agent_review_diff, agent_review_note_human_write, agent_review_records,
    agent_review_reject, agent_review_start, agent_review_stop, AgentReviewManager,
};
#[cfg(feature = "gui")]
use serde::Serialize;
#[cfg(feature = "gui")]
use std::sync::Arc;
#[cfg(feature = "gui")]
use tauri::{Emitter, Manager};
#[cfg(feature = "gui")]
use tokio::time::{self, Duration, MissedTickBehavior};
#[cfg(feature = "gui")]
use tracing::warn;
#[cfg(feature = "gui")]
use tracing_subscriber::EnvFilter;

#[cfg(feature = "gui")]
#[derive(Clone, Serialize)]
#[serde(tag = "type", rename_all = "lowercase")]
enum PtyWireEvent {
    Data { data: String },
    Exit { code: u32, signal: Option<String> },
}

#[cfg(feature = "gui")]
#[derive(Clone, Serialize)]
#[serde(rename_all = "camelCase")]
struct ApprovalServerConfig {
    port: u16,
    token: String,
}

#[cfg(feature = "gui")]
#[tauri::command]
async fn approval_server_config() -> Result<ApprovalServerConfig, String> {
    let token = secret::load_or_create_token().map_err(|err| err.to_string())?;
    Ok(ApprovalServerConfig {
        port: 17893,
        token: token.token,
    })
}

#[cfg(feature = "gui")]
fn emit_pty_data(window: &tauri::Window, id: PtyId, pending: &mut Vec<u8>) -> bool {
    if pending.is_empty() {
        return true;
    }
    let payload = PtyWireEvent::Data {
        data: STANDARD.encode(&pending),
    };
    pending.clear();
    window.emit(&format!("pty:{id}"), payload).is_ok()
}

#[cfg(feature = "gui")]
#[tauri::command]
async fn pty_spawn(
    window: tauri::Window,
    state: tauri::State<'_, Arc<PtyManager>>,
    req: PtySpawnRequest,
) -> Result<PtyId, String> {
    let id = state.spawn(req).await.map_err(|err| err.to_string())?;
    let mut rx = state.subscribe(id).map_err(|err| err.to_string())?;
    tauri::async_runtime::spawn(async move {
        let mut pending = Vec::with_capacity(64 * 1024);
        let mut flush = time::interval(Duration::from_millis(16));
        flush.set_missed_tick_behavior(MissedTickBehavior::Skip);

        loop {
            tokio::select! {
                _ = flush.tick() => {
                    if !emit_pty_data(&window, id, &mut pending) {
                        break;
                    }
                }
                event = rx.recv() => match event {
                    Ok(PtyEvent::Data(bytes)) => {
                        pending.extend_from_slice(&bytes);
                        if pending.len() >= 64 * 1024 && !emit_pty_data(&window, id, &mut pending) {
                            break;
                        }
                    }
                    Ok(PtyEvent::Exit(exit)) => {
                        if !emit_pty_data(&window, id, &mut pending) {
                            break;
                        }
                        let _ = window.emit(
                            &format!("pty:{id}"),
                            PtyWireEvent::Exit {
                                code: exit.code,
                                signal: exit.signal,
                            },
                        );
                        break;
                    }
                    Err(tokio::sync::broadcast::error::RecvError::Lagged(skipped)) => {
                        warn!(%id, skipped, "pty event relay lagged");
                    }
                    Err(tokio::sync::broadcast::error::RecvError::Closed) => break,
                }
            }
        }
    });
    Ok(id)
}

#[cfg(feature = "gui")]
#[tauri::command]
async fn pty_write(
    state: tauri::State<'_, Arc<PtyManager>>,
    id: PtyId,
    data: Vec<u8>,
) -> Result<(), String> {
    state.write(id, &data).await.map_err(|err| err.to_string())
}

#[cfg(feature = "gui")]
#[tauri::command]
async fn pty_resize(
    state: tauri::State<'_, Arc<PtyManager>>,
    id: PtyId,
    rows: u16,
    cols: u16,
) -> Result<(), String> {
    state
        .resize(id, rows, cols)
        .await
        .map_err(|err| err.to_string())
}

#[cfg(feature = "gui")]
#[tauri::command]
async fn pty_kill(state: tauri::State<'_, Arc<PtyManager>>, id: PtyId) -> Result<(), String> {
    state.kill(id).await.map_err(|err| err.to_string())
}

#[cfg(feature = "gui")]
#[tauri::command]
async fn pty_list(state: tauri::State<'_, Arc<PtyManager>>) -> Result<Vec<PtyId>, String> {
    Ok(state.list().await)
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
        .manage(PtyManager::new())
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
            approval_server_config,
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
            agent_review_start,
            agent_review_stop,
            agent_review_note_human_write,
            agent_review_records,
            agent_review_diff,
            agent_review_accept,
            agent_review_reject,
            list_font_families
        ])
        .on_window_event(|window, event| {
            if matches!(event, tauri::WindowEvent::Destroyed) {
                if let Some(manager) = window.try_state::<Arc<PtyManager>>() {
                    let manager = manager.inner().clone();
                    tauri::async_runtime::spawn(async move {
                        for id in manager.list().await {
                            let _ = manager.kill(id).await;
                        }
                    });
                }
            }
        })
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}

#[cfg(not(feature = "gui"))]
pub fn run() {
    panic!("Onibi was built without the gui feature; use CLI commands or --headless");
}
