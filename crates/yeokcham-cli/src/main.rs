#![forbid(unsafe_code)]

mod release_manifest;

use clap::{Args, Parser, Subcommand};
use crossterm::{
    cursor::{Hide, MoveTo, Show},
    event::{self, Event, KeyCode, KeyEventKind},
    execute, queue,
    style::Print,
    terminal::{self, Clear, ClearType, EnterAlternateScreen, LeaveAlternateScreen},
};
use sha2::{Digest, Sha256};
use std::{
    collections::BTreeMap,
    error::Error,
    fmt::Write as _,
    fs,
    io::{self, Write},
    path::{Path, PathBuf},
    time::{SystemTime, UNIX_EPOCH},
};
#[cfg(test)]
use yeokcham_core::KeystoreSecret;
use yeokcham_core::{IdentityKeypair, IdentityPublicKey, KeystoreEntryName, OsKeystore};
use yeokcham_daemon::{
    AttachmentSubmissionStore, ClientIdentity, ClientIdentityInitialization, ClientStateDirectory,
    ContactLifecycleService, ContactStatus, ContactStore, ContactVerificationMethod, DaemonRuntime,
    InboxMessage, MessageExpiry, PendingContactImportService, QrContactVerificationService,
    RecipientInboxDeduplication, SafetyNumberVerificationService, SenderOutbox,
};
use yeokcham_protocol::{
    ATTACHMENT_IDENTIFIER_BYTES, AttachmentIdentifier, AttachmentUploadJournal,
    CONTACT_INVITATION_BYTES, ContactInvitation, EncryptedAttachmentChunk,
    EncryptedAttachmentManifest, EncryptedMessageEnvelope, IDENTITY_ROTATION_BYTES,
    IdentityIdentifier, MAX_ENCODED_ATTACHMENT_CHUNK_BYTES, MAX_ENCODED_ATTACHMENT_MANIFEST_BYTES,
    QR_VERIFICATION_PAYLOAD_BYTES, SAFETY_NUMBER_FINGERPRINT_BYTES,
    TOR_ONION_SERVICE_PUBLIC_KEY_BYTES, TorMaildropProfileConfig,
};
use yeokcham_sdk::{
    SdkClient, SdkClientBuilder, SdkContactStatus, SdkDeliveryProfile, SdkDeliveryProfilePolicy,
    SdkDirectIpDisclosureAcknowledgement, SdkIdentityManager, SdkLocalMeshPolicy,
    SdkLocalMeshTransportKind, SdkMessageEnvelope, SdkMessageExpiry, SdkMessageSendRequest,
};

use release_manifest::{ReleaseArtifact, SignedReleaseArtifactManifest};

#[cfg(target_os = "linux")]
use yeokcham_core::LinuxKeystore;
#[cfg(target_os = "macos")]
use yeokcham_core::MacOsKeystore;
#[cfg(target_os = "windows")]
use yeokcham_core::WindowsKeystore;

const MAX_PROTOCOL_VECTOR_BYTES: u64 = 16_384;
const MAX_RELAY_PROFILE_BYTES: usize = 64;
const MAX_ENCRYPTED_MESSAGE_SUBMISSION_BYTES: usize = 1024 * 1024;
const MAX_TUI_CONTACT_INPUT_BYTES: usize = CONTACT_INVITATION_BYTES * 2;
const MAX_TUI_ATTACHMENT_TRANSFERS: usize = 256;
const PROTOCOL_V1_VECTORS: &str = include_str!("../../yeokcham-protocol/vectors/protocol-v1.txt");

#[derive(Parser)]
#[command(name = "yeokcham", version, about = "Yeokcham secure courier")]
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
    Create,
    Show,
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
    Create,
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

