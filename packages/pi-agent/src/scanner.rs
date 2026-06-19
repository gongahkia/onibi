use std::fmt::{Display, Formatter};
use std::io;
use std::process::{Command, ExitStatus};
use std::thread;
use std::time::{Duration, Instant};

#[derive(Debug, Clone, Copy, Default, PartialEq, Eq)]
pub struct ScannerLimits {
    pub max_requests_per_second: Option<u32>,
    pub max_concurrent_targets: Option<usize>,
    pub max_scan_duration_seconds: Option<u64>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ScannerLimitError {
    ZeroLimit(&'static str),
    TooManyTargets { requested: usize, limit: usize },
    UnsupportedRateLimit(String),
    UnsupportedTargetConcurrency(String),
}

#[derive(Debug)]
pub enum ScannerRunError {
    Io(io::Error),
    Limit(ScannerLimitError),
}

#[derive(Debug)]
pub struct ScannerRunOutcome {
    pub status: ExitStatus,
    pub timed_out: bool,
    pub enforced_args: Vec<String>,
}

impl ScannerLimits {
    pub fn validate(self) -> Result<Self, ScannerLimitError> {
        if self.max_requests_per_second == Some(0) {
            return Err(ScannerLimitError::ZeroLimit("max_requests_per_second"));
        }
        if self.max_concurrent_targets == Some(0) {
            return Err(ScannerLimitError::ZeroLimit("max_concurrent_targets"));
        }
        if self.max_scan_duration_seconds == Some(0) {
            return Err(ScannerLimitError::ZeroLimit("max_scan_duration_seconds"));
        }
        Ok(self)
    }

    pub fn enforce_target_count(self, requested: usize) -> Result<(), ScannerLimitError> {
        if let Some(limit) = self.max_concurrent_targets {
            if requested > limit {
                return Err(ScannerLimitError::TooManyTargets { requested, limit });
            }
        }
        Ok(())
    }
}

pub fn scanner_enforced_args(
    scanner: &str,
    limits: &ScannerLimits,
) -> Result<Vec<String>, ScannerLimitError> {
    let mut args = Vec::new();

    if let Some(rate) = limits.max_requests_per_second {
        match scanner {
            "nuclei" => args.extend(["-rate-limit".to_string(), rate.to_string()]),
            "nmap" => args.extend(["--max-rate".to_string(), rate.to_string()]),
            "zap" => return Err(ScannerLimitError::UnsupportedRateLimit(scanner.to_string())),
            _ => {}
        }
    }
    if let Some(concurrent) = limits.max_concurrent_targets {
        match scanner {
            "nuclei" => args.extend(["-bulk-size".to_string(), concurrent.to_string()]),
            "nmap" => args.extend(["--max-hostgroup".to_string(), concurrent.to_string()]),
            "zap" if concurrent > 1 => {
                return Err(ScannerLimitError::UnsupportedTargetConcurrency(
                    scanner.to_string(),
                ))
            }
            _ => {}
        }
    }
    if let Some(seconds) = limits.max_scan_duration_seconds {
        if scanner == "nmap" {
            args.extend(["--host-timeout".to_string(), format!("{seconds}s")]);
        }
    }

    Ok(args)
}

pub fn run_scanner_with_limits(
    scanner: &str,
    scanner_bin: &str,
    scanner_args: &[String],
    targets: &[String],
    limits: ScannerLimits,
) -> Result<ScannerRunOutcome, ScannerRunError> {
    let limits = limits.validate()?;
    limits.enforce_target_count(targets.len())?;
    let enforced_args = scanner_enforced_args(scanner, &limits)?;
    let mut child = Command::new(scanner_bin)
        .args(scanner_args)
        .args(&enforced_args)
        .args(targets)
        .spawn()?;

    if let Some(seconds) = limits.max_scan_duration_seconds {
        let timeout = Duration::from_secs(seconds);
        let started = Instant::now();
        loop {
            if let Some(status) = child.try_wait()? {
                return Ok(ScannerRunOutcome {
                    status,
                    timed_out: false,
                    enforced_args,
                });
            }
            if started.elapsed() >= timeout {
                child.kill()?;
                let status = child.wait()?;
                return Ok(ScannerRunOutcome {
                    status,
                    timed_out: true,
                    enforced_args,
                });
            }
            thread::sleep(Duration::from_millis(25));
        }
    }

    let status = child.wait()?;
    Ok(ScannerRunOutcome {
        status,
        timed_out: false,
        enforced_args,
    })
}

impl Display for ScannerLimitError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            ScannerLimitError::ZeroLimit(name) => write!(formatter, "{name} must be positive"),
            ScannerLimitError::TooManyTargets { requested, limit } => write!(
                formatter,
                "scanner target concurrency limit exceeded: requested {requested}, limit {limit}"
            ),
            ScannerLimitError::UnsupportedRateLimit(scanner) => {
                write!(
                    formatter,
                    "{scanner} does not support max_requests_per_second"
                )
            }
            ScannerLimitError::UnsupportedTargetConcurrency(scanner) => {
                write!(
                    formatter,
                    "{scanner} does not support max_concurrent_targets > 1"
                )
            }
        }
    }
}

