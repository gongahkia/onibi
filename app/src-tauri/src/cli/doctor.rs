use crate::secret;
use anyhow::Result;
use std::{fs::OpenOptions, net::TcpListener, path::Path, process::Command};

#[derive(Clone, Copy, PartialEq, Eq)]
enum Level {
    Ok,
    Warn,
    Fail,
}

struct Check {
    level: Level,
    name: String,
    detail: String,
    hint: Option<String>,
}

pub async fn run(port: u16) -> Result<()> {
    let mut checks = vec![check_token(), check_database(), check_port(port)];
    checks.extend(check_adapters());

    println!("Onibi doctor {}", env!("CARGO_PKG_VERSION"));
    println!();
    for check in &checks {
        println!("{} {:<22} {}", label(check.level), check.name, check.detail);
        if let Some(hint) = &check.hint {
            println!("    hint: {hint}");
        }
    }
    println!();

    let ok = checks
        .iter()
        .filter(|check| check.level == Level::Ok)
        .count();
    let warn = checks
        .iter()
        .filter(|check| check.level == Level::Warn)
        .count();
    let fail = checks
        .iter()
        .filter(|check| check.level == Level::Fail)
        .count();
    println!("Summary: {ok} ok, {warn} warning, {fail} failed");
    println!("Support: paste this whole output into the GitHub issue.");
    Ok(())
}

fn check_token() -> Check {
    if let Ok(entry) = keyring::Entry::new("onibi", "approval-bearer-token") {
        if entry
            .get_password()
            .is_ok_and(|token| !token.trim().is_empty())
        {
            return Check {
                level: Level::Ok,
                name: "token".to_string(),
                detail: "keychain entry present".to_string(),
                hint: None,
            };
        }
    }

    match secret::token_path() {
        Ok(path) if readable_nonempty(&path) => Check {
            level: Level::Warn,
            name: "token".to_string(),
            detail: format!("file fallback at {}", path.display()),
            hint: Some(
                "run `onibi token rotate` after Keychain or Secret Service is available"
                    .to_string(),
            ),
        },
        Ok(path) => Check {
            level: Level::Fail,
            name: "token".to_string(),
            detail: "no keychain token or fallback token file".to_string(),
            hint: Some(format!("run `onibi setup` to create {}", path.display())),
        },
        Err(error) => Check {
            level: Level::Fail,
            name: "token".to_string(),
            detail: error.to_string(),
            hint: Some("set ONIBI_CONFIG_DIR or fix the user home directory".to_string()),
        },
    }
}

fn check_database() -> Check {
    let path = match secret::db_path() {
        Ok(path) => path,
        Err(error) => {
            return Check {
                level: Level::Fail,
                name: "sqlite".to_string(),
                detail: error.to_string(),
                hint: Some("set ONIBI_CONFIG_DIR or fix the user home directory".to_string()),
            };
        }
    };

    if let Some(parent) = path.parent() {
        if !parent.exists() {
            return Check {
                level: Level::Fail,
                name: "sqlite".to_string(),
                detail: format!("config directory missing at {}", parent.display()),
                hint: Some("run `onibi setup` once before starting the daemon".to_string()),
            };
        }
    }

    match OpenOptions::new()
        .create(true)
        .append(true)
        .open(&path)
        .and_then(|_| rusqlite::Connection::open(&path).map_err(std::io::Error::other))
    {
        Ok(conn) => match conn.query_row("PRAGMA user_version", [], |_| Ok(())) {
            Ok(()) => Check {
                level: Level::Ok,
                name: "sqlite".to_string(),
                detail: format!("writable at {}", path.display()),
                hint: None,
            },
            Err(error) => Check {
                level: Level::Fail,
                name: "sqlite".to_string(),
                detail: error.to_string(),
                hint: Some("remove a corrupt DB only after backing it up".to_string()),
            },
        },
        Err(error) => Check {
            level: Level::Fail,
            name: "sqlite".to_string(),
            detail: format!("cannot write {}: {error}", path.display()),
            hint: Some("check directory permissions or set ONIBI_CONFIG_DIR".to_string()),
        },
    }
}

