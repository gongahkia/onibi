use bytes::Bytes;
use parking_lot::{Mutex, RwLock};
use portable_pty::{Child, MasterPty};
use serde::{Deserialize, Serialize};
use std::{collections::HashMap, io::Write, path::PathBuf, sync::Arc};
use thiserror::Error;
use tokio::sync::broadcast;
use uuid::Uuid;

pub type PtyId = Uuid;

#[derive(Debug, Error)]
pub enum PtyError {
    #[error("pty session {0} was not found")]
    NotFound(PtyId),
    #[error("pty session {0} has terminated")]
    Terminated(PtyId),
    #[error("pty worker failed: {0}")]
    Join(#[from] tokio::task::JoinError),
    #[error(transparent)]
    Io(#[from] std::io::Error),
    #[error(transparent)]
    Pty(#[from] anyhow::Error),
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct PtySpawnRequest {
    #[serde(default = "crate::util::shell::default_shell")]
    pub command: String,
    #[serde(default)]
    pub args: Vec<String>,
    #[serde(default)]
    pub cwd: Option<PathBuf>,
    #[serde(default)]
    pub env: Vec<(String, String)>,
    #[serde(default = "default_rows")]
    pub rows: u16,
    #[serde(default = "default_cols")]
    pub cols: u16,
}

fn default_rows() -> u16 {
    30
}

fn default_cols() -> u16 {
    100
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct PtyExitStatus {
    pub code: u32,
    pub signal: Option<String>,
}

impl From<portable_pty::ExitStatus> for PtyExitStatus {
    fn from(status: portable_pty::ExitStatus) -> Self {
        let message = status.to_string();
        Self {
            code: status.exit_code(),
            signal: message
                .strip_prefix("Terminated by ")
                .map(ToOwned::to_owned),
        }
    }
}

#[derive(Debug, Clone)]
pub enum PtyEvent {
    Data(Bytes),
    Exit(PtyExitStatus),
    Notification(super::notifications::OscNotification),
}

#[derive(Debug, Clone)]
enum PtySessionState {
    Running,
    Terminated(PtyExitStatus),
}

#[derive(Clone)]
pub struct PtySession {
    inner: Arc<PtySessionInner>,
}

struct PtySessionInner {
    id: PtyId,
    master: Arc<Mutex<Box<dyn MasterPty + Send>>>,
    child: Arc<Mutex<Box<dyn Child + Send + Sync>>>,
    writer: Arc<tokio::sync::Mutex<Box<dyn Write + Send>>>,
    tx: broadcast::Sender<PtyEvent>,
    state: Arc<RwLock<PtySessionState>>,
}

impl PtySession {
    pub fn new(
        id: PtyId,
        master: Box<dyn MasterPty + Send>,
        child: Box<dyn Child + Send + Sync>,
        writer: Box<dyn Write + Send>,
        tx: broadcast::Sender<PtyEvent>,
    ) -> Self {
        Self {
            inner: Arc::new(PtySessionInner {
                id,
                master: Arc::new(Mutex::new(master)),
                child: Arc::new(Mutex::new(child)),
                writer: Arc::new(tokio::sync::Mutex::new(writer)),
                tx,
                state: Arc::new(RwLock::new(PtySessionState::Running)),
            }),
        }
    }

    pub fn id(&self) -> PtyId {
        self.inner.id
    }

    pub fn child(&self) -> Arc<Mutex<Box<dyn Child + Send + Sync>>> {
        self.inner.child.clone()
    }

    pub fn master(&self) -> Arc<Mutex<Box<dyn MasterPty + Send>>> {
        self.inner.master.clone()
    }

    pub fn writer(&self) -> Arc<tokio::sync::Mutex<Box<dyn Write + Send>>> {
        self.inner.writer.clone()
    }

    pub fn sender(&self) -> broadcast::Sender<PtyEvent> {
        self.inner.tx.clone()
    }

    pub fn subscribe(&self) -> broadcast::Receiver<PtyEvent> {
        self.inner.tx.subscribe()
    }

    pub fn is_terminated(&self) -> bool {
        matches!(&*self.inner.state.read(), PtySessionState::Terminated(_))
    }

    pub fn exit_status(&self) -> Option<PtyExitStatus> {
        match &*self.inner.state.read() {
            PtySessionState::Running => None,
            PtySessionState::Terminated(status) => Some(status.clone()),
        }
    }

    pub fn set_terminated(&self, status: PtyExitStatus) {
        *self.inner.state.write() = PtySessionState::Terminated(status);
    }
}

impl Drop for PtySessionInner {
    fn drop(&mut self) {
        if matches!(&*self.state.read(), PtySessionState::Running) {
            let _ = self.child.lock().kill();
        }
    }
}

pub type PtyStore = Arc<RwLock<HashMap<PtyId, PtySession>>>;
