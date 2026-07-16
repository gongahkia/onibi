use std::{
    fs,
    path::PathBuf,
    sync::atomic::{AtomicU64, Ordering},
};

use yeokcham_daemon::{
    ClientStateDirectory, ClientStateDirectoryError, DaemonLifecycleError, DaemonRuntime,
};
use yeokcham_protocol::ProtocolVersion;

static NEXT_TEST_DIRECTORY: AtomicU64 = AtomicU64::new(0);

fn state_directory() -> PathBuf {
    std::env::temp_dir().join(format!(
        "yeokcham-client-state-layout-{}-{}",
        std::process::id(),
        NEXT_TEST_DIRECTORY.fetch_add(1, Ordering::Relaxed)
    ))
}

#[test]
fn daemon_runtime_uses_the_public_layout_lock_path() {
    let root = state_directory();
    let layout = ClientStateDirectory::new(&root).unwrap();
    let mut runtime = DaemonRuntime::start(ProtocolVersion::INITIAL, layout.root()).unwrap();
    assert!(layout.lock_path().is_file());
    runtime.shutdown().unwrap();
    fs::remove_dir_all(root).unwrap();
}

#[test]
fn public_layout_and_runtime_fail_closed_for_invalid_roots() {
    assert_eq!(
        ClientStateDirectory::new("relative-state"),
        Err(ClientStateDirectoryError::NotAbsolute)
    );
    assert!(matches!(
        DaemonRuntime::start(ProtocolVersion::INITIAL, "relative-state"),
        Err(DaemonLifecycleError::InvalidStateDirectory(
            ClientStateDirectoryError::NotAbsolute
        ))
    ));
}