#[derive(Args)]
struct TuiCommand {
    #[arg(long)]
    state_directory: PathBuf,
    #[arg(long)]
    snapshot: bool,
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
            yeokcham_protocol::ProtocolVersion::INITIAL.get()
        ),
        Command::Identity { command } => {
            let public_key = match command {
                IdentityCommand::Create => create_client_system_identity()?,
                IdentityCommand::Show => load_client_system_identity()?.public_key(),
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
        Command::Tui(command) => tui(&command.state_directory, command.snapshot)?,
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
            ContactInvitationCommand::Create => print!(
                "{}",
                contact_invitation_record(load_client_system_identity()?.keypair())?
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
        yeokcham_protocol::ProtocolVersion::INITIAL.get(),
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
        DaemonRuntime::start(yeokcham_protocol::ProtocolVersion::INITIAL, state_directory)?;
    let local_identity = load_client_system_identity()?.public_key();
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
        DaemonRuntime::start(yeokcham_protocol::ProtocolVersion::INITIAL, state_directory)?;
    let local_identity = load_client_system_identity()?.public_key();
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
        DaemonRuntime::start(yeokcham_protocol::ProtocolVersion::INITIAL, state_directory)?;
    let local_identity = load_client_system_identity()?.public_key();
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
        DaemonRuntime::start(yeokcham_protocol::ProtocolVersion::INITIAL, state_directory)?;
    let local_identity = load_client_system_identity()?.public_key();
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
        DaemonRuntime::start(yeokcham_protocol::ProtocolVersion::INITIAL, state_directory)?;
    let local_identity = load_client_system_identity()?.public_key();
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

fn queue_system_message(
    state_directory: &Path,
    recipient_public_key: &str,
    envelope: &str,
    created_at: u64,
    ttl_seconds: u32,
) -> Result<String, Box<dyn Error>> {
    validate_state_directory(state_directory)?;
    let _runtime =
        DaemonRuntime::start(yeokcham_protocol::ProtocolVersion::INITIAL, state_directory)?;
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

fn tui(state_directory: &Path, snapshot: bool) -> Result<(), Box<dyn Error>> {
    let (identity, initialization) = create_or_load_client_system_identity()?;
    validate_state_directory(state_directory)?;
    let mut client = start_embedded_tui_client(state_directory)?;
    let dashboard = match load_system_dashboard_with_keystore(state_directory) {
        Ok(dashboard) => dashboard.with_identity(identity, initialization),
        Err(error) => {
            let _ = client.shutdown();
            return Err(error);
        }
    };
    if snapshot {
        print!("{}", dashboard.snapshot());
        client.shutdown()?;
    } else {
        let result = run_dashboard(dashboard, state_directory, &mut client);
        let shutdown = client.shutdown();
        result?;
        shutdown?;
    }
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
    client: &mut SdkClient,
) -> Result<(), Box<dyn Error>> {
    let _restore = TerminalRestoreGuard;
    terminal::enable_raw_mode()?;
    let mut output = io::stdout();
    execute!(output, EnterAlternateScreen, Hide)?;
    let mut dashboard = TuiDashboard::new(dashboard, client.is_running());
    dashboard_event_loop(&mut output, &mut dashboard, state_directory, client)
}

fn dashboard_event_loop(
    output: &mut impl Write,
    dashboard: &mut TuiDashboard,
    state_directory: &Path,
    client: &mut SdkClient,
) -> Result<(), Box<dyn Error>> {
    loop {
        render_tui_dashboard(output, dashboard)?;
        if let Event::Key(key) = event::read()?
            && key.kind == KeyEventKind::Press
        {
            if dashboard.contact_input.is_some() {
                handle_tui_contact_input(dashboard, state_directory, key.code)?;
            } else if dashboard.message_input.is_some() {
                handle_tui_message_input(dashboard, state_directory, client, key.code)?;
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
            if submit_tui_contact_input(state_directory, &input).is_ok() {
                if let Ok(updated) = load_system_dashboard_with_keystore(state_directory) {
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
    client: &mut SdkClient,
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
                if submit_tui_message(client, &input).is_ok() {
                    if let Ok(updated) = load_system_dashboard_with_keystore(state_directory) {
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
    input: &TuiMessageInput,
) -> Result<(), Box<dyn Error>> {
    let created_at = SystemTime::now().duration_since(UNIX_EPOCH)?.as_secs();
    #[cfg(target_os = "linux")]
    {
        let mut identity = SdkIdentityManager::new(LinuxKeystore::new()?);
        return submit_tui_message_with_identity(client, &mut identity, input, created_at);
    }
    #[cfg(target_os = "macos")]
    {
        let mut identity = SdkIdentityManager::new(MacOsKeystore::new());
        return submit_tui_message_with_identity(client, &mut identity, input, created_at);
    }
    #[cfg(target_os = "windows")]
    {
        let mut identity = SdkIdentityManager::new(WindowsKeystore::new()?);
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
    let local_identity = load_client_system_identity()?.public_key();
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
    queue!(
        output,
        MoveTo(0, 0),
        Clear(ClearType::All),
        Print(dashboard.dashboard.snapshot())
    )?;
    if let Some(input) = &dashboard.contact_input {
        queue!(
            output,
            Print(format!("{}: {}\n", input.kind.prompt(), input.value)),
            Print("Enter submits; Esc cancels.\n")
        )?;
    } else if let Some(input) = &dashboard.message_input {
        queue!(
            output,
            Print(format!(
                "{}: {}\n",
                input.stage.prompt(),
                input.rendered_value()
            )),
            Print(
                "The encrypted envelope is never rendered. Enter advances or queues; Esc cancels.\n"
            )
        )?;
    } else {
        queue!(
            output,
            Print(
                "a: attachments; b: inbox; m: queue encrypted message; o: status; p: route policy; i: import invitation; r: verify QR; s: verify safety number; q: exit.\n"
            )
        )?;
    }
    if let Some(notice) = dashboard.notice {
        queue!(output, Print(format!("{notice}\n")))?;
    }
    output.flush()
}

fn render_tui_route_policy(
    output: &mut impl Write,
    dashboard: &TuiDashboard,
) -> std::io::Result<()> {
    queue!(
        output,
        MoveTo(0, 0),
        Clear(ClearType::All),
        Print("Route policy:\n"),
        Print(format!(
            "selected={}\n",
            dashboard
                .route_policy
                .selection
                .map_or("none", TuiRouteSelection::label)
        )),
        Print("d: direct (requires IP-disclosure acknowledgement); t: Tor maildrop.\n"),
        Print("l: LAN; h: Wi-Fi hotspot; w: Wi-Fi Direct; b: Bluetooth.\n"),
        Print("Every route change is explicit; no automatic route replacement occurs.\n")
    )?;
    if dashboard.route_policy.direct_acknowledgement_pending {
        queue!(
            output,
            Print(
                "Direct delivery may disclose your IP address. Press y to acknowledge or n/Esc to cancel.\n"
            )
        )?;
    } else {
        queue!(output, Print("p or Esc returns; q returns to overview.\n"))?;
    }
    if let Some(notice) = dashboard.notice {
        queue!(output, Print(format!("{notice}\n")))?;
    }
    if let Some(error) = dashboard.last_error {
        queue!(output, Print(format!("error={error}\n")))?;
    }
    output.flush()
}

fn render_tui_inbox(output: &mut impl Write, dashboard: &TuiDashboard) -> std::io::Result<()> {
    queue!(
        output,
        MoveTo(0, 0),
        Clear(ClearType::All),
        Print("Inbox:\n")
    )?;
    let count = dashboard.dashboard.inbox_messages.len();
    if let Some(message) = dashboard.selected_inbox_message() {
        queue!(
            output,
            Print(format!(
                "message={} of {} received_at={} encrypted_header_bytes={} ciphertext_bytes={}\n",
                dashboard.inbox_selection + 1,
                count,
                message.received_at(),
                message.encrypted_header_bytes(),
                message.ciphertext_bytes()
            )),
            Print("content=unavailable\n"),
            Print("Use Up/Down or j/k to select; b or Esc returns; q exits.\n")
        )?;
    } else {
        queue!(
            output,
            Print("no retained message metadata\n"),
            Print("b or Esc returns; q exits.\n")
        )?;
    }
    output.flush()
}

fn render_tui_attachments(
    output: &mut impl Write,
    dashboard: &TuiDashboard,
) -> std::io::Result<()> {
    queue!(
        output,
        MoveTo(0, 0),
        Clear(ClearType::All),
        Print("Attachment transfers:\n")
    )?;
    for transfer in &dashboard.dashboard.attachments {
        queue!(output, Print(format!("  {transfer}\n")))?;
    }
    queue!(output, Print("a or Esc returns; q exits.\n"))?;
    output.flush()
}

fn render_tui_status(output: &mut impl Write, dashboard: &TuiDashboard) -> std::io::Result<()> {
    queue!(
        output,
        MoveTo(0, 0),
        Clear(ClearType::All),
        Print("Operational status:\n"),
        Print(format!(
            "embedded_runtime={}\n",
            if dashboard.runtime_running {
                "running"
            } else {
                "unavailable"
            }
        )),
        Print(format!(
            "last_error={}\n",
            dashboard.last_error.unwrap_or("none")
        )),
        Print("o or Esc returns; q exits.\n")
    )?;
    output.flush()
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

fn queue_system_attachment(
    state_directory: &Path,
    manifest_path: &Path,
    chunk_paths: &[PathBuf],
) -> Result<String, Box<dyn Error>> {
    validate_state_directory(state_directory)?;
    let _runtime =
        DaemonRuntime::start(yeokcham_protocol::ProtocolVersion::INITIAL, state_directory)?;
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

fn create_client_system_identity() -> Result<IdentityPublicKey, Box<dyn Error>> {
    #[cfg(target_os = "linux")]
    {
        let mut keystore = LinuxKeystore::new()?;
        return Ok(ClientIdentity::create(&mut keystore)?.public_key());
    }
    #[cfg(target_os = "macos")]
    {
        let mut keystore = MacOsKeystore::new();
        return Ok(ClientIdentity::create(&mut keystore)?.public_key());
    }
    #[cfg(target_os = "windows")]
    {
        let mut keystore = WindowsKeystore::new()?;
        return Ok(ClientIdentity::create(&mut keystore)?.public_key());
    }
    #[allow(unreachable_code)]
    Err("unsupported operating system keystore".into())
}

fn create_or_load_client_system_identity()
-> Result<(IdentityPublicKey, ClientIdentityInitialization), Box<dyn Error>> {
    #[cfg(target_os = "linux")]
    {
        let mut keystore = LinuxKeystore::new()?;
        return initialize_tui_identity(&mut keystore);
    }
    #[cfg(target_os = "macos")]
    {
        let mut keystore = MacOsKeystore::new();
        return initialize_tui_identity(&mut keystore);
    }
    #[cfg(target_os = "windows")]
    {
        let mut keystore = WindowsKeystore::new()?;
        return initialize_tui_identity(&mut keystore);
    }
    #[allow(unreachable_code)]
    Err("unsupported operating system keystore".into())
}

fn initialize_tui_identity<K: OsKeystore>(
    keystore: &mut K,
) -> Result<(IdentityPublicKey, ClientIdentityInitialization), Box<dyn Error>> {
    let (identity, initialization) = ClientIdentity::create_or_load(keystore)?;
    Ok((identity.public_key(), initialization))
}

fn identity_initialization_label(initialization: ClientIdentityInitialization) -> &'static str {
    match initialization {
        ClientIdentityInitialization::Created => "created",
        ClientIdentityInitialization::Loaded => "loaded",
    }
}

fn load_client_system_identity() -> Result<ClientIdentity, Box<dyn Error>> {
    #[cfg(target_os = "linux")]
    {
        let keystore = LinuxKeystore::new()?;
        return Ok(ClientIdentity::load(&keystore)?);
    }
    #[cfg(target_os = "macos")]
    {
        let keystore = MacOsKeystore::new();
        return Ok(ClientIdentity::load(&keystore)?);
    }
    #[cfg(target_os = "windows")]
    {
        let keystore = WindowsKeystore::new()?;
        return Ok(ClientIdentity::load(&keystore)?);
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
        ContactInvitationCommand, EncryptedMessageEnvelope, IdentityCommand, IdentityKeypair,
        IdentityPublicKey, KeystoreEntryName, MAX_ENCRYPTED_MESSAGE_SUBMISSION_BYTES,
        MAX_TUI_CONTACT_INPUT_BYTES, MessageCommand, MessageExpiry, OsKeystore,
        RecipientInboxDeduplication, RelayProfileCommand, ReleaseManifestCommand, SenderOutbox,
        TorMaildropProfileConfig, TuiCommand, TuiContactInput, TuiContactInputKind, TuiDashboard,
        TuiMessageInput, TuiMessageInputStage, TuiRouteSelection, TuiScreen,
        apply_contact_rotation, contact_invitation_record, create_identity, dashboard_from_stores,
        decode_canonical_hex, decode_envelope, handle_tui_route_policy_key, hexadecimal,
        identity_record, import_contact_invitation, initialize_tui_identity,
        inspect_contact_invitation, inspect_relay_profile, load_attachment_transfers,
        load_identity, queue_attachment_submission, queue_message, relay_profile_record,
        release_metadata, render_dashboard, render_tui_attachments, render_tui_dashboard,
        render_tui_route_policy, render_tui_status, revoke_contact, sign_release_manifest,
        start_embedded_tui_client, submit_tui_contact_input_with_keystore,
        submit_tui_message_with_identity, tui_safety_number_parts, validate_state_directory,
        verify_contact_qr, verify_contact_safety_number, verify_release_manifest,
    };
    use yeokcham_core::KeystoreSecret;
    use yeokcham_daemon::{
        ATTACHMENT_UPLOAD_DIRECTORY, CONTACTS_DATABASE_FILE, ClientIdentityInitialization,
        ClientStateDirectory, ContactStatus, ContactStore, INBOX_DATABASE_FILE,
        OUTBOX_DATABASE_FILE,
    };
    use yeokcham_protocol::{
        ATTACHMENT_CHUNK_BYTES, AttachmentIdentifier, AttachmentKey, AttachmentManifest,
        DeliveryAcknowledgement, EncryptedAttachmentChunk, IdentityRotation, QrVerificationPayload,
        SafetyNumberFingerprint,
    };
    use yeokcham_sdk::{SdkDeliveryProfileKind, SdkIdentityManager, SdkLocalMeshTransportKind};

    const REVISION: &str = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
    static NEXT_TEST_STATE_DIRECTORY: AtomicU64 = AtomicU64::new(0);

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
            "identity_identifier=fbcc7bd59b35de83c8ea6d3ff094463cda5c962f1e71c9d6cdbef01fc36178ae\n",
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
            "public_key=d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a\nidentity_identifier=fbcc7bd59b35de83c8ea6d3ff094463cda5c962f1e71c9d6cdbef01fc36178ae\n"
        );
    }

    #[test]
    fn parses_identity_create_and_show_commands() {
        let create = Arguments::try_parse_from(["yeokcham", "identity", "create"]).unwrap();
        assert!(matches!(
            create.command,
            Command::Identity {
                command: IdentityCommand::Create
            }
        ));
        let show = Arguments::try_parse_from(["yeokcham", "identity", "show"]).unwrap();
        assert!(matches!(
            show.command,
            Command::Identity {
                command: IdentityCommand::Show
            }
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
            "yeokcham-cli-contact-import-{}-{}",
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
            "yeokcham-cli-qr-contact-verification-{}-{}",
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
            "yeokcham-cli-safety-number-verification-{}-{}",
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
            "yeokcham-cli-contact-lifecycle-{}-{}",
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
        let create =
            Arguments::try_parse_from(["yeokcham", "contact", "invitation", "create"]).unwrap();
        assert!(matches!(
            create.command,
            Command::Contact {
                command: ContactCommand::Invitation {
                    command: ContactInvitationCommand::Create
                }
            }
        ));
        let inspect = Arguments::try_parse_from([
            "yeokcham",
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
            "yeokcham",
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
            "yeokcham",
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
            "yeokcham",
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
            "yeokcham",
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
            "yeokcham",
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
            "yeokcham",
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
        let inspect = Arguments::try_parse_from([
            "yeokcham",
            "relay-profile",
            "inspect",
            "--profile",
            "8301",
        ])
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
            "yeokcham-cli-message-{}-{}",
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
            "yeokcham",
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
            "yeokcham-cli-dashboard-{}-{}",
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
            "yeokcham",
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
                snapshot: true
            }) if state_directory.as_path() == Path::new("/state")
        ));
    }

    #[test]
    fn tui_uses_an_embedded_sdk_runtime() {
        let state_directory = std::env::temp_dir().join(format!(
            "yeokcham-cli-tui-sdk-{}-{}",
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
            "yeokcham-cli-tui-message-{}-{}",
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
            "yeokcham-cli-tui-message-unverified-{}-{}",
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
            "yeokcham-cli-tui-contact-{}-{}",
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
        assert!(rendered.contains("embedded_runtime=running"));
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
            "yeokcham-release-command-{}-{}",
            std::process::id(),
            NEXT_TEST_STATE_DIRECTORY.fetch_add(1, Ordering::Relaxed)
        ));
        fs::create_dir(&directory).unwrap();
        let artifact = directory.join("yeokcham");
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
            "yeokcham",
            "release-manifest",
            "sign",
            "--source-revision",
            REVISION,
            "--source-date-epoch",
            "123",
            "--artifact",
            "yeokcham",
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
                && artifact == [PathBuf::from("yeokcham")]
                && output.as_path() == Path::new("release-manifest.cbor")
        ));
    }

    #[test]
    fn attachment_send_queue_persists_validated_encrypted_artifacts() {
        let state_directory = std::env::temp_dir().join(format!(
            "yeokcham-cli-attachment-{}-{}",
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
            "yeokcham",
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
