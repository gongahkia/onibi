fn main() {
    if let Err(error) = kelp::run() {
        eprintln!("error: {error:#}");
        std::process::exit(1);
    }
}
