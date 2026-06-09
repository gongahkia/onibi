use anyhow::{anyhow, bail, Context, Result};
use base64::{engine::general_purpose::STANDARD, Engine as _};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::{
    io::Write,
    process::{Command, Output, Stdio},
    time::{SystemTime, UNIX_EPOCH},
};
use ts_rs::TS;

pub const DEFAULT_REMOTE_HELPER_PATH: &str = "~/.onibi/bin/onibi";
pub const DEFAULT_REMOTE_STAGING_DIR: &str = "~/.onibi/staged";
pub const DEFAULT_REMOTE_RUN_DIR: &str = "~/.onibi/run";

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq, Eq, TS)]
#[serde(rename_all = "camelCase")]
pub struct RemoteSshBootstrapRequest {
    pub target: String,
    #[serde(default)]
    #[ts(optional)]
    pub user: Option<String>,
    pub host: String,
    #[serde(default)]
    #[ts(optional)]
    pub port: Option<u16>,
    #[serde(default)]
    #[ts(optional)]
    pub remote_cwd: Option<String>,
    #[serde(default)]
    #[ts(optional)]
    pub ssh_command: Option<String>,
    #[serde(default)]
    #[ts(optional)]
    pub helper_path: Option<String>,
    #[serde(default)]
    #[ts(optional)]
    pub staging_dir: Option<String>,
}

#[derive(Clone, Debug, Serialize, PartialEq, Eq, TS)]
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

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq, Eq, TS)]
#[serde(rename_all = "camelCase")]
pub struct RemoteSshDaemonRequest {
    pub target: String,
    #[serde(default)]
    #[ts(optional)]
    pub user: Option<String>,
    pub host: String,
    #[serde(default)]
    #[ts(optional)]
    pub port: Option<u16>,
    #[serde(default)]
    #[ts(optional)]
    pub remote_cwd: Option<String>,
    #[serde(default)]
    #[ts(optional)]
    pub ssh_command: Option<String>,
    #[serde(default)]
    #[ts(optional)]
    pub helper_path: Option<String>,
    #[serde(default)]
    #[ts(optional)]
    pub run_dir: Option<String>,
}

#[derive(Clone, Debug, Serialize, PartialEq, Eq, TS)]
#[serde(rename_all = "camelCase")]
pub struct RemoteSshDaemonResult {
    pub ok: bool,
    pub target: String,
    pub helper_path: String,
    pub run_dir: String,
    pub pid: u32,
    pub status: String,
    pub log_path: String,
    pub started_at: i64,
    pub stdout: String,
    pub stderr: String,
}

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq, Eq, TS)]
#[serde(rename_all = "camelCase")]
pub struct RemoteSshDaemonSessionRequest {
    pub target: String,
    #[serde(default)]
    #[ts(optional)]
    pub user: Option<String>,
    pub host: String,
    #[serde(default)]
    #[ts(optional)]
    pub port: Option<u16>,
    pub workspace: String,
    #[serde(default)]
    #[ts(optional)]
    pub remote_cwd: Option<String>,
    #[serde(default)]
    #[ts(optional)]
    pub ssh_command: Option<String>,
    #[serde(default)]
    #[ts(optional)]
    pub helper_path: Option<String>,
    #[serde(default)]
    #[ts(optional)]
    pub staging_dir: Option<String>,
    #[serde(default)]
    #[ts(optional)]
    pub run_dir: Option<String>,
    #[serde(default)]
    #[ts(optional)]
    pub name: Option<String>,
    #[serde(default)]
    #[ts(optional)]
    pub agent: Option<String>,
    #[serde(default)]
    #[ts(optional)]
    pub prompt: Option<String>,
}

#[derive(Clone, Debug, Serialize, PartialEq, Eq, TS)]
#[serde(rename_all = "camelCase")]
pub struct RemoteSshDaemonSessionResult {
    pub ok: bool,
    pub target: String,
    pub helper_path: String,
    pub run_dir: String,
    pub pid: u32,
    pub status: String,
    pub log_path: String,
    pub started_at: i64,
    pub remote_session_id: String,
    pub attach_command: String,
    pub attach_args: Vec<String>,
    pub stdout: String,
    pub stderr: String,
}

