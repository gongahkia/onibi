#![cfg_attr(windows, allow(unsafe_code))]

use crate::LocalTransportAvailability;

#[cfg(windows)]
use windows::Win32::{
    Foundation::HANDLE,
    NetworkManagement::WiFi::{WFDCloseHandle, WFDOpenHandle},
};

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct WindowsWifiDirectCapabilityProbe;

impl WindowsWifiDirectCapabilityProbe {
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
    const WFD_CLIENT_VERSION: u32 = 1;

    let mut negotiated_version = 0;
    let mut client_handle = HANDLE::default();
    let status = unsafe {
        WFDOpenHandle(
            WFD_CLIENT_VERSION,
            &raw mut negotiated_version,
            &raw mut client_handle,
        )
    }; // safety: output pointers are valid for this synchronous API call
    let availability = availability_from_wfd_open_status(status);
    if availability != LocalTransportAvailability::Available {
        return availability;
    }
    if client_handle.is_invalid() {
        return LocalTransportAvailability::Unavailable;
    }
    let close_status = unsafe { WFDCloseHandle(client_handle) }; // safety: WFDOpenHandle returned this valid handle
    if close_status == 0 {
        LocalTransportAvailability::Available
    } else {
        LocalTransportAvailability::Unavailable
    }
}

#[cfg(any(test, windows))]
fn availability_from_wfd_open_status(status: u32) -> LocalTransportAvailability {
    const ERROR_ACCESS_DENIED: u32 = 5;
    const ERROR_NOT_SUPPORTED: u32 = 50;
    const ERROR_CALL_NOT_IMPLEMENTED: u32 = 120;
    const ERROR_ACCESS_DISABLED_BY_POLICY: u32 = 1260;

    match status {
        0 => LocalTransportAvailability::Available,
        status if status == ERROR_ACCESS_DENIED || status == ERROR_ACCESS_DISABLED_BY_POLICY => {
            LocalTransportAvailability::PermissionDenied
        }
        status if status == ERROR_NOT_SUPPORTED || status == ERROR_CALL_NOT_IMPLEMENTED => {
            LocalTransportAvailability::Unavailable
        }
        _ => LocalTransportAvailability::Unavailable,
    }
}

#[cfg(test)]
mod tests {
    use super::{WindowsWifiDirectCapabilityProbe, availability_from_wfd_open_status};
    use crate::LocalTransportAvailability;

    #[test]
    fn maps_wfd_service_open_statuses_conservatively() {
        assert_eq!(
            availability_from_wfd_open_status(0),
            LocalTransportAvailability::Available
        );
        for status in [5, 1260] {
            assert_eq!(
                availability_from_wfd_open_status(status),
                LocalTransportAvailability::PermissionDenied
            );
        }
        for status in [50, 120, 1, u32::MAX] {
            assert_eq!(
                availability_from_wfd_open_status(status),
                LocalTransportAvailability::Unavailable
            );
        }
    }

    #[test]
    fn public_probe_fails_closed_off_windows() {
        #[cfg(not(windows))]
        assert_eq!(
            WindowsWifiDirectCapabilityProbe::new().probe(),
            LocalTransportAvailability::Unavailable
        );

        #[cfg(windows)]
        assert!(matches!(
            WindowsWifiDirectCapabilityProbe::new().probe(),
            LocalTransportAvailability::Available
                | LocalTransportAvailability::PermissionDenied
                | LocalTransportAvailability::Unavailable
        ));
    }
}
