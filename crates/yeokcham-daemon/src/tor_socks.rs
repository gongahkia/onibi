use std::net::SocketAddr;

use tokio::net::TcpStream;
use tokio_socks::tcp::Socks5Stream;

const TOR_V3_ONION_HOSTNAME_BYTES: usize = 62;
const ONION_SUFFIX: &str = ".onion";

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct TorSocksTarget {
    hostname: String,
    port: u16,
}

impl TorSocksTarget {
    pub fn new(hostname: String, port: u16) -> Result<Self, TorSocksError> {
        if port == 0 {
            return Err(TorSocksError::ZeroTargetPort);
        }
        if !is_canonical_onion_hostname(&hostname) {
            return Err(TorSocksError::InvalidOnionHostname);
        }
        Ok(Self { hostname, port })
    }

    #[must_use]
    pub fn hostname(&self) -> &str {
        &self.hostname
    }

    #[must_use]
    pub const fn port(&self) -> u16 {
        self.port
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct TorSocksConnector {
    proxy: SocketAddr,
}

impl TorSocksConnector {
    pub fn new(proxy: SocketAddr) -> Result<Self, TorSocksError> {
        if proxy.port() == 0 {
            return Err(TorSocksError::ZeroProxyPort);
        }
        if !proxy.ip().is_loopback() {
            return Err(TorSocksError::NonLoopbackProxy);
        }
        Ok(Self { proxy })
    }

    #[must_use]
    pub const fn proxy(self) -> SocketAddr {
        self.proxy
    }

    pub async fn connect(
        &self,
        target: &TorSocksTarget,
    ) -> Result<Socks5Stream<TcpStream>, TorSocksError> {
        Socks5Stream::connect(self.proxy, (target.hostname(), target.port()))
            .await
            .map_err(TorSocksError::Connect)
    }
}

#[derive(Debug, thiserror::Error)]
pub enum TorSocksError {
    #[error("Tor SOCKS proxy port must be nonzero")]
    ZeroProxyPort,
    #[error("Tor SOCKS proxy must be bound to a loopback address")]
    NonLoopbackProxy,
    #[error("Tor onion target port must be nonzero")]
    ZeroTargetPort,
    #[error("Tor onion target must be a canonical lowercase v3 onion hostname")]
    InvalidOnionHostname,
    #[error("Tor SOCKS connection failed: {0}")]
    Connect(#[source] tokio_socks::Error),
}

fn is_canonical_onion_hostname(hostname: &str) -> bool {
    hostname.len() == TOR_V3_ONION_HOSTNAME_BYTES
        && hostname.ends_with(ONION_SUFFIX)
        && hostname[..hostname.len() - ONION_SUFFIX.len()]
            .bytes()
            .all(|byte| byte.is_ascii_lowercase() || matches!(byte, b'2'..=b'7'))
}

#[cfg(test)]
mod tests {
    use tokio::{
        io::{AsyncReadExt, AsyncWriteExt},
        net::TcpListener,
    };

    use super::{TorSocksConnector, TorSocksError, TorSocksTarget};

    const ONION: &str = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.onion";

    #[test]
    fn validates_local_proxy_and_canonical_onion_target() {
        assert!(TorSocksConnector::new("127.0.0.1:9050".parse().unwrap()).is_ok());
        assert!(matches!(
            TorSocksConnector::new("192.0.2.1:9050".parse().unwrap()),
            Err(TorSocksError::NonLoopbackProxy)
        ));
        assert_eq!(
            TorSocksTarget::new(ONION.to_owned(), 443)
                .unwrap()
                .hostname(),
            ONION
        );
        assert!(matches!(
            TorSocksTarget::new(ONION.to_owned(), 0),
            Err(TorSocksError::ZeroTargetPort)
        ));
        assert!(matches!(
            TorSocksTarget::new(ONION.to_uppercase(), 443),
            Err(TorSocksError::InvalidOnionHostname)
        ));
    }

    #[tokio::test]
    async fn sends_the_onion_hostname_to_the_socks_proxy() {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let connector = TorSocksConnector::new(listener.local_addr().unwrap()).unwrap();
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
        let stream = connector.connect(&target).await.unwrap();
        drop(stream);
        proxy.await.unwrap();
    }
}
