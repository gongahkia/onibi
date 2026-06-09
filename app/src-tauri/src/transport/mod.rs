pub mod cloudflared;
pub mod lan;
pub mod manager;
pub mod tailscale;

pub use manager::{default_transport_names, TransportManager, TransportSnapshot};

use anyhow::Result;
use serde::Serialize;
use std::sync::{
    atomic::{AtomicBool, Ordering},
    Arc,
};
use tokio::sync::oneshot;
use ts_rs::TS;

#[async_trait::async_trait]
pub trait Transport: Send + Sync {
    fn name(&self) -> &'static str;
    fn label(&self) -> &'static str;
    fn requires_external_dep(&self) -> Option<&'static str>;
    async fn start(&self, local_port: u16) -> Result<TransportHandle>;
    async fn status(&self) -> TransportStatus;
}

#[derive(Debug)]
pub struct TransportHandle {
    pub public_url: Option<String>,
    pub fingerprint: Option<String>,
    pub alive: Arc<AtomicBool>,
    pub shutdown: oneshot::Sender<()>,
}

impl TransportHandle {
    pub fn new(
        public_url: Option<String>,
        fingerprint: Option<String>,
        shutdown: oneshot::Sender<()>,
    ) -> Self {
        Self {
            public_url,
            fingerprint,
            alive: Arc::new(AtomicBool::new(true)),
            shutdown,
        }
    }

    pub fn with_alive(
        public_url: Option<String>,
        fingerprint: Option<String>,
        shutdown: oneshot::Sender<()>,
        alive: Arc<AtomicBool>,
    ) -> Self {
        Self {
            public_url,
            fingerprint,
            alive,
            shutdown,
        }
    }

    pub fn is_alive(&self) -> bool {
        self.alive.load(Ordering::Relaxed)
    }
}

#[allow(dead_code)]
#[derive(Debug, Clone, Serialize, PartialEq, Eq, TS)]
#[serde(tag = "state", rename_all = "kebab-case")]
pub enum TransportStatus {
    Stopped,
    Starting,
    Running {
        url: Option<String>,
        #[serde(skip_serializing_if = "Option::is_none")]
        fingerprint: Option<String>,
    },
    Failed {
        message: String,
    },
}

impl TransportStatus {
    pub fn failed(error: impl Into<String>) -> Self {
        Self::Failed {
            message: error.into(),
        }
    }
}