#[derive(Clone, Debug, Deserialize, Serialize, PartialEq, Eq, TS)]
#[serde(rename_all = "camelCase")]
#[cfg(feature = "gui")]
pub struct RemoteSshStageFileRequest {
    pub target: String,
    #[serde(default)]
    #[ts(optional)]
    pub user: Option<String>,
    pub host: String,
    #[serde(default)]
    #[ts(optional)]
    pub port: Option<u16>,
    #[serde(default)]
    #[ts(optional)]
    pub remote_cwd: Option<String>,
    #[serde(default)]
    #[ts(optional)]
    pub ssh_command: Option<String>,
    #[serde(default)]
    #[ts(optional)]
    pub staging_dir: Option<String>,
    pub filename: String,
    pub data: Vec<u8>,
}

#[derive(Clone, Debug, Serialize, PartialEq, Eq, TS)]
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

pub fn remote_ssh_daemon(request: RemoteSshDaemonRequest) -> Result<RemoteSshDaemonResult> {
    validate_endpoint(&request.host, request.port)?;
    let helper_path = normalized_remote_path(
        request.helper_path.as_deref(),
        DEFAULT_REMOTE_HELPER_PATH,
        "helper path",
    )?;
    let run_dir = normalized_remote_path(
        request.run_dir.as_deref(),
        DEFAULT_REMOTE_RUN_DIR,
        "run directory",
    )?;
    let output = run_ssh_command(
        &request,
        &daemon_remote_script(&helper_path, &run_dir, request.remote_cwd.as_deref()),
        &[],
    )?;
    ensure_success(&output, "remote Onibi daemon start")?;
    let stdout = stdout_lossy(&output);
    let (pid, status, log_path) = parse_daemon_stdout(&stdout)?;
    Ok(RemoteSshDaemonResult {
        ok: true,
        target: request.target.trim().to_string(),
        helper_path,
        run_dir,
        pid,
        status,
        log_path,
        started_at: now_millis(),
        stdout,
        stderr: stderr_lossy(&output),
    })
}

