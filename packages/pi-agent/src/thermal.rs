use std::env;
use std::fs;
use std::path::{Path, PathBuf};
use std::process::Command;

use serde::Serialize;

pub const DEFAULT_SCAN_THERMAL_MAX_CELSIUS: f64 = 80.0;
pub const THERMAL_TEMP_PATH_ENV: &str = "KELP_PI_THERMAL_TEMP_PATH";
pub const THROTTLED_PATH_ENV: &str = "KELP_PI_THROTTLED_PATH";

#[derive(Debug, Clone, Serialize, PartialEq)]
pub struct ThermalStatus {
    pub celsius: Option<f64>,
    pub throttled_flags: Option<u32>,
    pub thermal_throttled: bool,
    pub max_celsius: f64,
    pub probe_errors: Vec<String>,
}

#[derive(Debug, Clone, Serialize, PartialEq)]
#[serde(rename_all = "kebab-case")]
pub enum ThermalScanDecision {
    Allow,
    Refuse,
}

#[derive(Debug, Clone, Serialize, PartialEq)]
pub struct ThermalScanGuard {
    pub decision: ThermalScanDecision,
    pub reason: String,
    pub status: ThermalStatus,
}

pub fn evaluate_scan_thermal_guard() -> ThermalScanGuard {
    evaluate_scan_thermal_guard_with_limit(DEFAULT_SCAN_THERMAL_MAX_CELSIUS)
}

pub fn evaluate_scan_thermal_guard_with_limit(max_celsius: f64) -> ThermalScanGuard {
    let status = thermal_status(max_celsius);
    thermal_guard_from_status(status)
}

fn thermal_status(max_celsius: f64) -> ThermalStatus {
    let mut probe_errors = Vec::new();
    let celsius = read_temperature_celsius(&temperature_path()).map_or_else(
        |error| {
            probe_errors.push(error);
            None
        },
        Some,
    );
    let throttled_flags = read_throttled_flags().map_or_else(
        |error| {
            probe_errors.push(error);
            None
        },
        Some,
    );
    let thermal_throttled = celsius.is_some_and(|value| value > max_celsius)
        || throttled_flags.is_some_and(current_thermal_throttle_bits_set);
    ThermalStatus {
        celsius,
        throttled_flags,
        thermal_throttled,
        max_celsius,
        probe_errors,
    }
}

fn thermal_guard_from_status(status: ThermalStatus) -> ThermalScanGuard {
    if status.thermal_throttled {
        let reason = match (status.celsius, status.throttled_flags) {
            (Some(celsius), Some(flags)) => format!(
                "thermal guard refused scan: {celsius:.1}C, throttled=0x{flags:x}, max={:.1}C",
                status.max_celsius
            ),
            (Some(celsius), None) => format!(
                "thermal guard refused scan: {celsius:.1}C, max={:.1}C",
                status.max_celsius
            ),
            (None, Some(flags)) => {
                format!("thermal guard refused scan: throttled=0x{flags:x}")
            }
            (None, None) => "thermal guard refused scan".to_string(),
        };
        ThermalScanGuard {
            decision: ThermalScanDecision::Refuse,
            reason,
            status,
        }
    } else {
        ThermalScanGuard {
            decision: ThermalScanDecision::Allow,
            reason: "thermal guard allows scan".to_string(),
            status,
        }
    }
}

fn temperature_path() -> PathBuf {
    env::var_os(THERMAL_TEMP_PATH_ENV)
        .map(PathBuf::from)
        .unwrap_or_else(|| PathBuf::from("/sys/class/thermal/thermal_zone0/temp"))
}

fn read_temperature_celsius(path: &Path) -> Result<f64, String> {
    let raw = fs::read_to_string(path).map_err(|error| {
        if error.kind() == std::io::ErrorKind::NotFound {
            format!("temperature probe unavailable: {}", path.display())
        } else {
            format!("temperature probe failed: {error}")
        }
    })?;
    let millicelsius = raw
        .trim()
        .parse::<f64>()
        .map_err(|error| format!("temperature probe invalid: {error}"))?;
    Ok(millicelsius / 1000.0)
}

fn read_throttled_flags() -> Result<u32, String> {
    if let Some(path) = env::var_os(THROTTLED_PATH_ENV).map(PathBuf::from) {
        let raw = fs::read_to_string(&path)
            .map_err(|error| format!("throttle probe failed: {}: {error}", path.display()))?;
        return parse_throttled_flags(&raw);
    }
    let output = Command::new("vcgencmd")
        .arg("get_throttled")
        .output()
        .map_err(|error| format!("throttle probe unavailable: {error}"))?;
    if !output.status.success() {
        return Err(format!("throttle probe exited with {}", output.status));
    }
    parse_throttled_flags(&String::from_utf8_lossy(&output.stdout))
}

fn parse_throttled_flags(raw: &str) -> Result<u32, String> {
    let value = raw
        .trim()
        .strip_prefix("throttled=")
        .unwrap_or_else(|| raw.trim())
        .trim();
    let value = value.strip_prefix("0x").unwrap_or(value);
    u32::from_str_radix(value, 16).map_err(|error| format!("throttle probe invalid: {error}"))
}

fn current_thermal_throttle_bits_set(flags: u32) -> bool {
    flags & 0x0c != 0
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::{SystemTime, UNIX_EPOCH};

    #[test]
    fn thermal_guard_refuses_above_temperature_limit() {
        let status = ThermalStatus {
            celsius: Some(81.0),
            throttled_flags: Some(0),
            thermal_throttled: true,
            max_celsius: 80.0,
            probe_errors: Vec::new(),
        };

        let guard = thermal_guard_from_status(status);

        assert_eq!(guard.decision, ThermalScanDecision::Refuse);
        assert!(guard.reason.contains("81.0C"));
    }

    #[test]
    fn thermal_guard_refuses_current_throttle_bits() {
        assert!(current_thermal_throttle_bits_set(0x4));
        assert!(current_thermal_throttle_bits_set(0x8));
        assert!(!current_thermal_throttle_bits_set(0x10000));
    }

    #[test]
    fn thermal_probe_parses_files() {
        let root = temp_root("thermal-paths");
        fs::create_dir_all(&root).expect("root");
        let temp_path = root.join("temp");
        let throttled_path = root.join("throttled");
        fs::write(&temp_path, "79000\n").expect("temp");
        fs::write(&throttled_path, "throttled=0x0\n").expect("throttled");

        let celsius = read_temperature_celsius(&temp_path).expect("read temp");
        let flags =
            parse_throttled_flags(&fs::read_to_string(&throttled_path).expect("read throttled"))
                .expect("parse throttled");

        fs::remove_dir_all(root).ok();
        assert_eq!(celsius, 79.0);
        assert_eq!(flags, 0);
    }

    fn temp_root(name: &str) -> PathBuf {
        let nonce = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("clock")
            .as_nanos();
        std::env::temp_dir().join(format!("kelp-pi-agent-{name}-{nonce}"))
    }
}
