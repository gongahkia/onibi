use std::time::Duration;

#[cfg(windows)]
use std::{future::Future, net::SocketAddr};

use arachne_protocol::{LocalMeshTransportKind, WifiGroupBootstrap, WifiGroupCredential};

#[cfg(windows)]
use crate::WifiGroupLifecycle;
#[cfg(windows)]
use arachne_protocol::WifiGroupHandoff;

pub const WINDOWS_WIFI_GROUP_OPERATION_TIMEOUT: Duration = Duration::from_secs(30);
const MAX_WINDOWS_WIFI_ADAPTERS: u32 = 8;
const MAX_WINDOWS_WIFI_NETWORKS: u32 = 128;
const MAX_WINDOWS_WIFI_SSID_BYTES: usize = 32;

#[cfg(windows)]
use tokio::time::timeout;
#[cfg(windows)]
use windows::{
    Devices::WiFi::{WiFiAdapter, WiFiReconnectionKind},
    Networking::{
        Connectivity::NetworkInformation,
        NetworkOperators::{
            NetworkOperatorTetheringManager,
            NetworkOperatorTetheringSessionAccessPointConfiguration,
            TetheringWiFiAuthenticationKind,
        },
    },
    Security::Credentials::PasswordCredential,
    core::HSTRING,
};
#[cfg(windows)]
use windows_future::{IAsyncAction, IAsyncOperation};

#[cfg(windows)]
pub struct WindowsWifiHotspotGroup {
    state: WindowsWifiHotspotGroupState,
}

#[cfg(windows)]
enum WindowsWifiHotspotGroupState {
    Inactive,
    Owner,
    Client(WiFiAdapter),
}

#[cfg(windows)]
impl WindowsWifiHotspotGroup {
    pub fn new() -> Result<Self, WindowsWifiHotspotGroupError> {
        with_windows_runtime(|| {
            let _ = tethering_manager()?;
            Ok(())
        })?;
        Ok(Self {
            state: WindowsWifiHotspotGroupState::Inactive,
        })
    }

    #[must_use]
    pub const fn is_active(&self) -> bool {
        !matches!(self.state, WindowsWifiHotspotGroupState::Inactive)
    }

    async fn activate_owner(
        &mut self,
        bootstrap: &WifiGroupBootstrap,
    ) -> Result<(), WindowsWifiHotspotGroupError> {
        if self.is_active() {
            return Err(WindowsWifiHotspotGroupError::AlreadyActive);
        }
        let operation = with_windows_runtime(|| {
            let manager = tethering_manager()?;
            let configuration = session_configuration(bootstrap)?;
            manager
                .StartTetheringAsync2(&configuration)
                .map_err(WindowsWifiHotspotGroupError::Winrt)
        })?;
        let result = wait_for_operation(operation).await?;
        let status = with_windows_runtime(|| {
            result
                .Status()
                .map(|status| status.0)
                .map_err(WindowsWifiHotspotGroupError::Winrt)
        })?;
        require_tethering_success(status)?;
        self.state = WindowsWifiHotspotGroupState::Owner;
        Ok(())
    }

