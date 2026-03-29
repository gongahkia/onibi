mod app;
mod cli;
mod config;
mod domain;
mod error;
mod legacy;
mod output;
mod render;
mod storage;

use anyhow::{anyhow, Result};
use clap::{error::ErrorKind, Parser};
use std::env;
use std::ffi::OsString;

pub use app::{execute, execute_with_config, Clock, FixedClock, SystemClock};
pub use cli::Cli;
pub use config::{AppConfig, JsonConfigStore, TaskSortKey};
pub use output::RunOutput;
pub use storage::{JsonFileStorage, Storage};

pub fn run() -> RunOutput {
    let args = env::args_os().collect::<Vec<_>>();
    let json_requested = detect_json_requested(&args);

    match Cli::try_parse_from(args) {
        Ok(cli) => {
            let explicit_data_dir = cli.data_dir.clone();
            let storage = match explicit_data_dir.clone() {
                Some(data_dir) => JsonFileStorage::at(data_dir),
                None => match JsonFileStorage::from_env() {
                    Ok(storage) => storage,
                    Err(error) => return format_runtime_error(error, json_requested),
                },
            };
            let colocate_config =
                explicit_data_dir.is_some() || env::var_os("KELP_DATA_DIR").is_some();
            let config_store = match JsonConfigStore::from_env_with_data_root(
                &storage.root_dir(),
                colocate_config,
            ) {
                Ok(store) => store,
                Err(error) => return format_runtime_error(error, json_requested),
            };
            let default_json_output = config_store
                .load()
                .map(|config| config.default_json_output)
                .unwrap_or(false);
            let wants_json = matches!(cli.requested_output(), Some(cli::OutputFormat::Json))
                || (cli.requested_output().is_none() && default_json_output);
            let clock = SystemClock;

            match execute_with_config(cli, &storage, &config_store, &clock) {
                Ok(output) => RunOutput::success(output),
                Err(error) => format_runtime_error(error, wants_json),
            }
        }
        Err(error) => handle_clap_error(error, json_requested),
    }
}

pub fn run_with_args<I, T, S, C>(args: I, storage: &S, clock: &C) -> Result<String>
where
    I: IntoIterator<Item = T>,
    T: Into<OsString> + Clone,
    S: Storage,
    C: Clock,
{
    let output = run_with_args_capture(args, storage, clock);
    if output.exit_code == 0 {
        Ok(output.stdout)
    } else {
        Err(anyhow!(output.stderr))
    }
}

pub fn run_with_args_capture<I, T, S, C>(args: I, storage: &S, clock: &C) -> RunOutput
where
    I: IntoIterator<Item = T>,
    T: Into<OsString> + Clone,
    S: Storage,
    C: Clock,
{
    let args = args.into_iter().map(Into::into).collect::<Vec<_>>();
    let json_requested = detect_json_requested(&args);

    match Cli::try_parse_from(args) {
        Ok(cli) => {
            let config_store = JsonConfigStore::at(storage.root_dir());
            let default_json_output = config_store
                .load()
                .map(|config| config.default_json_output)
                .unwrap_or(false);
            let wants_json = matches!(cli.requested_output(), Some(cli::OutputFormat::Json))
                || (cli.requested_output().is_none() && default_json_output);

            match execute_with_config(cli, storage, &config_store, clock) {
                Ok(output) => RunOutput::success(output),
                Err(error) => format_runtime_error(error, wants_json),
            }
        }
        Err(error) => handle_clap_error(error, json_requested),
    }
}

fn handle_clap_error(error: clap::Error, wants_json: bool) -> RunOutput {
    if matches!(
        error.kind(),
        ErrorKind::DisplayHelp | ErrorKind::DisplayVersion
    ) {
        return RunOutput::success(error.render().to_string());
    }

    format_runtime_error(error.into(), wants_json)
}

fn format_runtime_error(error: anyhow::Error, wants_json: bool) -> RunOutput {
    let report = error::classify_error(&error);
    let stderr = if wants_json {
        output::error_json(&report)
    } else {
        output::error_plain(&report)
    };

    RunOutput::failure(stderr, report.exit_code)
}

fn detect_json_requested(args: &[OsString]) -> bool {
    args.iter()
        .zip(args.iter().skip(1).map(Some).chain(std::iter::once(None)))
        .any(|(current, next)| {
            let current = current.to_string_lossy();
            current == "--json"
                || current == "--output=json"
                || (current == "--output"
                    && next
                        .map(|value| value.to_string_lossy() == "json")
                        .unwrap_or(false))
        })
}
