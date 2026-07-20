#[cfg(not(windows))]
fn main() -> std::process::ExitCode {
    std::process::ExitCode::FAILURE
}

#[cfg(windows)]
fn main() -> std::process::ExitCode {
    windows_service_host::run()
}

#[cfg(windows)]
mod windows_service_host {
    use std::{
        ffi::OsString,
        path::PathBuf,
        process::ExitCode,
        sync::{Arc, Mutex, mpsc},
        time::Duration,
    };

    use windows_service::{
        define_windows_service,
        service::{
            ServiceControl, ServiceControlAccept, ServiceExitCode, ServiceState, ServiceStatus,
            ServiceType,
        },
        service_control_handler::{self, ServiceControlHandlerResult, ServiceStatusHandle},
        service_dispatcher,
    };
    use yeokcham_relay::{RelayRuntimeConfig, RelayServer};

    const SERVICE_NAME: &str = "YeokchamRelay";
    const SERVICE_TYPE: ServiceType = ServiceType::OWN_PROCESS;
    const CONFIG_PATH: &str = r"C:\ProgramData\Yeokcham\relay.conf";
    const IDENTITY_PATH: &str = r"C:\ProgramData\Yeokcham\relay.identity";
    const HEALTH_ADDRESS: &str = "127.0.0.1:8080";
    const METRICS_ADDRESS: &str = "127.0.0.1:8081";
    const GRACEFUL_SHUTDOWN_TIMEOUT: Duration = Duration::from_secs(30);
    const PENDING_WAIT_HINT: Duration = Duration::from_secs(30);
    const INITIALIZATION_FAILURE: u32 = 1;
    const RUNTIME_FAILURE: u32 = 2;

    struct ControlState {
        status_handle: Option<ServiceStatusHandle>,
        stop_requested: bool,
    }

    impl ControlState {
        const fn new() -> Self {
            Self {
                status_handle: None,
                stop_requested: false,
            }
        }
    }

    pub fn run() -> ExitCode {
        match service_dispatcher::start(SERVICE_NAME, ffi_service_main) {
            Ok(()) => ExitCode::SUCCESS,
            Err(_) => ExitCode::FAILURE,
        }
    }

    define_windows_service!(ffi_service_main, service_main);

    fn service_main(_arguments: Vec<OsString>) {
        run_service();
    }

    fn run_service() {
        let (shutdown_sender, shutdown_receiver) = mpsc::sync_channel(1);
        let control_state = Arc::new(Mutex::new(ControlState::new()));
        let handler_state = Arc::clone(&control_state);
        let event_handler = move |control_event| match control_event {
            ServiceControl::Interrogate => ServiceControlHandlerResult::NoError,
            ServiceControl::Stop | ServiceControl::Shutdown => {
                let Ok(mut state) = handler_state.lock() else {
                    return ServiceControlHandlerResult::NoError;
                };
                state.stop_requested = true;
                if let Some(status_handle) = &state.status_handle {
                    let _ = status_handle.set_service_status(status(
                        ServiceState::StopPending,
                        ServiceControlAccept::empty(),
                        ServiceExitCode::Win32(0),
                        1,
                        PENDING_WAIT_HINT,
                    ));
                }
                let _ = shutdown_sender.try_send(());
                ServiceControlHandlerResult::NoError
            }
            _ => ServiceControlHandlerResult::NotImplemented,
        };

        let Ok(status_handle) = service_control_handler::register(SERVICE_NAME, event_handler)
        else {
            return;
        };
        let Ok(mut state) = control_state.lock() else {
            return;
        };
        state.status_handle = Some(status_handle);
        let Some(status_handle) = &state.status_handle else {
            return;
        };
        if status_handle
            .set_service_status(status(
                ServiceState::StartPending,
                ServiceControlAccept::empty(),
                ServiceExitCode::Win32(0),
                1,
                PENDING_WAIT_HINT,
            ))
            .is_err()
        {
            return;
        }
        drop(state);

        let Ok(runtime) = tokio::runtime::Builder::new_multi_thread()
            .enable_all()
            .build()
        else {
            report_stopped(
                &control_state,
                ServiceExitCode::ServiceSpecific(INITIALIZATION_FAILURE),
            );
            return;
        };
        let Ok(relay_runtime) = relay_runtime_config() else {
            report_stopped(
                &control_state,
                ServiceExitCode::ServiceSpecific(INITIALIZATION_FAILURE),
            );
            return;
        };
        let Ok(relay) = runtime.block_on(relay_runtime.bind()) else {
            report_stopped(
                &control_state,
                ServiceExitCode::ServiceSpecific(INITIALIZATION_FAILURE),
            );
            return;
        };
        if stop_requested(&control_state) {
            report_stopped(&control_state, ServiceExitCode::Win32(0));
            return;
        }
        if !report_running(&control_state) {
            return;
        }

        let result = runtime.block_on(run_relay(relay, shutdown_receiver));
        let exit_code = if result.is_ok() {
            ServiceExitCode::Win32(0)
        } else {
            ServiceExitCode::ServiceSpecific(RUNTIME_FAILURE)
        };
        report_stopped(&control_state, exit_code);
    }

    fn relay_runtime_config() -> Result<RelayRuntimeConfig, ()> {
        RelayRuntimeConfig::new(
            PathBuf::from(CONFIG_PATH),
            PathBuf::from(IDENTITY_PATH),
            HEALTH_ADDRESS.parse().map_err(|_| ())?,
            METRICS_ADDRESS.parse().map_err(|_| ())?,
            GRACEFUL_SHUTDOWN_TIMEOUT,
        )
        .map_err(|_| ())
    }

    async fn run_relay(
        relay: RelayServer,
        shutdown_receiver: mpsc::Receiver<()>,
    ) -> Result<(), ()> {
        relay
            .serve_until_with_drain_timeout(
                async move {
                    let _ = tokio::task::spawn_blocking(move || shutdown_receiver.recv()).await;
                },
                GRACEFUL_SHUTDOWN_TIMEOUT,
            )
            .await
            .map_err(|_| ())
    }

    fn status(
        current_state: ServiceState,
        controls_accepted: ServiceControlAccept,
        exit_code: ServiceExitCode,
        checkpoint: u32,
        wait_hint: Duration,
    ) -> ServiceStatus {
        ServiceStatus {
            service_type: SERVICE_TYPE,
            current_state,
            controls_accepted,
            exit_code,
            checkpoint,
            wait_hint,
            process_id: None,
        }
    }

    fn stop_requested(control_state: &Arc<Mutex<ControlState>>) -> bool {
        control_state
            .lock()
            .map(|state| state.stop_requested)
            .unwrap_or(true)
    }

    fn report_running(control_state: &Arc<Mutex<ControlState>>) -> bool {
        let Ok(state) = control_state.lock() else {
            return false;
        };
        if state.stop_requested {
            return false;
        }
        let Some(status_handle) = &state.status_handle else {
            return false;
        };
        status_handle
            .set_service_status(status(
                ServiceState::Running,
                ServiceControlAccept::STOP | ServiceControlAccept::SHUTDOWN,
                ServiceExitCode::Win32(0),
                0,
                Duration::ZERO,
            ))
            .is_ok()
    }

    fn report_stopped(control_state: &Arc<Mutex<ControlState>>, exit_code: ServiceExitCode) {
        let Ok(state) = control_state.lock() else {
            return;
        };
        let Some(status_handle) = &state.status_handle else {
            return;
        };
        let _ = status_handle.set_service_status(status(
            ServiceState::Stopped,
            ServiceControlAccept::empty(),
            exit_code,
            0,
            Duration::ZERO,
        ));
    }
}
