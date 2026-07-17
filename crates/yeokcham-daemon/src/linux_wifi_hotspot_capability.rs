use crate::LocalTransportAvailability;

pub const MAX_LINUX_WIFI_HOTSPOT_PROBE_OUTPUT_BYTES: usize = 16_384;

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
            probe_nmcli()
        }
        #[cfg(not(target_os = "linux"))]
        LocalTransportAvailability::Unavailable
    }
}

#[cfg(target_os = "linux")]
fn probe_nmcli() -> LocalTransportAvailability {
    use std::{
        io::Read,
        process::{Command, Stdio},
        sync::mpsc,
        thread,
        time::{Duration, Instant},
    };

    const PROBE_TIMEOUT: Duration = Duration::from_secs(2);
    const WAIT_POLL_INTERVAL: Duration = Duration::from_millis(10);

    let deadline = Instant::now() + PROBE_TIMEOUT;
    let Ok(mut child) = Command::new("nmcli")
        .args([
            "--terse",
            "--fields",
            "GENERAL.TYPE,WIFI-PROPERTIES.AP",
            "device",
            "show",
        ])
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .spawn()
    else {
        return LocalTransportAvailability::Unavailable;
    };
    let Some(stdout) = child.stdout.take() else {
        terminate(&mut child);
        return LocalTransportAvailability::Unavailable;
    };
    let (sender, receiver) = mpsc::sync_channel(1);
    let _reader = thread::spawn(move || {
        let mut output = Vec::with_capacity(MAX_LINUX_WIFI_HOTSPOT_PROBE_OUTPUT_BYTES + 1);
        let result = stdout
            .take((MAX_LINUX_WIFI_HOTSPOT_PROBE_OUTPUT_BYTES + 1) as u64)
            .read_to_end(&mut output)
            .map(|_| output);
        let _ = sender.send(result);
    });
    let output = match receiver.recv_timeout(deadline.saturating_duration_since(Instant::now())) {
        Ok(Ok(output)) if output.len() <= MAX_LINUX_WIFI_HOTSPOT_PROBE_OUTPUT_BYTES => output,
        Ok(Ok(_) | Err(_)) | Err(_) => {
            terminate(&mut child);
            return LocalTransportAvailability::Unavailable;
        }
    };
    loop {
        match child.try_wait() {
            Ok(Some(status)) if status.success() => break,
            Ok(Some(_)) | Err(_) => return LocalTransportAvailability::Unavailable,
            Ok(None) if Instant::now() >= deadline => {
                terminate(&mut child);
                return LocalTransportAvailability::Unavailable;
            }
            Ok(None) => thread::sleep(WAIT_POLL_INTERVAL),
        }
    }
    parse_nmcli_device_output(&output)
}

#[cfg(target_os = "linux")]
fn terminate(child: &mut std::process::Child) {
    let _ = child.kill();
    let _ = child.wait();
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
