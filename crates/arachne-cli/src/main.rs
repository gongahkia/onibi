#![forbid(unsafe_code)]

mod release_manifest;

#[cfg(any(test, unix))]
use arachne_core::KeystoreSecret;
use arachne_core::{
    IdentityKeypair, IdentityPublicKey, KeystoreEntryName, OsKeystore, initialize_file_tracing,
    list_log_files, log_directory, prune_log_files,
};
use arachne_daemon::{
    AttachmentSubmissionStore, COURIER_ONE_TIME_PREKEY_TARGET, ClientIdentity,
    ClientIdentityInitialization, ClientProfile, ClientStateDirectory, ContactLifecycleService,
    ContactStatus, ContactStore, ContactVerificationMethod, CourierAttachmentJobStore,
    CourierBundleStore, CourierCryptographer, CourierDaemon, CourierDaemonConfig,
    CourierMaildropClient, CourierOneTimePrekeyInventory, CourierSessionStore, DaemonRuntime,
    InboxMessage, MessageExpiry, PendingContactImportService, QrContactVerificationService,
    RecipientInboxDeduplication, SafetyNumberVerificationService, SenderOutbox,
    SharedIpMeshCertificatePin, SharedIpMeshConfig, SharedIpMeshEndpoint, SharedIpMeshPeer,
    SharedIpMeshTlsIdentity, SharedIpMeshTransport,
};
#[cfg(any(test, unix))]
use arachne_daemon::{DaemonLocalAuth, DaemonLocalAuthToken};
use arachne_protocol::{
    ATTACHMENT_CHUNK_BYTES, ATTACHMENT_IDENTIFIER_BYTES, AttachmentIdentifier, AttachmentKey,
    AttachmentManifest, AttachmentUploadJournal, CONTACT_INVITATION_BYTES, ContactInvitation,
    CourierAttachmentReference, CourierBundle, DEFAULT_REFERENCE_ATTACHMENT_MAX_BYTES,
    EncryptedAttachmentChunk, EncryptedAttachmentManifest, EncryptedMessageEnvelope,
    IDENTITY_ROTATION_BYTES, IdentityIdentifier, MAX_ENCODED_ATTACHMENT_CHUNK_BYTES,
    MAX_ENCODED_ATTACHMENT_MANIFEST_BYTES, MAX_RELAY_INVITATION_BYTES, MessageContentType,
    MessageIdentifier, MessagePayload, QR_VERIFICATION_PAYLOAD_BYTES,
    RELAY_TLS_CERTIFICATE_PIN_BYTES, RelayInvitation, SAFETY_NUMBER_FINGERPRINT_BYTES,
    TOR_ONION_SERVICE_PUBLIC_KEY_BYTES, TorMaildropProfileConfig,
};
use arachne_sdk::{
    SdkClient, SdkClientBuilder, SdkContactStatus, SdkDeliveryProfile, SdkDeliveryProfilePolicy,
    SdkDirectIpDisclosureAcknowledgement, SdkIdentityManager, SdkLocalMeshPolicy,
    SdkLocalMeshTransportKind, SdkMessageEnvelope, SdkMessageExpiry, SdkMessageSendRequest,
};
use clap::{Args, Parser, Subcommand};
#[cfg(test)]
use crossterm::style::Print;
use crossterm::{
    cursor::{Hide, MoveTo, Show},
    event::{self, Event, KeyCode, KeyEventKind},
    execute, queue,
    terminal::{self, Clear, ClearType, EnterAlternateScreen, LeaveAlternateScreen},
};
use getrandom::{SysRng, rand_core::TryRng};
use sha2::{Digest, Sha256};
use std::{
    collections::BTreeMap,
    error::Error,
    fmt::Write as _,
    fs,
    io::{self, Write},
    path::{Path, PathBuf},
    sync::Arc,
    time::{Duration, SystemTime, UNIX_EPOCH},
};

#[cfg(unix)]
use arachne_daemon::DaemonServer;
#[cfg(unix)]
use arachne_daemon_api::v1::{
    ContactStatus as RpcContactStatus, ContactVerificationMethod as RpcContactVerificationMethod,
    CreateOrLoadIdentityRequest, ImportContactInvitationRequest, ListContactsRequest,
    SendMessageRequest, StartClientRequest, VerifyContactQrRequest,
    VerifyContactSafetyNumberRequest, daemon_service_client::DaemonServiceClient,
};
#[cfg(unix)]
use hyper_util::rt::TokioIo;
#[cfg(unix)]
use tokio::net::UnixStream;
#[cfg(unix)]
use tonic::{
    Request,
    transport::{Channel, Endpoint},
};
#[cfg(unix)]
use tower::service_fn;

use release_manifest::{ReleaseArtifact, SignedReleaseArtifactManifest};

#[cfg(target_os = "linux")]
use arachne_core::LinuxKeystore;
#[cfg(target_os = "macos")]
use arachne_core::MacOsKeystore;
#[cfg(target_os = "windows")]
use arachne_core::WindowsKeystore;

const MAX_PROTOCOL_VECTOR_BYTES: u64 = 16_384;
const MAX_RELAY_PROFILE_BYTES: usize = 64;
const MAX_ENCRYPTED_MESSAGE_SUBMISSION_BYTES: usize = 1024 * 1024;
const MAX_TUI_CONTACT_INPUT_BYTES: usize = CONTACT_INVITATION_BYTES * 2;
const MAX_TUI_ATTACHMENT_TRANSFERS: usize = 256;
const PROTOCOL_V1_VECTORS: &str = include_str!("../../arachne-protocol/vectors/protocol-v1.txt");

#[derive(Parser)]
#[command(name = "arachne", version, about = "Arachne secure courier")]
struct Arguments {
    #[command(subcommand)]
    command: Command,
}

#[derive(Subcommand)]
enum Command {
    Version,
    Identity {
        #[command(subcommand)]
        command: IdentityCommand,
    },
    Contact {
        #[command(subcommand)]
        command: ContactCommand,
    },
    RelayProfile {
        #[command(subcommand)]
        command: RelayProfileCommand,
    },
    Message {
        #[command(subcommand)]
        command: MessageCommand,
    },
    Attachment {
        #[command(subcommand)]
        command: AttachmentCommand,
    },
    Courier {
        #[command(subcommand)]
        command: CourierCommand,
    },
    Daemon {
        #[command(subcommand)]
        command: DaemonCommand,
    },
    LocalMesh {
        #[command(subcommand)]
        command: LocalMeshCommand,
    },
    Logs {
        #[command(subcommand)]
        command: LogCommand,
    },
    Diagnose {
        #[arg(long)]
        state_directory: PathBuf,
    },
    Tui(TuiCommand),
    ReleaseManifest {
        #[command(subcommand)]
        command: ReleaseManifestCommand,
    },
    ReleaseMetadata {
        #[arg(long)]
        source_revision: String,
        #[arg(long)]
        source_date_epoch: u64,
        #[arg(long, default_value = "Cargo.lock")]
        lockfile: PathBuf,
        #[arg(long)]
        output: PathBuf,
    },
    ProtocolVectors {
        #[arg(long)]
        verify: Option<PathBuf>,
    },
}

#[derive(Subcommand)]
enum IdentityCommand {
    Create {
        #[arg(long)]
        state_directory: PathBuf,
    },
    Show {
        #[arg(long)]
        state_directory: PathBuf,
    },
}

#[derive(Subcommand)]
enum ContactCommand {
    Invitation {
        #[command(subcommand)]
        command: ContactInvitationCommand,
    },
    VerifyQr {
        #[arg(long)]
        state_directory: PathBuf,
        #[arg(long)]
        payload: String,
    },
    VerifySafetyNumber {
        #[arg(long)]
        state_directory: PathBuf,
        #[arg(long)]
        contact_public_key: String,
        #[arg(long)]
        safety_number: String,
    },
    Rotate {
        #[arg(long)]
        state_directory: PathBuf,
        #[arg(long)]
        rotation: String,
    },
    Revoke {
        #[arg(long)]
        state_directory: PathBuf,
        #[arg(long)]
        contact_public_key: String,
    },
}

#[derive(Subcommand)]
enum ContactInvitationCommand {
    Create {
        #[arg(long)]
        state_directory: PathBuf,
    },
    Import {
        #[arg(long)]
        state_directory: PathBuf,
        #[arg(long)]
        invitation: String,
    },
    Inspect {
        #[arg(long)]
        invitation: String,
    },
}

#[derive(Subcommand)]
enum RelayProfileCommand {
    Create {
        #[arg(long)]
        onion_service_public_key: String,
        #[arg(long)]
        virtual_port: u16,
    },
    Inspect {
        #[arg(long)]
        profile: String,
    },
}

#[derive(Subcommand)]
enum MessageCommand {
    Send {
        #[arg(long)]
        state_directory: PathBuf,
        #[arg(long)]
        recipient_public_key: String,
        #[arg(long)]
        envelope: String,
        #[arg(long)]
        created_at: u64,
        #[arg(long)]
        ttl_seconds: u32,
    },
}

#[derive(Subcommand)]
enum AttachmentCommand {
    Send {
        #[arg(long)]
        state_directory: PathBuf,
        #[arg(long)]
        manifest: PathBuf,
        #[arg(long, required = true, num_args = 1..)]
        chunk: Vec<PathBuf>,
    },
}

#[derive(Subcommand)]
enum CourierCommand {
    Bundle {
        #[command(subcommand)]
        command: CourierBundleCommand,
    },
    Send {
        #[arg(long)]
        config: PathBuf,
        #[arg(long)]
        recipient_public_key: String,
        #[arg(long)]
        text: String,
        #[arg(long, default_value_t = 86_400)]
        ttl_seconds: u32,
    },
    Inbox {
        #[arg(long)]
        state_directory: PathBuf,
    },
    Attachment {
        #[command(subcommand)]
        command: CourierAttachmentCommand,
    },
    Daemon {
        #[arg(long)]
        config: PathBuf,
    },
}

#[derive(Subcommand)]
enum CourierAttachmentCommand {
    Send {
        #[arg(long)]
        config: PathBuf,
        #[arg(long)]
        recipient_public_key: String,
        #[arg(long)]
        path: PathBuf,
        #[arg(long, default_value_t = 86_400)]
        ttl_seconds: u32,
    },
    Receive {
        #[arg(long)]
        config: PathBuf,
        #[arg(long)]
        message_identifier: String,
        #[arg(long)]
        output: PathBuf,
    },
}

#[derive(Subcommand)]
enum CourierBundleCommand {
    Create {
        #[arg(long)]
        state_directory: PathBuf,
        #[arg(long)]
        relay_invitation: String,
        #[arg(long)]
        relay_tls_pin: String,
    },
    Import {
        #[arg(long)]
        state_directory: PathBuf,
        #[arg(long)]
        bundle: String,
    },
    Export {
        #[arg(long)]
        state_directory: PathBuf,
    },
}

#[derive(Subcommand)]
enum DaemonCommand {
    Serve {
        #[arg(long)]
        config: PathBuf,
    },
}

#[derive(Subcommand)]
enum LocalMeshCommand {
    Init {
        #[arg(long)]
        config: PathBuf,
        #[arg(long)]
        state_directory: PathBuf,
        #[arg(long)]
        transport: String,
        #[arg(long)]
        listen_endpoint: String,
    },
    Fingerprint {
        #[arg(long)]
        state_directory: PathBuf,
    },
    Connect {
        #[arg(long)]
        config: PathBuf,
        #[arg(long)]
        identity: String,
        #[arg(long, default_value_t = 10)]
        timeout_seconds: u8,
    },
    Peer {
        #[command(subcommand)]
        command: LocalMeshPeerCommand,
    },
    Peers {
        #[arg(long)]
        config: PathBuf,
    },
}

#[derive(Subcommand)]
enum LocalMeshPeerCommand {
    Add {
        #[arg(long)]
        config: PathBuf,
        #[arg(long)]
        identity: String,
        #[arg(long)]
        certificate_fingerprint: String,
    },
}

#[derive(Subcommand)]
enum LogCommand {
    List {
        #[arg(long)]
        state_directory: PathBuf,
    },
    Prune {
        #[arg(long)]
        state_directory: PathBuf,
        #[arg(long)]
        older_than_days: u16,
    },
}

#[derive(Args)]
struct TuiCommand {
    #[arg(long)]
    state_directory: PathBuf,
    #[arg(long)]
    snapshot: bool,
    #[arg(long)]
    daemon: bool,
}

#[derive(Subcommand)]
enum ReleaseManifestCommand {
    Sign {
        #[arg(long)]
        source_revision: String,
        #[arg(long)]
        source_date_epoch: u64,
        #[arg(long, default_value = "release_signing")]
        signing_key_name: String,
        #[arg(long, required = true, num_args = 1..)]
        artifact: Vec<PathBuf>,
        #[arg(long)]
        output: PathBuf,
    },
    Verify {
        #[arg(long)]
        manifest: PathBuf,
        #[arg(long)]
        artifact_directory: PathBuf,
        #[arg(long)]
        trusted_public_key: String,
    },
}

fn main() -> Result<(), Box<dyn Error>> {
    let arguments = Arguments::parse();
    match arguments.command {
        Command::Version => println!(
            "protocol {}",
            arachne_protocol::ProtocolVersion::INITIAL.get()
        ),
        Command::Identity { command } => {
            let public_key = match command {
                IdentityCommand::Create { state_directory } => {
                    create_client_system_identity(&state_directory)?
                }
                IdentityCommand::Show { state_directory } => {
                    load_client_system_identity(&state_directory)?.public_key()
                }
            };
            print!("{}", identity_record(&public_key));
        }
        Command::Contact { command } => run_contact_command(command)?,
        Command::RelayProfile { command } => match command {
            RelayProfileCommand::Create {
                onion_service_public_key,
                virtual_port,
            } => {
                print!(
                    "{}",
                    relay_profile_record(&onion_service_public_key, virtual_port)?
                );
            }
            RelayProfileCommand::Inspect { profile } => {
                print!("{}", inspect_relay_profile(&profile)?);
            }
        },
        Command::Message { command } => match command {
            MessageCommand::Send {
                state_directory,
                recipient_public_key,
                envelope,
                created_at,
                ttl_seconds,
            } => print!(
                "{}",
                queue_system_message(
                    &state_directory,
                    &recipient_public_key,
                    &envelope,
                    created_at,
                    ttl_seconds,
                )?
            ),
        },
        Command::Attachment { command } => match command {
            AttachmentCommand::Send {
                state_directory,
                manifest,
                chunk,
            } => print!(
                "{}",
                queue_system_attachment(&state_directory, &manifest, &chunk)?
            ),
        },
        Command::Courier { command } => run_courier_command(command)?,
        Command::Daemon { command } => match command {
            DaemonCommand::Serve { config } => daemon_serve(&config)?,
        },
        Command::LocalMesh { command } => run_local_mesh_command(command)?,
        Command::Logs { command } => print!("{}", run_log_command(command)?),
        Command::Diagnose { state_directory } => print!("{}", diagnose(&state_directory)?),
        Command::Tui(command) => tui(&command.state_directory, command.snapshot, command.daemon)?,
        Command::ReleaseManifest { command } => release_manifest(command)?,
        Command::ReleaseMetadata {
            source_revision,
            source_date_epoch,
            lockfile,
            output,
        } => {
            let lockfile = fs::read(lockfile)?;
            let metadata = release_metadata(&source_revision, source_date_epoch, &lockfile)
                .map_err(|error| std::io::Error::new(std::io::ErrorKind::InvalidInput, error))?;
            fs::write(output, metadata)?;
        }
        Command::ProtocolVectors { verify } => protocol_vectors(verify)?,
    }
    Ok(())
}

fn run_local_mesh_command(command: LocalMeshCommand) -> Result<(), Box<dyn Error>> {
    match command {
        LocalMeshCommand::Init {
            config,
            state_directory,
            transport,
            listen_endpoint,
        } => print!(
            "{}",
            initialize_shared_ip_mesh(&config, &state_directory, &transport, &listen_endpoint)?
        ),
        LocalMeshCommand::Fingerprint { state_directory } => {
            print!("{}", shared_ip_mesh_fingerprint(&state_directory)?);
        }
        LocalMeshCommand::Connect {
            config,
            identity,
            timeout_seconds,
        } => print!(
            "{}",
            connect_shared_ip_mesh(&config, &identity, timeout_seconds)?
        ),
        LocalMeshCommand::Peer { command } => match command {
            LocalMeshPeerCommand::Add {
                config,
                identity,
                certificate_fingerprint,
            } => print!(
                "{}",
                add_shared_ip_mesh_peer(&config, &identity, &certificate_fingerprint)?
            ),
        },
        LocalMeshCommand::Peers { config } => print!("{}", list_shared_ip_mesh_peers(&config)?),
    }
    Ok(())
}

fn initialize_shared_ip_mesh(
    config_path: &Path,
    state_directory: &Path,
    transport: &str,
    listen_endpoint: &str,
) -> Result<String, Box<dyn Error>> {
    let transport = SharedIpMeshTransport::parse(transport)?;
    let listen_endpoint = listen_endpoint.parse()?;
    let config =
        SharedIpMeshConfig::new(state_directory.to_path_buf(), transport, listen_endpoint)?;
    if let Some(parent) = config_path.parent()
        && !parent.as_os_str().is_empty()
    {
        fs::create_dir_all(parent)?;
    }
    #[cfg(target_os = "linux")]
    {
        let mut keystore = LinuxKeystore::new()?;
        return initialize_shared_ip_mesh_with_keystore(&config, config_path, &mut keystore);
    }
    #[cfg(target_os = "macos")]
    {
        let mut keystore = MacOsKeystore::new();
        return initialize_shared_ip_mesh_with_keystore(&config, config_path, &mut keystore);
    }
    #[cfg(target_os = "windows")]
    {
        let mut keystore = WindowsKeystore::new()?;
        return initialize_shared_ip_mesh_with_keystore(&config, config_path, &mut keystore);
    }
    #[allow(unreachable_code)]
    Err("unsupported operating system keystore".into())
}

fn initialize_shared_ip_mesh_with_keystore<K: OsKeystore>(
    config: &SharedIpMeshConfig,
    config_path: &Path,
    keystore: &mut K,
) -> Result<String, Box<dyn Error>> {
    let layout = ClientStateDirectory::new(config.state_directory())?;
    fs::create_dir_all(layout.root())?;
    let profile = ClientProfile::open_or_create(&layout)?;
    let (identity, _) = profile.create_or_load_identity(keystore)?;
    let tls = SharedIpMeshTlsIdentity::create_or_load(&layout, keystore)?;
    config.write_new(config_path)?;
    Ok(format!(
        "identity={}\ncertificate_sha256={}\ntransport={}\nlisten_endpoint={}\n",
        hexadecimal(identity.public_key().as_bytes()),
        tls.certificate_pin().encode(),
        config.transport().encode(),
        config.listen_endpoint()
    ))
}

fn shared_ip_mesh_fingerprint(state_directory: &Path) -> Result<String, Box<dyn Error>> {
    let layout = ClientStateDirectory::new(state_directory)?;
    #[cfg(target_os = "linux")]
    {
        let keystore = LinuxKeystore::new()?;
        return shared_ip_mesh_fingerprint_with_keystore(&layout, &keystore);
    }
    #[cfg(target_os = "macos")]
    {
        let keystore = MacOsKeystore::new();
        return shared_ip_mesh_fingerprint_with_keystore(&layout, &keystore);
    }
    #[cfg(target_os = "windows")]
    {
        let keystore = WindowsKeystore::new()?;
        return shared_ip_mesh_fingerprint_with_keystore(&layout, &keystore);
    }
    #[allow(unreachable_code)]
    Err("unsupported operating system keystore".into())
}

fn shared_ip_mesh_fingerprint_with_keystore<K: OsKeystore>(
    state_directory: &ClientStateDirectory,
    keystore: &K,
) -> Result<String, Box<dyn Error>> {
    let tls = SharedIpMeshTlsIdentity::load(state_directory, keystore)?;
    Ok(format!(
        "certificate_sha256={}\n",
        tls.certificate_pin().encode()
    ))
}

fn connect_shared_ip_mesh(
    config_path: &Path,
    identity: &str,
    timeout_seconds: u8,
) -> Result<String, Box<dyn Error>> {
    if timeout_seconds == 0 || timeout_seconds > 30 {
        return Err("local-mesh discovery timeout must be between 1 and 30 seconds".into());
    }
    let identity = decode_identity_public_key(identity)?;
    let runtime = tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .build()?;
    #[cfg(target_os = "linux")]
    {
        let keystore = LinuxKeystore::new()?;
        return runtime.block_on(connect_shared_ip_mesh_with_keystore(
            config_path,
            identity,
            Duration::from_secs(u64::from(timeout_seconds)),
            &keystore,
        ));
    }
    #[cfg(target_os = "macos")]
    {
        let keystore = MacOsKeystore::new();
        return runtime.block_on(connect_shared_ip_mesh_with_keystore(
            config_path,
            identity,
            Duration::from_secs(u64::from(timeout_seconds)),
            &keystore,
        ));
    }
    #[cfg(target_os = "windows")]
    {
        let keystore = WindowsKeystore::new()?;
        return runtime.block_on(connect_shared_ip_mesh_with_keystore(
            config_path,
            identity,
            Duration::from_secs(u64::from(timeout_seconds)),
            &keystore,
        ));
    }
    #[allow(unreachable_code)]
    Err("unsupported operating system keystore".into())
}

async fn connect_shared_ip_mesh_with_keystore<K: OsKeystore + Sync>(
    config_path: &Path,
    identity: IdentityPublicKey,
    timeout: Duration,
    keystore: &K,
) -> Result<String, Box<dyn Error>> {
    let config = SharedIpMeshConfig::load(config_path)?;
    let layout = ClientStateDirectory::new(config.state_directory())?;
    let profile = ClientProfile::open_or_create(&layout)?;
    let local_identity = profile.load_identity(keystore)?;
    let tls_identity = SharedIpMeshTlsIdentity::load(&layout, keystore)?;
    let endpoint = SharedIpMeshEndpoint::start(config, local_identity.public_key(), &tls_identity)?;
    let connected = async {
        let peer = endpoint.discover_trusted_peer(identity, timeout)?;
        let connection = endpoint
            .connect(peer, local_identity.keypair(), &tls_identity)
            .await?;
        Ok::<_, Box<dyn Error>>(format!(
            "identity={}\nremote_endpoint={}\n",
            hexadecimal(connection.peer().identity().as_bytes()),
            connection.connection().remote_address()
        ))
    }
    .await;
    let shutdown = endpoint.shutdown();
    shutdown?;
    connected
}

fn add_shared_ip_mesh_peer(
    config_path: &Path,
    identity: &str,
    certificate_fingerprint: &str,
) -> Result<String, Box<dyn Error>> {
    let mut config = SharedIpMeshConfig::load(config_path)?;
    let identity = decode_identity_public_key(identity)?;
    let certificate_pin = SharedIpMeshCertificatePin::decode(certificate_fingerprint)?;
    config.add_peer(SharedIpMeshPeer::new(identity, certificate_pin))?;
    config.save(config_path)?;
    Ok(format!(
        "identity={}\ncertificate_sha256={}\n",
        hexadecimal(identity.as_bytes()),
        certificate_pin.encode()
    ))
}

fn list_shared_ip_mesh_peers(config_path: &Path) -> Result<String, Box<dyn Error>> {
    let config = SharedIpMeshConfig::load(config_path)?;
    let mut output = format!(
        "transport={}\nlisten_endpoint={}\n",
        config.transport().encode(),
        config.listen_endpoint()
    );
    for peer in config.peers() {
        writeln!(
            output,
            "peer_identity={}",
            hexadecimal(peer.identity().as_bytes()),
        )
        .expect("writing to String cannot fail");
        writeln!(
            output,
            "peer_certificate_sha256={}",
            peer.certificate_pin().encode()
        )
        .expect("writing to String cannot fail");
    }
    Ok(output)
}

fn protocol_vectors(verify: Option<PathBuf>) -> Result<(), Box<dyn Error>> {
    if let Some(input) = verify {
        if fs::metadata(&input)?.len() > MAX_PROTOCOL_VECTOR_BYTES {
            return Err("protocol vector file exceeds maximum size".into());
        }
        let candidate = fs::read_to_string(input)?;
        verify_protocol_vectors(&candidate)
            .map_err(|error| std::io::Error::new(std::io::ErrorKind::InvalidInput, error))?;
    } else {
        print!("{PROTOCOL_V1_VECTORS}");
    }
    Ok(())
}

