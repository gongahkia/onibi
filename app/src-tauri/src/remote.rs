use anyhow::{anyhow, bail, Context, Result};
use base64::{engine::general_purpose::STANDARD, Engine as _};
use serde::{Deserialize, Serialize};
use std::{
    io::Write,
    process::{Command, Output, Stdio},
    time::{SystemTime, UNIX_EPOCH},
};

pub const DEFAULT_REMOTE_HELPER_PATH: &str = "~/.onibi/bin/onibi";
pub const DEFAULT_REMOTE_STAGING_DIR: &str = "~/.onibi/staged";

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct RemoteSshBootstrapRequest {
    pub target: String,
    #[serde(default)]
    pub user: Option<String>,
    pub host: String,
    #[serde(default)]
    pub port: Option<u16>,
    #[serde(default)]
    pub remote_cwd: Option<String>,
    #[serde(default)]
    pub ssh_command: Option<String>,
    #[serde(default)]
    pub helper_path: Option<String>,
    #[serde(default)]
    pub staging_dir: Option<String>,
}

#[derive(Clone, Debug, Serialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct RemoteSshBootstrapResult {
    pub ok: bool,
    pub target: String,
    pub helper_path: String,
    pub helper_version: String,
    pub staging_dir: String,
    pub bootstrapped_at: i64,
    pub stdout: String,
    pub stderr: String,
}

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
#[cfg(feature = "gui")]
pub struct RemoteSshStageFileRequest {
    pub target: String,
    #[serde(default)]
    pub user: Option<String>,
    pub host: String,
    #[serde(default)]
    pub port: Option<u16>,
    #[serde(default)]
    pub remote_cwd: Option<String>,
    #[serde(default)]
    pub ssh_command: Option<String>,
    #[serde(default)]
    pub staging_dir: Option<String>,
    pub filename: String,
    pub data: Vec<u8>,
}

#[derive(Clone, Debug, Serialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
#[cfg(feature = "gui")]
pub struct RemoteSshStageFileResult {
    pub ok: bool,
    pub remote_path: String,
    pub bytes: usize,
    pub stdout: String,
    pub stderr: String,
}

pub fn remote_ssh_bootstrap(
    request: RemoteSshBootstrapRequest,
) -> Result<RemoteSshBootstrapResult> {
    let binary = std::env::current_exe().context("locate current Onibi binary")?;
    let bytes = std::fs::read(&binary)
        .with_context(|| format!("read current Onibi binary at {}", binary.display()))?;
    remote_ssh_bootstrap_with_bytes(request, &bytes)
}

pub fn remote_ssh_bootstrap_with_bytes(
    request: RemoteSshBootstrapRequest,
    helper_bytes: &[u8],
) -> Result<RemoteSshBootstrapResult> {
    if helper_bytes.is_empty() {
        bail!("current Onibi binary is empty");
    }
    validate_endpoint(&request.host, request.port)?;
    let helper_path = normalized_remote_path(
        request.helper_path.as_deref(),
        DEFAULT_REMOTE_HELPER_PATH,
        "helper path",
    )?;
    let staging_dir = normalized_remote_path(
        request.staging_dir.as_deref(),
        DEFAULT_REMOTE_STAGING_DIR,
        "staging directory",
    )?;
    let output = run_ssh_command(
        &request,
        &bootstrap_remote_script(&helper_path, &staging_dir),
        STANDARD.encode(helper_bytes).as_bytes(),
    )?;
    ensure_success(&output, "remote SSH bootstrap")?;
    Ok(RemoteSshBootstrapResult {
        ok: true,
        target: request.target.trim().to_string(),
        helper_path,
        helper_version: env!("CARGO_PKG_VERSION").to_string(),
        staging_dir,
        bootstrapped_at: now_millis(),
        stdout: stdout_lossy(&output),
        stderr: stderr_lossy(&output),
    })
}

#[cfg(feature = "gui")]
pub fn remote_ssh_stage_file(
    request: RemoteSshStageFileRequest,
) -> Result<RemoteSshStageFileResult> {
    validate_endpoint(&request.host, request.port)?;
    let filename = sanitized_remote_filename(&request.filename)?;
    let staging_dir = normalized_remote_path(
        request.staging_dir.as_deref(),
        DEFAULT_REMOTE_STAGING_DIR,
        "staging directory",
    )?;
    if request.data.is_empty() {
        bail!("cannot stage an empty file");
    }
    let output = run_ssh_command(
        &request,
        &stage_file_remote_script(&staging_dir, &filename),
        STANDARD.encode(&request.data).as_bytes(),
    )?;
    ensure_success(&output, "remote file staging")?;
    let remote_path = stdout_lossy(&output).trim().to_string();
    if remote_path.is_empty() {
        bail!("remote file staging did not return a remote path");
    }
    Ok(RemoteSshStageFileResult {
        ok: true,
        remote_path,
        bytes: request.data.len(),
        stdout: stdout_lossy(&output),
        stderr: stderr_lossy(&output),
    })
}

