use std::future::Future;

use yeokcham_protocol::{LocalMeshTransportKind, WireEnvelope, WireLimits};

pub trait LocalTransport: Send + Sync + 'static {
    type Error: Send + 'static;

    fn transport_kind(&self) -> LocalMeshTransportKind;

    fn send_frame(
        &self,
        frame: &WireEnvelope,
        limits: WireLimits,
    ) -> impl Future<Output = Result<(), Self::Error>> + Send;

    fn receive_frame(
        &self,
        limits: WireLimits,
    ) -> impl Future<Output = Result<WireEnvelope, Self::Error>> + Send;
}

#[cfg(test)]
mod tests {
    use std::collections::VecDeque;

    use tokio::sync::Mutex;
    use yeokcham_protocol::{EnvelopeKind, ProtocolVersion, WireEnvelope, WireLimits};

    use super::{LocalMeshTransportKind, LocalTransport};

    #[derive(Debug, Eq, PartialEq)]
    enum InMemoryTransportError {
        Empty,
        InvalidFrame,
    }

    struct InMemoryTransport {
        frames: Mutex<VecDeque<Vec<u8>>>,
        kind: LocalMeshTransportKind,
    }

    impl InMemoryTransport {
        const fn new(kind: LocalMeshTransportKind) -> Self {
            Self {
                frames: Mutex::const_new(VecDeque::new()),
                kind,
            }
        }
    }

    impl LocalTransport for InMemoryTransport {
        type Error = InMemoryTransportError;

        fn transport_kind(&self) -> LocalMeshTransportKind {
            self.kind
        }

        fn send_frame(
            &self,
            frame: &WireEnvelope,
            limits: WireLimits,
        ) -> impl Future<Output = Result<(), Self::Error>> + Send {
            async move {
                let encoded = frame
                    .encode(limits)
                    .map_err(|_| InMemoryTransportError::InvalidFrame)?;
                self.frames.lock().await.push_back(encoded);
                Ok(())
            }
        }

        fn receive_frame(
            &self,
            limits: WireLimits,
        ) -> impl Future<Output = Result<WireEnvelope, Self::Error>> + Send {
            async move {
                let encoded = self
                    .frames
                    .lock()
                    .await
                    .pop_front()
                    .ok_or(InMemoryTransportError::Empty)?;
                WireEnvelope::decode(&encoded, limits)
                    .map_err(|_| InMemoryTransportError::InvalidFrame)
            }
        }
    }

    fn frame(payload: Vec<u8>) -> WireEnvelope {
        WireEnvelope {
            version: ProtocolVersion::INITIAL,
            kind: EnvelopeKind::EncryptedMessage,
            payload,
        }
    }

    #[tokio::test]
    async fn local_transport_preserves_kind_and_validated_frames() {
        let transport = InMemoryTransport::new(LocalMeshTransportKind::Lan);
        let expected = frame(vec![0xa1, 0xb2]);
        assert_eq!(transport.transport_kind(), LocalMeshTransportKind::Lan);
        transport
            .send_frame(&expected, WireLimits::REFERENCE)
            .await
            .unwrap();
        assert_eq!(
            transport
                .receive_frame(WireLimits::REFERENCE)
                .await
                .unwrap(),
            expected
        );
    }

    #[tokio::test]
    async fn local_transport_rejects_oversized_frames_before_queueing() {
        let transport = InMemoryTransport::new(LocalMeshTransportKind::Bluetooth);
        let limits = WireLimits::new(64, 1).unwrap();
        assert_eq!(
            transport.send_frame(&frame(vec![0xa1, 0xb2]), limits).await,
            Err(InMemoryTransportError::InvalidFrame)
        );
        assert_eq!(
            transport.receive_frame(WireLimits::REFERENCE).await,
            Err(InMemoryTransportError::Empty)
        );
    }
}
