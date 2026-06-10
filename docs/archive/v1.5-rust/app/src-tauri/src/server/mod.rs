pub mod auth;
pub mod pairing;
pub mod routes;
pub mod static_files;
pub mod ws_hub;

use crate::{
    approval::{pending::PendingApprovals, store::ApprovalStore},
    config,
    orchestration::OrchestrationState,
    protocol::{DesktopSnapshotBody, ServerMessage},
    secret::{self, VapidKeys},
    transport::TransportManager,
};
use anyhow::{Context, Result};
use axum::{
    body::Body,
    extract::{ConnectInfo, DefaultBodyLimit},
    http::{header, HeaderMap, HeaderValue, Request},
    middleware,
    middleware::Next,
    response::Response,
    routing::{get, post},
    Router,
};
use std::{
    collections::{HashMap, VecDeque},
    convert::Infallible,
    fs::{self, OpenOptions},
    io::Write,
    net::{IpAddr, Ipv4Addr, SocketAddr},
    path::Path,
    sync::{Arc, RwLock as StdRwLock},
    time::{Duration, Instant},
};
use tokio::{net::TcpListener, sync::RwLock};
use tower_governor::{
    errors::GovernorError, governor::GovernorConfigBuilder, key_extractor::KeyExtractor,
    GovernorLayer,
};
use tower_http::{cors::CorsLayer, trace::TraceLayer};
use ulid::Ulid;

const APPROVAL_BODY_LIMIT: usize = 1024 * 1024;
const PTY_OUTPUT_BODY_LIMIT: usize = 5 * 1024 * 1024;

#[derive(Clone)]
pub struct AppState {
    pub store: ApprovalStore,
    pub pending: PendingApprovals,
    pub hub: ws_hub::WsHub,
    pub machine_id: String,
    pub token: Arc<StdRwLock<String>>,
    pub vapid: VapidKeys,
    pub transports: TransportManager,
    pub orchestration: Arc<OrchestrationState>,
    started_at: Instant,
    runtime_config: Arc<RwLock<config::RuntimeConfig>>,
    pty_ring: Arc<RwLock<HashMap<String, VecDeque<u8>>>>,
    desktop_snapshot: Arc<RwLock<DesktopSnapshotBody>>,
}

