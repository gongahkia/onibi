use crate::BluetoothCapabilityStatus;

#[cfg(target_os = "macos")]
use corebluetooth::CentralManager;

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct MacOsBluetoothCapabilityProbe;

impl MacOsBluetoothCapabilityProbe {
    #[must_use]
    pub const fn new() -> Self {
        Self
    }

    #[must_use]
    pub fn probe(self) -> BluetoothCapabilityStatus {
        #[cfg(target_os = "macos")]
        {
            availability_from_authorization(CentralManager::authorization().0)
        }
        #[cfg(not(target_os = "macos"))]
        BluetoothCapabilityStatus::Unavailable
    }
}

#[cfg(any(test, target_os = "macos"))]
fn availability_from_authorization(authorization: isize) -> BluetoothCapabilityStatus {
    match authorization {
        0 => BluetoothCapabilityStatus::PermissionNotDetermined,
        1 | 2 => BluetoothCapabilityStatus::PermissionDenied,
        3 => BluetoothCapabilityStatus::Indeterminate,
        _ => BluetoothCapabilityStatus::Unavailable,
    }
}

#[cfg(test)]
mod tests {
    use super::{MacOsBluetoothCapabilityProbe, availability_from_authorization};
    use crate::BluetoothCapabilityStatus;

    #[test]
    fn maps_core_bluetooth_authorization_without_prompting() {
        assert_eq!(
            availability_from_authorization(0),
            BluetoothCapabilityStatus::PermissionNotDetermined
        );
        for authorization in [1, 2] {
            assert_eq!(
                availability_from_authorization(authorization),
                BluetoothCapabilityStatus::PermissionDenied
            );
        }
        assert_eq!(
            availability_from_authorization(3),
            BluetoothCapabilityStatus::Indeterminate
        );
    }

    #[test]
    fn fails_closed_for_unknown_authorization() {
        for authorization in [isize::MIN, -1, 4, isize::MAX] {
            assert_eq!(
                availability_from_authorization(authorization),
                BluetoothCapabilityStatus::Unavailable
            );
        }
    }

    #[test]
    fn public_probe_fails_closed_or_reports_core_bluetooth_authorization() {
        #[cfg(not(target_os = "macos"))]
        assert_eq!(
            MacOsBluetoothCapabilityProbe::new().probe(),
            BluetoothCapabilityStatus::Unavailable
        );

        #[cfg(target_os = "macos")]
        assert!(matches!(
            MacOsBluetoothCapabilityProbe::new().probe(),
            BluetoothCapabilityStatus::PermissionDenied
                | BluetoothCapabilityStatus::PermissionNotDetermined
                | BluetoothCapabilityStatus::Indeterminate
                | BluetoothCapabilityStatus::Unavailable
        ));
    }
}
