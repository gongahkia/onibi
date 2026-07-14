#![forbid(unsafe_code)]

use clap::{Parser, Subcommand};
use sha2::{Digest, Sha256};
use std::{collections::BTreeMap, error::Error, fmt::Write as _, fs, path::PathBuf};

const MAX_PROTOCOL_VECTOR_BYTES: u64 = 16_384;
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

fn main() -> Result<(), Box<dyn Error>> {
    let arguments = Arguments::parse();
    match arguments.command {
        Command::Version => println!(
            "protocol {}",
            yeokcham_protocol::ProtocolVersion::INITIAL.get()
        ),
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
    let mut hexadecimal = String::with_capacity(64);
    for byte in Sha256::digest(input) {
        write!(&mut hexadecimal, "{byte:02x}").expect("writing to String cannot fail");
    }
    hexadecimal
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
    use super::release_metadata;

    const REVISION: &str = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";

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
}
