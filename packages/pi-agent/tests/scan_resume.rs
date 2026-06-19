#![cfg(unix)]

use std::fs;
use std::io::Write;
use std::os::unix::fs::PermissionsExt;
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};
use std::thread;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use ed25519_dalek::{SigningKey, VerifyingKey};
use kelp_pi_agent::{
    apply_scope_set, sign_envelope, PiEnvelopeKind, PiEnvelopeSender, ScopeSetPayload, ScopeTarget,
    ScopeTargetType, UnsignedPiWireEnvelope, REQUIRED_DATA_DIRS,
};
use rand_core::OsRng;
use serde_json::Value;

#[test]
fn killed_mid_scan_verifies_audit_log_and_marks_run_resumable_on_next_start() {
    let root = temp_root("scan-resume");
    create_layout(&root);
    let mut rng = OsRng;
    let signing_key = SigningKey::generate(&mut rng);
    apply_test_scope(&root, &signing_key);
    let trusted_key_hex = verifying_key_hex(&signing_key.verifying_key());
    let scanner = fake_scanner(&root);
    let token = approved_scanner_token(&root);
    let scan_request = signed_scan_request(&signing_key, &scanner, &token);
    let mut child = Command::new(env!("CARGO_BIN_EXE_kelp-pi-agent"))
        .args([
            "wire",
            "--stdio",
            "--trusted-cp-public-key-hex",
            &trusted_key_hex,
            "--data-dir",
            root.to_str().expect("temp path utf8"),
        ])
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .spawn()
        .expect("spawn wire command");
    {
        let mut stdin = child.stdin.take().expect("wire stdin");
        stdin
            .write_all(scan_request.as_bytes())
            .expect("write scan request");
        stdin.write_all(b"\n").expect("write newline");
    }

    let run_state = root.join("runs").join("sigterm-resume.json");
    wait_for_running_state(&run_state);
    child.kill().expect("kill wire process");
    child.wait().expect("wait for killed scan");
    kill_fake_scanner(&root);

    let output = Command::new(env!("CARGO_BIN_EXE_kelp-pi-agent"))
        .args([
            "start",
            "--data-dir",
            root.to_str().expect("temp path utf8"),
            "--check-only",
        ])
        .output()
        .expect("run start preflight");
    assert!(
        output.status.success(),
        "stderr={}",
        String::from_utf8_lossy(&output.stderr)
    );

    let state: Value =
        serde_json::from_slice(&fs::read(&run_state).expect("read run state")).expect("json");
    assert_eq!(state["status"], "resumable");
    assert_eq!(state["resumable"], true);
    assert_eq!(state["run_id"], "sigterm-resume");
    let audit = fs::read_to_string(root.join("audit").join("agent.jsonl")).expect("read audit log");
    assert!(
        audit.contains("\"event\":\"scan.run.resumable\""),
        "{audit}"
    );
    let verify = Command::new(env!("CARGO_BIN_EXE_kelp-pi-agent"))
        .args([
            "verify-audit-log",
            "--log-file",
            root.join("audit")
                .join("agent.jsonl")
                .to_str()
                .expect("audit path utf8"),
        ])
        .output()
        .expect("verify audit log");
    assert!(
        verify.status.success(),
        "stderr={}",
        String::from_utf8_lossy(&verify.stderr)
    );

    fs::remove_dir_all(root).ok();
}

fn signed_scan_request(signing_key: &SigningKey, scanner: &Path, token: &str) -> String {
    let payload = serde_json::json!({
        "run_id": "sigterm-resume",
        "scope_id": "scope-a",
        "scanner": "nuclei",
        "targets": [{ "type": "host", "value": "allowed.example.test" }],
        "options": {
            "scanner_bin": scanner.to_str().expect("scanner path utf8"),
            "approval_token": token,
            "max_scan_duration_seconds": 60
        }
    });
    let envelope = sign_envelope(
        UnsignedPiWireEnvelope {
            msg_id: "scan-request-resume".to_string(),
            ts: "2026-06-19T00:00:00Z".to_string(),
            sender: PiEnvelopeSender::Cp,
            kind: PiEnvelopeKind::ScanRequest,
            payload: payload
                .as_object()
                .expect("scan request object")
                .iter()
                .map(|(key, value)| (key.clone(), value.clone()))
                .collect::<serde_json::Map<String, Value>>(),
        },
        signing_key,
    )
    .expect("sign scan request");
    serde_json::to_string(&envelope).expect("scan request json")
}

fn verifying_key_hex(key: &VerifyingKey) -> String {
    key.to_bytes()
        .iter()
        .map(|byte| format!("{byte:02x}"))
        .collect()
}

fn approved_scanner_token(root: &Path) -> String {
    let output = Command::new(env!("CARGO_BIN_EXE_kelp-pi-agent"))
        .args([
            "approval-request",
            "--data-dir",
            root.to_str().expect("temp path utf8"),
            "--gate",
            "scanner-invocation",
            "--scope-id",
            "scope-a",
            "--command",
            "scan nuclei allowed.example.test",
            "--host",
            "allowed.example.test",
            "--allowed",
        ])
        .output()
        .expect("request approval");
    assert!(
        output.status.success(),
        "stderr={}",
        String::from_utf8_lossy(&output.stderr)
    );
    let request: Value = serde_json::from_slice(&output.stdout).expect("approval request json");
    let token = request["token"]
        .as_str()
        .expect("approval token")
        .to_string();
    let approval = Command::new(env!("CARGO_BIN_EXE_kelp-pi-agent"))
        .args([
            "approve",
            "--data-dir",
            root.to_str().expect("temp path utf8"),
            &token,
        ])
        .output()
        .expect("approve token");
    assert!(
        approval.status.success(),
        "stderr={}",
        String::from_utf8_lossy(&approval.stderr)
    );
    token
}

fn wait_for_running_state(path: &Path) {
    for _ in 0..200 {
        if let Ok(bytes) = fs::read(path) {
            let state: Value = serde_json::from_slice(&bytes).expect("run state json");
            if state["status"] == "running" {
                return;
            }
        }
        thread::sleep(Duration::from_millis(25));
    }
    panic!("run state did not become running");
}

fn kill_fake_scanner(root: &Path) {
    let pid_path = root.join("scanner.pid");
    let Ok(pid) = fs::read_to_string(pid_path) else {
        return;
    };
    Command::new("kill")
        .args(["-TERM", pid.trim()])
        .status()
        .ok();
}

fn fake_scanner(root: &Path) -> PathBuf {
    let scanner = root.join("fake-scanner.sh");
    let pid_path = root.join("scanner.pid");
    fs::write(
        &scanner,
        format!(
            "#!/usr/bin/env sh\necho $$ > '{}'\nsleep 30\n",
            pid_path.display()
        ),
    )
    .expect("write fake scanner");
    let mut permissions = fs::metadata(&scanner)
        .expect("scanner metadata")
        .permissions();
    permissions.set_mode(0o755);
    fs::set_permissions(&scanner, permissions).expect("chmod scanner");
    scanner
}

fn apply_test_scope(root: &Path, signing_key: &SigningKey) {
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
            msg_id: "scope-msg-resume".to_string(),
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
        signing_key,
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