fn run_contact_command(command: ContactCommand) -> Result<(), Box<dyn Error>> {
    match command {
        ContactCommand::Invitation { command } => match command {
            ContactInvitationCommand::Create { state_directory } => print!(
                "{}",
                contact_invitation_record(
                    load_client_system_identity(&state_directory)?.keypair()
                )?
            ),
            ContactInvitationCommand::Import {
                state_directory,
                invitation,
            } => print!(
                "{}",
                import_system_contact_invitation(&state_directory, &invitation)?
            ),
            ContactInvitationCommand::Inspect { invitation } => {
                print!("{}", inspect_contact_invitation(&invitation)?);
            }
        },
        ContactCommand::VerifyQr {
            state_directory,
            payload,
        } => print!("{}", verify_system_contact_qr(&state_directory, &payload)?),
        ContactCommand::VerifySafetyNumber {
            state_directory,
            contact_public_key,
            safety_number,
        } => print!(
            "{}",
            verify_system_contact_safety_number(
                &state_directory,
                &contact_public_key,
                &safety_number,
            )?
        ),
        ContactCommand::Rotate {
            state_directory,
            rotation,
        } => print!("{}", rotate_system_contact(&state_directory, &rotation)?),
        ContactCommand::Revoke {
            state_directory,
            contact_public_key,
        } => print!(
            "{}",
            revoke_system_contact(&state_directory, &contact_public_key)?
        ),
    }
    Ok(())
}

fn release_metadata(
    source_revision: &str,
    source_date_epoch: u64,
    lockfile: &[u8],
) -> Result<String, &'static str> {
    if !is_canonical_revision(source_revision) {
        return Err("source revision must be a lowercase 40- or 64-character hexadecimal digest");
    }

    Ok(format!(
        concat!(
            "{{\"format_version\":1,\"package_version\":\"{}\",",
            "\"protocol_version\":{},\"rust_toolchain\":\"1.93.0\",",
            "\"source_revision\":\"{}\",\"source_date_epoch\":{},",
            "\"cargo_lock_sha256\":\"{}\"}}\n"
        ),
        env!("CARGO_PKG_VERSION"),
        arachne_protocol::ProtocolVersion::INITIAL.get(),
        source_revision,
        source_date_epoch,
        sha256_hex(lockfile),
    ))
}

fn release_manifest(command: ReleaseManifestCommand) -> Result<(), Box<dyn Error>> {
    match command {
        ReleaseManifestCommand::Sign {
            source_revision,
            source_date_epoch,
            signing_key_name,
            artifact,
            output,
        } => print!(
            "{}",
            sign_system_release_manifest(
                &source_revision,
                source_date_epoch,
                &signing_key_name,
                &artifact,
                &output,
            )?
        ),
        ReleaseManifestCommand::Verify {
            manifest,
            artifact_directory,
            trusted_public_key,
        } => print!(
            "{}",
            verify_release_manifest(&manifest, &artifact_directory, &trusted_public_key)?
        ),
    }
    Ok(())
}

fn sign_system_release_manifest(
    source_revision: &str,
    source_date_epoch: u64,
    signing_key_name: &str,
    artifact_paths: &[PathBuf],
    output: &Path,
) -> Result<String, Box<dyn Error>> {
    let entry = KeystoreEntryName::new(signing_key_name.to_owned())?;
    let signing_key = load_system_signing_identity(&entry)?;
    sign_release_manifest(
        &signing_key,
        source_revision,
        source_date_epoch,
        artifact_paths,
        output,
    )
}

fn sign_release_manifest(
    signing_key: &IdentityKeypair,
    source_revision: &str,
    source_date_epoch: u64,
    artifact_paths: &[PathBuf],
    output: &Path,
) -> Result<String, Box<dyn Error>> {
    let artifacts = artifact_paths
        .iter()
        .map(|path| ReleaseArtifact::from_path(path))
        .collect::<Result<Vec<_>, _>>()?;
    let manifest = SignedReleaseArtifactManifest::sign(
        source_revision,
        source_date_epoch,
        artifacts,
        signing_key,
    )?;
    let encoded = manifest.encode()?;
    write_new_file(output, &encoded)?;
    Ok(format!(
        "signing_public_key={}\nmanifest_sha256={}\n",
        hexadecimal(manifest.signing_public_key().as_bytes()),
        sha256_hex(&encoded)
    ))
}

fn verify_release_manifest(
    manifest_path: &Path,
    artifact_directory: &Path,
    trusted_public_key: &str,
) -> Result<String, Box<dyn Error>> {
    let manifest = SignedReleaseArtifactManifest::load(manifest_path)?;
    let trusted_public_key = decode_identity_public_key(trusted_public_key)?;
    manifest.verify(&trusted_public_key)?;
    manifest.verify_artifacts(artifact_directory)?;
    Ok(format!(
        "verified_artifacts={}\nsource_revision={}\nsource_date_epoch={}\n",
        manifest.artifacts().len(),
        manifest.source_revision(),
        manifest.source_date_epoch()
    ))
}

fn sha256_hex(input: &[u8]) -> String {
    hexadecimal(&Sha256::digest(input))
}

fn hexadecimal(input: &[u8]) -> String {
    let mut hexadecimal = String::with_capacity(input.len() * 2);
    for byte in input {
        write!(&mut hexadecimal, "{byte:02x}").expect("writing to String cannot fail");
    }
    hexadecimal
}

fn identity_record(public_key: &IdentityPublicKey) -> String {
    format!(
        "public_key={}\nidentity_identifier={}\n",
        hexadecimal(public_key.as_bytes()),
        hexadecimal(IdentityIdentifier::derive(public_key).as_bytes()),
    )
}

fn contact_invitation_record(identity: &IdentityKeypair) -> Result<String, Box<dyn Error>> {
    let invitation = ContactInvitation::create(identity)?;
    Ok(format!(
        "invitation={}\n",
        hexadecimal(&invitation.encode()?)
    ))
}

fn inspect_contact_invitation(encoded: &str) -> Result<String, Box<dyn Error>> {
    let mut invitation = vec![0; CONTACT_INVITATION_BYTES];
    decode_canonical_hex(encoded, &mut invitation)?;
    let invitation = ContactInvitation::decode(&invitation)?;
    Ok(format!(
        "inviter_public_key={}\ninviter_identifier={}\n",
        hexadecimal(invitation.inviter().as_bytes()),
        hexadecimal(IdentityIdentifier::derive(invitation.inviter()).as_bytes()),
    ))
}

fn import_system_contact_invitation(
    state_directory: &Path,
    encoded: &str,
) -> Result<String, Box<dyn Error>> {
    validate_state_directory(state_directory)?;
    let _runtime =
        DaemonRuntime::start(arachne_protocol::ProtocolVersion::INITIAL, state_directory)?;
    let local_identity = load_client_system_identity(state_directory)?.public_key();
    #[cfg(target_os = "linux")]
    {
        let mut keystore = LinuxKeystore::new()?;
        return import_contact_invitation(state_directory, &local_identity, encoded, &mut keystore);
    }
    #[cfg(target_os = "macos")]
    {
        let mut keystore = MacOsKeystore::new();
        return import_contact_invitation(state_directory, &local_identity, encoded, &mut keystore);
    }
    #[cfg(target_os = "windows")]
    {
        let mut keystore = WindowsKeystore::new()?;
        return import_contact_invitation(state_directory, &local_identity, encoded, &mut keystore);
    }
    #[allow(unreachable_code)]
    Err("unsupported operating system keystore".into())
}

fn import_contact_invitation<K: OsKeystore>(
    state_directory: &Path,
    local_identity: &IdentityPublicKey,
    encoded: &str,
    keystore: &mut K,
) -> Result<String, Box<dyn Error>> {
    let mut invitation = vec![0; CONTACT_INVITATION_BYTES];
    decode_canonical_hex(encoded, &mut invitation)?;
    let contacts_path = ClientStateDirectory::new(state_directory)?.contacts_path();
    if let Some(parent) = contacts_path.parent() {
        fs::create_dir_all(parent)?;
    }
    let mut contacts = ContactStore::open(&contacts_path, keystore)?;
    let contact = PendingContactImportService::new(local_identity, &mut contacts)
        .import_encoded(&invitation)?;
    Ok(format!(
        "contact_public_key={}\nstatus={}\n",
        hexadecimal(contact.identity().as_bytes()),
        contact_status_label(contact.status())
    ))
}

fn verify_system_contact_qr(
    state_directory: &Path,
    encoded: &str,
) -> Result<String, Box<dyn Error>> {
    validate_state_directory(state_directory)?;
    let _runtime =
        DaemonRuntime::start(arachne_protocol::ProtocolVersion::INITIAL, state_directory)?;
    let local_identity = load_client_system_identity(state_directory)?.public_key();
    #[cfg(target_os = "linux")]
    {
        let mut keystore = LinuxKeystore::new()?;
        return verify_contact_qr(state_directory, &local_identity, encoded, &mut keystore);
    }
    #[cfg(target_os = "macos")]
    {
        let mut keystore = MacOsKeystore::new();
        return verify_contact_qr(state_directory, &local_identity, encoded, &mut keystore);
    }
    #[cfg(target_os = "windows")]
    {
        let mut keystore = WindowsKeystore::new()?;
        return verify_contact_qr(state_directory, &local_identity, encoded, &mut keystore);
    }
    #[allow(unreachable_code)]
    Err("unsupported operating system keystore".into())
}

fn verify_contact_qr<K: OsKeystore>(
    state_directory: &Path,
    local_identity: &IdentityPublicKey,
    encoded: &str,
    keystore: &mut K,
) -> Result<String, Box<dyn Error>> {
    let mut payload = vec![0; QR_VERIFICATION_PAYLOAD_BYTES];
    decode_canonical_hex(encoded, &mut payload)?;
    let contacts_path = ClientStateDirectory::new(state_directory)?.contacts_path();
    if let Some(parent) = contacts_path.parent() {
        fs::create_dir_all(parent)?;
    }
    let mut contacts = ContactStore::open(&contacts_path, keystore)?;
    let contact = QrContactVerificationService::new(local_identity, &mut contacts)
        .verify_encoded(&payload)?;
    Ok(format!(
        "contact_public_key={}\nstatus={}\n",
        hexadecimal(contact.identity().as_bytes()),
        contact_status_label(contact.status())
    ))
}

fn verify_system_contact_safety_number(
    state_directory: &Path,
    contact_public_key: &str,
    safety_number: &str,
) -> Result<String, Box<dyn Error>> {
    validate_state_directory(state_directory)?;
    let _runtime =
        DaemonRuntime::start(arachne_protocol::ProtocolVersion::INITIAL, state_directory)?;
    let local_identity = load_client_system_identity(state_directory)?.public_key();
    let remote_identity = decode_identity_public_key(contact_public_key)?;
    #[cfg(target_os = "linux")]
    {
        let mut keystore = LinuxKeystore::new()?;
        return verify_contact_safety_number(
            state_directory,
            &local_identity,
            &remote_identity,
            safety_number,
            &mut keystore,
        );
    }
    #[cfg(target_os = "macos")]
    {
        let mut keystore = MacOsKeystore::new();
        return verify_contact_safety_number(
            state_directory,
            &local_identity,
            &remote_identity,
            safety_number,
            &mut keystore,
        );
    }
    #[cfg(target_os = "windows")]
    {
        let mut keystore = WindowsKeystore::new()?;
        return verify_contact_safety_number(
            state_directory,
            &local_identity,
            &remote_identity,
            safety_number,
            &mut keystore,
        );
    }
    #[allow(unreachable_code)]
    Err("unsupported operating system keystore".into())
}

fn verify_contact_safety_number<K: OsKeystore>(
    state_directory: &Path,
    local_identity: &IdentityPublicKey,
    remote_identity: &IdentityPublicKey,
    encoded: &str,
    keystore: &mut K,
) -> Result<String, Box<dyn Error>> {
    let mut safety_number = [0; SAFETY_NUMBER_FINGERPRINT_BYTES];
    decode_canonical_hex(encoded, &mut safety_number)?;
    let contacts_path = ClientStateDirectory::new(state_directory)?.contacts_path();
    if let Some(parent) = contacts_path.parent() {
        fs::create_dir_all(parent)?;
    }
    let mut contacts = ContactStore::open(&contacts_path, keystore)?;
    let contact = SafetyNumberVerificationService::new(local_identity, &mut contacts)
        .verify(remote_identity, &safety_number)?;
    Ok(format!(
        "contact_public_key={}\nstatus={}\n",
        hexadecimal(contact.identity().as_bytes()),
        contact_status_label(contact.status())
    ))
}

fn rotate_system_contact(state_directory: &Path, encoded: &str) -> Result<String, Box<dyn Error>> {
    validate_state_directory(state_directory)?;
    let _runtime =
        DaemonRuntime::start(arachne_protocol::ProtocolVersion::INITIAL, state_directory)?;
    let local_identity = load_client_system_identity(state_directory)?.public_key();
    #[cfg(target_os = "linux")]
    {
        let mut keystore = LinuxKeystore::new()?;
        return apply_contact_rotation(state_directory, &local_identity, encoded, &mut keystore);
    }
    #[cfg(target_os = "macos")]
    {
        let mut keystore = MacOsKeystore::new();
        return apply_contact_rotation(state_directory, &local_identity, encoded, &mut keystore);
    }
    #[cfg(target_os = "windows")]
    {
        let mut keystore = WindowsKeystore::new()?;
        return apply_contact_rotation(state_directory, &local_identity, encoded, &mut keystore);
    }
    #[allow(unreachable_code)]
    Err("unsupported operating system keystore".into())
}

fn apply_contact_rotation<K: OsKeystore>(
    state_directory: &Path,
    local_identity: &IdentityPublicKey,
    encoded: &str,
    keystore: &mut K,
) -> Result<String, Box<dyn Error>> {
    let encoded = decode_bounded_canonical_hex(encoded, IDENTITY_ROTATION_BYTES)?;
    let contacts_path = ClientStateDirectory::new(state_directory)?.contacts_path();
    if let Some(parent) = contacts_path.parent() {
        fs::create_dir_all(parent)?;
    }
    let mut contacts = ContactStore::open(&contacts_path, keystore)?;
    let contact = ContactLifecycleService::new(local_identity, &mut contacts)
        .apply_rotation_encoded(&encoded)?;
    Ok(format!(
        "contact_public_key={}\nstatus={}\n",
        hexadecimal(contact.identity().as_bytes()),
        contact_status_label(contact.status())
    ))
}

fn revoke_system_contact(
    state_directory: &Path,
    contact_public_key: &str,
) -> Result<String, Box<dyn Error>> {
    validate_state_directory(state_directory)?;
    let _runtime =
        DaemonRuntime::start(arachne_protocol::ProtocolVersion::INITIAL, state_directory)?;
    let local_identity = load_client_system_identity(state_directory)?.public_key();
    let contact_identity = decode_identity_public_key(contact_public_key)?;
    #[cfg(target_os = "linux")]
    {
        let mut keystore = LinuxKeystore::new()?;
        return revoke_contact(
            state_directory,
            &local_identity,
            &contact_identity,
            &mut keystore,
        );
    }
    #[cfg(target_os = "macos")]
    {
        let mut keystore = MacOsKeystore::new();
        return revoke_contact(
            state_directory,
            &local_identity,
            &contact_identity,
            &mut keystore,
        );
    }
    #[cfg(target_os = "windows")]
    {
        let mut keystore = WindowsKeystore::new()?;
        return revoke_contact(
            state_directory,
            &local_identity,
            &contact_identity,
            &mut keystore,
        );
    }
    #[allow(unreachable_code)]
    Err("unsupported operating system keystore".into())
}

fn revoke_contact<K: OsKeystore>(
    state_directory: &Path,
    local_identity: &IdentityPublicKey,
    contact_identity: &IdentityPublicKey,
    keystore: &mut K,
) -> Result<String, Box<dyn Error>> {
    let contacts_path = ClientStateDirectory::new(state_directory)?.contacts_path();
    if let Some(parent) = contacts_path.parent() {
        fs::create_dir_all(parent)?;
    }
    let mut contacts = ContactStore::open(&contacts_path, keystore)?;
    let contact =
        ContactLifecycleService::new(local_identity, &mut contacts).revoke(contact_identity)?;
    Ok(format!(
        "contact_public_key={}\nstatus={}\n",
        hexadecimal(contact.identity().as_bytes()),
        contact_status_label(contact.status())
    ))
}

const fn contact_status_label(status: ContactStatus) -> &'static str {
    match status {
        ContactStatus::Pending => "pending",
        ContactStatus::Verified => "verified",
        ContactStatus::Revoked => "revoked",
    }
}

fn relay_profile_record(
    onion_service_public_key_hex: &str,
    virtual_port: u16,
) -> Result<String, Box<dyn Error>> {
    let mut onion_service_public_key = [0; TOR_ONION_SERVICE_PUBLIC_KEY_BYTES];
    decode_canonical_hex(onion_service_public_key_hex, &mut onion_service_public_key)?;
    let profile = TorMaildropProfileConfig::new(onion_service_public_key, virtual_port)?;
    Ok(format!("profile={}\n", hexadecimal(&profile.encode()?)))
}

fn inspect_relay_profile(encoded: &str) -> Result<String, Box<dyn Error>> {
    let encoded = decode_bounded_canonical_hex(encoded, MAX_RELAY_PROFILE_BYTES)?;
    let profile = TorMaildropProfileConfig::decode(&encoded)?;
    if profile.encode()? != encoded {
        return Err("relay profile is not canonically encoded".into());
    }
    Ok(format!(
        "onion_service_public_key={}\nvirtual_port={}\n",
        hexadecimal(&profile.onion_service_public_key()),
        profile.virtual_port(),
    ))
}

fn decode_canonical_hex(encoded: &str, output: &mut [u8]) -> Result<(), &'static str> {
    if encoded.len() != output.len() * 2 || !is_canonical_hex(encoded) {
        return Err("hexadecimal input is not canonical or has an invalid length");
    }
    for (byte, pair) in output.iter_mut().zip(encoded.as_bytes().chunks_exact(2)) {
        let high = hexadecimal_nibble(pair[0]).ok_or("hexadecimal input is invalid")?;
        let low = hexadecimal_nibble(pair[1]).ok_or("hexadecimal input is invalid")?;
        *byte = high << 4 | low;
    }
    Ok(())
}

fn decode_bounded_canonical_hex(
    encoded: &str,
    maximum_bytes: usize,
) -> Result<Vec<u8>, &'static str> {
    if encoded.is_empty() || !encoded.len().is_multiple_of(2) || encoded.len() / 2 > maximum_bytes {
        return Err("hexadecimal input has an invalid length");
    }
    let mut output = vec![0; encoded.len() / 2];
    decode_canonical_hex(encoded, &mut output)?;
    Ok(output)
}

fn decode_identity_public_key(encoded: &str) -> Result<IdentityPublicKey, Box<dyn Error>> {
    let mut public_key = [0; 32];
    decode_canonical_hex(encoded, &mut public_key)?;
    Ok(IdentityPublicKey::from_bytes(public_key)?)
}

fn decode_envelope(encoded: &str) -> Result<EncryptedMessageEnvelope, Box<dyn Error>> {
    let encoded = decode_bounded_canonical_hex(encoded, MAX_ENCRYPTED_MESSAGE_SUBMISSION_BYTES)?;
    let envelope = EncryptedMessageEnvelope::decode(&encoded)?;
    if envelope.encode()? != encoded {
        return Err("encrypted message envelope is not canonically encoded".into());
    }
    Ok(envelope)
}

fn run_courier_command(command: CourierCommand) -> Result<(), Box<dyn Error>> {
    match command {
        CourierCommand::Bundle { command } => match command {
            CourierBundleCommand::Create {
                state_directory,
                relay_invitation,
                relay_tls_pin,
            } => print!(
                "{}",
                create_courier_bundle(&state_directory, &relay_invitation, &relay_tls_pin)?
            ),
            CourierBundleCommand::Import {
                state_directory,
                bundle,
            } => print!("{}", import_courier_bundle(&state_directory, &bundle)?),
            CourierBundleCommand::Export { state_directory } => {
                print!("{}", export_courier_bundle(&state_directory)?)
            }
        },
        CourierCommand::Send {
            config,
            recipient_public_key,
            text,
            ttl_seconds,
        } => print!(
            "{}",
            queue_courier_text(&config, &recipient_public_key, text, ttl_seconds,)?
        ),
        CourierCommand::Inbox { state_directory } => {
            print!("{}", read_courier_inbox(&state_directory)?)
        }
        CourierCommand::Attachment { command } => match command {
            CourierAttachmentCommand::Send {
                config,
                recipient_public_key,
                path,
                ttl_seconds,
            } => print!(
                "{}",
                send_courier_attachment(&config, &recipient_public_key, &path, ttl_seconds)?
            ),
            CourierAttachmentCommand::Receive {
                config,
                message_identifier,
                output,
            } => print!(
                "{}",
                receive_courier_attachment(&config, &message_identifier, &output)?
            ),
        },
        CourierCommand::Daemon { config } => courier_daemon_serve(&config)?,
    }
    Ok(())
}

fn decode_courier_bundle(encoded: &str) -> Result<CourierBundle, Box<dyn Error>> {
    let encoded =
        decode_bounded_canonical_hex(encoded, arachne_protocol::MAX_COURIER_BUNDLE_BYTES)?;
    let bundle = CourierBundle::decode(&encoded)?;
    if bundle.encode()? != encoded {
        return Err("courier bundle is not canonically encoded".into());
    }
    Ok(bundle)
}

fn decode_relay_invitation(encoded: &str) -> Result<RelayInvitation, Box<dyn Error>> {
    let encoded = decode_bounded_canonical_hex(encoded, MAX_RELAY_INVITATION_BYTES)?;
    let invitation = RelayInvitation::decode(&encoded)?;
    if invitation.encode()? != encoded {
        return Err("relay invitation is not canonically encoded".into());
    }
    Ok(invitation)
}

fn decode_relay_tls_pin(
    encoded: &str,
) -> Result<[u8; RELAY_TLS_CERTIFICATE_PIN_BYTES], Box<dyn Error>> {
    let mut pin = [0; RELAY_TLS_CERTIFICATE_PIN_BYTES];
    decode_canonical_hex(encoded, &mut pin)?;
    if pin.iter().all(|byte| *byte == 0) {
        return Err("relay TLS pin must not be all zeroes".into());
    }
    Ok(pin)
}

fn create_courier_bundle(
    state_directory: &Path,
    relay_invitation: &str,
    relay_tls_pin: &str,
) -> Result<String, Box<dyn Error>> {
    validate_state_directory(state_directory)?;
    initialize_client_logging(state_directory)?;
    let _runtime =
        DaemonRuntime::start(arachne_protocol::ProtocolVersion::INITIAL, state_directory)?;
    let invitation = decode_relay_invitation(relay_invitation)?;
    let pin = decode_relay_tls_pin(relay_tls_pin)?;
    let now = unix_time_seconds()?;
    #[cfg(target_os = "linux")]
    {
        let mut keystore = LinuxKeystore::new()?;
        return create_courier_bundle_with_keystore(
            &mut keystore,
            state_directory,
            invitation,
            pin,
            now,
        );
    }
    #[cfg(target_os = "macos")]
    {
        let mut keystore = MacOsKeystore::new();
        return create_courier_bundle_with_keystore(
            &mut keystore,
            state_directory,
            invitation,
            pin,
            now,
        );
    }
    #[cfg(target_os = "windows")]
    {
        let mut keystore = WindowsKeystore::new()?;
        return create_courier_bundle_with_keystore(
            &mut keystore,
            state_directory,
            invitation,
            pin,
            now,
        );
    }
    #[allow(unreachable_code)]
    Err("unsupported operating system keystore".into())
}

