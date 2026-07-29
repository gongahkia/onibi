use std::{
    net::{IpAddr, Ipv4Addr, SocketAddr},
    time::Duration,
};

use arachne_protocol::{
    LocalMeshTransportKind, WifiGroupBootstrap, WifiGroupHandoff, WifiGroupRole,
};

const MACOS_HOST_AP_INTERFACE_MODE: isize = 3;
const MACOS_STATION_INTERFACE_MODE: isize = 1;
const MAX_MACOS_INTERFACE_NAME_BYTES: usize = 15;
const MAX_MACOS_INTERFACE_QUERY_OUTPUT_BYTES: usize = 16_384;
const MACOS_INTERFACE_QUERY_TIMEOUT: Duration = Duration::from_secs(2);
const MACOS_INTERFACE_QUERY_POLL_INTERVAL: Duration = Duration::from_millis(10);

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum MacOsWifiInterfaceMode {
    HostAp,
    Station,
}

#[cfg(target_os = "macos")]
use crate::WifiGroupLifecycle;
#[cfg(target_os = "macos")]
use corewlan::WiFiClient;
#[cfg(target_os = "macos")]
use std::{
    future::Future,
    io::Read,
    process::{Command, Stdio},
    sync::mpsc,
    thread,
    time::Instant,
};

#[cfg(target_os = "macos")]
pub struct MacOsExistingHotspotGroup {
    state: MacOsExistingHotspotState,
}

#[cfg(target_os = "macos")]
enum MacOsExistingHotspotState {
    Inactive,
    Owner(MacOsHotspotSnapshot),
    Client,
}

#[cfg(target_os = "macos")]
struct MacOsHotspotSnapshot {
    ssid: String,
    mode: MacOsWifiInterfaceMode,
    addresses: Vec<IpAddr>,
}

#[cfg(target_os = "macos")]
impl MacOsExistingHotspotGroup {
    #[must_use]
    pub const fn new() -> Self {
        Self {
            state: MacOsExistingHotspotState::Inactive,
        }
    }

    #[must_use]
    pub const fn is_active(&self) -> bool {
        !matches!(self.state, MacOsExistingHotspotState::Inactive)
    }

    fn attach_owner(
        &mut self,
        bootstrap: &WifiGroupBootstrap,
    ) -> Result<(), MacOsExistingHotspotGroupError> {
        if self.is_active() {
            return Err(MacOsExistingHotspotGroupError::AlreadyActive);
        }
        if bootstrap.transport() != LocalMeshTransportKind::WifiHotspot {
            return Err(MacOsExistingHotspotGroupError::UnsupportedTransport(
                bootstrap.transport(),
            ));
        }
        let snapshot = current_snapshot()?;
        validate_attachment(
            &snapshot,
            MacOsWifiInterfaceMode::HostAp,
            bootstrap.identifier(),
        )?;
        self.state = MacOsExistingHotspotState::Owner(snapshot);
        Ok(())
    }

    fn attach_client(
        &mut self,
        handoff: &WifiGroupHandoff,
    ) -> Result<(), MacOsExistingHotspotGroupError> {
        if self.is_active() {
            return Err(MacOsExistingHotspotGroupError::AlreadyActive);
        }
        if handoff.configuration().transport() != LocalMeshTransportKind::WifiHotspot {
            return Err(MacOsExistingHotspotGroupError::UnsupportedTransport(
                handoff.configuration().transport(),
            ));
        }
        if handoff.configuration().role() != WifiGroupRole::Owner {
            return Err(MacOsExistingHotspotGroupError::InvalidHandoffRole);
        }
        let snapshot = current_snapshot()?;
        validate_attachment(
            &snapshot,
            MacOsWifiInterfaceMode::Station,
            handoff.configuration().identifier(),
        )?;
        if snapshot
            .addresses
            .iter()
            .any(|address| *address == handoff.configuration().direct_profile().endpoint().ip())
        {
            return Err(MacOsExistingHotspotGroupError::ReflexiveDirectEndpoint);
        }
        self.state = MacOsExistingHotspotState::Client;
        Ok(())
    }

