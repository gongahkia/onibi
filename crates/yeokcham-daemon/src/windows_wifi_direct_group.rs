use std::{
    fmt,
    net::{IpAddr, SocketAddr},
    time::Duration,
};

use yeokcham_protocol::{
    DirectProfileConfig, LocalMeshTransportKind, WifiGroupBootstrap, WifiGroupHandoff,
    WifiGroupRole,
};

pub const WINDOWS_WIFI_DIRECT_OPERATION_TIMEOUT: Duration = Duration::from_secs(30);
const MAX_WINDOWS_WIFI_DIRECT_DEVICE_ID_BYTES: usize = 1024;
const MAX_WINDOWS_WIFI_DIRECT_DISCOVERED_PEERS: u32 = 32;
const MAX_WINDOWS_WIFI_DIRECT_ENDPOINTS: u32 = 8;

#[derive(Clone, Eq, PartialEq)]
pub struct WindowsWifiDirectPeer(String);

impl WindowsWifiDirectPeer {
    pub fn new(device_id: String) -> Result<Self, WindowsWifiDirectGroupError> {
        validate_device_id(&device_id)?;
        Ok(Self(device_id))
    }

    #[must_use]
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

impl fmt::Debug for WindowsWifiDirectPeer {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("WindowsWifiDirectPeer(REDACTED)")
    }
}

#[derive(Clone, Eq, PartialEq)]
pub struct WindowsWifiDirectEndpoint {
    local_address: IpAddr,
    remote_address: IpAddr,
}

impl WindowsWifiDirectEndpoint {
    #[must_use]
    pub const fn local_address(&self) -> IpAddr {
        self.local_address
    }

    #[must_use]
    pub const fn remote_address(&self) -> IpAddr {
        self.remote_address
    }
}

impl fmt::Debug for WindowsWifiDirectEndpoint {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("WindowsWifiDirectEndpoint(REDACTED)")
    }
}

#[cfg(windows)]
use std::future::Future;
#[cfg(windows)]
use tokio::{
    sync::mpsc::{self, Receiver},
    time::timeout,
};
#[cfg(windows)]
use windows::{
    Devices::{
        Enumeration::DeviceInformation,
        WiFiDirect::{
            WiFiDirectAdvertisementPublisher, WiFiDirectAdvertisementPublisherStatus,
            WiFiDirectConnectionListener, WiFiDirectConnectionRequest,
            WiFiDirectConnectionRequestedEventArgs, WiFiDirectDevice,
        },
    },
    Foundation::TypedEventHandler,
    core::HSTRING,
};
#[cfg(windows)]
use windows_future::IAsyncOperation;

#[cfg(windows)]
use crate::{LocalTransportAvailability, WifiGroupLifecycle, WindowsWifiDirectCapabilityProbe};

#[cfg(windows)]
pub struct WindowsWifiDirectGroup {
    expected_peer: WindowsWifiDirectPeer,
    state: WindowsWifiDirectGroupState,
}

#[cfg(windows)]
enum WindowsWifiDirectGroupState {
    Inactive,
    Owner(WindowsWifiDirectOwner),
    Client(WindowsWifiDirectClient),
}

#[cfg(windows)]
struct WindowsWifiDirectOwner {
    publisher: WiFiDirectAdvertisementPublisher,
    listener: WiFiDirectConnectionListener,
    request_token: i64,
    requests: Receiver<WindowsWifiDirectPendingRequest>,
    device: Option<WiFiDirectDevice>,
    endpoints: Vec<WindowsWifiDirectEndpoint>,
}

#[cfg(windows)]
struct WindowsWifiDirectClient {
    device: WiFiDirectDevice,
    endpoints: Vec<WindowsWifiDirectEndpoint>,
}

#[cfg(windows)]
struct WindowsWifiDirectPendingRequest {
    peer: WindowsWifiDirectPeer,
    request: WiFiDirectConnectionRequest,
}

