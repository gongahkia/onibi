use std::fs;
use std::io::Write;
use std::path::{Path, PathBuf};
use std::process::{Child, Command, Stdio};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use ed25519_dalek::SigningKey;
use kelp_pi_agent::{
    apply_scope_set, policy_sha256, sign_envelope, PiEnvelopeKind, PiEnvelopeSender,
    PiWireEnvelope, PolicyPushPayload, PolicyPushTrustEntry, PolicyTrustState, ScopeSetPayload,
    ScopeTarget, ScopeTargetType, UnsignedPiWireEnvelope, CURRENT_POLICY_FILE, REQUIRED_DATA_DIRS,
};
use rand_core::OsRng;
use serde_json::{json, Value};

#[test]
fn policy_pack_rotation_is_persisted_and_audited() {
    let root = temp_root("policy-sync");
    create_layout(&root);
    let mut rng = OsRng;
    let signing_key = SigningKey::generate(&mut rng);

    sync_policy_pack(
        &root,
        &signing_key,
        policy_push_payload(
            "appsec-agent-baseline@smoke-1",
            1,
            json!({ "mode": "enforce" }),
        ),
    );
    let pull1 = policy_pull(&root);
    assert_eq!(pull1.kind, PiEnvelopeKind::PolicyPull);
    assert_eq!(pull1.sender, PiEnvelopeSender::Pi);
    assert_eq!(
        pull1.payload["known_policy_packs"],
        json!(["appsec-agent-baseline@smoke-1"])
    );
    assert_eq!(pull1.payload["trust_epoch"], json!(1));

    sync_policy_pack(
        &root,
        &signing_key,
        policy_push_payload(
            "appsec-agent-baseline@smoke-2",
            2,
            json!({ "mode": "dry-run" }),
        ),
    );
    let pull2 = policy_pull(&root);
    assert_eq!(
        pull2.payload["known_policy_packs"],
        json!(["appsec-agent-baseline@smoke-2"])
    );
    assert_eq!(pull2.payload["trust_epoch"], json!(2));

    let stored: Value = serde_json::from_slice(
        &fs::read(root.join("policy").join(CURRENT_POLICY_FILE)).expect("read policy"),
    )
    .expect("policy json");
    assert_eq!(
        stored["payload"]["policy_pack_id"],
        json!("appsec-agent-baseline@smoke-2")
    );
    let audit_log =
        fs::read_to_string(root.join("audit").join("agent.jsonl")).expect("read audit log");
    assert!(audit_log.contains("\"event\":\"policy.push.accepted\""));
    assert!(audit_log.contains("\"event\":\"policy.pull.requested\""));
    assert!(audit_log.contains("appsec-agent-baseline@smoke-2"));

    fs::remove_dir_all(root).ok();
}

#[test]
fn daemon_policy_pull_applies_rotated_pack_on_interval() {
    let root = temp_root("policy-daemon");
    create_layout(&root);
    let mut rng = OsRng;
    let signing_key = SigningKey::generate(&mut rng);
    let policy_push_file = root.join("cp-policy-push.json");
    fs::write(
        &policy_push_file,
        serde_json::to_vec(&signed_policy_push(
            &signing_key,
            policy_push_payload(
                "appsec-agent-baseline@daemon-1",
                1,
                json!({ "mode": "enforce" }),
            ),
        ))
        .expect("policy push json"),
    )
    .expect("write policy push");

    let public_key_hex = encode_hex(signing_key.verifying_key().as_bytes());
    let mut child = Command::new(env!("CARGO_BIN_EXE_kelp-pi-agent"))
        .args([
            "start",
            "--data-dir",
            root.to_str().expect("temp path utf8"),
            "--policy-push-file",
            policy_push_file.to_str().expect("push path utf8"),
            "--trusted-cp-public-key-hex",
            public_key_hex.as_str(),
            "--policy-pull-interval-seconds",
            "1",
        ])
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .expect("spawn daemon");

    wait_for_policy_pack(&root, "appsec-agent-baseline@daemon-1");
    fs::write(
        &policy_push_file,
        serde_json::to_vec(&signed_policy_push(
            &signing_key,
            policy_push_payload(
                "appsec-agent-baseline@daemon-2",
                2,
                json!({ "mode": "dry-run" }),
            ),
        ))
        .expect("policy push json"),
    )
    .expect("write rotated policy push");
    wait_for_policy_pack(&root, "appsec-agent-baseline@daemon-2");
    terminate_child(&mut child);

    let audit_log =
        fs::read_to_string(root.join("audit").join("agent.jsonl")).expect("read audit log");
    assert!(audit_log.contains("\"event\":\"policy.pull.requested\""));
    assert!(audit_log.contains("\"event\":\"policy.push.accepted\""));
    assert!(audit_log.contains("appsec-agent-baseline@daemon-2"));

    fs::remove_dir_all(root).ok();
}

