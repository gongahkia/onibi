use super::{Transport, TransportHandle, TransportStatus};
use anyhow::{anyhow, Context, Result};
use if_addrs::IfAddr;
use mdns_sd::{ServiceDaemon, ServiceInfo};
use rcgen::{generate_simple_self_signed, CertifiedKey};
use rustls::{pki_types::CertificateDer, ServerConfig};
use sha2::{Digest, Sha256};
use std::{
    collections::HashMap,
    fs::{self, OpenOptions},
    io::{BufReader, Write},
    net::{IpAddr, Ipv4Addr, SocketAddr},
    path::{Path, PathBuf},
    sync::Arc,
};
use tokio::{
    io::copy_bidirectional,
    net::{TcpListener, TcpStream},
    sync::oneshot,
};
use tokio_rustls::TlsAcceptor;

const CERT_FILE: &str = "lan.crt";
const KEY_FILE: &str = "lan.key";
const SERVICE_TYPE: &str = "_onibi._tcp.local.";

#[derive(Debug, Clone, Default)]
pub struct LanTransport;

#[async_trait::async_trait]
impl Transport for LanTransport {
    fn name(&self) -> &'static str {
        "lan"
    }

    fn label(&self) -> &'static str {
        "LAN"
    }

    fn requires_external_dep(&self) -> Option<&'static str> {
        None
    }

    async fn start(&self, local_port: u16) -> Result<TransportHandle> {
        let cert = load_or_create_cert()?;
        let bind_ip = discover_lan_ipv4()?
            .into_iter()
            .next()
            .ok_or_else(|| anyhow!("no non-loopback LAN IPv4 address found"))?;
        let listener = TcpListener::bind(SocketAddr::new(IpAddr::V4(bind_ip), local_port))
            .await
            .with_context(|| format!("bind LAN TLS listener on {bind_ip}:{local_port}"))?;
        let config = tls_config(&cert.cert_pem, &cert.key_pem)?;
        let acceptor = TlsAcceptor::from(Arc::new(config));
        let mdns = register_mdns(bind_ip, local_port, &cert.fingerprint);
        let (shutdown, rx) = oneshot::channel();
        tokio::spawn(async move {
            serve_tls_proxy(listener, acceptor, local_port, rx, mdns).await;
        });

        Ok(TransportHandle::new(
            Some(format!("https://{bind_ip}:{local_port}/")),
            Some(cert.fingerprint),
            shutdown,
        ))
    }

    async fn status(&self) -> TransportStatus {
        match (load_or_create_cert(), discover_lan_ipv4()) {
            (Ok(_), Ok(ips)) if !ips.is_empty() => TransportStatus::Stopped,
            (Ok(_), Ok(_)) => TransportStatus::failed("no non-loopback LAN IPv4 address found"),
            (Err(error), _) | (_, Err(error)) => TransportStatus::failed(error.to_string()),
        }
    }
}

#[derive(Debug, Clone)]
struct LanCertificate {
    cert_pem: String,
    key_pem: String,
    fingerprint: String,
}

struct MdnsRegistration {
    daemon: ServiceDaemon,
    fullname: String,
}

pub fn cert_paths() -> Result<(PathBuf, PathBuf)> {
    Ok(cert_paths_in(&crate::secret::config_dir()?))
}

pub fn read_cert_pem() -> Result<String> {
    let (cert_path, _) = cert_paths()?;
    fs::read_to_string(&cert_path).with_context(|| format!("read {}", cert_path.display()))
}

fn cert_paths_in(config_dir: &Path) -> (PathBuf, PathBuf) {
    (config_dir.join(CERT_FILE), config_dir.join(KEY_FILE))
}

fn load_or_create_cert() -> Result<LanCertificate> {
    let config_dir = crate::secret::config_dir()?;
    load_or_create_cert_at(&config_dir)
}

fn load_or_create_cert_at(config_dir: &Path) -> Result<LanCertificate> {
    let (cert_path, key_path) = cert_paths_in(config_dir);
    if cert_path.exists() && key_path.exists() {
        let cert_pem = fs::read_to_string(&cert_path)
            .with_context(|| format!("read {}", cert_path.display()))?;
        let key_pem = fs::read_to_string(&key_path)
            .with_context(|| format!("read {}", key_path.display()))?;
        let fingerprint = fingerprint_from_pem(&cert_pem)?;
        return Ok(LanCertificate {
            cert_pem,
            key_pem,
            fingerprint,
        });
    }

    fs::create_dir_all(config_dir)
        .with_context(|| format!("create config directory {}", config_dir.display()))?;
    let mut subject_alt_names = vec!["localhost".to_string(), "127.0.0.1".to_string()];
    subject_alt_names.extend(discover_lan_ipv4()?.into_iter().map(|ip| ip.to_string()));
    let CertifiedKey { cert, key_pair } = generate_simple_self_signed(subject_alt_names)
        .context("generate self-signed LAN certificate")?;
    let cert_pem = cert.pem();
    let key_pem = key_pair.serialize_pem();
    write_secret_file(&cert_path, cert_pem.as_bytes())?;
    write_secret_file(&key_path, key_pem.as_bytes())?;
    let fingerprint = fingerprint_der(cert.der().as_ref());

    Ok(LanCertificate {
        cert_pem,
        key_pem,
        fingerprint,
    })
}

