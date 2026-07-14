#![forbid(unsafe_code)]

use clap::{Parser, Subcommand};
use sha2::{Digest, Sha256};
use std::{error::Error, fmt::Write as _, fs, path::PathBuf};

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
}
