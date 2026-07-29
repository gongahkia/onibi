use crate::LocalTransportAvailability;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum BluetoothCapabilityStatus {
    Available,
    PermissionDenied,
    PermissionNotDetermined,
    PoweredOff,
    Unsupported,
    Unavailable,
    Indeterminate,
}

impl BluetoothCapabilityStatus {
    #[must_use]
    pub const fn local_transport_availability(self) -> LocalTransportAvailability {
        match self {
            Self::Available => LocalTransportAvailability::Available,
            Self::PermissionDenied => LocalTransportAvailability::PermissionDenied,
            Self::PermissionNotDetermined
            | Self::PoweredOff
            | Self::Unsupported
            | Self::Unavailable
            | Self::Indeterminate => LocalTransportAvailability::Unavailable,
        }
    }
}

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct BluetoothCapabilityProbe;

impl BluetoothCapabilityProbe {
    #[must_use]
    pub const fn new() -> Self {
        Self
    }

    #[must_use]
    pub fn probe(self) -> BluetoothCapabilityStatus {
        #[cfg(target_os = "linux")]
        {
            crate::LinuxBluetoothCapabilityProbe::new().probe()
        }
        #[cfg(target_os = "macos")]
        {
            crate::MacOsBluetoothCapabilityProbe::new().probe()
        }
        #[cfg(windows)]
        {
            crate::WindowsBluetoothCapabilityProbe::new().probe()
        }
        #[cfg(not(any(target_os = "linux", target_os = "macos", windows)))]
        {
            BluetoothCapabilityStatus::Unavailable
        }
    }
}

#[cfg(test)]
mod tests {
    use super::{BluetoothCapabilityProbe, BluetoothCapabilityStatus};
    use crate::LocalTransportAvailability;

    #[test]
    fn only_a_confirmed_ready_adapter_is_transport_available() {
        assert_eq!(
            BluetoothCapabilityStatus::Available.local_transport_availability(),
            LocalTransportAvailability::Available
        );
        assert_eq!(
            BluetoothCapabilityStatus::PermissionDenied.local_transport_availability(),
            LocalTransportAvailability::PermissionDenied
        );
        for status in [
            BluetoothCapabilityStatus::PermissionNotDetermined,
            BluetoothCapabilityStatus::PoweredOff,
            BluetoothCapabilityStatus::Unsupported,
            BluetoothCapabilityStatus::Unavailable,
            BluetoothCapabilityStatus::Indeterminate,
        ] {
            assert_eq!(
                status.local_transport_availability(),
                LocalTransportAvailability::Unavailable
            );
        }
    }

    #[test]
    fn public_probe_reports_a_known_status() {
        assert!(matches!(
            BluetoothCapabilityProbe::new().probe(),
            BluetoothCapabilityStatus::Available
                | BluetoothCapabilityStatus::PermissionDenied
                | BluetoothCapabilityStatus::PermissionNotDetermined
                | BluetoothCapabilityStatus::PoweredOff
                | BluetoothCapabilityStatus::Unsupported
                | BluetoothCapabilityStatus::Unavailable
                | BluetoothCapabilityStatus::Indeterminate
        ));
    }
}
