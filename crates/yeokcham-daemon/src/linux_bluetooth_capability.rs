use crate::BluetoothCapabilityStatus;

#[cfg(any(test, target_os = "linux"))]
use crate::linux_network_manager::MAX_LINUX_NETWORK_MANAGER_PROBE_OUTPUT_BYTES;

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct LinuxBluetoothCapabilityProbe;

impl LinuxBluetoothCapabilityProbe {
    #[must_use]
    pub const fn new() -> Self {
        Self
    }

    #[must_use]
    pub fn probe(self) -> BluetoothCapabilityStatus {
        #[cfg(target_os = "linux")]
        {
            crate::linux_network_manager::bluetoothctl_show()
                .map_or(BluetoothCapabilityStatus::Unavailable, |output| {
                    parse_bluetoothctl_show_output(&output)
                })
        }
        #[cfg(not(target_os = "linux"))]
        BluetoothCapabilityStatus::Unavailable
    }
}

#[cfg(any(test, target_os = "linux"))]
fn parse_bluetoothctl_show_output(output: &[u8]) -> BluetoothCapabilityStatus {
    if output.len() > MAX_LINUX_NETWORK_MANAGER_PROBE_OUTPUT_BYTES {
        return BluetoothCapabilityStatus::Unavailable;
    }
    let Ok(output) = std::str::from_utf8(output) else {
        return BluetoothCapabilityStatus::Unavailable;
    };
    let mut powered = None;
    for line in output.lines() {
        let value = match line.trim() {
            "Powered: yes" => true,
            "Powered: no" => false,
            _ => continue,
        };
        if powered.replace(value).is_some() {
            return BluetoothCapabilityStatus::Unavailable;
        }
    }
    match powered {
        Some(true) => BluetoothCapabilityStatus::Available,
        Some(false) => BluetoothCapabilityStatus::PoweredOff,
        None => BluetoothCapabilityStatus::Unavailable,
    }
}

#[cfg(test)]
mod tests {
    use super::{LinuxBluetoothCapabilityProbe, parse_bluetoothctl_show_output};
    use crate::BluetoothCapabilityStatus;

    #[test]
    fn maps_powered_bluetoothctl_controller_state() {
        assert_eq!(
            parse_bluetoothctl_show_output(
                b"Controller 00:11:22:33:44:55 (public)\n\tPowered: yes\n"
            ),
            BluetoothCapabilityStatus::Available
        );
        assert_eq!(
            parse_bluetoothctl_show_output(
                b"Controller 00:11:22:33:44:55 (public)\n\tPowered: no\n"
            ),
            BluetoothCapabilityStatus::PoweredOff
        );
    }

    #[test]
    fn fails_closed_for_missing_duplicate_or_malformed_power_state() {
        for output in [
            b"No default controller available\n".as_slice(),
            b"Powered: yes\nPowered: no\n".as_slice(),
            b"Powered: YES\n".as_slice(),
            b"Powered: yes\xff\n".as_slice(),
        ] {
            assert_eq!(
                parse_bluetoothctl_show_output(output),
                BluetoothCapabilityStatus::Unavailable
            );
        }
    }

    #[test]
    fn public_probe_fails_closed_off_linux() {
        #[cfg(not(target_os = "linux"))]
        assert_eq!(
            LinuxBluetoothCapabilityProbe::new().probe(),
            BluetoothCapabilityStatus::Unavailable
        );

        #[cfg(target_os = "linux")]
        assert!(matches!(
            LinuxBluetoothCapabilityProbe::new().probe(),
            BluetoothCapabilityStatus::Available
                | BluetoothCapabilityStatus::PoweredOff
                | BluetoothCapabilityStatus::Unavailable
        ));
    }
}
