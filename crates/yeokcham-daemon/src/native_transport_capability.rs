use std::{
    io::ErrorKind,
    net::{SocketAddr, UdpSocket},
};

use yeokcham_protocol::LocalMeshTransportKind;

#[cfg(target_os = "linux")]
use crate::LinuxWifiDirectCapabilityProbe;
#[cfg(target_os = "linux")]
use crate::LinuxWifiHotspotCapabilityProbe;
#[cfg(target_os = "macos")]
use crate::MacOsWifiHotspotCapabilityProbe;
use crate::{BluetoothCapabilityProbe, BluetoothCapabilityStatus, LocalTransportAvailability};
#[cfg(target_os = "windows")]
use crate::{WindowsWifiDirectCapabilityProbe, WindowsWifiHotspotCapabilityProbe};

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct NativeTransportCapabilityProbe {
    lan_probe_address: SocketAddr,
}

impl NativeTransportCapabilityProbe {
    pub fn new(lan_probe_address: SocketAddr) -> Result<Self, NativeTransportCapabilityProbeError> {
        if lan_probe_address.port() != 0 {
            return Err(NativeTransportCapabilityProbeError::NonEphemeralLanProbePort);
        }
        Ok(Self { lan_probe_address })
    }

    #[must_use]
    pub const fn lan_probe_address(self) -> SocketAddr {
        self.lan_probe_address
    }

    #[must_use]
    pub fn probe(self, kind: LocalMeshTransportKind) -> LocalTransportAvailability {
        match kind {
            LocalMeshTransportKind::Lan => match UdpSocket::bind(self.lan_probe_address) {
                Ok(socket) => {
                    drop(socket);
                    LocalTransportAvailability::Available
                }
                Err(error) if error.kind() == ErrorKind::PermissionDenied => {
                    LocalTransportAvailability::PermissionDenied
                }
                Err(_) => LocalTransportAvailability::Unavailable,
            },
            LocalMeshTransportKind::WifiHotspot => probe_wifi_hotspot_capability(),
            LocalMeshTransportKind::WifiDirect => probe_wifi_direct_capability(),
            LocalMeshTransportKind::Bluetooth => {
                self.probe_bluetooth().local_transport_availability()
            }
        }
    }

    #[must_use]
    pub fn probe_bluetooth(self) -> BluetoothCapabilityStatus {
        BluetoothCapabilityProbe::new().probe()
    }
}

fn probe_wifi_direct_capability() -> LocalTransportAvailability {
    #[cfg(target_os = "linux")]
    {
        LinuxWifiDirectCapabilityProbe::new().probe()
    }
    #[cfg(target_os = "windows")]
    {
        WindowsWifiDirectCapabilityProbe::new().probe()
    }
    #[cfg(not(any(target_os = "linux", target_os = "windows")))]
    {
        LocalTransportAvailability::Unavailable
    }
}

fn probe_wifi_hotspot_capability() -> LocalTransportAvailability {
    #[cfg(target_os = "linux")]
    {
        LinuxWifiHotspotCapabilityProbe::new().probe()
    }
    #[cfg(target_os = "windows")]
    {
        WindowsWifiHotspotCapabilityProbe::new().probe()
    }
    #[cfg(target_os = "macos")]
    {
        MacOsWifiHotspotCapabilityProbe::new().probe()
    }
    #[cfg(not(any(target_os = "linux", target_os = "macos", target_os = "windows")))]
    {
        LocalTransportAvailability::Unavailable
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, thiserror::Error)]
pub enum NativeTransportCapabilityProbeError {
    #[error("native LAN transport probing requires an ephemeral port")]
    NonEphemeralLanProbePort,
}

#[cfg(test)]
mod tests {
    use yeokcham_protocol::LocalMeshTransportKind;

    use super::{NativeTransportCapabilityProbe, NativeTransportCapabilityProbeError};
    use crate::{BluetoothCapabilityStatus, LocalTransportAvailability};

    #[test]
    fn probes_lan_socket_capability_and_maps_bluetooth_status_conservatively() {
        let probe = NativeTransportCapabilityProbe::new("127.0.0.1:0".parse().unwrap()).unwrap();
        assert_eq!(
            probe.probe(LocalMeshTransportKind::Lan),
            LocalTransportAvailability::Available
        );
        let bluetooth_status = probe.probe_bluetooth();
        assert!(matches!(
            bluetooth_status,
            BluetoothCapabilityStatus::Available
                | BluetoothCapabilityStatus::PermissionDenied
                | BluetoothCapabilityStatus::PermissionNotDetermined
                | BluetoothCapabilityStatus::PoweredOff
                | BluetoothCapabilityStatus::Unsupported
                | BluetoothCapabilityStatus::Unavailable
                | BluetoothCapabilityStatus::Indeterminate
        ));
        assert_eq!(
            probe.probe(LocalMeshTransportKind::Bluetooth),
            bluetooth_status.local_transport_availability()
        );
        #[cfg(not(any(target_os = "linux", target_os = "macos", target_os = "windows")))]
        assert_eq!(
            probe.probe(LocalMeshTransportKind::WifiHotspot),
            LocalTransportAvailability::Unavailable
        );
    }

    #[test]
    fn rejects_shared_ports_and_reports_unbindable_lan_addresses_as_unavailable() {
        assert_eq!(
            NativeTransportCapabilityProbe::new("127.0.0.1:1".parse().unwrap()),
            Err(NativeTransportCapabilityProbeError::NonEphemeralLanProbePort)
        );
        let probe = NativeTransportCapabilityProbe::new("192.0.2.1:0".parse().unwrap()).unwrap();
        assert_eq!(
            probe.probe(LocalMeshTransportKind::Lan),
            LocalTransportAvailability::Unavailable
        );
    }
}
