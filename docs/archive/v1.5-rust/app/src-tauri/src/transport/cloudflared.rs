use super::{Transport, TransportHandle, TransportStatus};
use anyhow::{anyhow, Context, Result};
use regex::Regex;
use std::{
    path::PathBuf,
    process::Stdio,
    sync::{
        atomic::{AtomicBool, Ordering},
        Arc,
    },
};
use tokio::{
    io::{AsyncBufRead, AsyncBufReadExt, BufReader},
    process::Command,
    sync::oneshot,
    time::{timeout, Duration},
};

#[derive(Debug, Clone)]
pub struct CloudflareTunnel {
    binary_path: PathBuf,
}

impl Default for CloudflareTunnel {
    fn default() -> Self {
        Self {
            binary_path: PathBuf::from("cloudflared"),
        }
    }
}

#[async_trait::async_trait]
impl Transport for CloudflareTunnel {
    fn name(&self) -> &'static str {
        "cloudflared"
    }

    fn label(&self) -> &'static str {
        "Cloudflare Tunnel"
    }

    fn requires_external_dep(&self) -> Option<&'static str> {
        Some("cloudflared")
    }

    async fn start(&self, local_port: u16) -> Result<TransportHandle> {
        self.ensure_installed()?;
        let target = format!("http://127.0.0.1:{local_port}");
        let mut child = Command::new(&self.binary_path)
            .args(["tunnel", "--url", &target, "--no-autoupdate"])
            .stdout(Stdio::null())
            .stderr(Stdio::piped())
            .spawn()
            .with_context(|| format!("spawn {}", self.binary_path.display()))?;
        let stderr = child
            .stderr
            .take()
            .context("cloudflared stderr was not captured")?;
        let url = match parse_url_from_stderr(BufReader::new(stderr)).await {
            Ok(url) => url,
            Err(error) => {
                let _ = child.kill().await;
                return Err(error);
            }
        };

        let (shutdown, rx) = oneshot::channel();
        let alive = Arc::new(AtomicBool::new(true));
        let alive_task = alive.clone();
        tokio::spawn(async move {
            tokio::select! {
                _ = rx => {
                    let _ = child.kill().await;
                }
                _ = child.wait() => {}
            }
            alive_task.store(false, Ordering::Relaxed);
        });

        Ok(TransportHandle::with_alive(
            Some(url),
            None,
            shutdown,
            alive,
        ))
    }

    async fn status(&self) -> TransportStatus {
        match self.ensure_installed() {
            Ok(()) => TransportStatus::Stopped,
            Err(error) => TransportStatus::failed(error.to_string()),
        }
    }
}

impl CloudflareTunnel {
    fn ensure_installed(&self) -> Result<()> {
        which::which(&self.binary_path)
            .map(|_| ())
            .with_context(|| {
                "cloudflared not found on PATH; install cloudflared to use quick tunnels"
            })
    }
}

pub async fn parse_url_from_stderr<R>(reader: R) -> Result<String>
where
    R: AsyncBufRead + Unpin,
{
    timeout(Duration::from_secs(30), async move {
        let mut lines = reader.lines();
        while let Some(line) = lines.next_line().await? {
            if let Some(url) = parse_url_from_text(&line) {
                return Ok(url);
            }
        }
        Err(anyhow!(
            "cloudflared exited before printing a trycloudflare.com URL"
        ))
    })
    .await
    .context("timed out waiting for cloudflared quick-tunnel URL")?
}

pub fn parse_url_from_text(raw: &str) -> Option<String> {
    let pattern = Regex::new(r"https://[a-z0-9-]+\.trycloudflare\.com").ok()?;
    pattern.find(raw).map(|mat| {
        let url = mat.as_str();
        if url.ends_with('/') {
            url.to_string()
        } else {
            format!("{url}/")
        }
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use tokio::io::BufReader;

    #[test]
    fn parse_url_from_text_finds_first_trycloudflare_url() {
        let raw = "INF +--------------------------------------------------------------------------------------------+
        https://random-name.trycloudflare.com
        +--------------------------------------------------------------------------------------------+";

        assert_eq!(
            parse_url_from_text(raw).unwrap(),
            "https://random-name.trycloudflare.com/"
        );
    }

    #[tokio::test]
    async fn parse_url_from_stderr_reads_lines() {
        let raw = b"2026-05-27 INF starting\n2026-05-27 INF https://abc-123.trycloudflare.com\n";
        let url = parse_url_from_stderr(BufReader::new(&raw[..]))
            .await
            .unwrap();

        assert_eq!(url, "https://abc-123.trycloudflare.com/");
    }
}
