use std::path::PathBuf;
use std::process::ExitCode;

use kelp_pi_agent::{
    apply_index_schema, audit_log_path, index_db_path, init_audit_tracing,
    install_panic_audit_hook, load_or_generate_identity_key, run_doctor, search_chunks,
    validate_data_dir, verify_audit_log, DEFAULT_DATA_DIR, DEFAULT_KEY_LABEL, DEFAULT_QUOTAS,
};
use rusqlite::Connection;

fn main() -> ExitCode {
    match run() {
        Ok(()) => ExitCode::SUCCESS,
        Err(code) => code,
    }
}

fn run() -> Result<(), ExitCode> {
    let mut args = std::env::args().skip(1);
    let Some(command) = args.next() else {
        print_usage();
        return Ok(());
    };

    match command.as_str() {
        "ask" => ask_command(args.collect()),
        "check-data-dir" => check_data_dir(args.collect(), false),
        "doctor" => doctor_command(args.collect()),
        "keygen" => keygen_command(args.collect()),
        "quota-defaults" => {
            print_quota_defaults();
            Ok(())
        }
        "verify-audit-log" => verify_audit_log_command(args.collect()),
        "start" => check_data_dir(args.collect(), true),
        "-h" | "--help" | "help" => {
            print_usage();
            Ok(())
        }
        _ => {
            eprintln!("unknown command: {command}");
            print_usage();
            Err(ExitCode::from(64))
        }
    }
}

fn ask_command(args: Vec<String>) -> Result<(), ExitCode> {
    let mut data_dir = PathBuf::from(DEFAULT_DATA_DIR);
    let mut db_path = None;
    let mut top_k = 5_usize;
    let mut query_parts = Vec::new();
    let mut index = 0;

    while index < args.len() {
        match args[index].as_str() {
            "--data-dir" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--data-dir requires a value");
                    return Err(ExitCode::from(64));
                };
                data_dir = PathBuf::from(value);
                index += 2;
            }
            "--db" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--db requires a value");
                    return Err(ExitCode::from(64));
                };
                db_path = Some(PathBuf::from(value));
                index += 2;
            }
            "--top-k" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--top-k requires a value");
                    return Err(ExitCode::from(64));
                };
                let Ok(parsed) = value.parse::<usize>() else {
                    eprintln!("--top-k must be a positive integer");
                    return Err(ExitCode::from(64));
                };
                top_k = parsed.max(1);
                index += 2;
            }
            other => {
                query_parts.push(other.to_string());
                index += 1;
            }
        }
    }

    if query_parts.is_empty() {
        eprintln!("ask requires a query");
        return Err(ExitCode::from(64));
    }

    let query = query_parts.join(" ");
    let db_path = db_path.unwrap_or_else(|| index_db_path(&data_dir));
    match Connection::open(&db_path).and_then(|connection| {
        apply_index_schema(&connection)?;
        search_chunks(&connection, &query, top_k)
    }) {
        Ok(results) => {
            println!(
                "{}",
                serde_json::to_string(&serde_json::json!({
                    "query": query,
                    "top_k": top_k,
                    "results": results
                }))
                .expect("serialize ask response")
            );
            Ok(())
        }
        Err(error) => {
            eprintln!("ask failed: {error}");
            Err(ExitCode::from(65))
        }
    }
}

fn doctor_command(args: Vec<String>) -> Result<(), ExitCode> {
    let mut data_dir = PathBuf::from(DEFAULT_DATA_DIR);
    let mut index = 0;

    while index < args.len() {
        match args[index].as_str() {
            "--data-dir" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--data-dir requires a value");
                    return Err(ExitCode::from(64));
                };
                data_dir = PathBuf::from(value);
                index += 2;
            }
            other => {
                eprintln!("unknown argument: {other}");
                return Err(ExitCode::from(64));
            }
        }
    }

    let report = run_doctor(&data_dir);
    println!(
        "{}",
        serde_json::to_string_pretty(&report).expect("serialize doctor report")
    );
    if report.ok {
        Ok(())
    } else {
        Err(ExitCode::from(78))
    }
}