    async fn join_client(
        &mut self,
        handoff: &WifiGroupHandoff,
    ) -> Result<(), WindowsWifiHotspotGroupError> {
        if self.is_active() {
            return Err(WindowsWifiHotspotGroupError::AlreadyActive);
        }
        if handoff.configuration().transport() != LocalMeshTransportKind::WifiHotspot {
            return Err(WindowsWifiHotspotGroupError::UnsupportedTransport(
                handoff.configuration().transport(),
            ));
        }
        let identifier = handoff.configuration().identifier();
        let credential = hotspot_credential(handoff.credential())?;
        let access = wait_for_operation(with_windows_runtime(|| {
            WiFiAdapter::RequestAccessAsync().map_err(WindowsWifiHotspotGroupError::Winrt)
        })?)
        .await?;
        require_wifi_access(access.0)?;
        let wifi_adapters = {
            let adapters = wait_for_operation(with_windows_runtime(|| {
                WiFiAdapter::FindAllAdaptersAsync().map_err(WindowsWifiHotspotGroupError::Winrt)
            })?)
            .await?;
            let adapter_count = with_windows_runtime(|| {
                adapters.Size().map_err(WindowsWifiHotspotGroupError::Winrt)
            })?;
            if adapter_count == 0 {
                return Err(WindowsWifiHotspotGroupError::NoWifiAdapter);
            }
            if adapter_count > MAX_WINDOWS_WIFI_ADAPTERS {
                return Err(WindowsWifiHotspotGroupError::TooManyWifiAdapters);
            }
            let mut wifi_adapters = Vec::with_capacity(adapter_count as usize);
            for index in 0..adapter_count {
                wifi_adapters.push(with_windows_runtime(|| {
                    adapters
                        .GetAt(index)
                        .map_err(WindowsWifiHotspotGroupError::Winrt)
                })?);
            }
            wifi_adapters
        };

        let mut selected = None;
        for adapter in wifi_adapters {
            wait_for_action(with_windows_runtime(|| {
                adapter
                    .ScanAsync()
                    .map_err(WindowsWifiHotspotGroupError::Winrt)
            })?)
            .await?;
            let wifi_networks = {
                let networks = with_windows_runtime(|| {
                    adapter
                        .NetworkReport()
                        .and_then(|report| report.AvailableNetworks())
                        .map_err(WindowsWifiHotspotGroupError::Winrt)
                })?;
                let network_count = with_windows_runtime(|| {
                    networks.Size().map_err(WindowsWifiHotspotGroupError::Winrt)
                })?;
                if network_count > MAX_WINDOWS_WIFI_NETWORKS {
                    return Err(WindowsWifiHotspotGroupError::TooManyWifiNetworks);
                }
                let mut wifi_networks = Vec::with_capacity(network_count as usize);
                for network_index in 0..network_count {
                    wifi_networks.push(with_windows_runtime(|| {
                        networks
                            .GetAt(network_index)
                            .map_err(WindowsWifiHotspotGroupError::Winrt)
                    })?);
                }
                wifi_networks
            };
            for network in wifi_networks {
                let ssid = with_windows_runtime(|| {
                    network
                        .Ssid()
                        .map(|ssid| ssid.to_string())
                        .map_err(WindowsWifiHotspotGroupError::Winrt)
                })?;
                if ssid == identifier {
                    if selected.is_some() {
                        return Err(WindowsWifiHotspotGroupError::AmbiguousWifiNetwork);
                    }
                    selected = Some((adapter.clone(), network));
                }
            }
        }
        let (adapter, network) =
            selected.ok_or(WindowsWifiHotspotGroupError::WifiNetworkNotFound)?;
        let password = with_windows_runtime(|| {
            let password =
                PasswordCredential::new().map_err(WindowsWifiHotspotGroupError::Winrt)?;
            password
                .SetPassword(&HSTRING::from(credential))
                .map_err(WindowsWifiHotspotGroupError::Winrt)?;
            Ok(password)
        })?;
        let result = wait_for_operation(with_windows_runtime(|| {
            adapter
                .ConnectWithPasswordCredentialAsync(
                    &network,
                    WiFiReconnectionKind::Manual,
                    &password,
                )
                .map_err(WindowsWifiHotspotGroupError::Winrt)
        })?)
        .await?;
        let status = with_windows_runtime(|| {
            result
                .ConnectionStatus()
                .map(|status| status.0)
                .map_err(WindowsWifiHotspotGroupError::Winrt)
        })?;
        require_wifi_connection_success(status)?;
        self.state = WindowsWifiHotspotGroupState::Client(adapter);
        Ok(())
    }

