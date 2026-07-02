use std::collections::BTreeMap;
use std::fs;
use std::path::Path;

#[test]
fn service_unit_points_at_agent_daemon() {
    let unit = parse_unit(&unit_path("kelp-pi-agent.service"));
    let service = unit.get("Service").expect("service section");

    assert_eq!(service.get("Type").map(String::as_str), Some("simple"));
    assert_eq!(service.get("User").map(String::as_str), Some("kelp-pi"));
    assert_eq!(service.get("Group").map(String::as_str), Some("kelp-pi"));
    assert_eq!(
        service.get("ExecStart").map(String::as_str),
        Some("/usr/local/bin/kelp-pi-agent start --data-dir /var/lib/kelp-pi")
    );
    assert_eq!(
        service.get("StateDirectory").map(String::as_str),
        Some("kelp-pi")
    );
    assert_eq!(
        service.get("ReadWritePaths").map(String::as_str),
        Some("/var/lib/kelp-pi")
    );
}

#[test]
fn zig_service_unit_points_at_kelp_runtime() {
    let unit = parse_unit(&unit_path("kelp-pi.service"));
    let service = unit.get("Service").expect("service section");

    assert_eq!(service.get("Type").map(String::as_str), Some("oneshot"));
    assert_eq!(service.get("User").map(String::as_str), Some("kelp-pi"));
    assert_eq!(service.get("Group").map(String::as_str), Some("kelp-pi"));
    assert_eq!(
        service.get("ExecStart").map(String::as_str),
        Some("/opt/kelp-pi/run-kelp-pi.sh doctor --data-dir /var/lib/kelp-pi --policy policies/appsec-agent-baseline.toml --models models/manifest.toml")
    );
    assert_eq!(
        service.get("StateDirectory").map(String::as_str),
        Some("kelp-pi")
    );
    assert_eq!(
        service.get("ReadWritePaths").map(String::as_str),
        Some("/var/lib/kelp-pi")
    );
    assert_eq!(
        service.get("ReadOnlyPaths").map(String::as_str),
        Some("/etc/kelp-pi /opt/kelp-pi")
    );
}

#[test]
fn service_units_keep_required_hardening_directives() {
    for unit in ["kelp-pi-agent.service", "kelp-pi.service"] {
        let unit = parse_unit(unit_path(unit));
        assert_required_hardening(unit.get("Service").expect("service section"));
    }
}

#[test]
fn installer_stages_agent_and_zig_units() {
    let script = fs::read_to_string(format!(
        "{}/scripts/install-systemd.sh",
        env!("CARGO_MANIFEST_DIR")
    ))
    .expect("read installer");

    assert!(script.contains("systemd/kelp-pi-agent.service"));
    assert!(script.contains("systemd/kelp-pi.service"));
    assert!(script.contains("etc/systemd/system/kelp-pi-agent.service"));
    assert!(script.contains("etc/systemd/system/kelp-pi.service"));
}

#[test]
fn ci_gates_agent_and_zig_units_below_security_threshold() {
    let workflow = fs::read_to_string(format!(
        "{}/../../.github/workflows/ci.yml",
        env!("CARGO_MANIFEST_DIR")
    ))
    .expect("read ci workflow");

    assert!(workflow.contains("packages/pi-agent/systemd/kelp-pi-agent.service"));
    assert!(workflow.contains("packages/pi-agent/systemd/kelp-pi.service"));
    assert!(workflow.contains("systemd-analyze verify"));
    assert!(workflow.contains("systemd-analyze security --offline=yes --threshold=2.99 --no-pager"));
}

fn assert_required_hardening(service: &BTreeMap<String, String>) {
    let required = [
        ("ProtectSystem", "strict"),
        ("ProtectHome", "true"),
        ("PrivateTmp", "true"),
        ("NoNewPrivileges", "true"),
        ("PrivateDevices", "true"),
        ("ProtectKernelTunables", "true"),
        ("ProtectKernelModules", "true"),
        ("RestrictSUIDSGID", "true"),
        ("RestrictRealtime", "true"),
        ("LockPersonality", "true"),
        ("MemoryDenyWriteExecute", "true"),
        ("KeyringMode", "private"),
        ("RemoveIPC", "true"),
        ("ProtectProc", "invisible"),
        ("ProcSubset", "pid"),
        ("DevicePolicy", "closed"),
        ("SystemCallArchitectures", "native"),
        ("SystemCallFilter", "@system-service"),
        ("RestrictAddressFamilies", "AF_UNIX AF_INET AF_INET6"),
    ];

    for (key, value) in required {
        assert_eq!(service.get(key).map(String::as_str), Some(value), "{key}");
    }
    assert_eq!(
        service.get("CapabilityBoundingSet").map(String::as_str),
        Some("")
    );
}

