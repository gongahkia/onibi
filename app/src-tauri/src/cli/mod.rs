pub mod doctor;
pub mod setup;
pub mod status;

use crate::{adapters, config, headless, orchestration, secret, server, transport, util};
use anyhow::{bail, Context, Result};
use clap::{CommandFactory, Parser, Subcommand};
use serde_json::{json, Value};
use std::{io::Write, net::TcpStream, path::PathBuf};

#[derive(Debug, Parser)]
#[command(
    name = "onibi",
    version,
    about = "Onibi local approval daemon and adapter CLI"
)]
pub struct Cli {
    #[arg(long, help = "Run the approval server without launching the Tauri UI")]
    headless: bool,
    #[arg(long, help = "Override the local approval server port")]
    port: Option<u16>,
    #[arg(long, help = "Print the default ~/.config/onibi/config.toml and exit")]
    default_config: bool,
    #[arg(
        long,
        help = "Enable LAN, Tailscale Funnel, and Cloudflare Tunnel at headless startup"
    )]
    auto_transports: bool,
    #[arg(long, help = "Override the Onibi config directory")]
    config_dir: Option<PathBuf>,
    #[arg(long, global = true, help = "Emit machine-readable JSON output")]
    json: bool,
    #[command(subcommand)]
    command: Option<Command>,
}

#[derive(Debug, Subcommand)]
enum Command {
    Setup,
    Status,
    Doctor,
    Token {
        #[command(subcommand)]
        command: TokenCommand,
    },
    Adapter {
        #[command(subcommand)]
        command: AdapterCommand,
    },
    Transport {
        #[command(subcommand)]
        command: TransportCommand,
    },
    Session {
        #[command(subcommand)]
        command: SessionCommand,
    },
    Pane {
        #[command(subcommand)]
        command: PaneCommand,
    },
    Wait {
        #[command(subcommand)]
        command: WaitCommand,
    },
    Agent {
        #[command(subcommand)]
        command: AgentCommand,
    },
    Events {
        #[command(subcommand)]
        command: EventsCommand,
    },
    Attention,
    Arrangement {
        #[command(subcommand)]
        command: ArrangementCommand,
    },
    #[command(name = "_hook", hide = true)]
    Hook {
        name: String,
    },
}

#[derive(Debug, Subcommand)]
enum TokenCommand {
    Rotate,
    Show,
}

#[derive(Debug, Subcommand)]
enum AdapterCommand {
    List,
    Install { name: String },
    Uninstall { name: String },
}

#[derive(Debug, Subcommand)]
enum TransportCommand {
    List,
    Enable { name: String },
    Disable { name: String },
    Status,
}

#[derive(Debug, Subcommand)]
enum SessionCommand {
    List,
    Launch {
        #[arg(long)]
        name: Option<String>,
        #[arg(long)]
        agent: Option<String>,
        #[arg(long)]
        workspace: PathBuf,
        #[arg(long)]
        prompt: Option<String>,
    },
    Attach {
        id: String,
    },
    Stop {
        id: String,
    },
    Send {
        id: String,
        text: Vec<String>,
    },
    Focus {
        id: String,
    },
}

#[derive(Debug, Subcommand)]
enum PaneCommand {
    Read {
        id: String,
        #[arg(long, default_value = "recent")]
        source: String,
        #[arg(long, default_value = "text")]
        format: String,
        #[arg(long, default_value_t = 0)]
        limit: usize,
    },
    SendKeys {
        id: String,
        keys: Vec<String>,
    },
    Split {
        id: String,
        #[arg(long, default_value = "vertical")]
        direction: String,
    },
    Focus {
        id: String,
    },
    Maximize {
        id: String,
    },
}

#[derive(Debug, Subcommand)]
enum WaitCommand {
    Output {
        #[arg(long)]
        pane: Option<String>,
        #[arg(long)]
        session: Option<String>,
        #[arg(long)]
        agent: Option<String>,
        #[arg(long = "match")]
        match_text: Option<String>,
        #[arg(long)]
        regex: Option<String>,
        #[arg(long, default_value_t = 30_000)]
        timeout_ms: u64,
    },
    AgentStatus {
        #[arg(long)]
        agent: Option<String>,
        #[arg(long)]
        session: Option<String>,
        #[arg(long)]
        status: String,
        #[arg(long, default_value_t = 30_000)]
        timeout_ms: u64,
    },
}

