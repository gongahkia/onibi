use std::path::PathBuf;
use std::process::ExitCode;

use kelp_pi_agent::{validate_data_dir, DEFAULT_DATA_DIR, DEFAULT_QUOTAS};

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
        "check-data-dir" => check_data_dir(args.collect(), false),
        "quota-defaults" => {
            print_quota_defaults();
            Ok(())
        }
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

    if start_mode && !check_only {
        eprintln!("daemon runtime is not implemented; rerun with --check-only for preflight");
        return Err(ExitCode::from(69));
    }

    println!("data dir ok: {}", data_dir.display());
    Ok(())
}

fn print_usage() {
    eprintln!("usage: kelp-pi-agent check-data-dir [--data-dir PATH]");
    eprintln!("usage: kelp-pi-agent quota-defaults");
    eprintln!("usage: kelp-pi-agent start [--data-dir PATH] --check-only");
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
