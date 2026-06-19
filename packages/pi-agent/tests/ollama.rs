use std::fs;
#[cfg(unix)]
use std::os::unix::fs::PermissionsExt;
use std::path::PathBuf;
use std::process::Command;
use std::time::{SystemTime, UNIX_EPOCH};

#[cfg(unix)]
#[test]
fn ollama_load_check_runs_empty_prompt_after_guard_allows() {
    let root = temp_root("ollama-load-ok");
    fs::create_dir_all(&root).expect("create root");
    let model_path = root.join("model");
    let meminfo_path = root.join("meminfo");
    let fake_ollama = root.join("ollama");
    let args_path = root.join("ollama.args");
    fs::write(&model_path, "Raspberry Pi 5 Model B Rev 1.0\0").expect("write model");
    fs::write(&meminfo_path, "MemTotal:       15728640 kB\n").expect("write meminfo");
    fs::write(
        &fake_ollama,
        "#!/usr/bin/env sh\nprintf '%s\\n' \"$@\" > \"$KELP_PI_FAKE_OLLAMA_ARGS\"\n",
    )
    .expect("write fake ollama");
    make_executable(&fake_ollama);

    let output = Command::new(env!("CARGO_BIN_EXE_kelp-pi-agent"))
        .args([
            "ollama",
            "load-check",
            "--enable-ollama",
            "--model",
            "llama3.2:3b",
            "--ollama-bin",
            fake_ollama.to_str().expect("path utf8"),
        ])
        .env("KELP_PI_OLLAMA_MODEL_PATH", &model_path)
        .env("KELP_PI_OLLAMA_MEMINFO_PATH", &meminfo_path)
        .env("KELP_PI_FAKE_OLLAMA_ARGS", &args_path)
        .output()
        .expect("run load check");

    assert!(
        output.status.success(),
        "stderr={}",
        String::from_utf8_lossy(&output.stderr)
    );
    let stdout: serde_json::Value =
        serde_json::from_slice(&output.stdout).expect("parse load check json");
    assert_eq!(stdout["load"]["status"], "loaded");
    let args = fs::read_to_string(args_path).expect("read fake args");
    assert_eq!(
        args.lines().collect::<Vec<_>>(),
        vec!["run", "llama3.2:3b", ""]
    );

    fs::remove_dir_all(root).ok();
}

#[cfg(unix)]
#[test]
fn ollama_load_check_refuses_8gb_pi_before_invoking_ollama() {
    let root = temp_root("ollama-load-refuse");
    fs::create_dir_all(&root).expect("create root");
    let model_path = root.join("model");
    let meminfo_path = root.join("meminfo");
    let fake_ollama = root.join("ollama");
    let args_path = root.join("ollama.args");
    fs::write(&model_path, "Raspberry Pi 5 Model B Rev 1.0\0").expect("write model");
    fs::write(&meminfo_path, "MemTotal:        8388608 kB\n").expect("write meminfo");
    fs::write(
        &fake_ollama,
        "#!/usr/bin/env sh\nprintf '%s\\n' \"$@\" > \"$KELP_PI_FAKE_OLLAMA_ARGS\"\n",
    )
    .expect("write fake ollama");
    make_executable(&fake_ollama);

    let output = Command::new(env!("CARGO_BIN_EXE_kelp-pi-agent"))
        .args([
            "ollama",
            "load-check",
            "--enable-ollama",
            "--model",
            "llama3.2:3b",
            "--ollama-bin",
            fake_ollama.to_str().expect("path utf8"),
        ])
        .env("KELP_PI_OLLAMA_MODEL_PATH", &model_path)
        .env("KELP_PI_OLLAMA_MEMINFO_PATH", &meminfo_path)
        .env("KELP_PI_FAKE_OLLAMA_ARGS", &args_path)
        .output()
        .expect("run load check");

    assert_eq!(output.status.code(), Some(77));
    let stdout: serde_json::Value =
        serde_json::from_slice(&output.stdout).expect("parse refusal json");
    assert_eq!(stdout["guard"]["decision"], "refuse");
    assert_eq!(stdout["load"]["status"], "skipped");
    assert!(!args_path.exists());

    fs::remove_dir_all(root).ok();
}

#[cfg(unix)]
fn make_executable(path: &PathBuf) {
    let mut permissions = fs::metadata(path).expect("metadata").permissions();
    permissions.set_mode(0o755);
    fs::set_permissions(path, permissions).expect("chmod");
}

fn temp_root(name: &str) -> PathBuf {
    let nonce = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .expect("clock")
        .as_nanos();
    std::env::temp_dir().join(format!("kelp-pi-{name}-{}-{nonce}", std::process::id()))
}