#[cfg(windows)]
impl WindowsWifiDirectGroup {
    pub fn new(expected_peer: WindowsWifiDirectPeer) -> Result<Self, WindowsWifiDirectGroupError> {
        ensure_windows_wifi_direct_available()?;
        Ok(Self {
            expected_peer,
            state: WindowsWifiDirectGroupState::Inactive,
        })
    }

    #[must_use]
    pub const fn is_active(&self) -> bool {
        !matches!(self.state, WindowsWifiDirectGroupState::Inactive)
    }

    pub async fn discover_peers() -> Result<Vec<WindowsWifiDirectPeer>, WindowsWifiDirectGroupError>
    {
        ensure_windows_wifi_direct_available()?;
        let selector = with_windows_runtime(|| {
            WiFiDirectDevice::GetDeviceSelector().map_err(WindowsWifiDirectGroupError::Winrt)
        })?;
        let devices = wait_for_operation(with_windows_runtime(|| {
            DeviceInformation::FindAllAsyncAqsFilter(&selector)
                .map_err(WindowsWifiDirectGroupError::Winrt)
        })?)
        .await?;
        let count =
            with_windows_runtime(|| devices.Size().map_err(WindowsWifiDirectGroupError::Winrt))?;
        if count > MAX_WINDOWS_WIFI_DIRECT_DISCOVERED_PEERS {
            return Err(WindowsWifiDirectGroupError::TooManyDiscoveredPeers);
        }
        let mut peers = Vec::with_capacity(count as usize);
        for index in 0..count {
            let id = with_windows_runtime(|| {
                devices
                    .GetAt(index)
                    .and_then(|device| device.Id())
                    .map(|id| id.to_string())
                    .map_err(WindowsWifiDirectGroupError::Winrt)
            })?;
            peers.push(WindowsWifiDirectPeer::new(id)?);
        }
        Ok(peers)
    }

    pub async fn accept_expected_peer(
        &mut self,
    ) -> Result<&[WindowsWifiDirectEndpoint], WindowsWifiDirectGroupError> {
        let expected_peer = self.expected_peer.clone();
        let pending = match &mut self.state {
            WindowsWifiDirectGroupState::Owner(owner) => {
                timeout(WINDOWS_WIFI_DIRECT_OPERATION_TIMEOUT, owner.requests.recv())
                    .await
                    .map_err(|_| WindowsWifiDirectGroupError::ConnectionRequestTimedOut)?
                    .ok_or(WindowsWifiDirectGroupError::ConnectionRequestChannelClosed)?
            }
            _ => return Err(WindowsWifiDirectGroupError::OwnerNotActive),
        };
        if pending.peer != expected_peer {
            close_connection_request(pending.request)?;
            return Err(WindowsWifiDirectGroupError::UnexpectedPeer);
        }
        let device = connect_to_peer(&pending.peer).await;
        let close_result = close_connection_request(pending.request);
        let device = device?;
        if let Err(error) = close_result {
            let _ = close_device(&device);
            return Err(error);
        }
        let endpoints = match connection_endpoints(&device) {
            Ok(endpoints) => endpoints,
            Err(error) => {
                let _ = close_device(&device);
                return Err(error);
            }
        };
        match &mut self.state {
            WindowsWifiDirectGroupState::Owner(owner) => {
                owner.device = Some(device);
                owner.endpoints = endpoints;
                Ok(&owner.endpoints)
            }
            _ => unreachable!("Wi-Fi Direct owner state cannot change while accepting a request"),
        }
    }

    pub fn connection_endpoints(
        &self,
    ) -> Result<&[WindowsWifiDirectEndpoint], WindowsWifiDirectGroupError> {
        match &self.state {
            WindowsWifiDirectGroupState::Owner(owner) if !owner.endpoints.is_empty() => {
                Ok(&owner.endpoints)
            }
            WindowsWifiDirectGroupState::Owner(_) => {
                Err(WindowsWifiDirectGroupError::OwnerConnectionRequired)
            }
            WindowsWifiDirectGroupState::Client(client) => Ok(&client.endpoints),
            WindowsWifiDirectGroupState::Inactive => Err(WindowsWifiDirectGroupError::Inactive),
        }
    }