#[derive(Debug, Subcommand)]
enum AgentCommand {
    List,
    Read {
        id: String,
        #[arg(long, default_value = "recent")]
        source: String,
        #[arg(long, default_value = "text")]
        format: String,
    },
    Send {
        id: String,
        text: Vec<String>,
    },
    Focus {
        id: String,
    },
    Start {
        agent: Option<String>,
        #[arg(long)]
        workspace: Option<PathBuf>,
        #[arg(long)]
        prompt: Option<String>,
    },
}

#[derive(Debug, Subcommand)]
enum EventsCommand {
    Subscribe,
}

#[derive(Debug, Subcommand)]
enum ArrangementCommand {
    Restore { id_or_name: String },
}

pub fn should_dispatch(args: &[String]) -> bool {
    const COMMANDS: &[&str] = &[
        "setup",
        "status",
        "doctor",
        "token",
        "adapter",
        "transport",
        "session",
        "pane",
        "wait",
        "agent",
        "events",
        "attention",
        "arrangement",
        "_hook",
    ];

    args.iter().skip(1).any(|arg| {
        matches!(
            arg.as_str(),
            "--headless" | "--default-config" | "--help" | "-h" | "--version" | "-V"
        ) || COMMANDS.contains(&arg.as_str())
    })
}

pub fn run_blocking(args: Vec<String>) -> Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| "info,onibi=debug".into()),
        )
        .try_init()
        .ok();

    let cli = Cli::parse_from(args);
    let runtime = tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .build()
        .context("create tokio runtime")?;
    runtime.block_on(run(cli))
}

async fn run(cli: Cli) -> Result<()> {
    if let Some(config_dir) = cli.config_dir.as_ref() {
        std::env::set_var("ONIBI_CONFIG_DIR", config_dir);
    }

    if cli.default_config {
        print!("{}", config::default_config_toml());
        return Ok(());
    }

    if cli.headless {
        return headless::run(headless::HeadlessOpts {
            config_dir: None,
            port: cli.port,
            auto_transports: cli.auto_transports,
        })
        .await;
    }

    let port = cli.port.unwrap_or_else(|| {
        config::load()
            .map(|config| config.server_port())
            .unwrap_or(config::DEFAULT_PORT)
    });

    match cli.command {
        Some(Command::Setup) => setup::run(port, cli.json).await,
        Some(Command::Status) => status::run(port, cli.json).await,
        Some(Command::Doctor) => doctor::run(port, cli.json).await,
        Some(Command::Token { command }) => token(command, cli.json),
        Some(Command::Adapter { command }) => adapter(command, cli.json),
        Some(Command::Transport { command }) => transport(command, port, cli.json).await,
        Some(Command::Session { command }) => session(command, port, cli.json).await,
        Some(Command::Pane { command }) => pane(command, port, cli.json).await,
        Some(Command::Wait { command }) => wait(command, cli.json).await,
        Some(Command::Agent { command }) => agent(command, port, cli.json).await,
        Some(Command::Events { command }) => events(command, cli.json).await,
        Some(Command::Attention) => desktop_get(port, "/v1/desktop/attention", cli.json),
        Some(Command::Arrangement { command }) => arrangement(command, port, cli.json),
        Some(Command::Hook { name }) => hook(&name, port),
        None => {
            Cli::command().print_help()?;
            println!();
            Ok(())
        }
    }
}

async fn session(command: SessionCommand, port: u16, json_output: bool) -> Result<()> {
    match command {
        SessionCommand::List => print_orchestration("session.list", json!({}), json_output).await,
        SessionCommand::Launch {
            name,
            agent,
            workspace,
            prompt,
        } => {
            let command = agent.clone().unwrap_or_else(util::shell::default_shell);
            let args = prompt.into_iter().collect::<Vec<_>>();
            let response = orchestration::client::request(
                "pty.spawn",
                json!({
                    "command": command,
                    "args": args,
                    "cwd": workspace.display().to_string(),
                    "name": name,
                    "agent": agent,
                    "workspaceId": format!("workspace:{}", workspace.display()),
                }),
            )
            .await?;
            print_value(response, json_output)?;
            Ok(())
        }
        SessionCommand::Attach { id } => {
            let mut response =
                orchestration::client::request("session.attach", json!({"id": id})).await?;
            if healthz(port) {
                if let Some(session_id) = response
                    .get("session")
                    .and_then(|session| session.get("id"))
                    .and_then(Value::as_str)
                {
                    let focus = authed_http(
                        port,
                        "POST",
                        &format!("/v1/desktop/session/{}/focus", path_segment(session_id)),
                        Some("{}"),
                    )?;
                    if let Ok(value) = serde_json::from_str::<Value>(&focus) {
                        response["desktopFocus"] = value;
                    }
                }
            }
            print_value(response, json_output)
        }
        SessionCommand::Stop { id } => {
            print_orchestration("session.stop", json!({"id": id}), json_output).await
        }
        SessionCommand::Send { id, text } => {
            print_orchestration(
                "pty.write",
                json!({"id": id, "text": text.join(" ")}),
                json_output,
            )
            .await
        }
        SessionCommand::Focus { id } => {
            ensure_daemon_running(port)?;
            let response = authed_http(
                port,
                "POST",
                &format!("/v1/desktop/session/{}/focus", path_segment(&id)),
                Some("{}"),
            )?;
            print_raw_json_or_text(&response, json_output)
        }
    }
}

