use std::hint::black_box;

use criterion::{Criterion, criterion_group, criterion_main};
use yeokcham_protocol::{EnvelopeKind, ProtocolVersion, WireEnvelope, WireLimits};

const PAYLOAD_BYTES: usize = 1_024;

fn benchmark_wire(c: &mut Criterion) {
    let envelope = WireEnvelope {
        version: ProtocolVersion::INITIAL,
        kind: EnvelopeKind::EncryptedMessage,
        payload: vec![0xa5; PAYLOAD_BYTES],
    };
    let encoded = envelope
        .encode(WireLimits::REFERENCE)
        .expect("benchmark envelope must encode");
    let mut group = c.benchmark_group("wire");

    group.bench_function("encode_1024_bytes", |bench| {
        bench.iter(|| {
            black_box(
                black_box(&envelope)
                    .encode(WireLimits::REFERENCE)
                    .expect("benchmark envelope must encode"),
            );
        });
    });
    group.bench_function("decode_1024_bytes", |bench| {
        bench.iter(|| {
            black_box(
                WireEnvelope::decode(black_box(&encoded), WireLimits::REFERENCE)
                    .expect("benchmark frame must decode"),
            );
        });
    });
    group.finish();
}

criterion_group!(benches, benchmark_wire);
criterion_main!(benches);
