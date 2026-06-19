use std::fmt::{Display, Formatter};
use std::fs;
use std::io;
use std::net::Ipv4Addr;
#[cfg(unix)]
use std::os::unix::fs::PermissionsExt;
use std::path::{Path, PathBuf};

use serde::Serialize;

pub const DEFAULT_AP_INTERFACE: &str = "wlan0";
pub const DEFAULT_AP_SSID: &str = "Kelp-Pi";
pub const DEFAULT_AP_ADDRESS: Ipv4Addr = Ipv4Addr::new(10, 42, 0, 1);
pub const DEFAULT_AP_PREFIX: u8 = 24;
pub const DEFAULT_DHCP_START: Ipv4Addr = Ipv4Addr::new(10, 42, 0, 20);
pub const DEFAULT_DHCP_END: Ipv4Addr = Ipv4Addr::new(10, 42, 0, 200);
pub const DEFAULT_DHCP_LEASE: &str = "12h";

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PiNetworkHardeningConfig {
    pub ap_interface: String,
    pub ssid: String,
    pub wpa3_passphrase: String,
    pub ap_address: Ipv4Addr,
    pub ap_prefix: u8,
    pub dhcp_start: Ipv4Addr,
    pub dhcp_end: Ipv4Addr,
    pub dhcp_lease: String,
    pub captive_domains: Vec<String>,
    pub allow_outbound: Vec<OutboundEndpoint>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct OutboundEndpoint {
    pub host: String,
    pub port: u16,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RenderedHardeningFile {
    pub relative_path: PathBuf,
    pub mode: u32,
    pub contents: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum NetworkHardeningError {
    InvalidInterface(String),
    InvalidSsid,
    InvalidPassphrase,
    InvalidPrefix(u8),
    InvalidDhcpLease(String),
    InvalidDomain(String),
    InvalidEndpoint(String),
    Io(String),
}

impl Default for PiNetworkHardeningConfig {
    fn default() -> Self {
        Self {
            ap_interface: DEFAULT_AP_INTERFACE.to_string(),
            ssid: DEFAULT_AP_SSID.to_string(),
            wpa3_passphrase: "change-this-kelp-pi-passphrase".to_string(),
            ap_address: DEFAULT_AP_ADDRESS,
            ap_prefix: DEFAULT_AP_PREFIX,
            dhcp_start: DEFAULT_DHCP_START,
            dhcp_end: DEFAULT_DHCP_END,
            dhcp_lease: DEFAULT_DHCP_LEASE.to_string(),
            captive_domains: default_captive_domains()
                .into_iter()
                .map(str::to_string)
                .collect(),
            allow_outbound: Vec::new(),
        }
    }
}

pub fn default_captive_domains() -> Vec<&'static str> {
    vec![
        "captive.apple.com",
        "connectivitycheck.gstatic.com",
        "connectivitycheck.android.com",
        "clients3.google.com",
        "detectportal.firefox.com",
        "msftconnecttest.com",
        "www.msftconnecttest.com",
    ]
}

impl OutboundEndpoint {
    pub fn parse(value: &str) -> Result<Self, NetworkHardeningError> {
        let Some((host, port)) = value.rsplit_once(':') else {
            return Err(NetworkHardeningError::InvalidEndpoint(value.to_string()));
        };
        if !safe_host(host) {
            return Err(NetworkHardeningError::InvalidEndpoint(value.to_string()));
        }
        let Ok(port) = port.parse::<u16>() else {
            return Err(NetworkHardeningError::InvalidEndpoint(value.to_string()));
        };
        if port == 0 {
            return Err(NetworkHardeningError::InvalidEndpoint(value.to_string()));
        }
        Ok(Self {
            host: host.to_string(),
            port,
        })
    }
}

pub fn render_network_hardening_files(
    config: &PiNetworkHardeningConfig,
) -> Result<Vec<RenderedHardeningFile>, NetworkHardeningError> {
    validate_config(config)?;
    Ok(vec![
        RenderedHardeningFile {
            relative_path: PathBuf::from(
                "etc/NetworkManager/system-connections/kelp-pi-ap.nmconnection",
            ),
            mode: 0o600,
            contents: render_network_manager_connection(config),
        },
        RenderedHardeningFile {
            relative_path: PathBuf::from("etc/dnsmasq.d/kelp-pi-captive.conf"),
            mode: 0o644,
            contents: render_dnsmasq(config),
        },
        RenderedHardeningFile {
            relative_path: PathBuf::from("etc/nftables.d/kelp-pi.nft"),
            mode: 0o644,
            contents: render_nftables(config),
        },
        RenderedHardeningFile {
            relative_path: PathBuf::from("etc/sysctl.d/90-kelp-pi-network.conf"),
            mode: 0o644,
            contents: render_sysctl(),
        },
        RenderedHardeningFile {
            relative_path: PathBuf::from("etc/kelp-pi/network-hardening.json"),
            mode: 0o644,
            contents: render_network_config_json(config)?,
        },
        RenderedHardeningFile {
            relative_path: PathBuf::from("boot/firmware/config.txt.kelp-pi-fragment"),
            mode: 0o644,
            contents: render_boot_config_fragment(),
        },
    ])
}

pub fn write_network_hardening_files(
    root: &Path,
    config: &PiNetworkHardeningConfig,
) -> Result<Vec<PathBuf>, NetworkHardeningError> {
    let files = render_network_hardening_files(config)?;
    let mut written = Vec::new();
    for file in files {
        let path = root.join(&file.relative_path);
        let Some(parent) = path.parent() else {
            return Err(NetworkHardeningError::Io(format!(
                "path has no parent: {}",
                path.display()
            )));
        };
        fs::create_dir_all(parent).map_err(io_error)?;
        fs::write(&path, file.contents).map_err(io_error)?;
        #[cfg(unix)]
        {
            let mut permissions = fs::metadata(&path).map_err(io_error)?.permissions();
            permissions.set_mode(file.mode);
            fs::set_permissions(&path, permissions).map_err(io_error)?;
        }
        written.push(path);
    }
    Ok(written)
}

fn validate_config(config: &PiNetworkHardeningConfig) -> Result<(), NetworkHardeningError> {
    if !safe_interface_name(&config.ap_interface) {
        return Err(NetworkHardeningError::InvalidInterface(
            config.ap_interface.clone(),
        ));
    }
    if config.ssid.is_empty() || config.ssid.len() > 32 || has_line_break(&config.ssid) {
        return Err(NetworkHardeningError::InvalidSsid);
    }
    if config.wpa3_passphrase.len() < 8
        || config.wpa3_passphrase.len() > 63
        || has_line_break(&config.wpa3_passphrase)
    {
        return Err(NetworkHardeningError::InvalidPassphrase);
    }
    if config.ap_prefix == 0 || config.ap_prefix > 32 {
        return Err(NetworkHardeningError::InvalidPrefix(config.ap_prefix));
    }
    if !safe_token(&config.dhcp_lease) {
        return Err(NetworkHardeningError::InvalidDhcpLease(
            config.dhcp_lease.clone(),
        ));
    }
    for domain in &config.captive_domains {
        if !safe_domain(domain) {
            return Err(NetworkHardeningError::InvalidDomain(domain.clone()));
        }
    }
    for endpoint in &config.allow_outbound {
        if !safe_host(&endpoint.host) || endpoint.port == 0 {
            return Err(NetworkHardeningError::InvalidEndpoint(format!(
                "{}:{}",
                endpoint.host, endpoint.port
            )));
        }
    }
    Ok(())
}

fn render_network_manager_connection(config: &PiNetworkHardeningConfig) -> String {
    format!(
        "[connection]\nid=kelp-pi-ap\ntype=wifi\ninterface-name={}\nautoconnect=true\nuuid=4b50ad4c-51ff-45e4-a1fb-5f3313d72e5f\n\n[wifi]\nmode=ap\nssid={}\nband=bg\nchannel=6\nap-isolation=1\n\n[wifi-security]\nkey-mgmt=sae\npmf=3\nproto=rsn\npairwise=ccmp\ngroup=ccmp\npsk={}\n\n[ipv4]\nmethod=manual\naddress1={}/{}\nnever-default=true\nignore-auto-dns=true\n\n[ipv6]\nmethod=disabled\n",
        config.ap_interface,
        config.ssid,
        config.wpa3_passphrase,
        config.ap_address,
        config.ap_prefix
    )
}

fn render_dnsmasq(config: &PiNetworkHardeningConfig) -> String {
    let mut output = format!(
        "interface={}\nbind-interfaces\nno-resolv\nno-poll\ndomain-needed\nbogus-priv\ndhcp-authoritative\ndhcp-range={},{},255.255.255.0,{}\ndhcp-option=option:router,{}\ndhcp-option=option:dns-server,{}\n",
        config.ap_interface,
        config.dhcp_start,
        config.dhcp_end,
        config.dhcp_lease,
        config.ap_address,
        config.ap_address
    );
    for domain in &config.captive_domains {
        output.push_str(&format!("address=/{}/{}\n", domain, config.ap_address));
    }
    output
}

fn render_nftables(config: &PiNetworkHardeningConfig) -> String {
    let mut output = format!(
        "flush table inet kelp_pi_filter\n\ntable inet kelp_pi_filter {{\n  chain input {{\n    type filter hook input priority filter; policy drop;\n    iifname \"lo\" accept\n    ct state established,related accept\n    iifname \"{}\" udp dport {{ 53, 67 }} accept\n    iifname \"{}\" tcp dport {{ 80, 443, 8080 }} accept\n    iifname \"{}\" ip protocol icmp accept\n  }}\n\n  chain forward {{\n    type filter hook forward priority filter; policy drop;\n  }}\n\n  chain output {{\n    type filter hook output priority filter; policy drop;\n    oifname \"lo\" accept\n    ct state established,related accept\n",
        config.ap_interface, config.ap_interface, config.ap_interface
    );
    for endpoint in &config.allow_outbound {
        output.push_str(&format!(
            "    ip daddr {} tcp dport {} accept\n",
            endpoint.host, endpoint.port
        ));
    }
    output.push_str("  }\n}\n");
    output
}

fn render_sysctl() -> String {
    "net.ipv4.ip_forward=0\nnet.ipv6.conf.all.forwarding=0\n".to_string()
}

fn render_network_config_json(
    config: &PiNetworkHardeningConfig,
) -> Result<String, NetworkHardeningError> {
    #[derive(Serialize)]
    struct JsonConfig<'a> {
        ap_interface: &'a str,
        ssid: &'a str,
        ap_address: String,
        ap_prefix: u8,
        dhcp_start: String,
        dhcp_end: String,
        captive_domains: &'a [String],
        allow_outbound: &'a [OutboundEndpoint],
    }
    serde_json::to_string_pretty(&JsonConfig {
        ap_interface: &config.ap_interface,
        ssid: &config.ssid,
        ap_address: config.ap_address.to_string(),
        ap_prefix: config.ap_prefix,
        dhcp_start: config.dhcp_start.to_string(),
        dhcp_end: config.dhcp_end.to_string(),
        captive_domains: &config.captive_domains,
        allow_outbound: &config.allow_outbound,
    })
    .map(|json| format!("{json}\n"))
    .map_err(|error| NetworkHardeningError::Io(error.to_string()))
}

fn render_boot_config_fragment() -> String {
    "dtoverlay=disable-bt\ndtparam=audio=off\ndtoverlay=vc4-kms-v3d,noaudio\ndtparam=i2c_arm=off\ndtparam=spi=off\nhdmi_blanking=2\n".to_string()
}

fn safe_interface_name(value: &str) -> bool {
    !value.is_empty()
        && value.len() <= 15
        && value
            .bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'-' | b'_' | b'.'))
}