#[test]
fn offline_policy_source_keeps_last_policy_and_scope() {
    let root = temp_root("policy-offline");
    create_layout(&root);
    let mut rng = OsRng;
    let signing_key = SigningKey::generate(&mut rng);
    apply_test_scope(&root, &signing_key);
    let policy_push_file = root.join("cp-policy-push.json");
    fs::write(
        &policy_push_file,
        serde_json::to_vec(&signed_policy_push(
            &signing_key,
            policy_push_payload(
                "appsec-agent-baseline@offline-1",
                1,
                json!({ "mode": "enforce" }),
            ),
        ))
        .expect("policy push json"),
    )
    .expect("write policy push");

    let public_key_hex = encode_hex(signing_key.verifying_key().as_bytes());
    let mut child = Command::new(env!("CARGO_BIN_EXE_kelp-pi-agent"))
        .args([
            "start",
            "--data-dir",
            root.to_str().expect("temp path utf8"),
            "--policy-push-file",
            policy_push_file.to_str().expect("push path utf8"),
            "--trusted-cp-public-key-hex",
            public_key_hex.as_str(),
            "--policy-pull-interval-seconds",
            "1",
        ])
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .expect("spawn daemon");

    wait_for_policy_pack(&root, "appsec-agent-baseline@offline-1");
    fs::remove_file(&policy_push_file).expect("remove policy source");
    wait_for_audit_contains(&root, "\"event\":\"policy.pull.failed\"");
    terminate_child(&mut child);
    assert_current_policy_pack(&root, "appsec-agent-baseline@offline-1");

    let policy_refused = Command::new(env!("CARGO_BIN_EXE_kelp-pi-agent"))
        .args([
            "scan",
            "nuclei",
            "--data-dir",
            root.to_str().expect("temp path utf8"),
            "--target",
            "allowed.example.test",
            "--dry-run",
        ])
        .output()
        .expect("run policy-refused scan");
    assert_eq!(policy_refused.status.code(), Some(77));
    assert!(String::from_utf8_lossy(&policy_refused.stderr).contains("requires operator approval"));

    let scope_refused = Command::new(env!("CARGO_BIN_EXE_kelp-pi-agent"))
        .args([
            "scan",
            "nuclei",
            "--data-dir",
            root.to_str().expect("temp path utf8"),
            "--target",
            "evil.example.test",
            "--dry-run",
        ])
        .output()
        .expect("run scope-refused scan");
    assert_eq!(scope_refused.status.code(), Some(77));
    assert!(String::from_utf8_lossy(&scope_refused.stderr).contains("scan refused by scope"));

    fs::remove_dir_all(root).ok();
}

fn sync_policy_pack(root: &Path, signing_key: &SigningKey, payload: PolicyPushPayload) {
    let envelope = signed_policy_push(signing_key, payload);
    let public_key_hex = encode_hex(signing_key.verifying_key().as_bytes());
    let mut child = Command::new(env!("CARGO_BIN_EXE_kelp-pi-agent"))
        .args([
            "wire",
            "--stdio",
            "--trusted-cp-public-key-hex",
            public_key_hex.as_str(),
            "--data-dir",
            root.to_str().expect("temp path utf8"),
        ])
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .expect("spawn wire");
    child
        .stdin
        .as_mut()
        .expect("wire stdin")
        .write_all(format!("{}\n", serde_json::to_string(&envelope).expect("envelope")).as_bytes())
        .expect("write wire stdin");
    let output = child.wait_with_output().expect("wire output");
    assert!(
        output.status.success(),
        "wire failed\nstdout:\n{}\nstderr:\n{}",
        String::from_utf8_lossy(&output.stdout),
        String::from_utf8_lossy(&output.stderr)
    );
    let response: PiWireEnvelope =
        serde_json::from_slice(output.stdout.as_slice()).expect("wire response");
    assert_eq!(response.kind, PiEnvelopeKind::PolicyPush);
    assert_eq!(response.sender, PiEnvelopeSender::Pi);
}

fn policy_pull(root: &Path) -> PiWireEnvelope {
    let output = Command::new(env!("CARGO_BIN_EXE_kelp-pi-agent"))
        .args([
            "policy",
            "pull",
            "--data-dir",
            root.to_str().expect("temp path utf8"),
            "--device-id",
            "pi-field-01",
        ])
        .output()
        .expect("run policy pull");
    assert!(
        output.status.success(),
        "policy pull failed\nstdout:\n{}\nstderr:\n{}",
        String::from_utf8_lossy(&output.stdout),
        String::from_utf8_lossy(&output.stderr)
    );
    serde_json::from_slice(output.stdout.as_slice()).expect("policy pull envelope")
}

