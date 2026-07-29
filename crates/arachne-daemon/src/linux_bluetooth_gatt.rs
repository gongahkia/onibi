use std::{collections::HashSet, sync::Arc, time::Duration};

use arachne_core::{IdentityKeypair, IdentityPublicKey};
use arachne_protocol::{LocalMeshTransportKind, WireEnvelope, WireLimits};
use bluer::{
    AdapterEvent, Device, DiscoveryFilter, DiscoveryTransport,
    adv::{Advertisement, Type as AdvertisementType},
    gatt::{
        WriteOp,
        local::{
            Application, Characteristic, CharacteristicNotify, CharacteristicNotifyMethod,
            CharacteristicWrite, CharacteristicWriteMethod, ReqError, Service,
        },
        remote::{
            Characteristic as RemoteCharacteristic,
            CharacteristicWriteRequest as RemoteCharacteristicWriteRequest,
        },
    },
};
use futures_util::{FutureExt, StreamExt};
use tokio::{
    sync::{Mutex, mpsc, oneshot},
    task::JoinHandle,
    time::timeout,
};
use uuid::Uuid;

use crate::{
    AuthenticatedBluetoothGattTransport, BLUETOOTH_GATT_NOTIFY_UUID, BLUETOOTH_GATT_SERVICE_UUID,
    BLUETOOTH_GATT_WRITE_UUID, BluetoothGattError, BluetoothGattLink, BluetoothGattRole,
    DEFAULT_BLUETOOTH_GATT_PACKET_BYTES, LocalTransport, MAX_PENDING_BLUETOOTH_GATT_MESSAGES,
};

pub const LINUX_BLUETOOTH_GATT_OPERATION_TIMEOUT: Duration = Duration::from_secs(30);
pub const MAX_LINUX_BLUETOOTH_GATT_DISCOVERY_CANDIDATES: usize = 8;

pub struct LinuxBluetoothGattSession {
    transport: AuthenticatedBluetoothGattTransport,
    shutdown: Option<oneshot::Sender<()>>,
    worker: JoinHandle<()>,
}

impl LinuxBluetoothGattSession {
    pub async fn initiate(
        local_identity: &IdentityKeypair,
        expected_peer: &IdentityPublicKey,
    ) -> Result<Self, LinuxBluetoothGattSessionError> {
        if BluetoothGattRole::for_identities(&local_identity.public_key(), expected_peer)?
            != BluetoothGattRole::Central
        {
            return Err(LinuxBluetoothGattSessionError::Gatt(
                BluetoothGattError::RoleMismatch,
            ));
        }
        let (outgoing, mut outgoing_packets) = mpsc::channel(MAX_PENDING_BLUETOOTH_GATT_MESSAGES);
        let link = BluetoothGattLink::new(DEFAULT_BLUETOOTH_GATT_PACKET_BYTES, outgoing)?;
        let (device, write_characteristic, notify_characteristic) = timeout(
            LINUX_BLUETOOTH_GATT_OPERATION_TIMEOUT,
            discover_expected_service(),
        )
        .await
        .map_err(|_| LinuxBluetoothGattSessionError::TimedOut)??;
        let notifications = notify_characteristic
            .notify()
            .await
            .map_err(LinuxBluetoothGattSessionError::Bluez)?;
        let (shutdown, mut shutdown_receiver) = oneshot::channel();
        let worker_link = link.clone();
        let worker = tokio::spawn(async move {
            let mut notifications = Box::pin(notifications);
            let write_request = RemoteCharacteristicWriteRequest {
                op_type: WriteOp::Request,
                ..Default::default()
            };
            loop {
                tokio::select! {
                    _ = &mut shutdown_receiver => break,
                    packet = outgoing_packets.recv() => match packet {
                        Some(packet) => {
                            let sent = timeout(
                                LINUX_BLUETOOTH_GATT_OPERATION_TIMEOUT,
                                write_characteristic.write_ext(&packet, &write_request),
                            ).await;
                            if !matches!(sent, Ok(Ok(()))) {
                                break;
                            }
                        }
                        None => break,
                    },
                    packet = notifications.next() => match packet {
                        Some(packet) if worker_link.ingest_packet(&packet).await.is_ok() => (),
                        _ => break,
                    },
                }
            }
            worker_link.close().await;
            let _ = device.disconnect().await;
        });
        let transport = match AuthenticatedBluetoothGattTransport::initiate(
            link,
            local_identity,
            expected_peer,
        )
        .await
        {
            Ok(transport) => transport,
            Err(error) => {
                let _ = shutdown.send(());
                return Err(LinuxBluetoothGattSessionError::Gatt(error));
            }
        };
        Ok(Self {
            transport,
            shutdown: Some(shutdown),
            worker,
        })
    }