fn safe_domain(value: &str) -> bool {
    !value.is_empty()
        && value.len() <= 253
        && value.split('.').all(|label| {
            !label.is_empty()
                && label.len() <= 63
                && !label.starts_with('-')
                && !label.ends_with('-')
                && label
                    .bytes()
                    .all(|byte| byte.is_ascii_alphanumeric() || byte == b'-')
        })
}

fn safe_host(value: &str) -> bool {
    value.parse::<Ipv4Addr>().is_ok() || safe_domain(value)
}

fn safe_token(value: &str) -> bool {
    !value.is_empty()
        && value
            .bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'-' | b'_'))
}

fn has_line_break(value: &str) -> bool {
    value.bytes().any(|byte| matches!(byte, b'\n' | b'\r'))
}

fn io_error(error: io::Error) -> NetworkHardeningError {
    NetworkHardeningError::Io(error.to_string())
}

impl Display for NetworkHardeningError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            NetworkHardeningError::InvalidInterface(value) => {
                write!(formatter, "invalid interface name: {value}")
            }
            NetworkHardeningError::InvalidSsid => write!(formatter, "invalid AP SSID"),
            NetworkHardeningError::InvalidPassphrase => {
                write!(formatter, "invalid WPA3 passphrase")
            }
            NetworkHardeningError::InvalidPrefix(value) => {
                write!(formatter, "invalid AP prefix: {value}")
            }
            NetworkHardeningError::InvalidDhcpLease(value) => {
                write!(formatter, "invalid DHCP lease: {value}")
            }
            NetworkHardeningError::InvalidDomain(value) => {
                write!(formatter, "invalid captive domain: {value}")
            }
            NetworkHardeningError::InvalidEndpoint(value) => {
                write!(formatter, "invalid outbound endpoint: {value}")
            }
            NetworkHardeningError::Io(value) => write!(formatter, "{value}"),
        }
    }
}