fn apply_test_scope(root: &Path, signing_key: &SigningKey) {
    let payload = ScopeSetPayload {
        scope_id: "scope-offline".to_string(),
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
            msg_id: "scope-offline-1".to_string(),
            ts: "2026-06-19T00:00:00Z".to_string(),
            sender: PiEnvelopeSender::Cp,
            kind: PiEnvelopeKind::ScopeSet,
            payload: serde_json::to_value(payload)
                .expect("scope payload json")
                .as_object()
                .expect("scope payload object")
                .clone(),
        },
        signing_key,
    )
    .expect("sign scope");
    apply_scope_set(root, &envelope, &signing_key.verifying_key()).expect("apply scope");
}

fn policy_push_payload(policy_pack_id: &str, trust_epoch: u64, policy: Value) -> PolicyPushPayload {
    PolicyPushPayload {
        policy_pack_id: policy_pack_id.to_string(),
        policy_hash: policy_sha256(&policy).expect("policy hash"),
        trust_epoch,
        issued_at: "2026-06-19T06:00:00Z".to_string(),
        trust_list: vec![PolicyPushTrustEntry {
            key_id: "sha256:0000000000000000000000000000000000000000000000000000000000000000"
                .to_string(),
            device_id: "pi-field-01".to_string(),
            state: PolicyTrustState::Trusted,
        }],
        policy: Some(policy),
    }
}

fn signed_policy_push(signing_key: &SigningKey, payload: PolicyPushPayload) -> PiWireEnvelope {
    let payload = serde_json::to_value(payload)
        .expect("payload value")
        .as_object()
        .expect("payload object")
        .clone();
    sign_envelope(
        UnsignedPiWireEnvelope {
            msg_id: format!("msg-policy-push-{}", unix_nanos()),
            ts: "2026-06-19T06:00:01Z".to_string(),
            sender: PiEnvelopeSender::Cp,
            kind: PiEnvelopeKind::PolicyPush,
            payload,
        },
        signing_key,
    )
    .expect("sign policy push")
}

fn create_layout(root: &Path) {
    fs::create_dir_all(root).expect("create root");
    for name in REQUIRED_DATA_DIRS {
        fs::create_dir_all(root.join(name)).expect("create child");
    }
}

fn wait_for_policy_pack(root: &Path, expected: &str) {
    let path = root.join("policy").join(CURRENT_POLICY_FILE);
    for _ in 0..80 {
        if let Ok(bytes) = fs::read(&path) {
            let stored: Value = serde_json::from_slice(&bytes).expect("policy json");
            if stored["payload"]["policy_pack_id"] == json!(expected) {
                return;
            }
        }
        std::thread::sleep(Duration::from_millis(100));
    }
    panic!("policy pack {expected} was not installed");
}

fn wait_for_audit_contains(root: &Path, expected: &str) {
    let path = root.join("audit").join("agent.jsonl");
    for _ in 0..80 {
        if fs::read_to_string(&path)
            .map(|content| content.contains(expected))
            .unwrap_or(false)
        {
            return;
        }
        std::thread::sleep(Duration::from_millis(100));
    }
    panic!("audit log did not contain {expected}");
}

fn assert_current_policy_pack(root: &Path, expected: &str) {
    let path = root.join("policy").join(CURRENT_POLICY_FILE);
    let stored: Value =
        serde_json::from_slice(&fs::read(path).expect("read policy")).expect("policy json");
    assert_eq!(stored["payload"]["policy_pack_id"], json!(expected));
}

fn terminate_child(child: &mut Child) {
    #[cfg(unix)]
    {
        let pid = child.id().to_string();
        let _ = Command::new("kill").args(["-TERM", pid.as_str()]).status();
    }
    #[cfg(not(unix))]
    {
        let _ = child.kill();
    }
    let _ = child.wait();
}

fn temp_root(name: &str) -> PathBuf {
    std::env::temp_dir().join(format!(
        "kelp-pi-{name}-{}-{}",
        std::process::id(),
        unix_nanos()
    ))
}

fn unix_nanos() -> u128 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .expect("time after epoch")
        .as_nanos()
}

fn encode_hex(bytes: impl AsRef<[u8]>) -> String {
    const HEX: &[u8; 16] = b"0123456789abcdef";
    let bytes = bytes.as_ref();
    let mut encoded = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        encoded.push(HEX[(byte >> 4) as usize] as char);
        encoded.push(HEX[(byte & 0x0f) as usize] as char);
    }
    encoded
}