fn check_port(port: u16) -> Check {
    match TcpListener::bind(("127.0.0.1", port)) {
        Ok(_) => Check {
            level: Level::Ok,
            name: "port".to_string(),
            detail: format!("127.0.0.1:{port} is available"),
            hint: None,
        },
        Err(_) if super::healthz(port) => Check {
            level: Level::Warn,
            name: "port".to_string(),
            detail: format!("127.0.0.1:{port} is already used by Onibi"),
            hint: Some("this is expected while the desktop app or daemon is running".to_string()),
        },
        Err(error) => Check {
            level: Level::Fail,
            name: "port".to_string(),
            detail: format!("127.0.0.1:{port} unavailable: {error}"),
            hint: Some(
                "stop the conflicting process or run Onibi with `--port <port>`".to_string(),
            ),
        },
    }
}

fn check_adapters() -> Vec<Check> {
    [
        AdapterProbe {
            name: "claude-code",
            binary: "claude",
            min_version: Some((2, 0, 10)),
            hint: "install or upgrade Claude Code; minimum for edit-then-approve is 2.0.10",
        },
        AdapterProbe {
            name: "codex",
            binary: "codex",
            min_version: None,
            hint: "install Codex CLI if you want Bash approval interception",
        },
        AdapterProbe {
            name: "opencode",
            binary: "opencode",
            min_version: None,
            hint: "install opencode for mirror-only sessions",
        },
        AdapterProbe {
            name: "gemini",
            binary: "gemini",
            min_version: None,
            hint: "install Gemini CLI for shell sessions",
        },
        AdapterProbe {
            name: "aider",
            binary: "aider",
            min_version: None,
            hint: "install Aider for shell sessions",
        },
        AdapterProbe {
            name: "cursor",
            binary: "cursor-agent",
            min_version: None,
            hint: "install Cursor's agent CLI or configure a custom command",
        },
        AdapterProbe {
            name: "goose",
            binary: "goose",
            min_version: None,
            hint: "install Goose for shell sessions",
        },
    ]
    .into_iter()
    .map(check_adapter)
    .collect()
}

struct AdapterProbe {
    name: &'static str,
    binary: &'static str,
    min_version: Option<(u64, u64, u64)>,
    hint: &'static str,
}

fn check_adapter(probe: AdapterProbe) -> Check {
    let path = match which::which(probe.binary) {
        Ok(path) => path,
        Err(_) => {
            return Check {
                level: Level::Warn,
                name: format!("adapter:{}", probe.name),
                detail: format!("{} not found on PATH", probe.binary),
                hint: Some(probe.hint.to_string()),
            };
        }
    };

    let version = command_version(&path).unwrap_or_else(|| "version unknown".to_string());
    if let Some(minimum) = probe.min_version {
        if let Some(found) = first_semver(&version) {
            if found < minimum {
                return Check {
                    level: Level::Fail,
                    name: format!("adapter:{}", probe.name),
                    detail: format!("{} at {} reports {version}", probe.binary, path.display()),
                    hint: Some(probe.hint.to_string()),
                };
            }
        }
    }

    Check {
        level: Level::Ok,
        name: format!("adapter:{}", probe.name),
        detail: format!("{} at {} ({version})", probe.binary, path.display()),
        hint: None,
    }
}

fn command_version(path: &Path) -> Option<String> {
    for args in [["--version"].as_slice(), ["version"].as_slice()] {
        let output = Command::new(path).args(args).output().ok()?;
        if output.status.success() {
            let raw = if output.stdout.is_empty() {
                String::from_utf8_lossy(&output.stderr).to_string()
            } else {
                String::from_utf8_lossy(&output.stdout).to_string()
            };
            let raw = raw.trim();
            if !raw.is_empty() {
                return Some(raw.lines().next().unwrap_or(raw).to_string());
            }
        }
    }
    None
}

fn first_semver(raw: &str) -> Option<(u64, u64, u64)> {
    raw.split(|ch: char| !(ch.is_ascii_digit() || ch == '.'))
        .filter(|part| part.matches('.').count() >= 2)
        .find_map(|part| {
            let mut pieces = part.split('.');
            Some((
                pieces.next()?.parse().ok()?,
                pieces.next()?.parse().ok()?,
                pieces.next()?.parse().ok()?,
            ))
        })
}

fn readable_nonempty(path: &Path) -> bool {
    std::fs::read_to_string(path).is_ok_and(|raw| !raw.trim().is_empty())
}

fn label(level: Level) -> &'static str {
    match level {
        Level::Ok => "[ok]",
        Level::Warn => "[warn]",
        Level::Fail => "[fail]",
    }
}
