use std::collections::{BTreeMap, BTreeSet};
use std::ffi::CString;
use std::fmt::{Display, Formatter};
use std::fs;
use std::io;
use std::net::Ipv4Addr;
use std::path::Path;
use std::process::Command;

use rusqlite::Connection;
use serde::Serialize;
use serde_json::{json, Map, Value};

use crate::{
    apply_index_schema, audit_log_path, index_db_path, verify_audit_log_chain,
    AuditLogChainVerifyError,
};

#[cfg(unix)]
use std::os::unix::ffi::OsStrExt;

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct SelfcheckReport {
    pub ok: bool,
    pub data_dir: String,
    pub stale_index: bool,
    pub ap_state: Value,
    pub ssid: Value,
    pub ip: Value,
    pub isolation_rule_presence: Value,
    pub allowlist: Value,
    pub listening_ports: Value,
    pub free_disk: Value,
    pub ram: Value,
    pub battery_state: Value,
    pub cpu_temperature: Value,
    pub microsd_wear: Value,
    pub audit_log: Value,
    pub checks: Vec<SelfcheckCheck>,
    pub warnings: Vec<String>,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct SelfcheckCheck {
    pub id: String,
    pub status: SelfcheckStatus,
    pub message: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub details: Option<BTreeMap<String, Value>>,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
pub enum SelfcheckStatus {
    Pass,
    Warn,
    Fail,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SelfcheckTargetError {
    pub target: String,
    pub reason: String,
}

pub fn run_selfcheck(data_dir: &Path) -> SelfcheckReport {
    let (ap_state, ssid, ip) = network_posture_checks();
    let isolation_rule_presence = isolation_rule_check();
    let allowlist = allowlist_check();
    let listening_ports = listening_ports_check();
    let free_disk = free_disk_check(data_dir);
    let ram = ram_check();
    let battery_state = battery_state_check();
    let cpu_temperature = cpu_temperature_check();
    let microsd_wear = microsd_wear_check();
    let audit_log = audit_log_check(data_dir);
    let checks = vec![
        ap_state.clone(),
        ssid.clone(),
        ip.clone(),
        isolation_rule_presence.clone(),
        allowlist.clone(),
        listening_ports.clone(),
        free_disk.clone(),
        ram.clone(),
        battery_state.clone(),
        cpu_temperature.clone(),
        microsd_wear.clone(),
        audit_log.clone(),
        stale_index_check(data_dir),
    ];
    let ok = checks
        .iter()
        .all(|check| !matches!(check.status, SelfcheckStatus::Fail));
    let stale_index = checks
        .iter()
        .any(|check| check.id == "stale-index" && check.status == SelfcheckStatus::Warn);
    let warnings = checks
        .iter()
        .filter(|check| matches!(check.status, SelfcheckStatus::Warn))
        .map(|check| format!("{}: {}", check.id, check.message))
        .collect();

    SelfcheckReport {
        ok,
        data_dir: data_dir.display().to_string(),
        stale_index,
        ap_state: check_value(&ap_state),
        ssid: check_value(&ssid),
        ip: check_value(&ip),
        isolation_rule_presence: check_value(&isolation_rule_presence),
        allowlist: check_value(&allowlist),
        listening_ports: check_value(&listening_ports),
        free_disk: check_value(&free_disk),
        ram: check_value(&ram),
        battery_state: check_value(&battery_state),
        cpu_temperature: check_value(&cpu_temperature),
        microsd_wear: check_value(&microsd_wear),
        audit_log: check_value(&audit_log),
        checks,
        warnings,
    }
}

pub fn selfcheck_report_payload(
    check_id: &str,
    generated_at: &str,
    report: &SelfcheckReport,
) -> Map<String, Value> {
    let mut payload = Map::new();
    payload.insert("check_id".to_string(), json!(check_id));
    payload.insert("generated_at".to_string(), json!(generated_at));
    payload.insert("status".to_string(), json!(overall_status(report)));
    payload.insert(
        "checks".to_string(),
        Value::Array(report.checks.iter().map(check_payload).collect()),
    );
    payload
}

pub fn validate_selfcheck_target(
    data_dir: &Path,
    target: &str,
) -> Result<(), SelfcheckTargetError> {
    let config = load_selfcheck_target_config(data_dir);
    validate_selfcheck_target_with_config(target, &config)
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
struct SelfcheckTargetConfig {
    ap_cidrs: Vec<String>,
    allowlist: Vec<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct TargetParts {
    host: String,
    port: Option<u16>,
}

fn load_selfcheck_target_config(_data_dir: &Path) -> SelfcheckTargetConfig {
    let path = Path::new("/etc/kelp-pi/agent.json");
    let Ok(content) = fs::read_to_string(path) else {
        return SelfcheckTargetConfig::default();
    };
    let Ok(value) = serde_json::from_str::<Value>(&content) else {
        return SelfcheckTargetConfig::default();
    };
    SelfcheckTargetConfig {
        ap_cidrs: string_values(&value, &["ap-cidr", "ap_cidr", "ap-cidrs", "ap_cidrs"]),
        allowlist: string_values(&value, &["allow-outbound", "allow_outbound", "allowlist"]),
    }
}

fn validate_selfcheck_target_with_config(
    target: &str,
    config: &SelfcheckTargetConfig,
) -> Result<(), SelfcheckTargetError> {
    let parts = parse_target(target).ok_or_else(|| SelfcheckTargetError {
        target: target.to_string(),
        reason: "target is empty or malformed".to_string(),
    })?;

    if let Ok(ip) = parts.host.parse::<Ipv4Addr>() {
        if ip.octets()[0] == 127 {
            return Ok(());
        }
        if config.ap_cidrs.iter().any(|cidr| ipv4_in_cidr(ip, cidr)) {
            return Ok(());
        }
    }

    if config
        .allowlist
        .iter()
        .any(|entry| allowlist_entry_matches(&parts, entry))
    {
        return Ok(());
    }

    Err(SelfcheckTargetError {
        target: target.to_string(),
        reason: "outside 127.0.0.0/8, configured AP CIDR, and allowlist".to_string(),
    })
}

fn parse_target(target: &str) -> Option<TargetParts> {
    let trimmed = target.trim();
    if trimmed.is_empty() {
        return None;
    }
    let authority = if let Some((_, rest)) = trimmed.split_once("://") {
        rest.split(['/', '?', '#']).next().unwrap_or(rest)
    } else {
        trimmed.split(['/', '?', '#']).next().unwrap_or(trimmed)
    };
    let authority = authority.rsplit('@').next().unwrap_or(authority);
    if authority.starts_with('[') {
        return None;
    }
    let mut host = authority;
    let mut port = None;
    if authority.matches(':').count() == 1 {
        let (candidate_host, candidate_port) = authority.rsplit_once(':')?;
        if !candidate_port.is_empty() {
            if let Ok(parsed_port) = candidate_port.parse::<u16>() {
                host = candidate_host;
                port = Some(parsed_port);
            }
        }
    }
    let host = host.trim().trim_end_matches('.').to_ascii_lowercase();
    if host.is_empty() {
        return None;
    }
    Some(TargetParts { host, port })
}

fn allowlist_entry_matches(target: &TargetParts, entry: &str) -> bool {
    let Some(allowed) = parse_target(entry) else {
        return false;
    };
    allowed.host.eq_ignore_ascii_case(&target.host)
        && allowed.port.is_none_or(|port| target.port == Some(port))
}

fn ipv4_in_cidr(ip: Ipv4Addr, cidr: &str) -> bool {
    let Some((base, prefix)) = cidr.split_once('/') else {
        return ip.to_string() == cidr;
    };
    let Ok(base) = base.parse::<Ipv4Addr>() else {
        return false;
    };
    let Ok(prefix) = prefix.parse::<u32>() else {
        return false;
    };
    if prefix > 32 {
        return false;
    }
    let mask = if prefix == 0 {
        0
    } else {
        u32::MAX << (32 - prefix)
    };
    (u32::from(ip) & mask) == (u32::from(base) & mask)
}

fn string_values(value: &Value, keys: &[&str]) -> Vec<String> {
    for key in keys {
        if let Some(value) = value.get(*key) {
            return match value {
                Value::String(text) => vec![text.to_string()],
                Value::Array(values) => values
                    .iter()
                    .filter_map(Value::as_str)
                    .map(ToOwned::to_owned)
                    .collect(),
                _ => Vec::new(),
            };
        }
    }
    Vec::new()
}

impl Display for SelfcheckTargetError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        write!(formatter, "{}: {}", self.target, self.reason)
    }
}

impl std::error::Error for SelfcheckTargetError {}

fn network_posture_checks() -> (SelfcheckCheck, SelfcheckCheck, SelfcheckCheck) {
    let nmcli = run_command(
        "nmcli",
        &[
            "-t",
            "-f",
            "GENERAL.STATE,GENERAL.CONNECTION,IP4.ADDRESS",
            "device",
            "show",
            "wlan0",
        ],
    );
    let iw = run_command("iw", &["dev", "wlan0", "info"]);

    if nmcli.is_err() && iw.is_err() {
        let reason = format!(
            "nmcli: {}; iw: {}",
            nmcli.err().unwrap_or_else(|| "unavailable".to_string()),
            iw.err().unwrap_or_else(|| "unavailable".to_string())
        );
        let mut details = BTreeMap::new();
        details.insert("interface".to_string(), json!("wlan0"));
        details.insert("available".to_string(), json!(false));
        details.insert("reason".to_string(), json!(reason));
        let unavailable = warn_with_details(
            "ap-state",
            "AP state probe unavailable on this host",
            details.clone(),
        );
        return (
            unavailable,
            warn_with_details(
                "ssid",
                "SSID probe unavailable on this host",
                details.clone(),
            ),
            warn_with_details("ip", "IP probe unavailable on this host", details),
        );
    }

    let nmcli_fields = nmcli.as_deref().map(parse_nmcli_fields).unwrap_or_default();
    let iw_fields = iw.as_deref().map(parse_iw_fields).unwrap_or_default();
    let iw_type = iw_fields.get("type").cloned();
    let ap_mode = iw_type.as_deref() == Some("AP");
    let state = nmcli_fields.get("GENERAL.STATE").cloned();
    let ssid = iw_fields
        .get("ssid")
        .cloned()
        .or_else(|| nmcli_fields.get("GENERAL.CONNECTION").cloned())
        .filter(|value| !value.is_empty() && value != "--");
    let addresses: Vec<String> = nmcli_fields
        .iter()
        .filter(|(key, value)| key.starts_with("IP4.ADDRESS") && !value.is_empty())
        .map(|(_, value)| value.clone())
        .collect();

    let mut ap_details = BTreeMap::new();
    ap_details.insert("interface".to_string(), json!("wlan0"));
    ap_details.insert("state".to_string(), json!(state));
    ap_details.insert("iw_type".to_string(), json!(iw_type));
    ap_details.insert("ap_mode".to_string(), json!(ap_mode));
    let ap_check = if ap_mode {
        pass_with_details("ap-state", "wlan0 is in AP mode", ap_details)
    } else {
        fail_with_details("ap-state", "wlan0 is not in AP mode", ap_details)
    };

    let mut ssid_details = BTreeMap::new();
    ssid_details.insert("interface".to_string(), json!("wlan0"));
    ssid_details.insert("ssid".to_string(), json!(ssid));
    let ssid_check = if ssid_details.get("ssid").and_then(Value::as_str).is_some() {
        pass_with_details("ssid", "SSID probe succeeded", ssid_details)
    } else {
        fail_with_details("ssid", "SSID probe returned no value", ssid_details)
    };

    let mut ip_details = BTreeMap::new();
    ip_details.insert("interface".to_string(), json!("wlan0"));
    ip_details.insert("addresses".to_string(), json!(addresses));
    let ip_check = if ip_details
        .get("addresses")
        .and_then(Value::as_array)
        .is_some_and(|addresses| !addresses.is_empty())
    {
        pass_with_details("ip", "wlan0 has IPv4 address data", ip_details)
    } else {
        fail_with_details("ip", "wlan0 has no IPv4 address data", ip_details)
    };

    (ap_check, ssid_check, ip_check)
}

fn isolation_rule_check() -> SelfcheckCheck {
    match run_command("nft", &["list", "ruleset"]) {
        Ok(output) => {
            let present = output.contains("wlan0")
                && output.contains("drop")
                && (output.contains("iifname") || output.contains("oifname"));
            let mut details = BTreeMap::new();
            details.insert("present".to_string(), json!(present));
            if present {
                pass_with_details(
                    "isolation-rule-presence",
                    "nftables wlan0 isolation rule appears present",
                    details,
                )
            } else {
                fail_with_details(
                    "isolation-rule-presence",
                    "nftables wlan0 isolation rule was not found",
                    details,
                )
            }
        }
        Err(error) => {
            let mut details = BTreeMap::new();
            details.insert("available".to_string(), json!(false));
            details.insert("reason".to_string(), json!(error));
            warn_with_details(
                "isolation-rule-presence",
                "nftables probe unavailable on this host",
                details,
            )
        }
    }
}

fn allowlist_check() -> SelfcheckCheck {
    let path = Path::new("/etc/kelp-pi/agent.json");
    match fs::read_to_string(path) {
        Ok(content) => match serde_json::from_str::<Value>(&content) {
            Ok(value) => {
                let entries = value
                    .get("allow-outbound")
                    .or_else(|| value.get("allow_outbound"))
                    .cloned()
                    .unwrap_or_else(|| json!([]));
                let mut details = BTreeMap::new();
                details.insert("path".to_string(), json!(path.display().to_string()));
                details.insert("entries".to_string(), entries);
                pass_with_details("allowlist", "allowlist config loaded", details)
            }
            Err(error) => {
                let mut details = BTreeMap::new();
                details.insert("path".to_string(), json!(path.display().to_string()));
                details.insert("reason".to_string(), json!(error.to_string()));
                fail_with_details("allowlist", "allowlist config is invalid JSON", details)
            }
        },
        Err(error) if error.kind() == io::ErrorKind::NotFound => {
            let mut details = BTreeMap::new();
            details.insert("path".to_string(), json!(path.display().to_string()));
            details.insert("entries".to_string(), json!([]));
            details.insert("available".to_string(), json!(false));
            warn_with_details("allowlist", "allowlist config is not present", details)
        }
        Err(error) => {
            let mut details = BTreeMap::new();
            details.insert("path".to_string(), json!(path.display().to_string()));
            details.insert("reason".to_string(), json!(error.to_string()));
            fail_with_details("allowlist", "allowlist config could not be read", details)
        }
    }
}

fn listening_ports_check() -> SelfcheckCheck {
    match run_command("ss", &["-H", "-lntu"]) {
        Ok(output) => {
            let ports: Vec<String> = output
                .lines()
                .map(str::trim)
                .filter(|line| !line.is_empty())
                .map(ToOwned::to_owned)
                .collect();
            let mut details = BTreeMap::new();
            details.insert("ports".to_string(), json!(ports));
            pass_with_details("listening-ports", "listening port probe succeeded", details)
        }
        Err(error) => {
            let mut details = BTreeMap::new();
            details.insert("available".to_string(), json!(false));
            details.insert("reason".to_string(), json!(error));
            warn_with_details(
                "listening-ports",
                "listening port probe unavailable on this host",
                details,
            )
        }
    }
}

fn free_disk_check(data_dir: &Path) -> SelfcheckCheck {
    match free_disk_bytes(data_dir) {
        Ok(bytes) if bytes > 0 => {
            let mut details = BTreeMap::new();
            details.insert("bytes".to_string(), json!(bytes));
            pass_with_details("free-disk", "free disk probe succeeded", details)
        }
        Ok(_) => fail("free-disk", "free disk probe returned zero available bytes"),
        Err(error) => fail("free-disk", format!("free disk probe failed: {error}")),
    }
}

fn ram_check() -> SelfcheckCheck {
    match fs::read_to_string("/proc/meminfo") {
        Ok(content) => {
            let mut details = BTreeMap::new();
            details.insert(
                "mem_total_kib".to_string(),
                json!(parse_meminfo_kib(&content, "MemTotal")),
            );
            details.insert(
                "mem_available_kib".to_string(),
                json!(parse_meminfo_kib(&content, "MemAvailable")),
            );
            pass_with_details("ram", "RAM probe succeeded", details)
        }
        Err(error) if error.kind() == io::ErrorKind::NotFound => {
            let mut details = BTreeMap::new();
            details.insert("available".to_string(), json!(false));
            details.insert("reason".to_string(), json!("missing /proc/meminfo"));
            warn_with_details("ram", "RAM probe unavailable on this host", details)
        }
        Err(error) => fail("ram", format!("RAM probe failed: {error}")),
    }
}

fn battery_state_check() -> SelfcheckCheck {
    let power_supply = Path::new("/sys/class/power_supply");
    let Ok(entries) = fs::read_dir(power_supply) else {
        let mut details = BTreeMap::new();
        details.insert("percentage".to_string(), Value::Null);
        details.insert("status".to_string(), Value::Null);
        details.insert("available".to_string(), json!(false));
        return warn_with_details(
            "battery-state",
            "battery state unavailable on this host",
            details,
        );
    };

    for entry in entries.flatten() {
        let path = entry.path();
        let supply_type = read_trimmed(path.join("type"));
        let capacity: Option<u8> =
            read_trimmed(path.join("capacity")).and_then(|value| value.parse().ok());
        if supply_type.as_deref() == Some("Battery") || capacity.is_some() {
            let mut details = BTreeMap::new();
            details.insert("percentage".to_string(), json!(capacity));
            details.insert(
                "status".to_string(),
                json!(read_trimmed(path.join("status"))),
            );
            details.insert("source".to_string(), json!(path.display().to_string()));
            return pass_with_details("battery-state", "battery state probe succeeded", details);
        }
    }

    let mut details = BTreeMap::new();
    details.insert("percentage".to_string(), Value::Null);
    details.insert("status".to_string(), Value::Null);
    details.insert("available".to_string(), json!(false));
    warn_with_details(
        "battery-state",
        "battery state unavailable on this host",
        details,
    )
}

fn cpu_temperature_check() -> SelfcheckCheck {
    let path = Path::new("/sys/class/thermal/thermal_zone0/temp");
    match fs::read_to_string(path) {
        Ok(raw) => match raw.trim().parse::<f64>() {
            Ok(millicelsius) => {
                let mut details = BTreeMap::new();
                details.insert("celsius".to_string(), json!(millicelsius / 1000.0));
                pass_with_details(
                    "cpu-temperature",
                    "CPU temperature probe succeeded",
                    details,
                )
            }
            Err(error) => fail(
                "cpu-temperature",
                format!("CPU temperature probe returned invalid data: {error}"),
            ),
        },
        Err(error) if error.kind() == io::ErrorKind::NotFound => {
            let mut details = BTreeMap::new();
            details.insert("available".to_string(), json!(false));
            details.insert("path".to_string(), json!(path.display().to_string()));
            warn_with_details(
                "cpu-temperature",
                "CPU temperature probe unavailable on this host",
                details,
            )
        }
        Err(error) => fail(
            "cpu-temperature",
            format!("CPU temperature probe failed: {error}"),
        ),
    }
}

fn microsd_wear_check() -> SelfcheckCheck {
    let paths = [
        "/sys/block/mmcblk0/device/life_time",
        "/sys/block/mmcblk0/device/pre_eol_info",
    ];
    let mut values = BTreeMap::new();
    for path in paths {
        if let Ok(raw) = fs::read_to_string(path) {
            values.insert(path.to_string(), raw.trim().to_string());
        }
    }
    microsd_wear_check_from_values(values)
}

fn microsd_wear_check_from_values(values: BTreeMap<String, String>) -> SelfcheckCheck {
    let mut details = BTreeMap::new();
    let wear_warning = microsd_wear_warning(&values);
    details.insert("values".to_string(), json!(values));
    details.insert("wear_warning".to_string(), json!(wear_warning));
    if details
        .get("values")
        .and_then(Value::as_object)
        .is_some_and(|values| !values.is_empty())
    {
        if wear_warning {
            warn_with_details("microsd-wear", "microSD wear exceeds threshold", details)
        } else {
            pass_with_details("microsd-wear", "microSD wear probe succeeded", details)
        }
    } else {
        details.insert("available".to_string(), json!(false));
        warn_with_details(
            "microsd-wear",
            "microSD wear estimate unavailable on this host",
            details,
        )
    }
}

fn microsd_wear_warning(values: &BTreeMap<String, String>) -> bool {
    values.iter().any(|(path, raw)| {
        let readings = parse_wear_readings(raw);
        if path.ends_with("pre_eol_info") {
            readings.iter().any(|value| *value >= 0x02)
        } else if path.ends_with("life_time") {
            readings.iter().any(|value| *value >= 0x0a)
        } else {
            false
        }
    })
}

fn parse_wear_readings(raw: &str) -> Vec<u8> {
    raw.split_whitespace()
        .filter_map(|token| {
            token
                .strip_prefix("0x")
                .or_else(|| token.strip_prefix("0X"))
                .map_or_else(
                    || token.parse::<u8>().ok(),
                    |hex| u8::from_str_radix(hex, 16).ok(),
                )
        })
        .collect()
}

fn audit_log_check(data_dir: &Path) -> SelfcheckCheck {
    let path = audit_log_path(data_dir);
    match verify_audit_log_chain(data_dir, &data_dir.join("keys")) {
        Ok(result) => {
            let mut details = BTreeMap::new();
            details.insert("verified".to_string(), json!(true));
            details.insert("entries".to_string(), json!(result.entries));
            details.insert("segments".to_string(), json!(result.segments));
            details.insert("segment_entries".to_string(), json!(result.segment_entries));
            details.insert("active_entries".to_string(), json!(result.active_entries));
            details.insert("head_hash".to_string(), json!(result.head_hash));
            pass_with_details("audit-log", "audit log verifies", details)
        }
        Err(AuditLogChainVerifyError::Io(ref io_error))
            if io_error.kind() == io::ErrorKind::NotFound =>
        {
            let mut details = BTreeMap::new();
            details.insert("verified".to_string(), json!(false));
            details.insert("path".to_string(), json!(path.display().to_string()));
            details.insert("reason".to_string(), json!("audit log is not present"));
            warn_with_details("audit-log", "audit log is not present", details)
        }
        Err(error) => {
            let mut details = BTreeMap::new();
            details.insert("verified".to_string(), json!(false));
            details.insert("path".to_string(), json!(path.display().to_string()));
            details.insert("reason".to_string(), json!(error.to_string()));
            fail_with_details("audit-log", "audit log verification failed", details)
        }
    }
}

fn stale_index_check(data_dir: &Path) -> SelfcheckCheck {
    match unindexed_corpus_files(data_dir) {
        Ok(unindexed) if unindexed.is_empty() => {
            pass("stale-index", "all corpus files are indexed")
        }
        Ok(unindexed) => {
            let mut details = BTreeMap::new();
            details.insert("unindexed_sources".to_string(), json!(unindexed));
            warn_with_details(
                "stale-index",
                "corpus contains source files missing from the index",
                details,
            )
        }
        Err(error) => fail("stale-index", format!("stale-index probe failed: {error}")),
    }
}

fn unindexed_corpus_files(data_dir: &Path) -> Result<Vec<String>, String> {
    let corpus_dir = data_dir.join("corpus");
    let mut corpus_files = Vec::new();
    collect_regular_files(&corpus_dir, &corpus_dir, &mut corpus_files)?;
    let indexed = indexed_source_paths(data_dir)?;
    Ok(corpus_files
        .into_iter()
        .filter(|path| !indexed.contains(path))
        .collect())
}

fn collect_regular_files(root: &Path, current: &Path, out: &mut Vec<String>) -> Result<(), String> {
    let entries =
        fs::read_dir(current).map_err(|error| format!("{}: {error}", current.display()))?;
    for entry in entries {
        let entry = entry.map_err(|error| format!("{}: {error}", current.display()))?;
        let path = entry.path();
        let file_type = entry
            .file_type()
            .map_err(|error| format!("{}: {error}", path.display()))?;
        if file_type.is_dir() {
            collect_regular_files(root, &path, out)?;
        } else if file_type.is_file() {
            out.push(relative_source_path(root, &path)?);
        }
    }
    out.sort();
    Ok(())
}

fn indexed_source_paths(data_dir: &Path) -> Result<BTreeSet<String>, String> {
    let db_path = index_db_path(data_dir);
    let parent = db_path
        .parent()
        .ok_or_else(|| format!("index db has no parent: {}", db_path.display()))?;
    fs::create_dir_all(parent).map_err(|error| format!("{}: {error}", parent.display()))?;
    let connection =
        Connection::open(&db_path).map_err(|error| format!("{}: {error}", db_path.display()))?;
    apply_index_schema(&connection).map_err(|error| format!("apply index schema: {error}"))?;
    let mut statement = connection
        .prepare("SELECT path FROM source_files")
        .map_err(|error| format!("query source_files: {error}"))?;
    let rows = statement
        .query_map([], |row| row.get::<_, String>(0))
        .map_err(|error| format!("query source_files: {error}"))?;
    rows.collect::<Result<BTreeSet<_>, _>>()
        .map_err(|error| format!("read source_files: {error}"))
}

fn relative_source_path(root: &Path, path: &Path) -> Result<String, String> {
    let relative = path
        .strip_prefix(root)
        .map_err(|error| format!("{}: {error}", path.display()))?;
    Ok(relative.to_string_lossy().replace('\\', "/"))
}

fn check_payload(check: &SelfcheckCheck) -> Value {
    let mut payload = Map::new();
    payload.insert("name".to_string(), json!(check.id));
    payload.insert("status".to_string(), json!(status_name(&check.status)));
    payload.insert("message".to_string(), json!(check.message));
    if let Some(details) = &check.details {
        payload.insert(
            "value".to_string(),
            serde_json::to_value(details).unwrap_or(Value::Null),
        );
    }
    Value::Object(payload)
}

fn overall_status(report: &SelfcheckReport) -> &'static str {
    if report
        .checks
        .iter()
        .any(|check| matches!(check.status, SelfcheckStatus::Fail))
    {
        "fail"
    } else if report
        .checks
        .iter()
        .any(|check| matches!(check.status, SelfcheckStatus::Warn))
    {
        "warn"
    } else {
        "pass"
    }
}

fn status_name(status: &SelfcheckStatus) -> &'static str {
    match status {
        SelfcheckStatus::Pass => "pass",
        SelfcheckStatus::Warn => "warn",
        SelfcheckStatus::Fail => "fail",
    }
}

fn check_value(check: &SelfcheckCheck) -> Value {
    check
        .details
        .as_ref()
        .map(|details| serde_json::to_value(details).unwrap_or(Value::Null))
        .unwrap_or(Value::Null)
}

fn parse_nmcli_fields(output: &str) -> BTreeMap<String, String> {
    output
        .lines()
        .filter_map(|line| line.split_once(':'))
        .map(|(key, value)| (key.to_string(), value.to_string()))
        .collect()
}

fn parse_iw_fields(output: &str) -> BTreeMap<String, String> {
    output
        .lines()
        .filter_map(|line| {
            let trimmed = line.trim();
            trimmed
                .split_once(' ')
                .map(|(key, value)| (key.to_string(), value.trim().to_string()))
        })
        .collect()
}

fn parse_meminfo_kib(content: &str, key: &str) -> Option<u64> {
    content.lines().find_map(|line| {
        let (name, rest) = line.split_once(':')?;
        if name != key {
            return None;
        }
        rest.split_whitespace().next()?.parse::<u64>().ok()
    })
}

fn read_trimmed(path: impl AsRef<Path>) -> Option<String> {
    fs::read_to_string(path)
        .ok()
        .map(|value| value.trim().to_string())
        .filter(|value| !value.is_empty())
}

fn run_command(program: &str, args: &[&str]) -> Result<String, String> {
    let output = Command::new(program)
        .args(args)
        .output()
        .map_err(|error| error.to_string())?;
    if output.status.success() {
        Ok(String::from_utf8_lossy(&output.stdout).to_string())
    } else {
        let stderr = String::from_utf8_lossy(&output.stderr).trim().to_string();
        Err(if stderr.is_empty() {
            format!("{program} exited with {}", output.status)
        } else {
            stderr
        })
    }
}

#[cfg(unix)]
fn free_disk_bytes(path: &Path) -> io::Result<u64> {
    let raw_path = CString::new(path.as_os_str().as_bytes())
        .map_err(|_| io::Error::new(io::ErrorKind::InvalidInput, "path contains NUL byte"))?;
    let mut stat = std::mem::MaybeUninit::<libc::statvfs>::uninit();
    let result = unsafe { libc::statvfs(raw_path.as_ptr(), stat.as_mut_ptr()) };
    if result != 0 {
        return Err(io::Error::last_os_error());
    }
    let stat = unsafe { stat.assume_init() };
    Ok(u64::from(stat.f_bavail).saturating_mul(stat.f_frsize))
}

#[cfg(not(unix))]
fn free_disk_bytes(_path: &Path) -> io::Result<u64> {
    Err(io::Error::new(
        io::ErrorKind::Unsupported,
        "statvfs is unavailable on this platform",
    ))
}

fn pass(id: &str, message: impl Into<String>) -> SelfcheckCheck {
    SelfcheckCheck {
        id: id.to_string(),
        status: SelfcheckStatus::Pass,
        message: message.into(),
        details: None,
    }
}

fn pass_with_details(
    id: &str,
    message: impl Into<String>,
    details: BTreeMap<String, Value>,
) -> SelfcheckCheck {
    SelfcheckCheck {
        id: id.to_string(),
        status: SelfcheckStatus::Pass,
        message: message.into(),
        details: Some(details),
    }
}

fn warn_with_details(
    id: &str,
    message: impl Into<String>,
    details: BTreeMap<String, Value>,
) -> SelfcheckCheck {
    SelfcheckCheck {
        id: id.to_string(),
        status: SelfcheckStatus::Warn,
        message: message.into(),
        details: Some(details),
    }
}

fn fail(id: &str, message: impl Into<String>) -> SelfcheckCheck {
    SelfcheckCheck {
        id: id.to_string(),
        status: SelfcheckStatus::Fail,
        message: message.into(),
        details: None,
    }
}

fn fail_with_details(
    id: &str,
    message: impl Into<String>,
    details: BTreeMap<String, Value>,
) -> SelfcheckCheck {
    SelfcheckCheck {
        id: id.to_string(),
        status: SelfcheckStatus::Fail,
        message: message.into(),
        details: Some(details),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::REQUIRED_DATA_DIRS;
    use rusqlite::params;
    use std::path::PathBuf;
    use std::time::{SystemTime, UNIX_EPOCH};

    #[test]
    fn selfcheck_warns_when_corpus_file_has_never_been_ingested() {
        let root = temp_root("stale");
        create_layout(&root);
        fs::write(root.join("corpus").join("guide.txt"), "admin login").expect("write corpus");

        let report = run_selfcheck(&root);

        assert!(report.ok);
        assert!(report.stale_index);
        assert!(report
            .warnings
            .iter()
            .any(|warning| warning.contains("stale-index")));
        assert_eq!(
            check_by_id(&report, "stale-index")
                .details
                .as_ref()
                .expect("details")
                .get("unindexed_sources"),
            Some(&json!(["guide.txt"]))
        );
        fs::remove_dir_all(root).ok();
    }

    #[test]
    fn selfcheck_passes_when_corpus_file_is_indexed() {
        let root = temp_root("fresh");
        create_layout(&root);
        fs::write(root.join("corpus").join("guide.txt"), "admin login").expect("write corpus");
        let connection = Connection::open(index_db_path(&root)).expect("open index");
        apply_index_schema(&connection).expect("schema");
        connection
            .execute(
                "INSERT INTO source_files (path, content_hash, mtime_unix_nanos, size_bytes, chunk_count, ingested_at) VALUES (?1, ?2, ?3, ?4, ?5, ?6)",
                params!["guide.txt", "hash", 1_i64, 11_i64, 1_i64, "2026-06-19T00:00:00Z"],
            )
            .expect("insert source");

        let report = run_selfcheck(&root);

        assert!(report.ok);
        assert!(!report.stale_index);
        assert_eq!(
            check_by_id(&report, "stale-index").status,
            SelfcheckStatus::Pass
        );
        fs::remove_dir_all(root).ok();
    }

    #[test]
    fn selfcheck_report_contains_required_posture_fields() {
        let root = temp_root("posture-fields");
        create_layout(&root);

        let report = run_selfcheck(&root);
        let ids: BTreeSet<_> = report
            .checks
            .iter()
            .map(|check| check.id.as_str())
            .collect();

        for id in [
            "ap-state",
            "ssid",
            "ip",
            "isolation-rule-presence",
            "allowlist",
            "listening-ports",
            "free-disk",
            "ram",
            "battery-state",
            "cpu-temperature",
            "microsd-wear",
            "audit-log",
        ] {
            assert!(ids.contains(id), "missing {id}");
        }
        assert!(report.free_disk.get("bytes").is_some());
        assert!(report.battery_state.get("percentage").is_some());
        fs::remove_dir_all(root).ok();
    }

    #[test]
    fn selfcheck_payload_matches_wire_schema_shape() {
        let root = temp_root("payload");
        create_layout(&root);
        let report = run_selfcheck(&root);

        let payload = selfcheck_report_payload("selfcheck-01", "2026-06-19T00:00:00Z", &report);

        assert_eq!(payload.get("check_id"), Some(&json!("selfcheck-01")));
        assert_eq!(
            payload.get("generated_at"),
            Some(&json!("2026-06-19T00:00:00Z"))
        );
        assert!(matches!(
            payload.get("status").and_then(Value::as_str),
            Some("pass" | "warn" | "fail")
        ));
        assert!(payload
            .get("checks")
            .and_then(Value::as_array)
            .is_some_and(|checks| !checks.is_empty()));
        fs::remove_dir_all(root).ok();
    }

    #[test]
    fn selfcheck_target_validation_allows_loopback() {
        let config = SelfcheckTargetConfig::default();

        validate_selfcheck_target_with_config("127.9.8.7", &config).expect("loopback allowed");
        validate_selfcheck_target_with_config("http://127.0.0.1:8080/health", &config)
            .expect("loopback URL allowed");
    }

    #[test]
    fn selfcheck_target_validation_allows_ap_cidr_and_allowlist() {
        let config = SelfcheckTargetConfig {
            ap_cidrs: vec!["10.42.0.0/24".to_string()],
            allowlist: vec!["cp.example.test:443".to_string()],
        };

        validate_selfcheck_target_with_config("10.42.0.9", &config).expect("AP CIDR allowed");
        validate_selfcheck_target_with_config("https://cp.example.test:443/status", &config)
            .expect("allowlist host:port allowed");
    }

    #[test]
    fn selfcheck_target_validation_rejects_public_ip() {
        let config = SelfcheckTargetConfig::default();

        let error = validate_selfcheck_target_with_config("8.8.8.8", &config)
            .expect_err("public IP rejected");

        assert_eq!(error.target, "8.8.8.8");
        assert!(error.reason.contains("outside 127.0.0.0/8"));
    }

    #[test]
    fn microsd_wear_warns_when_threshold_exceeded() {
        let mut values = BTreeMap::new();
        values.insert(
            "/sys/block/mmcblk0/device/life_time".to_string(),
            "0x01 0x0a".to_string(),
        );

        let check = microsd_wear_check_from_values(values);

        assert_eq!(check.status, SelfcheckStatus::Warn);
        assert_eq!(
            check
                .details
                .as_ref()
                .and_then(|details| details.get("wear_warning")),
            Some(&json!(true))
        );
    }

    #[test]
    fn microsd_wear_passes_when_under_threshold() {
        let mut values = BTreeMap::new();
        values.insert(
            "/sys/block/mmcblk0/device/pre_eol_info".to_string(),
            "0x01".to_string(),
        );

        let check = microsd_wear_check_from_values(values);

        assert_eq!(check.status, SelfcheckStatus::Pass);
    }

    fn create_layout(root: &Path) {
        fs::create_dir_all(root).expect("create root");
        for name in REQUIRED_DATA_DIRS {
            fs::create_dir_all(root.join(name)).expect("create child");
        }
    }

    fn temp_root(name: &str) -> PathBuf {
        let nonce = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("time after epoch")
            .as_nanos();
        std::env::temp_dir().join(format!(
            "kelp-pi-selfcheck-{name}-{}-{nonce}",
            std::process::id()
        ))
    }

    fn check_by_id<'a>(report: &'a SelfcheckReport, id: &str) -> &'a SelfcheckCheck {
        report
            .checks
            .iter()
            .find(|check| check.id == id)
            .unwrap_or_else(|| panic!("missing {id}"))
    }
}
