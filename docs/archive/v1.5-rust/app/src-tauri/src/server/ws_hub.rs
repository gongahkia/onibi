use super::AppState;
use crate::protocol::{ServerMessage, PROTOCOL_VERSION};
use axum::{
    extract::{
        ws::{Message, WebSocket},
        State, WebSocketUpgrade,
    },
    response::IntoResponse,
};
use futures_util::{stream::SplitSink, SinkExt, StreamExt};
use serde_json::Value;
use std::time::{Duration, Instant};
use tokio::sync::broadcast;

const WS_MESSAGE_LIMIT: usize = 256 * 1024;
const HEARTBEAT_INTERVAL: Duration = Duration::from_secs(30);
const HEARTBEAT_TIMEOUT: Duration = Duration::from_secs(90);

type WsSender = SplitSink<WebSocket, Message>;

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
            let (mut sender, mut receiver) = socket.split();
            let pending = state.store.list_pending().unwrap_or_default();
            let approval_timeout = state
                .approval_timeout()
                .await
                .as_millis()
                .min(i64::MAX as u128) as i64;
            for approval in pending {
                let message = ServerMessage::approval_pending(
                    &approval,
                    Some(approval.created_at.saturating_add(approval_timeout)),
                );
                if send_message(&mut sender, &message).await.is_err() {
                    return;
                }
            }

            let mut rx = state.hub.subscribe();
            let mut ping = tokio::time::interval(HEARTBEAT_INTERVAL);
            let mut last_pong = Instant::now();
            loop {
                tokio::select! {
                    _ = ping.tick() => {
                        if last_pong.elapsed() >= HEARTBEAT_TIMEOUT {
                            let _ = sender.send(Message::Close(None)).await;
                            break;
                        }
                        let message = ServerMessage::Ping {
                            protocol_version: PROTOCOL_VERSION.to_string(),
                            machine_id: state.machine_id.clone(),
                        };
                        if send_message(&mut sender, &message).await.is_err() {
                            break;
                        }
                    }
                    incoming = receiver.next() => match incoming {
                        Some(Ok(message)) => match client_message_effect(&message) {
                            ClientMessageEffect::Pong => last_pong = Instant::now(),
                            ClientMessageEffect::Close => break,
                            ClientMessageEffect::Ignore => {}
                        },
                        Some(Err(_)) | None => break,
                    },
                    result = rx.recv() => match result {
                        Ok(message) => {
                            if send_message(&mut sender, &message).await.is_err() {
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

async fn send_message(sender: &mut WsSender, message: &ServerMessage) -> Result<(), axum::Error> {
    let raw = serde_json::to_string(message).unwrap_or_else(|_| "{\"type\":\"ping\"}".to_string());
    sender.send(Message::Text(raw)).await
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum ClientMessageEffect {
    Pong,
    Close,
    Ignore,
}

fn client_message_effect(message: &Message) -> ClientMessageEffect {
    match message {
        Message::Text(raw) if is_protocol_pong(raw) => ClientMessageEffect::Pong,
        Message::Pong(_) => ClientMessageEffect::Pong,
        Message::Close(_) => ClientMessageEffect::Close,
        _ => ClientMessageEffect::Ignore,
    }
}

fn is_protocol_pong(raw: &str) -> bool {
    serde_json::from_str::<Value>(raw)
        .ok()
        .and_then(|value| {
            value
                .get("type")
                .and_then(Value::as_str)
                .map(|message_type| message_type == "pong")
        })
        .unwrap_or(false)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn client_message_effect_detects_protocol_pong() {
        let message = Message::Text(r#"{"type":"pong","extra":true}"#.into());
        assert_eq!(client_message_effect(&message), ClientMessageEffect::Pong);
    }

    #[test]
    fn client_message_effect_ignores_unknown_text() {
        let message = Message::Text(r#"{"type":"unknown"}"#.into());
        assert_eq!(client_message_effect(&message), ClientMessageEffect::Ignore);
    }

    #[test]
    fn client_message_effect_detects_close() {
        let message = Message::Close(None);
        assert_eq!(client_message_effect(&message), ClientMessageEffect::Close);
    }
}
