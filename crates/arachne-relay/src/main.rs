use std::{net::SocketAddr, path::PathBuf, process::ExitCode, time::Duration};

use arachne_relay::{
    DEFAULT_RELAY_HEALTH_ADDRESS, DEFAULT_RELAY_METRICS_ADDRESS, RelayRuntimeConfig,
    check_relay_health, generate_relay_identity_file,
};
use clap::{Args, Parser, Subcommand};

#[derive(Parser)]
#[command(
    name = "arachne-relay",
    version,
    about = "Run a self-hosted Arachne relay"
)]
struct Cli {
    #[command(subcommand)]
    command: Command,
}

#[derive(Subcommand)]
enum Command {
    Run(RunArgs),
    Healthcheck(HealthcheckArgs),
    GenerateIdentity(GenerateIdentityArgs),
}

#[derive(Args)]
struct RunArgs {
    #[arg(long)]
    config: PathBuf,
    #[arg(long)]
    identity: PathBuf,
    #[arg(long, default_value = DEFAULT_RELAY_HEALTH_ADDRESS)]
    health_address: SocketAddr,
    #[arg(long, default_value = DEFAULT_RELAY_METRICS_ADDRESS)]
    metrics_address: SocketAddr,
    #[arg(long, default_value_t = 30)]
    graceful_shutdown_timeout_seconds: u64,
}

#[derive(Args)]
struct HealthcheckArgs {
    #[arg(long, default_value = DEFAULT_RELAY_HEALTH_ADDRESS)]
    address: SocketAddr,
}

#[derive(Args)]
struct GenerateIdentityArgs {
    #[arg(long)]
    output: PathBuf,
}

#[tokio::main]
async fn main() -> ExitCode {
    match Cli::parse().command {
        Command::Run(args) => run(args).await,
        Command::Healthcheck(args) => match check_relay_health(args.address) {
            Ok(()) => ExitCode::SUCCESS,
            Err(error) => {
                eprintln!("relay healthcheck failed: {error}");
                ExitCode::FAILURE
            }
        },
        Command::GenerateIdentity(args) => match generate_relay_identity_file(&args.output) {
            Ok(_) => ExitCode::SUCCESS,
            Err(error) => {
                eprintln!("relay identity generation failed: {error}");
                ExitCode::FAILURE
            }
        },
    }
}

async fn run(args: RunArgs) -> ExitCode {
    let runtime = RelayRuntimeConfig::new(
        args.config,
        args.identity,
        args.health_address,
        args.metrics_address,
        Duration::from_secs(args.graceful_shutdown_timeout_seconds),
    );
    let runtime = match runtime {
        Ok(runtime) => runtime,
        Err(error) => {
            eprintln!("relay failed: {error}");
            return ExitCode::FAILURE;
        }
    };
    match runtime.serve_until(shutdown_signal()).await {
        Ok(()) => ExitCode::SUCCESS,
        Err(error) => {
            eprintln!("relay failed: {error}");
            ExitCode::FAILURE
        }
    }
}

async fn shutdown_signal() {
    #[cfg(unix)]
    {
        let Ok(mut terminate) =
            tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate())
        else {
            let _ = tokio::signal::ctrl_c().await;
            return;
        };
        tokio::select! {
            _ = tokio::signal::ctrl_c() => {}
            _ = terminate.recv() => {}
        }
    }
    #[cfg(not(unix))]
    {
        let _ = tokio::signal::ctrl_c().await;
    }
}