    async fn activate_owner(
        &mut self,
        bootstrap: &WifiGroupBootstrap,
    ) -> Result<(), WindowsWifiDirectGroupError> {
        if self.is_active() {
            return Err(WindowsWifiDirectGroupError::AlreadyActive);
        }
        validate_wifi_direct_bootstrap(bootstrap)?;
        self.state = WindowsWifiDirectGroupState::Owner(create_owner()?);
        Ok(())
    }

    async fn join_client(
        &mut self,
        handoff: &WifiGroupHandoff,
    ) -> Result<(), WindowsWifiDirectGroupError> {
        if self.is_active() {
            return Err(WindowsWifiDirectGroupError::AlreadyActive);
        }
        validate_wifi_direct_handoff(handoff)?;
        let device = connect_to_peer(&self.expected_peer).await?;
        let endpoints = match connection_endpoints(&device) {
            Ok(endpoints) => endpoints,
            Err(error) => {
                let _ = close_device(&device);
                return Err(error);
            }
        };
        if !endpoint_matches_remote_profile(&endpoints, handoff.configuration().direct_profile()) {
            let _ = close_device(&device);
            return Err(WindowsWifiDirectGroupError::HandoffEndpointMismatch);
        }
        self.state =
            WindowsWifiDirectGroupState::Client(WindowsWifiDirectClient { device, endpoints });
        Ok(())
    }

    fn validate_owner_endpoint(
        &self,
        endpoint: SocketAddr,
    ) -> Result<(), WindowsWifiDirectGroupError> {
        match &self.state {
            WindowsWifiDirectGroupState::Owner(owner) if !owner.endpoints.is_empty() => {
                if endpoint_matches_owner_endpoint(&owner.endpoints, endpoint) {
                    Ok(())
                } else {
                    Err(WindowsWifiDirectGroupError::HandoffEndpointMismatch)
                }
            }
            WindowsWifiDirectGroupState::Owner(_) => {
                Err(WindowsWifiDirectGroupError::OwnerConnectionRequired)
            }
            _ => Err(WindowsWifiDirectGroupError::OwnerNotActive),
        }
    }

    fn teardown(&mut self) -> Result<(), WindowsWifiDirectGroupError> {
        let state = std::mem::replace(&mut self.state, WindowsWifiDirectGroupState::Inactive);
        match state {
            WindowsWifiDirectGroupState::Inactive => Ok(()),
            WindowsWifiDirectGroupState::Client(client) => close_device(&client.device),
            WindowsWifiDirectGroupState::Owner(mut owner) => {
                let mut failure = None;
                while let Ok(pending) = owner.requests.try_recv() {
                    record_failure(&mut failure, close_connection_request(pending.request));
                }
                record_failure(
                    &mut failure,
                    with_windows_runtime(|| {
                        owner
                            .listener
                            .RemoveConnectionRequested(owner.request_token)
                            .map_err(WindowsWifiDirectGroupError::Winrt)
                    }),
                );
                record_failure(
                    &mut failure,
                    with_windows_runtime(|| {
                        owner
                            .publisher
                            .Stop()
                            .map_err(WindowsWifiDirectGroupError::Winrt)
                    }),
                );
                if let Some(device) = owner.device {
                    record_failure(&mut failure, close_device(&device));
                }
                failure.map_or(Ok(()), Err)
            }
        }
    }
}

#[cfg(windows)]
impl fmt::Debug for WindowsWifiDirectGroup {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("WindowsWifiDirectGroup")
            .field("active", &self.is_active())
            .finish_non_exhaustive()
    }
}

#[cfg(windows)]
impl WifiGroupLifecycle for WindowsWifiDirectGroup {
    type Error = WindowsWifiDirectGroupError;

    fn activate_owner(
        &mut self,
        bootstrap: &WifiGroupBootstrap,
    ) -> impl Future<Output = Result<(), Self::Error>> + Send {
        self.activate_owner(bootstrap)
    }

    fn validate_owner_endpoint(&self, endpoint: SocketAddr) -> Result<(), Self::Error> {
        self.validate_owner_endpoint(endpoint)
    }

