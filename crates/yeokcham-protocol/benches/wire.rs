use std::hint::black_box;

use criterion::{BenchmarkId, Criterion, Throughput, criterion_group, criterion_main};
use yeokcham_protocol::{EnvelopeKind, ProtocolVersion, WireEnvelope, WireLimits};

const PAYLOAD_SIZES: [usize; 2] = [1_024, 65_536];

fn benchmark_wire(c: &mut Criterion) {
    let mut group = c.benchmark_group("wire");
    for payload_bytes in PAYLOAD_SIZES {
        let envelope = WireEnvelope {
            version: ProtocolVersion::INITIAL,
            kind: EnvelopeKind::EncryptedMessage,
            payload: vec![0xa5; payload_bytes],
        };
        let encoded = envelope
            .encode(WireLimits::REFERENCE)
            .expect("benchmark envelope must encode");
        group.throughput(Throughput::Bytes(u64::try_from(payload_bytes).unwrap()));
        group.bench_with_input(
            BenchmarkId::new("encode", payload_bytes),
            &envelope,
            |bench, envelope| {
                bench.iter(|| {
                    black_box(
                        black_box(envelope)
                            .encode(WireLimits::REFERENCE)
                            .expect("benchmark envelope must encode"),
                    );
                });
            },
        );
        group.bench_with_input(
            BenchmarkId::new("decode", payload_bytes),
            &encoded,
            |bench, encoded| {
                bench.iter(|| {
                    black_box(
                        WireEnvelope::decode(black_box(encoded), WireLimits::REFERENCE)
                            .expect("benchmark frame must decode"),
                    );
                });
            },
        );
    }
    group.finish();
}

criterion_group!(benches, benchmark_wire);
criterion_main!(benches);