#[cfg(feature = "gui")]
pub fn read_clipboard_image_png() -> Result<Option<Vec<u8>>> {
    let mut clipboard = arboard::Clipboard::new().context("open system clipboard")?;
    let image = match clipboard.get_image() {
        Ok(image) => image,
        Err(error) => {
            let message = error.to_string();
            let lowered = message.to_ascii_lowercase();
            if lowered.contains("content") && lowered.contains("available") {
                return Ok(None);
            }
            return Err(anyhow!(message).context("read image from system clipboard"));
        }
    };

    let mut png_bytes = Vec::new();
    {
        let mut encoder =
            png::Encoder::new(&mut png_bytes, image.width as u32, image.height as u32);
        encoder.set_color(png::ColorType::Rgba);
        encoder.set_depth(png::BitDepth::Eight);
        let mut writer = encoder.write_header().context("write PNG header")?;
        writer
            .write_image_data(image.bytes.as_ref())
            .context("write clipboard image PNG data")?;
    }
    Ok(Some(png_bytes))
}

pub fn ssh_command_args(host: &str, user: Option<&str>, port: Option<u16>) -> Result<Vec<String>> {
    validate_endpoint(host, port)?;
    let mut args = Vec::new();
    if let Some(port) = port {
        args.push("-p".to_string());
        args.push(port.to_string());
    }
    args.push(ssh_target(host, user));
    Ok(args)
}

fn run_ssh_command<T: RemoteSshEndpoint>(
    request: &T,
    remote_script: &str,
    stdin_data: &[u8],
) -> Result<Output> {
    let command = request
        .ssh_command()
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .unwrap_or("ssh");
    let mut args = ssh_command_args(request.host(), request.user(), request.port())?;
    args.push(remote_script.to_string());
    let mut child = Command::new(command)
        .args(args)
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .with_context(|| format!("spawn {command}"))?;
    let mut stdin = child
        .stdin
        .take()
        .ok_or_else(|| anyhow!("open {command} stdin"))?;
    stdin
        .write_all(stdin_data)
        .with_context(|| format!("write data to {command}"))?;
    drop(stdin);
    child
        .wait_with_output()
        .with_context(|| format!("wait for {command}"))
}

fn ensure_success(output: &Output, label: &str) -> Result<()> {
    if output.status.success() {
        return Ok(());
    }
    let stderr = stderr_lossy(&output);
    let stdout = stdout_lossy(&output);
    let detail = if !stderr.trim().is_empty() {
        stderr.trim()
    } else {
        stdout.trim()
    };
    if detail.is_empty() {
        bail!("{label} failed with status {}", output.status);
    }
    bail!("{label} failed with status {}: {detail}", output.status)
}

fn validate_endpoint(host: &str, port: Option<u16>) -> Result<()> {
    if host.trim().is_empty() {
        bail!("SSH target is missing a host");
    }
    if matches!(port, Some(0)) {
        bail!("SSH port must be between 1 and 65535");
    }
    Ok(())
}

fn ssh_target(host: &str, user: Option<&str>) -> String {
    user.map(str::trim)
        .filter(|value| !value.is_empty())
        .map(|user| format!("{user}@{}", host.trim()))
        .unwrap_or_else(|| host.trim().to_string())
}

fn normalized_remote_path(value: Option<&str>, fallback: &str, label: &str) -> Result<String> {
    let path = value
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .unwrap_or(fallback);
    if path.contains('\0') || path.contains('\n') || path.contains('\r') {
        bail!("{label} cannot contain control characters");
    }
    Ok(path.to_string())
}

#[cfg(feature = "gui")]
fn sanitized_remote_filename(filename: &str) -> Result<String> {
    let trimmed = filename.trim();
    if trimmed.is_empty() {
        bail!("remote staged filename is empty");
    }
    if trimmed == "." || trimmed == ".." || trimmed.contains('/') || trimmed.contains('\\') {
        bail!("remote staged filename must not contain path separators");
    }
    if !trimmed
        .bytes()
        .all(|byte| matches!(byte, b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'.' | b'_' | b'-'))
    {
        bail!("remote staged filename may only contain letters, numbers, '.', '_' and '-'");
    }
    Ok(trimmed.to_string())
}