impl Display for ScannerRunError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            ScannerRunError::Io(error) => write!(formatter, "{error}"),
            ScannerRunError::Limit(error) => write!(formatter, "{error}"),
        }
    }
}

impl std::error::Error for ScannerLimitError {}
impl std::error::Error for ScannerRunError {}

impl From<io::Error> for ScannerRunError {
    fn from(error: io::Error) -> Self {
        ScannerRunError::Io(error)
    }
}

impl From<ScannerLimitError> for ScannerRunError {
    fn from(error: ScannerLimitError) -> Self {
        ScannerRunError::Limit(error)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;
    #[cfg(unix)]
    use std::os::unix::fs::PermissionsExt;
    use std::time::{SystemTime, UNIX_EPOCH};

    #[test]
    fn scanner_limits_add_native_flags() {
        let limits = ScannerLimits {
            max_requests_per_second: Some(7),
            max_concurrent_targets: Some(2),
            max_scan_duration_seconds: Some(9),
        };

        assert_eq!(
            scanner_enforced_args("nuclei", &limits).expect("nuclei flags"),
            vec!["-rate-limit", "7", "-bulk-size", "2"]
        );
        assert_eq!(
            scanner_enforced_args("nmap", &limits).expect("nmap flags"),
            vec![
                "--max-rate",
                "7",
                "--max-hostgroup",
                "2",
                "--host-timeout",
                "9s"
            ]
        );
        assert!(scanner_enforced_args("zap", &limits).is_err());
    }

    #[test]
    fn scanner_limits_refuse_invalid_and_excessive_targets() {
        assert!(ScannerLimits {
            max_requests_per_second: Some(0),
            ..ScannerLimits::default()
        }
        .validate()
        .is_err());
        assert!(ScannerLimits {
            max_concurrent_targets: Some(1),
            ..ScannerLimits::default()
        }
        .enforce_target_count(2)
        .is_err());
    }

    #[cfg(unix)]
    #[test]
    fn scanner_runner_kills_process_after_duration_limit() {
        let dir = temp_root("scanner-timeout");
        fs::create_dir_all(&dir).expect("create temp dir");
        let script = dir.join("scanner.sh");
        fs::write(&script, "#!/bin/sh\nsleep 5\n").expect("write script");
        fs::set_permissions(&script, fs::Permissions::from_mode(0o755)).expect("chmod script");

        let started = Instant::now();
        let outcome = run_scanner_with_limits(
            "nuclei",
            script.to_str().expect("script path"),
            &[],
            &["https://app.example.test".to_string()],
            ScannerLimits {
                max_scan_duration_seconds: Some(1),
                ..ScannerLimits::default()
            },
        )
        .expect("run scanner");

        assert!(outcome.timed_out);
        assert!(started.elapsed() < Duration::from_secs(3));
        fs::remove_dir_all(dir).ok();
    }

    #[cfg(unix)]
    #[test]
    fn scanner_runner_passes_enforced_args_to_process() {
        let dir = temp_root("scanner-args");
        fs::create_dir_all(&dir).expect("create temp dir");
        let script = dir.join("scanner.sh");
        let output = dir.join("args.txt");
        fs::write(
            &script,
            format!("#!/bin/sh\nprintf '%s\\n' \"$@\" > {}\n", output.display()),
        )
        .expect("write script");
        fs::set_permissions(&script, fs::Permissions::from_mode(0o755)).expect("chmod script");

        let outcome = run_scanner_with_limits(
            "nuclei",
            script.to_str().expect("script path"),
            &["-jsonl".to_string()],
            &["https://app.example.test".to_string()],
            ScannerLimits {
                max_requests_per_second: Some(3),
                max_concurrent_targets: Some(1),
                ..ScannerLimits::default()
            },
        )
        .expect("run scanner");

        assert!(outcome.status.success());
        assert_eq!(
            outcome.enforced_args,
            vec!["-rate-limit", "3", "-bulk-size", "1"]
        );
        let args = fs::read_to_string(output).expect("read args");
        assert_eq!(
            args.lines().collect::<Vec<_>>(),
            vec![
                "-jsonl",
                "-rate-limit",
                "3",
                "-bulk-size",
                "1",
                "https://app.example.test"
            ]
        );
        fs::remove_dir_all(dir).ok();
    }

    fn temp_root(name: &str) -> std::path::PathBuf {
        let nonce = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("time after epoch")
            .as_nanos();
        std::env::temp_dir().join(format!("kelp-pi-{name}-{}-{nonce}", std::process::id()))
    }
}
