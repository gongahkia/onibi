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

#[test]
fn upload_refuses_when_free_disk_floor_is_not_met() {
    let root = temp_root("quota-upload");
    let input = root.join("raw-upload.txt");
    let floor = u64::MAX.to_string();
    create_layout(&root);
    fs::write(&input, b"upload bytes").expect("write upload");

    let output = Command::new(env!("CARGO_BIN_EXE_kelp-pi-agent"))
        .args([
            "upload",
            "accept",
            "--data-dir",
            root.to_str().expect("temp path utf8"),
            "--input",
            input.to_str().expect("input path utf8"),
            "--name",
            "raw-upload.txt",
            "--min-free-bytes",
            floor.as_str(),
        ])
        .output()
        .expect("run upload command");

    assert_eq!(output.status.code(), Some(75));
    let stderr = String::from_utf8_lossy(&output.stderr);
    assert!(
        stderr.contains("upload accept refused by storage quota"),
        "{stderr}"
    );
    assert!(!root
        .join("evidence")
        .join("uploads")
        .join("raw-upload.txt")
        .exists());
    let audit_log =
        fs::read_to_string(root.join("audit").join("agent.jsonl")).expect("read audit log");
    assert!(audit_log.contains("\"event\":\"storage.quota.refused\""));
    assert!(audit_log.contains("\"scope\":\"upload\""));

    fs::remove_dir_all(root).ok();
}

#[test]
fn upload_accept_stages_file_when_quota_allows() {
    let root = temp_root("quota-upload-ok");
    let input = root.join("raw-upload.txt");
    create_layout(&root);
    fs::write(&input, b"upload bytes").expect("write upload");

    let output = Command::new(env!("CARGO_BIN_EXE_kelp-pi-agent"))
        .args([
            "upload",
            "accept",
            "--data-dir",
            root.to_str().expect("temp path utf8"),
            "--input",
            input.to_str().expect("input path utf8"),
            "--name",
            "raw-upload.txt",
            "--min-free-bytes",
            "1",
        ])
        .output()
        .expect("run upload command");

    assert!(output.status.success());
    let staged = root.join("evidence").join("uploads").join("raw-upload.txt");
    assert_eq!(fs::read(&staged).expect("read staged"), b"upload bytes");
    let stdout = String::from_utf8_lossy(&output.stdout);
    assert!(stdout.contains("\"ok\": true"), "{stdout}");

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