    fn validate_owner_endpoint(
        &self,
        endpoint: SocketAddr,
    ) -> Result<(), MacOsExistingHotspotGroupError> {
        match &self.state {
            MacOsExistingHotspotState::Owner(snapshot)
                if snapshot
                    .addresses
                    .iter()
                    .any(|address| *address == endpoint.ip()) =>
            {
                Ok(())
            }
            MacOsExistingHotspotState::Owner(_) => {
                Err(MacOsExistingHotspotGroupError::EndpointAddressMismatch)
            }
            _ => Err(MacOsExistingHotspotGroupError::OwnerNotActive),
        }
    }

    fn teardown(&mut self) {
        self.state = MacOsExistingHotspotState::Inactive;
    }
}

#[cfg(target_os = "macos")]
impl Default for MacOsExistingHotspotGroup {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(target_os = "macos")]
impl std::fmt::Debug for MacOsExistingHotspotGroup {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        formatter
            .debug_struct("MacOsExistingHotspotGroup")
            .field("active", &self.is_active())
            .finish_non_exhaustive()
    }
}

#[cfg(target_os = "macos")]
impl WifiGroupLifecycle for MacOsExistingHotspotGroup {
    type Error = MacOsExistingHotspotGroupError;

    fn activate_owner(
        &mut self,
        bootstrap: &WifiGroupBootstrap,
    ) -> impl Future<Output = Result<(), Self::Error>> + Send {
        std::future::ready(self.attach_owner(bootstrap))
    }

    fn validate_owner_endpoint(&self, endpoint: SocketAddr) -> Result<(), Self::Error> {
        self.validate_owner_endpoint(endpoint)
    }

    fn join_client(
        &mut self,
        handoff: &WifiGroupHandoff,
    ) -> impl Future<Output = Result<(), Self::Error>> + Send {
        std::future::ready(self.attach_client(handoff))
    }

