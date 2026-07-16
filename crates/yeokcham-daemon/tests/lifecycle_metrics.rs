use std::{
    fs,
    io::{self, Write},
    path::PathBuf,
    sync::{
        Arc, Mutex,
        atomic::{AtomicU64, Ordering},
    },
};

use tracing::Dispatch;
use tracing_subscriber::fmt::MakeWriter;
use yeokcham_daemon::{DaemonLifecycleError, DaemonRuntime};
use yeokcham_protocol::ProtocolVersion;

static NEXT_TEST_PATH: AtomicU64 = AtomicU64::new(0);

#[derive(Clone, Default)]
struct Buffer(Arc<Mutex<Vec<u8>>>);

impl Write for Buffer {
    fn write(&mut self, bytes: &[u8]) -> io::Result<usize> {
        self.0.lock().unwrap().extend(bytes);
        Ok(bytes.len())
    }

    fn flush(&mut self) -> io::Result<()> {
        Ok(())
    }
}

impl<'writer> MakeWriter<'writer> for Buffer {
    type Writer = Self;

    fn make_writer(&'writer self) -> Self::Writer {
        self.clone()
    }
}

fn state_directory() -> PathBuf {
    let number = NEXT_TEST_PATH.fetch_add(1, Ordering::Relaxed);
    std::env::temp_dir().join(format!(
        "yeokcham-secret-lifecycle-state-{}-{number}",
        std::process::id()
    ))
}

#[test]
fn emits_one_redacted_start_and_stop_event_for_the_runtime_lifecycle() {
    let writer = Buffer::default();
    let dispatch = Dispatch::new(
        tracing_subscriber::fmt()
            .without_time()
            .with_target(true)
            .with_ansi(false)
            .with_writer(writer.clone())
            .finish(),
    );
    let state_directory = state_directory();

    tracing::dispatcher::with_default(&dispatch, || {
        let mut runtime = DaemonRuntime::start(ProtocolVersion::INITIAL, &state_directory).unwrap();
        assert!(matches!(
            DaemonRuntime::start(ProtocolVersion::INITIAL, &state_directory),
            Err(DaemonLifecycleError::AlreadyRunning)
        ));
        runtime.shutdown().unwrap();
        assert!(matches!(
            runtime.shutdown(),
            Err(DaemonLifecycleError::NotRunning)
        ));
    });

    let output = String::from_utf8(writer.0.lock().unwrap().clone()).unwrap();
    assert!(output.contains("yeokcham.daemon.lifecycle"));
    assert!(output.contains("event=\"started\""));
    assert!(output.contains("protocol_version=1"));
    assert!(output.contains("event=\"stopped\""));
    assert_eq!(output.matches("event=\"started\"").count(), 1);
    assert_eq!(output.matches("event=\"stopped\"").count(), 1);
    assert!(!output.contains(state_directory.to_str().unwrap()));
    fs::remove_dir_all(state_directory).unwrap();
}
