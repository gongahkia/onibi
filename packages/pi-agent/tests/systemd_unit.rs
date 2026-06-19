use std::collections::BTreeMap;
use std::fs;
use std::path::Path;

#[test]
fn service_unit_points_at_agent_daemon() {
    let unit = parse_unit(&unit_path());
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
fn service_unit_keeps_required_hardening_directives() {
    let unit = parse_unit(&unit_path());
    let service = unit.get("Service").expect("service section");
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
        ("DevicePolicy", "closed"),
        ("SystemCallArchitectures", "native"),
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

fn unit_path() -> String {
    format!(
        "{}/systemd/kelp-pi-agent.service",
        env!("CARGO_MANIFEST_DIR")
    )
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
