use crate::{config, secret, server};
use anyhow::{Context, Result};
use std::path::PathBuf;

#[derive(Debug, Clone, Default)]
pub struct HeadlessOpts {
    pub config_dir: Option<PathBuf>,
    pub port: Option<u16>,
    pub auto_transports: bool,
}

pub async fn run(opts: HeadlessOpts) -> Result<()> {
    if let Some(config_dir) = opts.config_dir {
        std::env::set_var("ONIBI_CONFIG_DIR", config_dir);
    }

    let config = config::load()?;
    let port = opts.port.unwrap_or_else(|| config.server_port());
    let state = server::AppState::from_config(port)?;

    if opts.auto_transports {
        for name in ["lan", "tailscale-funnel", "cloudflared"] {
            if let Err(error) = state.transports.enable(name).await {
                tracing::debug!(transport = name, %error, "auto-enable transport failed");
            }
        }
    }

    tracing::info!(
        port,
        config_dir = %secret::config_dir()?.display(),
        "starting Onibi headless daemon"
    );
    server::start_server(state, port)
        .await
        .context("run Onibi headless daemon")
}
