use crate::LocalTransportAvailability;

#[cfg(windows)]
use windows::Networking::{
    Connectivity::NetworkInformation, NetworkOperators::NetworkOperatorTetheringManager,
};

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct WindowsWifiHotspotCapabilityProbe;

impl WindowsWifiHotspotCapabilityProbe {
    #[must_use]
    pub const fn new() -> Self {
        Self
    }

    #[must_use]
    pub fn probe(self) -> LocalTransportAvailability {
        #[cfg(windows)]
        {
            probe_windows()
        }
        #[cfg(not(windows))]
        LocalTransportAvailability::Unavailable
    }
}

#[cfg(windows)]
fn probe_windows() -> LocalTransportAvailability {
    let Some(_runtime) = crate::windows_runtime::WindowsRuntime::initialize() else {
        return LocalTransportAvailability::Unavailable;
    };
    let Ok(profile) = NetworkInformation::GetInternetConnectionProfile() else {
        return LocalTransportAvailability::Unavailable;
    };
    let Ok(capability) =
        NetworkOperatorTetheringManager::GetTetheringCapabilityFromConnectionProfile(&profile)
    else {
        return LocalTransportAvailability::Unavailable;
    };
    availability_from_tethering_capability(capability.0)
}

#[cfg(any(test, windows))]
const TETHERING_CAPABILITY_ENABLED: i32 = 0;
#[cfg(any(test, windows))]
const TETHERING_CAPABILITY_DISABLED_BY_GROUP_POLICY: i32 = 1;
#[cfg(any(test, windows))]
const TETHERING_CAPABILITY_DISABLED_BY_SYSTEM_CAPABILITY: i32 = 7;

#[cfg(any(test, windows))]
fn availability_from_tethering_capability(capability: i32) -> LocalTransportAvailability {
    match capability {
        TETHERING_CAPABILITY_ENABLED => LocalTransportAvailability::Available,
        TETHERING_CAPABILITY_DISABLED_BY_GROUP_POLICY
        | TETHERING_CAPABILITY_DISABLED_BY_SYSTEM_CAPABILITY => {
            LocalTransportAvailability::PermissionDenied
        }
        _ => LocalTransportAvailability::Unavailable,
    }
}

#[cfg(test)]
mod tests {
    use super::{
        TETHERING_CAPABILITY_DISABLED_BY_GROUP_POLICY,
        TETHERING_CAPABILITY_DISABLED_BY_SYSTEM_CAPABILITY, TETHERING_CAPABILITY_ENABLED,
        WindowsWifiHotspotCapabilityProbe, availability_from_tethering_capability,
    };
    use crate::LocalTransportAvailability;

    #[test]
    fn maps_enabled_tethering_capability_to_available() {
        assert_eq!(
            availability_from_tethering_capability(TETHERING_CAPABILITY_ENABLED),
            LocalTransportAvailability::Available
        );
    }

    #[test]
    fn maps_policy_and_system_capability_to_permission_denied() {
        for capability in [
            TETHERING_CAPABILITY_DISABLED_BY_GROUP_POLICY,
            TETHERING_CAPABILITY_DISABLED_BY_SYSTEM_CAPABILITY,
        ] {
            assert_eq!(
                availability_from_tethering_capability(capability),
                LocalTransportAvailability::PermissionDenied
            );
        }
    }

    #[test]
    fn fails_closed_for_non_enabled_and_unknown_capabilities() {
        for capability in [2, 3, 4, 5, 6, i32::MIN, i32::MAX] {
            assert_eq!(
                availability_from_tethering_capability(capability),
                LocalTransportAvailability::Unavailable
            );
        }
    }

    #[test]
    fn public_probe_fails_closed_off_windows() {
        #[cfg(not(windows))]
        assert_eq!(
            WindowsWifiHotspotCapabilityProbe::new().probe(),
            LocalTransportAvailability::Unavailable
        );

        #[cfg(windows)]
        assert!(matches!(
            WindowsWifiHotspotCapabilityProbe::new().probe(),
            LocalTransportAvailability::Available
                | LocalTransportAvailability::PermissionDenied
                | LocalTransportAvailability::Unavailable
        ));
    }
}
