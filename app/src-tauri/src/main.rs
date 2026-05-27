// Prevents additional console window on Windows in release, DO NOT REMOVE!!
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod adapters;
mod approval;
mod cli;
mod headless;
mod protocol;
mod push;
mod secret;
mod server;
mod transport;

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
        server::start_background_server(17893);
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
