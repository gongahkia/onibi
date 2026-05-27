// Prevents additional console window on Windows in release, DO NOT REMOVE!!
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod adapters;
mod approval;
mod cli;
mod protocol;
mod secret;
mod server;
mod transport;

fn main() {
    let args = std::env::args().collect::<Vec<_>>();
    if cli::should_dispatch(&args) {
        if let Err(error) = cli::run_blocking(args) {
            eprintln!("onibi: {error:#}");
            std::process::exit(1);
        }
        return;
    }

    server::start_background_server(17893);
    app_lib::run()
}
