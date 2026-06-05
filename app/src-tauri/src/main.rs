// Prevents additional console window on Windows in release, DO NOT REMOVE!!
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod adapters;
mod approval;
mod cli;
mod config;
mod headless;
mod orchestration;
mod protocol;
mod push;
mod remote;
mod secret;
mod server;
mod transport;
mod util;

#[cfg(feature = "gui")]
use app_lib::pty;
#[cfg(not(feature = "gui"))]
mod pty;

fn main() {
    let args = std::env::args().collect::<Vec<_>>();
    if args.iter().any(|arg| arg == "--headless") {
        if let Err(error) = cli::run_blocking(args) {
            eprintln!("onibi: {error:#}");
            std::process::exit(1);
        }
        return;
    }

    if cli::should_dispatch(&args) {
        if let Err(error) = cli::run_blocking(args) {
            eprintln!("onibi: {error:#}");
            std::process::exit(1);
        }
        return;
    }

    #[cfg(feature = "gui")]
    {
        use std::sync::Arc;
        app_lib::pty::set_notification_hook(Arc::new(|session_id, notice| {
            if let Some(bridge) = push::bridge() {
                tokio::spawn(push::fanout_pty_notification(
                    bridge.store.clone(),
                    bridge.vapid.clone(),
                    session_id,
                    notice,
                ));
            }
        }));
        let port = config::load()
            .map(|config| config.server_port())
            .unwrap_or(config::DEFAULT_PORT);
        server::start_background_server(port);
        app_lib::run()
    }

    #[cfg(not(feature = "gui"))]
    {
        if let Err(error) = cli::run_blocking(args) {
            eprintln!("onibi: {error:#}");
            std::process::exit(1);
        }
    }
}
