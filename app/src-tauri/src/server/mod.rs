pub mod auth;
pub mod pairing;
pub mod routes;
pub mod ws_hub;

use crate::{
    approval::{pending::PendingApprovals, store::ApprovalStore},
    protocol::ServerMessage,
    secret,
};
use anyhow::{Context, Result};
use axum::{
    middleware,
    routing::{get, post},
    Router,
};
use serde_json::Value;
use std::{
    collections::{HashMap, VecDeque},
    net::SocketAddr,
    sync::Arc,
    time::Duration,
};
use tokio::{net::TcpListener, sync::RwLock};
use tower_http::{cors::CorsLayer, trace::TraceLayer};
use ulid::Ulid;

const DEFAULT_RING_LIMIT: usize = 5_000;

#[derive(Clone)]
pub struct AppState {
    pub store: ApprovalStore,
    pub pending: PendingApprovals,
    pub hub: ws_hub::WsHub,
    pub machine_id: String,
    pub token: String,
    pub port: u16,
    pub approval_timeout: Duration,
    pty_ring: Arc<RwLock<HashMap<String, VecDeque<u8>>>>,
    ring_limit: usize,
}

impl AppState {
    pub fn from_config(port: u16) -> Result<Self> {
        let token = secret::load_or_create_token()?.token;
        let _ = secret::load_or_create_vapid_keys()?;
        let store = ApprovalStore::open(secret::db_path()?)?;
        let machine_id = match store.get_meta("machine_id")? {
            Some(machine_id) => machine_id,
            None => {
                let machine_id = Ulid::new().to_string();
                store.set_meta("machine_id", &machine_id)?;
                machine_id
            }
        };
        Ok(Self {
            store,
            pending: PendingApprovals::default(),
            hub: ws_hub::WsHub::new(),
            machine_id,
            token,
            port,
            approval_timeout: Duration::from_secs(600),
            pty_ring: Arc::new(RwLock::new(HashMap::new())),
            ring_limit: DEFAULT_RING_LIMIT,
        })
    }

    #[cfg(test)]
    pub fn for_tests(store: ApprovalStore) -> Self {
        Self {
            store,
            pending: PendingApprovals::default(),
            hub: ws_hub::WsHub::new(),
            machine_id: "01H00000000000000000000000".to_string(),
            token: "test-token".to_string(),
            port: 17893,
            approval_timeout: Duration::from_secs(5),
            pty_ring: Arc::new(RwLock::new(HashMap::new())),
            ring_limit: DEFAULT_RING_LIMIT,
        }
    }

    pub fn broadcast(&self, message: ServerMessage) {
        self.hub.broadcast(message);
    }

    pub async fn append_pty_output(&self, session_id: &str, data: &str) {
        let mut ring = self.pty_ring.write().await;
        let buffer = ring.entry(session_id.to_string()).or_default();
        buffer.extend(data.as_bytes());
        while buffer.len() > self.ring_limit {
            buffer.pop_front();
        }
    }

    pub async fn pty_tail(&self, session_id: &str) -> Option<Value> {
        let ring = self.pty_ring.read().await;
        ring.get(session_id).map(|buffer| {
            let bytes: Vec<u8> = buffer.iter().copied().collect();
            Value::String(String::from_utf8_lossy(&bytes).to_string())
        })
    }
}

pub fn router(state: AppState) -> Router {
    let authed = Router::new()
        .route("/v1/approval/request", post(routes::approval_request))
        .route("/v1/approval/pending", get(routes::approval_pending))
        .route("/v1/approval/:id/decide", post(routes::approval_decide))
        .route("/v1/run/event", post(routes::run_event))
        .route("/v1/pty/output", post(routes::pty_output))
        .route("/v1/pair", post(routes::pair))
        .route("/v1/qr", get(routes::qr))
        .route("/v1/realtime", get(ws_hub::realtime))
        .route(
            "/v1/adapters/claude-code/hook",
            post(routes::claude_code_hook),
        )
        .route("/v1/adapters/codex/hook", post(routes::codex_hook))
        .route_layer(middleware::from_fn_with_state(
            state.clone(),
            auth::require_bearer,
        ));

    Router::new()
        .route("/healthz", get(routes::healthz))
        .merge(authed)
        .layer(CorsLayer::permissive())
        .layer(TraceLayer::new_for_http())
        .with_state(state)
}

pub async fn start_server(state: AppState, port: u16) -> Result<()> {
    let addr = SocketAddr::from(([127, 0, 0, 1], port));
    let listener = TcpListener::bind(addr)
        .await
        .with_context(|| format!("bind Onibi server on {addr}"))?;
    tracing::info!(%addr, "Onibi approval server listening");
    axum::serve(listener, router(state)).await?;
    Ok(())
}

pub fn start_background_server(port: u16) {
    std::thread::Builder::new()
        .name("onibi-approval-server".to_string())
        .spawn(move || {
            let runtime = match tokio::runtime::Builder::new_multi_thread()
                .enable_all()
                .thread_name("onibi-server")
                .build()
            {
                Ok(runtime) => runtime,
                Err(error) => {
                    tracing::error!(%error, "failed to create server runtime");
                    return;
                }
            };
            runtime.block_on(async move {
                match AppState::from_config(port) {
                    Ok(state) => {
                        if let Err(error) = start_server(state, port).await {
                            tracing::error!(%error, "Onibi approval server stopped");
                        }
                    }
                    Err(error) => tracing::error!(%error, "failed to initialize server state"),
                }
            });
        })
        .expect("spawn Onibi approval server");
}
