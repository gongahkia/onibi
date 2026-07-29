use crate::LocalTransportAvailability;

#[cfg(any(test, target_os = "linux"))]
use crate::linux_network_manager::MAX_LINUX_NETWORK_MANAGER_PROBE_OUTPUT_BYTES;

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct LinuxWifiDirectCapabilityProbe;

impl LinuxWifiDirectCapabilityProbe {
    #[must_use]
    pub const fn new() -> Self {
        Self
    }

    #[must_use]
    pub fn probe(self) -> LocalTransportAvailability {
        #[cfg(target_os = "linux")]
        {
            crate::linux_network_manager::nmcli_device_show("GENERAL.TYPE")
                .map_or(LocalTransportAvailability::Unavailable, |output| {
                    parse_nmcli_device_output(&output)
                })
        }
        #[cfg(not(target_os = "linux"))]
        LocalTransportAvailability::Unavailable
    }
}

#[cfg(any(test, target_os = "linux"))]
fn parse_nmcli_device_output(output: &[u8]) -> LocalTransportAvailability {
    if output.len() > MAX_LINUX_NETWORK_MANAGER_PROBE_OUTPUT_BYTES {
        return LocalTransportAvailability::Unavailable;
    }
    let Ok(output) = std::str::from_utf8(output) else {
        return LocalTransportAvailability::Unavailable;
    };
    if output.lines().any(|line| line == "GENERAL.TYPE:wifi-p2p") {
        LocalTransportAvailability::Available
    } else {
        LocalTransportAvailability::Unavailable
    }
}

#[cfg(test)]
mod tests {
    use super::{LinuxWifiDirectCapabilityProbe, parse_nmcli_device_output};
    use crate::LocalTransportAvailability;

    #[test]
    fn accepts_a_wifi_p2p_device() {
        assert_eq!(
            parse_nmcli_device_output(b"GENERAL.TYPE:wifi\nGENERAL.TYPE:wifi-p2p\n"),
            LocalTransportAvailability::Available
        );
    }

    #[test]
    fn fails_closed_for_non_p2p_or_malformed_output() {
        for output in [
            b"GENERAL.TYPE:wifi\n".as_slice(),
            b"GENERAL.TYPE:wifi-p2p\xff\n".as_slice(),
            b"GENERAL.TYPE:WIFI-P2P\n".as_slice(),
        ] {
            assert_eq!(
                parse_nmcli_device_output(output),
                LocalTransportAvailability::Unavailable
            );
        }
    }

    #[test]
    fn public_probe_fails_closed_off_linux() {
        #[cfg(not(target_os = "linux"))]
        assert_eq!(
            LinuxWifiDirectCapabilityProbe::new().probe(),
            LocalTransportAvailability::Unavailable
        );

        #[cfg(target_os = "linux")]
        assert!(matches!(
            LinuxWifiDirectCapabilityProbe::new().probe(),
            LocalTransportAvailability::Available | LocalTransportAvailability::Unavailable
        ));
    }
}
