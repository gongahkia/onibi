use crate::{adapters, secret, server};
use anyhow::{bail, Context, Result};
use clap::{CommandFactory, Parser, Subcommand};
use std::{io::Write, net::TcpStream};

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
    #[command(subcommand)]
    command: Option<Command>,
}

#[derive(Debug, Subcommand)]
enum Command {
    Setup,
    Status,
    Token {
        #[command(subcommand)]
        command: TokenCommand,
    },
    Adapter {
        #[command(subcommand)]
        command: AdapterCommand,
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

pub fn should_dispatch(args: &[String]) -> bool {
    args.len() > 1
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
        let state = server::AppState::from_config(cli.port)?;
        return server::start_server(state, cli.port).await;
    }

    match cli.command {
        Some(Command::Setup) => setup(cli.port),
        Some(Command::Status) => status(cli.port),
        Some(Command::Token { command }) => token(command),
        Some(Command::Adapter { command }) => adapter(command),
        Some(Command::Hook { name }) => hook(&name, cli.port),
        None => {
            Cli::command().print_help()?;
            println!();
            Ok(())
        }
    }
}

fn setup(port: u16) -> Result<()> {
    let token = secret::load_or_create_token()?;
    let vapid = secret::load_or_create_vapid_keys()?;
    let state = server::AppState::from_config(port)?;
    println!("Onibi setup complete");
    println!("config_dir={}", secret::config_dir()?.display());
    println!("db_path={}", state.store.path().display());
    println!("machine_id={}", state.machine_id);
    println!("token_source={:?}", token.source);
    println!("vapid_public_key={}", vapid.public_key);
    Ok(())
}

fn status(port: u16) -> Result<()> {
    let config_dir = secret::config_dir()?;
    let db_path = secret::db_path()?;
    println!("config_dir={}", config_dir.display());
    println!("db_path={}", db_path.display());
    println!("db_exists={}", db_path.exists());
    println!(
        "daemon={}",
        if healthz(port) { "running" } else { "stopped" }
    );
    Ok(())
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

fn healthz(port: u16) -> bool {
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
