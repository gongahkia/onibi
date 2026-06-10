use super::{Transport, TransportHandle, TransportStatus};
use anyhow::{anyhow, bail, Context, Result};
use serde_json::Value;
use std::{path::PathBuf, process::Stdio};
use tokio::{process::Command, sync::oneshot};

#[derive(Debug, Clone)]
pub struct TailscaleFunnel {
    binary_path: PathBuf,
}

impl Default for TailscaleFunnel {
    fn default() -> Self {
        Self {
            binary_path: PathBuf::from("tailscale"),
        }
    }
}

#[async_trait::async_trait]
impl Transport for TailscaleFunnel {
    fn name(&self) -> &'static str {
        "tailscale-funnel"
    }

    fn label(&self) -> &'static str {
        "Tailscale Funnel"
    }

    fn requires_external_dep(&self) -> Option<&'static str> {
        Some("tailscale")
    }

    async fn start(&self, local_port: u16) -> Result<TransportHandle> {
        self.ensure_installed()?;
        let status = self.run(&["status", "--json"]).await?;
        let dns_name = parse_dns_name(&status)?;
        let local_port = local_port.to_string();
        self.run(&["funnel", "--bg", &local_port])
            .await
            .map_err(|error| anyhow!(classify_funnel_error(&error.to_string())))?;

        let (shutdown, rx) = oneshot::channel();
        let binary_path = self.binary_path.clone();
        tokio::spawn(async move {
            let _ = rx.await;
            let _ = Command::new(binary_path)
                .args(["funnel", "reset"])
                .stdout(Stdio::null())
                .stderr(Stdio::null())
                .status()
                .await;
        });

        Ok(TransportHandle::new(
            Some(format!("https://{}/", dns_name.trim_end_matches('.'))),
            None,
            shutdown,
        ))
    }

    async fn status(&self) -> TransportStatus {
        if let Err(error) = self.ensure_installed() {
            return TransportStatus::failed(error.to_string());
        }
        match self.run(&["status", "--json"]).await {
            Ok(status) => {
                if let Err(error) = parse_dns_name(&status) {
                    return TransportStatus::failed(error.to_string());
                }
            }
            Err(error) => return TransportStatus::failed(error.to_string()),
        }

        match self.run(&["funnel", "status", "--json"]).await {
            Ok(raw) => match parse_funnel_url(&raw) {
                Some(url) => TransportStatus::Running {
                    url: Some(url),
                    fingerprint: None,
                },
                None => TransportStatus::Stopped,
            },
            Err(error) if is_funnel_not_configured(&error.to_string()) => TransportStatus::Stopped,
            Err(error) => TransportStatus::failed(error.to_string()),
        }
    }
}

impl TailscaleFunnel {
    fn ensure_installed(&self) -> Result<()> {
        which::which(&self.binary_path)
            .map(|_| ())
            .with_context(|| {
                "tailscale CLI not found on PATH; install Tailscale and run `tailscale up`"
            })
    }

    async fn run(&self, args: &[&str]) -> Result<String> {
        let output = Command::new(&self.binary_path)
            .args(args)
            .output()
            .await
            .with_context(|| format!("run tailscale {}", args.join(" ")))?;
        if output.status.success() {
            return Ok(String::from_utf8_lossy(&output.stdout).to_string());
        }
        let stderr = String::from_utf8_lossy(&output.stderr);
        bail!("tailscale {} failed: {}", args.join(" "), stderr.trim());
    }
}

pub fn parse_dns_name(raw: &str) -> Result<String> {
    let value: Value = serde_json::from_str(raw).context("parse tailscale status JSON")?;
    let state = value
        .get("BackendState")
        .and_then(Value::as_str)
        .unwrap_or_default();
    if !state.is_empty() && state != "Running" {
        bail!("tailscale is not logged in or not running (state: {state}); run `tailscale up`");
    }

    let dns_name = value
        .get("Self")
        .and_then(|self_node| self_node.get("DNSName"))
        .and_then(Value::as_str)
        .filter(|dns| !dns.trim().is_empty())
        .ok_or_else(|| {
            anyhow!("tailscale MagicDNS name unavailable; enable MagicDNS for Funnel")
        })?;

    Ok(dns_name.trim_end_matches('.').to_string())
}

pub fn parse_funnel_url(raw: &str) -> Option<String> {
    let value: Value = serde_json::from_str(raw).ok()?;
    first_https_ts_net(&value)
}

fn first_https_ts_net(value: &Value) -> Option<String> {
    match value {
        Value::String(raw) if raw.starts_with("https://") && raw.contains(".ts.net") => {
            Some(normalize_url(raw))
        }
        Value::Array(items) => items.iter().find_map(first_https_ts_net),
        Value::Object(map) => map.values().find_map(first_https_ts_net),
        _ => None,
    }
}

fn normalize_url(raw: &str) -> String {
    if raw.ends_with('/') {
        raw.to_string()
    } else {
        format!("{raw}/")
    }
}

fn is_funnel_not_configured(raw: &str) -> bool {
    let raw = raw.to_ascii_lowercase();
    raw.contains("not configured") || raw.contains("no funnel") || raw.contains("no serve config")
}

fn classify_funnel_error(raw: &str) -> String {
    let raw_lower = raw.to_ascii_lowercase();
    if raw_lower.contains("not logged in") || raw_lower.contains("needslogin") {
        return "tailscale is not logged in; run `tailscale up` and try again".to_string();
    }
    if raw_lower.contains("quota") || raw_lower.contains("rate limit") {
        return "Tailscale Funnel quota or certificate limit reached; use Cloudflare or LAN until it resets"
            .to_string();
    }
    raw.to_string()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_dns_name_from_status() {
        let raw = r#"{
            "BackendState": "Running",
            "Self": { "DNSName": "workstation.tailnet.ts.net." }
        }"#;

        assert_eq!(parse_dns_name(raw).unwrap(), "workstation.tailnet.ts.net");
    }

    #[test]
    fn parse_dns_name_rejects_logged_out_state() {
        let raw = r#"{
            "BackendState": "NeedsLogin",
            "Self": { "DNSName": "" }
        }"#;

        let error = parse_dns_name(raw).unwrap_err().to_string();
        assert!(error.contains("tailscale up"));
    }

    #[test]
    fn parse_funnel_url_finds_nested_url() {
        let raw = r#"{
            "Services": {
                "443": {
                    "URL": "https://workstation.tailnet.ts.net"
                }
            }
        }"#;

        assert_eq!(
            parse_funnel_url(raw).unwrap(),
            "https://workstation.tailnet.ts.net/"
        );
    }
}