fn create_courier_bundle_with_keystore<K: OsKeystore>(
    keystore: &mut K,
    state_directory: &Path,
    invitation: RelayInvitation,
    pin: [u8; RELAY_TLS_CERTIFICATE_PIN_BYTES],
    now: u64,
) -> Result<String, Box<dyn Error>> {
    let layout = ClientStateDirectory::new(state_directory)?;
    let profile = ClientProfile::open_or_create(&layout)?;
    let (identity, _) = profile.create_or_load_identity(keystore)?;
    if invitation.recipient() != &identity.public_key() {
        return Err("relay invitation is not bound to the local identity".into());
    }
    let cryptographer = CourierCryptographer::load_or_create_for_profile(
        identity.keypair(),
        profile.id(),
        keystore,
    )?;
    let mut inventory =
        CourierOneTimePrekeyInventory::open(&layout.courier_one_time_prekeys_path(), keystore)?;
    inventory.replenish(COURIER_ONE_TIME_PREKEY_TARGET)?;
    let mut store = CourierBundleStore::open(&layout.courier_bundles_path(), keystore)?;
    let generation = match store.bundle_for(&identity.public_key(), now)? {
        Some(previous) => previous
            .generation()
            .checked_add(1)
            .ok_or("courier bundle generation exhausted")?,
        None => 1,
    };
    let bundle = cryptographer.bundle_with_prekeys(
        identity.keypair(),
        invitation,
        pin,
        generation,
        inventory.unpublished_public(),
    )?;
    store.import(&bundle, now)?;
    Ok(format!("bundle={}\n", hexadecimal(&bundle.encode()?)))
}

fn import_courier_bundle(state_directory: &Path, encoded: &str) -> Result<String, Box<dyn Error>> {
    validate_state_directory(state_directory)?;
    initialize_client_logging(state_directory)?;
    let _runtime =
        DaemonRuntime::start(arachne_protocol::ProtocolVersion::INITIAL, state_directory)?;
    let bundle = decode_courier_bundle(encoded)?;
    let now = unix_time_seconds()?;
    #[cfg(target_os = "linux")]
    {
        let mut keystore = LinuxKeystore::new()?;
        return import_courier_bundle_with_keystore(&mut keystore, state_directory, &bundle, now);
    }
    #[cfg(target_os = "macos")]
    {
        let mut keystore = MacOsKeystore::new();
        return import_courier_bundle_with_keystore(&mut keystore, state_directory, &bundle, now);
    }
    #[cfg(target_os = "windows")]
    {
        let mut keystore = WindowsKeystore::new()?;
        return import_courier_bundle_with_keystore(&mut keystore, state_directory, &bundle, now);
    }
    #[allow(unreachable_code)]
    Err("unsupported operating system keystore".into())
}

fn import_courier_bundle_with_keystore<K: OsKeystore>(
    keystore: &mut K,
    state_directory: &Path,
    bundle: &CourierBundle,
    now: u64,
) -> Result<String, Box<dyn Error>> {
    let layout = ClientStateDirectory::new(state_directory)?;
    let contacts = ContactStore::open(&layout.contacts_path(), keystore)?;
    if contacts
        .contact(bundle.publisher())
        .is_none_or(|contact| contact.status() != ContactStatus::Verified)
    {
        return Err("courier bundle publisher is not a verified contact".into());
    }
    let mut store = CourierBundleStore::open(&layout.courier_bundles_path(), keystore)?;
    store.import(bundle, now)?;
    Ok(format!(
        "contact_public_key={}\n",
        hexadecimal(bundle.publisher().as_bytes())
    ))
}

fn export_courier_bundle(state_directory: &Path) -> Result<String, Box<dyn Error>> {
    validate_state_directory(state_directory)?;
    let now = unix_time_seconds()?;
    #[cfg(target_os = "linux")]
    {
        let mut keystore = LinuxKeystore::new()?;
        return export_courier_bundle_with_keystore(&mut keystore, state_directory, now);
    }
    #[cfg(target_os = "macos")]
    {
        let mut keystore = MacOsKeystore::new();
        return export_courier_bundle_with_keystore(&mut keystore, state_directory, now);
    }
    #[cfg(target_os = "windows")]
    {
        let mut keystore = WindowsKeystore::new()?;
        return export_courier_bundle_with_keystore(&mut keystore, state_directory, now);
    }
    #[allow(unreachable_code)]
    Err("unsupported operating system keystore".into())
}

fn export_courier_bundle_with_keystore<K: OsKeystore>(
    keystore: &mut K,
    state_directory: &Path,
    now: u64,
) -> Result<String, Box<dyn Error>> {
    let layout = ClientStateDirectory::new(state_directory)?;
    let profile = ClientProfile::open_or_create(&layout)?;
    let identity = profile.load_identity(keystore)?;
    let store = CourierBundleStore::open(&layout.courier_bundles_path(), keystore)?;
    let bundle = store
        .bundle_for(&identity.public_key(), now)?
        .ok_or("local courier bundle is unavailable")?;
    Ok(format!("bundle={}\n", hexadecimal(&bundle.encode()?)))
}

fn queue_courier_text(
    config_path: &Path,
    recipient_public_key: &str,
    text: String,
    ttl_seconds: u32,
) -> Result<String, Box<dyn Error>> {
    let config = CourierDaemonConfig::load(config_path)?;
    validate_state_directory(config.state_directory())?;
    initialize_client_logging(config.state_directory())?;
    let recipient = decode_identity_public_key(recipient_public_key)?;
    let now = unix_time_seconds()?;
    let expiry = MessageExpiry::new(now, ttl_seconds)?;
    let runtime = tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .build()?;
    #[cfg(target_os = "linux")]
    {
        let mut keystore = LinuxKeystore::new()?;
        return runtime.block_on(queue_courier_text_with_keystore(
            &mut keystore,
            &config,
            recipient,
            text,
            expiry,
            now,
        ));
    }
    #[cfg(target_os = "macos")]
    {
        let mut keystore = MacOsKeystore::new();
        return runtime.block_on(queue_courier_text_with_keystore(
            &mut keystore,
            &config,
            recipient,
            text,
            expiry,
            now,
        ));
    }
    #[cfg(target_os = "windows")]
    {
        let mut keystore = WindowsKeystore::new()?;
        return runtime.block_on(queue_courier_text_with_keystore(
            &mut keystore,
            &config,
            recipient,
            text,
            expiry,
            now,
        ));
    }
    #[allow(unreachable_code)]
    Err("unsupported operating system keystore".into())
}

async fn queue_courier_text_with_keystore<K: OsKeystore>(
    keystore: &mut K,
    config: &CourierDaemonConfig,
    recipient: IdentityPublicKey,
    text: String,
    expiry: MessageExpiry,
    now: u64,
) -> Result<String, Box<dyn Error>> {
    let layout = ClientStateDirectory::new(config.state_directory())?;
    let profile = ClientProfile::open_or_create(&layout)?;
    let identity = profile.load_identity(keystore)?;
    let contacts = ContactStore::open(&layout.contacts_path(), keystore)?;
    if contacts
        .contact(&recipient)
        .is_none_or(|contact| contact.status() != ContactStatus::Verified)
    {
        return Err("courier recipient is not a verified contact".into());
    }
    let mut bundles = CourierBundleStore::open(&layout.courier_bundles_path(), keystore)?;
    let directory = bundles
        .bundle_for(&recipient, now)?
        .ok_or("courier recipient bundle is unavailable")?;
    let mut sessions = CourierSessionStore::open(&layout.courier_sessions_path(), keystore)?;
    let cryptographer = CourierCryptographer::load_or_create_for_profile(
        identity.keypair(),
        profile.id(),
        keystore,
    )?;
    let (bundle, selected_one_time_prekey) = if sessions.contains(&recipient) {
        (directory, None)
    } else {
        let fetched = CourierMaildropClient::new(config.tor().runtime())
            .fetch_courier_bundle(&directory, identity.keypair(), &recipient)
            .await?;
        bundles.import(fetched.bundle(), now)?;
        fetched.into_parts()
    };
    let payload = MessagePayload::new(MessageContentType::TextUtf8, text.into_bytes())?;
    let frame = cryptographer.encrypt_with_one_time_prekey(
        &identity.public_key(),
        &bundle,
        &mut sessions,
        &payload,
        selected_one_time_prekey,
    )?;
    let identifier = frame
        .message_identifier()
        .ok_or("courier text frame is missing its message identifier")?;
    let envelope = frame.into_envelope()?;
    let mut outbox = SenderOutbox::open(&layout.outbox_path(), keystore)?;
    outbox.enqueue_with_identifier(identifier, recipient, envelope, expiry)?;
    tracing::info!(
        target: "arachne.courier.delivery",
        event = "text_queued",
        "courier delivery event"
    );
    Ok(format!(
        "message_identifier={}\n",
        hexadecimal(identifier.as_bytes())
    ))
}

fn read_courier_inbox(state_directory: &Path) -> Result<String, Box<dyn Error>> {
    validate_state_directory(state_directory)?;
    #[cfg(target_os = "linux")]
    {
        let mut keystore = LinuxKeystore::new()?;
        return read_courier_inbox_with_keystore(&mut keystore, state_directory);
    }
    #[cfg(target_os = "macos")]
    {
        let mut keystore = MacOsKeystore::new();
        return read_courier_inbox_with_keystore(&mut keystore, state_directory);
    }
    #[cfg(target_os = "windows")]
    {
        let mut keystore = WindowsKeystore::new()?;
        return read_courier_inbox_with_keystore(&mut keystore, state_directory);
    }
    #[allow(unreachable_code)]
    Err("unsupported operating system keystore".into())
}

fn read_courier_inbox_with_keystore<K: OsKeystore>(
    keystore: &mut K,
    state_directory: &Path,
) -> Result<String, Box<dyn Error>> {
    let layout = ClientStateDirectory::new(state_directory)?;
    let sessions = CourierSessionStore::open(&layout.courier_sessions_path(), keystore)?;
    let mut output = String::new();
    for message in sessions.inbox_messages() {
        let text = match message.payload().content_type() {
            MessageContentType::TextUtf8 => std::str::from_utf8(message.payload().body())?,
            MessageContentType::Binary => "<binary payload>",
        };
        let _ = writeln!(
            output,
            "message_identifier={}\nsender_public_key={}\nreceived_at={}\ntext={}\n",
            hexadecimal(message.identifier().as_bytes()),
            hexadecimal(message.sender().as_bytes()),
            message.received_at(),
            text,
        );
    }
    Ok(output)
}

fn send_courier_attachment(
    config_path: &Path,
    recipient_public_key: &str,
    path: &Path,
    ttl_seconds: u32,
) -> Result<String, Box<dyn Error>> {
    let config = CourierDaemonConfig::load(config_path)?;
    validate_state_directory(config.state_directory())?;
    initialize_client_logging(config.state_directory())?;
    let recipient = decode_identity_public_key(recipient_public_key)?;
    let plaintext = fs::read(path)?;
    let now = unix_time_seconds()?;
    let runtime = tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .build()?;
    #[cfg(target_os = "linux")]
    {
        let mut keystore = LinuxKeystore::new()?;
        return runtime.block_on(send_courier_attachment_with_keystore(
            &mut keystore,
            &config,
            recipient,
            plaintext,
            ttl_seconds,
            now,
        ));
    }
    #[cfg(target_os = "macos")]
    {
        let mut keystore = MacOsKeystore::new();
        return runtime.block_on(send_courier_attachment_with_keystore(
            &mut keystore,
            &config,
            recipient,
            plaintext,
            ttl_seconds,
            now,
        ));
    }
    #[cfg(target_os = "windows")]
    {
        let mut keystore = WindowsKeystore::new()?;
        return runtime.block_on(send_courier_attachment_with_keystore(
            &mut keystore,
            &config,
            recipient,
            plaintext,
            ttl_seconds,
            now,
        ));
    }
    #[allow(unreachable_code)]
    Err("unsupported operating system keystore".into())
}

async fn send_courier_attachment_with_keystore<K: OsKeystore>(
    keystore: &mut K,
    config: &CourierDaemonConfig,
    recipient: IdentityPublicKey,
    plaintext: Vec<u8>,
    ttl_seconds: u32,
    now: u64,
) -> Result<String, Box<dyn Error>> {
    if plaintext.is_empty() || plaintext.len() > DEFAULT_REFERENCE_ATTACHMENT_MAX_BYTES as usize {
        return Err("attachment size is outside the supported courier bounds".into());
    }
    let layout = ClientStateDirectory::new(config.state_directory())?;
    let _profile = ClientProfile::open_or_create(&layout)?;
    let contacts = ContactStore::open(&layout.contacts_path(), keystore)?;
    if contacts
        .contact(&recipient)
        .is_none_or(|contact| contact.status() != ContactStatus::Verified)
    {
        return Err("courier recipient is not a verified contact".into());
    }
    let identifier = AttachmentIdentifier::generate()?;
    let mut message_key = [0; 32];
    SysRng
        .try_fill_bytes(&mut message_key)
        .map_err(|_| "attachment message key could not be generated")?;
    let key = AttachmentKey::derive(&message_key, identifier)?;
    let mut chunks = Vec::new();
    let mut hashes = Vec::new();
    for (index, plaintext_chunk) in plaintext.chunks(ATTACHMENT_CHUNK_BYTES).enumerate() {
        let mut padded = vec![0; ATTACHMENT_CHUNK_BYTES];
        padded[..plaintext_chunk.len()].copy_from_slice(plaintext_chunk);
        let index = u32::try_from(index).map_err(|_| "attachment has too many chunks")?;
        let chunk = EncryptedAttachmentChunk::encrypt(
            identifier,
            index,
            &key.derive_chunk_key(index)?,
            &padded,
        )?;
        hashes.push(chunk.hash()?);
        chunks.push(chunk);
    }
    let manifest =
        AttachmentManifest::new(identifier, plaintext.len() as u64, hashes)?.encrypt(&key)?;
    let reference = CourierAttachmentReference::new(identifier, message_key, manifest)?;
    let encoded_manifest = reference.manifest().encode()?;
    let store = AttachmentSubmissionStore::new(layout.clone());
    let mut submission = store.begin(&encoded_manifest)?;
    for chunk in &chunks {
        submission.append_chunk(&chunk.encode()?)?;
    }
    submission.finish()?;

    let message_identifier = MessageIdentifier::generate()?;
    let expiry = MessageExpiry::new(now, ttl_seconds)?;
    let mut jobs =
        CourierAttachmentJobStore::open(&layout.courier_attachment_jobs_path(), keystore)?;
    if let Err(error) = jobs.enqueue(recipient, message_identifier, reference, expiry) {
        let _ = store.delete(identifier);
        return Err(error.into());
    }
    tracing::info!(
        target: "arachne.courier.attachment",
        event = "staged",
        chunks = chunks.len(),
        "courier attachment awaits daemon upload"
    );
    Ok(format!(
        "message_identifier={}\nattachment_identifier={}\n",
        hexadecimal(message_identifier.as_bytes()),
        hexadecimal(identifier.as_bytes())
    ))
}

fn receive_courier_attachment(
    config_path: &Path,
    message_identifier: &str,
    output: &Path,
) -> Result<String, Box<dyn Error>> {
    let config = CourierDaemonConfig::load(config_path)?;
    validate_state_directory(config.state_directory())?;
    let message_identifier = decode_message_identifier(message_identifier)?;
    let now = unix_time_seconds()?;
    let runtime = tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .build()?;
    #[cfg(target_os = "linux")]
    {
        let mut keystore = LinuxKeystore::new()?;
        return runtime.block_on(receive_courier_attachment_with_keystore(
            &mut keystore,
            &config,
            message_identifier,
            output,
            now,
        ));
    }
    #[cfg(target_os = "macos")]
    {
        let mut keystore = MacOsKeystore::new();
        return runtime.block_on(receive_courier_attachment_with_keystore(
            &mut keystore,
            &config,
            message_identifier,
            output,
            now,
        ));
    }
    #[cfg(target_os = "windows")]
    {
        let mut keystore = WindowsKeystore::new()?;
        return runtime.block_on(receive_courier_attachment_with_keystore(
            &mut keystore,
            &config,
            message_identifier,
            output,
            now,
        ));
    }
    #[allow(unreachable_code)]
    Err("unsupported operating system keystore".into())
}

async fn receive_courier_attachment_with_keystore<K: OsKeystore>(
    keystore: &mut K,
    config: &CourierDaemonConfig,
    message_identifier: MessageIdentifier,
    output: &Path,
    now: u64,
) -> Result<String, Box<dyn Error>> {
    let layout = ClientStateDirectory::new(config.state_directory())?;
    let profile = ClientProfile::open_or_create(&layout)?;
    let identity = profile.load_identity(keystore)?;
    let sessions = CourierSessionStore::open(&layout.courier_sessions_path(), keystore)?;
    let message = sessions
        .inbox_message(message_identifier)
        .ok_or("courier attachment message is unavailable")?;
    if message.payload().content_type() != MessageContentType::Binary {
        return Err("courier message is not an attachment reference".into());
    }
    let reference = CourierAttachmentReference::decode(message.payload().body())?;
    let key = AttachmentKey::derive(reference.message_key(), reference.identifier())?;
    let manifest = reference.manifest().decrypt(&key)?;
    let bundles = CourierBundleStore::open(&layout.courier_bundles_path(), keystore)?;
    let local_bundle = bundles
        .bundle_for(&identity.public_key(), now)?
        .ok_or("local courier bundle is unavailable")?;
    let mut file = fs::OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(output)?;
    let maildrop = CourierMaildropClient::new(config.tor().runtime());
    let mut remaining = manifest.plaintext_length();
    for (index, expected_hash) in manifest.chunk_hashes().iter().enumerate() {
        let index = u32::try_from(index).map_err(|_| "attachment has too many chunks")?;
        let chunk = maildrop
            .download_attachment_chunk(&local_bundle, manifest.identifier(), index)
            .await?;
        chunk.validate_hash(expected_hash)?;
        let plaintext = chunk.decrypt(&key.derive_chunk_key(index)?)?;
        let write_length = usize::try_from(remaining.min(ATTACHMENT_CHUNK_BYTES as u64))?;
        file.write_all(&plaintext[..write_length])?;
        remaining = remaining.saturating_sub(write_length as u64);
    }
    if remaining != 0 {
        return Err("attachment manifest does not cover its plaintext length".into());
    }
    file.sync_all()?;
    tracing::info!(
        target: "arachne.courier.attachment",
        event = "received",
        chunk_count = manifest.chunk_hashes().len(),
        "courier attachment event"
    );
    Ok(format!("output={}\n", output.display()))
}

fn decode_message_identifier(encoded: &str) -> Result<MessageIdentifier, Box<dyn Error>> {
    let mut bytes = [0; 16];
    decode_canonical_hex(encoded, &mut bytes)?;
    Ok(MessageIdentifier::from_bytes(bytes)?)
}

fn unix_time_seconds() -> Result<u64, Box<dyn Error>> {
    Ok(SystemTime::now().duration_since(UNIX_EPOCH)?.as_secs())
}

fn queue_system_message(
    state_directory: &Path,
    recipient_public_key: &str,
    envelope: &str,
    created_at: u64,
    ttl_seconds: u32,
) -> Result<String, Box<dyn Error>> {
    validate_state_directory(state_directory)?;
    initialize_client_logging(state_directory)?;
    let _runtime =
        DaemonRuntime::start(arachne_protocol::ProtocolVersion::INITIAL, state_directory)?;
    let recipient = decode_identity_public_key(recipient_public_key)?;
    let envelope = decode_envelope(envelope)?;
    let expiry = MessageExpiry::new(created_at, ttl_seconds)?;
    #[cfg(target_os = "linux")]
    {
        let mut keystore = LinuxKeystore::new()?;
        return queue_message(&mut keystore, state_directory, recipient, envelope, expiry);
    }
    #[cfg(target_os = "macos")]
    {
        let mut keystore = MacOsKeystore::new();
        return queue_message(&mut keystore, state_directory, recipient, envelope, expiry);
    }
    #[cfg(target_os = "windows")]
    {
        let mut keystore = WindowsKeystore::new()?;
        return queue_message(&mut keystore, state_directory, recipient, envelope, expiry);
    }
    #[allow(unreachable_code)]
    Err("unsupported operating system keystore".into())
}

fn queue_message<K: OsKeystore>(
    keystore: &mut K,
    state_directory: &Path,
    recipient: IdentityPublicKey,
    envelope: EncryptedMessageEnvelope,
    expiry: MessageExpiry,
) -> Result<String, Box<dyn Error>> {
    let layout = ClientStateDirectory::new(state_directory)?;
    let mut outbox = SenderOutbox::open(&layout.outbox_path(), keystore)?;
    outbox.enqueue(recipient, envelope, expiry)?;
    let identifier = outbox
        .messages()
        .last()
        .ok_or("enqueued message is missing")?
        .identifier();
    tracing::info!(
        target: "arachne.client.delivery",
        event = "message_queued",
        "client delivery event"
    );
    Ok(format!(
        "message_identifier={}\n",
        hexadecimal(identifier.as_bytes())
    ))
}

struct Dashboard {
    identity: Vec<String>,
    contacts: Vec<String>,
    attachments: Vec<String>,
    inbox: Vec<String>,
    inbox_messages: Vec<InboxMessage>,
    outbox: Vec<String>,
    delivery_state: Vec<String>,
}

impl Dashboard {
    fn snapshot(&self) -> String {
        let mut output = String::new();
        append_dashboard_section(&mut output, "Identity", &self.identity);
        append_dashboard_section(&mut output, "Contacts", &self.contacts);
        append_dashboard_section(&mut output, "Attachments", &self.attachments);
        append_dashboard_section(&mut output, "Inbox", &self.inbox);
        append_dashboard_section(&mut output, "Outbox", &self.outbox);
        append_dashboard_section(&mut output, "Delivery state", &self.delivery_state);
        output
    }

    fn with_identity(
        mut self,
        identity: IdentityPublicKey,
        initialization: ClientIdentityInitialization,
    ) -> Self {
        self.identity = vec![
            format!("status={}", identity_initialization_label(initialization)),
            format!(
                "identifier={}",
                hexadecimal(IdentityIdentifier::derive(&identity).as_bytes())
            ),
        ];
        self
    }
}

fn append_dashboard_section(output: &mut String, title: &str, lines: &[String]) {
    let _ = writeln!(output, "{title}:");
    for line in lines {
        let _ = writeln!(output, "  {line}");
    }
}

fn load_system_dashboard_with_keystore(
    state_directory: &Path,
) -> Result<Dashboard, Box<dyn Error>> {
    #[cfg(target_os = "linux")]
    {
        let mut keystore = LinuxKeystore::new()?;
        return load_dashboard(&mut keystore, state_directory);
    }
    #[cfg(target_os = "macos")]
    {
        let mut keystore = MacOsKeystore::new();
        return load_dashboard(&mut keystore, state_directory);
    }
    #[cfg(target_os = "windows")]
    {
        let mut keystore = WindowsKeystore::new()?;
        return load_dashboard(&mut keystore, state_directory);
    }
    #[allow(unreachable_code)]
    Err("unsupported operating system keystore".into())
}

fn start_embedded_tui_client(state_directory: &Path) -> Result<SdkClient, Box<dyn Error>> {
    let config = SdkClientBuilder::new(state_directory.to_path_buf())
        .embedded()
        .event_buffer_capacity(1)
        .build()?;
    Ok(SdkClient::start(&config)?)
}

#[cfg(any(test, unix))]
const DAEMON_AUTH_TOKEN_ENTRY_PREFIX: &str = "daemon_auth_v1_";
#[cfg(any(test, unix))]
const DAEMON_AUTH_TOKEN_ENTRY_DIGEST_BYTES: usize = 24;

#[cfg(any(test, unix))]
fn daemon_auth_token_entry(state_directory: &Path) -> Result<KeystoreEntryName, Box<dyn Error>> {
    let canonical = fs::canonicalize(state_directory)?;
    let digest = Sha256::digest(canonical.as_os_str().as_encoded_bytes());
    Ok(KeystoreEntryName::new(format!(
        "{DAEMON_AUTH_TOKEN_ENTRY_PREFIX}{}",
        hexadecimal(&digest[..DAEMON_AUTH_TOKEN_ENTRY_DIGEST_BYTES])
    ))?)
}

#[cfg(unix)]
fn load_daemon_auth_token<K: OsKeystore>(
    keystore: &K,
    state_directory: &Path,
) -> Result<DaemonLocalAuthToken, Box<dyn Error>> {
    let entry = daemon_auth_token_entry(state_directory)?;
    let secret = keystore
        .load(&entry)?
        .ok_or("daemon local authentication token is unavailable")?;
    Ok(DaemonLocalAuthToken::from_bytes(secret.as_bytes())?)
}