    pub async fn respond(
        local_identity: &IdentityKeypair,
        expected_peer: &IdentityPublicKey,
    ) -> Result<Self, LinuxBluetoothGattSessionError> {
        if BluetoothGattRole::for_identities(&local_identity.public_key(), expected_peer)?
            != BluetoothGattRole::Peripheral
        {
            return Err(LinuxBluetoothGattSessionError::Gatt(
                BluetoothGattError::RoleMismatch,
            ));
        }
        let (outgoing, outgoing_packets) = mpsc::channel(MAX_PENDING_BLUETOOTH_GATT_MESSAGES);
        let link = BluetoothGattLink::new(DEFAULT_BLUETOOTH_GATT_PACKET_BYTES, outgoing)?;
        let (shutdown, shutdown_receiver) = oneshot::channel();
        let (ready_sender, ready_receiver) = oneshot::channel();
        let (subscribed_sender, subscribed_receiver) = oneshot::channel();
        let worker_link = link.clone();
        let worker = tokio::spawn(run_peripheral(
            worker_link,
            outgoing_packets,
            shutdown_receiver,
            ready_sender,
            subscribed_sender,
        ));
        timeout(LINUX_BLUETOOTH_GATT_OPERATION_TIMEOUT, ready_receiver)
            .await
            .map_err(|_| LinuxBluetoothGattSessionError::TimedOut)?
            .map_err(|_| LinuxBluetoothGattSessionError::WorkerStopped)??;
        timeout(LINUX_BLUETOOTH_GATT_OPERATION_TIMEOUT, subscribed_receiver)
            .await
            .map_err(|_| LinuxBluetoothGattSessionError::TimedOut)?
            .map_err(|_| LinuxBluetoothGattSessionError::WorkerStopped)?;
        let transport =
            match AuthenticatedBluetoothGattTransport::respond(link, local_identity, expected_peer)
                .await
            {
                Ok(transport) => transport,
                Err(error) => {
                    let _ = shutdown.send(());
                    return Err(LinuxBluetoothGattSessionError::Gatt(error));
                }
            };
        Ok(Self {
            transport,
            shutdown: Some(shutdown),
            worker,
        })
    }

    #[must_use]
    pub const fn transport(&self) -> &AuthenticatedBluetoothGattTransport {
        &self.transport
    }
}

impl Drop for LinuxBluetoothGattSession {
    fn drop(&mut self) {
        if let Some(shutdown) = self.shutdown.take() {
            let _ = shutdown.send(());
        }
        let _ = &self.worker;
    }
}

impl LocalTransport for LinuxBluetoothGattSession {
    type Error = BluetoothGattError;

    fn transport_kind(&self) -> LocalMeshTransportKind {
        self.transport.transport_kind()
    }

    #[allow(clippy::manual_async_fn)]
    fn send_frame(
        &self,
        frame: &WireEnvelope,
        limits: WireLimits,
    ) -> impl std::future::Future<Output = Result<(), Self::Error>> + Send {
        self.transport.send_frame(frame, limits)
    }

    #[allow(clippy::manual_async_fn)]
    fn receive_frame(
        &self,
        limits: WireLimits,
    ) -> impl std::future::Future<Output = Result<WireEnvelope, Self::Error>> + Send {
        self.transport.receive_frame(limits)
    }
}

impl crate::BluetoothTransport for LinuxBluetoothGattSession {}

