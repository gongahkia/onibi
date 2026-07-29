use crate::BluetoothCapabilityStatus;

#[cfg(windows)]
use std::{
    sync::mpsc,
    time::{Duration, Instant},
};

#[cfg(windows)]
use windows::Devices::{Bluetooth::BluetoothAdapter, Enumeration::DeviceAccessInformation};

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct WindowsBluetoothCapabilityProbe;

impl WindowsBluetoothCapabilityProbe {
    #[must_use]
    pub const fn new() -> Self {
        Self
    }

    #[must_use]
    pub fn probe(self) -> BluetoothCapabilityStatus {
        #[cfg(windows)]
        {
            probe_windows()
        }
        #[cfg(not(windows))]
        BluetoothCapabilityStatus::Unavailable
    }
}

#[cfg(windows)]
fn probe_windows() -> BluetoothCapabilityStatus {
    const PROBE_TIMEOUT: Duration = Duration::from_secs(2);

    let Some(_runtime) = crate::windows_runtime::WindowsRuntime::initialize() else {
        return BluetoothCapabilityStatus::Unavailable;
    };
    let deadline = Instant::now() + PROBE_TIMEOUT;
    let Some(adapter) = wait_for_adapter(deadline) else {
        return BluetoothCapabilityStatus::Unavailable;
    };
    let access_status = match adapter
        .DeviceId()
        .ok()
        .and_then(|id| DeviceAccessInformation::CreateFromId(&id).ok())
        .and_then(|access| access.CurrentStatus().ok())
    {
        Some(status) => availability_from_access_status(status.0),
        None => return BluetoothCapabilityStatus::Unavailable,
    };
    if access_status != BluetoothCapabilityStatus::Available {
        return access_status;
    }
    let Some(radio_state) = wait_for_radio_state(&adapter, deadline) else {
        return BluetoothCapabilityStatus::Unavailable;
    };
    match availability_from_radio_state(radio_state) {
        BluetoothCapabilityStatus::Available => availability_from_adapter_capabilities(
            adapter.IsLowEnergySupported().unwrap_or(false),
            adapter.IsCentralRoleSupported().unwrap_or(false),
            adapter.IsPeripheralRoleSupported().unwrap_or(false),
        ),
        status => status,
    }
}

#[cfg(windows)]
fn wait_for_adapter(deadline: Instant) -> Option<BluetoothAdapter> {
    let operation = BluetoothAdapter::GetDefaultAsync().ok()?;
    let (sender, receiver) = mpsc::sync_channel(1);
    operation
        .when(move |result| {
            let _ = sender.send(result.ok());
        })
        .ok()?;
    receiver
        .recv_timeout(deadline.saturating_duration_since(Instant::now()))
        .ok()
        .flatten()
}

#[cfg(windows)]
fn wait_for_radio_state(adapter: &BluetoothAdapter, deadline: Instant) -> Option<i32> {
    let operation = adapter.GetRadioAsync().ok()?;
    let (sender, receiver) = mpsc::sync_channel(1);
    operation
        .when(move |result| {
            let _ = sender.send(result.ok().and_then(|radio| radio.State().ok()));
        })
        .ok()?;
    receiver
        .recv_timeout(deadline.saturating_duration_since(Instant::now()))
        .ok()
        .flatten()
        .map(|state| state.0)
}

#[cfg(any(test, windows))]
fn availability_from_access_status(status: i32) -> BluetoothCapabilityStatus {
    match status {
        1 => BluetoothCapabilityStatus::Available,
        2 | 3 => BluetoothCapabilityStatus::PermissionDenied,
        0 => BluetoothCapabilityStatus::PermissionNotDetermined,
        _ => BluetoothCapabilityStatus::Unavailable,
    }
}

#[cfg(any(test, windows))]
fn availability_from_radio_state(state: i32) -> BluetoothCapabilityStatus {
    match state {
        1 => BluetoothCapabilityStatus::Available,
        2 => BluetoothCapabilityStatus::PoweredOff,
        0 => BluetoothCapabilityStatus::Indeterminate,
        _ => BluetoothCapabilityStatus::Unavailable,
    }
}

#[cfg(any(test, windows))]
fn availability_from_adapter_capabilities(
    low_energy_supported: bool,
    central_role_supported: bool,
    peripheral_role_supported: bool,
) -> BluetoothCapabilityStatus {
    if low_energy_supported && central_role_supported && peripheral_role_supported {
        BluetoothCapabilityStatus::Available
    } else {
        BluetoothCapabilityStatus::Unsupported
    }
}

#[cfg(test)]
mod tests {
    use super::{
        WindowsBluetoothCapabilityProbe, availability_from_access_status,
        availability_from_adapter_capabilities, availability_from_radio_state,
    };
    use crate::BluetoothCapabilityStatus;

    #[test]
    fn maps_windows_access_statuses() {
        assert_eq!(
            availability_from_access_status(1),
            BluetoothCapabilityStatus::Available
        );
        for status in [2, 3] {
            assert_eq!(
                availability_from_access_status(status),
                BluetoothCapabilityStatus::PermissionDenied
            );
        }
        assert_eq!(
            availability_from_access_status(0),
            BluetoothCapabilityStatus::PermissionNotDetermined
        );
        assert_eq!(
            availability_from_access_status(i32::MAX),
            BluetoothCapabilityStatus::Unavailable
        );
    }

    #[test]
    fn maps_windows_radio_and_le_role_states() {
        assert_eq!(
            availability_from_radio_state(1),
            BluetoothCapabilityStatus::Available
        );
        assert_eq!(
            availability_from_radio_state(2),
            BluetoothCapabilityStatus::PoweredOff
        );
        assert_eq!(
            availability_from_radio_state(0),
            BluetoothCapabilityStatus::Indeterminate
        );
        assert_eq!(
            availability_from_radio_state(3),
            BluetoothCapabilityStatus::Unavailable
        );
        assert_eq!(
            availability_from_adapter_capabilities(true, true, true),
            BluetoothCapabilityStatus::Available
        );
        for capabilities in [
            (false, true, true),
            (true, false, true),
            (true, true, false),
        ] {
            assert_eq!(
                availability_from_adapter_capabilities(
                    capabilities.0,
                    capabilities.1,
                    capabilities.2,
                ),
                BluetoothCapabilityStatus::Unsupported
            );
        }
    }

    #[test]
    fn public_probe_fails_closed_off_windows() {
        #[cfg(not(windows))]
        assert_eq!(
            WindowsBluetoothCapabilityProbe::new().probe(),
            BluetoothCapabilityStatus::Unavailable
        );

        #[cfg(windows)]
        assert!(matches!(
            WindowsBluetoothCapabilityProbe::new().probe(),
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
