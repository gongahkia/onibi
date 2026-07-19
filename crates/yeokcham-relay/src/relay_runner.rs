use std::{
    fs::{self, File, OpenOptions},
    future::Future,
    io::{self, Read, Write},
    net::{Shutdown, SocketAddr, TcpStream},
    path::{Path, PathBuf},
    time::Duration,
};

use yeokcham_core::{RELAY_IDENTITY_SERIALIZED_BYTES, RelayPublicKey, RelaySigningKeypair};
use zeroize::Zeroize;

use crate::{
    RelayHealthEndpoint, RelayHealthEndpointError, RelayMetricsEndpoint, RelayMetricsEndpointError,
    RelayServer, RelayServerError, SelfHostedRelayConfig, SelfHostedRelayConfigError,
};

pub const DEFAULT_RELAY_HEALTH_ADDRESS: &str = "127.0.0.1:8080";
pub const DEFAULT_RELAY_METRICS_ADDRESS: &str = "127.0.0.1:8081";
pub const MAX_RELAY_GRACEFUL_SHUTDOWN_TIMEOUT: Duration = Duration::from_secs(300);
const HEALTHCHECK_TIMEOUT: Duration = Duration::from_secs(2);
const MAX_HEALTHCHECK_RESPONSE_BYTES: usize = 1024;

pub struct RelayRuntimeConfig {
    config_path: PathBuf,
    identity_path: PathBuf,
    health: RelayHealthEndpoint,
    metrics: RelayMetricsEndpoint,
    graceful_shutdown_timeout: Duration,
}

impl RelayRuntimeConfig {
    pub fn new(
        config_path: PathBuf,
        identity_path: PathBuf,
        health_address: SocketAddr,
        metrics_address: SocketAddr,
        graceful_shutdown_timeout: Duration,
    ) -> Result<Self, RelayRuntimeConfigError> {
        if !config_path.is_absolute() {
            return Err(RelayRuntimeConfigError::RelativeConfigPath);
        }
        if !identity_path.is_absolute() {
            return Err(RelayRuntimeConfigError::RelativeIdentityPath);
        }
        if graceful_shutdown_timeout.is_zero() {
            return Err(RelayRuntimeConfigError::ZeroGracefulShutdownTimeout);
        }
        if graceful_shutdown_timeout > MAX_RELAY_GRACEFUL_SHUTDOWN_TIMEOUT {
            return Err(RelayRuntimeConfigError::ExcessiveGracefulShutdownTimeout);
        }
        Ok(Self {
            config_path,
            identity_path,
            health: RelayHealthEndpoint::new(health_address)
                .map_err(RelayRuntimeConfigError::HealthEndpoint)?,
            metrics: RelayMetricsEndpoint::new(metrics_address)
                .map_err(RelayRuntimeConfigError::MetricsEndpoint)?,
            graceful_shutdown_timeout,
        })
    }

    #[must_use]
    pub fn config_path(&self) -> &Path {
        &self.config_path
    }

    #[must_use]
    pub fn identity_path(&self) -> &Path {
        &self.identity_path
    }

    #[must_use]
    pub const fn health_address(&self) -> SocketAddr {
        self.health.address()
    }

    #[must_use]
    pub const fn metrics_address(&self) -> SocketAddr {
        self.metrics.address()
    }

    pub async fn bind(&self) -> Result<RelayServer, RelayRuntimeError> {
        let config = SelfHostedRelayConfig::load(&self.config_path)
            .map_err(RelayRuntimeError::ConfigurationFile)?;
        let identity =
            load_relay_identity(&self.identity_path).map_err(RelayRuntimeError::Identity)?;
        RelayServer::bind_with_health_and_metrics(&config, identity, self.health, self.metrics)
            .await
            .map_err(RelayRuntimeError::Server)
    }