#[cfg(unix)]
async fn serve_daemon_with_keystore<K: OsKeystore>(
    config_path: &Path,
    keystore: &mut K,
) -> Result<(), Box<dyn Error>> {
    let config = SharedIpMeshConfig::load(config_path)?;
    initialize_file_tracing(config.state_directory(), "daemon")?;
    tracing::info!(
        target: "arachne.daemon.lifecycle",
        event = "starting",
        "daemon lifecycle event"
    );
    let mut runtime = DaemonRuntime::start(
        arachne_protocol::ProtocolVersion::INITIAL,
        config.state_directory(),
    )?;
    let profile = ClientProfile::open_or_create(runtime.state_directory())?;
    let local_identity = profile.load_identity(keystore)?;
    let tls_identity = SharedIpMeshTlsIdentity::load(runtime.state_directory(), keystore)?;
    let mesh = Arc::new(SharedIpMeshEndpoint::start(
        config,
        local_identity.public_key(),
        &tls_identity,
    )?);
    let accepting_mesh = Arc::clone(&mesh);
    let accept_task = tokio::spawn(async move {
        loop {
            match accepting_mesh.accept(local_identity.keypair()).await {
                Ok(connection) => drop(connection),
                Err(arachne_daemon::SharedIpMeshError::Direct(
                    arachne_daemon::DirectTransportError::Shutdown,
                )) => break,
                Err(error) => eprintln!("shared-IP mesh connection rejected: {error}"),
            }
        }
    });
    let entry = daemon_auth_token_entry(runtime.state_directory().root())?;
    let (auth, token) = DaemonLocalAuth::initialize()?;
    let secret = KeystoreSecret::new(token.as_bytes().to_vec())?;
    keystore.store(&entry, &secret)?;
    let served = {
        let server = DaemonServer::bind(&runtime, auth.clone());
        match server {
            Ok(server) => {
                server
                    .serve_until(async {
                        let _ = tokio::signal::ctrl_c().await;
                    })
                    .await
            }
            Err(error) => Err(error),
        }
    };
    let revoked = auth.revoke();
    let deleted = keystore.delete(&entry);
    let mesh_shutdown = mesh.shutdown();
    let accepted = accept_task.await;
    let shutdown = runtime.shutdown();
    let result = (|| {
        served?;
        revoked?;
        deleted?;
        mesh_shutdown?;
        accepted?;
        shutdown?;
        Ok(())
    })();
    if result.is_ok() {
        tracing::info!(
            target: "arachne.daemon.lifecycle",
            event = "stopped",
            "daemon lifecycle event"
        );
    } else {
        tracing::error!(
            target: "arachne.daemon.lifecycle",
            event = "stopped_with_error",
            error_class = "runtime",
            "daemon lifecycle event"
        );
    }
    result
}

#[cfg(unix)]
fn daemon_serve(config_path: &Path) -> Result<(), Box<dyn Error>> {
    let runtime = tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .build()?;
    #[cfg(target_os = "linux")]
    {
        let mut keystore = LinuxKeystore::new()?;
        return runtime.block_on(serve_daemon_with_keystore(config_path, &mut keystore));
    }
    #[cfg(target_os = "macos")]
    {
        let mut keystore = MacOsKeystore::new();
        return runtime.block_on(serve_daemon_with_keystore(config_path, &mut keystore));
    }
    #[allow(unreachable_code)]
    Err("unsupported operating system keystore".into())
}

async fn serve_courier_daemon_with_keystore<K: OsKeystore>(
    config_path: &Path,
    keystore: &mut K,
) -> Result<(), Box<dyn Error>> {
    let config = CourierDaemonConfig::load(config_path)?;
    initialize_file_tracing(config.state_directory(), "courier")?;
    let mut runtime = DaemonRuntime::start(
        arachne_protocol::ProtocolVersion::INITIAL,
        config.state_directory(),
    )?;
    let mut daemon = CourierDaemon::start(&config, keystore, unix_time_seconds()?)?;
    tracing::info!(
        target: "arachne.courier.lifecycle",
        event = "started",
        "courier daemon lifecycle event"
    );
    let mut interval = tokio::time::interval(config.poll_interval());
    loop {
        tokio::select! {
            _ = tokio::signal::ctrl_c() => break,
            _ = interval.tick() => match daemon.run_cycle(unix_time_seconds()?).await {
                Ok(cycle) => tracing::info!(
                    target: "arachne.courier.cycle",
                    event = "completed",
                    uploaded = cycle.uploaded(),
                    received = cycle.received(),
                    acknowledged = cycle.acknowledged(),
                    expired = cycle.expired(),
                    failed = cycle.failed(),
                    attachment_chunks = cycle.attachment_chunks(),
                    "courier daemon cycle"
                ),
                Err(_) => tracing::warn!(
                    target: "arachne.courier.cycle",
                    event = "failed",
                    error_class = "courier_cycle",
                    "courier daemon cycle failed"
                ),
            }
        }
    }
    runtime.shutdown()?;
    tracing::info!(
        target: "arachne.courier.lifecycle",
        event = "stopped",
        "courier daemon lifecycle event"
    );
    Ok(())
}

fn courier_daemon_serve(config_path: &Path) -> Result<(), Box<dyn Error>> {
    let runtime = tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .build()?;
    #[cfg(target_os = "linux")]
    {
        let mut keystore = LinuxKeystore::new()?;
        return runtime.block_on(serve_courier_daemon_with_keystore(
            config_path,
            &mut keystore,
        ));
    }
    #[cfg(target_os = "macos")]
    {
        let mut keystore = MacOsKeystore::new();
        return runtime.block_on(serve_courier_daemon_with_keystore(
            config_path,
            &mut keystore,
        ));
    }
    #[cfg(target_os = "windows")]
    {
        let mut keystore = WindowsKeystore::new()?;
        return runtime.block_on(serve_courier_daemon_with_keystore(
            config_path,
            &mut keystore,
        ));
    }
    #[allow(unreachable_code)]
    Err("unsupported operating system keystore".into())
}

#[cfg(windows)]
async fn serve_mesh_daemon_with_keystore<K: OsKeystore + Sync>(
    config_path: &Path,
    keystore: &K,
) -> Result<(), Box<dyn Error>> {
    let config = SharedIpMeshConfig::load(config_path)?;
    let mut runtime = DaemonRuntime::start(
        arachne_protocol::ProtocolVersion::INITIAL,
        config.state_directory(),
    )?;
    let profile = ClientProfile::open_or_create(runtime.state_directory())?;
    let local_identity = profile.load_identity(keystore)?;
    let tls_identity = SharedIpMeshTlsIdentity::load(runtime.state_directory(), keystore)?;
    let mesh = Arc::new(SharedIpMeshEndpoint::start(
        config,
        local_identity.public_key(),
        &tls_identity,
    )?);
    let accepting_mesh = Arc::clone(&mesh);
    let accept_task = tokio::spawn(async move {
        loop {
            match accepting_mesh.accept(local_identity.keypair()).await {
                Ok(connection) => drop(connection),
                Err(arachne_daemon::SharedIpMeshError::Direct(
                    arachne_daemon::DirectTransportError::Shutdown,
                )) => break,
                Err(error) => eprintln!("shared-IP mesh connection rejected: {error}"),
            }
        }
    });
    let interrupted = tokio::signal::ctrl_c().await;
    let mesh_shutdown = mesh.shutdown();
    let accepted = accept_task.await;
    let shutdown = runtime.shutdown();
    interrupted?;
    mesh_shutdown?;
    accepted?;
    shutdown?;
    Ok(())
}

#[cfg(windows)]
fn daemon_serve(config_path: &Path) -> Result<(), Box<dyn Error>> {
    let runtime = tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .build()?;
    let keystore = WindowsKeystore::new()?;
    runtime.block_on(serve_mesh_daemon_with_keystore(config_path, &keystore))
}

#[cfg(all(not(unix), not(windows)))]
fn daemon_serve(_config_path: &Path) -> Result<(), Box<dyn Error>> {
    Err("daemon serve is unavailable on this platform".into())
}

enum TuiRuntime {
    Embedded(SdkClient),
    #[cfg(unix)]
    Daemon(DaemonTuiClient),
}

impl TuiRuntime {
    fn is_running(&self) -> bool {
        match self {
            Self::Embedded(client) => client.is_running(),
            #[cfg(unix)]
            Self::Daemon(client) => client.is_running(),
        }
    }

    fn refresh_dashboard(&mut self, state_directory: &Path) -> Result<Dashboard, Box<dyn Error>> {
        match self {
            Self::Embedded(_) => load_system_dashboard_with_keystore(state_directory),
            #[cfg(unix)]
            Self::Daemon(client) => client.dashboard(),
        }
    }

    fn submit_contact_input(
        &mut self,
        state_directory: &Path,
        input: &TuiContactInput,
    ) -> Result<(), Box<dyn Error>> {
        match self {
            Self::Embedded(_) => submit_tui_contact_input(state_directory, input),
            #[cfg(unix)]
            Self::Daemon(client) => client.submit_contact_input(input),
        }
    }

    fn submit_message(
        &mut self,
        state_directory: &Path,
        input: &TuiMessageInput,
    ) -> Result<(), Box<dyn Error>> {
        match self {
            Self::Embedded(client) => submit_tui_message(client, state_directory, input),
            #[cfg(unix)]
            Self::Daemon(client) => client.submit_message(input),
        }
    }

    fn shutdown(&mut self) -> Result<(), Box<dyn Error>> {
        match self {
            Self::Embedded(client) => Ok(client.shutdown()?),
            #[cfg(unix)]
            Self::Daemon(_) => Ok(()),
        }
    }
}

#[cfg(unix)]
struct DaemonTuiClient {
    runtime: tokio::runtime::Runtime,
    client: DaemonServiceClient<Channel>,
    token: DaemonLocalAuthToken,
    running: bool,
}

#[cfg(unix)]
impl DaemonTuiClient {
    fn connect(state_directory: &Path) -> Result<Self, Box<dyn Error>> {
        let token = load_system_daemon_auth_token(state_directory)?;
        let socket_path = state_directory.join(arachne_daemon::DAEMON_UNIX_SOCKET_FILE);
        Self::connect_with_token(socket_path, token)
    }

    fn connect_with_token(
        socket_path: PathBuf,
        token: DaemonLocalAuthToken,
    ) -> Result<Self, Box<dyn Error>> {
        let runtime = tokio::runtime::Builder::new_multi_thread()
            .enable_all()
            .build()?;
        let channel = runtime.block_on(async move {
            Endpoint::from_static("http://[::]:50051")
                .connect_with_connector(service_fn(move |_| {
                    let socket_path = socket_path.clone();
                    async move { UnixStream::connect(socket_path).await.map(TokioIo::new) }
                }))
                .await
        })?;
        let mut client = Self {
            runtime,
            client: DaemonServiceClient::new(channel),
            token,
            running: false,
        };
        client.start()?;
        Ok(client)
    }

    fn is_running(&self) -> bool {
        self.running
    }

    fn request<T>(&self, value: T) -> Request<T> {
        let mut request = Request::new(value);
        request.metadata_mut().insert_bin(
            arachne_daemon::LOCAL_AUTH_TOKEN_METADATA_KEY,
            self.token.metadata_value(),
        );
        request
    }

    fn start(&mut self) -> Result<(), Box<dyn Error>> {
        let response = self.runtime.block_on(
            self.client
                .start_client(self.request(StartClientRequest {})),
        )?;
        let response = response.into_inner();
        if response.api_major != u32::from(arachne_protocol::ProtocolVersion::INITIAL.get())
            || !response.running
        {
            return Err("daemon protocol is incompatible or not running".into());
        }
        self.running = true;
        Ok(())
    }

    fn dashboard(&mut self) -> Result<Dashboard, Box<dyn Error>> {
        let identity_response = self.runtime.block_on(
            self.client
                .create_or_load_identity(self.request(CreateOrLoadIdentityRequest {})),
        )?;
        let identity_response = identity_response.into_inner();
        let public_key: [u8; 32] = identity_response
            .public_key
            .try_into()
            .map_err(|_| "daemon identity is invalid")?;
        let identity = IdentityPublicKey::from_bytes(public_key)?;
        let contacts = self
            .runtime
            .block_on(
                self.client
                    .list_contacts(self.request(ListContactsRequest {})),
            )?
            .into_inner()
            .contacts;
        let mut contacts: Vec<_> = contacts
            .into_iter()
            .map(|contact| {
                let status = RpcContactStatus::try_from(contact.status)
                    .map_err(|_| "daemon contact status is invalid")?;
                let verification =
                    RpcContactVerificationMethod::try_from(contact.verification_method)
                        .map_err(|_| "daemon contact verification method is invalid")?;
                let public_key: [u8; 32] = contact
                    .identity
                    .try_into()
                    .map_err(|_| "daemon contact identity is invalid")?;
                Ok(format!(
                    "contact={} status={} verification={}",
                    hexadecimal(&public_key),
                    daemon_contact_status_label(status),
                    daemon_contact_verification_label(verification)
                ))
            })
            .collect::<Result<_, Box<dyn Error>>>()?;
        contacts.sort_unstable();
        if contacts.is_empty() {
            contacts.push("no contacts".to_owned());
        }
        Ok(Dashboard {
            identity: vec![
                format!(
                    "status={}",
                    daemon_identity_initialization_label(identity_response.initialization)
                ),
                format!(
                    "identifier={}",
                    hexadecimal(IdentityIdentifier::derive(&identity).as_bytes())
                ),
            ],
            contacts,
            attachments: vec!["daemon attachment list is unavailable via API".to_owned()],
            inbox: vec!["daemon inbox list is unavailable via API".to_owned()],
            inbox_messages: Vec::new(),
            outbox: vec!["daemon outbox list is unavailable via API".to_owned()],
            delivery_state: vec!["daemon delivery list is unavailable via API".to_owned()],
        })
    }

    fn submit_contact_input(&mut self, input: &TuiContactInput) -> Result<(), Box<dyn Error>> {
        match input.kind {
            TuiContactInputKind::Invitation => {
                let invitation =
                    decode_bounded_canonical_hex(&input.value, CONTACT_INVITATION_BYTES)?;
                let _ = self
                    .runtime
                    .block_on(self.client.import_contact_invitation(
                        self.request(ImportContactInvitationRequest { invitation }),
                    ))?;
            }
            TuiContactInputKind::QrVerification => {
                let payload =
                    decode_bounded_canonical_hex(&input.value, QR_VERIFICATION_PAYLOAD_BYTES)?;
                let _ = self.runtime.block_on(
                    self.client
                        .verify_contact_qr(self.request(VerifyContactQrRequest { payload })),
                )?;
            }
            TuiContactInputKind::SafetyNumberVerification => {
                let (contact_public_key, safety_number) = tui_safety_number_parts(&input.value)?;
                let identity = decode_identity_public_key(contact_public_key)?;
                let fingerprint =
                    decode_bounded_canonical_hex(safety_number, SAFETY_NUMBER_FINGERPRINT_BYTES)?;
                let _ = self
                    .runtime
                    .block_on(self.client.verify_contact_safety_number(self.request(
                        VerifyContactSafetyNumberRequest {
                            identity: identity.as_bytes().to_vec(),
                            fingerprint,
                        },
                    )))?;
            }
        }
        Ok(())
    }

    fn submit_message(&mut self, input: &TuiMessageInput) -> Result<(), Box<dyn Error>> {
        let recipient = decode_identity_public_key(&input.recipient)?;
        let envelope =
            decode_bounded_canonical_hex(&input.envelope, MAX_ENCRYPTED_MESSAGE_SUBMISSION_BYTES)?;
        let _ = SdkMessageEnvelope::from_encoded(&envelope)?;
        let ttl_seconds = input.ttl_seconds.parse::<u32>()?;
        let created_at = SystemTime::now().duration_since(UNIX_EPOCH)?.as_secs();
        let _ = SdkMessageExpiry::new(created_at, ttl_seconds)?;
        let contacts = self
            .runtime
            .block_on(
                self.client
                    .list_contacts(self.request(ListContactsRequest {})),
            )?
            .into_inner()
            .contacts;
        let verified = contacts.iter().any(|contact| {
            contact.identity == recipient.as_bytes()
                && RpcContactStatus::try_from(contact.status) == Ok(RpcContactStatus::Verified)
        });
        if !verified {
            return Err("recipient contact is not verified".into());
        }
        let _ = self
            .runtime
            .block_on(self.client.send_message(self.request(SendMessageRequest {
                recipient: recipient.as_bytes().to_vec(),
                envelope,
                created_at,
                ttl_seconds,
            })))?;
        Ok(())
    }
}

#[cfg(unix)]
fn load_system_daemon_auth_token(
    state_directory: &Path,
) -> Result<DaemonLocalAuthToken, Box<dyn Error>> {
    #[cfg(target_os = "linux")]
    {
        return load_daemon_auth_token(&LinuxKeystore::new()?, state_directory);
    }
    #[cfg(target_os = "macos")]
    {
        return load_daemon_auth_token(&MacOsKeystore::new(), state_directory);
    }
    #[allow(unreachable_code)]
    Err("unsupported operating system keystore".into())
}

#[cfg(unix)]
const fn daemon_identity_initialization_label(initialization: i32) -> &'static str {
    match initialization {
        1 => "created",
        2 => "loaded",
        3 => "recovered",
        _ => "unavailable",
    }
}

#[cfg(unix)]
const fn daemon_contact_status_label(status: RpcContactStatus) -> &'static str {
    match status {
        RpcContactStatus::Pending => "pending",
        RpcContactStatus::Verified => "verified",
        RpcContactStatus::Revoked => "revoked",
        RpcContactStatus::Unspecified => "unavailable",
    }
}

#[cfg(unix)]
const fn daemon_contact_verification_label(method: RpcContactVerificationMethod) -> &'static str {
    match method {
        RpcContactVerificationMethod::Qr => "qr",
        RpcContactVerificationMethod::SafetyNumber => "safety_number",
        RpcContactVerificationMethod::Unspecified => "none",
    }
}

fn tui(state_directory: &Path, snapshot: bool, daemon: bool) -> Result<(), Box<dyn Error>> {
    validate_state_directory(state_directory)?;
    initialize_client_logging(state_directory)?;
    let (mut runtime, dashboard) = if daemon {
        #[cfg(unix)]
        {
            let mut client = DaemonTuiClient::connect(state_directory)?;
            let dashboard = client.dashboard()?;
            (TuiRuntime::Daemon(client), dashboard)
        }
        #[cfg(not(unix))]
        return Err("daemon TUI mode is unavailable on this platform".into());
    } else {
        let (identity, initialization) = create_or_load_client_system_identity(state_directory)?;
        let client = start_embedded_tui_client(state_directory)?;
        let dashboard = match load_system_dashboard_with_keystore(state_directory) {
            Ok(dashboard) => dashboard.with_identity(identity, initialization),
            Err(error) => {
                let mut runtime = TuiRuntime::Embedded(client);
                let _ = runtime.shutdown();
                return Err(error);
            }
        };
        (TuiRuntime::Embedded(client), dashboard)
    };
    if snapshot {
        print!("{}", dashboard.snapshot());
    } else {
        let result = run_dashboard(dashboard, state_directory, &mut runtime);
        let shutdown = runtime.shutdown();
        result?;
        shutdown?;
        return Ok(());
    }
    runtime.shutdown()?;
    Ok(())
}

fn load_dashboard<K: OsKeystore>(
    keystore: &mut K,
    state_directory: &Path,
) -> Result<Dashboard, Box<dyn Error>> {
    let layout = ClientStateDirectory::new(state_directory)?;
    let attachments = load_attachment_transfers(&layout)?;
    let inbox_path = layout.inbox_path();
    let outbox_path = layout.outbox_path();
    let contacts_path = layout.contacts_path();
    let inbox = state_file_exists(&inbox_path)?
        .then(|| RecipientInboxDeduplication::open(&inbox_path, keystore))
        .transpose()?;
    let outbox = state_file_exists(&outbox_path)?
        .then(|| SenderOutbox::open(&outbox_path, keystore))
        .transpose()?;
    let contacts = state_file_exists(&contacts_path)?
        .then(|| ContactStore::open(&contacts_path, keystore))
        .transpose()?;
    let mut dashboard = dashboard_from_stores(contacts.as_ref(), inbox.as_ref(), outbox.as_ref());
    dashboard.attachments = attachments;
    Ok(dashboard)
}

fn state_file_exists(path: &Path) -> Result<bool, Box<dyn Error>> {
    match fs::symlink_metadata(path) {
        Ok(metadata) if metadata.file_type().is_file() => Ok(true),
        Ok(_) => Err("state database path must be a regular file".into()),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(false),
        Err(error) => Err(error.into()),
    }
}

fn dashboard_from_stores(
    contacts: Option<&ContactStore>,
    inbox: Option<&RecipientInboxDeduplication>,
    outbox: Option<&SenderOutbox>,
) -> Dashboard {
    let contacts = contacts.map_or_else(
        || vec!["no contacts state".to_owned()],
        |contacts| {
            let mut contacts = contacts.contacts().to_vec();
            contacts.sort_unstable_by_key(|contact| *contact.identity().as_bytes());
            if contacts.is_empty() {
                vec!["no contacts".to_owned()]
            } else {
                contacts
                    .iter()
                    .map(|contact| {
                        format!(
                            "contact={} status={} verification={}",
                            hexadecimal(contact.identity().as_bytes()),
                            contact_status_label(contact.status()),
                            contact_verification_label(contact.verification_method())
                        )
                    })
                    .collect()
            }
        },
    );
    let inbox_messages = inbox.map_or_else(Vec::new, |inbox| {
        let mut messages: Vec<_> = inbox.messages().collect();
        messages.sort_unstable_by_key(|message| message.received_at());
        messages
    });
    let inbox = inbox.map_or_else(
        || vec!["no inbox state".to_owned()],
        |inbox| {
            let mut lines = vec![format!(
                "deduplication_entries={} retained_messages={}",
                inbox.len(),
                inbox_messages.len()
            )];
            if inbox_messages.is_empty() {
                lines.push("no retained message metadata".to_owned());
            } else {
                for (index, message) in inbox_messages.iter().enumerate() {
                    lines.push(format!(
                        "message={} received_at={} encrypted_header_bytes={} ciphertext_bytes={}",
                        index + 1,
                        message.received_at(),
                        message.encrypted_header_bytes(),
                        message.ciphertext_bytes()
                    ));
                }
            }
            lines
        },
    );
    let outbox_lines = match outbox {
        Some(outbox) if !outbox.messages().is_empty() => outbox
            .messages()
            .iter()
            .map(|message| {
                format!(
                    "message={} recipient={} expires_at={} encrypted_header_bytes={} ciphertext_bytes={}",
                    hexadecimal(message.identifier().as_bytes()),
                    hexadecimal(IdentityIdentifier::derive(message.recipient()).as_bytes()),
                    message.expiry().expires_at(),
                    message.envelope().encrypted_header().len(),
                    message.envelope().ciphertext().len()
                )
            })
            .collect(),
        Some(_) => vec!["no queued messages".to_owned()],
        None => vec!["no outbox state".to_owned()],
    };
    let delivery_state = match outbox {
        Some(outbox) if !outbox.delivery_statuses().is_empty() => outbox
            .delivery_statuses()
            .iter()
            .map(|status| {
                format!(
                    "message={} state={:?}",
                    hexadecimal(status.identifier().as_bytes()),
                    status.state()
                )
            })
            .collect(),
        Some(_) => vec!["no finalized deliveries".to_owned()],
        None => vec!["no outbox state".to_owned()],
    };
    Dashboard {
        identity: vec!["status=unavailable".to_owned()],
        contacts,
        attachments: vec!["no attachment transfers".to_owned()],
        inbox,
        inbox_messages,
        outbox: outbox_lines,
        delivery_state,
    }
}

