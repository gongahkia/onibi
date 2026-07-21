use std::{
    collections::HashMap,
    fmt,
    future::Future,
    net::{IpAddr, Ipv4Addr, SocketAddr},
    time::Duration,
};

use tokio::time::{sleep, timeout};
use yeokcham_protocol::{
    LocalMeshTransportKind, WifiGroupBootstrap, WifiGroupCredential, WifiGroupHandoff,
};
use zbus::zvariant::{OwnedObjectPath, OwnedValue, Value};
use zbus::{Connection, Proxy};

use crate::WifiGroupLifecycle;

const NETWORK_MANAGER_DESTINATION: &str = "org.freedesktop.NetworkManager";
const NETWORK_MANAGER_PATH: &str = "/org/freedesktop/NetworkManager";
const NETWORK_MANAGER_INTERFACE: &str = "org.freedesktop.NetworkManager";
const ACTIVE_CONNECTION_INTERFACE: &str = "org.freedesktop.NetworkManager.Connection.Active";
const IP4_CONFIG_INTERFACE: &str = "org.freedesktop.NetworkManager.IP4Config";
const ACTIVATED_STATE: u32 = 2;
const DEACTIVATED_STATE: u32 = 4;
pub const MAX_LINUX_NETWORK_INTERFACE_BYTES: usize = 15;
pub const NETWORK_MANAGER_OPERATION_TIMEOUT: Duration = Duration::from_secs(10);
pub const NETWORK_MANAGER_ACTIVATION_TIMEOUT: Duration = Duration::from_secs(30);
const ACTIVATION_POLL_INTERVAL: Duration = Duration::from_millis(100);

type NetworkManagerSettings = HashMap<String, HashMap<String, OwnedValue>>;

pub struct LinuxNetworkManagerWifiGroup {
    connection: Connection,
    interface: String,
    p2p_peer: Option<LinuxWifiP2pPeer>,
    active_connection: Option<OwnedObjectPath>,
    owner_address: Option<IpAddr>,
}

impl LinuxNetworkManagerWifiGroup {
    pub async fn connect(interface: String) -> Result<Self, LinuxNetworkManagerWifiGroupError> {
        validate_interface(&interface)?;
        Ok(Self {
            connection: bounded(Connection::system()).await?,
            interface,
            p2p_peer: None,
            active_connection: None,
            owner_address: None,
        })
    }

    #[must_use]
    pub fn with_p2p_peer(mut self, p2p_peer: LinuxWifiP2pPeer) -> Self {
        self.p2p_peer = Some(p2p_peer);
        self
    }

    async fn activate_owner(
        &mut self,
        bootstrap: &WifiGroupBootstrap,
    ) -> Result<(), LinuxNetworkManagerWifiGroupError> {
        let owner_address = self
            .activate(
                bootstrap.transport(),
                bootstrap.identifier(),
                bootstrap.credential(),
                true,
            )
            .await?;
        self.owner_address = Some(owner_address);
        Ok(())
    }

    async fn join_client(
        &mut self,
        handoff: &WifiGroupHandoff,
    ) -> Result<(), LinuxNetworkManagerWifiGroupError> {
        self.activate(
            handoff.configuration().transport(),
            handoff.configuration().identifier(),
            handoff.credential(),
            false,
        )
        .await
        .map(|_| ())
    }

    async fn activate(
        &mut self,
        transport: LocalMeshTransportKind,
        identifier: &str,
        credential: &WifiGroupCredential,
        owner: bool,
    ) -> Result<IpAddr, LinuxNetworkManagerWifiGroupError> {
        if self.active_connection.is_some() {
            return Err(LinuxNetworkManagerWifiGroupError::AlreadyActive);
        }
        let settings = self.settings(transport, identifier, credential, owner)?;
        let root = self.root_proxy().await?;
        let device: OwnedObjectPath =
            bounded(root.call("GetDeviceByIpIface", &self.interface)).await?;
        let null_path = OwnedObjectPath::try_from("/")
            .map_err(LinuxNetworkManagerWifiGroupError::InvalidObjectPath)?;
        let options = activation_options()?;
        let (_, active_connection, _): (
            OwnedObjectPath,
            OwnedObjectPath,
            HashMap<String, OwnedValue>,
        ) = bounded(root.call(
            "AddAndActivateConnection2",
            &(settings, device, null_path, options),
        ))
        .await?;
        self.active_connection = Some(active_connection.clone());
        if let Err(error) = self.wait_for_activation(&active_connection).await {
            let _ = self.deactivate(&active_connection).await;
            self.active_connection = None;
            return Err(error);
        }
        match self.owner_address(&active_connection).await {
            Ok(address) => Ok(address),
            Err(error) => {
                let _ = self.deactivate(&active_connection).await;
                self.active_connection = None;
                Err(error)
            }
        }
    }

