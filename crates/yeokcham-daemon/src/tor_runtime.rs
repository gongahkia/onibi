use std::{net::SocketAddr, time::Duration};

use tokio::net::TcpStream;
use tokio_socks::tcp::Socks5Stream;

use crate::{TorSocksConnector, TorSocksError, TorSocksTarget};

pub const DEFAULT_EXTERNAL_TOR_SOCKS_PORT: u16 = 9050;
pub const DEFAULT_EXTERNAL_TOR_CONNECT_TIMEOUT: Duration = Duration::from_secs(10);
pub const MAX_EXTERNAL_TOR_CONNECT_TIMEOUT: Duration = Duration::from_secs(60);

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ExternalTorRuntimeConfig {
    socks_proxy: SocketAddr,
    connect_timeout: Duration,
}

impl ExternalTorRuntimeConfig {
    pub fn new(
        socks_proxy: SocketAddr,
        connect_timeout: Duration,
    ) -> Result<Self, ExternalTorRuntimeConfigError> {
        if connect_timeout.is_zero() {
            return Err(ExternalTorRuntimeConfigError::ZeroConnectTimeout);
        }
        if connect_timeout > MAX_EXTERNAL_TOR_CONNECT_TIMEOUT {
            return Err(ExternalTorRuntimeConfigError::ConnectTimeoutExceedsMaximum);
        }
        TorSocksConnector::new(socks_proxy).map_err(ExternalTorRuntimeConfigError::Socks)?;
        Ok(Self {
            socks_proxy,
            connect_timeout,
        })
    }

    #[must_use]
    pub fn localhost_default() -> Self {
        Self {
            socks_proxy: ([127, 0, 0, 1], DEFAULT_EXTERNAL_TOR_SOCKS_PORT).into(),
            connect_timeout: DEFAULT_EXTERNAL_TOR_CONNECT_TIMEOUT,
        }
    }

    #[must_use]
    pub const fn socks_proxy(&self) -> SocketAddr {
        self.socks_proxy
    }

    #[must_use]
    pub const fn connect_timeout(&self) -> Duration {
        self.connect_timeout
    }

    #[must_use]
    pub fn runtime(&self) -> ExternalTorRuntime {
        ExternalTorRuntime {
            connector: TorSocksConnector::new(self.socks_proxy)
                .expect("external Tor runtime configuration was validated"),
            connect_timeout: self.connect_timeout,
        }
    }
}

