use crate::{adapters, secret, server, transport::TransportStatus};
use anyhow::{Context, Result};
use dialoguer::{theme::ColorfulTheme, MultiSelect};
use qrcode::{render::unicode, QrCode};
use serde_json::{json, Value};
use std::io::IsTerminal;

pub async fn run(port: u16, json_output: bool) -> Result<()> {
    if json_output {
        return run_json(port).await;
    }

    println!("Welcome to Onibi setup.");
    println!();

    print!("[1/4] Generating bearer token... ");
    let token = secret::load_or_create_token()?;
    let vapid = secret::load_or_create_vapid_keys()?;
    let state = server::AppState::from_config(port)?;
    println!("done");

    println!("[2/4] Detecting transports:");
    let daemon_running = super::healthz(port);
    if daemon_running {
        enable_daemon_transports(port);
    } else {
        print_local_transport_status(&state).await;
        println!(
            "      start `onibi --headless --auto-transports` or the desktop app to publish transports"
        );
    }

    println!("[3/4] Which agent do you want to install hooks for?");
    install_selected_adapters(interactive_terminal(), &token.token)?;

    println!("[4/4] Pair your phone:");
    let pair_uri = pairing_uri(port, &state, daemon_running).await?;
    println!("      Scan this QR with your phone camera:");
    println!("{}", ascii_qr(&pair_uri)?);
    println!("      Or open: {pair_uri}");
    println!();
    println!("Setup complete. Run `onibi status` to verify.");
    println!("config_dir={}", secret::config_dir()?.display());
    println!("db_path={}", state.store.path().display());
    println!("machine_id={}", state.machine_id);
    println!("token_source={:?}", token.source);
    println!("vapid_public_key={}", vapid.public_key);
    Ok(())
}

async fn run_json(port: u16) -> Result<()> {
    let token = secret::load_or_create_token()?;
    let vapid = secret::load_or_create_vapid_keys()?;
    let state = server::AppState::from_config(port)?;
    let daemon_running = super::healthz(port);
    let transports = if daemon_running {
        enable_daemon_transports_json(port)
    } else {
        state
            .transports
            .status_snapshot()
            .await
            .into_iter()
            .map(serde_json::to_value)
            .collect::<std::result::Result<Vec<_>, _>>()?
    };
    let adapters = install_selected_adapters_json(&token.token);
    let pair_uri = pairing_uri(port, &state, daemon_running).await?;
    println!(
        "{}",
        serde_json::to_string_pretty(&json!({
            "ok": true,
            "configDir": secret::config_dir()?,
            "dbPath": state.store.path(),
            "machineId": state.machine_id,
            "tokenSource": format!("{:?}", token.source),
            "vapidPublicKey": vapid.public_key,
            "daemonRunning": daemon_running,
            "transports": transports,
            "adapters": adapters.clone(),
            "integrations": adapters,
            "pairUri": pair_uri,
        }))?
    );
    Ok(())
}

fn enable_daemon_transports_json(port: u16) -> Vec<Value> {
    ["lan", "tailscale-funnel", "cloudflared"]
        .into_iter()
        .map(|name| {
            match super::authed_http(
                port,
                "POST",
                &format!("/v1/transport/{name}/enable"),
                Some("{}"),
            ) {
                Ok(raw) => serde_json::from_str::<Value>(&raw)
                    .unwrap_or_else(|_| json!({"name": name, "ok": true, "message": raw})),
                Err(error) => json!({"name": name, "ok": false, "error": error.to_string()}),
            }
        })
        .collect()
}

fn enable_daemon_transports(port: u16) {
    for name in ["lan", "tailscale-funnel", "cloudflared"] {
        match super::authed_http(
            port,
            "POST",
            &format!("/v1/transport/{name}/enable"),
            Some("{}"),
        ) {
            Ok(raw) => print_transport_value(name, serde_json::from_str(&raw).ok()),
            Err(error) => println!("      - {name}: {error}"),
        }
    }
}

async fn print_local_transport_status(state: &server::AppState) {
    for snapshot in state.transports.status_snapshot().await {
        let status = match &snapshot.status {
            TransportStatus::Stopped => "available".to_string(),
            TransportStatus::Starting => "starting".to_string(),
            TransportStatus::Running { url, .. } => {
                format!("running {}", url.as_deref().unwrap_or(""))
            }
            TransportStatus::Failed { message } => message.clone(),
        };
        println!("      - {}: {status}", snapshot.name);
    }
}

fn print_transport_value(name: &str, value: Option<Value>) {
    let Some(value) = value else {
        println!("      - {name}: enabled");
        return;
    };
    let url = value.get("url").and_then(Value::as_str);
    let status = value
        .get("status")
        .and_then(|status| status.get("state"))
        .and_then(Value::as_str)
        .unwrap_or("running");
    match url {
        Some(url) => println!("      - {name}: {status} {url}"),
        None => println!("      - {name}: {status}"),
    }
}

