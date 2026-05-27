pub mod pty;
pub mod util;

use base64::{engine::general_purpose::STANDARD, Engine as _};
use pty::{PtyEvent, PtyId, PtyManager, PtySpawnRequest};
use serde::Serialize;
use std::sync::Arc;
use tauri::{Emitter, Manager};
use tokio::time::{self, Duration, MissedTickBehavior};
use tracing::warn;
use tracing_subscriber::EnvFilter;

#[derive(Clone, Serialize)]
#[serde(tag = "type", rename_all = "lowercase")]
enum PtyWireEvent {
    Data { data: String },
    Exit { code: u32, signal: Option<String> },
}

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

#[tauri::command]
async fn pty_write(
    state: tauri::State<'_, Arc<PtyManager>>,
    id: PtyId,
    data: Vec<u8>,
) -> Result<(), String> {
    state.write(id, &data).await.map_err(|err| err.to_string())
}

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

#[tauri::command]
async fn pty_kill(state: tauri::State<'_, Arc<PtyManager>>, id: PtyId) -> Result<(), String> {
    state.kill(id).await.map_err(|err| err.to_string())
}

#[tauri::command]
async fn pty_list(state: tauri::State<'_, Arc<PtyManager>>) -> Result<Vec<PtyId>, String> {
    Ok(state.list().await)
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let _ = tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| "info,onibi=debug,app_lib=debug".into()),
        )
        .try_init();

    tauri::Builder::default()
        .manage(PtyManager::new())
        .plugin(tauri_plugin_opener::init())
        .invoke_handler(tauri::generate_handler![
            pty_spawn, pty_write, pty_resize, pty_kill, pty_list
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