    async fn teardown(&mut self) -> Result<(), WindowsWifiHotspotGroupError> {
        match &self.state {
            WindowsWifiHotspotGroupState::Inactive => Ok(()),
            WindowsWifiHotspotGroupState::Owner => {
                let operation = with_windows_runtime(|| {
                    tethering_manager()?
                        .StopTetheringAsync()
                        .map_err(WindowsWifiHotspotGroupError::Winrt)
                })?;
                let result = wait_for_operation(operation).await?;
                let status = with_windows_runtime(|| {
                    result
                        .Status()
                        .map(|status| status.0)
                        .map_err(WindowsWifiHotspotGroupError::Winrt)
                })?;
                require_tethering_success(status)?;
                self.state = WindowsWifiHotspotGroupState::Inactive;
                Ok(())
            }
            WindowsWifiHotspotGroupState::Client(adapter) => {
                with_windows_runtime(|| {
                    adapter
                        .Disconnect()
                        .map_err(WindowsWifiHotspotGroupError::Winrt)
                })?;
                self.state = WindowsWifiHotspotGroupState::Inactive;
                Ok(())
            }
        }
    }
}

#[cfg(windows)]
impl std::fmt::Debug for WindowsWifiHotspotGroup {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        formatter
            .debug_struct("WindowsWifiHotspotGroup")
            .field("active", &self.is_active())
            .finish_non_exhaustive()
    }
}

#[cfg(windows)]
impl WifiGroupLifecycle for WindowsWifiHotspotGroup {
    type Error = WindowsWifiHotspotGroupError;

    fn activate_owner(
        &mut self,
        bootstrap: &WifiGroupBootstrap,
    ) -> impl Future<Output = Result<(), Self::Error>> + Send {
        self.activate_owner(bootstrap)
    }

    fn validate_owner_endpoint(&self, endpoint: SocketAddr) -> Result<(), Self::Error> {
        if !matches!(self.state, WindowsWifiHotspotGroupState::Owner) {
            return Err(WindowsWifiHotspotGroupError::OwnerNotActive);
        }
        if endpoint.ip().is_unspecified() || endpoint.ip().is_loopback() {
            return Err(WindowsWifiHotspotGroupError::InvalidDirectEndpoint);
        }
        Ok(())
    }

    fn join_client(
        &mut self,
        handoff: &WifiGroupHandoff,
    ) -> impl Future<Output = Result<(), Self::Error>> + Send {
        self.join_client(handoff)
    }

    fn teardown(&mut self) -> impl Future<Output = Result<(), Self::Error>> + Send {
        self.teardown()
    }
}