    fn join_client(
        &mut self,
        handoff: &WifiGroupHandoff,
    ) -> impl Future<Output = Result<(), Self::Error>> + Send {
        self.join_client(handoff)
    }

    fn teardown(&mut self) -> impl Future<Output = Result<(), Self::Error>> + Send {
        std::future::ready(self.teardown())
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum WindowsWifiDirectGroupError {
    #[error("Windows Runtime initialization failed")]
    WindowsRuntime,
    #[error("Windows Wi-Fi Direct is unavailable")]
    Unavailable,
    #[error("Windows Wi-Fi Direct permission was denied")]
    PermissionDenied,
    #[error("Windows Wi-Fi Direct device ID is invalid")]
    InvalidDeviceId,
    #[error(
        "Windows Wi-Fi Direct supports at most {MAX_WINDOWS_WIFI_DIRECT_DISCOVERED_PEERS} discovered peers"
    )]
    TooManyDiscoveredPeers,
    #[error("Windows Wi-Fi Direct exposed too many endpoint pairs")]
    TooManyEndpoints,
    #[error("Windows Wi-Fi Direct did not expose an endpoint pair")]
    MissingEndpoint,
    #[error("Windows Wi-Fi Direct exposed an invalid endpoint address")]
    InvalidEndpointAddress,
    #[error("Windows Wi-Fi Direct handoff has an unsupported transport: {0:?}")]
    UnsupportedTransport(LocalMeshTransportKind),
    #[error("Windows Wi-Fi Direct handoff requires an owner profile")]
    InvalidHandoffRole,
    #[error("Windows Wi-Fi Direct group is already active")]
    AlreadyActive,
    #[error("Windows Wi-Fi Direct owner is not active")]
    OwnerNotActive,
    #[error("Windows Wi-Fi Direct owner connection has not completed")]
    OwnerConnectionRequired,
    #[error("Windows Wi-Fi Direct group is inactive")]
    Inactive,
    #[error("Windows Wi-Fi Direct connection request timed out")]
    ConnectionRequestTimedOut,
    #[error("Windows Wi-Fi Direct request listener stopped unexpectedly")]
    ConnectionRequestChannelClosed,
    #[error("Windows Wi-Fi Direct request was from an unexpected peer")]
    UnexpectedPeer,
    #[error("Windows Wi-Fi Direct did not establish a connection")]
    Disconnected,
    #[error("Windows Wi-Fi Direct advertisement did not start: {0}")]
    PublisherStatus(i32),
    #[error("Windows Wi-Fi Direct endpoint does not match the authenticated handoff")]
    HandoffEndpointMismatch,
    #[error("Windows Wi-Fi Direct operation timed out")]
    TimedOut,
    #[cfg(windows)]
    #[error("Windows Wi-Fi Direct API failed: {0}")]
    Winrt(#[source] windows::core::Error),
}

fn validate_device_id(device_id: &str) -> Result<(), WindowsWifiDirectGroupError> {
    if device_id.is_empty()
        || device_id.len() > MAX_WINDOWS_WIFI_DIRECT_DEVICE_ID_BYTES
        || device_id.bytes().any(|byte| byte.is_ascii_control())
    {
        return Err(WindowsWifiDirectGroupError::InvalidDeviceId);
    }
    Ok(())
}

fn validate_wifi_direct_bootstrap(
    bootstrap: &WifiGroupBootstrap,
) -> Result<(), WindowsWifiDirectGroupError> {
    if bootstrap.transport() != LocalMeshTransportKind::WifiDirect {
        return Err(WindowsWifiDirectGroupError::UnsupportedTransport(
            bootstrap.transport(),
        ));
    }
    Ok(())
}

fn validate_wifi_direct_handoff(
    handoff: &WifiGroupHandoff,
) -> Result<(), WindowsWifiDirectGroupError> {
    if handoff.configuration().transport() != LocalMeshTransportKind::WifiDirect {
        return Err(WindowsWifiDirectGroupError::UnsupportedTransport(
            handoff.configuration().transport(),
        ));
    }
    if handoff.configuration().role() != WifiGroupRole::Owner {
        return Err(WindowsWifiDirectGroupError::InvalidHandoffRole);
    }
    Ok(())
}

fn endpoint_matches_owner_endpoint(
    endpoints: &[WindowsWifiDirectEndpoint],
    endpoint: SocketAddr,
) -> bool {
    endpoints
        .iter()
        .any(|candidate| candidate.local_address == endpoint.ip())
}

fn endpoint_matches_remote_profile(
    endpoints: &[WindowsWifiDirectEndpoint],
    profile: DirectProfileConfig,
) -> bool {
    endpoints
        .iter()
        .any(|candidate| candidate.remote_address == profile.endpoint().ip())
}

#[cfg(windows)]
fn ensure_windows_wifi_direct_available() -> Result<(), WindowsWifiDirectGroupError> {
    match WindowsWifiDirectCapabilityProbe::new().probe() {
        LocalTransportAvailability::Available => Ok(()),
        LocalTransportAvailability::PermissionDenied => {
            Err(WindowsWifiDirectGroupError::PermissionDenied)
        }
        LocalTransportAvailability::Unavailable => Err(WindowsWifiDirectGroupError::Unavailable),
    }
}

#[cfg(windows)]
fn create_owner() -> Result<WindowsWifiDirectOwner, WindowsWifiDirectGroupError> {
    with_windows_runtime(|| {
        let (sender, requests) = mpsc::channel(1);
        let listener =
            WiFiDirectConnectionListener::new().map_err(WindowsWifiDirectGroupError::Winrt)?;
        let handler = TypedEventHandler::<
            WiFiDirectConnectionListener,
            WiFiDirectConnectionRequestedEventArgs,
        >::new(move |_sender, arguments| {
            let request = arguments.ok()?.GetConnectionRequest()?;
            let device_id = request.DeviceInformation()?.Id()?.to_string();
            let Ok(peer) = WindowsWifiDirectPeer::new(device_id) else {
                request.Close()?;
                return Ok(());
            };
            if let Err(error) = sender.try_send(WindowsWifiDirectPendingRequest { peer, request }) {
                error.into_inner().request.Close()?;
            }
            Ok(())
        });
        let request_token = listener
            .ConnectionRequested(&handler)
            .map_err(WindowsWifiDirectGroupError::Winrt)?;
        let publisher = match WiFiDirectAdvertisementPublisher::new() {
            Ok(publisher) => publisher,
            Err(error) => {
                let _ = listener.RemoveConnectionRequested(request_token);
                return Err(WindowsWifiDirectGroupError::Winrt(error));
            }
        };
        let started = (|| {
            publisher
                .Advertisement()
                .and_then(|advertisement| advertisement.SetIsAutonomousGroupOwnerEnabled(true))?;
            publisher.Start()?;
            publisher.Status()
        })();
        let status = match started {
            Ok(status) => status,
            Err(error) => {
                let _ = listener.RemoveConnectionRequested(request_token);
                let _ = publisher.Stop();
                return Err(WindowsWifiDirectGroupError::Winrt(error));
            }
        };
        if status != WiFiDirectAdvertisementPublisherStatus::Started {
            let _ = listener.RemoveConnectionRequested(request_token);
            let _ = publisher.Stop();
            return Err(WindowsWifiDirectGroupError::PublisherStatus(status.0));
        }
        Ok(WindowsWifiDirectOwner {
            publisher,
            listener,
            request_token,
            requests,
            device: None,
            endpoints: Vec::new(),
        })
    })
}

#[cfg(windows)]
async fn connect_to_peer(
    peer: &WindowsWifiDirectPeer,
) -> Result<WiFiDirectDevice, WindowsWifiDirectGroupError> {
    let operation = with_windows_runtime(|| {
        WiFiDirectDevice::FromIdAsync(&HSTRING::from(peer.as_str()))
            .map_err(WindowsWifiDirectGroupError::Winrt)
    })?;
    let device = wait_for_operation(operation).await?;
    let status = with_windows_runtime(|| {
        device
            .ConnectionStatus()
            .map(|status| status.0)
            .map_err(WindowsWifiDirectGroupError::Winrt)
    })?;
    if status != 1 {
        let _ = close_device(&device);
        return Err(WindowsWifiDirectGroupError::Disconnected);
    }
    Ok(device)
}

#[cfg(windows)]
fn connection_endpoints(
    device: &WiFiDirectDevice,
) -> Result<Vec<WindowsWifiDirectEndpoint>, WindowsWifiDirectGroupError> {
    let pairs = with_windows_runtime(|| {
        device
            .GetConnectionEndpointPairs()
            .map_err(WindowsWifiDirectGroupError::Winrt)
    })?;
    let count = with_windows_runtime(|| pairs.Size().map_err(WindowsWifiDirectGroupError::Winrt))?;
    if count == 0 {
        return Err(WindowsWifiDirectGroupError::MissingEndpoint);
    }
    if count > MAX_WINDOWS_WIFI_DIRECT_ENDPOINTS {
        return Err(WindowsWifiDirectGroupError::TooManyEndpoints);
    }
    let mut endpoints = Vec::with_capacity(count as usize);
    for index in 0..count {
        let endpoint = with_windows_runtime(|| {
            let pair = pairs
                .GetAt(index)
                .map_err(WindowsWifiDirectGroupError::Winrt)?;
            let local_address = pair
                .LocalHostName()
                .and_then(|host| host.RawName())
                .map(|address| address.to_string())
                .map_err(WindowsWifiDirectGroupError::Winrt)?;
            let remote_address = pair
                .RemoteHostName()
                .and_then(|host| host.RawName())
                .map(|address| address.to_string())
                .map_err(WindowsWifiDirectGroupError::Winrt)?;
            Ok((local_address, remote_address))
        })?;
        endpoints.push(WindowsWifiDirectEndpoint {
            local_address: parse_endpoint_address(&endpoint.0)?,
            remote_address: parse_endpoint_address(&endpoint.1)?,
        });
    }
    Ok(endpoints)
}

#[cfg(windows)]
fn parse_endpoint_address(address: &str) -> Result<IpAddr, WindowsWifiDirectGroupError> {
    let address = address
        .parse::<IpAddr>()
        .map_err(|_| WindowsWifiDirectGroupError::InvalidEndpointAddress)?;
    if address.is_unspecified() || address.is_loopback() || address.is_multicast() {
        return Err(WindowsWifiDirectGroupError::InvalidEndpointAddress);
    }
    Ok(address)
}

#[cfg(windows)]
fn close_connection_request(
    request: WiFiDirectConnectionRequest,
) -> Result<(), WindowsWifiDirectGroupError> {
    with_windows_runtime(|| request.Close().map_err(WindowsWifiDirectGroupError::Winrt))
}

#[cfg(windows)]
fn close_device(device: &WiFiDirectDevice) -> Result<(), WindowsWifiDirectGroupError> {
    with_windows_runtime(|| device.Close().map_err(WindowsWifiDirectGroupError::Winrt))
}

#[cfg(windows)]
fn record_failure(
    failure: &mut Option<WindowsWifiDirectGroupError>,
    result: Result<(), WindowsWifiDirectGroupError>,
) {
    if failure.is_none() {
        if let Err(error) = result {
            *failure = Some(error);
        }
    }
}

#[cfg(windows)]
fn with_windows_runtime<T>(
    operation: impl FnOnce() -> Result<T, WindowsWifiDirectGroupError>,
) -> Result<T, WindowsWifiDirectGroupError> {
    let _runtime = crate::windows_runtime::WindowsRuntime::initialize()
        .ok_or(WindowsWifiDirectGroupError::WindowsRuntime)?;
    operation()
}

#[cfg(windows)]
async fn wait_for_operation<T: windows::core::RuntimeType + 'static>(
    operation: IAsyncOperation<T>,
) -> Result<T, WindowsWifiDirectGroupError> {
    let cancellation = operation.clone();
    timeout(WINDOWS_WIFI_DIRECT_OPERATION_TIMEOUT, operation)
        .await
        .map_err(|_| {
            let _ = cancellation.Cancel();
            WindowsWifiDirectGroupError::TimedOut
        })?
        .map_err(WindowsWifiDirectGroupError::Winrt)
}