impl std::error::Error for NetworkHardeningError {}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn renders_wpa3_ap_dns_sinkhole_nft_allowlist_and_boot_fragment() {
        let config = PiNetworkHardeningConfig {
            wpa3_passphrase: "correct-horse-battery".to_string(),
            allow_outbound: vec![OutboundEndpoint::parse("cp.example.test:443").expect("endpoint")],
            ..PiNetworkHardeningConfig::default()
        };

        let files = render_network_hardening_files(&config).expect("render files");
        let nm = contents(
            &files,
            "etc/NetworkManager/system-connections/kelp-pi-ap.nmconnection",
        );
        assert!(nm.contains("mode=ap"));
        assert!(nm.contains("ap-isolation=1"));
        assert!(nm.contains("key-mgmt=sae"));
        assert!(nm.contains("pmf=3"));
        assert!(nm.contains("never-default=true"));
        let dnsmasq = contents(&files, "etc/dnsmasq.d/kelp-pi-captive.conf");
        assert!(dnsmasq.contains("no-resolv"));
        assert!(dnsmasq.contains("address=/captive.apple.com/10.42.0.1"));
        assert!(dnsmasq.contains("address=/connectivitycheck.gstatic.com/10.42.0.1"));
        let nft = contents(&files, "etc/nftables.d/kelp-pi.nft");
        assert!(nft.contains("chain output"));
        assert!(nft.contains("policy drop;"));
        assert!(nft.contains("ip daddr cp.example.test tcp dport 443 accept"));
        let boot = contents(&files, "boot/firmware/config.txt.kelp-pi-fragment");
        assert!(boot.contains("dtoverlay=disable-bt"));
        assert!(boot.contains("dtparam=audio=off"));
        assert!(boot.contains("hdmi_blanking=2"));
    }

    #[test]
    fn rejects_unsafe_passphrase_domain_and_endpoint() {
        let short = PiNetworkHardeningConfig {
            wpa3_passphrase: "short".to_string(),
            ..PiNetworkHardeningConfig::default()
        };
        assert!(matches!(
            render_network_hardening_files(&short),
            Err(NetworkHardeningError::InvalidPassphrase)
        ));
        assert!(OutboundEndpoint::parse("bad host:443").is_err());
        assert!(OutboundEndpoint::parse("cp.example.test:0").is_err());
    }

    fn contents<'a>(files: &'a [RenderedHardeningFile], path: &str) -> &'a str {
        files
            .iter()
            .find(|file| file.relative_path == PathBuf::from(path))
            .expect("file exists")
            .contents
            .as_str()
    }
}