#[derive(Debug, thiserror::Error)]
pub enum WindowsWifiHotspotGroupError {
    #[error("Windows Runtime initialization failed")]
    WindowsRuntime,
    #[error("Windows Wi-Fi hotspot capability is unavailable: {0}")]
    TetheringCapability(i32),
    #[error("Windows Wi-Fi hotspot requires WPA2 support")]
    Wpa2Unsupported,
    #[error("Windows Wi-Fi hotspot SSID must be 1 to 32 printable bytes")]
    InvalidSsid,
    #[error("Windows Wi-Fi hotspot credentials must be 8 to 63 printable ASCII bytes")]
    InvalidCredential,
    #[error("unsupported Windows Wi-Fi group transport: {0:?}")]
    UnsupportedTransport(LocalMeshTransportKind),
    #[error("Windows Wi-Fi group is already active")]
    AlreadyActive,
    #[error("Windows Wi-Fi hotspot owner is not active")]
    OwnerNotActive,
    #[error("Windows Wi-Fi hotspot direct endpoint is invalid")]
    InvalidDirectEndpoint,
    #[error("Windows Wi-Fi access was denied: {0}")]
    WifiAccess(i32),
    #[error("Windows did not expose a Wi-Fi adapter")]
    NoWifiAdapter,
    #[error("Windows exposed too many Wi-Fi adapters")]
    TooManyWifiAdapters,
    #[error("Windows exposed too many Wi-Fi networks")]
    TooManyWifiNetworks,
    #[error("Windows hotspot SSID was not found")]
    WifiNetworkNotFound,
    #[error("Windows hotspot SSID is ambiguous")]
    AmbiguousWifiNetwork,
    #[error("Windows Wi-Fi connection failed: {0}")]
    WifiConnection(i32),
    #[error("Windows tethering operation failed: {0}")]
    TetheringOperation(i32),
    #[error("Windows Wi-Fi operation timed out")]
    TimedOut,
    #[cfg(windows)]
    #[error("Windows Wi-Fi API failed: {0}")]
    Winrt(#[source] windows::core::Error),
}

fn validate_hotspot_bootstrap(
    bootstrap: &WifiGroupBootstrap,
) -> Result<String, WindowsWifiHotspotGroupError> {
    if bootstrap.transport() != LocalMeshTransportKind::WifiHotspot {
        return Err(WindowsWifiHotspotGroupError::UnsupportedTransport(
            bootstrap.transport(),
        ));
    }
    let identifier = bootstrap.identifier();
    if identifier.is_empty()
        || identifier.len() > MAX_WINDOWS_WIFI_SSID_BYTES
        || identifier.bytes().any(|byte| !byte.is_ascii_graphic())
    {
        return Err(WindowsWifiHotspotGroupError::InvalidSsid);
    }
    hotspot_credential(bootstrap.credential())
}

fn hotspot_credential(
    credential: &WifiGroupCredential,
) -> Result<String, WindowsWifiHotspotGroupError> {
    let credential = std::str::from_utf8(credential.as_bytes())
        .map_err(|_| WindowsWifiHotspotGroupError::InvalidCredential)?;
    if !(8..=63).contains(&credential.len())
        || credential.bytes().any(|byte| !byte.is_ascii_graphic())
    {
        return Err(WindowsWifiHotspotGroupError::InvalidCredential);
    }
    Ok(credential.to_owned())
}

fn require_tethering_success(status: i32) -> Result<(), WindowsWifiHotspotGroupError> {
    if status == 0 {
        Ok(())
    } else {
        Err(WindowsWifiHotspotGroupError::TetheringOperation(status))
    }
}

fn require_wifi_access(status: i32) -> Result<(), WindowsWifiHotspotGroupError> {
    if status == 1 {
        Ok(())
    } else {
        Err(WindowsWifiHotspotGroupError::WifiAccess(status))
    }
}

fn require_wifi_connection_success(status: i32) -> Result<(), WindowsWifiHotspotGroupError> {
    if status == 1 {
        Ok(())
    } else {
        Err(WindowsWifiHotspotGroupError::WifiConnection(status))
    }
}

#[cfg(windows)]
fn with_windows_runtime<T>(
    operation: impl FnOnce() -> Result<T, WindowsWifiHotspotGroupError>,
) -> Result<T, WindowsWifiHotspotGroupError> {
    let _runtime = crate::windows_runtime::WindowsRuntime::initialize()
        .ok_or(WindowsWifiHotspotGroupError::WindowsRuntime)?;
    operation()
}

#[cfg(windows)]
fn tethering_manager() -> Result<NetworkOperatorTetheringManager, WindowsWifiHotspotGroupError> {
    let profile = NetworkInformation::GetInternetConnectionProfile()
        .map_err(WindowsWifiHotspotGroupError::Winrt)?;
    let capability =
        NetworkOperatorTetheringManager::GetTetheringCapabilityFromConnectionProfile(&profile)
            .map_err(WindowsWifiHotspotGroupError::Winrt)?;
    if capability.0 != 0 {
        return Err(WindowsWifiHotspotGroupError::TetheringCapability(
            capability.0,
        ));
    }
    NetworkOperatorTetheringManager::CreateFromConnectionProfile(&profile)
        .map_err(WindowsWifiHotspotGroupError::Winrt)
}

#[cfg(windows)]
fn session_configuration(
    bootstrap: &WifiGroupBootstrap,
) -> Result<NetworkOperatorTetheringSessionAccessPointConfiguration, WindowsWifiHotspotGroupError> {
    let credential = validate_hotspot_bootstrap(bootstrap)?;
    let configuration = NetworkOperatorTetheringSessionAccessPointConfiguration::new()
        .map_err(WindowsWifiHotspotGroupError::Winrt)?;
    configuration
        .SetSsid(&HSTRING::from(bootstrap.identifier()))
        .map_err(WindowsWifiHotspotGroupError::Winrt)?;
    configuration
        .SetPassphrase(&HSTRING::from(credential))
        .map_err(WindowsWifiHotspotGroupError::Winrt)?;
    if !configuration
        .IsAuthenticationKindSupported(TetheringWiFiAuthenticationKind::Wpa2)
        .map_err(WindowsWifiHotspotGroupError::Winrt)?
    {
        return Err(WindowsWifiHotspotGroupError::Wpa2Unsupported);
    }
    configuration
        .SetAuthenticationKind(TetheringWiFiAuthenticationKind::Wpa2)
        .map_err(WindowsWifiHotspotGroupError::Winrt)?;
    Ok(configuration)
}

#[cfg(windows)]
async fn wait_for_operation<T: windows::core::RuntimeType + 'static>(
    operation: IAsyncOperation<T>,
) -> Result<T, WindowsWifiHotspotGroupError> {
    let cancellation = operation.clone();
    timeout(WINDOWS_WIFI_GROUP_OPERATION_TIMEOUT, operation)
        .await
        .map_err(|_| {
            let _ = cancellation.Cancel();
            WindowsWifiHotspotGroupError::TimedOut
        })?
        .map_err(WindowsWifiHotspotGroupError::Winrt)
}