#[cfg(test)]
mod tests {
    use super::{
        WindowsWifiDirectEndpoint, WindowsWifiDirectGroupError, WindowsWifiDirectPeer,
        endpoint_matches_owner_endpoint, endpoint_matches_remote_profile, validate_device_id,
        validate_wifi_direct_handoff,
    };
    use yeokcham_protocol::{
        DirectProfileConfig, LocalMeshTransportKind, WifiGroupConfiguration, WifiGroupCredential,
        WifiGroupHandoff, WifiGroupRole,
    };

    fn endpoint(local: &str, remote: &str) -> WindowsWifiDirectEndpoint {
        WindowsWifiDirectEndpoint {
            local_address: local.parse().unwrap(),
            remote_address: remote.parse().unwrap(),
        }
    }

    fn handoff(transport: LocalMeshTransportKind, role: WifiGroupRole) -> WifiGroupHandoff {
        WifiGroupHandoff::new(
            WifiGroupConfiguration::new(
                transport,
                role,
                "yeokcham-wfd".to_owned(),
                DirectProfileConfig::new("192.0.2.1:4444".parse().unwrap()).unwrap(),
            )
            .unwrap(),
            WifiGroupCredential::new(b"group-secret".to_vec()).unwrap(),
        )
    }

    #[test]
    fn validates_bounded_non_control_device_ids() {
        assert_eq!(
            WindowsWifiDirectPeer::new("\\\\?\\SWD#WiFiDirect#peer".to_owned())
                .unwrap()
                .as_str(),
            "\\\\?\\SWD#WiFiDirect#peer"
        );
        assert!(matches!(
            validate_device_id(""),
            Err(WindowsWifiDirectGroupError::InvalidDeviceId)
        ));
        assert!(matches!(
            validate_device_id("peer\u{7f}"),
            Err(WindowsWifiDirectGroupError::InvalidDeviceId)
        ));
        assert!(matches!(
            validate_device_id(&"x".repeat(1025)),
            Err(WindowsWifiDirectGroupError::InvalidDeviceId)
        ));
    }