#[derive(Debug, thiserror::Error)]
pub enum LinuxBluetoothGattSessionError {
    #[error("BlueZ Bluetooth operation failed: {0}")]
    Bluez(#[source] bluer::Error),
    #[error("Bluetooth GATT protocol failed: {0}")]
    Gatt(#[from] BluetoothGattError),
    #[error("Bluetooth GATT operation timed out")]
    TimedOut,
    #[error("Bluetooth GATT worker stopped before setup completed")]
    WorkerStopped,
    #[error("Bluetooth GATT UUID is invalid")]
    InvalidUuid,
    #[error("no matching Bluetooth GATT peer was discovered")]
    PeerNotFound,
    #[error("matching Bluetooth GATT peer did not expose the required characteristics")]
    MissingCharacteristic,
    #[error("matching Bluetooth GATT peer exposed duplicate required characteristics")]
    AmbiguousCharacteristic,
}

async fn run_peripheral(
    link: Arc<BluetoothGattLink>,
    outgoing_packets: mpsc::Receiver<Vec<u8>>,
    shutdown: oneshot::Receiver<()>,
    ready: oneshot::Sender<Result<(), LinuxBluetoothGattSessionError>>,
    subscribed: oneshot::Sender<()>,
) {
    let (application, advertisement) =
        match build_peripheral(link.clone(), outgoing_packets, subscribed).await {
            Ok(handles) => handles,
            Err(error) => {
                let _ = ready.send(Err(error));
                link.close().await;
                return;
            }
        };
    if ready.send(Ok(())).is_err() {
        link.close().await;
        return;
    }
    let _ = shutdown.await;
    drop(application);
    drop(advertisement);
    link.close().await;
}

async fn build_peripheral(
    link: Arc<BluetoothGattLink>,
    outgoing_packets: mpsc::Receiver<Vec<u8>>,
    subscribed: oneshot::Sender<()>,
) -> Result<
    (
        bluer::gatt::local::ApplicationHandle,
        bluer::adv::AdvertisementHandle,
    ),
    LinuxBluetoothGattSessionError,
> {
    let service_uuid = parse_uuid(BLUETOOTH_GATT_SERVICE_UUID)?;
    let write_uuid = parse_uuid(BLUETOOTH_GATT_WRITE_UUID)?;
    let notify_uuid = parse_uuid(BLUETOOTH_GATT_NOTIFY_UUID)?;
    let session = bluer::Session::new()
        .await
        .map_err(LinuxBluetoothGattSessionError::Bluez)?;
    let adapter = session
        .default_adapter()
        .await
        .map_err(LinuxBluetoothGattSessionError::Bluez)?;
    let write_link = link.clone();
    let packet_receiver = Arc::new(Mutex::new(Some(outgoing_packets)));
    let notification_ready = Arc::new(std::sync::Mutex::new(Some(subscribed)));
    let packet_receiver_for_notification = packet_receiver.clone();
    let notification_ready_for_notification = notification_ready.clone();
    let application = Application {
        services: vec![Service {
            uuid: service_uuid,
            primary: true,
            characteristics: vec![
                Characteristic {
                    uuid: write_uuid,
                    write: Some(CharacteristicWrite {
                        write: true,
                        write_without_response: true,
                        method: CharacteristicWriteMethod::Fun(Box::new(move |packet, request| {
                            let link = write_link.clone();
                            async move {
                                if request.offset != 0 {
                                    return Err(ReqError::InvalidOffset);
                                }
                                link.ingest_packet(&packet)
                                    .await
                                    .map_err(|_| ReqError::NotAuthorized)
                            }
                            .boxed()
                        })),
                        ..Default::default()
                    }),
                    ..Default::default()
                },
                Characteristic {
                    uuid: notify_uuid,
                    notify: Some(CharacteristicNotify {
                        notify: true,
                        method: CharacteristicNotifyMethod::Fun(Box::new(move |mut notifier| {
                            let packet_receiver = packet_receiver_for_notification.clone();
                            let notification_ready = notification_ready_for_notification.clone();
                            async move {
                                let Some(mut packet_receiver) = packet_receiver.lock().await.take()
                                else {
                                    return;
                                };
                                if let Ok(mut notification_ready) = notification_ready.lock() {
                                    if let Some(ready) = notification_ready.take() {
                                        let _ = ready.send(());
                                    }
                                }
                                while let Some(packet) = packet_receiver.recv().await {
                                    if notifier.notify(packet).await.is_err() {
                                        break;
                                    }
                                }
                            }
                            .boxed()
                        })),
                        ..Default::default()
                    }),
                    ..Default::default()
                },
            ],
            ..Default::default()
        }],
        ..Default::default()
    };
    let application = adapter
        .serve_gatt_application(application)
        .await
        .map_err(LinuxBluetoothGattSessionError::Bluez)?;
    let advertisement = adapter
        .advertise(Advertisement {
            advertisement_type: AdvertisementType::Peripheral,
            service_uuids: [service_uuid].into_iter().collect(),
            discoverable: Some(true),
            ..Default::default()
        })
        .await
        .map_err(LinuxBluetoothGattSessionError::Bluez)?;
    Ok((application, advertisement))
}

async fn discover_expected_service()
-> Result<(Device, RemoteCharacteristic, RemoteCharacteristic), LinuxBluetoothGattSessionError> {
    let service_uuid = parse_uuid(BLUETOOTH_GATT_SERVICE_UUID)?;
    let write_uuid = parse_uuid(BLUETOOTH_GATT_WRITE_UUID)?;
    let notify_uuid = parse_uuid(BLUETOOTH_GATT_NOTIFY_UUID)?;
    let session = bluer::Session::new()
        .await
        .map_err(LinuxBluetoothGattSessionError::Bluez)?;
    let adapter = session
        .default_adapter()
        .await
        .map_err(LinuxBluetoothGattSessionError::Bluez)?;
    adapter
        .set_discovery_filter(DiscoveryFilter {
            uuids: HashSet::from([service_uuid]),
            transport: DiscoveryTransport::Le,
            ..Default::default()
        })
        .await
        .map_err(LinuxBluetoothGattSessionError::Bluez)?;
    let discovery = adapter
        .discover_devices()
        .await
        .map_err(LinuxBluetoothGattSessionError::Bluez)?;
    futures_util::pin_mut!(discovery);
    let mut candidates: usize = 0;
    while let Some(event) = discovery.next().await {
        let AdapterEvent::DeviceAdded(address) = event else {
            continue;
        };
        candidates = candidates.saturating_add(1);
        if candidates > MAX_LINUX_BLUETOOTH_GATT_DISCOVERY_CANDIDATES {
            return Err(LinuxBluetoothGattSessionError::PeerNotFound);
        }
        let device = adapter
            .device(address)
            .map_err(LinuxBluetoothGattSessionError::Bluez)?;
        let uuids = device
            .uuids()
            .await
            .map_err(LinuxBluetoothGattSessionError::Bluez)?
            .unwrap_or_default();
        if !uuids.contains(&service_uuid) {
            continue;
        }
        if device
            .connect()
            .await
            .map_err(LinuxBluetoothGattSessionError::Bluez)
            .is_err()
        {
            continue;
        }
        match find_remote_characteristics(&device, service_uuid, write_uuid, notify_uuid).await {
            Ok((write, notify)) => return Ok((device, write, notify)),
            Err(_) => {
                let _ = device.disconnect().await;
            }
        }
    }
    Err(LinuxBluetoothGattSessionError::PeerNotFound)
}

async fn find_remote_characteristics(
    device: &Device,
    service_uuid: Uuid,
    write_uuid: Uuid,
    notify_uuid: Uuid,
) -> Result<(RemoteCharacteristic, RemoteCharacteristic), LinuxBluetoothGattSessionError> {
    let mut write = None;
    let mut notify = None;
    for service in device
        .services()
        .await
        .map_err(LinuxBluetoothGattSessionError::Bluez)?
    {
        if service
            .uuid()
            .await
            .map_err(LinuxBluetoothGattSessionError::Bluez)?
            != service_uuid
        {
            continue;
        }
        for characteristic in service
            .characteristics()
            .await
            .map_err(LinuxBluetoothGattSessionError::Bluez)?
        {
            let characteristic_uuid = characteristic
                .uuid()
                .await
                .map_err(LinuxBluetoothGattSessionError::Bluez)?;
            if characteristic_uuid == write_uuid {
                if write.replace(characteristic).is_some() {
                    return Err(LinuxBluetoothGattSessionError::AmbiguousCharacteristic);
                }
            } else if characteristic_uuid == notify_uuid && notify.replace(characteristic).is_some()
            {
                return Err(LinuxBluetoothGattSessionError::AmbiguousCharacteristic);
            }
        }
    }
    match (write, notify) {
        (Some(write), Some(notify)) => Ok((write, notify)),
        _ => Err(LinuxBluetoothGattSessionError::MissingCharacteristic),
    }
}

fn parse_uuid(value: &str) -> Result<Uuid, LinuxBluetoothGattSessionError> {
    Uuid::parse_str(value).map_err(|_| LinuxBluetoothGattSessionError::InvalidUuid)
}

#[cfg(test)]
mod tests {
    use super::{
        BluetoothGattError, BluetoothGattRole, LinuxBluetoothGattSession,
        LinuxBluetoothGattSessionError,
    };
    use arachne_core::IdentityKeypair;

    #[tokio::test]
    async fn rejects_the_non_central_identity_before_discovery() {
        let first = IdentityKeypair::generate().unwrap();
        let second = IdentityKeypair::generate().unwrap();
        let (central, peripheral) = if BluetoothGattRole::for_identities(
            &first.public_key(),
            &second.public_key(),
        )
        .unwrap()
            == BluetoothGattRole::Central
        {
            (&first, &second)
        } else {
            (&second, &first)
        };
        assert!(matches!(
            LinuxBluetoothGattSession::initiate(peripheral, &central.public_key()).await,
            Err(LinuxBluetoothGattSessionError::Gatt(
                BluetoothGattError::RoleMismatch
            ))
        ));
        assert!(matches!(
            LinuxBluetoothGattSession::respond(central, &peripheral.public_key()).await,
            Err(LinuxBluetoothGattSessionError::Gatt(
                BluetoothGattError::RoleMismatch
            ))
        ));
    }
}
