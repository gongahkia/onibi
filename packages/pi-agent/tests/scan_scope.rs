use std::fs;
use std::path::{Path, PathBuf};
use std::process::Command;
use std::time::{SystemTime, UNIX_EPOCH};

use ed25519_dalek::SigningKey;
use kelp_pi_agent::{
    apply_scope_set, sign_envelope, PiEnvelopeKind, PiEnvelopeSender, ScopeSetPayload, ScopeTarget,
    ScopeTargetType, UnsignedPiWireEnvelope, REQUIRED_DATA_DIRS,
};
use rand_core::OsRng;
use serde_json::Value;

#[test]
fn out_of_scope_scan_is_blocked_and_audited() {
    let root = temp_root("out-of-scope-scan");
    create_layout(&root);
    apply_test_scope(&root);

    let output = Command::new(env!("CARGO_BIN_EXE_kelp-pi-agent"))
        .args([
            "scan",
            "nmap",
            "--data-dir",
            root.to_str().expect("temp path utf8"),
            "--target",
            "https://evil.example.test",
        ])
        .output()
        .expect("run scan command");

    assert_eq!(output.status.code(), Some(77));
    let stderr = String::from_utf8_lossy(&output.stderr);
    assert!(stderr.contains("scan refused by scope"), "{stderr}");

    let audit_log =
        fs::read_to_string(root.join("audit").join("agent.jsonl")).expect("read audit log");
    assert!(audit_log.contains("\"event\":\"scope.violation\""));
    assert!(audit_log.contains("https://evil.example.test"));

    fs::remove_dir_all(root).ok();
}

fn apply_test_scope(root: &Path) {
    let mut rng = OsRng;
    let signing_key = SigningKey::generate(&mut rng);
    let payload = ScopeSetPayload {
        scope_id: "scope-a".to_string(),
        issued_at: "2026-06-19T00:00:00Z".to_string(),
        valid_from: "2020-01-01T00:00:00Z".to_string(),
        valid_until: "2100-01-01T00:00:00Z".to_string(),
        targets: vec![ScopeTarget {
            target_type: ScopeTargetType::Host,
            value: "allowed.example.test".to_string(),
            ports: None,
        }],
    };
    let envelope = sign_envelope(
        UnsignedPiWireEnvelope {
            msg_id: "scope-msg-1".to_string(),
            ts: "2026-06-19T00:00:00Z".to_string(),
            sender: PiEnvelopeSender::Cp,
            kind: PiEnvelopeKind::ScopeSet,
            payload: serde_json::to_value(payload)
                .expect("scope payload json")
                .as_object()
                .expect("scope payload object")
                .iter()
                .map(|(key, value)| (key.clone(), value.clone()))
                .collect::<serde_json::Map<String, Value>>(),
        },
        &signing_key,
    )
    .expect("sign scope");
    apply_scope_set(root, &envelope, &signing_key.verifying_key()).expect("apply scope");
}

fn create_layout(root: &Path) {
    fs::create_dir_all(root).expect("create root");
    for name in REQUIRED_DATA_DIRS {
        fs::create_dir_all(root.join(name)).expect("create child");
    }
}

fn temp_root(name: &str) -> PathBuf {
    let nonce = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .expect("time after epoch")
        .as_nanos();
    std::env::temp_dir().join(format!("kelp-pi-{name}-{}-{nonce}", std::process::id()))
}
