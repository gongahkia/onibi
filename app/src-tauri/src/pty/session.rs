use bytes::Bytes;
use parking_lot::{Mutex, RwLock};
use portable_pty::{Child, MasterPty};
use serde::{Deserialize, Serialize};
use std::{
    collections::{HashMap, VecDeque},
    io::Write,
    path::PathBuf,
    sync::Arc,
};
use thiserror::Error;
use tokio::sync::broadcast;
use uuid::Uuid;

pub type PtyId = Uuid;

const OUTPUT_REPLAY_LIMIT: usize = 1024 * 1024;

#[derive(Debug, Clone, Copy, Deserialize, Serialize, PartialEq, Eq, Default)]
#[serde(rename_all = "snake_case")]
pub enum ShellMode {
    #[default]
    Auto,
    Login,
    NonLogin,
}

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

#[derive(Debug, Clone, Deserialize, Serialize)]
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
    #[serde(default)]
    pub shell_mode: ShellMode,
    #[serde(default = "default_rows")]
    pub rows: u16,
    #[serde(default = "default_cols")]
    pub cols: u16,
    #[serde(default)]
    pub name: Option<String>,
    #[serde(default)]
    pub agent: Option<String>,
    #[serde(default)]
    pub workspace_id: Option<String>,
    #[serde(default)]
    pub title: Option<String>,
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
    Data { bytes: Bytes, offset: u64 },
    Exit(PtyExitStatus),
    Notification(super::notifications::OscNotification),
}

#[derive(Debug, Clone)]
pub struct PtyOutputSnapshot {
    pub data: Bytes,
    pub start_offset: u64,
    pub end_offset: u64,
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
    child_process_id: Option<u32>,
    master: Arc<Mutex<Box<dyn MasterPty + Send>>>,
    child: Arc<Mutex<Box<dyn Child + Send + Sync>>>,
    writer: Arc<tokio::sync::Mutex<Box<dyn Write + Send>>>,
    tx: broadcast::Sender<PtyEvent>,
    state: Arc<RwLock<PtySessionState>>,
    output: Arc<Mutex<PtyOutputBuffer>>,
}

#[derive(Default)]
struct PtyOutputBuffer {
    bytes: VecDeque<u8>,
    end_offset: u64,
}

impl PtyOutputBuffer {
    fn append(&mut self, bytes: &[u8]) -> u64 {
        let start_offset = self.end_offset;
        if bytes.is_empty() {
            return start_offset;
        }
        self.end_offset = self.end_offset.saturating_add(bytes.len() as u64);
        if bytes.len() >= OUTPUT_REPLAY_LIMIT {
            self.bytes.clear();
            self.bytes
                .extend(bytes[bytes.len() - OUTPUT_REPLAY_LIMIT..].iter().copied());
            return start_offset;
        }
        let overflow = self.bytes.len() + bytes.len();
        if overflow > OUTPUT_REPLAY_LIMIT {
            for _ in 0..overflow - OUTPUT_REPLAY_LIMIT {
                self.bytes.pop_front();
            }
        }
        self.bytes.extend(bytes.iter().copied());
        start_offset
    }

    fn snapshot(&self) -> PtyOutputSnapshot {
        let start_offset = self.end_offset.saturating_sub(self.bytes.len() as u64);
        let (front, back) = self.bytes.as_slices();
        let data = if back.is_empty() {
            Bytes::copy_from_slice(front)
        } else {
            let mut snapshot = Vec::with_capacity(self.bytes.len());
            snapshot.extend_from_slice(front);
            snapshot.extend_from_slice(back);
            Bytes::from(snapshot)
        };
        PtyOutputSnapshot {
            data,
            start_offset,
            end_offset: self.end_offset,
        }
    }
}

impl PtySession {
    pub fn new(
        id: PtyId,
        master: Box<dyn MasterPty + Send>,
        child: Box<dyn Child + Send + Sync>,
        writer: Box<dyn Write + Send>,
        tx: broadcast::Sender<PtyEvent>,
    ) -> Self {
        let child_process_id = child.process_id();
        Self {
            inner: Arc::new(PtySessionInner {
                id,
                child_process_id,
                master: Arc::new(Mutex::new(master)),
                child: Arc::new(Mutex::new(child)),
                writer: Arc::new(tokio::sync::Mutex::new(writer)),
                tx,
                state: Arc::new(RwLock::new(PtySessionState::Running)),
                output: Arc::new(Mutex::new(PtyOutputBuffer::default())),
            }),
        }
    }

    pub fn id(&self) -> PtyId {
        self.inner.id
    }

    pub fn child(&self) -> Arc<Mutex<Box<dyn Child + Send + Sync>>> {
        self.inner.child.clone()
    }

    pub fn child_process_id(&self) -> Option<u32> {
        self.inner.child_process_id
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

    pub fn append_output(&self, bytes: &[u8]) -> u64 {
        let mut output = self.inner.output.lock();
        output.append(bytes)
    }

    pub fn output_snapshot(&self) -> PtyOutputSnapshot {
        let output = self.inner.output.lock();
        output.snapshot()
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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn output_buffer_tracks_offsets_without_truncation() {
        let mut output = PtyOutputBuffer::default();

        assert_eq!(output.append(b"hello"), 0);
        assert_eq!(output.append(b" world"), 5);

        let snapshot = output.snapshot();
        assert_eq!(snapshot.start_offset, 0);
        assert_eq!(snapshot.end_offset, 11);
        assert_eq!(&snapshot.data[..], b"hello world");
    }

    #[test]
    fn output_buffer_replays_latest_bytes_after_large_truncation() {
        let mut output = PtyOutputBuffer::default();
        assert_eq!(output.append(b"prefix"), 0);
        let large = vec![b'x'; OUTPUT_REPLAY_LIMIT + 8];

        assert_eq!(output.append(&large), 6);

        let snapshot = output.snapshot();
        assert_eq!(snapshot.data.len(), OUTPUT_REPLAY_LIMIT);
        assert_eq!(snapshot.end_offset, (OUTPUT_REPLAY_LIMIT + 14) as u64);
        assert_eq!(
            snapshot.start_offset,
            snapshot.end_offset - OUTPUT_REPLAY_LIMIT as u64,
        );
        assert!(snapshot.data.iter().all(|byte| *byte == b'x'));
    }

    #[test]
    fn output_buffer_replays_latest_bytes_after_incremental_overflow() {
        let mut output = PtyOutputBuffer::default();
        let first = vec![b'a'; OUTPUT_REPLAY_LIMIT - 2];

        assert_eq!(output.append(&first), 0);
        assert_eq!(output.append(b"bcdef"), (OUTPUT_REPLAY_LIMIT - 2) as u64);

        let snapshot = output.snapshot();
        assert_eq!(snapshot.data.len(), OUTPUT_REPLAY_LIMIT);
        assert_eq!(snapshot.start_offset, 3);
        assert_eq!(snapshot.end_offset, (OUTPUT_REPLAY_LIMIT + 3) as u64);
        assert!(snapshot.data.ends_with(b"bcdef"));
    }
}
