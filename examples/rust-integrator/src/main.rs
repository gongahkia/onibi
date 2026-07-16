use std::error::Error;

use yeokcham_core::OsKeystore;
use yeokcham_rust_integrator_example::config_from_environment;
use yeokcham_sdk::{SdkClient, SdkConfig, SdkIdentityManager};

#[cfg(target_os = "linux")]
use yeokcham_core::LinuxKeystore;
#[cfg(target_os = "macos")]
use yeokcham_core::MacOsKeystore;
#[cfg(target_os = "windows")]
use yeokcham_core::WindowsKeystore;

#[cfg(target_os = "linux")]
#[tokio::main]
async fn main() -> Result<(), Box<dyn Error>> {
    let config = config_from_environment()?;
    run(config, LinuxKeystore::new()?).await
}

#[cfg(target_os = "macos")]
#[tokio::main]
async fn main() -> Result<(), Box<dyn Error>> {
    let config = config_from_environment()?;
    run(config, MacOsKeystore::new()).await
}

#[cfg(target_os = "windows")]
#[tokio::main]
async fn main() -> Result<(), Box<dyn Error>> {
    let config = config_from_environment()?;
    run(config, WindowsKeystore::new()?).await
}

#[cfg(not(any(target_os = "linux", target_os = "macos", target_os = "windows")))]
compile_error!("the Rust integrator example requires a supported platform keystore");

async fn run<K: OsKeystore>(config: SdkConfig, keystore: K) -> Result<(), Box<dyn Error>> {
    let client = SdkClient::start_async(&config).await?;
    let mut identities = SdkIdentityManager::new(keystore);
    let identity = identities.create_or_load()?;
    println!("SDK identity {:?}", identity.initialization());
    client.shutdown_async().await?;
    Ok(())
}