    fn teardown(&mut self) -> impl Future<Output = Result<(), Self::Error>> + Send {
        self.teardown();
        std::future::ready(Ok(()))
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum MacOsExistingHotspotGroupError {
    #[error("macOS Wi-Fi hotspot group is already active")]
    AlreadyActive,
    #[error("macOS did not expose a default Wi-Fi interface")]
    MissingWifiInterface,
    #[error("macOS Wi-Fi interface name is invalid")]
    InvalidInterfaceName,
    #[error("macOS Wi-Fi interface is not a compatible existing access point")]
    IncompatibleInterfaceMode,
    #[error("macOS Wi-Fi interface does not expose the expected existing SSID")]
    SsidMismatch,
    #[error("macOS existing hotspot has no usable IPv4 owner address")]
    MissingOwnerAddress,
    #[error("macOS Wi-Fi interface query failed")]
    InterfaceQueryFailed,
    #[error("macOS Wi-Fi interface query exceeded its output bound")]
    InterfaceQueryTooLarge,
    #[error("macOS Wi-Fi interface query timed out")]
    InterfaceQueryTimedOut,
    #[error("macOS Wi-Fi hotspot handoff has an unsupported transport: {0:?}")]
    UnsupportedTransport(LocalMeshTransportKind),
    #[error("macOS Wi-Fi hotspot handoff requires an owner profile")]
    InvalidHandoffRole,
    #[error("macOS Wi-Fi hotspot owner is not active")]
    OwnerNotActive,
    #[error("direct endpoint does not bind the existing macOS hotspot interface")]
    EndpointAddressMismatch,
    #[error("macOS Wi-Fi hotspot handoff targets the local interface")]
    ReflexiveDirectEndpoint,
}

fn interface_mode(raw: isize) -> Option<MacOsWifiInterfaceMode> {
    match raw {
        MACOS_HOST_AP_INTERFACE_MODE => Some(MacOsWifiInterfaceMode::HostAp),
        MACOS_STATION_INTERFACE_MODE => Some(MacOsWifiInterfaceMode::Station),
        _ => None,
    }
}

fn validate_interface_name(name: &str) -> Result<(), MacOsExistingHotspotGroupError> {
    if name.is_empty()
        || name.len() > MAX_MACOS_INTERFACE_NAME_BYTES
        || name.bytes().any(|byte| !byte.is_ascii_alphanumeric())
    {
        return Err(MacOsExistingHotspotGroupError::InvalidInterfaceName);
    }
    Ok(())
}

fn parse_interface_ipv4_addresses(
    output: &[u8],
) -> Result<Vec<IpAddr>, MacOsExistingHotspotGroupError> {
    if output.len() > MAX_MACOS_INTERFACE_QUERY_OUTPUT_BYTES {
        return Err(MacOsExistingHotspotGroupError::InterfaceQueryTooLarge);
    }
    let output = std::str::from_utf8(output)
        .map_err(|_| MacOsExistingHotspotGroupError::InterfaceQueryFailed)?;
    let addresses = output
        .lines()
        .filter_map(|line| {
            let mut fields = line.split_ascii_whitespace();
            (fields.next() == Some("inet"))
                .then(|| fields.next())
                .flatten()
                .and_then(|address| address.parse::<Ipv4Addr>().ok())
        })
        .filter(|address| {
            !address.is_unspecified()
                && !address.is_loopback()
                && !address.is_multicast()
                && !address.is_broadcast()
        })
        .map(IpAddr::V4)
        .collect::<Vec<_>>();
    if addresses.is_empty() {
        return Err(MacOsExistingHotspotGroupError::MissingOwnerAddress);
    }
    Ok(addresses)
}

#[cfg(target_os = "macos")]
fn current_snapshot() -> Result<MacOsHotspotSnapshot, MacOsExistingHotspotGroupError> {
    let client =
        WiFiClient::shared().map_err(|_| MacOsExistingHotspotGroupError::MissingWifiInterface)?;
    let interface = client
        .interface()
        .ok_or(MacOsExistingHotspotGroupError::MissingWifiInterface)?;
    let interface_name = interface
        .interface_name()
        .ok_or(MacOsExistingHotspotGroupError::MissingWifiInterface)?;
    validate_interface_name(&interface_name)?;
    let ssid = interface
        .ssid()
        .ok_or(MacOsExistingHotspotGroupError::SsidMismatch)?;
    let mode = interface_mode(interface.interface_mode().as_raw())
        .ok_or(MacOsExistingHotspotGroupError::IncompatibleInterfaceMode)?;
    let addresses = query_interface_addresses(&interface_name)?;
    Ok(MacOsHotspotSnapshot {
        ssid,
        mode,
        addresses,
    })
}

#[cfg(target_os = "macos")]
fn validate_attachment(
    snapshot: &MacOsHotspotSnapshot,
    expected_mode: MacOsWifiInterfaceMode,
    expected_ssid: &str,
) -> Result<(), MacOsExistingHotspotGroupError> {
    if snapshot.mode != expected_mode {
        return Err(MacOsExistingHotspotGroupError::IncompatibleInterfaceMode);
    }
    if snapshot.ssid != expected_ssid {
        return Err(MacOsExistingHotspotGroupError::SsidMismatch);
    }
    Ok(())
}

#[cfg(target_os = "macos")]
fn query_interface_addresses(
    interface: &str,
) -> Result<Vec<IpAddr>, MacOsExistingHotspotGroupError> {
    validate_interface_name(interface)?;
    let deadline = Instant::now() + MACOS_INTERFACE_QUERY_TIMEOUT;
    let mut child = Command::new("/sbin/ifconfig")
        .arg(interface)
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .spawn()
        .map_err(|_| MacOsExistingHotspotGroupError::InterfaceQueryFailed)?;
    let stdout = child
        .stdout
        .take()
        .ok_or(MacOsExistingHotspotGroupError::InterfaceQueryFailed)?;
    let (sender, receiver) = mpsc::sync_channel(1);
    let _reader = thread::spawn(move || {
        let mut output = Vec::with_capacity(MAX_MACOS_INTERFACE_QUERY_OUTPUT_BYTES + 1);
        let result = stdout
            .take((MAX_MACOS_INTERFACE_QUERY_OUTPUT_BYTES + 1) as u64)
            .read_to_end(&mut output)
            .map(|_| output);
        let _ = sender.send(result);
    });
    let output = receiver
        .recv_timeout(deadline.saturating_duration_since(Instant::now()))
        .map_err(|_| {
            terminate(&mut child);
            MacOsExistingHotspotGroupError::InterfaceQueryTimedOut
        })?
        .map_err(|_| {
            terminate(&mut child);
            MacOsExistingHotspotGroupError::InterfaceQueryFailed
        })?;
    if output.len() > MAX_MACOS_INTERFACE_QUERY_OUTPUT_BYTES {
        terminate(&mut child);
        return Err(MacOsExistingHotspotGroupError::InterfaceQueryTooLarge);
    }
    loop {
        match child.try_wait() {
            Ok(Some(status)) if status.success() => return parse_interface_ipv4_addresses(&output),
            Ok(Some(_)) | Err(_) => {
                return Err(MacOsExistingHotspotGroupError::InterfaceQueryFailed);
            }
            Ok(None) if Instant::now() >= deadline => {
                terminate(&mut child);
                return Err(MacOsExistingHotspotGroupError::InterfaceQueryTimedOut);
            }
            Ok(None) => thread::sleep(MACOS_INTERFACE_QUERY_POLL_INTERVAL),
        }
    }
}

#[cfg(target_os = "macos")]
fn terminate(child: &mut std::process::Child) {
    let _ = child.kill();
    let _ = child.wait();
}

#[cfg(test)]
mod tests {
    use super::{
        MacOsExistingHotspotGroupError, MacOsWifiInterfaceMode, interface_mode,
        parse_interface_ipv4_addresses, validate_interface_name,
    };

    #[test]
    fn accepts_only_host_ap_and_station_modes() {
        assert_eq!(interface_mode(3), Some(MacOsWifiInterfaceMode::HostAp));
        assert_eq!(interface_mode(1), Some(MacOsWifiInterfaceMode::Station));
        for mode in [isize::MIN, -1, 0, 2, 4, isize::MAX] {
            assert_eq!(interface_mode(mode), None);
        }
    }

    #[test]
    fn parses_only_usable_bounded_interface_ipv4_addresses() {
        assert_eq!(
            parse_interface_ipv4_addresses(b"en0: flags=\n\tinet 192.0.2.1 netmask 0xffffff00\n")
                .unwrap(),
            ["192.0.2.1".parse::<std::net::IpAddr>().unwrap()]
        );
        assert!(matches!(
            parse_interface_ipv4_addresses(b"\tinet 127.0.0.1 netmask 0xff000000\n"),
            Err(MacOsExistingHotspotGroupError::MissingOwnerAddress)
        ));
        assert!(matches!(
            parse_interface_ipv4_addresses(&vec![b'x'; 16_385]),
            Err(MacOsExistingHotspotGroupError::InterfaceQueryTooLarge)
        ));
    }

    #[test]
    fn rejects_unbounded_or_injectable_interface_names() {
        assert!(validate_interface_name("en0").is_ok());
        for name in ["", "en0;rm", "en/0", "x123456789012345"] {
            assert!(matches!(
                validate_interface_name(name),
                Err(MacOsExistingHotspotGroupError::InvalidInterfaceName)
            ));
        }
    }
}
