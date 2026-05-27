use super::AppState;
use crate::protocol::{ServerMessage, PROTOCOL_VERSION};
use axum::{
    extract::{State, WebSocketUpgrade},
    response::IntoResponse,
};
use tokio::sync::broadcast;

const WS_MESSAGE_LIMIT: usize = 256 * 1024;

#[derive(Clone)]
pub struct WsHub {
    tx: broadcast::Sender<ServerMessage>,
}

impl WsHub {
    pub fn new() -> Self {
        let (tx, _) = broadcast::channel(256);
        Self { tx }
    }

    pub fn subscribe(&self) -> broadcast::Receiver<ServerMessage> {
        self.tx.subscribe()
    }

    pub fn broadcast(&self, message: ServerMessage) {
        let _ = self.tx.send(message);
    }
}

pub async fn realtime(State(state): State<AppState>, ws: WebSocketUpgrade) -> impl IntoResponse {
    ws.max_message_size(WS_MESSAGE_LIMIT)
        .max_frame_size(WS_MESSAGE_LIMIT)
        .on_upgrade(move |socket| async move {
            let mut socket = socket;
            let pending = state.store.list_pending().unwrap_or_default();
            for approval in pending {
                let message = ServerMessage::from(&approval);
                if send_message(&mut socket, &message).await.is_err() {
                    return;
                }
            }

            let mut rx = state.hub.subscribe();
            let mut ping = tokio::time::interval(std::time::Duration::from_secs(30));
            loop {
                tokio::select! {
                    _ = ping.tick() => {
                        let message = ServerMessage::Ping {
                            protocol_version: PROTOCOL_VERSION.to_string(),
                            machine_id: state.machine_id.clone(),
                        };
                        if send_message(&mut socket, &message).await.is_err() {
                            break;
                        }
                    }
                    result = rx.recv() => match result {
                        Ok(message) => {
                            if send_message(&mut socket, &message).await.is_err() {
                                break;
                            }
                        }
                        Err(broadcast::error::RecvError::Lagged(_)) => continue,
                        Err(broadcast::error::RecvError::Closed) => break,
                    }
                }
            }
        })
}

async fn send_message(
    socket: &mut axum::extract::ws::WebSocket,
    message: &ServerMessage,
) -> Result<(), axum::Error> {
    let raw = serde_json::to_string(message).unwrap_or_else(|_| "{\"type\":\"ping\"}".to_string());
    socket.send(axum::extract::ws::Message::Text(raw)).await
}
