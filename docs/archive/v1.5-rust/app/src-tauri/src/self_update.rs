use anyhow::{anyhow, bail, Context, Result};
use base64::{engine::general_purpose::STANDARD, Engine as _};
use p256::{
    ecdsa::{signature::Verifier, Signature, VerifyingKey},
    pkcs8::DecodePublicKey,
};
use semver::Version;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::{
    collections::BTreeMap,
    fs,
    io::{Read, Write},
    path::{Path, PathBuf},
    process::Command,
};

pub const DEFAULT_HEADLESS_UPDATE_ENDPOINT: &str =
    "https://github.com/gongahkia/onibi/releases/latest/download/latest-headless.json";
const HEADLESS_PUBLIC_KEY: Option<&str> = option_env!("ONIBI_HEADLESS_UPDATE_PUBLIC_KEY");

#[derive(Debug, Clone, Deserialize, Serialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct HeadlessUpdateManifest {
    pub version: String,
    #[serde(default, alias = "pub_date")]
    pub pub_date: Option<String>,
    #[serde(default)]
    pub notes: Option<String>,
    #[serde(default)]
    pub release_url: Option<String>,
    pub assets: BTreeMap<String, HeadlessUpdateAsset>,
}

#[derive(Debug, Clone, Deserialize, Serialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct HeadlessUpdateAsset {
    pub url: String,
    pub sha256: String,
    #[serde(default)]
    pub signature: Option<String>,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct HeadlessUpdateCheck {
    pub update_available: bool,
    pub current_version: String,
    pub latest_version: String,
    pub platform: String,
    pub url: Option<String>,
    pub sha256: Option<String>,
    pub signature: Option<String>,
    pub pub_date: Option<String>,
    pub notes: Option<String>,
    pub release_url: Option<String>,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct HeadlessUpdateInstall {
    pub installed: bool,
    pub current_version: String,
    pub latest_version: String,
    pub path: String,
    pub service_restarted: bool,
}

pub fn check_for_headless_update(endpoint: Option<&str>) -> Result<HeadlessUpdateCheck> {
    let endpoint = endpoint
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .unwrap_or(DEFAULT_HEADLESS_UPDATE_ENDPOINT);
    let body = http_get_string(endpoint)?;
    let manifest: HeadlessUpdateManifest =
        serde_json::from_str(&body).context("parse headless update manifest")?;
    check_manifest(&manifest, env!("CARGO_PKG_VERSION"), platform_key())
}

pub fn install_headless_update(endpoint: Option<&str>) -> Result<HeadlessUpdateInstall> {
    let check = check_for_headless_update(endpoint)?;
    if !check.update_available {
        return Ok(HeadlessUpdateInstall {
            installed: false,
            current_version: check.current_version,
            latest_version: check.latest_version,
            path: current_exe_path()?.display().to_string(),
            service_restarted: false,
        });
    }
    let url = check
        .url
        .as_deref()
        .ok_or_else(|| anyhow!("update manifest did not include an asset URL"))?;
    let bytes = http_get_bytes(url)?;
    let sha256 = check
        .sha256
        .as_deref()
        .ok_or_else(|| anyhow!("update manifest did not include a SHA256 checksum"))?;
    verify_sha256(&bytes, sha256)?;
    if let Some(public_key) = HEADLESS_PUBLIC_KEY.and_then(non_empty) {
        let signature = check
            .signature
            .as_deref()
            .ok_or_else(|| anyhow!("update manifest did not include a headless signature"))?;
        verify_signature(&bytes, signature, public_key)?;
    }
    let path = current_exe_path()?;
    replace_current_binary(&path, &bytes)?;
    let service_restarted = restart_user_service_if_present("onibi.service");
    Ok(HeadlessUpdateInstall {
        installed: true,
        current_version: check.current_version,
        latest_version: check.latest_version,
        path: path.display().to_string(),
        service_restarted,
    })
}

fn check_manifest(
    manifest: &HeadlessUpdateManifest,
    current_version: &str,
    platform: &str,
) -> Result<HeadlessUpdateCheck> {
    let current = parse_version(current_version).context("parse current version")?;
    let latest = parse_version(&manifest.version).context("parse latest version")?;
    let asset = select_asset(manifest, platform)?;
    let update_available = latest > current;
    Ok(HeadlessUpdateCheck {
        update_available,
        current_version: current_version.to_string(),
        latest_version: manifest.version.trim_start_matches('v').to_string(),
        platform: platform.to_string(),
        url: update_available.then(|| asset.url.clone()),
        sha256: update_available.then(|| asset.sha256.clone()),
        signature: update_available.then(|| asset.signature.clone()).flatten(),
        pub_date: manifest.pub_date.clone(),
        notes: manifest.notes.clone(),
        release_url: manifest.release_url.clone(),
    })
}

fn select_asset<'a>(
    manifest: &'a HeadlessUpdateManifest,
    platform: &str,
) -> Result<&'a HeadlessUpdateAsset> {
    let aliases = platform_aliases(platform);
    for key in aliases {
        if let Some(asset) = manifest.assets.get(key) {
            return Ok(asset);
        }
    }
    bail!("headless update manifest has no asset for {platform}");
}