    async fn root_proxy(&self) -> Result<Proxy<'_>, LinuxNetworkManagerWifiGroupError> {
        bounded(Proxy::new(
            &self.connection,
            NETWORK_MANAGER_DESTINATION,
            NETWORK_MANAGER_PATH,
            NETWORK_MANAGER_INTERFACE,
        ))
        .await
    }

    async fn active_proxy<'a>(
        &'a self,
        active_connection: &'a OwnedObjectPath,
    ) -> Result<Proxy<'a>, LinuxNetworkManagerWifiGroupError> {
        bounded(Proxy::new(
            &self.connection,
            NETWORK_MANAGER_DESTINATION,
            active_connection.as_str(),
            ACTIVE_CONNECTION_INTERFACE,
        ))
        .await
    }

    async fn wait_for_activation(
        &self,
        active_connection: &OwnedObjectPath,
    ) -> Result<(), LinuxNetworkManagerWifiGroupError> {
        timeout(NETWORK_MANAGER_ACTIVATION_TIMEOUT, async {
            let proxy = self.active_proxy(active_connection).await?;
            loop {
                let state: u32 = bounded(proxy.get_property("State")).await?;
                if state == ACTIVATED_STATE {
                    return Ok(());
                }
                if state == DEACTIVATED_STATE {
                    return Err(LinuxNetworkManagerWifiGroupError::ActivationFailed);
                }
                sleep(ACTIVATION_POLL_INTERVAL).await;
            }
        })
        .await
        .map_err(|_| LinuxNetworkManagerWifiGroupError::ActivationTimedOut)?
    }

    async fn owner_address(
        &self,
        active_connection: &OwnedObjectPath,
    ) -> Result<IpAddr, LinuxNetworkManagerWifiGroupError> {
        let active_proxy = self.active_proxy(active_connection).await?;
        let ip4_config: OwnedObjectPath = bounded(active_proxy.get_property("Ip4Config")).await?;
        if ip4_config.as_str() == "/" {
            return Err(LinuxNetworkManagerWifiGroupError::MissingIpv4Address);
        }
        let ip4_proxy = bounded(Proxy::new(
            &self.connection,
            NETWORK_MANAGER_DESTINATION,
            ip4_config.as_str(),
            IP4_CONFIG_INTERFACE,
        ))
        .await?;
        let address_data: Vec<HashMap<String, OwnedValue>> =
            bounded(ip4_proxy.get_property("AddressData")).await?;
        address_data
            .into_iter()
            .filter_map(|address| address.get("address").cloned())
            .filter_map(|address| String::try_from(address).ok())
            .filter_map(|address| address.parse::<Ipv4Addr>().ok())
            .find(|address| {
                !address.is_unspecified()
                    && !address.is_loopback()
                    && !address.is_multicast()
                    && !address.is_broadcast()
            })
            .map(IpAddr::V4)
            .ok_or(LinuxNetworkManagerWifiGroupError::MissingIpv4Address)
    }

    async fn teardown(&mut self) -> Result<(), LinuxNetworkManagerWifiGroupError> {
        let Some(active_connection) = self.active_connection.clone() else {
            return Ok(());
        };
        self.deactivate(&active_connection).await?;
        self.active_connection = None;
        self.owner_address = None;
        Ok(())
    }

    async fn deactivate(
        &self,
        active_connection: &OwnedObjectPath,
    ) -> Result<(), LinuxNetworkManagerWifiGroupError> {
        let root = self.root_proxy().await?;
        bounded(root.call::<_, _, ()>("DeactivateConnection", active_connection)).await
    }

    fn settings(
        &self,
        transport: LocalMeshTransportKind,
        identifier: &str,
        credential: &WifiGroupCredential,
        owner: bool,
    ) -> Result<NetworkManagerSettings, LinuxNetworkManagerWifiGroupError> {
        match transport {
            LocalMeshTransportKind::WifiHotspot => {
                hotspot_settings(&self.interface, identifier, credential, owner)
            }
            LocalMeshTransportKind::WifiDirect => p2p_settings(
                &self.interface,
                identifier,
                self.p2p_peer
                    .as_ref()
                    .ok_or(LinuxNetworkManagerWifiGroupError::MissingP2pPeer)?,
            ),
            _ => Err(LinuxNetworkManagerWifiGroupError::UnsupportedTransport(
                transport,
            )),
        }
    }
}

