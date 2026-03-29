use std::env;
use std::fs;
use std::path::{Path, PathBuf};
use std::process::Command;
use std::time::{SystemTime, UNIX_EPOCH};

fn temp_root(prefix: &str) -> PathBuf {
    let nanos = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .expect("system time should be after the unix epoch")
        .as_nanos();
    env::temp_dir().join(format!(
        "kelp-packaging-{prefix}-{}-{nanos}",
        std::process::id()
    ))
}

fn package_version(repo_root: &Path) -> String {
    fs::read_to_string(repo_root.join("Cargo.toml"))
        .expect("Cargo.toml should be readable")
        .lines()
        .find_map(|line| {
            line.strip_prefix("version = ")
                .map(|value| value.trim_matches('"').to_string())
        })
        .expect("Cargo.toml should define a package version")
}

#[test]
fn package_release_script_generates_archives_and_formula() {
    let repo_root = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    let output_dir = temp_root("dist");
    let version = package_version(&repo_root);

    let installer_syntax = Command::new("bash")
        .arg("-n")
        .arg(repo_root.join("installer.sh"))
        .status()
        .expect("installer syntax check should run");
    assert!(installer_syntax.success());

    let package_syntax = Command::new("bash")
        .arg("-n")
        .arg(repo_root.join("scripts/package-release.sh"))
        .status()
        .expect("package script syntax check should run");
    assert!(package_syntax.success());

    let package_status = Command::new(repo_root.join("scripts/package-release.sh"))
        .arg(&output_dir)
        .current_dir(&repo_root)
        .status()
        .expect("package release script should run");
    assert!(package_status.success());

    assert!(output_dir
        .join(format!("kelp-v{version}-source.tar.gz"))
        .exists());
    assert!(output_dir
        .join(format!("kelp-v{version}-source.tar.gz.sha256"))
        .exists());
    assert!(output_dir.join("kelp.rb").exists());

    let formula = fs::read_to_string(repo_root.join("Formula/kelp.rb"))
        .expect("formula should be written to the repo");
    assert!(formula.contains("class Kelp < Formula"));
    assert!(formula.contains(&format!("v{version}")));
}

#[test]
fn installer_falls_back_to_source_install_when_release_download_fails() {
    let repo_root = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    let cargo_home = temp_root("cargo-home");
    let cargo_target_dir = temp_root("cargo-target");
    let status = Command::new(repo_root.join("installer.sh"))
        .arg("--release-version")
        .arg("0.0.0")
        .env(
            "KELP_INSTALL_BASE_URL",
            "https://invalid.example.invalid/releases",
        )
        .env("KELP_INSTALL_SOURCE_PATH", &repo_root)
        .env("CARGO_HOME", &cargo_home)
        .env("CARGO_TARGET_DIR", &cargo_target_dir)
        .current_dir(&repo_root)
        .status()
        .expect("installer should run");
    assert!(status.success());
    assert!(cargo_home.join("bin/kelp").exists());
}