fn platform_aliases(platform: &str) -> Vec<&'static str> {
    match platform {
        "linux-x86_64" => vec!["linux-x86_64", "linux-amd64"],
        "linux-aarch64" => vec!["linux-aarch64", "linux-arm64"],
        _ => vec![],
    }
}

fn platform_key() -> &'static str {
    match (std::env::consts::OS, std::env::consts::ARCH) {
        ("linux", "x86_64") => "linux-x86_64",
        ("linux", "aarch64") => "linux-aarch64",
        _ => "unsupported",
    }
}

fn parse_version(version: &str) -> Result<Version> {
    Version::parse(version.trim().trim_start_matches('v')).map_err(Into::into)
}

fn http_get_string(url: &str) -> Result<String> {
    let bytes = http_get_bytes(url)?;
    String::from_utf8(bytes).context("update response was not UTF-8")
}

fn http_get_bytes(url: &str) -> Result<Vec<u8>> {
    let response = ureq::get(url)
        .timeout(std::time::Duration::from_secs(30))
        .call()
        .with_context(|| format!("GET {url}"))?;
    if response.status() >= 400 {
        bail!("GET {url} returned HTTP {}", response.status());
    }
    let mut reader = response.into_reader();
    let mut bytes = Vec::new();
    reader
        .read_to_end(&mut bytes)
        .with_context(|| format!("read response body from {url}"))?;
    Ok(bytes)
}

fn verify_sha256(bytes: &[u8], expected_hex: &str) -> Result<()> {
    let digest = Sha256::digest(bytes);
    let actual = hex::encode(digest);
    if actual.eq_ignore_ascii_case(expected_hex.trim()) {
        Ok(())
    } else {
        bail!("headless update checksum mismatch: expected {expected_hex}, got {actual}")
    }
}

fn verify_signature(bytes: &[u8], signature_base64: &str, public_key_base64: &str) -> Result<()> {
    let public_key = STANDARD
        .decode(public_key_base64.trim())
        .context("decode headless update public key")?;
    let verifying_key =
        VerifyingKey::from_public_key_der(&public_key).context("parse headless public key")?;
    let signature = STANDARD
        .decode(signature_base64.trim())
        .context("decode headless update signature")?;
    let signature = Signature::from_der(&signature).context("parse headless update signature")?;
    verifying_key
        .verify(bytes, &signature)
        .context("verify headless update signature")
}

fn current_exe_path() -> Result<PathBuf> {
    std::env::current_exe().context("locate current Onibi binary")
}

#[cfg(unix)]
fn replace_current_binary(path: &Path, bytes: &[u8]) -> Result<()> {
    use std::os::unix::fs::PermissionsExt;

    let parent = path
        .parent()
        .ok_or_else(|| anyhow!("current binary has no parent directory"))?;
    let name = path
        .file_name()
        .and_then(|value| value.to_str())
        .ok_or_else(|| anyhow!("current binary path is not valid UTF-8"))?;
    let temp = parent.join(format!(".{name}.update-{}", std::process::id()));
    {
        let mut file = fs::File::create(&temp)
            .with_context(|| format!("create temporary update {}", temp.display()))?;
        file.write_all(bytes)
            .with_context(|| format!("write temporary update {}", temp.display()))?;
        file.sync_all()
            .with_context(|| format!("sync temporary update {}", temp.display()))?;
    }
    let mode = fs::metadata(path)
        .map(|metadata| metadata.permissions().mode())
        .unwrap_or(0o755);
    fs::set_permissions(&temp, fs::Permissions::from_mode(mode | 0o111))
        .with_context(|| format!("set executable permissions on {}", temp.display()))?;
    fs::rename(&temp, path).with_context(|| {
        format!(
            "replace current binary {} with {}",
            path.display(),
            temp.display()
        )
    })?;
    Ok(())
}