impl AppState {
    pub fn from_config(port: u16) -> Result<Self> {
        let app_config = config::load()?;
        let runtime_config = app_config.runtime_config();
        let token = secret::load_or_create_token()?.token;
        mirror_token_file(&token);
        let vapid = secret::load_or_create_vapid_keys()?;
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
            machine_id: machine_id.clone(),
            token: Arc::new(StdRwLock::new(token.clone())),
            vapid: vapid.clone(),
            transports: TransportManager::new(
                port,
                machine_id.clone(),
                token.clone(),
                vapid.public_key,
            ),
            orchestration: OrchestrationState::new(token.clone()),
            started_at: Instant::now(),
            runtime_config: Arc::new(RwLock::new(runtime_config)),
            pty_ring: Arc::new(RwLock::new(HashMap::new())),
            desktop_snapshot: Arc::new(RwLock::new(DesktopSnapshotBody::default())),
        })
    }

    #[cfg(test)]
    pub fn for_tests(store: ApprovalStore) -> Self {
        Self {
            store,
            pending: PendingApprovals::default(),
            hub: ws_hub::WsHub::new(),
            machine_id: "01H00000000000000000000000".to_string(),
            token: Arc::new(StdRwLock::new("test-token".to_string())),
            vapid: VapidKeys {
                public_key: "test-vapid".to_string(),
                private_key: "test-vapid-private".to_string(),
            },
            transports: TransportManager::new(
                17893,
                "01H00000000000000000000000".to_string(),
                "test-token".to_string(),
                "test-vapid".to_string(),
            ),
            orchestration: OrchestrationState::new("test-token".to_string()),
            started_at: Instant::now(),
            runtime_config: Arc::new(RwLock::new(config::RuntimeConfig {
                approval_timeout_secs: 5,
                pty_ring_limit: config::DEFAULT_PTY_RING_LIMIT,
                checkpointing_enabled: false,
                checkpoint_max_records: config::DEFAULT_CHECKPOINT_MAX_RECORDS,
                checkpoint_max_age_days: config::DEFAULT_CHECKPOINT_MAX_AGE_DAYS,
                checkpoint_max_changed_files: config::DEFAULT_CHECKPOINT_MAX_CHANGED_FILES,
                checkpoint_max_index_bytes: config::DEFAULT_CHECKPOINT_MAX_INDEX_BYTES,
                checkpoint_max_file_bytes: config::DEFAULT_CHECKPOINT_MAX_FILE_BYTES,
                checkpoint_ignored_path_globs: Vec::new(),
            })),
            pty_ring: Arc::new(RwLock::new(HashMap::new())),
            desktop_snapshot: Arc::new(RwLock::new(DesktopSnapshotBody::default())),
        }
    }

    pub fn broadcast(&self, message: ServerMessage) {
        self.hub.broadcast(message);
    }

    pub fn token(&self) -> String {
        self.token
            .read()
            .map(|token| token.clone())
            .unwrap_or_default()
    }

    pub fn replace_token(&self, token: String) {
        if let Ok(mut current) = self.token.write() {
            *current = token.clone();
        }
        self.transports.set_token(token.clone());
        self.orchestration.set_token(token.clone());
        mirror_token_file(&token);
    }

    pub async fn append_pty_output(&self, session_id: &str, data: &str) {
        let limit = self.runtime_config.read().await.pty_ring_limit;
        let mut ring = self.pty_ring.write().await;
        let buffer = ring.entry(session_id.to_string()).or_default();
        buffer.extend(data.as_bytes());
        while buffer.len() > limit {
            buffer.pop_front();
        }
    }

    pub async fn approval_timeout(&self) -> Duration {
        Duration::from_secs(self.runtime_config.read().await.approval_timeout_secs)
    }

    pub async fn runtime_config(&self) -> config::RuntimeConfig {
        self.runtime_config.read().await.clone()
    }

    pub async fn checkpointing_enabled(&self) -> bool {
        self.runtime_config.read().await.checkpointing_enabled
    }

    pub async fn checkpoint_retention(&self) -> (usize, u64) {
        let runtime = self.runtime_config.read().await;
        (
            runtime.checkpoint_max_records,
            runtime.checkpoint_max_age_days,
        )
    }

    pub async fn checkpoint_guardrails(&self) -> crate::checkpointing::CheckpointGuardrails {
        let runtime = self.runtime_config.read().await;
        crate::checkpointing::CheckpointGuardrails {
            max_changed_files: runtime.checkpoint_max_changed_files,
            max_index_bytes: runtime.checkpoint_max_index_bytes,
            max_file_bytes: runtime.checkpoint_max_file_bytes,
            ignored_path_globs: runtime.checkpoint_ignored_path_globs.clone(),
        }
    }

    pub async fn reload_runtime_config(&self) -> Result<config::RuntimeConfig> {
        let runtime_config = config::load()?.runtime_config();
        *self.runtime_config.write().await = runtime_config.clone();
        Ok(runtime_config)
    }

    pub fn uptime_secs(&self) -> u64 {
        self.started_at.elapsed().as_secs()
    }

    pub fn config_path(&self) -> Result<std::path::PathBuf> {
        config::path()
    }

    pub async fn desktop_snapshot(&self) -> DesktopSnapshotBody {
        self.desktop_snapshot.read().await.clone()
    }

    pub async fn set_desktop_snapshot(&self, snapshot: DesktopSnapshotBody) {
        *self.desktop_snapshot.write().await = snapshot;
    }
}

fn mirror_token_file(token: &str) {
    let Ok(path) = secret::token_path() else {
        return;
    };
    if fs::read_to_string(&path).is_ok_and(|raw| raw.trim() == token) {
        return;
    }
    if let Err(error) = write_token_file(&path, token) {
        tracing::debug!(%error, "failed to mirror bearer token to file");
    }
}

fn write_token_file(path: &Path, token: &str) -> Result<()> {
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent)
            .with_context(|| format!("create config directory {}", parent.display()))?;
    }
    #[cfg(unix)]
    {
        use std::os::unix::fs::{OpenOptionsExt, PermissionsExt};
        let mut file = OpenOptions::new()
            .create(true)
            .truncate(true)
            .write(true)
            .mode(0o600)
            .open(path)
            .with_context(|| format!("open {}", path.display()))?;
        file.write_all(format!("{token}\n").as_bytes())
            .with_context(|| format!("write {}", path.display()))?;
        fs::set_permissions(path, fs::Permissions::from_mode(0o600))
            .with_context(|| format!("chmod 0600 {}", path.display()))?;
    }
    #[cfg(not(unix))]
    {
        let mut file = OpenOptions::new()
            .create(true)
            .truncate(true)
            .write(true)
            .open(path)
            .with_context(|| format!("open {}", path.display()))?;
        file.write_all(format!("{token}\n").as_bytes())
            .with_context(|| format!("write {}", path.display()))?;
    }
    Ok(())
}