fn load_attachment_transfers(layout: &ClientStateDirectory) -> Result<Vec<String>, Box<dyn Error>> {
    let root = layout.attachment_uploads_path();
    match fs::symlink_metadata(&root) {
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => {
            return Ok(vec!["no attachment transfers".to_owned()]);
        }
        Ok(metadata) if metadata.file_type().is_symlink() || !metadata.is_dir() => {
            return Err("attachment transfer root is invalid".into());
        }
        Ok(_) => {}
        Err(error) => return Err(error.into()),
    }
    let store = AttachmentSubmissionStore::new(layout.clone());
    let mut identifiers = Vec::new();
    for (index, entry) in fs::read_dir(&root)?.enumerate() {
        if index >= MAX_TUI_ATTACHMENT_TRANSFERS {
            return Err("attachment transfer count exceeds the configured limit".into());
        }
        let entry = entry?;
        let name = entry
            .file_name()
            .into_string()
            .map_err(|_| "attachment transfer name is invalid")?;
        let metadata = fs::symlink_metadata(entry.path())?;
        if metadata.file_type().is_symlink() || !metadata.is_dir() {
            return Err("attachment transfer path is invalid".into());
        }
        if is_attachment_staging_name(&name) {
            continue;
        }
        identifiers.push(decode_attachment_identifier(&name)?);
    }
    identifiers.sort_unstable_by_key(|identifier| *identifier.as_bytes());
    if identifiers.is_empty() {
        return Ok(vec!["no attachment transfers".to_owned()]);
    }
    identifiers
        .into_iter()
        .enumerate()
        .map(|(index, identifier)| {
            let status = store.status(identifier)?;
            Ok(format!(
                "transfer={} chunk_count={} complete={} next_pending_index={}",
                index + 1,
                status.chunk_count(),
                status.complete(),
                status
                    .next_pending_index()
                    .map_or_else(|| "none".to_owned(), |value| value.to_string())
            ))
        })
        .collect()
}

fn decode_attachment_identifier(encoded: &str) -> Result<AttachmentIdentifier, Box<dyn Error>> {
    let mut identifier = [0; ATTACHMENT_IDENTIFIER_BYTES];
    decode_canonical_hex(encoded, &mut identifier)?;
    Ok(AttachmentIdentifier::from_bytes(identifier)?)
}

fn is_attachment_staging_name(name: &str) -> bool {
    name.strip_prefix('.')
        .unwrap_or(name)
        .strip_suffix(".pending")
        .is_some_and(|identifier| decode_attachment_identifier(identifier).is_ok())
}

const fn contact_verification_label(method: Option<ContactVerificationMethod>) -> &'static str {
    match method {
        None => "none",
        Some(ContactVerificationMethod::Qr) => "qr",
        Some(ContactVerificationMethod::SafetyNumber) => "safety_number",
    }
}

struct TerminalRestoreGuard;

impl Drop for TerminalRestoreGuard {
    fn drop(&mut self) {
        let _ = terminal::disable_raw_mode();
        let _ = execute!(io::stdout(), Show, LeaveAlternateScreen);
    }
}

#[derive(Clone, Copy)]
enum TuiContactInputKind {
    Invitation,
    QrVerification,
    SafetyNumberVerification,
}

impl TuiContactInputKind {
    const fn prompt(self) -> &'static str {
        match self {
            Self::Invitation => "Invitation hexadecimal",
            Self::QrVerification => "QR verification hexadecimal",
            Self::SafetyNumberVerification => "Contact public key and safety number hexadecimal",
        }
    }
}

struct TuiContactInput {
    kind: TuiContactInputKind,
    value: String,
}

impl TuiContactInput {
    const fn new(kind: TuiContactInputKind) -> Self {
        Self {
            kind,
            value: String::new(),
        }
    }

    fn push(&mut self, character: char) {
        if self.value.len() + character.len_utf8() <= MAX_TUI_CONTACT_INPUT_BYTES {
            self.value.push(character);
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum TuiMessageInputStage {
    Recipient,
    Envelope,
    Ttl,
}

impl TuiMessageInputStage {
    const fn prompt(self) -> &'static str {
        match self {
            Self::Recipient => "Recipient public-key hexadecimal",
            Self::Envelope => "Canonical encrypted-envelope hexadecimal",
            Self::Ttl => "TTL seconds",
        }
    }
}

struct TuiMessageInput {
    stage: TuiMessageInputStage,
    recipient: String,
    envelope: String,
    ttl_seconds: String,
}

impl TuiMessageInput {
    const fn new() -> Self {
        Self {
            stage: TuiMessageInputStage::Recipient,
            recipient: String::new(),
            envelope: String::new(),
            ttl_seconds: String::new(),
        }
    }

    fn push(&mut self, character: char) {
        let (value, maximum, valid) = match self.stage {
            TuiMessageInputStage::Recipient => (
                &mut self.recipient,
                64,
                character.is_ascii_digit() || matches!(character, 'a'..='f'),
            ),
            TuiMessageInputStage::Envelope => (
                &mut self.envelope,
                MAX_ENCRYPTED_MESSAGE_SUBMISSION_BYTES * 2,
                character.is_ascii_digit() || matches!(character, 'a'..='f'),
            ),
            TuiMessageInputStage::Ttl => (&mut self.ttl_seconds, 10, character.is_ascii_digit()),
        };
        if valid && value.len() + character.len_utf8() <= maximum {
            value.push(character);
        }
    }

    fn pop(&mut self) {
        match self.stage {
            TuiMessageInputStage::Recipient => self.recipient.pop(),
            TuiMessageInputStage::Envelope => self.envelope.pop(),
            TuiMessageInputStage::Ttl => self.ttl_seconds.pop(),
        };
    }

    fn advance(&mut self) -> Result<bool, ()> {
        match self.stage {
            TuiMessageInputStage::Recipient => {
                decode_identity_public_key(&self.recipient).map_err(|_| ())?;
                self.stage = TuiMessageInputStage::Envelope;
                Ok(false)
            }
            TuiMessageInputStage::Envelope => {
                let encoded = decode_bounded_canonical_hex(
                    &self.envelope,
                    MAX_ENCRYPTED_MESSAGE_SUBMISSION_BYTES,
                )
                .map_err(|_| ())?;
                SdkMessageEnvelope::from_encoded(&encoded).map_err(|_| ())?;
                self.stage = TuiMessageInputStage::Ttl;
                Ok(false)
            }
            TuiMessageInputStage::Ttl => {
                let ttl_seconds = self.ttl_seconds.parse::<u32>().map_err(|_| ())?;
                SdkMessageExpiry::new(0, ttl_seconds).map_err(|_| ())?;
                Ok(true)
            }
        }
    }

    fn rendered_value(&self) -> String {
        match self.stage {
            TuiMessageInputStage::Recipient => self.recipient.clone(),
            TuiMessageInputStage::Envelope => {
                format!("redacted ({} hexadecimal characters)", self.envelope.len())
            }
            TuiMessageInputStage::Ttl => self.ttl_seconds.clone(),
        }
    }
}

struct TuiDashboard {
    dashboard: Dashboard,
    screen: TuiScreen,
    inbox_selection: usize,
    runtime_running: bool,
    route_policy: TuiRoutePolicy,
    contact_input: Option<TuiContactInput>,
    message_input: Option<TuiMessageInput>,
    notice: Option<&'static str>,
    last_error: Option<&'static str>,
}

#[derive(Clone, Copy, Eq, PartialEq)]
enum TuiScreen {
    Overview,
    Inbox,
    Attachments,
    Status,
    RoutePolicy,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum TuiRouteSelection {
    Direct,
    TorMaildrop,
    LocalMesh(SdkLocalMeshTransportKind),
}

impl TuiRouteSelection {
    const fn label(self) -> &'static str {
        match self {
            Self::Direct => "direct",
            Self::TorMaildrop => "tor_maildrop",
            Self::LocalMesh(SdkLocalMeshTransportKind::Lan) => "local_mesh_lan",
            Self::LocalMesh(SdkLocalMeshTransportKind::WifiHotspot) => "local_mesh_wifi_hotspot",
            Self::LocalMesh(SdkLocalMeshTransportKind::WifiDirect) => "local_mesh_wifi_direct",
            Self::LocalMesh(SdkLocalMeshTransportKind::Bluetooth) => "local_mesh_bluetooth",
        }
    }
}

struct TuiRoutePolicy {
    selection: Option<TuiRouteSelection>,
    profile: Option<SdkDeliveryProfile>,
    direct_acknowledgement_pending: bool,
}

impl TuiRoutePolicy {
    const fn new() -> Self {
        Self {
            selection: None,
            profile: None,
            direct_acknowledgement_pending: false,
        }
    }

    fn select(&mut self, selection: TuiRouteSelection) -> Result<(), ()> {
        let profile = match selection {
            TuiRouteSelection::Direct => SdkDeliveryProfilePolicy::new(true, false, None)
                .map_err(|_| ())?
                .select_direct(SdkDirectIpDisclosureAcknowledgement::acknowledge())
                .map_err(|_| ())?,
            TuiRouteSelection::TorMaildrop => SdkDeliveryProfilePolicy::new(false, true, None)
                .map_err(|_| ())?
                .select_tor_maildrop()
                .map_err(|_| ())?,
            TuiRouteSelection::LocalMesh(transport) => {
                let local_mesh = SdkLocalMeshPolicy::new(&[transport]).map_err(|_| ())?;
                SdkDeliveryProfilePolicy::new(false, false, Some(local_mesh))
                    .map_err(|_| ())?
                    .select_local_mesh(transport)
                    .map_err(|_| ())?
            }
        };
        self.selection = Some(selection);
        self.profile = Some(profile);
        self.direct_acknowledgement_pending = false;
        Ok(())
    }
}

impl TuiDashboard {
    const fn new(dashboard: Dashboard, runtime_running: bool) -> Self {
        Self {
            dashboard,
            screen: TuiScreen::Overview,
            inbox_selection: 0,
            runtime_running,
            route_policy: TuiRoutePolicy::new(),
            contact_input: None,
            message_input: None,
            notice: None,
            last_error: None,
        }
    }

    fn select_next_inbox_message(&mut self) {
        self.inbox_selection = self
            .inbox_selection
            .saturating_add(1)
            .min(self.dashboard.inbox_messages.len().saturating_sub(1));
    }

    fn select_previous_inbox_message(&mut self) {
        self.inbox_selection = self.inbox_selection.saturating_sub(1);
    }

    fn selected_inbox_message(&self) -> Option<InboxMessage> {
        self.dashboard
            .inbox_messages
            .get(self.inbox_selection)
            .copied()
    }
}

fn run_dashboard(
    dashboard: Dashboard,
    state_directory: &Path,
    runtime: &mut TuiRuntime,
) -> Result<(), Box<dyn Error>> {
    let _restore = TerminalRestoreGuard;
    terminal::enable_raw_mode()?;
    let mut output = io::stdout();
    execute!(output, EnterAlternateScreen, Hide)?;
    let mut dashboard = TuiDashboard::new(dashboard, runtime.is_running());
    dashboard_event_loop(&mut output, &mut dashboard, state_directory, runtime)
}

fn dashboard_event_loop(
    output: &mut impl Write,
    dashboard: &mut TuiDashboard,
    state_directory: &Path,
    runtime: &mut TuiRuntime,
) -> Result<(), Box<dyn Error>> {
    loop {
        render_tui_dashboard(output, dashboard)?;
        if let Event::Key(key) = event::read()?
            && key.kind == KeyEventKind::Press
        {
            if dashboard.contact_input.is_some() {
                handle_tui_contact_input(dashboard, state_directory, runtime, key.code)?;
            } else if dashboard.message_input.is_some() {
                handle_tui_message_input(dashboard, state_directory, runtime, key.code)?;
            } else if dashboard.screen == TuiScreen::Inbox {
                match key.code {
                    KeyCode::Char('q') => return Ok(()),
                    KeyCode::Esc | KeyCode::Char('b') => dashboard.screen = TuiScreen::Overview,
                    KeyCode::Up | KeyCode::Char('k') => dashboard.select_previous_inbox_message(),
                    KeyCode::Down | KeyCode::Char('j') => dashboard.select_next_inbox_message(),
                    _ => {}
                }
            } else if dashboard.screen == TuiScreen::Attachments {
                match key.code {
                    KeyCode::Char('q') => return Ok(()),
                    KeyCode::Esc | KeyCode::Char('a') => dashboard.screen = TuiScreen::Overview,
                    _ => {}
                }
            } else if dashboard.screen == TuiScreen::Status {
                match key.code {
                    KeyCode::Char('q') => return Ok(()),
                    KeyCode::Esc | KeyCode::Char('o') => dashboard.screen = TuiScreen::Overview,
                    _ => {}
                }
            } else if dashboard.screen == TuiScreen::RoutePolicy {
                handle_tui_route_policy_key(dashboard, key.code);
            } else {
                match key.code {
                    KeyCode::Char('q') | KeyCode::Esc => return Ok(()),
                    KeyCode::Char('b') => {
                        dashboard.screen = TuiScreen::Inbox;
                        dashboard.inbox_selection = 0;
                        dashboard.notice = None;
                    }
                    KeyCode::Char('a') => {
                        dashboard.screen = TuiScreen::Attachments;
                        dashboard.notice = None;
                    }
                    KeyCode::Char('o') => {
                        dashboard.screen = TuiScreen::Status;
                        dashboard.notice = None;
                    }
                    KeyCode::Char('p') => {
                        dashboard.screen = TuiScreen::RoutePolicy;
                        dashboard.notice = None;
                    }
                    KeyCode::Char('m') => {
                        dashboard.message_input = Some(TuiMessageInput::new());
                        dashboard.notice = None;
                    }
                    KeyCode::Char('i') => {
                        dashboard.contact_input =
                            Some(TuiContactInput::new(TuiContactInputKind::Invitation));
                        dashboard.notice = None;
                    }
                    KeyCode::Char('r') => {
                        dashboard.contact_input =
                            Some(TuiContactInput::new(TuiContactInputKind::QrVerification));
                        dashboard.notice = None;
                    }
                    KeyCode::Char('s') => {
                        dashboard.contact_input = Some(TuiContactInput::new(
                            TuiContactInputKind::SafetyNumberVerification,
                        ));
                        dashboard.notice = None;
                    }
                    _ => {}
                }
            }
        }
    }
}

fn handle_tui_route_policy_key(dashboard: &mut TuiDashboard, key: KeyCode) {
    if dashboard.route_policy.direct_acknowledgement_pending {
        match key {
            KeyCode::Char('y') => {
                if dashboard
                    .route_policy
                    .select(TuiRouteSelection::Direct)
                    .is_ok()
                {
                    dashboard.notice = Some("route selected");
                    dashboard.last_error = None;
                } else {
                    dashboard.notice = None;
                    dashboard.last_error = Some("route selection failed");
                }
            }
            KeyCode::Char('n') | KeyCode::Esc => {
                dashboard.route_policy.direct_acknowledgement_pending = false;
                dashboard.notice = Some("direct route not selected");
                dashboard.last_error = None;
            }
            _ => {}
        }
        return;
    }
    match key {
        KeyCode::Char('q' | 'p') | KeyCode::Esc => {
            dashboard.screen = TuiScreen::Overview;
        }
        KeyCode::Char('d') => dashboard.route_policy.direct_acknowledgement_pending = true,
        KeyCode::Char('t') => select_tui_route(dashboard, TuiRouteSelection::TorMaildrop),
        KeyCode::Char('l') => select_tui_route(
            dashboard,
            TuiRouteSelection::LocalMesh(SdkLocalMeshTransportKind::Lan),
        ),
        KeyCode::Char('h') => select_tui_route(
            dashboard,
            TuiRouteSelection::LocalMesh(SdkLocalMeshTransportKind::WifiHotspot),
        ),
        KeyCode::Char('w') => select_tui_route(
            dashboard,
            TuiRouteSelection::LocalMesh(SdkLocalMeshTransportKind::WifiDirect),
        ),
        KeyCode::Char('b') => select_tui_route(
            dashboard,
            TuiRouteSelection::LocalMesh(SdkLocalMeshTransportKind::Bluetooth),
        ),
        _ => {}
    }
}

fn select_tui_route(dashboard: &mut TuiDashboard, selection: TuiRouteSelection) {
    if dashboard.route_policy.select(selection).is_ok() {
        dashboard.notice = Some("route selected");
        dashboard.last_error = None;
    } else {
        dashboard.notice = None;
        dashboard.last_error = Some("route selection failed");
    }
}

fn handle_tui_contact_input(
    dashboard: &mut TuiDashboard,
    state_directory: &Path,
    runtime: &mut TuiRuntime,
    key: KeyCode,
) -> Result<(), Box<dyn Error>> {
    let Some(input) = dashboard.contact_input.as_mut() else {
        return Ok(());
    };
    match key {
        KeyCode::Esc => {
            dashboard.contact_input = None;
            dashboard.notice = Some("contact action cancelled");
            dashboard.last_error = None;
        }
        KeyCode::Backspace => {
            input.value.pop();
        }
        KeyCode::Char(character) => input.push(character),
        KeyCode::Enter => {
            let input = dashboard
                .contact_input
                .take()
                .ok_or("contact input is unavailable")?;
            if runtime
                .submit_contact_input(state_directory, &input)
                .is_ok()
            {
                if let Ok(updated) = runtime.refresh_dashboard(state_directory) {
                    let identity = dashboard.dashboard.identity.clone();
                    dashboard.dashboard = updated;
                    dashboard.dashboard.identity = identity;
                    dashboard.notice = Some("contact updated");
                    dashboard.last_error = None;
                } else {
                    dashboard.notice = None;
                    dashboard.last_error = Some("contact state refresh failed");
                }
            } else {
                dashboard.notice = None;
                dashboard.last_error = Some("contact update failed");
            }
        }
        _ => {}
    }
    Ok(())
}

fn handle_tui_message_input(
    dashboard: &mut TuiDashboard,
    state_directory: &Path,
    runtime: &mut TuiRuntime,
    key: KeyCode,
) -> Result<(), Box<dyn Error>> {
    let Some(input) = dashboard.message_input.as_mut() else {
        return Ok(());
    };
    match key {
        KeyCode::Esc => {
            dashboard.message_input = None;
            dashboard.notice = Some("message composition cancelled");
            dashboard.last_error = None;
        }
        KeyCode::Backspace => input.pop(),
        KeyCode::Char(character) => input.push(character),
        KeyCode::Enter => match input.advance() {
            Ok(false) => {
                dashboard.last_error = None;
            }
            Ok(true) => {
                let input = dashboard
                    .message_input
                    .take()
                    .ok_or("message input is unavailable")?;
                if runtime.submit_message(state_directory, &input).is_ok() {
                    if let Ok(updated) = runtime.refresh_dashboard(state_directory) {
                        let identity = dashboard.dashboard.identity.clone();
                        dashboard.dashboard = updated;
                        dashboard.dashboard.identity = identity;
                        dashboard.notice = Some("encrypted message queued");
                        dashboard.last_error = None;
                    } else {
                        dashboard.notice = None;
                        dashboard.last_error = Some("message state refresh failed");
                    }
                } else {
                    dashboard.notice = None;
                    dashboard.last_error = Some("encrypted message was not queued");
                }
            }
            Err(()) => {
                dashboard.last_error = Some("message input is invalid");
            }
        },
        _ => {}
    }
    Ok(())
}

fn submit_tui_message(
    client: &mut SdkClient,
    state_directory: &Path,
    input: &TuiMessageInput,
) -> Result<(), Box<dyn Error>> {
    let created_at = SystemTime::now().duration_since(UNIX_EPOCH)?.as_secs();
    let profile = ClientProfile::open_or_create(&ClientStateDirectory::new(state_directory)?)?;
    #[cfg(target_os = "linux")]
    {
        let mut identity = SdkIdentityManager::for_profile(LinuxKeystore::new()?, profile);
        return submit_tui_message_with_identity(client, &mut identity, input, created_at);
    }
    #[cfg(target_os = "macos")]
    {
        let mut identity = SdkIdentityManager::for_profile(MacOsKeystore::new(), profile);
        return submit_tui_message_with_identity(client, &mut identity, input, created_at);
    }
    #[cfg(target_os = "windows")]
    {
        let mut identity = SdkIdentityManager::for_profile(WindowsKeystore::new()?, profile);
        return submit_tui_message_with_identity(client, &mut identity, input, created_at);
    }
    #[allow(unreachable_code)]
    Err("unsupported operating system keystore".into())
}

fn submit_tui_message_with_identity<K: OsKeystore>(
    client: &mut SdkClient,
    identity: &mut SdkIdentityManager<K>,
    input: &TuiMessageInput,
    created_at: u64,
) -> Result<(), Box<dyn Error>> {
    let recipient = decode_identity_public_key(&input.recipient)?;
    let encoded =
        decode_bounded_canonical_hex(&input.envelope, MAX_ENCRYPTED_MESSAGE_SUBMISSION_BYTES)?;
    let envelope = SdkMessageEnvelope::from_encoded(&encoded)?;
    let ttl_seconds = input.ttl_seconds.parse::<u32>()?;
    let expiry = SdkMessageExpiry::new(created_at, ttl_seconds)?;
    let verified = {
        let contacts = client.contact_manager(identity)?;
        matches!(
            contacts.contact(&recipient),
            Some(contact) if contact.status() == SdkContactStatus::Verified
        )
    };
    if !verified {
        return Err("recipient contact is not verified".into());
    }
    let _ = client.send_message(
        identity,
        SdkMessageSendRequest::new(recipient, envelope, expiry),
    )?;
    Ok(())
}

fn submit_tui_contact_input(
    state_directory: &Path,
    input: &TuiContactInput,
) -> Result<(), Box<dyn Error>> {
    let local_identity = load_client_system_identity(state_directory)?.public_key();
    #[cfg(target_os = "linux")]
    {
        let mut keystore = LinuxKeystore::new()?;
        return submit_tui_contact_input_with_keystore(
            state_directory,
            &local_identity,
            input,
            &mut keystore,
        );
    }
    #[cfg(target_os = "macos")]
    {
        let mut keystore = MacOsKeystore::new();
        return submit_tui_contact_input_with_keystore(
            state_directory,
            &local_identity,
            input,
            &mut keystore,
        );
    }
    #[cfg(target_os = "windows")]
    {
        let mut keystore = WindowsKeystore::new()?;
        return submit_tui_contact_input_with_keystore(
            state_directory,
            &local_identity,
            input,
            &mut keystore,
        );
    }
    #[allow(unreachable_code)]
    Err("unsupported operating system keystore".into())
}

fn submit_tui_contact_input_with_keystore<K: OsKeystore>(
    state_directory: &Path,
    local_identity: &IdentityPublicKey,
    input: &TuiContactInput,
    keystore: &mut K,
) -> Result<(), Box<dyn Error>> {
    match input.kind {
        TuiContactInputKind::Invitation => {
            let _ =
                import_contact_invitation(state_directory, local_identity, &input.value, keystore)?;
        }
        TuiContactInputKind::QrVerification => {
            let _ = verify_contact_qr(state_directory, local_identity, &input.value, keystore)?;
        }
        TuiContactInputKind::SafetyNumberVerification => {
            let (contact_public_key, safety_number) = tui_safety_number_parts(&input.value)?;
            let remote_identity = decode_identity_public_key(contact_public_key)?;
            let _ = verify_contact_safety_number(
                state_directory,
                local_identity,
                &remote_identity,
                safety_number,
                keystore,
            )?;
        }
    }
    Ok(())
}

fn tui_safety_number_parts(input: &str) -> Result<(&str, &str), &'static str> {
    let (contact_public_key, safety_number) = input
        .split_once(' ')
        .ok_or("safety verification requires a contact public key and safety number")?;
    if contact_public_key.is_empty()
        || safety_number.is_empty()
        || safety_number.contains(' ')
        || contact_public_key.contains(' ')
    {
        return Err("safety verification input is invalid");
    }
    Ok((contact_public_key, safety_number))
}

fn render_tui_dashboard(output: &mut impl Write, dashboard: &TuiDashboard) -> std::io::Result<()> {
    if dashboard.screen == TuiScreen::Inbox {
        return render_tui_inbox(output, dashboard);
    }
    if dashboard.screen == TuiScreen::Attachments {
        return render_tui_attachments(output, dashboard);
    }
    if dashboard.screen == TuiScreen::Status {
        return render_tui_status(output, dashboard);
    }
    if dashboard.screen == TuiScreen::RoutePolicy {
        return render_tui_route_policy(output, dashboard);
    }
    queue!(output, MoveTo(0, 0), Clear(ClearType::All))?;
    let mut content = String::from("Arachne | secure courier\n───────────────────────\n\n");
    content.push_str(&dashboard.dashboard.snapshot());
    if let Some(input) = &dashboard.contact_input {
        let _ = writeln!(content, "{}: {}", input.kind.prompt(), input.value);
        content.push_str("Enter submits; Esc cancels.\n");
    } else if let Some(input) = &dashboard.message_input {
        let _ = writeln!(
            content,
            "{}: {}",
            input.stage.prompt(),
            input.rendered_value()
        );
        content.push_str(
            "The encrypted envelope is never rendered. Enter advances or queues; Esc cancels.\n",
        );
    } else {
        content.push('\n');
        content.push_str("───────────────────────\n");
        content.push_str("[a] attachments  [b] inbox  [m] message  [o] status  [p] route\n");
        content.push_str("[i] invite  [r] verify QR  [s] safety number  [q] exit\n");
    }
    if let Some(notice) = dashboard.notice {
        let _ = writeln!(content, "{notice}");
    }
    write_tui_text(output, &content)?;
    output.flush()
}

fn render_tui_route_policy(
    output: &mut impl Write,
    dashboard: &TuiDashboard,
) -> std::io::Result<()> {
    queue!(output, MoveTo(0, 0), Clear(ClearType::All))?;
    let mut content = String::from("Route policy:\n");
    let _ = writeln!(
        content,
        "selected={}",
        dashboard
            .route_policy
            .selection
            .map_or("none", TuiRouteSelection::label)
    );
    content.push_str("d: direct (requires IP-disclosure acknowledgement); t: Tor maildrop.\n");
    content.push_str("l: LAN; h: Wi-Fi hotspot; w: Wi-Fi Direct; b: Bluetooth.\n");
    content.push_str("Every route change is explicit; no automatic route replacement occurs.\n");
    if dashboard.route_policy.direct_acknowledgement_pending {
        content.push_str(
            "Direct delivery may disclose your IP address. Press y to acknowledge or n/Esc to cancel.\n",
        );
    } else {
        content.push_str("p or Esc returns; q returns to overview.\n");
    }
    if let Some(notice) = dashboard.notice {
        let _ = writeln!(content, "{notice}");
    }
    if let Some(error) = dashboard.last_error {
        let _ = writeln!(content, "error={error}");
    }
    write_tui_text(output, &content)?;
    output.flush()
}

fn render_tui_inbox(output: &mut impl Write, dashboard: &TuiDashboard) -> std::io::Result<()> {
    queue!(output, MoveTo(0, 0), Clear(ClearType::All))?;
    let mut content = String::from("Inbox:\n");
    let count = dashboard.dashboard.inbox_messages.len();
    if let Some(message) = dashboard.selected_inbox_message() {
        let _ = writeln!(
            content,
            "message={} of {} received_at={} encrypted_header_bytes={} ciphertext_bytes={}",
            dashboard.inbox_selection + 1,
            count,
            message.received_at(),
            message.encrypted_header_bytes(),
            message.ciphertext_bytes()
        );
        content.push_str("content=unavailable\n");
        content.push_str("Use Up/Down or j/k to select; b or Esc returns; q exits.\n");
    } else {
        content.push_str("no retained message metadata\n");
        content.push_str("b or Esc returns; q exits.\n");
    }
    write_tui_text(output, &content)?;
    output.flush()
}

fn render_tui_attachments(
    output: &mut impl Write,
    dashboard: &TuiDashboard,
) -> std::io::Result<()> {
    queue!(output, MoveTo(0, 0), Clear(ClearType::All))?;
    let mut content = String::from("Attachment transfers:\n");
    for transfer in &dashboard.dashboard.attachments {
        let _ = writeln!(content, "  {transfer}");
    }
    content.push_str("a or Esc returns; q exits.\n");
    write_tui_text(output, &content)?;
    output.flush()
}

fn render_tui_status(output: &mut impl Write, dashboard: &TuiDashboard) -> std::io::Result<()> {
    queue!(output, MoveTo(0, 0), Clear(ClearType::All))?;
    let mut content = String::from("Operational status:\n");
    let _ = writeln!(
        content,
        "runtime={}",
        if dashboard.runtime_running {
            "running"
        } else {
            "unavailable"
        }
    );
    let _ = writeln!(
        content,
        "last_error={}",
        dashboard.last_error.unwrap_or("none")
    );
    content.push_str("o or Esc returns; q exits.\n");
    write_tui_text(output, &content)?;
    output.flush()
}

fn write_tui_text(output: &mut impl Write, text: &str) -> std::io::Result<()> {
    for line in text.split_inclusive('\n') {
        if let Some(line) = line.strip_suffix('\n') {
            output.write_all(line.as_bytes())?;
            output.write_all(b"\r\n")?;
        } else {
            output.write_all(line.as_bytes())?;
        }
    }
    Ok(())
}

#[cfg(test)]
fn render_dashboard(output: &mut impl Write, dashboard: &Dashboard) -> std::io::Result<()> {
    queue!(
        output,
        MoveTo(0, 0),
        Clear(ClearType::All),
        Print(dashboard.snapshot()),
        Print("Press q or Esc to exit.\n")
    )?;
    output.flush()
}

fn validate_state_directory(state_directory: &Path) -> Result<(), &'static str> {
    ClientStateDirectory::new(state_directory)
        .map(|_| ())
        .map_err(|_| "state directory must be an absolute non-root path without parent traversal")
}