#[cfg(windows)]
async fn wait_for_action(operation: IAsyncAction) -> Result<(), WindowsWifiHotspotGroupError> {
    let cancellation = operation.clone();
    timeout(WINDOWS_WIFI_GROUP_OPERATION_TIMEOUT, operation)
        .await
        .map_err(|_| {
            let _ = cancellation.Cancel();
            WindowsWifiHotspotGroupError::TimedOut
        })?
        .map_err(WindowsWifiHotspotGroupError::Winrt)
}

#[cfg(test)]
mod tests {
    use super::{
        WindowsWifiHotspotGroupError, hotspot_credential, require_tethering_success,
        require_wifi_access, require_wifi_connection_success, validate_hotspot_bootstrap,
    };
    use arachne_protocol::{LocalMeshTransportKind, WifiGroupBootstrap, WifiGroupCredential};

    fn bootstrap(identifier: &str, credential: &[u8]) -> WifiGroupBootstrap {
        WifiGroupBootstrap::new(
            LocalMeshTransportKind::WifiHotspot,
            identifier.to_owned(),
            WifiGroupCredential::new(credential.to_vec()).unwrap(),
        )
        .unwrap()
    }

    #[test]
    fn validates_session_only_wpa2_inputs() {
        assert_eq!(
            validate_hotspot_bootstrap(&bootstrap("arachne", b"password1")).unwrap(),
            "password1"
        );
        assert!(matches!(
            hotspot_credential(&WifiGroupCredential::new(b"short".to_vec()).unwrap()),
            Err(WindowsWifiHotspotGroupError::InvalidCredential)
        ));
    }

    #[test]
    fn maps_tethering_access_and_connection_statuses_conservatively() {
        assert!(require_tethering_success(0).is_ok());
        assert!(require_wifi_access(1).is_ok());
        assert!(require_wifi_connection_success(1).is_ok());
        assert!(matches!(
            require_tethering_success(9),
            Err(WindowsWifiHotspotGroupError::TetheringOperation(9))
        ));
        assert!(matches!(
            require_wifi_access(2),
            Err(WindowsWifiHotspotGroupError::WifiAccess(2))
        ));
        assert!(matches!(
            require_wifi_connection_success(3),
            Err(WindowsWifiHotspotGroupError::WifiConnection(3))
        ));
    }
}