    pub async fn serve_until<F>(self, shutdown: F) -> Result<(), RelayRuntimeError>
    where
        F: Future<Output = ()>,
    {
        self.bind()
            .await?
            .serve_until_with_drain_timeout(shutdown, self.graceful_shutdown_timeout)
            .await
            .map_err(RelayRuntimeError::Server)
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum RelayRuntimeConfigError {
    #[error("relay runtime config path must be absolute")]
    RelativeConfigPath,
    #[error("relay runtime identity path must be absolute")]
    RelativeIdentityPath,
    #[error("relay runtime graceful shutdown timeout must be nonzero")]
    ZeroGracefulShutdownTimeout,
    #[error("relay runtime graceful shutdown timeout exceeds the configured limit")]
    ExcessiveGracefulShutdownTimeout,
    #[error("relay runtime health endpoint is invalid")]
    HealthEndpoint(#[source] RelayHealthEndpointError),
    #[error("relay runtime metrics endpoint is invalid")]
    MetricsEndpoint(#[source] RelayMetricsEndpointError),
}

#[derive(Debug, thiserror::Error)]
pub enum RelayRuntimeError {
    #[error("relay runtime configuration file is invalid")]
    ConfigurationFile(#[source] SelfHostedRelayConfigError),
    #[error("relay runtime identity is invalid")]
    Identity(#[source] RelayIdentityFileError),
    #[error("relay runtime server could not start or stop")]
    Server(#[source] RelayServerError),
}

#[derive(Debug, thiserror::Error)]
pub enum RelayIdentityFileError {
    #[error("relay identity file could not be read")]
    Read(#[source] io::Error),
    #[error("relay identity file is not a regular file")]
    NotRegularFile,
    #[error("relay identity file must be mounted read-only")]
    Writable,
    #[error("relay identity file has an invalid length")]
    InvalidLength,
    #[error("relay identity file is invalid")]
    Invalid(#[source] yeokcham_core::RelayIdentitySerializationError),
}

#[derive(Debug, thiserror::Error)]
pub enum RelayIdentityFileGenerationError {
    #[error("relay identity output path must be absolute")]
    RelativePath,
    #[error("relay identity could not be generated")]
    Randomness(#[source] yeokcham_core::RelayIdentityKeyError),
    #[error("relay identity output file could not be created")]
    Create(#[source] io::Error),
    #[error("relay identity output file could not be written")]
    Write(#[source] io::Error),
    #[error("relay identity output file could not be synchronized")]
    Synchronize(#[source] io::Error),
    #[error("relay identity output file could not be made read-only")]
    ReadOnly(#[source] io::Error),
}

pub fn generate_relay_identity_file(
    path: &Path,
) -> Result<RelayPublicKey, RelayIdentityFileGenerationError> {
    if !path.is_absolute() {
        return Err(RelayIdentityFileGenerationError::RelativePath);
    }
    let identity =
        RelaySigningKeypair::generate().map_err(RelayIdentityFileGenerationError::Randomness)?;
    let mut encoded = identity.serialize();
    let mut file = match create_identity_file(path) {
        Ok(file) => file,
        Err(error) => {
            encoded.zeroize();
            return Err(RelayIdentityFileGenerationError::Create(error));
        }
    };
    let result = (|| {
        file.write_all(encoded.as_ref())
            .map_err(RelayIdentityFileGenerationError::Write)?;
        file.sync_all()
            .map_err(RelayIdentityFileGenerationError::Synchronize)?;
        #[cfg(not(unix))]
        ensure_generated_identity_read_only(path)?;
        Ok(identity.public_key())
    })();
    encoded.zeroize();
    if result.is_err() {
        let _ = fs::remove_file(path);
    }
    result
}

pub fn check_relay_health(address: SocketAddr) -> Result<(), RelayHealthcheckError> {
    if !address.ip().is_loopback() || address.port() == 0 {
        return Err(RelayHealthcheckError::InvalidAddress);
    }
    let mut stream = TcpStream::connect_timeout(&address, HEALTHCHECK_TIMEOUT)
        .map_err(RelayHealthcheckError::Connect)?;
    stream
        .set_read_timeout(Some(HEALTHCHECK_TIMEOUT))
        .map_err(RelayHealthcheckError::ReadTimeout)?;
    stream
        .set_write_timeout(Some(HEALTHCHECK_TIMEOUT))
        .map_err(RelayHealthcheckError::WriteTimeout)?;
    stream
        .write_all(b"GET /readyz HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")
        .map_err(RelayHealthcheckError::Write)?;
    stream
        .shutdown(Shutdown::Write)
        .map_err(RelayHealthcheckError::Shutdown)?;
    let mut response = Vec::with_capacity(MAX_HEALTHCHECK_RESPONSE_BYTES + 1);
    stream
        .take((MAX_HEALTHCHECK_RESPONSE_BYTES + 1) as u64)
        .read_to_end(&mut response)
        .map_err(RelayHealthcheckError::Read)?;
    if response.len() > MAX_HEALTHCHECK_RESPONSE_BYTES {
        return Err(RelayHealthcheckError::ResponseTooLarge);
    }
    if !response.starts_with(b"HTTP/1.1 200 OK\r\n") {
        return Err(RelayHealthcheckError::Unhealthy);
    }
    Ok(())
}

#[derive(Debug, thiserror::Error)]
pub enum RelayHealthcheckError {
    #[error("relay healthcheck address must be loopback with a nonzero port")]
    InvalidAddress,
    #[error("relay healthcheck could not connect")]
    Connect(#[source] io::Error),
    #[error("relay healthcheck could not set the read timeout")]
    ReadTimeout(#[source] io::Error),
    #[error("relay healthcheck could not set the write timeout")]
    WriteTimeout(#[source] io::Error),
    #[error("relay healthcheck request could not be written")]
    Write(#[source] io::Error),
    #[error("relay healthcheck request could not be finalized")]
    Shutdown(#[source] io::Error),
    #[error("relay healthcheck response could not be read")]
    Read(#[source] io::Error),
    #[error("relay healthcheck response exceeds the configured limit")]
    ResponseTooLarge,
    #[error("relay healthcheck did not receive a healthy response")]
    Unhealthy,
}

fn load_relay_identity(path: &Path) -> Result<RelaySigningKeypair, RelayIdentityFileError> {
    let file = File::open(path).map_err(RelayIdentityFileError::Read)?;
    let metadata = file.metadata().map_err(RelayIdentityFileError::Read)?;
    if !metadata.file_type().is_file() {
        return Err(RelayIdentityFileError::NotRegularFile);
    }
    ensure_read_only_identity_file(&metadata)?;
    let mut encoded = Vec::with_capacity(RELAY_IDENTITY_SERIALIZED_BYTES + 1);
    file.take((RELAY_IDENTITY_SERIALIZED_BYTES + 1) as u64)
        .read_to_end(&mut encoded)
        .map_err(RelayIdentityFileError::Read)?;
    if encoded.len() != RELAY_IDENTITY_SERIALIZED_BYTES {
        encoded.zeroize();
        return Err(RelayIdentityFileError::InvalidLength);
    }
    let identity =
        RelaySigningKeypair::deserialize(&encoded).map_err(RelayIdentityFileError::Invalid);
    encoded.zeroize();
    identity
}

fn create_identity_file(path: &Path) -> io::Result<File> {
    let mut options = OpenOptions::new();
    options.write(true).create_new(true);
    #[cfg(unix)]
    {
        use std::os::unix::fs::OpenOptionsExt;

        options.mode(0o400);
    }
    options.open(path)
}

#[cfg(not(unix))]
fn ensure_generated_identity_read_only(
    path: &Path,
) -> Result<(), RelayIdentityFileGenerationError> {
    let mut permissions = fs::metadata(path)
        .map_err(RelayIdentityFileGenerationError::ReadOnly)?
        .permissions();
    permissions.set_readonly(true);
    fs::set_permissions(path, permissions).map_err(RelayIdentityFileGenerationError::ReadOnly)
}

#[cfg(unix)]
fn ensure_read_only_identity_file(metadata: &fs::Metadata) -> Result<(), RelayIdentityFileError> {
    use std::os::unix::fs::MetadataExt;

    if metadata.mode() & 0o222 != 0 {
        return Err(RelayIdentityFileError::Writable);
    }
    Ok(())
}

#[cfg(not(unix))]
fn ensure_read_only_identity_file(_: &fs::Metadata) -> Result<(), RelayIdentityFileError> {
    Ok(())
}

#[cfg(test)]
mod tests {
    use std::{
        fs,
        net::SocketAddr,
        path::PathBuf,
        sync::atomic::{AtomicU64, Ordering},
        time::Duration,
    };

    use tokio::sync::oneshot;
    use yeokcham_core::RelaySigningKeypair;

    use super::{
        MAX_RELAY_GRACEFUL_SHUTDOWN_TIMEOUT, RelayIdentityFileError,
        RelayIdentityFileGenerationError, RelayRuntimeConfig, RelayRuntimeConfigError,
        check_relay_health, generate_relay_identity_file, load_relay_identity,
    };

    static NEXT_PATH: AtomicU64 = AtomicU64::new(0);

    #[test]
    fn validates_runtime_paths_endpoints_and_graceful_shutdown_bounds() {
        assert!(matches!(
            RelayRuntimeConfig::new(
                PathBuf::from("relay.conf"),
                PathBuf::from("/run/secrets/relay"),
                "127.0.0.1:8080".parse().unwrap(),
                "127.0.0.1:8081".parse().unwrap(),
                Duration::from_secs(30),
            ),
            Err(RelayRuntimeConfigError::RelativeConfigPath)
        ));
        assert!(matches!(
            RelayRuntimeConfig::new(
                PathBuf::from("/etc/relay.conf"),
                PathBuf::from("relay"),
                "127.0.0.1:8080".parse().unwrap(),
                "127.0.0.1:8081".parse().unwrap(),
                Duration::from_secs(30),
            ),
            Err(RelayRuntimeConfigError::RelativeIdentityPath)
        ));
        assert!(matches!(
            RelayRuntimeConfig::new(
                PathBuf::from("/etc/relay.conf"),
                PathBuf::from("/run/secrets/relay"),
                "127.0.0.1:8080".parse().unwrap(),
                "127.0.0.1:8081".parse().unwrap(),
                Duration::ZERO,
            ),
            Err(RelayRuntimeConfigError::ZeroGracefulShutdownTimeout)
        ));
        assert!(matches!(
            RelayRuntimeConfig::new(
                PathBuf::from("/etc/relay.conf"),
                PathBuf::from("/run/secrets/relay"),
                "127.0.0.1:8080".parse().unwrap(),
                "127.0.0.1:8081".parse().unwrap(),
                MAX_RELAY_GRACEFUL_SHUTDOWN_TIMEOUT + Duration::from_secs(1),
            ),
            Err(RelayRuntimeConfigError::ExcessiveGracefulShutdownTimeout)
        ));
        assert!(matches!(
            RelayRuntimeConfig::new(
                PathBuf::from("/etc/relay.conf"),
                PathBuf::from("/run/secrets/relay"),
                "192.0.2.1:8080".parse().unwrap(),
                "127.0.0.1:8081".parse().unwrap(),
                Duration::from_secs(30),
            ),
            Err(RelayRuntimeConfigError::HealthEndpoint(
                crate::RelayHealthEndpointError::NonLoopbackAddress
            ))
        ));
    }

    #[test]
    fn accepts_exact_read_only_identity_and_rejects_invalid_material() {
        let identity_path = temporary_path("identity");
        let identity = RelaySigningKeypair::generate().unwrap();
        fs::write(&identity_path, identity.serialize().as_ref()).unwrap();
        set_read_only(&identity_path);
        assert_eq!(
            load_relay_identity(&identity_path).unwrap().public_key(),
            identity.public_key()
        );
        fs::remove_file(&identity_path).unwrap();

        let invalid_path = temporary_path("identity-invalid");
        fs::write(&invalid_path, [0_u8; 1]).unwrap();
        set_read_only(&invalid_path);
        assert!(matches!(
            load_relay_identity(&invalid_path),
            Err(RelayIdentityFileError::InvalidLength)
        ));
        fs::remove_file(invalid_path).unwrap();
    }

    #[cfg(unix)]
    #[test]
    fn rejects_writable_identity_material() {
        let identity_path = temporary_path("identity-writable");
        fs::write(
            &identity_path,
            RelaySigningKeypair::generate()
                .unwrap()
                .serialize()
                .as_ref(),
        )
        .unwrap();
        assert!(matches!(
            load_relay_identity(&identity_path),
            Err(RelayIdentityFileError::Writable)
        ));
        fs::remove_file(identity_path).unwrap();
    }

    #[test]
    fn generates_exact_read_only_identity_without_overwriting_existing_material() {
        let identity_path = temporary_path("identity-generated");
        let public_key = generate_relay_identity_file(&identity_path).unwrap();
        assert_eq!(
            load_relay_identity(&identity_path).unwrap().public_key(),
            public_key
        );
        assert!(matches!(
            generate_relay_identity_file(&identity_path),
            Err(RelayIdentityFileGenerationError::Create(_))
        ));
        fs::remove_file(identity_path).unwrap();
    }

    #[tokio::test]
    async fn serves_a_real_ready_endpoint_from_mounted_runtime_material() {
        let config_path = temporary_path("config");
        let identity_path = temporary_path("identity-runtime");
        let database_path = temporary_path("database");
        let relay_address = available_loopback_address();
        let health_address = available_loopback_address();
        let metrics_address = available_loopback_address();
        fs::write(
            &config_path,
            format!(
                "config_version = 1\nlisten_address = \"{relay_address}\"\ndatabase_path = \"{}\"\nmailbox_quota_bytes = 1024\nretention_ttl_seconds = 60\ningress_max_requests = 2\ningress_window_seconds = 60\n",
                database_path.display()
            ),
        )
        .unwrap();
        fs::write(
            &identity_path,
            RelaySigningKeypair::generate()
                .unwrap()
                .serialize()
                .as_ref(),
        )
        .unwrap();
        set_read_only(&identity_path);
        let runtime = RelayRuntimeConfig::new(
            config_path.clone(),
            identity_path.clone(),
            health_address,
            metrics_address,
            Duration::from_secs(1),
        )
        .unwrap();
        let relay = runtime.bind().await.unwrap();
        let (shutdown_sender, shutdown_receiver) = oneshot::channel();
        let task = tokio::spawn(relay.serve_until(async move {
            let _ = shutdown_receiver.await;
        }));
        let health = tokio::task::spawn_blocking(move || {
            let mut attempts = 0;
            loop {
                match check_relay_health(health_address) {
                    Ok(()) => return Ok(()),
                    Err(_) if attempts < 10 => {
                        attempts += 1;
                        std::thread::sleep(Duration::from_millis(10));
                    }
                    Err(error) => return Err(error),
                }
            }
        })
        .await
        .unwrap();
        assert!(health.is_ok());
        shutdown_sender.send(()).unwrap();
        assert!(task.await.unwrap().is_ok());
        remove_runtime_files(&config_path, &identity_path, &database_path);
    }

    fn available_loopback_address() -> SocketAddr {
        let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
        listener.local_addr().unwrap()
    }

    fn temporary_path(name: &str) -> PathBuf {
        let number = NEXT_PATH.fetch_add(1, Ordering::Relaxed);
        std::env::temp_dir().join(format!(
            "yeokcham-relay-runner-{name}-{}-{number}",
            std::process::id()
        ))
    }

    fn set_read_only(path: &std::path::Path) {
        let mut permissions = fs::metadata(path).unwrap().permissions();
        permissions.set_readonly(true);
        fs::set_permissions(path, permissions).unwrap();
    }

    fn remove_runtime_files(
        config_path: &std::path::Path,
        identity_path: &std::path::Path,
        database_path: &std::path::Path,
    ) {
        for path in [
            config_path.to_path_buf(),
            identity_path.to_path_buf(),
            database_path.to_path_buf(),
            database_path.with_extension("sqlite-wal"),
            database_path.with_extension("sqlite-shm"),
        ] {
            if path.exists() {
                let _ = fs::remove_file(path);
            }
        }
    }
}
