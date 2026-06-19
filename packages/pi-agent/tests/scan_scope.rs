use std::fs;
#[cfg(unix)]
use std::os::unix::fs::PermissionsExt;
use std::path::{Path, PathBuf};
use std::process::Command;
use std::time::{SystemTime, UNIX_EPOCH};

use ed25519_dalek::SigningKey;
use kelp_pi_agent::{
    apply_scope_set, sign_envelope, PiEnvelopeKind, PiEnvelopeSender, ScopeSetPayload, ScopeTarget,
    ScopeTargetType, UnsignedPiWireEnvelope, DEFAULT_NUCLEI_BINARY_PATH,
    PINNED_NUCLEI_TEMPLATES_REVISION, REQUIRED_DATA_DIRS,
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

#[test]
fn nuclei_dry_run_uses_pinned_image_binary_by_default() {
    let root = temp_root("nuclei-default-bin");
    create_layout(&root);
    apply_test_scope(&root);
    let token = approved_scanner_token(&root);

    let output = Command::new(env!("CARGO_BIN_EXE_kelp-pi-agent"))
        .args([
            "scan",
            "nuclei",
            "--data-dir",
            root.to_str().expect("temp path utf8"),
            "--target",
            "allowed.example.test",
            "--dry-run",
            "--approval-token",
            &token,
        ])
        .output()
        .expect("run scan command");

    assert!(
        output.status.success(),
        "stderr={}",
        String::from_utf8_lossy(&output.stderr)
    );
    let response: Value = serde_json::from_slice(&output.stdout).expect("dry-run json");
    assert_eq!(response["scanner"], "nuclei");
    assert_eq!(response["scanner_bin"], DEFAULT_NUCLEI_BINARY_PATH);
    assert_eq!(
        response["nuclei_templates_revision"],
        PINNED_NUCLEI_TEMPLATES_REVISION
    );

    fs::remove_dir_all(root).ok();
}

#[cfg(unix)]
#[test]
fn sandboxed_scan_reloads_scanner_target_set_before_launch() {
    let root = temp_root("sandbox-target-set");
    create_layout(&root);
    apply_test_scope_for(&root, "10.42.0.20");
    let token = approved_scanner_token_for(&root, "10.42.0.20");
    let scanner = root.join("fake-scanner.sh");
    let scanner_args = root.join("scanner.args");
    let nft_stdin = root.join("nft.stdin");
    let systemd_args = root.join("systemd.args");
    let fake_nft = root.join("fake-nft.sh");
    let fake_systemd_run = root.join("fake-systemd-run.sh");
    fs::write(
        &scanner,
        format!(
            "#!/usr/bin/env sh\nprintf '%s\\n' \"$@\" > {}\n",
            scanner_args.display()
        ),
    )
    .expect("write scanner");
    fs::write(
        &fake_nft,
        "#!/usr/bin/env sh\ntest \"$1\" = \"-f\"\ntest \"$2\" = \"-\"\ncat > \"$KELP_PI_FAKE_NFT_STDIN\"\n",
    )
    .expect("write fake nft");
    fs::write(
        &fake_systemd_run,
        format!(
            "#!/usr/bin/env sh\nprintf '%s\\n' \"$@\" > {}\nwhile [ \"$1\" != \"--\" ]; do shift; done\nshift\n\"$@\"\n",
            systemd_args.display()
        ),
    )
    .expect("write fake systemd-run");
    for path in [&scanner, &fake_nft, &fake_systemd_run] {
        let mut permissions = fs::metadata(path).expect("metadata").permissions();
        permissions.set_mode(0o755);
        fs::set_permissions(path, permissions).expect("chmod");
    }

    let output = Command::new(env!("CARGO_BIN_EXE_kelp-pi-agent"))
        .args([
            "scan",
            "nuclei",
            "--data-dir",
            root.to_str().expect("temp path utf8"),
            "--target",
            "10.42.0.20",
            "--scanner-bin",
            scanner.to_str().expect("scanner path utf8"),
            "--approval-token",
            &token,
            "--sandbox",
            "--systemd-run-bin",
            fake_systemd_run.to_str().expect("systemd path utf8"),
            "--nft-bin",
            fake_nft.to_str().expect("nft path utf8"),
            "--min-free-bytes",
            "1",
        ])
        .env("KELP_PI_FAKE_NFT_STDIN", &nft_stdin)
        .output()
        .expect("run scan command");

    assert!(
        output.status.success(),
        "stderr={}",
        String::from_utf8_lossy(&output.stderr)
    );
    let nft = fs::read_to_string(nft_stdin).expect("read nft stdin");
    assert!(nft.contains("flush set inet kelp_pi_filter scanner_ipv4_targets"));
    assert!(nft.contains("add element inet kelp_pi_filter scanner_ipv4_targets { 10.42.0.20 }"));
    let systemd = fs::read_to_string(systemd_args).expect("read systemd args");
    assert!(systemd.contains("--property=NFTSet=user:inet:kelp_pi_filter:scanner_users"));
    let args = fs::read_to_string(scanner_args).expect("read scanner args");
    assert_eq!(args.lines().collect::<Vec<_>>(), vec!["10.42.0.20"]);

    fs::remove_dir_all(root).ok();
}

fn approved_scanner_token(root: &Path) -> String {
    approved_scanner_token_for(root, "allowed.example.test")
}

fn approved_scanner_token_for(root: &Path, target: &str) -> String {
    let command = format!("scan nuclei {target}");
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
            &command,
            "--host",
            target,
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

fn apply_test_scope(root: &Path) {
    apply_test_scope_for(root, "allowed.example.test")
}

fn apply_test_scope_for(root: &Path, target: &str) {
    let mut rng = OsRng;
    let signing_key = SigningKey::generate(&mut rng);
    let payload = ScopeSetPayload {
        scope_id: "scope-a".to_string(),
        issued_at: "2026-06-19T00:00:00Z".to_string(),
        valid_from: "2020-01-01T00:00:00Z".to_string(),
        valid_until: "2100-01-01T00:00:00Z".to_string(),
        targets: vec![ScopeTarget {
            target_type: ScopeTargetType::Host,
            value: target.to_string(),
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