#[test]
fn sysusers_provisions_agent_and_scanner_users() {
    let records = parse_records(&format!(
        "{}/systemd/kelp-pi-agent.sysusers.conf",
        env!("CARGO_MANIFEST_DIR")
    ));

    assert!(records.contains(&vec![
        "u!".to_string(),
        "kelp-pi".to_string(),
        "-".to_string(),
        "Kelp Pi Agent".to_string(),
        "/var/lib/kelp-pi".to_string(),
        "/usr/sbin/nologin".to_string(),
    ]));
    assert!(records.contains(&vec![
        "u!".to_string(),
        "kelp-pi-scanner".to_string(),
        "-".to_string(),
        "Kelp Pi Scanner Sandbox".to_string(),
        "/var/lib/kelp-pi".to_string(),
        "/usr/sbin/nologin".to_string(),
    ]));
    assert!(records.contains(&vec![
        "m".to_string(),
        "kelp-pi-scanner".to_string(),
        "kelp-pi".to_string(),
    ]));
}

#[test]
fn tmpfiles_provisions_agent_directories() {
    let records = parse_records(&format!(
        "{}/systemd/kelp-pi-agent.tmpfiles.conf",
        env!("CARGO_MANIFEST_DIR")
    ));
    let expected = [
        "/var/lib/kelp-pi",
        "/var/lib/kelp-pi/corpus",
        "/var/lib/kelp-pi/evidence",
        "/var/lib/kelp-pi/evidence/uploads",
        "/var/lib/kelp-pi/bundles",
        "/var/lib/kelp-pi/index",
        "/var/lib/kelp-pi/audit",
        "/var/lib/kelp-pi/keys",
        "/var/lib/kelp-pi/policy",
        "/var/lib/kelp-pi/scope",
        "/var/lib/kelp-pi/scanners",
        "/etc/kelp-pi",
    ];

    for path in expected {
        assert!(
            records.iter().any(|record| record
                == &vec![
                    "d".to_string(),
                    path.to_string(),
                    "0750".to_string(),
                    "kelp-pi".to_string(),
                    "kelp-pi".to_string(),
                    "-".to_string(),
                ]),
            "{path}"
        );
    }
}

fn unit_path(name: &str) -> String {
    format!("{}/systemd/{name}", env!("CARGO_MANIFEST_DIR"))
}

fn parse_records(path: impl AsRef<Path>) -> Vec<Vec<String>> {
    fs::read_to_string(path)
        .expect("read records")
        .lines()
        .filter_map(|line| {
            let line = line.trim();
            if line.is_empty() || line.starts_with('#') {
                None
            } else {
                Some(split_record(line))
            }
        })
        .collect()
}

fn split_record(line: &str) -> Vec<String> {
    let mut fields = Vec::new();
    let mut current = String::new();
    let mut quoted = false;
    for character in line.chars() {
        match character {
            '"' => quoted = !quoted,
            ' ' | '\t' if !quoted => {
                if !current.is_empty() {
                    fields.push(std::mem::take(&mut current));
                }
            }
            value => current.push(value),
        }
    }
    if !current.is_empty() {
        fields.push(current);
    }
    assert!(!quoted, "unterminated quoted record: {line}");
    fields
}

fn parse_unit(path: impl AsRef<Path>) -> BTreeMap<String, BTreeMap<String, String>> {
    let contents = fs::read_to_string(path).expect("read unit");
    let mut sections = BTreeMap::<String, BTreeMap<String, String>>::new();
    let mut current = None::<String>;
    for line in contents.lines() {
        let line = line.trim();
        if line.is_empty() || line.starts_with('#') {
            continue;
        }
        if let Some(section) = line.strip_prefix('[').and_then(|v| v.strip_suffix(']')) {
            sections.entry(section.to_string()).or_default();
            current = Some(section.to_string());
            continue;
        }
        let Some((key, value)) = line.split_once('=') else {
            panic!("invalid unit line: {line}");
        };
        let section = current.as_ref().expect("section before key");
        sections
            .entry(section.clone())
            .or_default()
            .insert(key.to_string(), value.to_string());
    }
    sections
}