impl fmt::Debug for LinuxNetworkManagerWifiGroup {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("LinuxNetworkManagerWifiGroup")
            .field("interface", &self.interface)
            .field("p2p_peer", &self.p2p_peer)
            .field("active", &self.active_connection.is_some())
            .finish_non_exhaustive()
    }
}

impl WifiGroupLifecycle for LinuxNetworkManagerWifiGroup {
    type Error = LinuxNetworkManagerWifiGroupError;

    fn activate_owner(
        &mut self,
        bootstrap: &WifiGroupBootstrap,
    ) -> impl Future<Output = Result<(), Self::Error>> + Send {
        self.activate_owner(bootstrap)
    }

    fn validate_owner_endpoint(&self, endpoint: SocketAddr) -> Result<(), Self::Error> {
        let owner_address = self
            .owner_address
            .ok_or(LinuxNetworkManagerWifiGroupError::MissingOwnerAddress)?;
        if endpoint.ip() != owner_address {
            return Err(LinuxNetworkManagerWifiGroupError::EndpointAddressMismatch {
                endpoint,
                owner_address,
            });
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

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct LinuxWifiP2pPeer(String);

impl LinuxWifiP2pPeer {
    pub fn new(address: &str) -> Result<Self, LinuxNetworkManagerWifiGroupError> {
        let mut bytes = [0_u8; 6];
        for (index, component) in address.split(':').enumerate() {
            if index >= bytes.len() || component.len() != 2 {
                return Err(LinuxNetworkManagerWifiGroupError::InvalidP2pPeer);
            }
            bytes[index] = u8::from_str_radix(component, 16)
                .map_err(|_| LinuxNetworkManagerWifiGroupError::InvalidP2pPeer)?;
        }
        if address.split(':').count() != bytes.len() || bytes.iter().all(|byte| *byte == 0) {
            return Err(LinuxNetworkManagerWifiGroupError::InvalidP2pPeer);
        }
        Ok(Self(
            bytes
                .iter()
                .map(|byte| format!("{byte:02x}"))
                .collect::<Vec<_>>()
                .join(":"),
        ))
    }

    #[must_use]
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[derive(Debug, thiserror::Error)]
pub enum LinuxNetworkManagerWifiGroupError {
    #[error("Linux network interface is invalid")]
    InvalidInterface,
    #[error("Wi-Fi Direct peer MAC address is invalid")]
    InvalidP2pPeer,
    #[error("Wi-Fi Direct requires a peer MAC address discovered over authenticated BLE")]
    MissingP2pPeer,
    #[error("Wi-Fi hotspot credentials must be 8 to 63 printable ASCII bytes")]
    InvalidHotspotCredential,
    #[error("unsupported Linux Wi-Fi group transport: {0:?}")]
    UnsupportedTransport(LocalMeshTransportKind),
    #[error("Linux Wi-Fi group is already active")]
    AlreadyActive,
    #[error("NetworkManager activation timed out")]
    ActivationTimedOut,
    #[error("NetworkManager deactivated the Wi-Fi group before activation completed")]
    ActivationFailed,
    #[error("NetworkManager did not provide a usable IPv4 address")]
    MissingIpv4Address,
    #[error("Linux Wi-Fi group owner address is unavailable")]
    MissingOwnerAddress,
    #[error("direct endpoint {endpoint} does not bind active owner address {owner_address}")]
    EndpointAddressMismatch {
        endpoint: SocketAddr,
        owner_address: IpAddr,
    },
    #[error("NetworkManager D-Bus operation timed out")]
    OperationTimedOut,
    #[error("NetworkManager D-Bus operation failed: {0}")]
    Dbus(#[source] zbus::Error),
    #[error("NetworkManager returned an invalid object path: {0}")]
    InvalidObjectPath(#[source] zbus::zvariant::Error),
    #[error("NetworkManager value encoding failed: {0}")]
    Variant(#[source] zbus::zvariant::Error),
}

async fn bounded<T>(
    operation: impl Future<Output = zbus::Result<T>>,
) -> Result<T, LinuxNetworkManagerWifiGroupError> {
    timeout(NETWORK_MANAGER_OPERATION_TIMEOUT, operation)
        .await
        .map_err(|_| LinuxNetworkManagerWifiGroupError::OperationTimedOut)?
        .map_err(LinuxNetworkManagerWifiGroupError::Dbus)
}

fn validate_interface(interface: &str) -> Result<(), LinuxNetworkManagerWifiGroupError> {
    if interface.is_empty()
        || interface.len() > MAX_LINUX_NETWORK_INTERFACE_BYTES
        || interface
            .bytes()
            .any(|byte| !byte.is_ascii_graphic() || byte == b'/')
    {
        return Err(LinuxNetworkManagerWifiGroupError::InvalidInterface);
    }
    Ok(())
}

fn activation_options() -> Result<HashMap<String, OwnedValue>, LinuxNetworkManagerWifiGroupError> {
    Ok(HashMap::from([
        ("persist".to_owned(), owned_value("volatile".to_owned())?),
        (
            "bind-activation".to_owned(),
            owned_value("dbus-client".to_owned())?,
        ),
    ]))
}

fn hotspot_settings(
    interface: &str,
    identifier: &str,
    credential: &WifiGroupCredential,
    owner: bool,
) -> Result<NetworkManagerSettings, LinuxNetworkManagerWifiGroupError> {
    let psk = hotspot_psk(credential)?;
    let mut settings = common_settings(interface, identifier, "802-11-wireless")?;
    let mut wireless = HashMap::from([(
        "ssid".to_owned(),
        owned_value(identifier.as_bytes().to_vec())?,
    )]);
    if owner {
        wireless.insert("mode".to_owned(), owned_value("ap".to_owned())?);
    }
    settings.insert("802-11-wireless".to_owned(), wireless);
    settings.insert(
        "802-11-wireless-security".to_owned(),
        HashMap::from([
            ("key-mgmt".to_owned(), owned_value("wpa-psk".to_owned())?),
            ("psk".to_owned(), owned_value(psk)?),
            ("proto".to_owned(), owned_value(vec!["rsn".to_owned()])?),
        ]),
    );
    settings.insert(
        "ipv4".to_owned(),
        HashMap::from([(
            "method".to_owned(),
            owned_value(if owner { "shared" } else { "auto" }.to_owned())?,
        )]),
    );
    settings.insert(
        "ipv6".to_owned(),
        HashMap::from([("method".to_owned(), owned_value("disabled".to_owned())?)]),
    );
    Ok(settings)
}

fn p2p_settings(
    interface: &str,
    identifier: &str,
    peer: &LinuxWifiP2pPeer,
) -> Result<NetworkManagerSettings, LinuxNetworkManagerWifiGroupError> {
    let mut settings = common_settings(interface, identifier, "wifi-p2p")?;
    settings.insert(
        "wifi-p2p".to_owned(),
        HashMap::from([("peer".to_owned(), owned_value(peer.as_str().to_owned())?)]),
    );
    settings.insert(
        "ipv4".to_owned(),
        HashMap::from([("method".to_owned(), owned_value("auto".to_owned())?)]),
    );
    settings.insert(
        "ipv6".to_owned(),
        HashMap::from([("method".to_owned(), owned_value("disabled".to_owned())?)]),
    );
    Ok(settings)
}

fn common_settings(
    interface: &str,
    identifier: &str,
    connection_type: &str,
) -> Result<NetworkManagerSettings, LinuxNetworkManagerWifiGroupError> {
    Ok(HashMap::from([(
        "connection".to_owned(),
        HashMap::from([
            ("id".to_owned(), owned_value(identifier.to_owned())?),
            ("type".to_owned(), owned_value(connection_type.to_owned())?),
            (
                "interface-name".to_owned(),
                owned_value(interface.to_owned())?,
            ),
            ("autoconnect".to_owned(), OwnedValue::from(false)),
        ]),
    )]))
}

fn hotspot_psk(
    credential: &WifiGroupCredential,
) -> Result<String, LinuxNetworkManagerWifiGroupError> {
    let psk = std::str::from_utf8(credential.as_bytes())
        .map_err(|_| LinuxNetworkManagerWifiGroupError::InvalidHotspotCredential)?;
    if !(8..=63).contains(&psk.len()) || psk.bytes().any(|byte| !byte.is_ascii_graphic()) {
        return Err(LinuxNetworkManagerWifiGroupError::InvalidHotspotCredential);
    }
    Ok(psk.to_owned())
}

fn owned_value<T>(value: T) -> Result<OwnedValue, LinuxNetworkManagerWifiGroupError>
where
    T: Into<Value<'static>> + zbus::zvariant::DynamicType,
{
    OwnedValue::try_from(Value::new(value)).map_err(LinuxNetworkManagerWifiGroupError::Variant)
}

#[cfg(test)]
mod tests {
    use super::{
        LinuxNetworkManagerWifiGroupError, LinuxWifiP2pPeer, activation_options, hotspot_psk,
        hotspot_settings, p2p_settings, validate_interface,
    };
    use yeokcham_protocol::WifiGroupCredential;

    #[test]
    fn validates_interface_peer_and_wpa2_psk_inputs() {
        assert!(validate_interface("wlan0").is_ok());
        assert!(matches!(
            validate_interface("bad/interface"),
            Err(LinuxNetworkManagerWifiGroupError::InvalidInterface)
        ));
        assert_eq!(
            LinuxWifiP2pPeer::new("AA:BB:CC:DD:EE:FF").unwrap().as_str(),
            "aa:bb:cc:dd:ee:ff"
        );
        assert!(matches!(
            LinuxWifiP2pPeer::new("00:00:00:00:00:00"),
            Err(LinuxNetworkManagerWifiGroupError::InvalidP2pPeer)
        ));
        assert!(matches!(
            LinuxWifiP2pPeer::new("0:01:02:03:04:05"),
            Err(LinuxNetworkManagerWifiGroupError::InvalidP2pPeer)
        ));
        assert!(matches!(
            hotspot_psk(&WifiGroupCredential::new(b"1234567".to_vec()).unwrap()),
            Err(LinuxNetworkManagerWifiGroupError::InvalidHotspotCredential)
        ));
    }

    #[test]
    fn uses_volatile_rsn_hotspot_and_peer_bound_p2p_settings() {
        let credential = WifiGroupCredential::new(b"password1".to_vec()).unwrap();
        let owner = hotspot_settings("wlan0", "yeokcham", &credential, true).unwrap();
        let client = hotspot_settings("wlan0", "yeokcham", &credential, false).unwrap();

        assert_eq!(
            String::try_from(owner["802-11-wireless"]["mode"].clone()).unwrap(),
            "ap"
        );
        assert_eq!(
            String::try_from(owner["ipv4"]["method"].clone()).unwrap(),
            "shared"
        );
        assert!(!client["802-11-wireless"].contains_key("mode"));
        assert_eq!(
            String::try_from(client["ipv4"]["method"].clone()).unwrap(),
            "auto"
        );
        assert_eq!(
            String::try_from(owner["802-11-wireless-security"]["key-mgmt"].clone()).unwrap(),
            "wpa-psk"
        );
        assert_eq!(
            Vec::<String>::try_from(owner["802-11-wireless-security"]["proto"].clone()).unwrap(),
            ["rsn"]
        );
        let options = activation_options().unwrap();
        assert_eq!(
            String::try_from(options["persist"].clone()).unwrap(),
            "volatile"
        );
        assert_eq!(
            String::try_from(options["bind-activation"].clone()).unwrap(),
            "dbus-client"
        );

        let peer = LinuxWifiP2pPeer::new("AA:BB:CC:DD:EE:FF").unwrap();
        let p2p = p2p_settings("wlan0", "yeokcham", &peer).unwrap();
        assert_eq!(
            String::try_from(p2p["wifi-p2p"]["peer"].clone()).unwrap(),
            "aa:bb:cc:dd:ee:ff"
        );
    }
}
