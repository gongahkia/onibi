mod app;
mod cli;
mod domain;
mod legacy;
mod render;
mod storage;

use anyhow::Result;
use clap::Parser;

pub use app::{execute, Clock, FixedClock, SystemClock};
pub use cli::Cli;
pub use storage::{JsonFileStorage, Storage};

pub fn run() -> Result<()> {
    let cli = Cli::parse();
    let storage = JsonFileStorage::from_env()?;
    let clock = SystemClock;
    let output = execute(cli, &storage, &clock)?;
    if !output.is_empty() {
        println!("{output}");
    }

    Ok(())
}

pub fn run_with_args<I, T, S, C>(args: I, storage: &S, clock: &C) -> Result<String>
where
    I: IntoIterator<Item = T>,
    T: Into<std::ffi::OsString> + Clone,
    S: Storage,
    C: Clock,
{
    let cli = Cli::parse_from(args);
    execute(cli, storage, clock)
}
