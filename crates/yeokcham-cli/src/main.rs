#![forbid(unsafe_code)]

use clap::{Parser, Subcommand};
use sha2::{Digest, Sha256};
use std::{collections::BTreeMap, error::Error, fmt::Write as _, fs, path::PathBuf};
use yeokcham_core::{
    IdentityKeypair, IdentityPublicKey, KeystoreEntryName, KeystoreSecret, OsKeystore,
};
use yeokcham_protocol::{
    CONTACT_INVITATION_BYTES, ContactInvitation, IdentityIdentifier,
    TOR_ONION_SERVICE_PUBLIC_KEY_BYTES, TorMaildropProfileConfig,
};

#[cfg(target_os = "linux")]
use yeokcham_core::LinuxKeystore;
#[cfg(target_os = "macos")]
use yeokcham_core::MacOsKeystore;
#[cfg(target_os = "windows")]
use yeokcham_core::WindowsKeystore;

const MAX_PROTOCOL_VECTOR_BYTES: u64 = 16_384;
const MAX_RELAY_PROFILE_BYTES: usize = 64;
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
        #[arg(long, default_value = "identity_primary")]
        name: String,
    },
    Show {
        #[arg(long, default_value = "identity_primary")]
        name: String,
    },
}

#[derive(Subcommand)]
enum ContactCommand {
    Invitation {
        #[command(subcommand)]
        command: ContactInvitationCommand,
    },
}

#[derive(Subcommand)]
enum ContactInvitationCommand {
    Create {
        #[arg(long, default_value = "identity_primary")]
        name: String,
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

fn main() -> Result<(), Box<dyn Error>> {
    let arguments = Arguments::parse();
    match arguments.command {
        Command::Version => println!(
            "protocol {}",
            yeokcham_protocol::ProtocolVersion::INITIAL.get()
        ),
        Command::Identity { command } => {
            let name = match &command {
                IdentityCommand::Create { name } | IdentityCommand::Show { name } => name.clone(),
            };
            let entry = KeystoreEntryName::new(name)?;
            let public_key = match command {
                IdentityCommand::Create { .. } => create_system_identity(&entry)?,
                IdentityCommand::Show { .. } => load_system_identity(&entry)?.public_key(),
            };
            print!("{}", identity_record(&public_key));
        }
        Command::Contact { command } => match command {
            ContactCommand::Invitation { command } => match command {
                ContactInvitationCommand::Create { name } => {
                    let entry = KeystoreEntryName::new(name)?;
                    print!(
                        "{}",
                        contact_invitation_record(&load_system_identity(&entry)?)?
                    );
                }
                ContactInvitationCommand::Inspect { invitation } => {
                    print!("{}", inspect_contact_invitation(&invitation)?);
                }
            },
        },
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
        Command::ProtocolVectors { verify } => {
            if let Some(input) = verify {
                if fs::metadata(&input)?.len() > MAX_PROTOCOL_VECTOR_BYTES {
                    return Err("protocol vector file exceeds maximum size".into());
                }
                let candidate = fs::read_to_string(input)?;
                verify_protocol_vectors(&candidate).map_err(|error| {
                    std::io::Error::new(std::io::ErrorKind::InvalidInput, error)
                })?;
            } else {
                print!("{PROTOCOL_V1_VECTORS}");
            }
        }
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
    if encoded.is_empty() || encoded.len() % 2 != 0 || encoded.len() / 2 > maximum_bytes {
        return Err("hexadecimal input has an invalid length");
    }
    let mut output = vec![0; encoded.len() / 2];
    decode_canonical_hex(encoded, &mut output)?;
    Ok(output)
}

const fn hexadecimal_nibble(byte: u8) -> Option<u8> {
    match byte {
        b'0'..=b'9' => Some(byte - b'0'),
        b'a'..=b'f' => Some(byte - b'a' + 10),
        _ => None,
    }
}

fn create_system_identity(entry: &KeystoreEntryName) -> Result<IdentityPublicKey, Box<dyn Error>> {
    #[cfg(target_os = "linux")]
    {
        let mut keystore = LinuxKeystore::new()?;
        return create_identity(&mut keystore, entry);
    }
    #[cfg(target_os = "macos")]
    {
        let mut keystore = MacOsKeystore::new();
        return create_identity(&mut keystore, entry);
    }
    #[cfg(target_os = "windows")]
    {
        let mut keystore = WindowsKeystore::new()?;
        return create_identity(&mut keystore, entry);
    }
    #[allow(unreachable_code)]
    Err("unsupported operating system keystore".into())
}

fn load_system_identity(entry: &KeystoreEntryName) -> Result<IdentityKeypair, Box<dyn Error>> {
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
    use std::convert::Infallible;

    use clap::Parser;

    use super::{
        Arguments, Command, ContactCommand, ContactInvitation, ContactInvitationCommand,
        IdentityCommand, IdentityKeypair, IdentityPublicKey, KeystoreEntryName, KeystoreSecret,
        OsKeystore, RelayProfileCommand, TorMaildropProfileConfig, contact_invitation_record,
        create_identity, decode_canonical_hex, identity_record, inspect_contact_invitation,
        inspect_relay_profile, load_identity, relay_profile_record, release_metadata,
    };

    const REVISION: &str = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";

    #[derive(Default)]
    struct InMemoryKeystore {
        secret: Option<KeystoreSecret>,
    }

    impl OsKeystore for InMemoryKeystore {
        type Error = Infallible;

        fn load(&self, _: &KeystoreEntryName) -> Result<Option<KeystoreSecret>, Self::Error> {
            Ok(self
                .secret
                .as_ref()
                .map(|secret| KeystoreSecret::new(secret.as_bytes().to_vec()).unwrap()))
        }

        fn store(
            &mut self,
            _: &KeystoreEntryName,
            secret: &KeystoreSecret,
        ) -> Result<(), Self::Error> {
            self.secret = Some(KeystoreSecret::new(secret.as_bytes().to_vec()).unwrap());
            Ok(())
        }

        fn delete(&mut self, _: &KeystoreEntryName) -> Result<(), Self::Error> {
            self.secret = None;
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
                command: IdentityCommand::Create { name }
            } if name == "identity_primary"
        ));
        let show = Arguments::try_parse_from([
            "yeokcham",
            "identity",
            "show",
            "--name",
            "identity_secondary",
        ])
        .unwrap();
        assert!(matches!(
            show.command,
            Command::Identity {
                command: IdentityCommand::Show { name }
            } if name == "identity_secondary"
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
    fn parses_contact_invitation_commands() {
        let create =
            Arguments::try_parse_from(["yeokcham", "contact", "invitation", "create"]).unwrap();
        assert!(matches!(
            create.command,
            Command::Contact {
                command: ContactCommand::Invitation {
                    command: ContactInvitationCommand::Create { name }
                }
            } if name == "identity_primary"
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
        let mut binary = Vec::new();
        binary.resize(profile.len() / 2, 0);
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
}
