use std::{env, fs, path::PathBuf};

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let manifest_directory = PathBuf::from(env::var("CARGO_MANIFEST_DIR")?);
    let proto_directory = manifest_directory.join("proto");
    let generated_directory = manifest_directory.join("src/generated");
    fs::create_dir_all(&generated_directory)?;
    let protoc = protoc_bin_vendored::protoc_bin_path()?;
    let mut config = tonic_prost_build::Config::new();
    config.protoc_executable(protoc);
    tonic_prost_build::configure()
        .build_client(true)
        .build_server(true)
        .out_dir(generated_directory)
        .compile_with_config(
            config,
            &[proto_directory.join("arachne/relay/v1/relay.proto")],
            &[proto_directory],
        )?;
    println!("cargo::rerun-if-changed=proto/arachne/relay/v1/relay.proto");
    Ok(())
}