pub fn router(state: AppState) -> Router {
    let mut governor = GovernorConfigBuilder::default();
    governor.per_millisecond(100).burst_size(10);
    let approval_rate_limit = governor
        .key_extractor(OnibiIpKeyExtractor)
        .finish()
        .expect("valid approval rate-limit config");
    let approval_rate_limit = Arc::new(approval_rate_limit);

    let authed = Router::new()
        .route(
            "/v1/approval/request",
            post(routes::approval_request)
                .layer::<_, Infallible>(DefaultBodyLimit::max(APPROVAL_BODY_LIMIT))
                .layer(GovernorLayer {
                    config: approval_rate_limit,
                }),
        )
        .route("/v1/approval/pending", get(routes::approval_pending))
        .route("/v1/approval/history", get(routes::approval_history))
        .route(
            "/v1/approval/history/export",
            get(routes::approval_history_export),
        )
        .route("/v1/checkpoints/list", get(routes::checkpoints_list))
        .route("/v1/checkpoints/prune", post(routes::checkpoints_prune))
        .route("/v1/checkpoints/:id/diff", get(routes::checkpoint_diff))
        .route(
            "/v1/checkpoints/:id/restore",
            post(routes::checkpoint_restore),
        )
        .route("/v1/approval/:id/decide", post(routes::approval_decide))
        .route("/v1/emergency-stop", post(routes::emergency_stop))
        .route("/v1/run/event", post(routes::run_event))
        .route(
            "/v1/pty/output",
            post(routes::pty_output)
                .layer::<_, Infallible>(DefaultBodyLimit::max(PTY_OUTPUT_BODY_LIMIT)),
        )
        .route("/v1/panes/targets", get(routes::pane_targets))
        .route("/v1/panes/:id/run", post(routes::pane_run))
        .route("/v1/panes/:id/send-text", post(routes::pane_send_text))
        .route("/v1/panes/:id/send-keys", post(routes::pane_send_keys))
        .route("/v1/pair", post(routes::pair))
        .route(
            "/v1/token/spectator",
            post(routes::spectator_pairing_payload),
        )
        .route("/v1/token/rotate", post(routes::token_rotate))
        .route("/v1/qr", get(routes::qr))
        .route("/v1/run/recent", get(routes::run_recent))
        .route("/v1/desktop/state", post(routes::desktop_state))
        .route("/v1/desktop/sessions", get(routes::desktop_sessions))
        .route("/v1/desktop/attention", get(routes::desktop_attention))
        .route(
            "/v1/desktop/command-blocks",
            get(routes::desktop_command_blocks).post(routes::desktop_command_block),
        )
        .route(
            "/v1/desktop/session/launch",
            post(routes::desktop_session_launch),
        )
        .route("/v1/desktop/remote/ssh", post(routes::desktop_remote_ssh))
        .route(
            "/v1/desktop/session/:id/input",
            post(routes::desktop_session_input),
        )
        .route(
            "/v1/desktop/worktree/open",
            post(routes::desktop_worktree_open),
        )
        .route(
            "/v1/desktop/session/:id/focus",
            post(routes::desktop_session_focus),
        )
        .route(
            "/v1/desktop/arrangement/:id/restore",
            post(routes::desktop_arrangement_restore),
        )
        .route(
            "/v1/desktop/pane/:id/split",
            post(routes::desktop_pane_split),
        )
        .route(
            "/v1/desktop/pane/:id/focus",
            post(routes::desktop_pane_focus),
        )
        .route(
            "/v1/desktop/pane/:id/maximize",
            post(routes::desktop_pane_maximize),
        )
        .route("/v1/status", get(routes::status))
        .route("/v1/config/status", get(routes::config_status))
        .route("/v1/config/reload", post(routes::config_reload))
        .route("/v1/transport/status", get(routes::transport_status))
        .route("/v1/transport/:name/enable", post(routes::transport_enable))
        .route(
            "/v1/transport/:name/disable",
            post(routes::transport_disable),
        )
        .route("/v1/transport/lan/cert", get(routes::lan_cert))
        .route("/v1/transport/lan/cert-qr", get(routes::lan_cert_qr))
        .route("/v1/realtime", get(ws_hub::realtime))
        .route(
            "/v1/adapters/claude-code/hook",
            post(routes::claude_code_hook),
        )
        .route("/v1/adapters/codex/hook", post(routes::codex_hook))
        .route(
            "/v1/adapters/:agent/acp/prompt",
            post(routes::provider_acp_prompt),
        )
        .route("/v1/adapters/:agent/event", post(routes::provider_event))
        .route_layer(middleware::from_fn_with_state(
            state.clone(),
            auth::require_bearer,
        ));

    Router::new()
        .route("/healthz", get(routes::healthz))
        .nest_service("/m", static_files::mobile_service())
        .merge(authed)
        .layer(middleware::from_fn(security_headers))
        .layer(CorsLayer::permissive())
        .layer(TraceLayer::new_for_http())
        .with_state(state)
}

