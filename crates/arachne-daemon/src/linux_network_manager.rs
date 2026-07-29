#[cfg(target_os = "linux")]
use std::{
    io::Read,
    process::{Command, Stdio},
    sync::mpsc,
    thread,
    time::{Duration, Instant},
};

pub const MAX_LINUX_NETWORK_MANAGER_PROBE_OUTPUT_BYTES: usize = 16_384;

#[cfg(target_os = "linux")]
pub fn nmcli_device_show(fields: &str) -> Option<Vec<u8>> {
    run_command("nmcli", &["--terse", "--fields", fields, "device", "show"])
}

#[cfg(target_os = "linux")]
pub fn bluetoothctl_show() -> Option<Vec<u8>> {
    run_command("bluetoothctl", &["show"])
}

#[cfg(target_os = "linux")]
fn run_command(command: &str, args: &[&str]) -> Option<Vec<u8>> {
    const PROBE_TIMEOUT: Duration = Duration::from_secs(2);
    const WAIT_POLL_INTERVAL: Duration = Duration::from_millis(10);

    let deadline = Instant::now() + PROBE_TIMEOUT;
    let Ok(mut child) = Command::new(command)
        .args(args)
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .spawn()
    else {
        return None;
    };
    let Some(stdout) = child.stdout.take() else {
        terminate(&mut child);
        return None;
    };
    let (sender, receiver) = mpsc::sync_channel(1);
    let _reader = thread::spawn(move || {
        let mut output = Vec::with_capacity(MAX_LINUX_NETWORK_MANAGER_PROBE_OUTPUT_BYTES + 1);
        let result = stdout
            .take((MAX_LINUX_NETWORK_MANAGER_PROBE_OUTPUT_BYTES + 1) as u64)
            .read_to_end(&mut output)
            .map(|_| output);
        let _ = sender.send(result);
    });
    let output = match receiver.recv_timeout(deadline.saturating_duration_since(Instant::now())) {
        Ok(Ok(output)) if output.len() <= MAX_LINUX_NETWORK_MANAGER_PROBE_OUTPUT_BYTES => output,
        Ok(Ok(_) | Err(_)) | Err(_) => {
            terminate(&mut child);
            return None;
        }
    };
    loop {
        match child.try_wait() {
            Ok(Some(status)) if status.success() => return Some(output),
            Ok(Some(_)) | Err(_) => return None,
            Ok(None) if Instant::now() >= deadline => {
                terminate(&mut child);
                return None;
            }
            Ok(None) => thread::sleep(WAIT_POLL_INTERVAL),
        }
    }
}

#[cfg(target_os = "linux")]
fn terminate(child: &mut std::process::Child) {
    let _ = child.kill();
    let _ = child.wait();
}
