use std::{
    io::ErrorKind,
    net::{SocketAddr, UdpSocket},
};

use yeokcham_protocol::LocalMeshTransportKind;

use crate::{
    LinuxWifiDirectCapabilityProbe, LinuxWifiHotspotCapabilityProbe, LocalTransportAvailability,
};

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
            LocalMeshTransportKind::WifiHotspot => LinuxWifiHotspotCapabilityProbe::new().probe(),
            LocalMeshTransportKind::WifiDirect => LinuxWifiDirectCapabilityProbe::new().probe(),
            LocalMeshTransportKind::Bluetooth => LocalTransportAvailability::Unavailable,
        }
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
    use crate::LocalTransportAvailability;

    #[test]
    fn probes_lan_socket_capability_and_fails_closed_for_unimplemented_backends() {
        let probe = NativeTransportCapabilityProbe::new("127.0.0.1:0".parse().unwrap()).unwrap();
        assert_eq!(
            probe.probe(LocalMeshTransportKind::Lan),
            LocalTransportAvailability::Available
        );
        assert_eq!(
            probe.probe(LocalMeshTransportKind::Bluetooth),
            LocalTransportAvailability::Unavailable
        );
        #[cfg(not(target_os = "linux"))]
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
