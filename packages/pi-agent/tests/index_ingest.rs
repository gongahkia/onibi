use std::fs;
use std::path::{Path, PathBuf};
use std::process::Command;
use std::time::{SystemTime, UNIX_EPOCH};

use kelp_pi_agent::REQUIRED_DATA_DIRS;
use serde_json::Value;

#[test]
fn index_ingest_makes_file_queryable_with_citation() {
    let root = temp_root("index-ingest");
    let input = root.join("normalized-findings.json");
    create_layout(&root);
    fs::write(
        &input,
        r#"{"findings":[{"title":"Default admin marker exposed","description":"The fixture exposes an obvious default-admin marker."}]}"#,
    )
    .expect("write input");

    let ingest = Command::new(env!("CARGO_BIN_EXE_kelp-pi-agent"))
        .args([
            "index",
            "ingest",
            "--data-dir",
            root.to_str().expect("temp path utf8"),
            "--input",
            input.to_str().expect("input path utf8"),
            "--path",
            "evidence/fixture-target/normalized-findings.json",
            "--min-free-bytes",
            "1",
        ])
        .output()
        .expect("run index ingest command");

    assert!(ingest.status.success());
    let ingest_stdout = String::from_utf8_lossy(&ingest.stdout);
    assert!(ingest_stdout.contains("\"ok\": true"), "{ingest_stdout}");

    let ask = Command::new(env!("CARGO_BIN_EXE_kelp-pi-agent"))
        .args([
            "ask",
            "--data-dir",
            root.to_str().expect("temp path utf8"),
            "--top-k",
            "1",
            "default",
            "admin",
            "marker",
        ])
        .output()
        .expect("run ask command");

    assert!(ask.status.success());
    let answer: Value = serde_json::from_slice(&ask.stdout).expect("ask json");
    assert!(answer["no_answer"].is_null(), "{answer}");
    let citations = answer["citations"].as_array().expect("citations array");
    assert_eq!(citations.len(), 1, "{answer}");
    assert_eq!(
        citations[0]["path"],
        Value::String("evidence/fixture-target/normalized-findings.json".to_string())
    );

    fs::remove_dir_all(root).ok();
}

#[test]
fn index_ingest_refuses_binary_source() {
    let root = temp_root("index-ingest-binary");
    let input = root.join("payload.bin");
    create_layout(&root);
    fs::write(&input, b"\x7fELFpayload").expect("write binary input");

    let output = Command::new(env!("CARGO_BIN_EXE_kelp-pi-agent"))
        .args([
            "index",
            "ingest",
            "--data-dir",
            root.to_str().expect("temp path utf8"),
            "--input",
            input.to_str().expect("input path utf8"),
            "--path",
            "evidence/payload.bin",
        ])
        .output()
        .expect("run index ingest command");

    assert_eq!(output.status.code(), Some(65));
    let stderr = String::from_utf8_lossy(&output.stderr);
    assert!(stderr.contains("index ingest refused source"), "{stderr}");

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
