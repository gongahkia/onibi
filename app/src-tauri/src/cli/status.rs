use crate::{adapters, approval::store::now_millis, secret, server};
use anyhow::{Context, Result};
use rusqlite::Connection;
use serde_json::Value;
use std::path::Path;

#[derive(Default)]
struct DbSummary {
    pending_approvals: i64,
    resolved_24h: i64,
    sessions_24h: i64,
    devices: Vec<DeviceSummary>,
}

struct DeviceSummary {
    label: String,
    last_seen: Option<i64>,
}

pub async fn run(port: u16) -> Result<()> {
    let config_dir = secret::config_dir()?;
    let db_path = secret::db_path()?;
    let daemon_running = super::healthz(port);
    let db_summary = db_summary(&db_path)?;

    let mode = if cfg!(feature = "gui") {
        "gui-capable build"
    } else {
        "headless-only build"
    };
    println!("Onibi {} ({mode})", env!("CARGO_PKG_VERSION"));
    println!(
        "Daemon:    {}",
        if daemon_running { "running" } else { "stopped" }
    );
    println!("Config:    {}", config_dir.display());
    println!("Database:  {}", db_path.display());
    println!("Sessions:  {} active in last 24h", db_summary.sessions_24h);
    println!(
        "Approvals: {} pending, {} resolved in last 24h",
        db_summary.pending_approvals, db_summary.resolved_24h
    );
    println!();

    println!("Transports:");
    if daemon_running {
        if let Ok(raw) = super::authed_http(port, "GET", "/v1/status", None) {
            print_status_json(&raw);
        } else {
            print_daemon_transports(port);
        }
    } else {
        let state = server::AppState::from_config(port)?;
        for snapshot in state.transports.status_snapshot().await {
            print_transport_json(&serde_json::to_value(snapshot)?);
        }
    }
    println!();

    println!("Paired devices:");
    if db_summary.devices.is_empty() {
        println!("  none");
    } else {
        for device in db_summary.devices {
            let seen = device
                .last_seen
                .map(format_age)
                .unwrap_or_else(|| "never".to_string());
            println!("  {} (last seen {seen})", device.label);
        }
    }
    println!();

    println!("Adapters installed:");
    let installed = adapters::list()
        .into_iter()
        .filter(|adapter| adapter.installed)
        .collect::<Vec<_>>();
    if installed.is_empty() {
        println!("  none");
    } else {
        for adapter in installed {
            println!("  {} ({})", adapter.name, adapter.support);
        }
    }

    Ok(())
}

fn print_status_json(raw: &str) {
    let Ok(value) = serde_json::from_str::<Value>(raw) else {
        print_daemon_transports_fallback();
        return;
    };
    let Some(transports) = value.get("transports").and_then(Value::as_array) else {
        print_daemon_transports_fallback();
        return;
    };
    if transports.is_empty() {
        println!("  none");
        return;
    }
    for transport in transports {
        print_transport_json(transport);
    }
}

fn print_daemon_transports(port: u16) {
    match super::authed_http(port, "GET", "/v1/transport/status", None)
        .and_then(|raw| serde_json::from_str::<Vec<Value>>(&raw).context("parse transport status"))
    {
        Ok(transports) if transports.is_empty() => println!("  none"),
        Ok(transports) => {
            for transport in transports {
                print_transport_json(&transport);
            }
        }
        Err(error) => println!("  unavailable: {error:#}"),
    }
}

fn print_daemon_transports_fallback() {
    println!("  unavailable: daemon did not return transport details");
}

fn print_transport_json(transport: &Value) {
    let name = transport
        .get("name")
        .and_then(Value::as_str)
        .unwrap_or("unknown");
    let enabled = transport
        .get("enabled")
        .and_then(Value::as_bool)
        .unwrap_or(false);
    let url = transport.get("url").and_then(Value::as_str);
    let message = transport
        .get("status")
        .and_then(|status| status.get("message"))
        .and_then(Value::as_str)
        .or_else(|| {
            transport
                .get("status")
                .and_then(|status| status.get("state"))
                .and_then(Value::as_str)
        });

    match (enabled, url, message) {
        (true, Some(url), _) => println!("  ok {name:<18} {url}"),
        (true, None, _) => println!("  ok {name:<18} running"),
        (false, _, Some(message)) => println!("  -- {name:<18} {message}"),
        (false, _, None) => println!("  -- {name:<18} not running"),
    }
}

fn db_summary(path: &Path) -> Result<DbSummary> {
    if !path.exists() {
        return Ok(DbSummary::default());
    }

    let conn = Connection::open(path).with_context(|| format!("open {}", path.display()))?;
    let cutoff = now_millis() - 24 * 60 * 60 * 1000;
    Ok(DbSummary {
        pending_approvals: count(
            &conn,
            "SELECT COUNT(*) FROM approvals WHERE decided_at IS NULL",
            &[],
        ),
        resolved_24h: count(
            &conn,
            "SELECT COUNT(*) FROM approvals WHERE decided_at >= ?1",
            &[&cutoff],
        ),
        sessions_24h: count(
            &conn,
            "SELECT COUNT(DISTINCT session_id) FROM run_events WHERE ts >= ?1",
            &[&cutoff],
        ),
        devices: devices(&conn),
    })
}

fn count(conn: &Connection, sql: &str, params: &[&dyn rusqlite::ToSql]) -> i64 {
    conn.query_row(sql, params, |row| row.get::<_, i64>(0))
        .unwrap_or(0)
}

fn devices(conn: &Connection) -> Vec<DeviceSummary> {
    let Ok(mut stmt) = conn.prepare(
        "SELECT COALESCE(NULLIF(label, ''), device_id), last_seen
         FROM devices
         ORDER BY COALESCE(last_seen, created_at) DESC
         LIMIT 8",
    ) else {
        return Vec::new();
    };
    let Ok(rows) = stmt.query_map([], |row| {
        Ok(DeviceSummary {
            label: row.get(0)?,
            last_seen: row.get(1)?,
        })
    }) else {
        return Vec::new();
    };
    rows.filter_map(std::result::Result::ok).collect()
}

fn format_age(seen_at: i64) -> String {
    let elapsed = now_millis().saturating_sub(seen_at);
    let seconds = elapsed / 1000;
    if seconds < 60 {
        return format!("{seconds}s ago");
    }
    let minutes = seconds / 60;
    if minutes < 60 {
        return format!("{minutes}m ago");
    }
    let hours = minutes / 60;
    if hours < 48 {
        return format!("{hours}h ago");
    }
    format!("{}d ago", hours / 24)
}
