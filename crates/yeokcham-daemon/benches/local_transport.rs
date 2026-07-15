use std::{future::Future, hint::black_box};

use criterion::{BenchmarkId, Criterion, criterion_group, criterion_main};
use tokio::sync::{Mutex, mpsc};
use yeokcham_daemon::LocalTransport;
use yeokcham_protocol::{
    EnvelopeKind, LocalMeshTransportKind, ProtocolVersion, WireEnvelope, WireLimits,
};

const MAX_PENDING_FRAMES: usize = 1;
const PAYLOAD_BYTES: usize = 1_024;

#[derive(Debug)]
enum BenchmarkTransportError {
    Disconnected,
    InvalidFrame,
}

struct BenchmarkTransport {
    incoming: Mutex<mpsc::Receiver<Vec<u8>>>,
    kind: LocalMeshTransportKind,
    outgoing: mpsc::Sender<Vec<u8>>,
}

impl BenchmarkTransport {
    fn pair(kind: LocalMeshTransportKind) -> (Self, Self) {
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

    async fn inject(&self, encoded: &[u8]) -> Result<(), BenchmarkTransportError> {
        self.outgoing
            .send(encoded.to_vec())
            .await
            .map_err(|_| BenchmarkTransportError::Disconnected)
    }
}

impl LocalTransport for BenchmarkTransport {
    type Error = BenchmarkTransportError;

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
                .map_err(|_| BenchmarkTransportError::InvalidFrame)?;
            self.outgoing
                .send(encoded)
                .await
                .map_err(|_| BenchmarkTransportError::Disconnected)
        }
    }

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
                .ok_or(BenchmarkTransportError::Disconnected)?;
            WireEnvelope::decode(&encoded, limits)
                .map_err(|_| BenchmarkTransportError::InvalidFrame)
        }
    }
}

fn benchmark_local_transport(c: &mut Criterion) {
    let runtime = tokio::runtime::Builder::new_current_thread()
        .enable_all()
        .build()
        .expect("benchmark runtime must initialize");
    let frame = WireEnvelope {
        version: ProtocolVersion::INITIAL,
        kind: EnvelopeKind::EncryptedMessage,
        payload: vec![0xa5; PAYLOAD_BYTES],
    };
    let malformed = [0x9f];
    let mut group = c.benchmark_group("local_transport");
    for kind in [
        LocalMeshTransportKind::Lan,
        LocalMeshTransportKind::WifiHotspot,
        LocalMeshTransportKind::WifiDirect,
        LocalMeshTransportKind::Bluetooth,
    ] {
        group.bench_with_input(
            BenchmarkId::new("round_trip_1024_bytes", format!("{kind:?}")),
            &kind,
            |bench, &kind| {
                let (sender, receiver) = BenchmarkTransport::pair(kind);
                bench.iter(|| {
                    runtime.block_on(async {
                        sender
                            .send_frame(black_box(&frame), WireLimits::REFERENCE)
                            .await
                            .expect("benchmark frame must send");
                        black_box(
                            receiver
                                .receive_frame(WireLimits::REFERENCE)
                                .await
                                .expect("benchmark frame must receive"),
                        );
                    });
                });
            },
        );
        group.bench_with_input(
            BenchmarkId::new("malformed_frame_rejection", format!("{kind:?}")),
            &kind,
            |bench, &kind| {
                let (sender, receiver) = BenchmarkTransport::pair(kind);
                bench.iter(|| {
                    runtime.block_on(async {
                        sender
                            .inject(black_box(&malformed))
                            .await
                            .expect("benchmark malformed frame must enqueue");
                        black_box(
                            receiver
                                .receive_frame(WireLimits::REFERENCE)
                                .await
                                .expect_err("benchmark malformed frame must reject"),
                        );
                    });
                });
            },
        );
    }
    group.finish();
}

criterion_group!(benches, benchmark_local_transport);
criterion_main!(benches);