    #[test]
    fn pins_owner_and_client_profiles_to_windows_endpoint_pairs() {
        let endpoints = [endpoint("192.0.2.1", "192.0.2.2")];
        assert!(endpoint_matches_owner_endpoint(
            &endpoints,
            "192.0.2.1:4444".parse().unwrap()
        ));
        assert!(!endpoint_matches_owner_endpoint(
            &endpoints,
            "192.0.2.9:4444".parse().unwrap()
        ));
        assert!(endpoint_matches_remote_profile(
            &endpoints,
            DirectProfileConfig::new("192.0.2.2:4444".parse().unwrap()).unwrap()
        ));
        assert!(!endpoint_matches_remote_profile(
            &endpoints,
            DirectProfileConfig::new("192.0.2.9:4444".parse().unwrap()).unwrap()
        ));
    }

    #[test]
    fn requires_an_owner_wifi_direct_handoff() {
        assert!(
            validate_wifi_direct_handoff(&handoff(
                LocalMeshTransportKind::WifiDirect,
                WifiGroupRole::Owner
            ))
            .is_ok()
        );
        assert!(matches!(
            validate_wifi_direct_handoff(&handoff(
                LocalMeshTransportKind::WifiHotspot,
                WifiGroupRole::Owner
            )),
            Err(WindowsWifiDirectGroupError::UnsupportedTransport(_))
        ));
        assert!(matches!(
            validate_wifi_direct_handoff(&handoff(
                LocalMeshTransportKind::WifiDirect,
                WifiGroupRole::Client
            )),
            Err(WindowsWifiDirectGroupError::InvalidHandoffRole)
        ));
    }
}