fn initialize_client_logging(state_directory: &Path) -> Result<(), Box<dyn Error>> {
    let layout = ClientStateDirectory::new(state_directory)?;
    initialize_file_tracing(layout.root(), "client")?;
    Ok(())
}

fn run_log_command(command: LogCommand) -> Result<String, Box<dyn Error>> {
    match command {
        LogCommand::List { state_directory } => {
            let layout = ClientStateDirectory::new(state_directory)?;
            let directory = log_directory(layout.root())?;
            let files = list_log_files(layout.root())?;
            let bytes = files.iter().map(arachne_core::LogFile::bytes).sum::<u64>();
            let mut output = String::new();
            let _ = writeln!(
                output,
                "log_directory={}\nlog_files={}\nlog_bytes={bytes}",
                directory.display(),
                files.len()
            );
            for file in files {
                let _ = writeln!(output, "log_file={} bytes={}", file.name(), file.bytes());
            }
            Ok(output)
        }
        LogCommand::Prune {
            state_directory,
            older_than_days,
        } => {
            let layout = ClientStateDirectory::new(state_directory)?;
            let age = Duration::from_secs(u64::from(older_than_days).saturating_mul(86_400));
            let pruned = prune_log_files(layout.root(), age, SystemTime::now())?;
            Ok(format!("pruned_log_files={pruned}\n"))
        }
    }
}

fn diagnose(state_directory: &Path) -> Result<String, Box<dyn Error>> {
    let layout = ClientStateDirectory::new(state_directory)?;
    let mut output = format!("state_directory={}\n", layout.root().display());
    for (name, path) in [
        ("daemon_lock", layout.lock_path()),
        ("contacts", layout.contacts_path()),
        ("inbox", layout.inbox_path()),
        ("outbox", layout.outbox_path()),
        ("ratchets", layout.ratchets_path()),
        ("one_time_prekeys", layout.one_time_prekey_inventory_path()),
    ] {
        let state = match fs::metadata(path) {
            Ok(metadata) if metadata.is_file() => "present",
            Ok(_) => "invalid",
            Err(error) if error.kind() == io::ErrorKind::NotFound => "absent",
            Err(_) => "unavailable",
        };
        let _ = writeln!(output, "{name}={state}");
    }
    let files = list_log_files(layout.root())?;
    let _ = writeln!(output, "log_files={}", files.len());
    let _ = writeln!(
        output,
        "log_bytes={}",
        files.iter().map(arachne_core::LogFile::bytes).sum::<u64>()
    );
    Ok(output)
}

fn queue_system_attachment(
    state_directory: &Path,
    manifest_path: &Path,
    chunk_paths: &[PathBuf],
) -> Result<String, Box<dyn Error>> {
    validate_state_directory(state_directory)?;
    initialize_client_logging(state_directory)?;
    let _runtime =
        DaemonRuntime::start(arachne_protocol::ProtocolVersion::INITIAL, state_directory)?;
    queue_attachment_submission(state_directory, manifest_path, chunk_paths)
}

fn queue_attachment_submission(
    state_directory: &Path,
    manifest_path: &Path,
    chunk_paths: &[PathBuf],
) -> Result<String, Box<dyn Error>> {
    let manifest_bytes = read_bounded_file(manifest_path, MAX_ENCODED_ATTACHMENT_MANIFEST_BYTES)?;
    let manifest = EncryptedAttachmentManifest::decode(&manifest_bytes)?;
    if manifest.encode()? != manifest_bytes {
        return Err("encrypted attachment manifest is not canonically encoded".into());
    }
    let mut chunk_bytes = Vec::with_capacity(chunk_paths.len());
    let mut chunks = Vec::with_capacity(chunk_paths.len());
    for path in chunk_paths {
        let encoded = read_bounded_file(path, MAX_ENCODED_ATTACHMENT_CHUNK_BYTES)?;
        let chunk = EncryptedAttachmentChunk::decode(&encoded)?;
        if chunk.encode()? != encoded {
            return Err("encrypted attachment chunk is not canonically encoded".into());
        }
        chunk_bytes.push(encoded);
        chunks.push(chunk);
    }
    let journal = AttachmentUploadJournal::new(&chunks)?;
    if journal.identifier() != manifest.identifier() {
        return Err("attachment manifest and chunks use different identifiers".into());
    }
    let identifier = hexadecimal(manifest.identifier().as_bytes());
    let root = ClientStateDirectory::new(state_directory)?.attachment_uploads_path();
    fs::create_dir_all(&root)?;
    let destination = root.join(&identifier);
    if destination.exists() {
        return Err("attachment submission already exists".into());
    }
    let staging = root.join(format!("{identifier}.pending"));
    fs::create_dir(&staging)?;
    let persisted = (|| -> Result<(), Box<dyn Error>> {
        write_new_file(&staging.join("manifest.cbor"), &manifest_bytes)?;
        write_new_file(&staging.join("journal.cbor"), &journal.encode()?)?;
        for (index, encoded) in chunk_bytes.iter().enumerate() {
            write_new_file(&staging.join(format!("chunk-{index}.cbor")), encoded)?;
        }
        fs::rename(&staging, &destination)?;
        Ok(())
    })();
    if let Err(error) = persisted {
        let _ = fs::remove_dir_all(&staging);
        return Err(error);
    }
    tracing::info!(
        target: "arachne.client.attachment",
        event = "attachment_queued",
        chunk_count = chunks.len(),
        "client attachment event"
    );
    Ok(format!(
        "attachment_identifier={identifier}\nchunk_count={}\n",
        chunks.len()
    ))
}

fn read_bounded_file(path: &Path, maximum_bytes: usize) -> Result<Vec<u8>, Box<dyn Error>> {
    let metadata = fs::metadata(path)?;
    if !metadata.is_file() || metadata.len() > u64::try_from(maximum_bytes)? {
        return Err("attachment artifact has an invalid size or is not a file".into());
    }
    let content = fs::read(path)?;
    if content.len() > maximum_bytes {
        return Err("attachment artifact exceeds its configured limit".into());
    }
    Ok(content)
}

fn write_new_file(path: &Path, content: &[u8]) -> Result<(), Box<dyn Error>> {
    let mut file = fs::OpenOptions::new()
        .write(true)
        .create_new(true)
        .open(path)?;
    file.write_all(content)?;
    file.sync_all()?;
    Ok(())
}

const fn hexadecimal_nibble(byte: u8) -> Option<u8> {
    match byte {
        b'0'..=b'9' => Some(byte - b'0'),
        b'a'..=b'f' => Some(byte - b'a' + 10),
        _ => None,
    }
}

fn create_client_system_identity(
    state_directory: &Path,
) -> Result<IdentityPublicKey, Box<dyn Error>> {
    validate_state_directory(state_directory)?;
    let layout = ClientStateDirectory::new(state_directory)?;
    let profile = ClientProfile::open_or_create(&layout)?;
    #[cfg(target_os = "linux")]
    {
        let mut keystore = LinuxKeystore::new()?;
        return Ok(profile.create_identity(&mut keystore)?.public_key());
    }
    #[cfg(target_os = "macos")]
    {
        let mut keystore = MacOsKeystore::new();
        return Ok(profile.create_identity(&mut keystore)?.public_key());
    }
    #[cfg(target_os = "windows")]
    {
        let mut keystore = WindowsKeystore::new()?;
        return Ok(profile.create_identity(&mut keystore)?.public_key());
    }
    #[allow(unreachable_code)]
    Err("unsupported operating system keystore".into())
}

fn create_or_load_client_system_identity(
    state_directory: &Path,
) -> Result<(IdentityPublicKey, ClientIdentityInitialization), Box<dyn Error>> {
    validate_state_directory(state_directory)?;
    let layout = ClientStateDirectory::new(state_directory)?;
    let profile = ClientProfile::open_or_create(&layout)?;
    #[cfg(target_os = "linux")]
    {
        let mut keystore = LinuxKeystore::new()?;
        return initialize_tui_profile_identity(&profile, &mut keystore);
    }
    #[cfg(target_os = "macos")]
    {
        let mut keystore = MacOsKeystore::new();
        return initialize_tui_profile_identity(&profile, &mut keystore);
    }
    #[cfg(target_os = "windows")]
    {
        let mut keystore = WindowsKeystore::new()?;
        return initialize_tui_profile_identity(&profile, &mut keystore);
    }
    #[allow(unreachable_code)]
    Err("unsupported operating system keystore".into())
}

#[cfg(test)]
fn initialize_tui_identity<K: OsKeystore>(
    keystore: &mut K,
) -> Result<(IdentityPublicKey, ClientIdentityInitialization), Box<dyn Error>> {
    let (identity, initialization) = ClientIdentity::create_or_load(keystore)?;
    Ok((identity.public_key(), initialization))
}

fn initialize_tui_profile_identity<K: OsKeystore>(
    profile: &ClientProfile,
    keystore: &mut K,
) -> Result<(IdentityPublicKey, ClientIdentityInitialization), Box<dyn Error>> {
    let (identity, initialization) = profile.create_or_load_identity(keystore)?;
    Ok((identity.public_key(), initialization))
}

fn identity_initialization_label(initialization: ClientIdentityInitialization) -> &'static str {
    match initialization {
        ClientIdentityInitialization::Created => "created",
        ClientIdentityInitialization::Loaded => "loaded",
    }
}

fn load_client_system_identity(state_directory: &Path) -> Result<ClientIdentity, Box<dyn Error>> {
    validate_state_directory(state_directory)?;
    let layout = ClientStateDirectory::new(state_directory)?;
    let profile = ClientProfile::open_or_create(&layout)?;
    #[cfg(target_os = "linux")]
    {
        let keystore = LinuxKeystore::new()?;
        return Ok(profile.load_identity(&keystore)?);
    }
    #[cfg(target_os = "macos")]
    {
        let keystore = MacOsKeystore::new();
        return Ok(profile.load_identity(&keystore)?);
    }
    #[cfg(target_os = "windows")]
    {
        let keystore = WindowsKeystore::new()?;
        return Ok(profile.load_identity(&keystore)?);
    }
    #[allow(unreachable_code)]
    Err("unsupported operating system keystore".into())
}

fn load_system_signing_identity(
    entry: &KeystoreEntryName,
) -> Result<IdentityKeypair, Box<dyn Error>> {
    #[cfg(target_os = "linux")]
    {
        let keystore = LinuxKeystore::new()?;
        return load_identity(&keystore, entry);
    }
    #[cfg(target_os = "macos")]
    {
        let keystore = MacOsKeystore::new();
        return load_identity(&keystore, entry);
    }
    #[cfg(target_os = "windows")]
    {
        let keystore = WindowsKeystore::new()?;
        return load_identity(&keystore, entry);
    }
    #[allow(unreachable_code)]
    Err("unsupported operating system keystore".into())
}

#[cfg(test)]
fn create_identity<K: OsKeystore>(
    keystore: &mut K,
    entry: &KeystoreEntryName,
) -> Result<IdentityPublicKey, Box<dyn Error>> {
    if keystore.load(entry)?.is_some() {
        return Err("identity already exists".into());
    }
    let identity = IdentityKeypair::generate()?;
    let secret = KeystoreSecret::new(identity.serialize().to_vec())?;
    keystore.store(entry, &secret)?;
    Ok(identity.public_key())
}

fn load_identity<K: OsKeystore>(
    keystore: &K,
    entry: &KeystoreEntryName,
) -> Result<IdentityKeypair, Box<dyn Error>> {
    let secret = keystore.load(entry)?.ok_or("identity does not exist")?;
    Ok(IdentityKeypair::deserialize(secret.as_bytes())?)
}

fn is_canonical_revision(revision: &str) -> bool {
    matches!(revision.len(), 40 | 64)
        && revision
            .bytes()
            .all(|byte| byte.is_ascii_digit() || matches!(byte, b'a'..=b'f'))
}

fn verify_protocol_vectors(candidate: &str) -> Result<(), &'static str> {
    let expected = parse_protocol_vectors(PROTOCOL_V1_VECTORS)
        .expect("bundled protocol vectors must be valid");
    let candidate = parse_protocol_vectors(candidate)?;
    if candidate.len() != expected.len() {
        return Err("protocol vector names do not match");
    }
    for (name, expected_value) in expected {
        if candidate.get(name) != Some(&expected_value) {
            return Err("protocol vector bytes do not match");
        }
    }
    Ok(())
}

fn parse_protocol_vectors(input: &str) -> Result<BTreeMap<&str, &str>, &'static str> {
    let mut vectors = BTreeMap::new();
    for line in input.lines() {
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        let Some((name, value)) = line.split_once('=') else {
            return Err("protocol vector has an invalid line");
        };
        if name.is_empty() || !is_canonical_hex(value) {
            return Err("protocol vector has an invalid name or value");
        }
        if vectors.insert(name, value).is_some() {
            return Err("protocol vector name is duplicated");
        }
    }
    Ok(vectors)
}

fn is_canonical_hex(value: &str) -> bool {
    !value.is_empty()
        && value.len().is_multiple_of(2)
        && value
            .bytes()
            .all(|byte| byte.is_ascii_digit() || matches!(byte, b'a'..=b'f'))
}

#[cfg(test)]
mod tests {
    use crossterm::event::KeyCode;
    use std::{
        collections::BTreeMap,
        convert::Infallible,
        fs,
        path::{Path, PathBuf},
        sync::atomic::{AtomicU64, Ordering},
    };

    use clap::Parser;

    use super::{
        Arguments, AttachmentCommand, Command, ContactCommand, ContactInvitation,
        ContactInvitationCommand, DaemonCommand, EncryptedMessageEnvelope, IdentityCommand,
        IdentityKeypair, IdentityPublicKey, KeystoreEntryName, LocalMeshCommand,
        LocalMeshPeerCommand, LogCommand, MAX_ENCRYPTED_MESSAGE_SUBMISSION_BYTES,
        MAX_TUI_CONTACT_INPUT_BYTES, MessageCommand, MessageExpiry, OsKeystore,
        RecipientInboxDeduplication, RelayProfileCommand, ReleaseManifestCommand, SenderOutbox,
        TorMaildropProfileConfig, TuiCommand, TuiContactInput, TuiContactInputKind, TuiDashboard,
        TuiMessageInput, TuiMessageInputStage, TuiRouteSelection, TuiScreen,
        apply_contact_rotation, contact_invitation_record, create_identity,
        daemon_auth_token_entry, dashboard_from_stores, decode_canonical_hex, decode_envelope,
        diagnose, handle_tui_route_policy_key, hexadecimal, identity_record,
        import_contact_invitation, initialize_tui_identity, inspect_contact_invitation,
        inspect_relay_profile, load_attachment_transfers, load_identity,
        queue_attachment_submission, queue_message, relay_profile_record, release_metadata,
        render_dashboard, render_tui_attachments, render_tui_dashboard, render_tui_route_policy,
        render_tui_status, revoke_contact, run_log_command, sign_release_manifest,
        start_embedded_tui_client, submit_tui_contact_input_with_keystore,
        submit_tui_message_with_identity, tui_safety_number_parts, validate_state_directory,
        verify_contact_qr, verify_contact_safety_number, verify_release_manifest,
    };
    use arachne_core::KeystoreSecret;
    use arachne_daemon::{
        ATTACHMENT_UPLOAD_DIRECTORY, CONTACTS_DATABASE_FILE, ClientIdentityInitialization,
        ClientStateDirectory, ContactStatus, ContactStore, INBOX_DATABASE_FILE,
        OUTBOX_DATABASE_FILE,
    };
    use arachne_protocol::{
        ATTACHMENT_CHUNK_BYTES, AttachmentIdentifier, AttachmentKey, AttachmentManifest,
        DeliveryAcknowledgement, EncryptedAttachmentChunk, IdentityRotation, QrVerificationPayload,
        SafetyNumberFingerprint,
    };
    use arachne_sdk::{SdkDeliveryProfileKind, SdkIdentityManager, SdkLocalMeshTransportKind};

    #[cfg(unix)]
    use super::DaemonTuiClient;
    #[cfg(unix)]
    use arachne_daemon::{DaemonLocalAuth, DaemonRuntime, DaemonServer};
    #[cfg(unix)]
    use arachne_daemon_api::v1::ShutdownDaemonRequest;
    #[cfg(unix)]
    use arachne_protocol::ProtocolVersion;

    const REVISION: &str = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
    static NEXT_TEST_STATE_DIRECTORY: AtomicU64 = AtomicU64::new(0);

    #[test]
    fn tui_output_uses_carriage_return_newlines_in_raw_mode() {
        let tui = TuiDashboard::new(dashboard_from_stores(None, None, None), true);
        let mut rendered = Vec::new();
        render_tui_dashboard(&mut rendered, &tui).unwrap();
        let rendered = String::from_utf8(rendered).unwrap();
        assert!(rendered.contains("Identity:\r\n"));
        assert!(!rendered.contains("Identity:\n"));
    }

    #[derive(Default)]
    struct InMemoryKeystore {
        secrets: BTreeMap<String, Vec<u8>>,
    }

    impl OsKeystore for InMemoryKeystore {
        type Error = Infallible;

        fn load(&self, entry: &KeystoreEntryName) -> Result<Option<KeystoreSecret>, Self::Error> {
            Ok(self
                .secrets
                .get(entry.as_str())
                .map(|secret| KeystoreSecret::new(secret.clone()).unwrap()))
        }

        fn store(
            &mut self,
            entry: &KeystoreEntryName,
            secret: &KeystoreSecret,
        ) -> Result<(), Self::Error> {
            self.secrets
                .insert(entry.as_str().to_owned(), secret.as_bytes().to_vec());
            Ok(())
        }

        fn delete(&mut self, entry: &KeystoreEntryName) -> Result<(), Self::Error> {
            self.secrets.remove(entry.as_str());
            Ok(())
        }
    }

    #[test]
    fn metadata_is_canonical_and_deterministic() {
        let first = release_metadata(REVISION, 1_700_000_000, b"lockfile").expect("valid metadata");
        let second =
            release_metadata(REVISION, 1_700_000_000, b"lockfile").expect("valid metadata");
        assert_eq!(first, second);
        assert_eq!(
            first,
            "{\"format_version\":1,\"package_version\":\"0.1.0\",\"protocol_version\":1,\"rust_toolchain\":\"1.93.0\",\"source_revision\":\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\",\"source_date_epoch\":1700000000,\"cargo_lock_sha256\":\"d6f5483103ee386e1f3453bff6da949b7d95fe942218d3774a449e38bbd9317f\"}\n"
        );
    }

    #[test]
    fn metadata_rejects_noncanonical_source_revisions() {
        assert!(release_metadata("ABC", 0, b"lockfile").is_err());
    }

    #[test]
    fn verifies_reordered_protocol_vectors() {
        let candidate = concat!(
            "wire_envelope=83010243010203\n",
            "# peer implementation output\n",
            "encrypted_message=8241a142b2c3\n",
            "version_offer=83010103\n",
            "version_accept=820203\n",
            "version_reject=83030101\n",
            "message_payload_text=8201426869\n",
            "extension_frame=82182a420102\n",
            "delivery_profile_direct=820101\n",
            "direct_profile=84010444c000020119115c\n",
            "tor_maildrop_profile=83015820111111111111111111111111111111111111111111111111111111111111111119115c\n",
            "local_mesh_profile_bluetooth=820104\n",
            "mailbox_capability=8301502222222222222222222222222222222258203333333333333333333333333333333333333333333333333333333333333333\n",
            "recipient_capability_direct=8401014b84010444c000020119115c40\n",
            "encrypted_header_direct=8201508401014b84010444c000020119115c40\n",
            "identity_identifier=5ac9a6c5424ce1b184a00426c0322cb44f0bb8084c0ac32c765aa23c37fa6db0\n",
            "qr_verification=830158203d4017c3e843895a92b70aa74d1b7ebc9c982ccf2ec4968cc0cd55f12af4660c5820d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a\n",
        );
        assert!(super::verify_protocol_vectors(candidate).is_ok());
    }

