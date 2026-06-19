use std::env;
use std::fs;
use std::path::{Path, PathBuf};

use serde::Serialize;

use crate::GIB;

pub const OLLAMA_PI_MIN_RAM_BYTES: u64 = 14 * GIB;
pub const OLLAMA_MODEL_PATH_ENV: &str = "KELP_PI_OLLAMA_MODEL_PATH";
pub const OLLAMA_MEMINFO_PATH_ENV: &str = "KELP_PI_OLLAMA_MEMINFO_PATH";
pub const DEFAULT_OLLAMA_MODEL: &str = "llama3.2:3b";

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct OllamaStatus {
    pub opt_in: bool,
    pub model: String,
    pub raspberry_pi: bool,
    pub ram_bytes: Option<u64>,
    pub min_pi_ram_bytes: u64,
    pub probe_errors: Vec<String>,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
#[serde(rename_all = "kebab-case")]
pub enum OllamaDecision {
    Allow,
    Refuse,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct OllamaGuard {
    pub decision: OllamaDecision,
    pub reason: String,
    pub status: OllamaStatus,
}

pub fn evaluate_ollama_guard(opt_in: bool, model: &str) -> OllamaGuard {
    let mut probe_errors = Vec::new();
    let pi_model = read_optional_string(&model_path()).unwrap_or_else(|error| {
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
    ollama_guard_from_probe(opt_in, model, pi_model.as_deref(), ram_bytes, probe_errors)
}

fn ollama_guard_from_probe(
    opt_in: bool,
    model: &str,
    pi_model: Option<&str>,
    ram_bytes: Option<u64>,
    probe_errors: Vec<String>,
) -> OllamaGuard {
    let raspberry_pi = pi_model
        .map(|value| value.to_ascii_lowercase().contains("raspberry pi"))
        .unwrap_or(false);
    let status = OllamaStatus {
        opt_in,
        model: model.to_string(),
        raspberry_pi,
        ram_bytes,
        min_pi_ram_bytes: OLLAMA_PI_MIN_RAM_BYTES,
        probe_errors,
    };
    if !opt_in {
        return OllamaGuard {
            decision: OllamaDecision::Refuse,
            reason: "Ollama synthesis requires --enable-ollama".to_string(),
            status,
        };
    }
    if raspberry_pi && ram_bytes.is_none_or(|bytes| bytes < OLLAMA_PI_MIN_RAM_BYTES) {
        return OllamaGuard {
            decision: OllamaDecision::Refuse,
            reason: format!(
                "Ollama synthesis requires a 16GB-class Raspberry Pi; detected {} bytes",
                ram_bytes.unwrap_or(0)
            ),
            status,
        };
    }
    OllamaGuard {
        decision: OllamaDecision::Allow,
        reason: "Ollama guard allows synthesis".to_string(),
        status,
    }
}

fn model_path() -> PathBuf {
    env::var_os(OLLAMA_MODEL_PATH_ENV)
        .map(PathBuf::from)
        .unwrap_or_else(|| PathBuf::from("/proc/device-tree/model"))
}

fn meminfo_path() -> PathBuf {
    env::var_os(OLLAMA_MEMINFO_PATH_ENV)
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

    #[test]
    fn ollama_requires_explicit_opt_in() {
        let guard =
            ollama_guard_from_probe(false, DEFAULT_OLLAMA_MODEL, None, Some(32 * GIB), vec![]);

        assert_eq!(guard.decision, OllamaDecision::Refuse);
        assert!(guard.reason.contains("--enable-ollama"));
    }

    #[test]
    fn ollama_refuses_8gb_pi_and_allows_16gb_pi() {
        let eight_gb = ollama_guard_from_probe(
            true,
            DEFAULT_OLLAMA_MODEL,
            Some("Raspberry Pi 5 Model B Rev 1.0"),
            Some(8 * GIB),
            Vec::new(),
        );
        let sixteen_gb = ollama_guard_from_probe(
            true,
            DEFAULT_OLLAMA_MODEL,
            Some("Raspberry Pi 5 Model B Rev 1.0"),
            Some(15 * GIB),
            Vec::new(),
        );

        assert_eq!(eight_gb.decision, OllamaDecision::Refuse);
        assert_eq!(sixteen_gb.decision, OllamaDecision::Allow);
    }

    #[test]
    fn ollama_allows_opted_in_non_pi_hosts() {
        let guard = ollama_guard_from_probe(
            true,
            DEFAULT_OLLAMA_MODEL,
            Some("MacBookPro"),
            Some(8 * GIB),
            vec![],
        );

        assert_eq!(guard.decision, OllamaDecision::Allow);
    }
}