async fn pane(command: PaneCommand, port: u16, json_output: bool) -> Result<()> {
    ensure_daemon_running(port)?;
    match command {
        PaneCommand::Read {
            id,
            source,
            format,
            limit,
        } => {
            let response = orchestration::client::request(
                "pane.read",
                json!({"id": id, "source": source, "format": format, "limit": limit}),
            )
            .await?;
            if json_output {
                print_value(response, true)
            } else {
                println!(
                    "{}",
                    response.get("text").and_then(Value::as_str).unwrap_or("")
                );
                Ok(())
            }
        }
        PaneCommand::SendKeys { id, keys } => {
            print_orchestration(
                "pane.send_keys",
                json!({"id": id, "keys": keys}),
                json_output,
            )
            .await
        }
        PaneCommand::Split { id, direction } => {
            let body = json!({
                "protocol_version": "1.0",
                "direction": direction,
            });
            print_raw_json_or_text(
                &authed_http(
                    port,
                    "POST",
                    &format!("/v1/desktop/pane/{}/split", path_segment(&id)),
                    Some(&body.to_string()),
                )?,
                json_output,
            )
        }
        PaneCommand::Focus { id } => print_raw_json_or_text(
            &authed_http(
                port,
                "POST",
                &format!("/v1/desktop/pane/{}/focus", path_segment(&id)),
                Some("{}"),
            )?,
            json_output,
        ),
        PaneCommand::Maximize { id } => print_raw_json_or_text(
            &authed_http(
                port,
                "POST",
                &format!("/v1/desktop/pane/{}/maximize", path_segment(&id)),
                Some("{}"),
            )?,
            json_output,
        ),
    }
}

fn arrangement(command: ArrangementCommand, port: u16, json_output: bool) -> Result<()> {
    match command {
        ArrangementCommand::Restore { id_or_name } => {
            ensure_daemon_running(port)?;
            print_raw_json_or_text(
                &authed_http(
                    port,
                    "POST",
                    &format!(
                        "/v1/desktop/arrangement/{}/restore",
                        path_segment(&id_or_name)
                    ),
                    Some("{}"),
                )?,
                json_output,
            )
        }
    }
}

fn desktop_get(port: u16, path: &str, json_output: bool) -> Result<()> {
    ensure_daemon_running(port)?;
    print_raw_json_or_text(&authed_http(port, "GET", path, None)?, json_output)
}

fn path_segment(value: &str) -> String {
    value
        .bytes()
        .flat_map(|byte| match byte {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => {
                vec![byte as char]
            }
            _ => format!("%{byte:02X}").chars().collect(),
        })
        .collect()
}

fn token(command: TokenCommand, json_output: bool) -> Result<()> {
    match command {
        TokenCommand::Rotate => {
            let token = secret::rotate_token()?;
            print_value(json!({"token": token.token}), json_output)?;
        }
        TokenCommand::Show => {
            let token = secret::load_or_create_token()?;
            print_value(json!({"token": token.token}), json_output)?;
        }
    }
    Ok(())
}

fn adapter(command: AdapterCommand, json_output: bool) -> Result<()> {
    match command {
        AdapterCommand::List => {
            let adapters = adapters::list();
            if json_output {
                print_value(serde_json::to_value(adapters)?, true)?;
            } else {
                for adapter in adapters {
                    println!(
                        "{}\tsupport={}\tinstalled={}",
                        adapter.name, adapter.support, adapter.installed
                    );
                }
            }
        }
        AdapterCommand::Install { name } => {
            let token = secret::load_or_create_token()?.token;
            let message = adapters::install(&name, &token)?;
            if json_output {
                print_value(json!({"ok": true, "name": name, "message": message}), true)?;
            } else {
                println!("{message}");
            }
        }
        AdapterCommand::Uninstall { name } => {
            let message = adapters::uninstall(&name)?;
            if json_output {
                print_value(json!({"ok": true, "name": name, "message": message}), true)?;
            } else {
                println!("{message}");
            }
        }
    }
    Ok(())
}