fn bootstrap_remote_script(helper_path: &str, staging_dir: &str) -> String {
    format!(
        r#"set -e
{helper}
{staging}
helper_dir=$(dirname "$helper")
mkdir -p "$helper_dir" "$staging_dir"
tmp="$helper.tmp.$$"
base64 -d > "$tmp"
chmod 755 "$tmp"
mv "$tmp" "$helper"
printf 'onibi helper staged at %s\n' "$helper"
printf 'onibi staging dir %s\n' "$staging_dir"
"#,
        helper = remote_path_assignment("helper", helper_path),
        staging = remote_path_assignment("staging_dir", staging_dir),
    )
}

#[cfg(feature = "gui")]
fn stage_file_remote_script(staging_dir: &str, filename: &str) -> String {
    format!(
        r#"set -e
{staging}
filename={filename}
mkdir -p "$staging_dir"
path="$staging_dir/$filename"
base64 -d > "$path"
chmod 600 "$path"
printf '%s' "$path"
"#,
        staging = remote_path_assignment("staging_dir", staging_dir),
        filename = shell_single_quote(filename),
    )
}

fn remote_path_assignment(variable: &str, spec: &str) -> String {
    let spec_variable = format!("{variable}_spec");
    format!(
        r#"{spec_variable}={spec}
case "${{{spec_variable}}}" in
  "~") {variable}="$HOME" ;;
  "~/"*) {variable}="$HOME/${{{spec_variable}#~/}}" ;;
  *) {variable}="${{{spec_variable}}}" ;;
esac"#,
        spec = shell_single_quote(spec)
    )
}

fn shell_single_quote(value: &str) -> String {
    format!("'{}'", value.replace('\'', "'\\''"))
}

fn stdout_lossy(output: &Output) -> String {
    String::from_utf8_lossy(&output.stdout).to_string()
}

fn stderr_lossy(output: &Output) -> String {
    String::from_utf8_lossy(&output.stderr).to_string()
}

fn now_millis() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|duration| duration.as_millis().min(i64::MAX as u128) as i64)
        .unwrap_or(0)
}

trait RemoteSshEndpoint {
    fn host(&self) -> &str;
    fn user(&self) -> Option<&str>;
    fn port(&self) -> Option<u16>;
    fn ssh_command(&self) -> Option<&str>;
}

impl RemoteSshEndpoint for RemoteSshBootstrapRequest {
    fn host(&self) -> &str {
        &self.host
    }

    fn user(&self) -> Option<&str> {
        self.user.as_deref()
    }

    fn port(&self) -> Option<u16> {
        self.port
    }

    fn ssh_command(&self) -> Option<&str> {
        self.ssh_command.as_deref()
    }
}

#[cfg(feature = "gui")]
impl RemoteSshEndpoint for RemoteSshStageFileRequest {
    fn host(&self) -> &str {
        &self.host
    }

    fn user(&self) -> Option<&str> {
        self.user.as_deref()
    }

    fn port(&self) -> Option<u16> {
        self.port
    }

    fn ssh_command(&self) -> Option<&str> {
        self.ssh_command.as_deref()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn ssh_args_include_user_host_and_port() {
        assert_eq!(
            ssh_command_args("example.com", Some("alice"), Some(2222)).unwrap(),
            vec!["-p", "2222", "alice@example.com"]
        );
    }

    #[test]
    fn rejects_unsafe_stage_filenames() {
        assert!(sanitized_remote_filename("image.png").is_ok());
        assert!(sanitized_remote_filename("../image.png").is_err());
        assert!(sanitized_remote_filename("image name.png").is_err());
        assert!(sanitized_remote_filename("").is_err());
    }

    #[test]
    fn stage_script_expands_tilde_and_prints_remote_path() {
        let script = stage_file_remote_script("~/.onibi/staged", "image.png");
        assert!(script.contains("staging_dir=\"$HOME/${staging_dir_spec#~/}\""));
        assert!(script.contains("path=\"$staging_dir/$filename\""));
        assert!(script.contains("printf '%s' \"$path\""));
    }

    #[test]
    fn bootstrap_script_stages_helper_and_staging_dir() {
        let script = bootstrap_remote_script("~/.onibi/bin/onibi", "~/.onibi/staged");
        assert!(script.contains("helper_dir=$(dirname \"$helper\")"));
        assert!(script.contains("base64 -d > \"$tmp\""));
        assert!(script.contains("mv \"$tmp\" \"$helper\""));
        assert!(script.contains("mkdir -p \"$helper_dir\" \"$staging_dir\""));
    }
}