fn install_selected_adapters(interactive: bool, token: &str) -> Result<()> {
    let adapters = adapters::list();
    let supported = adapters
        .iter()
        .filter(|adapter| adapter.support == "full" || adapter.support == "bash-only")
        .cloned()
        .collect::<Vec<_>>();

    if supported.is_empty() {
        println!("      no installable adapters found");
        return Ok(());
    }

    let selected = if interactive {
        let labels = supported
            .iter()
            .map(|adapter| {
                let detected = adapter_detected(adapter.name);
                let suffix = if detected { " detected" } else { "" };
                format!("{} ({}){suffix}", adapter.name, adapter.support)
            })
            .collect::<Vec<_>>();
        let defaults = supported
            .iter()
            .map(|adapter| adapter.installed || adapter_detected(adapter.name))
            .collect::<Vec<_>>();
        MultiSelect::with_theme(&ColorfulTheme::default())
            .items(&labels)
            .defaults(&defaults)
            .interact()
            .context("read adapter selection")?
    } else {
        supported
            .iter()
            .enumerate()
            .filter_map(|(index, adapter)| adapter.installed.then_some(index))
            .collect()
    };

    if selected.is_empty() {
        println!("      no hooks installed");
        return Ok(());
    }

    for index in selected {
        let adapter = &supported[index];
        match adapters::install(adapter.name, token) {
            Ok(message) => println!("      {message}"),
            Err(error) => println!("      {}: {error:#}", adapter.name),
        }
    }
    Ok(())
}

fn install_selected_adapters_json(token: &str) -> Vec<Value> {
    adapters::list()
        .into_iter()
        .filter(|adapter| adapter.support == "full" || adapter.support == "bash-only")
        .filter(|adapter| adapter.installed || adapter_detected(adapter.name))
        .map(|adapter| match adapters::install(adapter.name, token) {
            Ok(message) => json!({
                "name": adapter.name,
                "ok": true,
                "message": message,
            }),
            Err(error) => json!({
                "name": adapter.name,
                "ok": false,
                "error": error.to_string(),
            }),
        })
        .collect()
}

async fn pairing_uri(port: u16, state: &server::AppState, daemon_running: bool) -> Result<String> {
    let host = if daemon_running {
        best_daemon_pair_host(port).unwrap_or_else(|| format!("http://127.0.0.1:{port}/"))
    } else {
        state
            .transports
            .pairing_payload()
            .await
            .transports
            .into_iter()
            .next()
            .map(|endpoint| endpoint.url)
            .unwrap_or_else(|| format!("http://127.0.0.1:{port}/"))
    };

    Ok(format!(
        "onibi://pair?token={}&baseUrl={}&machineId={}&vapidPublicKey={}",
        percent_encode(&state.token),
        percent_encode(&host),
        percent_encode(&state.machine_id),
        percent_encode(&state.vapid.public_key)
    ))
}

fn best_daemon_pair_host(port: u16) -> Option<String> {
    let raw = super::authed_http(port, "GET", "/v1/transport/status", None).ok()?;
    let transports = serde_json::from_str::<Vec<Value>>(&raw).ok()?;
    for preferred in ["tailscale-funnel", "cloudflared", "lan"] {
        if let Some(url) = transports.iter().find_map(|transport| {
            let name = transport.get("name").and_then(Value::as_str)?;
            let enabled = transport.get("enabled").and_then(Value::as_bool)?;
            let url = transport.get("url").and_then(Value::as_str)?;
            (name == preferred && enabled).then(|| url.to_string())
        }) {
            return Some(url);
        }
    }
    None
}

fn ascii_qr(payload: &str) -> Result<String> {
    let code = QrCode::new(payload.as_bytes()).context("build pairing QR")?;
    Ok(code.render::<unicode::Dense1x2>().quiet_zone(false).build())
}

fn percent_encode(raw: &str) -> String {
    let mut encoded = String::new();
    for byte in raw.bytes() {
        if byte.is_ascii_alphanumeric() || matches!(byte, b'-' | b'_' | b'.' | b'~') {
            encoded.push(byte as char);
        } else {
            encoded.push_str(&format!("%{byte:02X}"));
        }
    }
    encoded
}

fn adapter_detected(name: &str) -> bool {
    match name {
        "claude-code" => which::which("claude").is_ok(),
        "codex" => which::which("codex").is_ok(),
        _ => false,
    }
}

fn interactive_terminal() -> bool {
    std::io::stdin().is_terminal()
        && std::io::stdout().is_terminal()
        && std::env::var("ONIBI_NONINTERACTIVE").is_err()
}
