use std::fs;
use std::path::PathBuf;
use std::process::Command;
use std::time::{SystemTime, UNIX_EPOCH};

use serde_json::Value;

#[test]
fn acceptance_manifest_is_signed_and_tamper_evident() {
    let root = temp_root("acceptance-manifest");
    let data_dir = root.join("data");
    let artifact_dir = root.join("field-acceptance");
    fs::create_dir_all(&artifact_dir).expect("create artifact dir");
    fs::write(artifact_dir.join("summary.txt"), "OK node node.log\n").expect("write summary");
    fs::write(
        artifact_dir.join("node.log"),
        "agent version: kelp-pi-agent 0.1.0\n",
    )
    .expect("write node log");

    let sign = Command::new(env!("CARGO_BIN_EXE_kelp-pi-agent"))
        .args([
            "acceptance",
            "sign",
            "--artifact-dir",
            artifact_dir.to_str().expect("artifact dir utf8"),
            "--data-dir",
            data_dir.to_str().expect("data dir utf8"),
        ])
        .output()
        .expect("run acceptance sign");
    assert!(
        sign.status.success(),
        "{}",
        String::from_utf8_lossy(&sign.stderr)
    );
    let sign_json: Value = serde_json::from_slice(&sign.stdout).expect("sign json");
    assert_eq!(sign_json["ok"], Value::Bool(true));
    assert_eq!(sign_json["fileCount"], Value::from(2));
    assert!(artifact_dir.join("acceptance-manifest.json").exists());
    assert!(artifact_dir.join("acceptance-manifest.sig").exists());
    assert!(artifact_dir.join("acceptance-manifest.pub.json").exists());

    let verify = Command::new(env!("CARGO_BIN_EXE_kelp-pi-agent"))
        .args([
            "acceptance",
            "verify",
            "--artifact-dir",
            artifact_dir.to_str().expect("artifact dir utf8"),
        ])
        .output()
        .expect("run acceptance verify");
    assert!(
        verify.status.success(),
        "{}",
        String::from_utf8_lossy(&verify.stderr)
    );

    fs::write(
        artifact_dir.join("summary.txt"),
        "OK node node.log\nFAIL tamper\n",
    )
    .expect("tamper summary");
    let tampered = Command::new(env!("CARGO_BIN_EXE_kelp-pi-agent"))
        .args([
            "acceptance",
            "verify",
            "--artifact-dir",
            artifact_dir.to_str().expect("artifact dir utf8"),
        ])
        .output()
        .expect("run tampered acceptance verify");
    assert_eq!(tampered.status.code(), Some(77));

    fs::remove_dir_all(root).ok();
}

fn temp_root(name: &str) -> PathBuf {
    let mut path = std::env::temp_dir();
    let nanos = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .expect("time")
        .as_nanos();
    path.push(format!("kelp-pi-{name}-{nanos}"));
    path
}