    #[test]
    fn rejects_invalid_protocol_vectors() {
        assert!(super::verify_protocol_vectors("version_offer=83010103\n").is_err());
        assert!(super::verify_protocol_vectors("version_offer=8301010G\n").is_err());
        assert!(
            super::verify_protocol_vectors("version_offer=83010103\nversion_offer=83010103\n")
                .is_err()
        );
    }

    #[test]
    fn identity_commands_store_only_redacted_key_material_and_public_records() {
        let entry = KeystoreEntryName::new("identity_primary".to_owned()).unwrap();
        let mut keystore = InMemoryKeystore::default();
        let created = create_identity(&mut keystore, &entry).unwrap();
        let loaded = load_identity(&keystore, &entry).unwrap();
        let record = identity_record(&created);
        assert_eq!(created, loaded.public_key());
        assert!(record.starts_with("public_key="));
        assert!(record.contains("\nidentity_identifier="));
        assert!(!record.contains("signing_key"));
        assert!(create_identity(&mut keystore, &entry).is_err());
    }

    #[test]
    fn identity_record_is_canonical_for_a_valid_public_key() {
        let public_key = IdentityPublicKey::from_bytes([
            0xd7, 0x5a, 0x98, 0x01, 0x82, 0xb1, 0x0a, 0xb7, 0xd5, 0x4b, 0xfe, 0xd3, 0xc9, 0x64,
            0x07, 0x3a, 0x0e, 0xe1, 0x72, 0xf3, 0xda, 0xa6, 0x23, 0x25, 0xaf, 0x02, 0x1a, 0x68,
            0xf7, 0x07, 0x51, 0x1a,
        ])
        .unwrap();
        assert_eq!(
            identity_record(&public_key),
            "public_key=d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a\nidentity_identifier=5ac9a6c5424ce1b184a00426c0322cb44f0bb8084c0ac32c765aa23c37fa6db0\n"
        );
    }

    #[test]
    fn parses_identity_create_and_show_commands() {
        let create = Arguments::try_parse_from([
            "arachne",
            "identity",
            "create",
            "--state-directory",
            "/tmp/alice",
        ])
        .unwrap();
        assert!(matches!(
            create.command,
            Command::Identity {
                command: IdentityCommand::Create { state_directory }
            } if state_directory == Path::new("/tmp/alice")
        ));
        let show = Arguments::try_parse_from([
            "arachne",
            "identity",
            "show",
            "--state-directory",
            "/tmp/alice",
        ])
        .unwrap();
        assert!(matches!(
            show.command,
            Command::Identity {
                command: IdentityCommand::Show { state_directory }
            } if state_directory == Path::new("/tmp/alice")
        ));
    }

    #[test]
    fn contact_invitation_commands_emit_and_inspect_canonical_invitations() {
        let identity = IdentityKeypair::generate().unwrap();
        let encoded = contact_invitation_record(&identity).unwrap();
        let invitation = encoded.strip_prefix("invitation=").unwrap().trim_end();
        let inspected = inspect_contact_invitation(invitation).unwrap();
        let mut binary = vec![0; invitation.len() / 2];
        decode_canonical_hex(invitation, &mut binary).unwrap();
        assert_eq!(
            ContactInvitation::decode(&binary).unwrap().inviter(),
            &identity.public_key()
        );
        assert!(inspected.starts_with("inviter_public_key="));
        assert!(inspected.contains("\ninviter_identifier="));
        assert!(inspect_contact_invitation(&invitation.to_uppercase()).is_err());
        let mut tampered = invitation.to_owned();
        tampered.replace_range(0..2, "00");
        assert!(inspect_contact_invitation(&tampered).is_err());
    }

    #[test]
    fn contact_invitation_import_persists_only_a_pending_public_contact() {
        let state_directory = std::env::temp_dir().join(format!(
            "arachne-cli-contact-import-{}-{}",
            std::process::id(),
            NEXT_TEST_STATE_DIRECTORY.fetch_add(1, Ordering::Relaxed)
        ));
        fs::create_dir(&state_directory).unwrap();
        let local_identity = IdentityKeypair::generate().unwrap();
        let remote_identity = IdentityKeypair::generate().unwrap();
        let encoded = hexadecimal(
            &ContactInvitation::create(&remote_identity)
                .unwrap()
                .encode()
                .unwrap(),
        );
        let mut keystore = InMemoryKeystore::default();
        let output = import_contact_invitation(
            &state_directory,
            &local_identity.public_key(),
            &encoded,
            &mut keystore,
        )
        .unwrap();
        assert_eq!(
            output,
            format!(
                "contact_public_key={}\nstatus=pending\n",
                hexadecimal(remote_identity.public_key().as_bytes())
            )
        );
        assert_eq!(
            import_contact_invitation(
                &state_directory,
                &local_identity.public_key(),
                &encoded,
                &mut keystore,
            )
            .unwrap(),
            output
        );
        assert!(
            import_contact_invitation(
                &state_directory,
                &local_identity.public_key(),
                &encoded.to_uppercase(),
                &mut keystore,
            )
            .is_err()
        );
        fs::remove_dir_all(state_directory).unwrap();
    }

    #[test]
    fn qr_contact_verification_promotes_a_pending_contact_once() {
        let state_directory = std::env::temp_dir().join(format!(
            "arachne-cli-qr-contact-verification-{}-{}",
            std::process::id(),
            NEXT_TEST_STATE_DIRECTORY.fetch_add(1, Ordering::Relaxed)
        ));
        fs::create_dir(&state_directory).unwrap();
        let local_identity = IdentityKeypair::generate().unwrap();
        let local_public_key = local_identity.public_key();
        let remote_identity = IdentityKeypair::generate().unwrap();
        let invitation = hexadecimal(
            &ContactInvitation::create(&remote_identity)
                .unwrap()
                .encode()
                .unwrap(),
        );
        let payload = hexadecimal(
            &QrVerificationPayload::new(local_public_key, remote_identity.public_key())
                .unwrap()
                .encode()
                .unwrap(),
        );
        let mut keystore = InMemoryKeystore::default();
        import_contact_invitation(
            &state_directory,
            &local_public_key,
            &invitation,
            &mut keystore,
        )
        .unwrap();
        let output =
            verify_contact_qr(&state_directory, &local_public_key, &payload, &mut keystore)
                .unwrap();
        assert_eq!(
            output,
            format!(
                "contact_public_key={}\nstatus=verified\n",
                hexadecimal(remote_identity.public_key().as_bytes())
            )
        );
        assert!(
            verify_contact_qr(&state_directory, &local_public_key, &payload, &mut keystore,)
                .is_err()
        );
        fs::remove_dir_all(state_directory).unwrap();
    }

    #[test]
    fn safety_number_verification_promotes_a_pending_contact_once() {
        let state_directory = std::env::temp_dir().join(format!(
            "arachne-cli-safety-number-verification-{}-{}",
            std::process::id(),
            NEXT_TEST_STATE_DIRECTORY.fetch_add(1, Ordering::Relaxed)
        ));
        fs::create_dir(&state_directory).unwrap();
        let local_identity = IdentityKeypair::generate().unwrap();
        let local_public_key = local_identity.public_key();
        let remote_identity = IdentityKeypair::generate().unwrap();
        let invitation = hexadecimal(
            &ContactInvitation::create(&remote_identity)
                .unwrap()
                .encode()
                .unwrap(),
        );
        let safety_number = hexadecimal(
            SafetyNumberFingerprint::derive(&local_public_key, &remote_identity.public_key())
                .unwrap()
                .as_bytes(),
        );
        let mut keystore = InMemoryKeystore::default();
        import_contact_invitation(
            &state_directory,
            &local_public_key,
            &invitation,
            &mut keystore,
        )
        .unwrap();
        let output = verify_contact_safety_number(
            &state_directory,
            &local_public_key,
            &remote_identity.public_key(),
            &safety_number,
            &mut keystore,
        )
        .unwrap();
        assert_eq!(
            output,
            format!(
                "contact_public_key={}\nstatus=verified\n",
                hexadecimal(remote_identity.public_key().as_bytes())
            )
        );
        assert!(
            verify_contact_safety_number(
                &state_directory,
                &local_public_key,
                &remote_identity.public_key(),
                &safety_number,
                &mut keystore,
            )
            .is_err()
        );
        fs::remove_dir_all(state_directory).unwrap();
    }

    #[test]
    fn contact_rotation_and_revocation_update_only_public_contact_lifecycle_state() {
        let state_directory = std::env::temp_dir().join(format!(
            "arachne-cli-contact-lifecycle-{}-{}",
            std::process::id(),
            NEXT_TEST_STATE_DIRECTORY.fetch_add(1, Ordering::Relaxed)
        ));
        fs::create_dir(&state_directory).unwrap();
        let local_identity = IdentityKeypair::generate().unwrap();
        let local_public_key = local_identity.public_key();
        let remote_identity = IdentityKeypair::generate().unwrap();
        let replacement_identity = IdentityKeypair::generate().unwrap();
        let invitation = hexadecimal(
            &ContactInvitation::create(&remote_identity)
                .unwrap()
                .encode()
                .unwrap(),
        );
        let verification = hexadecimal(
            &QrVerificationPayload::new(local_public_key, remote_identity.public_key())
                .unwrap()
                .encode()
                .unwrap(),
        );
        let rotation = hexadecimal(
            &IdentityRotation::create(&remote_identity, replacement_identity.public_key())
                .unwrap()
                .encode()
                .unwrap(),
        );
        let mut keystore = InMemoryKeystore::default();
        import_contact_invitation(
            &state_directory,
            &local_public_key,
            &invitation,
            &mut keystore,
        )
        .unwrap();
        verify_contact_qr(
            &state_directory,
            &local_public_key,
            &verification,
            &mut keystore,
        )
        .unwrap();
        assert_eq!(
            apply_contact_rotation(
                &state_directory,
                &local_public_key,
                &rotation,
                &mut keystore
            )
            .unwrap(),
            format!(
                "contact_public_key={}\nstatus=pending\n",
                hexadecimal(replacement_identity.public_key().as_bytes())
            )
        );
        assert_eq!(
            revoke_contact(
                &state_directory,
                &local_public_key,
                &replacement_identity.public_key(),
                &mut keystore,
            )
            .unwrap(),
            format!(
                "contact_public_key={}\nstatus=revoked\n",
                hexadecimal(replacement_identity.public_key().as_bytes())
            )
        );
        assert!(
            revoke_contact(
                &state_directory,
                &local_public_key,
                &replacement_identity.public_key(),
                &mut keystore,
            )
            .is_err()
        );
        fs::remove_dir_all(state_directory).unwrap();
    }

    #[test]
    fn parses_contact_invitation_commands() {
        let create = Arguments::try_parse_from([
            "arachne",
            "contact",
            "invitation",
            "create",
            "--state-directory",
            "/tmp/alice",
        ])
        .unwrap();
        assert!(matches!(
            create.command,
            Command::Contact {
                command: ContactCommand::Invitation {
                    command: ContactInvitationCommand::Create { state_directory }
                }
            } if state_directory == Path::new("/tmp/alice")
        ));
        let inspect = Arguments::try_parse_from([
            "arachne",
            "contact",
            "invitation",
            "inspect",
            "--invitation",
            "00",
        ])
        .unwrap();
        assert!(matches!(
            inspect.command,
            Command::Contact {
                command: ContactCommand::Invitation {
                    command: ContactInvitationCommand::Inspect { invitation }
                }
            } if invitation == "00"
        ));
        let import = Arguments::try_parse_from([
            "arachne",
            "contact",
            "invitation",
            "import",
            "--state-directory",
            "/state",
            "--invitation",
            "00",
        ])
        .unwrap();
        assert!(matches!(
            import.command,
            Command::Contact {
                command: ContactCommand::Invitation {
                    command: ContactInvitationCommand::Import {
                        state_directory,
                        invitation
                    }
                }
            } if state_directory.as_path() == Path::new("/state") && invitation == "00"
        ));
    }

    #[test]
    fn parses_contact_verification_commands() {
        let verify = Arguments::try_parse_from([
            "arachne",
            "contact",
            "verify-qr",
            "--state-directory",
            "/state",
            "--payload",
            "00",
        ])
        .unwrap();
        assert!(matches!(
            verify.command,
            Command::Contact {
                command: ContactCommand::VerifyQr {
                    state_directory,
                    payload
                }
            } if state_directory.as_path() == Path::new("/state") && payload == "00"
        ));
        let safety_number = "11".repeat(32);
        let verify_safety_number = Arguments::try_parse_from([
            "arachne",
            "contact",
            "verify-safety-number",
            "--state-directory",
            "/state",
            "--contact-public-key",
            &safety_number,
            "--safety-number",
            &safety_number,
        ])
        .unwrap();
        assert!(matches!(
            verify_safety_number.command,
            Command::Contact {
                command: ContactCommand::VerifySafetyNumber {
                    state_directory,
                    contact_public_key,
                    safety_number: parsed_safety_number
                }
            } if state_directory.as_path() == Path::new("/state")
                && contact_public_key == safety_number
                && parsed_safety_number == safety_number
        ));
    }

    #[test]
    fn parses_contact_lifecycle_commands() {
        let rotation = Arguments::try_parse_from([
            "arachne",
            "contact",
            "rotate",
            "--state-directory",
            "/state",
            "--rotation",
            "00",
        ])
        .unwrap();
        assert!(matches!(
            rotation.command,
            Command::Contact {
                command: ContactCommand::Rotate {
                    state_directory,
                    rotation
                }
            } if state_directory.as_path() == Path::new("/state") && rotation == "00"
        ));
        let revoke = Arguments::try_parse_from([
            "arachne",
            "contact",
            "revoke",
            "--state-directory",
            "/state",
            "--contact-public-key",
            "00",
        ])
        .unwrap();
        assert!(matches!(
            revoke.command,
            Command::Contact {
                command: ContactCommand::Revoke {
                    state_directory,
                    contact_public_key
                }
            } if state_directory.as_path() == Path::new("/state") && contact_public_key == "00"
        ));
    }

    #[test]
    fn relay_profile_commands_emit_and_inspect_canonical_profiles() {
        let onion_service_public_key = "11".repeat(32);
        let encoded = relay_profile_record(&onion_service_public_key, 4444).unwrap();
        let profile = encoded.strip_prefix("profile=").unwrap().trim_end();
        assert_eq!(
            inspect_relay_profile(profile).unwrap(),
            format!("onion_service_public_key={onion_service_public_key}\nvirtual_port=4444\n")
        );
        let mut binary = vec![0; profile.len() / 2];
        decode_canonical_hex(profile, &mut binary).unwrap();
        assert_eq!(
            TorMaildropProfileConfig::decode(&binary)
                .unwrap()
                .virtual_port(),
            4444
        );
        assert!(inspect_relay_profile(&profile.to_uppercase()).is_err());
        assert!(inspect_relay_profile(&format!("{profile}00")).is_err());
        assert!(inspect_relay_profile(&format!("9803{}", &profile[2..])).is_err());
        assert!(relay_profile_record(&"00".repeat(32), 4444).is_err());
        assert!(relay_profile_record(&onion_service_public_key, 0).is_err());
    }

    #[test]
    fn parses_relay_profile_commands() {
        let onion_service_public_key = "11".repeat(32);
        let create = Arguments::try_parse_from([
            "arachne",
            "relay-profile",
            "create",
            "--onion-service-public-key",
            &onion_service_public_key,
            "--virtual-port",
            "4444",
        ])
        .unwrap();
        assert!(matches!(
            create.command,
            Command::RelayProfile {
                command: RelayProfileCommand::Create {
                    virtual_port: 4444,
                    ..
                }
            }
        ));
        let inspect =
            Arguments::try_parse_from(["arachne", "relay-profile", "inspect", "--profile", "8301"])
                .unwrap();
        assert!(matches!(
            inspect.command,
            Command::RelayProfile {
                command: RelayProfileCommand::Inspect { profile }
            } if profile == "8301"
        ));
    }

    #[test]
    fn message_send_queue_persists_a_canonical_encrypted_envelope() {
        let state_directory = std::env::temp_dir().join(format!(
            "arachne-cli-message-{}-{}",
            std::process::id(),
            NEXT_TEST_STATE_DIRECTORY.fetch_add(1, Ordering::Relaxed)
        ));
        fs::create_dir(&state_directory).unwrap();
        let recipient = IdentityPublicKey::from_bytes([0x11; 32]).unwrap();
        let envelope = EncryptedMessageEnvelope::new(vec![0xa1], vec![0xb2]).unwrap();
        let output = queue_message(
            &mut InMemoryKeystore::default(),
            &state_directory,
            recipient,
            envelope,
            MessageExpiry::new(100, 60).unwrap(),
        )
        .unwrap();
        assert!(output.starts_with("message_identifier="));
        fs::remove_dir_all(state_directory).unwrap();
    }

    #[test]
    fn message_submission_rejects_noncanonical_envelopes_and_unsafe_state_paths() {
        assert!(decode_envelope("8241a141b2").is_ok());
        assert!(decode_envelope("8241A141b2").is_err());
        assert!(decode_envelope("820141a141b2").is_err());
        assert!(validate_state_directory(Path::new("relative-state")).is_err());
        assert!(validate_state_directory(Path::new("/state/../other")).is_err());
    }

    #[test]
    fn parses_message_send_command() {
        let recipient_public_key = "11".repeat(32);
        let command = Arguments::try_parse_from([
            "arachne",
            "message",
            "send",
            "--state-directory",
            "/state",
            "--recipient-public-key",
            &recipient_public_key,
            "--envelope",
            "8241a141b2",
            "--created-at",
            "100",
            "--ttl-seconds",
            "60",
        ])
        .unwrap();
        assert!(matches!(
            command.command,
            Command::Message {
                command: MessageCommand::Send {
                    created_at: 100,
                    ttl_seconds: 60,
                    ..
                }
            }
        ));
    }

    #[test]
    fn dashboard_snapshot_and_widgets_show_redacted_inbox_outbox_and_delivery_state() {
        let state_directory = std::env::temp_dir().join(format!(
            "arachne-cli-dashboard-{}-{}",
            std::process::id(),
            NEXT_TEST_STATE_DIRECTORY.fetch_add(1, Ordering::Relaxed)
        ));
        fs::create_dir(&state_directory).unwrap();
        let mut keystore = InMemoryKeystore::default();
        let local_identity = IdentityKeypair::generate().unwrap();
        let recipient = IdentityKeypair::generate().unwrap();
        let invitation = hexadecimal(
            &ContactInvitation::create(&recipient)
                .unwrap()
                .encode()
                .unwrap(),
        );
        import_contact_invitation(
            &state_directory,
            &local_identity.public_key(),
            &invitation,
            &mut keystore,
        )
        .unwrap();
        let delivered =
            EncryptedMessageEnvelope::new(vec![0xa1], b"do-not-display-this-ciphertext".to_vec())
                .unwrap();
        let queued = EncryptedMessageEnvelope::new(vec![0xc3], vec![0xd4]).unwrap();
        let mut outbox =
            SenderOutbox::open(&state_directory.join(OUTBOX_DATABASE_FILE), &mut keystore).unwrap();
        outbox
            .enqueue(
                recipient.public_key(),
                delivered.clone(),
                MessageExpiry::new(100, 60).unwrap(),
            )
            .unwrap();
        let delivered_identifier = outbox.next().unwrap().identifier();
        outbox
            .acknowledge_delivery(
                &DeliveryAcknowledgement::create(&recipient, delivered_identifier, 101).unwrap(),
            )
            .unwrap();
        outbox
            .enqueue(
                recipient.public_key(),
                queued,
                MessageExpiry::new(102, 60).unwrap(),
            )
            .unwrap();
        let mut inbox = RecipientInboxDeduplication::open(
            &state_directory.join(INBOX_DATABASE_FILE),
            &mut keystore,
        )
        .unwrap();
        inbox.record_at(&delivered, 101).unwrap();
        let second_inbox = EncryptedMessageEnvelope::new(vec![0xe1], vec![0xf2]).unwrap();
        inbox.record_at(&second_inbox, 102).unwrap();
        let contacts =
            ContactStore::open(&state_directory.join(CONTACTS_DATABASE_FILE), &mut keystore)
                .unwrap();

        let dashboard = dashboard_from_stores(Some(&contacts), Some(&inbox), Some(&outbox));
        let snapshot = dashboard.snapshot();
        assert!(snapshot.contains("Contacts:\n"));
        assert!(snapshot.contains("Inbox:\n"));
        assert!(snapshot.contains("Identity:\n"));
        assert!(snapshot.contains("Outbox:\n"));
        assert!(snapshot.contains("Delivery state:\n"));
        assert!(snapshot.contains("received_at=101"));
        assert!(snapshot.contains("state=Delivered"));
        assert!(snapshot.contains("expires_at=162"));
        assert!(snapshot.contains("status=pending verification=none"));
        assert!(!snapshot.contains("do-not-display-this-ciphertext"));
        let mut rendered = Vec::new();
        render_dashboard(&mut rendered, &dashboard).unwrap();
        let rendered = String::from_utf8(rendered).unwrap();
        assert!(rendered.contains("Inbox"));
        assert!(rendered.contains("Outbox"));
        assert!(rendered.contains("Delivery state"));

        let mut tui = TuiDashboard::new(dashboard, true);
        tui.screen = TuiScreen::Inbox;
        tui.select_next_inbox_message();
        assert_eq!(tui.inbox_selection, 1);
        tui.select_next_inbox_message();
        assert_eq!(tui.inbox_selection, 1);
        tui.select_previous_inbox_message();
        assert_eq!(tui.inbox_selection, 0);
        tui.select_next_inbox_message();
        let mut inbox_rendered = Vec::new();
        render_tui_dashboard(&mut inbox_rendered, &tui).unwrap();
        let inbox_rendered = String::from_utf8(inbox_rendered).unwrap();
        assert!(inbox_rendered.contains("message=2 of 2 received_at=102"));
        assert!(inbox_rendered.contains("content=unavailable"));
        assert!(!inbox_rendered.contains("do-not-display-this-ciphertext"));

        drop(contacts);
        drop(inbox);
        drop(outbox);
        fs::remove_dir_all(state_directory).unwrap();
    }

    #[test]
    fn parses_tui_snapshot_command() {
        let command = Arguments::try_parse_from([
            "arachne",
            "tui",
            "--state-directory",
            "/state",
            "--snapshot",
        ])
        .unwrap();
        assert!(matches!(
            command.command,
            Command::Tui(TuiCommand {
                state_directory,
                snapshot: true,
                daemon: false
            }) if state_directory.as_path() == Path::new("/state")
        ));
    }

    #[test]
    fn parses_daemon_commands() {
        let command = Arguments::try_parse_from([
            "arachne",
            "daemon",
            "serve",
            "--config",
            "/config/mesh.conf",
        ])
        .unwrap();
        assert!(matches!(
            command.command,
            Command::Daemon {
                command: DaemonCommand::Serve { config }
            } if config.as_path() == Path::new("/config/mesh.conf")
        ));
        let command = Arguments::try_parse_from([
            "arachne",
            "tui",
            "--state-directory",
            "/state",
            "--daemon",
        ])
        .unwrap();
        assert!(matches!(
            command.command,
            Command::Tui(TuiCommand { daemon: true, .. })
        ));
    }

    #[test]
    fn parses_log_and_diagnostics_commands() {
        let logs = Arguments::try_parse_from([
            "arachne",
            "logs",
            "prune",
            "--state-directory",
            "/state",
            "--older-than-days",
            "7",
        ])
        .unwrap();
        assert!(matches!(
            logs.command,
            Command::Logs {
                command: LogCommand::Prune {
                    state_directory,
                    older_than_days: 7,
                }
            } if state_directory.as_path() == Path::new("/state")
        ));
        let diagnose =
            Arguments::try_parse_from(["arachne", "diagnose", "--state-directory", "/state"])
                .unwrap();
        assert!(matches!(
            diagnose.command,
            Command::Diagnose { state_directory } if state_directory.as_path() == Path::new("/state")
        ));
    }