fn keygen_command(args: Vec<String>) -> Result<(), ExitCode> {
    let mut data_dir = PathBuf::from(DEFAULT_DATA_DIR);
    let mut key_dir = None;
    let mut label = DEFAULT_KEY_LABEL.to_string();
    let mut index = 0;

    while index < args.len() {
        match args[index].as_str() {
            "--data-dir" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--data-dir requires a value");
                    return Err(ExitCode::from(64));
                };
                data_dir = PathBuf::from(value);
                index += 2;
            }
            "--key-dir" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--key-dir requires a value");
                    return Err(ExitCode::from(64));
                };
                key_dir = Some(PathBuf::from(value));
                index += 2;
            }
            "--label" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--label requires a value");
                    return Err(ExitCode::from(64));
                };
                label = value.to_string();
                index += 2;
            }
            other => {
                eprintln!("unknown argument: {other}");
                return Err(ExitCode::from(64));
            }
        }
    }

    let key_dir = key_dir.unwrap_or_else(|| data_dir.join("keys"));
    match load_or_generate_identity_key(&key_dir, &label) {
        Ok(identity) => {
            println!(
                "{}",
                serde_json::to_string(&identity.metadata).expect("serialize key metadata")
            );
            Ok(())
        }
        Err(error) => {
            eprintln!("keygen failed: {error}");
            Err(ExitCode::from(74))
        }
    }
}

fn verify_audit_log_command(args: Vec<String>) -> Result<(), ExitCode> {
    let mut data_dir = PathBuf::from(DEFAULT_DATA_DIR);
    let mut log_file = None;
    let mut index = 0;

    while index < args.len() {
        match args[index].as_str() {
            "--data-dir" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--data-dir requires a value");
                    return Err(ExitCode::from(64));
                };
                data_dir = PathBuf::from(value);
                index += 2;
            }
            "--log-file" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--log-file requires a value");
                    return Err(ExitCode::from(64));
                };
                log_file = Some(PathBuf::from(value));
                index += 2;
            }
            other => {
                eprintln!("unknown argument: {other}");
                return Err(ExitCode::from(64));
            }
        }
    }

    let path = log_file.unwrap_or_else(|| audit_log_path(&data_dir));
    match verify_audit_log(&path) {
        Ok(result) => {
            println!(
                "audit log ok: entries={} head_hash={}",
                result.entries, result.head_hash
            );
            Ok(())
        }
        Err(error) => {
            eprintln!("audit log invalid: {error}");
            Err(ExitCode::from(65))
        }
    }
}

fn check_data_dir(args: Vec<String>, start_mode: bool) -> Result<(), ExitCode> {
    let mut data_dir = PathBuf::from(DEFAULT_DATA_DIR);
    let mut check_only = false;
    let mut index = 0;

    while index < args.len() {
        match args[index].as_str() {
            "--data-dir" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--data-dir requires a value");
                    return Err(ExitCode::from(64));
                };
                data_dir = PathBuf::from(value);
                index += 2;
            }
            "--check-only" => {
                check_only = true;
                index += 1;
            }
            other => {
                eprintln!("unknown argument: {other}");
                return Err(ExitCode::from(64));
            }
        }
    }

    if let Err(issues) = validate_data_dir(&data_dir) {
        for issue in issues {
            eprintln!("{issue}");
        }
        return Err(ExitCode::from(78));
    }

    let _panic_hook = if start_mode {
        if let Err(error) = init_audit_tracing(&data_dir) {
            eprintln!("audit tracing init failed: {error}");
            return Err(ExitCode::from(78));
        }
        let panic_hook = install_panic_audit_hook();
        tracing::info!(
            event = "agent.preflight.ok",
            msg = "data dir preflight passed",
            msg_id = "agent-preflight-ok",
            data_dir = %data_dir.display()
        );
        Some(panic_hook)
    } else {
        None
    };

    if start_mode && !check_only {
        tracing::error!(
            event = "agent.start.unimplemented",
            msg = "daemon runtime is not implemented",
            msg_id = "agent-start-unimplemented"
        );
        eprintln!("daemon runtime is not implemented; rerun with --check-only for preflight");
        return Err(ExitCode::from(69));
    }

    println!("data dir ok: {}", data_dir.display());
    Ok(())
}

fn print_usage() {
    eprintln!("usage: kelp-pi-agent ask QUERY [--data-dir PATH] [--db PATH] [--top-k N]");
    eprintln!("usage: kelp-pi-agent check-data-dir [--data-dir PATH]");
    eprintln!("usage: kelp-pi-agent doctor [--data-dir PATH]");
    eprintln!("usage: kelp-pi-agent keygen [--data-dir PATH] [--key-dir PATH] [--label LABEL]");
    eprintln!("usage: kelp-pi-agent quota-defaults");
    eprintln!("usage: kelp-pi-agent start [--data-dir PATH] --check-only");
    eprintln!("usage: kelp-pi-agent verify-audit-log [--data-dir PATH] [--log-file PATH]");
}

fn print_quota_defaults() {
    println!(
        "{{\"corpus_bytes\":{},\"uploads_bytes\":{},\"index_bytes\":{},\"audit_log_bytes\":{}}}",
        DEFAULT_QUOTAS.corpus_bytes,
        DEFAULT_QUOTAS.uploads_bytes,
        DEFAULT_QUOTAS.index_bytes,
        DEFAULT_QUOTAS.audit_log_bytes
    );
}