async fn transport(command: TransportCommand, port: u16, json_output: bool) -> Result<()> {
    match command {
        TransportCommand::List => {
            let names = transport::default_transport_names();
            if json_output {
                print_value(json!({"transports": names}), true)?;
            } else {
                for name in names {
                    println!("{name}");
                }
            }
        }
        TransportCommand::Status => {
            if healthz(port) {
                print_raw_json_or_text(
                    &authed_http(port, "GET", "/v1/transport/status", None)?,
                    json_output,
                )?;
            } else {
                let state = server::AppState::from_config(port)?;
                let snapshots = state.transports.status_snapshot().await;
                if json_output {
                    print_value(serde_json::to_value(snapshots)?, true)?;
                } else {
                    for snapshot in snapshots {
                        println!(
                            "{}\tenabled={}\tstatus={}",
                            snapshot.name,
                            snapshot.enabled,
                            serde_json::to_string(&snapshot.status)?
                        );
                    }
                }
            }
        }
        TransportCommand::Enable { name } => {
            ensure_daemon_running(port)?;
            print_raw_json_or_text(
                &authed_http(
                    port,
                    "POST",
                    &format!("/v1/transport/{name}/enable"),
                    Some("{}"),
                )?,
                json_output,
            )?;
        }
        TransportCommand::Disable { name } => {
            ensure_daemon_running(port)?;
            print_raw_json_or_text(
                &authed_http(
                    port,
                    "POST",
                    &format!("/v1/transport/{name}/disable"),
                    Some("{}"),
                )?,
                json_output,
            )?;
        }
    }
    Ok(())
}

async fn wait(command: WaitCommand, json_output: bool) -> Result<()> {
    match command {
        WaitCommand::Output {
            pane,
            session,
            agent,
            match_text,
            regex,
            timeout_ms,
        } => {
            let response = orchestration::client::request(
                "wait.output",
                json!({
                    "paneId": pane,
                    "sessionId": session,
                    "agent": agent,
                    "match": match_text,
                    "regex": regex,
                    "timeoutMs": timeout_ms,
                }),
            )
            .await?;
            print_value(response, json_output)
        }
        WaitCommand::AgentStatus {
            agent,
            session,
            status,
            timeout_ms,
        } => {
            let response = orchestration::client::request(
                "wait.agent_status",
                json!({
                    "sessionId": session,
                    "agent": agent,
                    "status": status,
                    "timeoutMs": timeout_ms,
                }),
            )
            .await?;
            print_value(response, json_output)
        }
    }
}

async fn agent(command: AgentCommand, port: u16, json_output: bool) -> Result<()> {
    match command {
        AgentCommand::List => print_orchestration("agent.list", json!({}), json_output).await,
        AgentCommand::Read { id, source, format } => {
            let response = orchestration::client::request(
                "agent.read",
                json!({"id": id, "source": source, "format": format}),
            )
            .await?;
            if json_output {
                print_value(response, true)
            } else {
                println!(
                    "{}",
                    response.get("text").and_then(Value::as_str).unwrap_or("")
                );
                Ok(())
            }
        }
        AgentCommand::Send { id, text } => {
            print_orchestration(
                "agent.send",
                json!({"id": id, "text": text.join(" ")}),
                json_output,
            )
            .await
        }
        AgentCommand::Focus { id } => {
            let mut response =
                orchestration::client::request("agent.focus", json!({"id": id})).await?;
            if healthz(port) {
                if let Some(session_id) = response
                    .get("session")
                    .and_then(|session| session.get("id"))
                    .and_then(Value::as_str)
                {
                    let focus = authed_http(
                        port,
                        "POST",
                        &format!("/v1/desktop/session/{}/focus", path_segment(session_id)),
                        Some("{}"),
                    )?;
                    if let Ok(value) = serde_json::from_str::<Value>(&focus) {
                        response["desktopFocus"] = value;
                    }
                }
            }
            print_value(response, json_output)
        }
        AgentCommand::Start {
            agent,
            workspace,
            prompt,
        } => {
            let command = agent.clone().unwrap_or_else(util::shell::default_shell);
            print_orchestration(
                "agent.start",
                json!({
                    "command": command,
                    "args": prompt.into_iter().collect::<Vec<_>>(),
                    "cwd": workspace.map(|path| path.display().to_string()),
                    "agent": agent,
                }),
                json_output,
            )
            .await
        }
    }
}

