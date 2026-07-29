#![no_main]

use libfuzzer_sys::fuzz_target;
use arachne_protocol::{
    DeliveryProfile, DirectProfileConfig, ExtensionFrame, LocalMeshProfileConfig, MessagePayload,
    RecipientCapability, TorMaildropProfileConfig, VersionNegotiation, WireEnvelope, WireLimits,
};

fuzz_target!(|data: &[u8]| {
    let _ = DeliveryProfile::decode(data);
    let _ = DirectProfileConfig::decode(data);
    let _ = ExtensionFrame::decode(data);
    let _ = LocalMeshProfileConfig::decode(data);
    let _ = MessagePayload::decode(data);
    let _ = RecipientCapability::decode(data);
    let _ = TorMaildropProfileConfig::decode(data);
    let _ = VersionNegotiation::decode(data);
    let _ = WireEnvelope::decode(data, WireLimits::REFERENCE);
});
