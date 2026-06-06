pub mod doctor;
pub mod setup;
pub mod status;

#[cfg(not(feature = "gui"))]
use crate::remote;
use crate::{adapters, config, headless, orchestration, secret, server, transport, util};
use anyhow::{bail, Context, Result};
#[cfg(feature = "gui")]
use app_lib::remote;
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
    Status {
        #[command(subcommand)]
        command: Option<StatusCommand>,
    },
    Doctor,
    Config {
        #[command(subcommand)]
        command: ConfigCommand,
    },
    Token {
        #[command(subcommand)]
        command: TokenCommand,
    },
    Adapter {
        #[command(subcommand)]
        command: AdapterCommand,
    },
    Integration {
        #[command(subcommand)]
        command: IntegrationCommand,
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
    Worktree {
        #[command(subcommand)]
        command: WorktreeCommand,
    },
    Remote {
        #[command(subcommand)]
        command: RemoteCommand,
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
enum StatusCommand {
    Server,
    Client,
}

#[derive(Debug, Subcommand)]
enum ConfigCommand {
    Validate,
    Reload,
    ResetKeys,
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
enum IntegrationCommand {
    Status {
        #[arg(long)]
        outdated_only: bool,
    },
    List {
        #[arg(long)]
        outdated_only: bool,
    },
    Install {
        name: String,
    },
    Uninstall {
        name: String,
    },
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
enum WorktreeCommand {
    Open {
        path: PathBuf,
        #[arg(long)]
        agent: Option<String>,
        #[arg(long)]
        prompt: Option<String>,
    },
}

#[derive(Debug, Subcommand)]
enum RemoteCommand {
    Ssh {
        target: String,
        #[arg(long)]
        workspace: PathBuf,
        #[arg(long)]
        cwd: Option<String>,
        #[arg(long)]
        name: Option<String>,
        #[arg(long = "keybindings", default_value = "local")]
        keybindings: String,
        #[arg(long = "ssh-command", default_value = "ssh")]
        ssh_command: String,
    },
    Bootstrap {
        #[command(subcommand)]
        command: RemoteBootstrapCommand,
    },
}

#[derive(Debug, Subcommand)]
enum RemoteBootstrapCommand {
    Ssh {
        target: String,
        #[arg(long)]
        workspace: PathBuf,
        #[arg(long)]
        cwd: Option<String>,
        #[arg(long = "ssh-command", default_value = "ssh")]
        ssh_command: String,
        #[arg(long)]
        helper_path: Option<String>,
        #[arg(long)]
        staging_dir: Option<String>,
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
        "config",
        "token",
        "adapter",
        "integration",
        "transport",
        "session",
        "pane",
        "wait",
        "agent",
        "worktree",
        "remote",
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
        Some(Command::Status { command }) => {
            let target = match command {
                Some(StatusCommand::Server) => status::StatusTarget::Server,
                Some(StatusCommand::Client) => status::StatusTarget::Client,
                None => status::StatusTarget::All,
            };
            status::run(port, cli.json, target).await
        }
        Some(Command::Doctor) => doctor::run(port, cli.json).await,
        Some(Command::Config { command }) => config_command(command, port, cli.json),
        Some(Command::Token { command }) => token(command, cli.json),
        Some(Command::Adapter { command }) => adapter(command, cli.json),
        Some(Command::Integration { command }) => integration(command, cli.json),
        Some(Command::Transport { command }) => transport(command, port, cli.json).await,
        Some(Command::Session { command }) => session(command, port, cli.json).await,
        Some(Command::Pane { command }) => pane(command, port, cli.json).await,
        Some(Command::Wait { command }) => wait(command, cli.json).await,
        Some(Command::Agent { command }) => agent(command, port, cli.json).await,
        Some(Command::Worktree { command }) => worktree(command, port, cli.json),
        Some(Command::Remote { command }) => remote(command, cli.json).await,
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

async fn remote(command: RemoteCommand, json_output: bool) -> Result<()> {
    match command {
        RemoteCommand::Ssh {
            target,
            workspace,
            cwd,
            name,
            keybindings,
            ssh_command,
        } => {
            let parsed = parse_ssh_remote_target(&target)?;
            let remote_cwd = cwd
                .as_deref()
                .map(str::trim)
                .filter(|value| !value.is_empty())
                .map(ToOwned::to_owned)
                .or_else(|| parsed.remote_cwd.clone());
            let keybinding_policy = remote_keybinding_policy(&keybindings)?;
            let mut args = Vec::new();
            if let Some(port) = parsed.port {
                args.push("-p".to_string());
                args.push(port.to_string());
            }
            if remote_cwd.is_some() {
                args.push("-t".to_string());
            }
            args.push(parsed.ssh_target.clone());
            if let Some(remote_cwd) = remote_cwd.as_deref() {
                args.push(remote_shell_command_for_cwd(remote_cwd));
            }
            let title = name
                .clone()
                .filter(|value| !value.trim().is_empty())
                .unwrap_or_else(|| format!("SSH: {}", parsed.ssh_target));
            let command = if ssh_command.trim().is_empty() {
                "ssh".to_string()
            } else {
                ssh_command.trim().to_string()
            };
            let response = orchestration::client::request(
                "pty.spawn",
                json!({
                    "command": command,
                    "args": args,
                    "cwd": workspace.display().to_string(),
                    "name": name,
                    "agent": "shell",
                    "workspaceId": format!("workspace:{}", workspace.display()),
                    "title": title,
                    "remote": {
                        "kind": "ssh",
                        "target": parsed.target,
                        "user": parsed.user,
                        "host": parsed.host,
                        "port": parsed.port,
                        "remoteCwd": remote_cwd,
                        "keybindingPolicy": keybinding_policy,
                    },
                }),
            )
            .await?;
            print_value(response, json_output)
        }
        RemoteCommand::Bootstrap { command } => match command {
            RemoteBootstrapCommand::Ssh {
                target,
                workspace,
                cwd,
                ssh_command,
                helper_path,
                staging_dir,
            } => {
                let parsed = parse_ssh_remote_target(&target)?;
                let remote_cwd = cwd
                    .as_deref()
                    .map(str::trim)
                    .filter(|value| !value.is_empty())
                    .map(ToOwned::to_owned)
                    .or_else(|| parsed.remote_cwd.clone());
                let result = remote::remote_ssh_bootstrap(remote::RemoteSshBootstrapRequest {
                    target: parsed.target,
                    user: parsed.user,
                    host: parsed.host,
                    port: parsed.port,
                    remote_cwd,
                    ssh_command: Some(ssh_command),
                    helper_path,
                    staging_dir,
                })?;
                if json_output {
                    print_value(serde_json::to_value(result)?, true)
                } else {
                    println!("Bootstrapped remote SSH helper for {}", result.target);
                    println!("Workspace: {}", workspace.display());
                    println!("Helper: {}", result.helper_path);
                    println!("Staging: {}", result.staging_dir);
                    Ok(())
                }
            }
        },
    }
}

#[derive(Debug)]
struct ParsedSshRemoteTarget {
    target: String,
    ssh_target: String,
    user: Option<String>,
    host: String,
    port: Option<u16>,
    remote_cwd: Option<String>,
}

fn parse_ssh_remote_target(input: &str) -> Result<ParsedSshRemoteTarget> {
    let raw = input.trim();
    if raw.is_empty() {
        bail!("enter an SSH target");
    }
    if raw.contains("://") && !raw.starts_with("ssh://") {
        bail!("only ssh:// remote targets are supported");
    }
    if let Some(rest) = raw.strip_prefix("ssh://") {
        let (authority, path) = rest.split_once('/').unwrap_or((rest, ""));
        if authority.trim().is_empty() {
            bail!("SSH target is missing a host");
        }
        let (user, host_port) = split_user_host(authority);
        let (host, port) = split_host_port(&host_port)?;
        if host.is_empty() {
            bail!("SSH target is missing a host");
        }
        let remote_cwd = if path.is_empty() {
            None
        } else {
            Some(percent_decode_path(&format!("/{path}")))
        };
        let ssh_target = user
            .as_ref()
            .map(|user| format!("{user}@{host}"))
            .unwrap_or_else(|| host.clone());
        return Ok(ParsedSshRemoteTarget {
            target: raw.to_string(),
            ssh_target,
            user,
            host,
            port,
            remote_cwd,
        });
    }

    let (user, host_port) = split_user_host(raw);
    let (host, port) = split_host_port(&host_port)?;
    if host.is_empty() {
        bail!("SSH target is missing a host");
    }
    let ssh_target = user
        .as_ref()
        .map(|user| format!("{user}@{host}"))
        .unwrap_or_else(|| host.clone());
    Ok(ParsedSshRemoteTarget {
        target: raw.to_string(),
        ssh_target,
        user,
        host,
        port,
        remote_cwd: None,
    })
}

fn split_user_host(value: &str) -> (Option<String>, String) {
    match value.split_once('@') {
        Some((user, host)) if !user.trim().is_empty() => {
            (Some(user.trim().to_string()), host.trim().to_string())
        }
        _ => (None, value.trim().to_string()),
    }
}

fn split_host_port(value: &str) -> Result<(String, Option<u16>)> {
    let trimmed = value.trim();
    let Some((host, port)) = trimmed.rsplit_once(':') else {
        return Ok((trimmed.to_string(), None));
    };
    if port.is_empty() || !port.chars().all(|ch| ch.is_ascii_digit()) {
        return Ok((trimmed.to_string(), None));
    }
    let port: u16 = port
        .parse()
        .with_context(|| format!("invalid SSH port: {port}"))?;
    if port == 0 {
        bail!("SSH port must be between 1 and 65535");
    }
    Ok((host.to_string(), Some(port)))
}

fn percent_decode_path(value: &str) -> String {
    let mut output = Vec::with_capacity(value.len());
    let bytes = value.as_bytes();
    let mut index = 0;
    while index < bytes.len() {
        if bytes[index] == b'%' && index + 2 < bytes.len() {
            if let (Some(high), Some(low)) =
                (hex_value(bytes[index + 1]), hex_value(bytes[index + 2]))
            {
                output.push((high << 4) | low);
                index += 3;
                continue;
            }
        }
        output.push(bytes[index]);
        index += 1;
    }
    String::from_utf8_lossy(&output).to_string()
}

fn hex_value(byte: u8) -> Option<u8> {
    match byte {
        b'0'..=b'9' => Some(byte - b'0'),
        b'a'..=b'f' => Some(byte - b'a' + 10),
        b'A'..=b'F' => Some(byte - b'A' + 10),
        _ => None,
    }
}

fn remote_keybinding_policy(value: &str) -> Result<&'static str> {
    match value.trim() {
        "local" | "" => Ok("local"),
        "remote" => Ok("remote"),
        other => bail!("remote keybindings must be 'local' or 'remote', got {other}"),
    }
}

fn remote_shell_command_for_cwd(remote_cwd: &str) -> String {
    format!(
        "cd {} && exec \"${{SHELL:-sh}}\" -l",
        shell_single_quote(remote_cwd)
    )
}

fn shell_single_quote(value: &str) -> String {
    format!("'{}'", value.replace('\'', "'\\''"))
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

fn config_command(command: ConfigCommand, port: u16, json_output: bool) -> Result<()> {
    match command {
        ConfigCommand::Validate => {
            let validation = config::validate()?;
            if json_output {
                print_value(serde_json::to_value(validation)?, true)
            } else {
                println!("Config:    {}", validation.path.display());
                println!(
                    "File:      {}",
                    if validation.exists {
                        "found"
                    } else {
                        "missing; defaults apply"
                    }
                );
                println!(
                    "Runtime:   approval_timeout_secs={}, pty_ring_limit={}",
                    validation.runtime.approval_timeout_secs, validation.runtime.pty_ring_limit
                );
                Ok(())
            }
        }
        ConfigCommand::Reload => {
            ensure_daemon_running(port)?;
            print_raw_json_or_text(
                &authed_http(port, "POST", "/v1/config/reload", Some("{}"))?,
                json_output,
            )
        }
        ConfigCommand::ResetKeys => {
            let reset = config::reset_keybindings()?;
            if json_output {
                print_value(serde_json::to_value(reset)?, true)
            } else {
                println!("Config:    {}", reset.path.display());
                println!(
                    "File:      {}",
                    if reset.existed { "updated" } else { "created" }
                );
                println!("Prefix:    {}", reset.prefix);
                println!(
                    "Bindings:  app={}, command={}",
                    reset.app_binding_count, reset.command_binding_count
                );
                Ok(())
            }
        }
    }
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

fn integration(command: IntegrationCommand, json_output: bool) -> Result<()> {
    match command {
        IntegrationCommand::Status { outdated_only }
        | IntegrationCommand::List { outdated_only } => {
            let integrations = adapters::status(outdated_only);
            if json_output {
                print_value(serde_json::to_value(integrations)?, true)?;
            } else if integrations.is_empty() {
                println!(
                    "{}",
                    if outdated_only {
                        "no outdated integrations"
                    } else {
                        "no integrations"
                    }
                );
            } else {
                for integration in integrations {
                    let state = if integration.installed {
                        if integration.outdated {
                            "outdated"
                        } else {
                            "installed"
                        }
                    } else {
                        "missing"
                    };
                    let installed_version = integration.installed_version.as_deref().unwrap_or(
                        if integration.installed {
                            "unknown"
                        } else {
                            "-"
                        },
                    );
                    let bundled_version = integration.bundled_version.unwrap_or("-");
                    let path = integration
                        .install_path
                        .as_ref()
                        .map(|path| path.display().to_string())
                        .unwrap_or_else(|| "-".to_string());
                    println!(
                        "{}\tsupport={}\tstate={}\tinstalledVersion={}\tbundledVersion={}\tpath={}",
                        integration.name,
                        integration.support,
                        state,
                        installed_version,
                        bundled_version,
                        path
                    );
                }
            }
        }
        IntegrationCommand::Install { name } => {
            let token = secret::load_or_create_token()?.token;
            let message = adapters::install(&name, &token)?;
            if json_output {
                print_value(json!({"ok": true, "name": name, "message": message}), true)?;
            } else {
                println!("{message}");
            }
        }
        IntegrationCommand::Uninstall { name } => {
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

fn worktree(command: WorktreeCommand, port: u16, json_output: bool) -> Result<()> {
    match command {
        WorktreeCommand::Open {
            path,
            agent,
            prompt,
        } => {
            ensure_daemon_running(port)?;
            let body = json!({
                "protocol_version": "1.0",
                "path": path.display().to_string(),
                "agent": agent,
                "prompt": prompt,
            });
            print_raw_json_or_text(
                &authed_http(
                    port,
                    "POST",
                    "/v1/desktop/worktree/open",
                    Some(&body.to_string()),
                )?,
                json_output,
            )
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
        "copilot" | "goose" | "qoder" => {
            let exit = adapters::run_stdin_provider_hook(name, env_port().unwrap_or(port))?;
            if exit.code != 0 {
                std::process::exit(exit.code);
            }
            Ok(())
        }
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
