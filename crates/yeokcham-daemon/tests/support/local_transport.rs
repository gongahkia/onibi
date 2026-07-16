use std::{fmt::Debug, future::Future};

use tokio::sync::{Mutex, mpsc};
use yeokcham_daemon::{BluetoothTransport, LocalTransport};
use yeokcham_protocol::{
    EnvelopeKind, LocalMeshTransportKind, ProtocolVersion, WireEnvelope, WireLimits,
};

const MAX_PENDING_FRAMES: usize = 2;

#[derive(Debug, Eq, PartialEq)]
pub enum InMemoryTransportError {
    Disconnected,
    InvalidFrame,
}

pub struct InMemoryTransport {
    incoming: Mutex<mpsc::Receiver<Vec<u8>>>,
    kind: LocalMeshTransportKind,
    outgoing: mpsc::Sender<Vec<u8>>,
}

pub struct InMemoryBluetoothTransport(InMemoryTransport);

impl InMemoryBluetoothTransport {
    pub fn pair() -> (Self, Self) {
        let (first, second) = InMemoryTransport::pair(LocalMeshTransportKind::Bluetooth);
        (Self(first), Self(second))
    }

    pub async fn inject(&self, encoded: Vec<u8>) -> Result<(), InMemoryTransportError> {
        self.0.inject(encoded).await
    }

    pub async fn pending_frames(&self) -> usize {
        self.0.pending_frames().await
    }
}

impl InMemoryTransport {
    pub fn pair(kind: LocalMeshTransportKind) -> (Self, Self) {
        let (a_to_b_sender, a_to_b_receiver) = mpsc::channel(MAX_PENDING_FRAMES);
        let (b_to_a_sender, b_to_a_receiver) = mpsc::channel(MAX_PENDING_FRAMES);
        (
            Self {
                incoming: Mutex::new(b_to_a_receiver),
                kind,
                outgoing: a_to_b_sender,
            },
            Self {
                incoming: Mutex::new(a_to_b_receiver),
                kind,
                outgoing: b_to_a_sender,
            },
        )
    }

    pub async fn inject(&self, encoded: Vec<u8>) -> Result<(), InMemoryTransportError> {
        self.outgoing
            .send(encoded)
            .await
            .map_err(|_| InMemoryTransportError::Disconnected)
    }

    pub async fn pending_frames(&self) -> usize {
        self.incoming.lock().await.len()
    }
}

impl LocalTransport for InMemoryTransport {
    type Error = InMemoryTransportError;

    fn transport_kind(&self) -> LocalMeshTransportKind {
        self.kind
    }

    #[allow(clippy::manual_async_fn)]
    fn send_frame(
        &self,
        frame: &WireEnvelope,
        limits: WireLimits,
    ) -> impl Future<Output = Result<(), Self::Error>> + Send {
        async move {
            let encoded = frame
                .encode(limits)
                .map_err(|_| InMemoryTransportError::InvalidFrame)?;
            self.outgoing
                .send(encoded)
                .await
                .map_err(|_| InMemoryTransportError::Disconnected)
        }
    }

    #[allow(clippy::manual_async_fn)]
    fn receive_frame(
        &self,
        limits: WireLimits,
    ) -> impl Future<Output = Result<WireEnvelope, Self::Error>> + Send {
        async move {
            let encoded = self
                .incoming
                .lock()
                .await
                .recv()
                .await
                .ok_or(InMemoryTransportError::Disconnected)?;
            WireEnvelope::decode(&encoded, limits).map_err(|_| InMemoryTransportError::InvalidFrame)
        }
    }
}

impl LocalTransport for InMemoryBluetoothTransport {
    type Error = InMemoryTransportError;

    fn transport_kind(&self) -> LocalMeshTransportKind {
        self.0.transport_kind()
    }

    fn send_frame(
        &self,
        frame: &WireEnvelope,
        limits: WireLimits,
    ) -> impl Future<Output = Result<(), Self::Error>> + Send {
        self.0.send_frame(frame, limits)
    }

    fn receive_frame(
        &self,
        limits: WireLimits,
    ) -> impl Future<Output = Result<WireEnvelope, Self::Error>> + Send {
        self.0.receive_frame(limits)
    }
}

impl BluetoothTransport for InMemoryBluetoothTransport {}

pub async fn assert_transport_conformance<T>(sender: &T, receiver: &T, kind: LocalMeshTransportKind)
where
    T: LocalTransport,
    T::Error: Debug,
{
    assert_eq!(sender.transport_kind(), kind);
    assert_eq!(receiver.transport_kind(), kind);
    let from_sender = frame(vec![0xa1, 0xb2]);
    sender
        .send_frame(&from_sender, WireLimits::REFERENCE)
        .await
        .unwrap();
    assert_eq!(
        receiver.receive_frame(WireLimits::REFERENCE).await.unwrap(),
        from_sender
    );
    let from_receiver = frame(vec![0xc3, 0xd4]);
    receiver
        .send_frame(&from_receiver, WireLimits::REFERENCE)
        .await
        .unwrap();
    assert_eq!(
        sender.receive_frame(WireLimits::REFERENCE).await.unwrap(),
        from_receiver
    );
}

fn frame(payload: Vec<u8>) -> WireEnvelope {
    WireEnvelope {
        version: ProtocolVersion::INITIAL,
        kind: EnvelopeKind::EncryptedMessage,
        payload,
    }
}
