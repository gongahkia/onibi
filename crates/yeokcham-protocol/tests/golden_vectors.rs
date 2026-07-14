use yeokcham_protocol::{
    DeliveryProfile, DeliveryProfileKind, DirectProfileConfig, EncryptedHeader,
    EncryptedMessageEnvelope, ExtensionFrame, IdentityIdentifier, LocalMeshProfileConfig,
    LocalMeshTransportKind, MAILBOX_CAPABILITY_TOKEN_BYTES, MAILBOX_IDENTIFIER_BYTES,
    MailboxCapability, MessageContentType, MessagePayload, ProtocolVersion, QrVerificationPayload,
    RecipientCapability, TOR_ONION_SERVICE_PUBLIC_KEY_BYTES, TorMaildropProfileConfig,
    VersionNegotiation, VersionRange, WireEnvelope, WireLimits,
};

const VECTORS: &str = include_str!("../vectors/protocol-v1.txt");

macro_rules! assert_vector {
    ($name:expr, $actual:expr $(,)?) => {
        assert_eq!($actual, vector($name), "golden vector mismatch: {}", $name);
    };
}

#[test]
fn protocol_v1_vectors_match_public_encoders() {
    let range = version_range(1, 3);
    assert_vector!(
        "version_offer",
        VersionNegotiation::offer(range).encode().unwrap(),
    );
    assert_vector!(
        "version_accept",
        VersionNegotiation::accept(version_range(2, 4), VersionNegotiation::offer(range))
            .unwrap()
            .encode()
            .unwrap(),
    );
    assert_vector!(
        "version_reject",
        VersionNegotiation::respond(
            version_range(1, 1),
            VersionNegotiation::offer(version_range(2, 3)),
        )
        .unwrap()
        .encode()
        .unwrap(),
    );
    assert_vector!(
        "wire_envelope",
        WireEnvelope {
            version: ProtocolVersion::INITIAL,
            kind: yeokcham_protocol::EnvelopeKind::EncryptedMessage,
            payload: vec![1, 2, 3],
        }
        .encode(WireLimits::REFERENCE)
        .unwrap(),
    );
    assert_vector!(
        "encrypted_message",
        EncryptedMessageEnvelope::new(vec![0xa1], vec![0xb2, 0xc3])
            .unwrap()
            .encode()
            .unwrap(),
    );
    assert_vector!(
        "message_payload_text",
        MessagePayload::new(MessageContentType::TextUtf8, b"hi".to_vec())
            .unwrap()
            .encode()
            .unwrap(),
    );
    assert_vector!(
        "extension_frame",
        ExtensionFrame::new(42, vec![1, 2])
            .unwrap()
            .encode()
            .unwrap(),
    );
    assert_vector!(
        "delivery_profile_direct",
        DeliveryProfile::new(DeliveryProfileKind::Direct)
            .encode()
            .unwrap(),
    );
    assert_vector!("direct_profile", direct_profile().encode().unwrap());
    assert_vector!(
        "tor_maildrop_profile",
        tor_maildrop_profile().encode().unwrap(),
    );
    assert_vector!(
        "local_mesh_profile_bluetooth",
        LocalMeshProfileConfig::new(LocalMeshTransportKind::Bluetooth)
            .encode()
            .unwrap(),
    );
    assert_vector!("mailbox_capability", mailbox_capability().encode().unwrap());
    assert_vector!(
        "recipient_capability_direct",
        RecipientCapability::Direct(direct_profile())
            .encode()
            .unwrap(),
    );
    assert_vector!(
        "encrypted_header_direct",
        EncryptedHeader::new(RecipientCapability::Direct(direct_profile()))
            .encode()
            .unwrap(),
    );
    assert_vector!(
        "identity_identifier",
        IdentityIdentifier::derive(
            &yeokcham_core::IdentityPublicKey::from_bytes([
                0xd7, 0x5a, 0x98, 0x01, 0x82, 0xb1, 0x0a, 0xb7, 0xd5, 0x4b, 0xfe, 0xd3, 0xc9, 0x64,
                0x07, 0x3a, 0x0e, 0xe1, 0x72, 0xf3, 0xda, 0xa6, 0x23, 0x25, 0xaf, 0x02, 0x1a, 0x68,
                0xf7, 0x07, 0x51, 0x1a,
            ])
            .unwrap(),
        )
        .as_bytes()
        .to_vec(),
    );
    assert_vector!(
        "qr_verification",
        qr_verification_payload().encode().unwrap(),
    );
}

fn version_range(minimum: u16, maximum: u16) -> VersionRange {
    VersionRange::new(
        ProtocolVersion::new(minimum).unwrap(),
        ProtocolVersion::new(maximum).unwrap(),
    )
    .unwrap()
}

fn direct_profile() -> DirectProfileConfig {
    DirectProfileConfig::new("192.0.2.1:4444".parse().unwrap()).unwrap()
}

fn tor_maildrop_profile() -> TorMaildropProfileConfig {
    TorMaildropProfileConfig::new([0x11; TOR_ONION_SERVICE_PUBLIC_KEY_BYTES], 4444).unwrap()
}

fn mailbox_capability() -> MailboxCapability {
    MailboxCapability::new(
        [0x22; MAILBOX_IDENTIFIER_BYTES],
        [0x33; MAILBOX_CAPABILITY_TOKEN_BYTES],
    )
    .unwrap()
}

fn qr_verification_payload() -> QrVerificationPayload {
    let first = yeokcham_core::IdentityPublicKey::from_bytes([
        0xd7, 0x5a, 0x98, 0x01, 0x82, 0xb1, 0x0a, 0xb7, 0xd5, 0x4b, 0xfe, 0xd3, 0xc9, 0x64, 0x07,
        0x3a, 0x0e, 0xe1, 0x72, 0xf3, 0xda, 0xa6, 0x23, 0x25, 0xaf, 0x02, 0x1a, 0x68, 0xf7, 0x07,
        0x51, 0x1a,
    ])
    .unwrap();
    let second = yeokcham_core::IdentityPublicKey::from_bytes([
        0x3d, 0x40, 0x17, 0xc3, 0xe8, 0x43, 0x89, 0x5a, 0x92, 0xb7, 0x0a, 0xa7, 0x4d, 0x1b, 0x7e,
        0xbc, 0x9c, 0x98, 0x2c, 0xcf, 0x2e, 0xc4, 0x96, 0x8c, 0xc0, 0xcd, 0x55, 0xf1, 0x2a, 0xf4,
        0x66, 0x0c,
    ])
    .unwrap();
    QrVerificationPayload::new(first, second).unwrap()
}

fn vector(name: &str) -> Vec<u8> {
    let value = VECTORS
        .lines()
        .filter(|line| !line.is_empty() && !line.starts_with('#'))
        .find_map(|line| {
            line.split_once('=')
                .filter(|(candidate, _)| *candidate == name)
        })
        .map(|(_, value)| value)
        .unwrap();
    decode_hex(value)
}

fn decode_hex(value: &str) -> Vec<u8> {
    assert_eq!(value.len() % 2, 0, "golden vector has odd hex length");
    value
        .as_bytes()
        .chunks_exact(2)
        .map(|pair| (hex_nibble(pair[0]) << 4) | hex_nibble(pair[1]))
        .collect()
}

fn hex_nibble(value: u8) -> u8 {
    match value {
        b'0'..=b'9' => value - b'0',
        b'a'..=b'f' => value - b'a' + 10,
        _ => panic!("golden vector contains invalid lowercase hexadecimal"),
    }
}