fn write_secret_file(path: &Path, bytes: &[u8]) -> Result<()> {
    #[cfg(unix)]
    {
        use std::os::unix::fs::{OpenOptionsExt, PermissionsExt};
        let mut file = OpenOptions::new()
            .create(true)
            .truncate(true)
            .write(true)
            .mode(0o600)
            .open(path)
            .with_context(|| format!("open {}", path.display()))?;
        file.write_all(bytes)
            .with_context(|| format!("write {}", path.display()))?;
        fs::set_permissions(path, fs::Permissions::from_mode(0o600))
            .with_context(|| format!("chmod 0600 {}", path.display()))?;
    }
    #[cfg(not(unix))]
    {
        let mut file = OpenOptions::new()
            .create(true)
            .truncate(true)
            .write(true)
            .open(path)
            .with_context(|| format!("open {}", path.display()))?;
        file.write_all(bytes)
            .with_context(|| format!("write {}", path.display()))?;
    }
    Ok(())
}

fn tls_config(cert_pem: &str, key_pem: &str) -> Result<ServerConfig> {
    let certs = rustls_pemfile::certs(&mut BufReader::new(cert_pem.as_bytes()))
        .collect::<std::result::Result<Vec<CertificateDer<'static>>, _>>()
        .context("parse LAN certificate PEM")?;
    let key = rustls_pemfile::private_key(&mut BufReader::new(key_pem.as_bytes()))
        .context("parse LAN private key PEM")?
        .ok_or_else(|| anyhow!("LAN private key PEM did not contain a key"))?;
    ServerConfig::builder()
        .with_no_client_auth()
        .with_single_cert(certs, key)
        .context("build LAN TLS config")
}

fn discover_lan_ipv4() -> Result<Vec<Ipv4Addr>> {
    let mut ips = Vec::new();
    for iface in if_addrs::get_if_addrs().context("enumerate network interfaces")? {
        let IfAddr::V4(addr) = iface.addr else {
            continue;
        };
        if addr.ip.is_loopback() {
            continue;
        }
        if addr.ip.is_private() || addr.ip.is_link_local() {
            ips.push(addr.ip);
        }
    }
    ips.sort_unstable();
    ips.dedup();
    Ok(ips)
}

fn register_mdns(ip: Ipv4Addr, port: u16, fingerprint: &str) -> Option<MdnsRegistration> {
    let daemon = ServiceDaemon::new().ok()?;
    let suffix: String = crate::secret::generate_token()
        .chars()
        .filter(|ch| ch.is_ascii_alphanumeric())
        .take(8)
        .collect::<String>()
        .to_lowercase();
    let host_name = format!("onibi-{suffix}.local.");
    let ip_string = ip.to_string();
    let mut props = HashMap::new();
    props.insert("protocol".to_string(), "onibi".to_string());
    props.insert("fingerprint".to_string(), fingerprint.to_string());
    let service = ServiceInfo::new(SERVICE_TYPE, "Onibi", &host_name, ip_string, port, props)
        .ok()?
        .enable_addr_auto();
    let fullname = service.get_fullname().to_string();
    daemon.register(service).ok()?;
    Some(MdnsRegistration { daemon, fullname })
}

async fn serve_tls_proxy(
    listener: TcpListener,
    acceptor: TlsAcceptor,
    local_port: u16,
    mut shutdown: oneshot::Receiver<()>,
    mdns: Option<MdnsRegistration>,
) {
    loop {
        tokio::select! {
            biased;
            _ = &mut shutdown => break,
            accepted = listener.accept() => {
                let Ok((stream, _peer)) = accepted else {
                    break;
                };
                let acceptor = acceptor.clone();
                tokio::spawn(async move {
                    if let Err(error) = proxy_one(stream, acceptor, local_port).await {
                        tracing::debug!(%error, "LAN TLS proxy connection failed");
                    }
                });
            }
        }
    }

    if let Some(mdns) = mdns {
        let _ = mdns.daemon.unregister(&mdns.fullname);
        let _ = mdns.daemon.shutdown();
    }
}

async fn proxy_one(stream: TcpStream, acceptor: TlsAcceptor, local_port: u16) -> Result<()> {
    let mut tls = acceptor.accept(stream).await.context("accept LAN TLS")?;
    let mut upstream = TcpStream::connect(("127.0.0.1", local_port))
        .await
        .with_context(|| format!("connect local Onibi server on 127.0.0.1:{local_port}"))?;
    copy_bidirectional(&mut tls, &mut upstream)
        .await
        .context("proxy LAN TLS stream")?;
    Ok(())
}

fn fingerprint_from_pem(cert_pem: &str) -> Result<String> {
    let cert = rustls_pemfile::certs(&mut BufReader::new(cert_pem.as_bytes()))
        .next()
        .transpose()
        .context("parse LAN certificate PEM")?
        .ok_or_else(|| anyhow!("LAN certificate PEM did not contain a certificate"))?;
    Ok(fingerprint_der(cert.as_ref()))
}

fn fingerprint_der(der: &[u8]) -> String {
    let digest = Sha256::digest(der);
    format!("sha256:{}", hex::encode(digest))
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::tempdir;

    #[test]
    fn cert_fingerprint_stability() {
        let dir = tempdir().unwrap();
        let first = load_or_create_cert_at(dir.path()).unwrap();
        let second = load_or_create_cert_at(dir.path()).unwrap();

        assert_eq!(first.fingerprint, second.fingerprint);
        assert!(first.fingerprint.starts_with("sha256:"));
        assert_eq!(first.cert_pem, second.cert_pem);
        assert_eq!(first.key_pem, second.key_pem);
    }
}