impl Default for ExternalTorRuntimeConfig {
    fn default() -> Self {
        Self::localhost_default()
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ExternalTorRuntime {
    connector: TorSocksConnector,
    connect_timeout: Duration,
}

impl ExternalTorRuntime {
    #[must_use]
    pub const fn socks_proxy(&self) -> SocketAddr {
        self.connector.proxy()
    }

    #[must_use]
    pub const fn connect_timeout(&self) -> Duration {
        self.connect_timeout
    }

    pub async fn connect(
        &self,
        target: &TorSocksTarget,
    ) -> Result<Socks5Stream<TcpStream>, ExternalTorRuntimeError> {
        tokio::time::timeout(self.connect_timeout, self.connector.connect(target))
            .await
            .map_err(|_| ExternalTorRuntimeError::ConnectTimeout)?
            .map_err(ExternalTorRuntimeError::Socks)
    }
}

#[derive(Debug, thiserror::Error)]
pub enum ExternalTorRuntimeConfigError {
    #[error("external Tor connect timeout must be nonzero")]
    ZeroConnectTimeout,
    #[error("external Tor connect timeout exceeds the configured maximum")]
    ConnectTimeoutExceedsMaximum,
    #[error("external Tor SOCKS proxy configuration is invalid")]
    Socks(#[source] TorSocksError),
}

#[derive(Debug, thiserror::Error)]
pub enum ExternalTorRuntimeError {
    #[error("external Tor SOCKS connection timed out")]
    ConnectTimeout,
    #[error("external Tor SOCKS connection failed")]
    Socks(#[source] TorSocksError),
}

#[cfg(test)]
mod tests {
    use std::time::Duration;

    use tokio::{
        io::{AsyncReadExt, AsyncWriteExt},
        net::TcpListener,
    };

    use super::{
        DEFAULT_EXTERNAL_TOR_CONNECT_TIMEOUT, DEFAULT_EXTERNAL_TOR_SOCKS_PORT,
        ExternalTorRuntimeConfig, ExternalTorRuntimeConfigError, ExternalTorRuntimeError,
        MAX_EXTERNAL_TOR_CONNECT_TIMEOUT,
    };
    use crate::TorSocksTarget;

    const ONION: &str = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.onion";

    #[test]
    fn creates_the_local_default_external_tor_runtime() {
        let config = ExternalTorRuntimeConfig::localhost_default();
        assert_eq!(config.socks_proxy(), "127.0.0.1:9050".parse().unwrap());
        assert_eq!(config.socks_proxy().port(), DEFAULT_EXTERNAL_TOR_SOCKS_PORT);
        assert_eq!(
            config.connect_timeout(),
            DEFAULT_EXTERNAL_TOR_CONNECT_TIMEOUT
        );
        assert_eq!(config.runtime().socks_proxy(), config.socks_proxy());
    }

    #[test]
    fn rejects_unbounded_or_nonlocal_external_tor_configuration() {
        assert!(matches!(
            ExternalTorRuntimeConfig::new("127.0.0.1:9050".parse().unwrap(), Duration::ZERO,),
            Err(ExternalTorRuntimeConfigError::ZeroConnectTimeout)
        ));
        assert!(matches!(
            ExternalTorRuntimeConfig::new(
                "127.0.0.1:9050".parse().unwrap(),
                MAX_EXTERNAL_TOR_CONNECT_TIMEOUT + Duration::from_secs(1),
            ),
            Err(ExternalTorRuntimeConfigError::ConnectTimeoutExceedsMaximum)
        ));
        assert!(matches!(
            ExternalTorRuntimeConfig::new(
                "192.0.2.1:9050".parse().unwrap(),
                Duration::from_secs(1),
            ),
            Err(ExternalTorRuntimeConfigError::Socks(_))
        ));
    }

    #[tokio::test]
    async fn connects_through_the_configured_external_tor_socks_runtime() {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let runtime =
            ExternalTorRuntimeConfig::new(listener.local_addr().unwrap(), Duration::from_secs(1))
                .unwrap()
                .runtime();
        let target = TorSocksTarget::new(ONION.to_owned(), 4444).unwrap();
        let proxy = tokio::spawn(async move {
            let (mut stream, _) = listener.accept().await.unwrap();
            let mut greeting = [0; 3];
            stream.read_exact(&mut greeting).await.unwrap();
            assert_eq!(greeting, [5, 1, 0]);
            stream.write_all(&[5, 0]).await.unwrap();
            let mut header = [0; 5];
            stream.read_exact(&mut header).await.unwrap();
            assert_eq!(&header[..4], &[5, 1, 0, 3]);
            let mut hostname = vec![0; usize::from(header[4])];
            stream.read_exact(&mut hostname).await.unwrap();
            let mut port = [0; 2];
            stream.read_exact(&mut port).await.unwrap();
            assert_eq!(hostname, ONION.as_bytes());
            assert_eq!(u16::from_be_bytes(port), 4444);
            stream
                .write_all(&[5, 0, 0, 1, 0, 0, 0, 0, 0, 0])
                .await
                .unwrap();
        });
        let stream = runtime.connect(&target).await.unwrap();
        drop(stream);
        proxy.await.unwrap();
    }

    #[tokio::test]
    async fn bounds_an_unresponsive_external_tor_socks_runtime() {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let runtime = ExternalTorRuntimeConfig::new(
            listener.local_addr().unwrap(),
            Duration::from_millis(10),
        )
        .unwrap()
        .runtime();
        let proxy = tokio::spawn(async move {
            let (_stream, _) = listener.accept().await.unwrap();
            std::future::pending::<()>().await;
        });
        let target = TorSocksTarget::new(ONION.to_owned(), 443).unwrap();
        assert!(matches!(
            runtime.connect(&target).await,
            Err(ExternalTorRuntimeError::ConnectTimeout)
        ));
        proxy.abort();
        assert!(proxy.await.unwrap_err().is_cancelled());
    }
}
