use std::env;
use std::fs;
use std::path::{Path, PathBuf};

use serde::Serialize;

use crate::GIB;

pub const ZAP_PI_MIN_RAM_BYTES: u64 = 14 * GIB;
pub const ZAP_MODEL_PATH_ENV: &str = "KELP_PI_MODEL_PATH";
pub const ZAP_MEMINFO_PATH_ENV: &str = "KELP_PI_MEMINFO_PATH";

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct ZapStatus {
    pub opt_in: bool,
    pub raspberry_pi: bool,
    pub ram_bytes: Option<u64>,
    pub min_pi_ram_bytes: u64,
    pub probe_errors: Vec<String>,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
#[serde(rename_all = "kebab-case")]
pub enum ZapDecision {
    Allow,
    Refuse,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct ZapGuard {
    pub decision: ZapDecision,
    pub reason: String,
    pub status: ZapStatus,
}

pub fn evaluate_zap_guard(opt_in: bool) -> ZapGuard {
    let mut probe_errors = Vec::new();
    let model = read_optional_string(&model_path()).unwrap_or_else(|error| {
        probe_errors.push(error);
        None
    });
    let ram_bytes = read_mem_total_bytes(&meminfo_path()).map_or_else(
        |error| {
            probe_errors.push(error);
            None
        },
        Some,
    );
    zap_guard_from_probe(opt_in, model.as_deref(), ram_bytes, probe_errors)
}

fn zap_guard_from_probe(
    opt_in: bool,
    model: Option<&str>,
    ram_bytes: Option<u64>,
    probe_errors: Vec<String>,
) -> ZapGuard {
    let raspberry_pi = model
        .map(|model| model.to_ascii_lowercase().contains("raspberry pi"))
        .unwrap_or(false);
    let status = ZapStatus {
        opt_in,
        raspberry_pi,
        ram_bytes,
        min_pi_ram_bytes: ZAP_PI_MIN_RAM_BYTES,
        probe_errors,
    };
    if !opt_in {
        return ZapGuard {
            decision: ZapDecision::Refuse,
            reason: "ZAP scans require --enable-zap".to_string(),
            status,
        };
    }
    if raspberry_pi && ram_bytes.is_none_or(|bytes| bytes < ZAP_PI_MIN_RAM_BYTES) {
        return ZapGuard {
            decision: ZapDecision::Refuse,
            reason: format!(
                "ZAP scans require a 16GB-class Raspberry Pi; detected {} bytes",
                ram_bytes.unwrap_or(0)
            ),
            status,
        };
    }
    ZapGuard {
        decision: ZapDecision::Allow,
        reason: "ZAP guard allows scan".to_string(),
        status,
    }
}

fn model_path() -> PathBuf {
    env::var_os(ZAP_MODEL_PATH_ENV)
        .map(PathBuf::from)
        .unwrap_or_else(|| PathBuf::from("/proc/device-tree/model"))
}

fn meminfo_path() -> PathBuf {
    env::var_os(ZAP_MEMINFO_PATH_ENV)
        .map(PathBuf::from)
        .unwrap_or_else(|| PathBuf::from("/proc/meminfo"))
}

fn read_optional_string(path: &Path) -> Result<Option<String>, String> {
    match fs::read_to_string(path) {
        Ok(value) => Ok(Some(value.trim_end_matches('\0').trim().to_string())),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(None),
        Err(error) => Err(format!("model probe failed: {}: {error}", path.display())),
    }
}

fn read_mem_total_bytes(path: &Path) -> Result<u64, String> {
    let content = fs::read_to_string(path)
        .map_err(|error| format!("memory probe failed: {}: {error}", path.display()))?;
    let kib = content
        .lines()
        .find_map(|line| {
            let (key, rest) = line.split_once(':')?;
            if key != "MemTotal" {
                return None;
            }
            rest.split_whitespace().next()?.parse::<u64>().ok()
        })
        .ok_or_else(|| "memory probe missing MemTotal".to_string())?;
    Ok(kib.saturating_mul(1024))
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::{SystemTime, UNIX_EPOCH};

    #[test]
    fn zap_requires_explicit_opt_in() {
        let guard = zap_guard_from_probe(false, None, Some(32 * GIB), Vec::new());

        assert_eq!(guard.decision, ZapDecision::Refuse);
        assert!(guard.reason.contains("--enable-zap"));
    }

    #[test]
    fn zap_refuses_8gb_pi_and_allows_16gb_pi() {
        let eight_gb = zap_guard_from_probe(
            true,
            Some("Raspberry Pi 5 Model B Rev 1.0"),
            Some(8 * GIB),
            Vec::new(),
        );
        let sixteen_gb = zap_guard_from_probe(
            true,
            Some("Raspberry Pi 5 Model B Rev 1.0"),
            Some(15 * GIB),
            Vec::new(),
        );

        assert_eq!(eight_gb.decision, ZapDecision::Refuse);
        assert_eq!(sixteen_gb.decision, ZapDecision::Allow);
    }

    #[test]
    fn zap_allows_opted_in_non_pi_hosts() {
        let guard = zap_guard_from_probe(true, Some("MacBookPro"), Some(8 * GIB), Vec::new());

        assert_eq!(guard.decision, ZapDecision::Allow);
    }

    #[test]
    fn zap_probe_reads_meminfo() {
        let root = temp_root("zap-meminfo");
        fs::create_dir_all(&root).expect("root");
        let meminfo = root.join("meminfo");
        fs::write(&meminfo, "MemTotal:       16384000 kB\n").expect("meminfo");

        let bytes = read_mem_total_bytes(&meminfo).expect("read meminfo");

        fs::remove_dir_all(root).ok();
        assert_eq!(bytes, 16_777_216_000);
    }

    fn temp_root(name: &str) -> PathBuf {
        let nonce = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("clock")
            .as_nanos();
        std::env::temp_dir().join(format!("kelp-pi-agent-{name}-{nonce}"))
    }
}
