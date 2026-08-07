use std::{
    net::SocketAddr,
    path::PathBuf,
    process::ExitCode,
    time::{Duration, SystemTime, UNIX_EPOCH},
};

use arachne_core::{IdentityPublicKey, initialize_file_tracing};
use arachne_protocol::{TOR_ONION_SERVICE_PUBLIC_KEY_BYTES, TorMaildropProfileConfig};
use arachne_relay::{
    DEFAULT_RELAY_HEALTH_ADDRESS, DEFAULT_RELAY_METRICS_ADDRESS, RelayDatabase,
    RelayInviteProvisioner, RelayRuntimeConfig, SelfHostedRelayConfig, check_relay_health,
    generate_relay_identity_file, load_relay_identity,
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
    ProvisionInvite(ProvisionInviteArgs),
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
    #[arg(long, requires = "tls_private_key")]
    tls_certificate: Option<PathBuf>,
    #[arg(long, requires = "tls_certificate")]
    tls_private_key: Option<PathBuf>,
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

#[derive(Args)]
struct ProvisionInviteArgs {
    #[arg(long)]
    config: PathBuf,
    #[arg(long)]
    identity: PathBuf,
    #[arg(long)]
    recipient_public_key: String,
    #[arg(long)]
    onion_service_public_key: String,
    #[arg(long)]
    virtual_port: u16,
    #[arg(long, default_value_t = 2_592_000)]
    ttl_seconds: u32,
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
        Command::ProvisionInvite(args) => provision_invite(args),
    }
}

fn provision_invite(args: ProvisionInviteArgs) -> ExitCode {
    let result = (|| -> Result<Vec<u8>, Box<dyn std::error::Error>> {
        let config = SelfHostedRelayConfig::load(&args.config)?;
        let identity = load_relay_identity(&args.identity)?;
        let recipient = decode_identity_public_key(&args.recipient_public_key)?;
        let mut onion_service_public_key = [0; TOR_ONION_SERVICE_PUBLIC_KEY_BYTES];
        decode_canonical_hex(
            &args.onion_service_public_key,
            &mut onion_service_public_key,
        )?;
        let endpoint = TorMaildropProfileConfig::new(onion_service_public_key, args.virtual_port)?;
        let now = SystemTime::now().duration_since(UNIX_EPOCH)?.as_secs();
        let mut provisioner = RelayInviteProvisioner::new(
            RelayDatabase::open(config.database_path())?,
            &identity,
            endpoint,
            config.mailbox_quota(),
        );
        let invitation = provisioner.provision(recipient, now, args.ttl_seconds)?;
        Ok(invitation.encode()?)
    })();
    match result {
        Ok(invitation) => {
            println!("relay_invitation={}", hexadecimal(&invitation));
            ExitCode::SUCCESS
        }
        Err(error) => {
            eprintln!("relay invite provisioning failed: {error}");
            ExitCode::FAILURE
        }
    }
}

fn decode_identity_public_key(encoded: &str) -> Result<IdentityPublicKey, &'static str> {
    let mut bytes = [0; 32];
    decode_canonical_hex(encoded, &mut bytes)?;
    IdentityPublicKey::from_bytes(bytes).map_err(|_| "identity public key is invalid")
}

fn decode_canonical_hex(encoded: &str, output: &mut [u8]) -> Result<(), &'static str> {
    if encoded.len() != output.len() * 2
        || !encoded
            .bytes()
            .all(|byte| byte.is_ascii_digit() || matches!(byte, b'a'..=b'f'))
    {
        return Err("hexadecimal input is not canonical or has an invalid length");
    }
    for (byte, pair) in output.iter_mut().zip(encoded.as_bytes().chunks_exact(2)) {
        let high = hexadecimal_nibble(pair[0]).ok_or("hexadecimal input is invalid")?;
        let low = hexadecimal_nibble(pair[1]).ok_or("hexadecimal input is invalid")?;
        *byte = high << 4 | low;
    }
    Ok(())
}

fn hexadecimal_nibble(byte: u8) -> Option<u8> {
    match byte {
        b'0'..=b'9' => Some(byte - b'0'),
        b'a'..=b'f' => Some(byte - b'a' + 10),
        _ => None,
    }
}

fn hexadecimal(bytes: &[u8]) -> String {
    const HEX: &[u8; 16] = b"0123456789abcdef";
    let mut output = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        output.push(char::from(HEX[usize::from(byte >> 4)]));
        output.push(char::from(HEX[usize::from(byte & 0x0f)]));
    }
    output
}

async fn run(args: RunArgs) -> ExitCode {
    let config = match SelfHostedRelayConfig::load(&args.config) {
        Ok(config) => config,
        Err(error) => {
            eprintln!("relay failed: {error}");
            return ExitCode::FAILURE;
        }
    };
    let Some(log_root) = config.database_path().parent() else {
        eprintln!("relay failed: relay database path has no parent");
        return ExitCode::FAILURE;
    };
    if let Err(error) = initialize_file_tracing(log_root, "relay") {
        eprintln!("relay logging failed: {error}");
        return ExitCode::FAILURE;
    }
    let runtime = RelayRuntimeConfig::new(
        args.config,
        args.identity,
        args.health_address,
        args.metrics_address,
        Duration::from_secs(args.graceful_shutdown_timeout_seconds),
    );
    let runtime = match (runtime, args.tls_certificate, args.tls_private_key) {
        (Ok(runtime), Some(certificate), Some(private_key)) => {
            runtime.with_tls_pem(certificate, private_key)
        }
        (Ok(runtime), None, None) => Ok(runtime),
        (Ok(_), _, _) => Err(arachne_relay::RelayRuntimeConfigError::RelativeTlsPath),
        (Err(error), _, _) => Err(error),
    };
    let runtime = match runtime {
        Ok(runtime) => runtime,
        Err(error) => {
            eprintln!("relay failed: {error}");
            return ExitCode::FAILURE;
        }
    };
    tracing::info!(
        target: "arachne.relay.lifecycle",
        event = "starting",
        "relay lifecycle event"
    );
    match runtime.serve_until(shutdown_signal()).await {
        Ok(()) => {
            tracing::info!(
                target: "arachne.relay.lifecycle",
                event = "stopped",
                "relay lifecycle event"
            );
            ExitCode::SUCCESS
        }
        Err(error) => {
            tracing::error!(
                target: "arachne.relay.lifecycle",
                event = "stopped_with_error",
                error_class = "runtime",
                "relay lifecycle event"
            );
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
