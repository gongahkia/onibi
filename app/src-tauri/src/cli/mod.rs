pub mod doctor;
pub mod setup;
pub mod status;

use crate::{adapters, headless, secret, server, transport};
use anyhow::{bail, Context, Result};
use clap::{CommandFactory, Parser, Subcommand};
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
    #[arg(long, default_value_t = 17893, help = "Local approval server port")]
    port: u16,
    #[arg(
        long,
        help = "Enable LAN, Tailscale Funnel, and Cloudflare Tunnel at headless startup"
    )]
    auto_transports: bool,
    #[arg(long, help = "Override the Onibi config directory")]
    config_dir: Option<PathBuf>,
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

pub fn should_dispatch(args: &[String]) -> bool {
    const COMMANDS: &[&str] = &[
        "setup",
        "status",
        "doctor",
        "token",
        "adapter",
        "transport",
        "_hook",
    ];

    args.iter().skip(1).any(|arg| {
        matches!(
            arg.as_str(),
            "--headless" | "--help" | "-h" | "--version" | "-V"
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
    if cli.headless {
        return headless::run(headless::HeadlessOpts {
            config_dir: cli.config_dir,
            port: Some(cli.port),
            auto_transports: cli.auto_transports,
        })
        .await;
    }

    if let Some(config_dir) = cli.config_dir {
        std::env::set_var("ONIBI_CONFIG_DIR", config_dir);
    }

    match cli.command {
        Some(Command::Setup) => setup::run(cli.port).await,
        Some(Command::Status) => status::run(cli.port).await,
        Some(Command::Doctor) => doctor::run(cli.port).await,
        Some(Command::Token { command }) => token(command),
        Some(Command::Adapter { command }) => adapter(command),
        Some(Command::Transport { command }) => transport(command, cli.port).await,
        Some(Command::Hook { name }) => hook(&name, cli.port),
        None => {
            Cli::command().print_help()?;
            println!();
            Ok(())
        }
    }
}

fn token(command: TokenCommand) -> Result<()> {
    match command {
        TokenCommand::Rotate => {
            let token = secret::rotate_token()?;
            println!("{}", token.token);
        }
        TokenCommand::Show => {
            let token = secret::load_or_create_token()?;
            println!("{}", token.token);
        }
    }
    Ok(())
}

fn adapter(command: AdapterCommand) -> Result<()> {
    match command {
        AdapterCommand::List => {
            for adapter in adapters::list() {
                println!(
                    "{}\tsupport={}\tinstalled={}",
                    adapter.name, adapter.support, adapter.installed
                );
            }
        }
        AdapterCommand::Install { name } => {
            let token = secret::load_or_create_token()?.token;
            println!("{}", adapters::install(&name, &token)?);
        }
        AdapterCommand::Uninstall { name } => {
            println!("{}", adapters::uninstall(&name)?);
        }
    }
    Ok(())
}

async fn transport(command: TransportCommand, port: u16) -> Result<()> {
    match command {
        TransportCommand::List => {
            for name in transport::default_transport_names() {
                println!("{name}");
            }
        }
        TransportCommand::Status => {
            if healthz(port) {
                println!(
                    "{}",
                    authed_http(port, "GET", "/v1/transport/status", None)?
                );
            } else {
                let state = server::AppState::from_config(port)?;
                for snapshot in state.transports.status_snapshot().await {
                    println!(
                        "{}\tenabled={}\tstatus={}",
                        snapshot.name,
                        snapshot.enabled,
                        serde_json::to_string(&snapshot.status)?
                    );
                }
            }
        }
        TransportCommand::Enable { name } => {
            ensure_daemon_running(port)?;
            println!(
                "{}",
                authed_http(
                    port,
                    "POST",
                    &format!("/v1/transport/{name}/enable"),
                    Some("{}"),
                )?
            );
        }
        TransportCommand::Disable { name } => {
            ensure_daemon_running(port)?;
            println!(
                "{}",
                authed_http(
                    port,
                    "POST",
                    &format!("/v1/transport/{name}/disable"),
                    Some("{}"),
                )?
            );
        }
    }
    Ok(())
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
