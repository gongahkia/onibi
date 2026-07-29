use crate::LocalTransportAvailability;

#[cfg(target_os = "macos")]
use corewlan::WiFiClient;

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct MacOsWifiHotspotCapabilityProbe;

impl MacOsWifiHotspotCapabilityProbe {
    #[must_use]
    pub const fn new() -> Self {
        Self
    }

    #[must_use]
    pub fn probe(self) -> LocalTransportAvailability {
        #[cfg(target_os = "macos")]
        {
            WiFiClient::shared()
                .ok()
                .and_then(|client| client.interface())
                .map_or(LocalTransportAvailability::Unavailable, |interface| {
                    availability_from_interface_mode(interface.interface_mode().as_raw())
                })
        }
        #[cfg(not(target_os = "macos"))]
        LocalTransportAvailability::Unavailable
    }
}

#[cfg(any(test, target_os = "macos"))]
fn availability_from_interface_mode(mode: isize) -> LocalTransportAvailability {
    if mode == 3 {
        LocalTransportAvailability::Available
    } else {
        LocalTransportAvailability::Unavailable
    }
}

#[cfg(test)]
mod tests {
    use super::{MacOsWifiHotspotCapabilityProbe, availability_from_interface_mode};
    use crate::LocalTransportAvailability;

    #[test]
    fn accepts_a_host_access_point_interface_mode() {
        assert_eq!(
            availability_from_interface_mode(3),
            LocalTransportAvailability::Available
        );
    }

    #[test]
    fn fails_closed_for_non_access_point_and_unknown_interface_modes() {
        for mode in [isize::MIN, -1, 0, 1, 2, 4, isize::MAX] {
            assert_eq!(
                availability_from_interface_mode(mode),
                LocalTransportAvailability::Unavailable
            );
        }
    }

    #[test]
    fn public_probe_fails_closed_or_reports_active_hotspot() {
        #[cfg(not(target_os = "macos"))]
        assert_eq!(
            MacOsWifiHotspotCapabilityProbe::new().probe(),
            LocalTransportAvailability::Unavailable
        );

        #[cfg(target_os = "macos")]
        assert!(matches!(
            MacOsWifiHotspotCapabilityProbe::new().probe(),
            LocalTransportAvailability::Available | LocalTransportAvailability::Unavailable
        ));
    }
}