pub async fn start_server(state: AppState, port: u16) -> Result<()> {
    state
        .orchestration
        .clone()
        .start_listeners()
        .await
        .context("start orchestration listeners")?;
    let addr = SocketAddr::from(([127, 0, 0, 1], port));
    let listener = TcpListener::bind(addr)
        .await
        .with_context(|| format!("bind Onibi server on {addr}"))?;
    tracing::info!(%addr, "Onibi approval server listening");
    axum::serve(
        listener,
        router(state).into_make_service_with_connect_info::<SocketAddr>(),
    )
    .await?;
    Ok(())
}

#[derive(Clone)]
struct OnibiIpKeyExtractor;

impl KeyExtractor for OnibiIpKeyExtractor {
    type Key = IpAddr;

    fn extract<T>(&self, req: &Request<T>) -> Result<Self::Key, GovernorError> {
        Ok(first_forwarded_ip(req.headers())
            .or_else(|| {
                req.extensions()
                    .get::<ConnectInfo<SocketAddr>>()
                    .map(|ConnectInfo(addr)| addr.ip())
            })
            .unwrap_or(IpAddr::V4(Ipv4Addr::LOCALHOST)))
    }
}

fn first_forwarded_ip(headers: &HeaderMap) -> Option<IpAddr> {
    headers
        .get("x-forwarded-for")
        .and_then(|value| value.to_str().ok())
        .and_then(|value| value.split(',').next())
        .and_then(parse_forwarded_ip)
        .or_else(|| {
            headers
                .get("x-real-ip")
                .and_then(|value| value.to_str().ok())
                .and_then(parse_forwarded_ip)
        })
        .or_else(|| {
            headers
                .get(header::FORWARDED)
                .and_then(|value| value.to_str().ok())
                .and_then(parse_rfc_forwarded_ip)
        })
}

fn parse_rfc_forwarded_ip(value: &str) -> Option<IpAddr> {
    value.split(';').find_map(|part| {
        let (key, value) = part.trim().split_once('=')?;
        key.eq_ignore_ascii_case("for")
            .then_some(value)
            .and_then(parse_forwarded_ip)
    })
}

fn parse_forwarded_ip(value: &str) -> Option<IpAddr> {
    let value = value
        .trim()
        .trim_matches('"')
        .trim_start_matches('[')
        .trim_end_matches(']');
    value.parse().ok()
}

async fn security_headers(req: Request<Body>, next: Next) -> Response {
    let path = req.uri().path().to_string();
    let hsts = should_send_hsts(req.headers());
    let mut response = next.run(req).await;

    if hsts {
        response.headers_mut().insert(
            header::STRICT_TRANSPORT_SECURITY,
            HeaderValue::from_static("max-age=31536000; includeSubDomains"),
        );
    }

    if path == "/m" || path.starts_with("/m/") {
        response.headers_mut().insert(
            header::CONTENT_SECURITY_POLICY,
            HeaderValue::from_static(
                "default-src 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; img-src 'self' data: blob:; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self' https: wss:; worker-src 'self'; manifest-src 'self'",
            ),
        );
        response.headers_mut().insert(
            header::X_CONTENT_TYPE_OPTIONS,
            HeaderValue::from_static("nosniff"),
        );
        response.headers_mut().insert(
            header::REFERRER_POLICY,
            HeaderValue::from_static("no-referrer"),
        );
    }

    response
}

fn should_send_hsts(headers: &HeaderMap) -> bool {
    let forwarded_https = headers
        .get("x-forwarded-proto")
        .and_then(|value| value.to_str().ok())
        .is_some_and(|value| value.eq_ignore_ascii_case("https"));
    let tunnel_host = headers
        .get(header::HOST)
        .and_then(|value| value.to_str().ok())
        .is_some_and(|host| host.ends_with(".ts.net") || host.ends_with(".trycloudflare.com"));

    forwarded_https || tunnel_host
}

#[cfg(feature = "gui")]
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
                        crate::push::register_bridge(state.store.clone(), state.vapid.clone());
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
