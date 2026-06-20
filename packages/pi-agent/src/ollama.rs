use std::env;
use std::fs;
use std::path::{Path, PathBuf};

use serde::Serialize;

use crate::GIB;

pub const OLLAMA_PI_MIN_RAM_BYTES: u64 = 3 * GIB;
pub const OLLAMA_PI_8GB_MIN_RAM_BYTES: u64 = 7 * GIB;
pub const OLLAMA_PI_16GB_MIN_RAM_BYTES: u64 = 14 * GIB;
pub const OLLAMA_MODEL_PATH_ENV: &str = "KELP_PI_OLLAMA_MODEL_PATH";
pub const OLLAMA_MEMINFO_PATH_ENV: &str = "KELP_PI_OLLAMA_MEMINFO_PATH";
pub const DEFAULT_OLLAMA_MODEL: &str = "qwen2.5:0.5b";

#[derive(Debug, Clone, Copy, Serialize, PartialEq, Eq)]
#[serde(rename_all = "kebab-case")]
pub enum OllamaModelTier {
    FourGb,
    EightGb,
    SixteenGb,
}

#[derive(Debug, Clone, Copy, Serialize, PartialEq, Eq)]
pub struct OllamaModelProfile {
    pub model: &'static str,
    pub tier: OllamaModelTier,
    pub min_pi_ram_bytes: u64,
    pub model_size_bytes: u64,
    pub context_tokens: u32,
    pub source: &'static str,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct OllamaModelAvailability {
    pub model: &'static str,
    pub tier: OllamaModelTier,
    pub min_pi_ram_bytes: u64,
    pub model_size_bytes: u64,
    pub context_tokens: u32,
    pub source: &'static str,
    pub default: bool,
    pub available: bool,
    pub reason: String,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct OllamaModelsReport {
    pub raspberry_pi: bool,
    pub ram_bytes: Option<u64>,
    pub default_model: &'static str,
    pub models: Vec<OllamaModelAvailability>,
    pub probe_errors: Vec<String>,
}

pub const OLLAMA_MODEL_PROFILES: &[OllamaModelProfile] = &[
    OllamaModelProfile {
        model: "qwen2.5:0.5b",
        tier: OllamaModelTier::FourGb,
        min_pi_ram_bytes: OLLAMA_PI_MIN_RAM_BYTES,
        model_size_bytes: 398_000_000,
        context_tokens: 32_000,
        source: "Ollama qwen2.5 catalog",
    },
    OllamaModelProfile {
        model: "smollm2:360m",
        tier: OllamaModelTier::FourGb,
        min_pi_ram_bytes: OLLAMA_PI_MIN_RAM_BYTES,
        model_size_bytes: 726_000_000,
        context_tokens: 8_000,
        source: "Ollama smollm2 catalog",
    },
    OllamaModelProfile {
        model: "gemma3:1b",
        tier: OllamaModelTier::FourGb,
        min_pi_ram_bytes: OLLAMA_PI_MIN_RAM_BYTES,
        model_size_bytes: 815_000_000,
        context_tokens: 32_000,
        source: "Ollama gemma3 catalog",
    },
    OllamaModelProfile {
        model: "qwen2.5:1.5b",
        tier: OllamaModelTier::FourGb,
        min_pi_ram_bytes: OLLAMA_PI_MIN_RAM_BYTES,
        model_size_bytes: 986_000_000,
        context_tokens: 32_000,
        source: "Ollama qwen2.5 catalog",
    },
    OllamaModelProfile {
        model: "llama3.2:1b",
        tier: OllamaModelTier::FourGb,
        min_pi_ram_bytes: OLLAMA_PI_MIN_RAM_BYTES,
        model_size_bytes: 1_300_000_000,
        context_tokens: 128_000,
        source: "Ollama llama3.2 catalog",
    },
    OllamaModelProfile {
        model: "smollm2:1.7b",
        tier: OllamaModelTier::EightGb,
        min_pi_ram_bytes: OLLAMA_PI_8GB_MIN_RAM_BYTES,
        model_size_bytes: 1_800_000_000,
        context_tokens: 8_000,
        source: "Ollama smollm2 catalog",
    },
    OllamaModelProfile {
        model: "qwen2.5:3b",
        tier: OllamaModelTier::EightGb,
        min_pi_ram_bytes: OLLAMA_PI_8GB_MIN_RAM_BYTES,
        model_size_bytes: 1_900_000_000,
        context_tokens: 32_000,
        source: "Ollama qwen2.5 catalog",
    },
    OllamaModelProfile {
        model: "llama3.2:3b",
        tier: OllamaModelTier::EightGb,
        min_pi_ram_bytes: OLLAMA_PI_8GB_MIN_RAM_BYTES,
        model_size_bytes: 2_000_000_000,
        context_tokens: 128_000,
        source: "Ollama llama3.2 catalog",
    },
    OllamaModelProfile {
        model: "gemma3:4b",
        tier: OllamaModelTier::SixteenGb,
        min_pi_ram_bytes: OLLAMA_PI_16GB_MIN_RAM_BYTES,
        model_size_bytes: 3_300_000_000,
        context_tokens: 128_000,
        source: "Ollama gemma3 catalog",
    },
];

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct OllamaStatus {
    pub opt_in: bool,
    pub model: String,
    pub raspberry_pi: bool,
    pub ram_bytes: Option<u64>,
    pub min_pi_ram_bytes: u64,
    pub model_profile: Option<OllamaModelProfile>,
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

pub fn ollama_model_profiles() -> &'static [OllamaModelProfile] {
    OLLAMA_MODEL_PROFILES
}

pub fn evaluate_ollama_models() -> OllamaModelsReport {
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
    let raspberry_pi = is_raspberry_pi(pi_model.as_deref());
    let models = OLLAMA_MODEL_PROFILES
        .iter()
        .map(|profile| model_availability(*profile, raspberry_pi, ram_bytes))
        .collect();
    OllamaModelsReport {
        raspberry_pi,
        ram_bytes,
        default_model: DEFAULT_OLLAMA_MODEL,
        models,
        probe_errors,
    }
}

fn ollama_guard_from_probe(
    opt_in: bool,
    model: &str,
    pi_model: Option<&str>,
    ram_bytes: Option<u64>,
    probe_errors: Vec<String>,
) -> OllamaGuard {
    let raspberry_pi = is_raspberry_pi(pi_model);
    let model_profile = find_model_profile(model);
    let min_pi_ram_bytes = model_profile
        .map(|profile| profile.min_pi_ram_bytes)
        .unwrap_or(OLLAMA_PI_16GB_MIN_RAM_BYTES);
    let status = OllamaStatus {
        opt_in,
        model: model.to_string(),
        raspberry_pi,
        ram_bytes,
        min_pi_ram_bytes,
        model_profile,
        probe_errors,
    };
    if !opt_in {
        return OllamaGuard {
            decision: OllamaDecision::Refuse,
            reason: "Ollama synthesis requires --enable-ollama".to_string(),
            status,
        };
    }
    if raspberry_pi && model_profile.is_none() {
        return OllamaGuard {
            decision: OllamaDecision::Refuse,
            reason: format!(
                "Ollama model {model} is not in the Kelp Pi catalog; run `kelp-pi-agent ollama models`"
            ),
            status,
        };
    }
    if raspberry_pi && ram_bytes.is_none_or(|bytes| bytes < min_pi_ram_bytes) {
        return OllamaGuard {
            decision: OllamaDecision::Refuse,
            reason: format!(
                "Ollama model {model} requires at least {min_pi_ram_bytes} bytes on Raspberry Pi; detected {} bytes",
                ram_bytes.unwrap_or(0),
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

fn find_model_profile(model: &str) -> Option<OllamaModelProfile> {
    OLLAMA_MODEL_PROFILES
        .iter()
        .copied()
        .find(|profile| profile.model == model)
}

fn is_raspberry_pi(model: Option<&str>) -> bool {
    model
        .map(|value| value.to_ascii_lowercase().contains("raspberry pi"))
        .unwrap_or(false)
}

fn model_availability(
    profile: OllamaModelProfile,
    raspberry_pi: bool,
    ram_bytes: Option<u64>,
) -> OllamaModelAvailability {
    let available =
        !raspberry_pi || ram_bytes.is_some_and(|bytes| bytes >= profile.min_pi_ram_bytes);
    let reason = if !raspberry_pi {
        "not a Raspberry Pi host; catalog RAM gates are Pi-specific".to_string()
    } else if let Some(bytes) = ram_bytes {
        if bytes >= profile.min_pi_ram_bytes {
            format!(
                "detected {bytes} bytes RAM, meets {}",
                profile.min_pi_ram_bytes
            )
        } else {
            format!(
                "detected {bytes} bytes RAM, needs {}",
                profile.min_pi_ram_bytes
            )
        }
    } else {
        "RAM probe unavailable".to_string()
    };
    OllamaModelAvailability {
        model: profile.model,
        tier: profile.tier,
        min_pi_ram_bytes: profile.min_pi_ram_bytes,
        model_size_bytes: profile.model_size_bytes,
        context_tokens: profile.context_tokens,
        source: profile.source,
        default: profile.model == DEFAULT_OLLAMA_MODEL,
        available,
        reason,
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
    fn ollama_allows_4gb_pi_for_default_small_model() {
        let four_gb = ollama_guard_from_probe(
            true,
            DEFAULT_OLLAMA_MODEL,
            Some("Raspberry Pi 5 Model B Rev 1.0"),
            Some(4 * GIB),
            Vec::new(),
        );

        assert_eq!(four_gb.decision, OllamaDecision::Allow);
        assert_eq!(four_gb.status.min_pi_ram_bytes, OLLAMA_PI_MIN_RAM_BYTES);
        assert_eq!(
            four_gb.status.model_profile.map(|profile| profile.model),
            Some(DEFAULT_OLLAMA_MODEL)
        );
    }

    #[test]
    fn ollama_refuses_4gb_pi_for_8gb_model() {
        let four_gb = ollama_guard_from_probe(
            true,
            "llama3.2:3b",
            Some("Raspberry Pi 5 Model B Rev 1.0"),
            Some(4 * GIB),
            Vec::new(),
        );
        let eight_gb = ollama_guard_from_probe(
            true,
            "llama3.2:3b",
            Some("Raspberry Pi 5 Model B Rev 1.0"),
            Some(8 * GIB),
            Vec::new(),
        );

        assert_eq!(four_gb.decision, OllamaDecision::Refuse);
        assert_eq!(four_gb.status.min_pi_ram_bytes, OLLAMA_PI_8GB_MIN_RAM_BYTES);
        assert_eq!(eight_gb.decision, OllamaDecision::Allow);
    }

    #[test]
    fn ollama_refuses_unlisted_models_on_pi() {
        let guard = ollama_guard_from_probe(
            true,
            "custom:70b",
            Some("Raspberry Pi 5 Model B Rev 1.0"),
            Some(16 * GIB),
            Vec::new(),
        );

        assert_eq!(guard.decision, OllamaDecision::Refuse);
        assert!(guard.reason.contains("not in the Kelp Pi catalog"));
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

    #[test]
    fn model_availability_marks_4gb_models() {
        let small = model_availability(
            find_model_profile(DEFAULT_OLLAMA_MODEL).expect("default model profile"),
            true,
            Some(4 * GIB),
        );
        let large = model_availability(
            find_model_profile("llama3.2:3b").expect("3b model profile"),
            true,
            Some(4 * GIB),
        );

        assert!(small.available);
        assert!(small.default);
        assert!(!large.available);
    }
}