#[cfg(not(unix))]
fn replace_current_binary(_path: &Path, _bytes: &[u8]) -> Result<()> {
    bail!("headless updater install is only supported on Unix platforms")
}

fn restart_user_service_if_present(service: &str) -> bool {
    let active = Command::new("systemctl")
        .args(["--user", "is-active", "--quiet", service])
        .status()
        .map(|status| status.success())
        .unwrap_or(false);
    let enabled = Command::new("systemctl")
        .args(["--user", "is-enabled", "--quiet", service])
        .status()
        .map(|status| status.success())
        .unwrap_or(false);
    if !active && !enabled {
        return false;
    }
    Command::new("systemctl")
        .args(["--user", "restart", service])
        .status()
        .map(|status| status.success())
        .unwrap_or(false)
}

fn non_empty(value: &str) -> Option<&str> {
    let trimmed = value.trim();
    if trimmed.is_empty() {
        None
    } else {
        Some(trimmed)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use p256::ecdsa::{signature::Signer, SigningKey};
    use p256::pkcs8::EncodePublicKey;

    fn manifest(version: &str) -> HeadlessUpdateManifest {
        HeadlessUpdateManifest {
            version: version.to_string(),
            pub_date: Some("2026-06-06T00:00:00Z".to_string()),
            notes: Some("Release notes".to_string()),
            release_url: Some("https://github.com/gongahkia/onibi/releases/tag/v1.5.1".to_string()),
            assets: BTreeMap::from([
                (
                    "linux-x86_64".to_string(),
                    HeadlessUpdateAsset {
                        url: "https://example.com/onibi-linux-x86_64".to_string(),
                        sha256: "abc".to_string(),
                        signature: Some("sig".to_string()),
                    },
                ),
                (
                    "linux-arm64".to_string(),
                    HeadlessUpdateAsset {
                        url: "https://example.com/onibi-linux-arm64".to_string(),
                        sha256: "def".to_string(),
                        signature: None,
                    },
                ),
            ]),
        }
    }

    #[test]
    fn detects_available_headless_update() {
        let check = check_manifest(&manifest("v1.5.1"), "1.5.0-dev", "linux-x86_64").unwrap();
        assert!(check.update_available);
        assert_eq!(check.latest_version, "1.5.1");
        assert_eq!(
            check.url.as_deref(),
            Some("https://example.com/onibi-linux-x86_64")
        );
    }

    #[test]
    fn suppresses_same_or_older_headless_update() {
        let check = check_manifest(&manifest("1.5.0-dev"), "1.5.0-dev", "linux-x86_64").unwrap();
        assert!(!check.update_available);
        assert!(check.url.is_none());
    }

    #[test]
    fn selects_arm64_alias_asset() {
        let check = check_manifest(&manifest("1.5.1"), "1.5.0", "linux-aarch64").unwrap();
        assert_eq!(
            check.url.as_deref(),
            Some("https://example.com/onibi-linux-arm64")
        );
        assert_eq!(check.sha256.as_deref(), Some("def"));
    }

    #[test]
    fn verifies_sha256_checksums() {
        let bytes = b"onibi";
        let digest = hex::encode(Sha256::digest(bytes));
        assert!(verify_sha256(bytes, &digest).is_ok());
        assert!(verify_sha256(bytes, "deadbeef").is_err());
    }

    #[test]
    fn verifies_p256_signature() {
        let signing_key = SigningKey::from_bytes((&[7u8; 32]).into()).unwrap();
        let verifying_key = signing_key.verifying_key();
        let public_der = verifying_key.to_public_key_der().unwrap();
        let payload = b"onibi update";
        let signature: Signature = signing_key.sign(payload);

        assert!(verify_signature(
            payload,
            &STANDARD.encode(signature.to_der()),
            &STANDARD.encode(public_der.as_bytes()),
        )
        .is_ok());
        assert!(verify_signature(
            b"tampered",
            &STANDARD.encode(signature.to_der()),
            &STANDARD.encode(public_der.as_bytes()),
        )
        .is_err());
    }
}
