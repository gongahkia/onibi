use crate::{
    LocalTransportAvailability, linux_network_manager::MAX_LINUX_NETWORK_MANAGER_PROBE_OUTPUT_BYTES,
};

pub const MAX_LINUX_WIFI_HOTSPOT_PROBE_OUTPUT_BYTES: usize =
    MAX_LINUX_NETWORK_MANAGER_PROBE_OUTPUT_BYTES;

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct LinuxWifiHotspotCapabilityProbe;

impl LinuxWifiHotspotCapabilityProbe {
    #[must_use]
    pub const fn new() -> Self {
        Self
    }

    #[must_use]
    pub fn probe(self) -> LocalTransportAvailability {
        #[cfg(target_os = "linux")]
        {
            crate::linux_network_manager::nmcli_device_show("GENERAL.TYPE,WIFI-PROPERTIES.AP")
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
    if output.len() > MAX_LINUX_WIFI_HOTSPOT_PROBE_OUTPUT_BYTES {
        return LocalTransportAvailability::Unavailable;
    }
    let Ok(output) = std::str::from_utf8(output) else {
        return LocalTransportAvailability::Unavailable;
    };
    let mut wifi_device = false;
    for line in output.lines() {
        let Some((field, value)) = line.split_once(':') else {
            continue;
        };
        match field {
            "GENERAL.TYPE" => wifi_device = value == "wifi",
            "WIFI-PROPERTIES.AP" if wifi_device && value == "yes" => {
                return LocalTransportAvailability::Available;
            }
            _ => {}
        }
    }
    LocalTransportAvailability::Unavailable
}

#[cfg(test)]
mod tests {
    use super::{
        LinuxWifiHotspotCapabilityProbe, MAX_LINUX_WIFI_HOTSPOT_PROBE_OUTPUT_BYTES,
        parse_nmcli_device_output,
    };
    use crate::LocalTransportAvailability;

    #[test]
    fn accepts_an_access_point_capable_wifi_device() {
        assert_eq!(
            parse_nmcli_device_output(
                b"GENERAL.TYPE:ethernet\nWIFI-PROPERTIES.AP:no\nGENERAL.TYPE:wifi\nWIFI-PROPERTIES.AP:yes\n"
            ),
            LocalTransportAvailability::Available
        );
    }

    #[test]
    fn fails_closed_for_malformed_unavailable_and_oversized_output() {
        for output in [
            b"WIFI-PROPERTIES.AP:yes\nGENERAL.TYPE:wifi\n".as_slice(),
            b"GENERAL.TYPE:wifi\nWIFI-PROPERTIES.AP:no\n".as_slice(),
            b"GENERAL.TYPE:wifi\nWIFI-PROPERTIES.AP:yes\xff\n".as_slice(),
        ] {
            assert_eq!(
                parse_nmcli_device_output(output),
                LocalTransportAvailability::Unavailable
            );
        }
        assert_eq!(
            parse_nmcli_device_output(&vec![b'x'; MAX_LINUX_WIFI_HOTSPOT_PROBE_OUTPUT_BYTES + 1]),
            LocalTransportAvailability::Unavailable
        );
    }

    #[test]
    fn public_probe_fails_closed_off_linux() {
        #[cfg(not(target_os = "linux"))]
        assert_eq!(
            LinuxWifiHotspotCapabilityProbe::new().probe(),
            LocalTransportAvailability::Unavailable
        );

        #[cfg(target_os = "linux")]
        assert!(matches!(
            LinuxWifiHotspotCapabilityProbe::new().probe(),
            LocalTransportAvailability::Available | LocalTransportAvailability::Unavailable
        ));
    }
}