pub fn remote_ssh_daemon_session(
    request: RemoteSshDaemonSessionRequest,
) -> Result<RemoteSshDaemonSessionResult> {
    validate_endpoint(&request.host, request.port)?;
    let workspace = normalized_remote_path(Some(&request.workspace), "", "workspace")?;
    if workspace.is_empty() {
        bail!("remote workspace is required");
    }
    let bootstrap = remote_ssh_bootstrap(RemoteSshBootstrapRequest {
        target: request.target.clone(),
        user: request.user.clone(),
        host: request.host.clone(),
        port: request.port,
        remote_cwd: request.remote_cwd.clone().or_else(|| Some(workspace.clone())),
        ssh_command: request.ssh_command.clone(),
        helper_path: request.helper_path.clone(),
        staging_dir: request.staging_dir.clone(),
    })?;
    let daemon = remote_ssh_daemon(RemoteSshDaemonRequest {
        target: request.target.clone(),
        user: request.user.clone(),
        host: request.host.clone(),
        port: request.port,
        remote_cwd: request.remote_cwd.clone().or_else(|| Some(workspace.clone())),
        ssh_command: request.ssh_command.clone(),
        helper_path: Some(bootstrap.helper_path.clone()),
        run_dir: request.run_dir.clone(),
    })?;
    let launch = run_ssh_command(
        &request,
        &daemon_session_launch_script(
            &bootstrap.helper_path,
            &workspace,
            request.name.as_deref(),
            request.agent.as_deref(),
            request.prompt.as_deref(),
        ),
        &[],
    )?;
    ensure_success(&launch, "remote Onibi daemon session launch")?;
    let launch_stdout = stdout_lossy(&launch);
    let remote_session_id = parse_remote_session_id(&launch_stdout)?;
    let attach_command = request
        .ssh_command
        .as_deref()
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .unwrap_or("ssh")
        .to_string();
    Ok(RemoteSshDaemonSessionResult {
        ok: true,
        target: request.target.trim().to_string(),
        helper_path: bootstrap.helper_path.clone(),
        run_dir: daemon.run_dir,
        pid: daemon.pid,
        status: daemon.status,
        log_path: daemon.log_path,
        started_at: daemon.started_at,
        remote_session_id: remote_session_id.clone(),
        attach_command,
        attach_args: remote_session_stream_args(
            &request.host,
            request.user.as_deref(),
            request.port,
            &bootstrap.helper_path,
            &remote_session_id,
        )?,
        stdout: format!("{}{}", daemon.stdout, launch_stdout),
        stderr: format!("{}{}", daemon.stderr, stderr_lossy(&launch)),
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

fn daemon_remote_script(helper_path: &str, run_dir: &str, remote_cwd: Option<&str>) -> String {
    let cwd_setup = remote_cwd
        .map(|cwd| {
            format!(
                r#"{cwd}
cd "$remote_cwd"
"#,
                cwd = remote_path_assignment("remote_cwd", cwd)
            )
        })
        .unwrap_or_default();
    format!(
        r#"set -e
{helper}
{run_dir}
{cwd_setup}if [ ! -x "$helper" ]; then
  echo "remote Onibi helper is not executable: $helper" >&2
  exit 1
fi
mkdir -p "$run_dir"
pid_file="$run_dir/onibi.pid"
log_file="$run_dir/onibi.log"
if [ -s "$pid_file" ]; then
  old_pid=$(cat "$pid_file" 2>/dev/null || true)
  case "$old_pid" in
    ''|*[!0-9]*) old_pid="" ;;
  esac
  if [ -n "$old_pid" ] && kill -0 "$old_pid" 2>/dev/null; then
    printf 'pid=%s\n' "$old_pid"
    printf 'status=running\n'
    printf 'log=%s\n' "$log_file"
    exit 0
  fi
fi
nohup "$helper" --headless > "$log_file" 2>&1 < /dev/null &
pid=$!
printf '%s\n' "$pid" > "$pid_file"
sleep 1
if ! kill -0 "$pid" 2>/dev/null; then
  echo "remote Onibi daemon failed to stay running" >&2
  if [ -s "$log_file" ]; then tail -n 20 "$log_file" >&2; fi
  exit 1
fi
printf 'pid=%s\n' "$pid"
printf 'status=started\n'
printf 'log=%s\n' "$log_file"
"#,
        helper = remote_path_assignment("helper", helper_path),
        run_dir = remote_path_assignment("run_dir", run_dir),
    )
}

fn daemon_session_launch_script(
    helper_path: &str,
    workspace: &str,
    name: Option<&str>,
    agent: Option<&str>,
    prompt: Option<&str>,
) -> String {
    let name = name.map(str::trim).filter(|value| !value.is_empty());
    let agent = agent
        .map(str::trim)
        .filter(|value| !value.is_empty() && *value != "shell");
    let prompt = prompt.map(str::trim).filter(|value| !value.is_empty());
    format!(
        r#"set -e
{helper}
{workspace}
if [ ! -x "$helper" ]; then
  echo "remote Onibi helper is not executable: $helper" >&2
  exit 1
fi
set -- "$helper" --json session launch --workspace "$workspace"
{name_arg}{agent_arg}{prompt_arg}"$@"
"#,
        helper = remote_path_assignment("helper", helper_path),
        workspace = remote_path_assignment("workspace", workspace),
        name_arg = name
            .map(|value| format!("set -- \"$@\" --name {}\n", shell_single_quote(value)))
            .unwrap_or_default(),
        agent_arg = agent
            .map(|value| format!("set -- \"$@\" --agent {}\n", shell_single_quote(value)))
            .unwrap_or_default(),
        prompt_arg = prompt
            .map(|value| format!("set -- \"$@\" --prompt {}\n", shell_single_quote(value)))
            .unwrap_or_default(),
    )
}

pub fn remote_session_stream_args(
    host: &str,
    user: Option<&str>,
    port: Option<u16>,
    helper_path: &str,
    session_id: &str,
) -> Result<Vec<String>> {
    validate_endpoint(host, port)?;
    if session_id.trim().is_empty() || session_id.contains('\0') {
        bail!("remote session id is required");
    }
    let mut args = Vec::new();
    if let Some(port) = port {
        args.push("-p".to_string());
        args.push(port.to_string());
    }
    args.push("-t".to_string());
    args.push(ssh_target(host, user));
    args.push(remote_session_stream_command(helper_path, session_id));
    Ok(args)
}

fn remote_session_stream_command(helper_path: &str, session_id: &str) -> String {
    format!(
        r#"{helper}
"$helper" session stream {session_id}"#,
        helper = remote_path_assignment("helper", helper_path),
        session_id = shell_single_quote(session_id),
    )
}

fn parse_remote_session_id(stdout: &str) -> Result<String> {
    let payload: Value = serde_json::from_str(stdout.trim())
        .context("parse remote Onibi session launch response")?;
    let id = payload
        .get("session")
        .and_then(|session| session.get("id"))
        .and_then(Value::as_str)
        .or_else(|| payload.get("sessionId").and_then(Value::as_str))
        .or_else(|| payload.get("id").and_then(Value::as_str))
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .ok_or_else(|| anyhow!("remote session launch did not return a session id"))?;
    Ok(id.to_string())
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

fn parse_daemon_stdout(stdout: &str) -> Result<(u32, String, String)> {
    let mut pid = None;
    let mut status = None;
    let mut log_path = None;
    for line in stdout.lines() {
        if let Some(value) = line.strip_prefix("pid=") {
            pid = Some(
                value
                    .trim()
                    .parse::<u32>()
                    .context("parse remote daemon pid")?,
            );
        } else if let Some(value) = line.strip_prefix("status=") {
            status = Some(value.trim().to_string());
        } else if let Some(value) = line.strip_prefix("log=") {
            log_path = Some(value.trim().to_string());
        }
    }
    let pid = pid.ok_or_else(|| anyhow!("remote daemon start did not return a pid"))?;
    let status = status.ok_or_else(|| anyhow!("remote daemon start did not return a status"))?;
    let log_path =
        log_path.ok_or_else(|| anyhow!("remote daemon start did not return a log path"))?;
    Ok((pid, status, log_path))
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

impl RemoteSshEndpoint for RemoteSshDaemonRequest {
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

impl RemoteSshEndpoint for RemoteSshDaemonSessionRequest {
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

    #[test]
    fn daemon_script_starts_headless_helper_and_reuses_live_pid() {
        let script = daemon_remote_script("~/.onibi/bin/onibi", "~/.onibi/run", Some("~/repo"));
        assert!(script.contains("cd \"$remote_cwd\""));
        assert!(script.contains("kill -0 \"$old_pid\""));
        assert!(script.contains("nohup \"$helper\" --headless"));
        assert!(script.contains("pid_file=\"$run_dir/onibi.pid\""));
        assert!(script.contains("log_file=\"$run_dir/onibi.log\""));
    }

    #[test]
    fn daemon_session_launch_script_invokes_helper_session_launch() {
        let script = daemon_session_launch_script(
            "~/.onibi/bin/onibi",
            "~/repo",
            Some("remote task"),
            Some("claude-code"),
            Some("continue"),
        );
        assert!(script.contains("--json session launch --workspace \"$workspace\""));
        assert!(script.contains("set -- \"$@\" --name 'remote task'"));
        assert!(script.contains("set -- \"$@\" --agent 'claude-code'"));
        assert!(script.contains("set -- \"$@\" --prompt 'continue'"));
    }

    #[test]
    fn remote_stream_args_build_tty_ssh_command() {
        let args = remote_session_stream_args(
            "example.com",
            Some("alice"),
            Some(2222),
            "~/.onibi/bin/onibi",
            "session-1",
        )
        .unwrap();
        assert_eq!(args[0], "-p");
        assert_eq!(args[1], "2222");
        assert_eq!(args[2], "-t");
        assert_eq!(args[3], "alice@example.com");
        assert!(args[4].contains("session stream 'session-1'"));
    }

    #[test]
    fn parses_remote_session_launch_response() {
        assert_eq!(
            parse_remote_session_id(r#"{"session":{"id":"remote-1"}}"#).unwrap(),
            "remote-1"
        );
        assert_eq!(
            parse_remote_session_id(r#"{"sessionId":"remote-2"}"#).unwrap(),
            "remote-2"
        );
    }

    #[test]
    fn parses_remote_daemon_start_output() {
        let (pid, status, log_path) =
            parse_daemon_stdout("pid=42\nstatus=started\nlog=/home/alice/.onibi/run/onibi.log\n")
                .unwrap();
        assert_eq!(pid, 42);
        assert_eq!(status, "started");
        assert_eq!(log_path, "/home/alice/.onibi/run/onibi.log");
    }
}