async fn events(command: EventsCommand, json_output: bool) -> Result<()> {
    match command {
        EventsCommand::Subscribe => {
            let mut rx = orchestration::client::event_receiver(json!({})).await?;
            while let Some(frame) = rx.recv().await {
                if json_output {
                    println!("{}", serde_json::to_string(&frame)?);
                } else {
                    println!("{}", serde_json::to_string_pretty(&frame)?);
                }
            }
            Ok(())
        }
    }
}

async fn print_orchestration(command: &str, payload: Value, json_output: bool) -> Result<()> {
    let response = orchestration::client::request(command, payload).await?;
    print_value(response, json_output)
}

fn print_value(value: Value, json_output: bool) -> Result<()> {
    if json_output {
        println!("{}", serde_json::to_string_pretty(&value)?);
    } else if let Some(text) = value.as_str() {
        println!("{text}");
    } else {
        println!("{}", serde_json::to_string(&value)?);
    }
    Ok(())
}

fn print_raw_json_or_text(raw: &str, json_output: bool) -> Result<()> {
    if json_output {
        let value = serde_json::from_str::<Value>(raw)?;
        print_value(value, true)
    } else {
        println!("{raw}");
        Ok(())
    }
}

fn hook(name: &str, port: u16) -> Result<()> {
    match name {
        "codex" => adapters::codex::run_stdin_hook(env_port().unwrap_or(port)),
        other => bail!("unsupported internal hook adapter: {other}"),
    }
}

fn env_port() -> Option<u16> {
    std::env::var("ONIBI_PORT")
        .ok()
        .and_then(|port| port.parse::<u16>().ok())
}

pub(crate) fn healthz(port: u16) -> bool {
    let Ok(mut stream) = TcpStream::connect(("127.0.0.1", port)) else {
        return false;
    };
    if write!(
        stream,
        "GET /healthz HTTP/1.1\r\nHost: 127.0.0.1:{port}\r\nConnection: close\r\n\r\n"
    )
    .is_err()
    {
        return false;
    }
    let mut raw = String::new();
    std::io::Read::read_to_string(&mut stream, &mut raw).is_ok() && raw.starts_with("HTTP/1.1 200")
}

fn ensure_daemon_running(port: u16) -> Result<()> {
    if healthz(port) {
        Ok(())
    } else {
        bail!("Onibi daemon is not running on 127.0.0.1:{port}; start the app or `onibi --headless` first")
    }
}

pub(crate) fn authed_http(
    port: u16,
    method: &str,
    path: &str,
    body: Option<&str>,
) -> Result<String> {
    let tokens = auth_token_candidates()?;
    let body = body.unwrap_or_default();
    let mut last_error = None;

    for token in tokens {
        match authed_http_with_token(port, method, path, body, &token) {
            Ok(body) => return Ok(body),
            Err(error) => {
                let message = error.to_string();
                if !message.contains(" 401 ") {
                    return Err(error);
                }
                last_error = Some(error);
            }
        }
    }

    Err(last_error.unwrap_or_else(|| anyhow::anyhow!("no bearer token candidates available")))
}

fn auth_token_candidates() -> Result<Vec<String>> {
    let mut tokens = vec![secret::load_or_create_token()?.token];
    if let Ok(path) = secret::token_path() {
        if let Ok(raw) = std::fs::read_to_string(path) {
            let token = raw.trim().to_string();
            if !token.is_empty() && !tokens.iter().any(|known| known == &token) {
                tokens.push(token);
            }
        }
    }
    Ok(tokens)
}

fn authed_http_with_token(
    port: u16,
    method: &str,
    path: &str,
    body: &str,
    token: &str,
) -> Result<String> {
    let mut stream = TcpStream::connect(("127.0.0.1", port))
        .with_context(|| format!("connect Onibi daemon on 127.0.0.1:{port}"))?;
    write!(
        stream,
        "{method} {path} HTTP/1.1\r\nHost: 127.0.0.1:{port}\r\nAuthorization: Bearer {token}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{body}",
        body.len()
    )?;
    let mut raw = String::new();
    std::io::Read::read_to_string(&mut stream, &mut raw)?;
    let (head, body) = raw.split_once("\r\n\r\n").unwrap_or((&raw, ""));
    let status = head.lines().next().unwrap_or_default();
    if status.contains(" 200 ") {
        Ok(body.trim().to_string())
    } else {
        bail!("{method} {path} failed: {status} {body}")
    }
}
