use std::fs;
use std::path::{Path, PathBuf};
use std::process::Command;
use std::time::{SystemTime, UNIX_EPOCH};

use kelp_pi_agent::REQUIRED_DATA_DIRS;

#[test]
fn scan_refuses_when_free_disk_floor_is_not_met() {
    let root = temp_root("quota-scan");
    create_layout(&root);
    let floor = u64::MAX.to_string();

    let output = Command::new(env!("CARGO_BIN_EXE_kelp-pi-agent"))
        .args([
            "scan",
            "nmap",
            "--data-dir",
            root.to_str().expect("temp path utf8"),
            "--target",
            "127.0.0.1",
            "--dry-run",
            "--min-free-bytes",
            floor.as_str(),
        ])
        .output()
        .expect("run scan command");

    assert_eq!(output.status.code(), Some(75));
    let stderr = String::from_utf8_lossy(&output.stderr);
    assert!(stderr.contains("scan refused by storage quota"), "{stderr}");
    let audit_log =
        fs::read_to_string(root.join("audit").join("agent.jsonl")).expect("read audit log");
    assert!(audit_log.contains("\"event\":\"storage.quota.refused\""));
    assert!(audit_log.contains("\"scope\":\"scan\""));

    fs::remove_dir_all(root).ok();
}

#[test]
fn normalize_refuses_when_free_disk_floor_is_not_met() {
    let root = temp_root("quota-normalize");
    let workspace = root.join("workspace");
    let raw = workspace.join("raw").join("nuclei.jsonl");
    let floor = u64::MAX.to_string();
    create_layout(&root);
    fs::create_dir_all(raw.parent().expect("raw parent")).expect("create raw dir");
    fs::write(
        &raw,
        r#"{"template-id":"t","matched-at":"https://app.example.test","info":{"name":"n","severity":"low"}}"#,
    )
    .expect("write raw");

    let output = Command::new(env!("CARGO_BIN_EXE_kelp-pi-agent"))
        .args([
            "normalize",
            "nuclei",
            "--data-dir",
            root.to_str().expect("temp path utf8"),
            "--input",
            raw.to_str().expect("raw path utf8"),
            "--workspace",
            workspace.to_str().expect("workspace path utf8"),
            "--min-free-bytes",
            floor.as_str(),
        ])
        .output()
        .expect("run normalize command");

    assert_eq!(output.status.code(), Some(75));
    let stderr = String::from_utf8_lossy(&output.stderr);
    assert!(
        stderr.contains("normalize nuclei refused by storage quota"),
        "{stderr}"
    );
    assert!(!workspace.join("normalized").join("findings.json").exists());
    let audit_log =
        fs::read_to_string(root.join("audit").join("agent.jsonl")).expect("read audit log");
    assert!(audit_log.contains("\"event\":\"storage.quota.refused\""));
    assert!(audit_log.contains("\"scope\":\"ingest\""));

    fs::remove_dir_all(root).ok();
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