    #[test]
    fn logs_and_diagnostics_report_only_local_operational_state() {
        let state_directory = std::env::temp_dir().join(format!(
            "arachne-cli-observability-{}-{}",
            std::process::id(),
            NEXT_TEST_STATE_DIRECTORY.fetch_add(1, Ordering::Relaxed)
        ));
        let log_directory = arachne_core::log_directory(&state_directory).unwrap();
        fs::create_dir_all(&log_directory).unwrap();
        fs::write(log_directory.join("arachne-client.jsonl"), b"{}\n").unwrap();
        fs::write(log_directory.join("notes.txt"), b"do-not-report").unwrap();

        let listed = run_log_command(LogCommand::List {
            state_directory: state_directory.clone(),
        })
        .unwrap();
        assert!(listed.contains("log_files=1\n"));
        assert!(listed.contains("log_file=arachne-client.jsonl bytes=3\n"));
        assert!(!listed.contains("notes.txt"));

        let diagnostics = diagnose(&state_directory).unwrap();
        assert!(diagnostics.contains("contacts=absent\n"));
        assert!(diagnostics.contains("log_files=1\n"));
        assert!(!diagnostics.contains("do-not-report"));

        let pruned = run_log_command(LogCommand::Prune {
            state_directory: state_directory.clone(),
            older_than_days: 0,
        })
        .unwrap();
        assert_eq!(pruned, "pruned_log_files=1\n");
        assert!(log_directory.join("notes.txt").is_file());
        fs::remove_dir_all(state_directory).unwrap();
    }

    #[test]
    fn parses_shared_ip_mesh_commands() {
        let command = Arguments::try_parse_from([
            "arachne",
            "local-mesh",
            "init",
            "--config",
            "/config/mesh.conf",
            "--state-directory",
            "/state",
            "--transport",
            "wifi_hotspot",
            "--listen-endpoint",
            "192.0.2.10:4242",
        ])
        .unwrap();
        assert!(matches!(
            command.command,
            Command::LocalMesh {
                command: LocalMeshCommand::Init { transport, .. }
            } if transport == "wifi_hotspot"
        ));
        let command = Arguments::try_parse_from([
            "arachne",
            "local-mesh",
            "peer",
            "add",
            "--config",
            "/config/mesh.conf",
            "--identity",
            "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a",
            "--certificate-fingerprint",
            "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a",
        ])
        .unwrap();
        assert!(matches!(
            command.command,
            Command::LocalMesh {
                command: LocalMeshCommand::Peer {
                    command: LocalMeshPeerCommand::Add { .. }
                }
            }
        ));
        let command = Arguments::try_parse_from([
            "arachne",
            "local-mesh",
            "connect",
            "--config",
            "/config/mesh.conf",
            "--identity",
            "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a",
        ])
        .unwrap();
        assert!(matches!(
            command.command,
            Command::LocalMesh {
                command: LocalMeshCommand::Connect {
                    timeout_seconds: 10,
                    ..
                }
            }
        ));
    }

    #[test]
    fn daemon_auth_token_entries_are_state_root_specific() {
        let first = std::env::temp_dir().join(format!(
            "arachne-cli-daemon-token-a-{}-{}",
            std::process::id(),
            NEXT_TEST_STATE_DIRECTORY.fetch_add(1, Ordering::Relaxed)
        ));
        let second = std::env::temp_dir().join(format!(
            "arachne-cli-daemon-token-b-{}-{}",
            std::process::id(),
            NEXT_TEST_STATE_DIRECTORY.fetch_add(1, Ordering::Relaxed)
        ));
        fs::create_dir(&first).unwrap();
        fs::create_dir(&second).unwrap();
        let first_entry = daemon_auth_token_entry(&first).unwrap();
        let second_entry = daemon_auth_token_entry(&second).unwrap();
        assert_ne!(first_entry, second_entry);
        assert!(first_entry.as_str().starts_with("daemon_auth_v1_"));
        assert!(first_entry.as_str().len() <= 64);
        fs::remove_dir_all(first).unwrap();
        fs::remove_dir_all(second).unwrap();
    }

    #[cfg(unix)]
    #[test]
    fn daemon_tui_client_uses_authenticated_unix_grpc() {
        let state_directory = std::env::temp_dir().join(format!(
            "arachne-cli-daemon-tui-{}-{}",
            std::process::id(),
            NEXT_TEST_STATE_DIRECTORY.fetch_add(1, Ordering::Relaxed)
        ));
        fs::create_dir(&state_directory).unwrap();
        let (auth, token) = DaemonLocalAuth::initialize().unwrap();
        let (ready_tx, ready_rx) = std::sync::mpsc::sync_channel(1);
        let server_state_directory = state_directory.clone();
        let server = std::thread::spawn(move || {
            let runtime = tokio::runtime::Builder::new_multi_thread()
                .enable_all()
                .build()
                .unwrap();
            runtime.block_on(async move {
                let mut daemon =
                    DaemonRuntime::start(ProtocolVersion::INITIAL, &server_state_directory)
                        .unwrap();
                let server = DaemonServer::bind_with_identity_keystore(
                    &daemon,
                    auth,
                    InMemoryKeystore::default(),
                )
                .unwrap();
                ready_tx.send(()).unwrap();
                let result = server.serve_until(std::future::pending()).await;
                daemon.shutdown().unwrap();
                result.unwrap();
            });
        });
        ready_rx.recv().unwrap();
        let socket_path = state_directory.join(arachne_daemon::DAEMON_UNIX_SOCKET_FILE);
        let mut client = DaemonTuiClient::connect_with_token(socket_path, token).unwrap();
        let dashboard = client.dashboard().unwrap();
        assert!(
            dashboard
                .identity
                .iter()
                .any(|line| line == "status=created")
        );
        let request = client.request(ShutdownDaemonRequest {});
        let response = client
            .runtime
            .block_on(client.client.shutdown_daemon(request))
            .unwrap()
            .into_inner();
        assert!(!response.running);
        drop(client);
        server.join().unwrap();
        fs::remove_dir_all(state_directory).unwrap();
    }

    #[test]
    fn tui_uses_an_embedded_sdk_runtime() {
        let state_directory = std::env::temp_dir().join(format!(
            "arachne-cli-tui-sdk-{}-{}",
            std::process::id(),
            NEXT_TEST_STATE_DIRECTORY.fetch_add(1, Ordering::Relaxed)
        ));
        fs::create_dir(&state_directory).unwrap();
        let mut client = start_embedded_tui_client(&state_directory).unwrap();

        assert!(client.is_running());
        client.shutdown().unwrap();
        fs::remove_dir_all(state_directory).unwrap();
    }

    #[test]
    fn tui_identity_flow_creates_then_reuses_the_client_identity() {
        let mut keystore = InMemoryKeystore::default();
        let (first, first_initialization) = initialize_tui_identity(&mut keystore).unwrap();
        let (second, second_initialization) = initialize_tui_identity(&mut keystore).unwrap();

        assert_eq!(first, second);
        assert_eq!(first_initialization, ClientIdentityInitialization::Created);
        assert_eq!(second_initialization, ClientIdentityInitialization::Loaded);
        let snapshot = dashboard_from_stores(None, None, None)
            .with_identity(first, first_initialization)
            .snapshot();
        assert!(snapshot.contains("Identity:\n"));
        assert!(snapshot.contains("status=created"));
        assert!(snapshot.contains("identifier="));
    }

    #[test]
    fn tui_contact_input_is_bounded_and_safety_verification_requires_two_fields() {
        let mut input = TuiContactInput::new(TuiContactInputKind::Invitation);
        for _ in 0..=MAX_TUI_CONTACT_INPUT_BYTES {
            input.push('a');
        }
        assert_eq!(input.value.len(), MAX_TUI_CONTACT_INPUT_BYTES);

        assert_eq!(
            tui_safety_number_parts("contact safety"),
            Ok(("contact", "safety"))
        );
        assert!(tui_safety_number_parts("contact").is_err());
        assert!(tui_safety_number_parts("contact safety extra").is_err());
        assert!(tui_safety_number_parts(" contact").is_err());
    }

    #[test]
    fn tui_message_input_is_bounded_canonical_and_redacts_the_envelope() {
        let recipient = IdentityKeypair::generate().unwrap().public_key();
        let envelope = EncryptedMessageEnvelope::new(vec![0xa1], b"encrypted-body".to_vec())
            .unwrap()
            .encode()
            .unwrap();
        let envelope = hexadecimal(&envelope);
        let mut input = TuiMessageInput::new();
        input.recipient = hexadecimal(recipient.as_bytes());

        assert_eq!(input.advance(), Ok(false));
        assert_eq!(input.stage, TuiMessageInputStage::Envelope);
        input.envelope = envelope.clone();
        assert_eq!(input.advance(), Ok(false));
        assert_eq!(input.stage, TuiMessageInputStage::Ttl);
        input.ttl_seconds = "60".to_owned();
        assert_eq!(input.advance(), Ok(true));

        let mut malformed = TuiMessageInput::new();
        malformed.stage = TuiMessageInputStage::Envelope;
        malformed.envelope = "820141a141b2".to_owned();
        assert_eq!(malformed.advance(), Err(()));

        let mut invalid_ttl = TuiMessageInput::new();
        invalid_ttl.stage = TuiMessageInputStage::Ttl;
        invalid_ttl.ttl_seconds = "0".to_owned();
        assert_eq!(invalid_ttl.advance(), Err(()));

        let mut bounded = TuiMessageInput::new();
        bounded.stage = TuiMessageInputStage::Envelope;
        bounded.envelope = "a".repeat(MAX_ENCRYPTED_MESSAGE_SUBMISSION_BYTES * 2);
        bounded.push('a');
        assert_eq!(
            bounded.envelope.len(),
            MAX_ENCRYPTED_MESSAGE_SUBMISSION_BYTES * 2
        );

        let mut tui = TuiDashboard::new(dashboard_from_stores(None, None, None), true);
        tui.message_input = Some(TuiMessageInput {
            stage: TuiMessageInputStage::Envelope,
            recipient: String::new(),
            envelope: envelope.clone(),
            ttl_seconds: String::new(),
        });
        let mut rendered = Vec::new();
        render_tui_dashboard(&mut rendered, &tui).unwrap();
        let rendered = String::from_utf8(rendered).unwrap();
        assert!(rendered.contains("redacted"));
        assert!(rendered.contains("never rendered"));
        assert!(!rendered.contains(&envelope));
    }

    #[test]
    fn tui_message_submission_queues_only_verified_contacts_through_the_public_sdk() {
        let state_directory = std::env::temp_dir().join(format!(
            "arachne-cli-tui-message-{}-{}",
            std::process::id(),
            NEXT_TEST_STATE_DIRECTORY.fetch_add(1, Ordering::Relaxed)
        ));
        fs::create_dir(&state_directory).unwrap();
        let mut client = start_embedded_tui_client(&state_directory).unwrap();
        let mut identity = SdkIdentityManager::new(InMemoryKeystore::default());
        let local_identity = identity.create_or_load().unwrap().public_key();
        let recipient = IdentityKeypair::generate().unwrap();
        let invitation = ContactInvitation::create(&recipient)
            .unwrap()
            .encode()
            .unwrap();
        let fingerprint =
            SafetyNumberFingerprint::derive(&local_identity, &recipient.public_key()).unwrap();
        {
            let mut contacts = client.contact_manager(&mut identity).unwrap();
            contacts.import_invitation(&invitation).unwrap();
            contacts
                .verify_safety_number(&recipient.public_key(), fingerprint.as_bytes())
                .unwrap();
        }
        let input = TuiMessageInput {
            stage: TuiMessageInputStage::Ttl,
            recipient: hexadecimal(recipient.public_key().as_bytes()),
            envelope: hexadecimal(
                &EncryptedMessageEnvelope::new(vec![0xa1], b"encrypted-body".to_vec())
                    .unwrap()
                    .encode()
                    .unwrap(),
            ),
            ttl_seconds: "60".to_owned(),
        };

        submit_tui_message_with_identity(&mut client, &mut identity, &input, 100).unwrap();
        client.shutdown().unwrap();
        let mut keystore = identity.into_inner();
        let outbox =
            SenderOutbox::open(&state_directory.join(OUTBOX_DATABASE_FILE), &mut keystore).unwrap();
        assert_eq!(outbox.messages().len(), 1);
        assert_eq!(outbox.messages()[0].recipient(), &recipient.public_key());
        assert_eq!(
            outbox.messages()[0].expiry(),
            MessageExpiry::new(100, 60).unwrap()
        );
        fs::remove_dir_all(state_directory).unwrap();
    }

    #[test]
    fn tui_message_submission_rejects_unverified_recipients_without_an_outbox_write() {
        let state_directory = std::env::temp_dir().join(format!(
            "arachne-cli-tui-message-unverified-{}-{}",
            std::process::id(),
            NEXT_TEST_STATE_DIRECTORY.fetch_add(1, Ordering::Relaxed)
        ));
        fs::create_dir(&state_directory).unwrap();
        let mut client = start_embedded_tui_client(&state_directory).unwrap();
        let mut identity = SdkIdentityManager::new(InMemoryKeystore::default());
        identity.create_or_load().unwrap();
        let recipient = IdentityKeypair::generate().unwrap();
        let input = TuiMessageInput {
            stage: TuiMessageInputStage::Ttl,
            recipient: hexadecimal(recipient.public_key().as_bytes()),
            envelope: hexadecimal(
                &EncryptedMessageEnvelope::new(vec![0xa1], b"encrypted-body".to_vec())
                    .unwrap()
                    .encode()
                    .unwrap(),
            ),
            ttl_seconds: "60".to_owned(),
        };

        assert!(submit_tui_message_with_identity(&mut client, &mut identity, &input, 100).is_err());
        client.shutdown().unwrap();
        assert!(!state_directory.join(OUTBOX_DATABASE_FILE).exists());
        fs::remove_dir_all(state_directory).unwrap();
    }

    #[test]
    fn tui_contact_input_updates_state_without_restarting_the_embedded_runtime() {
        let state_directory = std::env::temp_dir().join(format!(
            "arachne-cli-tui-contact-{}-{}",
            std::process::id(),
            NEXT_TEST_STATE_DIRECTORY.fetch_add(1, Ordering::Relaxed)
        ));
        fs::create_dir(&state_directory).unwrap();
        let local_identity = IdentityKeypair::generate().unwrap();
        let remote_identity = IdentityKeypair::generate().unwrap();
        let input = TuiContactInput {
            kind: TuiContactInputKind::Invitation,
            value: hexadecimal(
                &ContactInvitation::create(&remote_identity)
                    .unwrap()
                    .encode()
                    .unwrap(),
            ),
        };
        let mut keystore = InMemoryKeystore::default();

        submit_tui_contact_input_with_keystore(
            &state_directory,
            &local_identity.public_key(),
            &input,
            &mut keystore,
        )
        .unwrap();
        let contacts =
            ContactStore::open(&state_directory.join(CONTACTS_DATABASE_FILE), &mut keystore)
                .unwrap();
        assert_eq!(contacts.contacts().len(), 1);
        assert_eq!(contacts.contacts()[0].status(), ContactStatus::Pending);

        fs::remove_dir_all(state_directory).unwrap();
    }

    #[test]
    fn tui_status_exposes_only_runtime_and_generic_error_state() {
        let mut tui = TuiDashboard::new(dashboard_from_stores(None, None, None), true);
        tui.screen = TuiScreen::Status;
        tui.last_error = Some("contact update failed");
        let mut rendered = Vec::new();

        render_tui_status(&mut rendered, &tui).unwrap();

        let rendered = String::from_utf8(rendered).unwrap();
        assert!(rendered.contains("runtime=running"));
        assert!(rendered.contains("last_error=contact update failed"));
        assert!(!rendered.contains("ciphertext"));
    }

    #[test]
    fn tui_route_policy_requires_direct_acknowledgement_and_explicitly_changes_routes() {
        let mut tui = TuiDashboard::new(dashboard_from_stores(None, None, None), true);
        tui.screen = TuiScreen::RoutePolicy;

        handle_tui_route_policy_key(&mut tui, KeyCode::Char('d'));
        assert!(tui.route_policy.direct_acknowledgement_pending);
        assert_eq!(tui.route_policy.selection, None);
        handle_tui_route_policy_key(&mut tui, KeyCode::Char('t'));
        assert_eq!(tui.route_policy.selection, None);
        handle_tui_route_policy_key(&mut tui, KeyCode::Char('n'));
        assert!(!tui.route_policy.direct_acknowledgement_pending);
        assert_eq!(tui.route_policy.selection, None);

        handle_tui_route_policy_key(&mut tui, KeyCode::Char('d'));
        handle_tui_route_policy_key(&mut tui, KeyCode::Char('y'));
        let direct = tui.route_policy.profile.unwrap();
        assert_eq!(tui.route_policy.selection, Some(TuiRouteSelection::Direct));
        assert_eq!(direct.kind(), SdkDeliveryProfileKind::Direct);
        assert!(direct.has_direct_ip_disclosure_warning());

        handle_tui_route_policy_key(&mut tui, KeyCode::Char('t'));
        let tor = tui.route_policy.profile.unwrap();
        assert_eq!(
            tui.route_policy.selection,
            Some(TuiRouteSelection::TorMaildrop)
        );
        assert_eq!(tor.kind(), SdkDeliveryProfileKind::TorMaildrop);
        assert!(direct.validate_automatic_replacement(tor).is_err());
    }

    #[test]
    fn tui_route_policy_selects_each_local_route_and_renders_only_policy_state() {
        let mut tui = TuiDashboard::new(dashboard_from_stores(None, None, None), true);
        tui.screen = TuiScreen::RoutePolicy;
        let mut rendered = Vec::new();

        render_tui_route_policy(&mut rendered, &tui).unwrap();
        let rendered = String::from_utf8(rendered).unwrap();
        assert!(rendered.contains("selected=none"));
        assert!(rendered.contains("no automatic route replacement"));
        assert!(!rendered.contains("ciphertext"));

        for (key, transport) in [
            ('l', SdkLocalMeshTransportKind::Lan),
            ('h', SdkLocalMeshTransportKind::WifiHotspot),
            ('w', SdkLocalMeshTransportKind::WifiDirect),
            ('b', SdkLocalMeshTransportKind::Bluetooth),
        ] {
            handle_tui_route_policy_key(&mut tui, KeyCode::Char(key));
            assert_eq!(
                tui.route_policy.selection,
                Some(TuiRouteSelection::LocalMesh(transport))
            );
            assert_eq!(
                tui.route_policy.profile.unwrap().kind(),
                SdkDeliveryProfileKind::LocalMesh
            );
        }
    }

    #[test]
    fn release_manifest_sign_and_verify_pin_the_signing_key() {
        let directory = std::env::temp_dir().join(format!(
            "arachne-release-command-{}-{}",
            std::process::id(),
            NEXT_TEST_STATE_DIRECTORY.fetch_add(1, Ordering::Relaxed)
        ));
        fs::create_dir(&directory).unwrap();
        let artifact = directory.join("arachne");
        let manifest = directory.join("release-manifest.cbor");
        fs::write(&artifact, b"release artifact").unwrap();
        let signer = IdentityKeypair::generate().unwrap();

        let record = sign_release_manifest(&signer, REVISION, 123, &[artifact], &manifest).unwrap();
        assert!(record.contains("signing_public_key="));
        let verified = verify_release_manifest(
            &manifest,
            &directory,
            &hexadecimal(signer.public_key().as_bytes()),
        )
        .unwrap();
        assert_eq!(
            verified,
            format!("verified_artifacts=1\nsource_revision={REVISION}\nsource_date_epoch=123\n")
        );
        assert!(
            verify_release_manifest(
                &manifest,
                &directory,
                &hexadecimal(IdentityKeypair::generate().unwrap().public_key().as_bytes())
            )
            .is_err()
        );
        fs::remove_dir_all(directory).unwrap();
    }

    #[test]
    fn parses_release_manifest_sign_command() {
        let command = Arguments::try_parse_from([
            "arachne",
            "release-manifest",
            "sign",
            "--source-revision",
            REVISION,
            "--source-date-epoch",
            "123",
            "--artifact",
            "arachne",
            "--output",
            "release-manifest.cbor",
        ])
        .unwrap();
        assert!(matches!(
            command.command,
            Command::ReleaseManifest {
                command: ReleaseManifestCommand::Sign {
                    source_revision,
                    source_date_epoch: 123,
                    signing_key_name,
                    artifact,
                    output,
                },
            } if source_revision == REVISION
                && signing_key_name == "release_signing"
                && artifact == [PathBuf::from("arachne")]
                && output.as_path() == Path::new("release-manifest.cbor")
        ));
    }

    #[test]
    fn attachment_send_queue_persists_validated_encrypted_artifacts() {
        let state_directory = std::env::temp_dir().join(format!(
            "arachne-cli-attachment-{}-{}",
            std::process::id(),
            NEXT_TEST_STATE_DIRECTORY.fetch_add(1, Ordering::Relaxed)
        ));
        fs::create_dir(&state_directory).unwrap();
        let identifier = AttachmentIdentifier::from_bytes([0x22; 16]).unwrap();
        let key = AttachmentKey::derive(&[0x11; 32], identifier).unwrap();
        let chunk = EncryptedAttachmentChunk::encrypt(
            identifier,
            0,
            &key.derive_chunk_key(0).unwrap(),
            &vec![0xA5; ATTACHMENT_CHUNK_BYTES],
        )
        .unwrap();
        let manifest = AttachmentManifest::new(
            identifier,
            ATTACHMENT_CHUNK_BYTES as u64,
            vec![chunk.hash().unwrap()],
        )
        .unwrap()
        .encrypt(&key)
        .unwrap();
        let manifest_path = state_directory.join("input-manifest.cbor");
        let chunk_path = state_directory.join("input-chunk.cbor");
        fs::write(&manifest_path, manifest.encode().unwrap()).unwrap();
        fs::write(&chunk_path, chunk.encode().unwrap()).unwrap();
        let output = queue_attachment_submission(
            &state_directory,
            &manifest_path,
            std::slice::from_ref(&chunk_path),
        )
        .unwrap();
        assert!(output.starts_with("attachment_identifier="));
        let submission = state_directory
            .join(ATTACHMENT_UPLOAD_DIRECTORY)
            .join(hexadecimal(identifier.as_bytes()));
        assert!(submission.join("manifest.cbor").is_file());
        assert!(submission.join("journal.cbor").is_file());
        assert!(submission.join("chunk-0.cbor").is_file());
        let layout = ClientStateDirectory::new(&state_directory).unwrap();
        let transfers = load_attachment_transfers(&layout).unwrap();
        assert_eq!(
            transfers,
            ["transfer=1 chunk_count=1 complete=false next_pending_index=0"]
        );
        assert!(!transfers[0].contains(&hexadecimal(identifier.as_bytes())));
        let root = state_directory.join(ATTACHMENT_UPLOAD_DIRECTORY);
        fs::create_dir(root.join(format!(".{}.pending", hexadecimal(identifier.as_bytes()))))
            .unwrap();
        assert_eq!(load_attachment_transfers(&layout).unwrap(), transfers);
        fs::remove_dir(root.join(format!(".{}.pending", hexadecimal(identifier.as_bytes()))))
            .unwrap();
        fs::create_dir(root.join("invalid.pending")).unwrap();
        assert!(load_attachment_transfers(&layout).is_err());
        fs::remove_dir(root.join("invalid.pending")).unwrap();
        let mut dashboard = dashboard_from_stores(None, None, None);
        dashboard.attachments = transfers;
        let mut tui = TuiDashboard::new(dashboard, true);
        tui.screen = TuiScreen::Attachments;
        let mut rendered = Vec::new();
        render_tui_attachments(&mut rendered, &tui).unwrap();
        let rendered = String::from_utf8(rendered).unwrap();
        assert!(rendered.contains("Attachment transfers"));
        assert!(rendered.contains("chunk_count=1"));
        assert!(!rendered.contains(&hexadecimal(identifier.as_bytes())));
        assert!(
            queue_attachment_submission(
                &state_directory,
                &manifest_path,
                std::slice::from_ref(&chunk_path),
            )
            .is_err()
        );
        fs::remove_dir_all(state_directory).unwrap();
    }

    #[test]
    fn parses_attachment_send_command() {
        let command = Arguments::try_parse_from([
            "arachne",
            "attachment",
            "send",
            "--state-directory",
            "/state",
            "--manifest",
            "manifest.cbor",
            "--chunk",
            "chunk-0.cbor",
        ])
        .unwrap();
        assert!(matches!(
            command.command,
            Command::Attachment {
                command: AttachmentCommand::Send { chunk, .. }
            } if chunk == [PathBuf::from("chunk-0.cbor")]
        ));
    }
}
