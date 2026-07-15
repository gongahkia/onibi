use yeokcham_protocol::{
    DirectProfileConfig, DirectProfileConfigError, MailboxCapability, MailboxCapabilityError,
    WireEnvelope, WireError, WireLimits,
};

const VECTORS: &str = include_str!("../vectors/security-regression-v1.txt");

#[test]
fn security_regression_vectors_fail_closed() {
    assert_eq!(
        WireEnvelope::decode(&vector("wire_trailing_data"), WireLimits::REFERENCE),
        Err(WireError::TrailingBytes),
    );
    assert_eq!(
        WireEnvelope::decode(&vector("wire_nested_payload"), WireLimits::REFERENCE),
        Err(WireError::NestingTooDeep),
    );
    assert_eq!(
        DirectProfileConfig::decode(&vector("direct_profile_unspecified")),
        Err(DirectProfileConfigError::UnspecifiedAddress),
    );
    assert_eq!(
        DirectProfileConfig::decode(&vector("direct_profile_broadcast")),
        Err(DirectProfileConfigError::BroadcastAddress),
    );
    assert_eq!(
        MailboxCapability::decode(&vector("mailbox_capability_zero_token")).unwrap_err(),
        MailboxCapabilityError::InvalidToken,
    );
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
    assert_eq!(value.len() % 2, 0, "security vector has odd hex length");
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
        _ => panic!("security vector contains invalid lowercase hexadecimal"),
    }
}
