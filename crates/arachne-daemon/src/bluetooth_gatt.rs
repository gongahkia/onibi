use std::{
    cmp::Ordering,
    future::Future,
    sync::{
        Arc,
        atomic::{AtomicU16, Ordering as AtomicOrdering},
    },
};

use arachne_core::{IdentityKeypair, IdentityPublicKey};
use arachne_protocol::{
    LocalLinkAuthentication, LocalLinkHandshake, LocalLinkHandshakeError, LocalLinkOffer,
    LocalLinkResponse, LocalLinkRole, LocalMeshTransportKind, WireEnvelope, WireError, WireLimits,
};
use tokio::sync::{Mutex, mpsc};

use crate::{BluetoothTransport, LocalTransport};

pub const BLUETOOTH_GATT_SERVICE_UUID: &str = "e4c4e40b-d6e8-46e5-aa0b-4f9684a8ab00";
pub const BLUETOOTH_GATT_WRITE_UUID: &str = "e4c4e40b-d6e8-46e5-aa0b-4f9684a8ab01";
pub const BLUETOOTH_GATT_NOTIFY_UUID: &str = "e4c4e40b-d6e8-46e5-aa0b-4f9684a8ab02";
pub const BLUETOOTH_GATT_SCHEMA_VERSION: u8 = 1;
pub const BLUETOOTH_GATT_PACKET_HEADER_BYTES: usize = 12;
pub const MIN_BLUETOOTH_GATT_PACKET_BYTES: usize = 20;
pub const MAX_BLUETOOTH_GATT_PACKET_BYTES: usize = 512;
pub const DEFAULT_BLUETOOTH_GATT_PACKET_BYTES: usize = MIN_BLUETOOTH_GATT_PACKET_BYTES;
pub const MAX_BLUETOOTH_GATT_MESSAGE_BYTES: usize = 65_536;
pub const MAX_BLUETOOTH_GATT_FRAGMENT_COUNT: usize = 8_192;
pub const MAX_BLUETOOTH_GATT_MESSAGES_PER_DIRECTION: u16 = 4_096;
pub const MAX_PENDING_BLUETOOTH_GATT_MESSAGES: usize = 8;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum BluetoothGattRole {
    Central,
    Peripheral,
}

impl BluetoothGattRole {
    pub fn for_identities(
        local_identity: &IdentityPublicKey,
        peer_identity: &IdentityPublicKey,
    ) -> Result<Self, BluetoothGattError> {
        match local_identity.as_bytes().cmp(peer_identity.as_bytes()) {
            Ordering::Less => Ok(Self::Central),
            Ordering::Greater => Ok(Self::Peripheral),
            Ordering::Equal => Err(BluetoothGattError::IdenticalPeerIdentity),
        }
    }

    #[must_use]
    pub const fn local_link_role(self) -> LocalLinkRole {
        match self {
            Self::Central => LocalLinkRole::Initiator,
            Self::Peripheral => LocalLinkRole::Responder,
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum BluetoothGattMessageKind {
    Offer = 1,
    Response = 2,
    Authentication = 3,
    Envelope = 4,
}

impl TryFrom<u8> for BluetoothGattMessageKind {
    type Error = BluetoothGattError;

    fn try_from(value: u8) -> Result<Self, Self::Error> {
        match value {
            1 => Ok(Self::Offer),
            2 => Ok(Self::Response),
            3 => Ok(Self::Authentication),
            4 => Ok(Self::Envelope),
            _ => Err(BluetoothGattError::InvalidMessageKind(value)),
        }
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum BluetoothGattError {
    #[error("Bluetooth GATT packet size is outside the supported range")]
    InvalidPacketSize,
    #[error("Bluetooth GATT peers must use distinct identities")]
    IdenticalPeerIdentity,
    #[error("Bluetooth GATT packet is too short")]
    PacketTooShort,
    #[error("Bluetooth GATT packet exceeds the configured packet size")]
    PacketTooLarge,
    #[error("Bluetooth GATT packet schema version is unsupported: {0}")]
    UnsupportedSchemaVersion(u8),
    #[error("Bluetooth GATT message kind is invalid: {0}")]
    InvalidMessageKind(u8),
    #[error("Bluetooth GATT message length is invalid")]
    InvalidMessageLength,
    #[error("Bluetooth GATT fragment count is invalid")]
    InvalidFragmentCount,
    #[error("Bluetooth GATT fragment index is invalid")]
    InvalidFragmentIndex,
    #[error("Bluetooth GATT fragment has an invalid payload length")]
    InvalidFragmentLength,
    #[error("Bluetooth GATT fragments are not in order")]
    FragmentOutOfOrder,
    #[error("Bluetooth GATT fragment metadata changed during a message")]
    FragmentMetadataMismatch,
    #[error("Bluetooth GATT direction exceeded its message limit")]
    MessageLimitReached,
    #[error("Bluetooth GATT receive queue is full")]
    ReceiveQueueFull,
    #[error("Bluetooth GATT link disconnected")]
    Disconnected,
    #[error("Bluetooth GATT message is not valid for this state")]
    UnexpectedMessage,
    #[error("Bluetooth GATT local role does not match the authenticated identities")]
    RoleMismatch,
    #[error("Bluetooth GATT local-link handshake failed: {0}")]
    Handshake(#[source] LocalLinkHandshakeError),
    #[error("Bluetooth GATT envelope is invalid: {0}")]
    Wire(#[source] WireError),
}

#[derive(Clone, Debug, Eq, PartialEq)]
struct BluetoothGattMessage {
    kind: BluetoothGattMessageKind,
    payload: Vec<u8>,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
struct BluetoothGattFragmentHeader {
    kind: BluetoothGattMessageKind,
    message_id: u16,
    index: u16,
    count: u16,
    message_length: usize,
}

impl BluetoothGattFragmentHeader {
    fn decode(packet: &[u8], packet_bytes: usize) -> Result<(Self, &[u8]), BluetoothGattError> {
        if packet.len() < BLUETOOTH_GATT_PACKET_HEADER_BYTES {
            return Err(BluetoothGattError::PacketTooShort);
        }
        if packet.len() > packet_bytes {
            return Err(BluetoothGattError::PacketTooLarge);
        }
        if packet[0] != BLUETOOTH_GATT_SCHEMA_VERSION {
            return Err(BluetoothGattError::UnsupportedSchemaVersion(packet[0]));
        }
        let kind = BluetoothGattMessageKind::try_from(packet[1])?;
        let message_id = u16::from_le_bytes([packet[2], packet[3]]);
        let index = u16::from_le_bytes([packet[4], packet[5]]);
        let count = u16::from_le_bytes([packet[6], packet[7]]);
        let message_length = usize::try_from(u32::from_le_bytes([
            packet[8], packet[9], packet[10], packet[11],
        ]))
        .map_err(|_| BluetoothGattError::InvalidMessageLength)?;
        let header = Self {
            kind,
            message_id,
            index,
            count,
            message_length,
        };
        header.validate(
            packet.len() - BLUETOOTH_GATT_PACKET_HEADER_BYTES,
            packet_bytes,
        )?;
        Ok((header, &packet[BLUETOOTH_GATT_PACKET_HEADER_BYTES..]))
    }

    fn validate(
        &self,
        payload_length: usize,
        packet_bytes: usize,
    ) -> Result<(), BluetoothGattError> {
        if self.message_length == 0 || self.message_length > MAX_BLUETOOTH_GATT_MESSAGE_BYTES {
            return Err(BluetoothGattError::InvalidMessageLength);
        }
        let payload_capacity = packet_bytes
            .checked_sub(BLUETOOTH_GATT_PACKET_HEADER_BYTES)
            .ok_or(BluetoothGattError::InvalidPacketSize)?;
        let expected_count = self.message_length.div_ceil(payload_capacity);
        if expected_count == 0
            || expected_count > MAX_BLUETOOTH_GATT_FRAGMENT_COUNT
            || usize::from(self.count) != expected_count
        {
            return Err(BluetoothGattError::InvalidFragmentCount);
        }
        if usize::from(self.index) >= expected_count {
            return Err(BluetoothGattError::InvalidFragmentIndex);
        }
        let expected_payload_length = if usize::from(self.index) + 1 == expected_count {
            self.message_length - payload_capacity * (expected_count - 1)
        } else {
            payload_capacity
        };
        if payload_length != expected_payload_length {
            return Err(BluetoothGattError::InvalidFragmentLength);
        }
        Ok(())
    }
}

struct ActiveBluetoothGattMessage {
    header: BluetoothGattFragmentHeader,
    next_index: u16,
    payload: Vec<u8>,
}

struct BluetoothGattReassembler {
    active: Option<ActiveBluetoothGattMessage>,
    next_message_id: u16,
    received_messages: u16,
    packet_bytes: usize,
}

impl BluetoothGattReassembler {
    const fn new(packet_bytes: usize) -> Self {
        Self {
            active: None,
            next_message_id: 0,
            received_messages: 0,
            packet_bytes,
        }
    }

    fn ingest(
        &mut self,
        packet: &[u8],
    ) -> Result<Option<BluetoothGattMessage>, BluetoothGattError> {
        let (header, payload) = BluetoothGattFragmentHeader::decode(packet, self.packet_bytes)?;
        if self.received_messages >= MAX_BLUETOOTH_GATT_MESSAGES_PER_DIRECTION {
            return Err(BluetoothGattError::MessageLimitReached);
        }
        if header.message_id != self.next_message_id {
            self.active = None;
            return Err(BluetoothGattError::FragmentOutOfOrder);
        }
        if header.index == 0 {
            if self.active.is_some() {
                self.active = None;
                return Err(BluetoothGattError::FragmentOutOfOrder);
            }
            self.active = Some(ActiveBluetoothGattMessage {
                header,
                next_index: 1,
                payload: payload.to_vec(),
            });
        } else {
            let Some(active) = self.active.as_mut() else {
                return Err(BluetoothGattError::FragmentOutOfOrder);
            };
            if active.header.kind != header.kind
                || active.header.message_id != header.message_id
                || active.header.count != header.count
                || active.header.message_length != header.message_length
            {
                self.active = None;
                return Err(BluetoothGattError::FragmentMetadataMismatch);
            }
            if active.next_index != header.index {
                self.active = None;
                return Err(BluetoothGattError::FragmentOutOfOrder);
            }
            active.payload.extend_from_slice(payload);
            active.next_index = active
                .next_index
                .checked_add(1)
                .ok_or(BluetoothGattError::InvalidFragmentIndex)?;
        }
        if usize::from(header.index) + 1 != usize::from(header.count) {
            return Ok(None);
        }
        let Some(active) = self.active.take() else {
            return Err(BluetoothGattError::FragmentOutOfOrder);
        };
        if active.payload.len() != active.header.message_length {
            return Err(BluetoothGattError::InvalidMessageLength);
        }
        self.received_messages = self
            .received_messages
            .checked_add(1)
            .ok_or(BluetoothGattError::MessageLimitReached)?;
        self.next_message_id = self
            .next_message_id
            .checked_add(1)
            .ok_or(BluetoothGattError::MessageLimitReached)?;
        Ok(Some(BluetoothGattMessage {
            kind: active.header.kind,
            payload: active.payload,
        }))
    }
}

pub struct BluetoothGattLink {
    incoming: Mutex<mpsc::Receiver<BluetoothGattMessage>>,
    incoming_sender: mpsc::Sender<BluetoothGattMessage>,
    outgoing: mpsc::Sender<Vec<u8>>,
    packet_bytes: usize,
    reassembler: Mutex<BluetoothGattReassembler>,
    next_message_id: AtomicU16,
}

impl BluetoothGattLink {
    pub fn new(
        packet_bytes: usize,
        outgoing: mpsc::Sender<Vec<u8>>,
    ) -> Result<Arc<Self>, BluetoothGattError> {
        if !(MIN_BLUETOOTH_GATT_PACKET_BYTES..=MAX_BLUETOOTH_GATT_PACKET_BYTES)
            .contains(&packet_bytes)
        {
            return Err(BluetoothGattError::InvalidPacketSize);
        }
        let (incoming, receiver) = mpsc::channel(MAX_PENDING_BLUETOOTH_GATT_MESSAGES);
        Ok(Arc::new(Self {
            incoming: Mutex::new(receiver),
            incoming_sender: incoming,
            outgoing,
            packet_bytes,
            reassembler: Mutex::new(BluetoothGattReassembler::new(packet_bytes)),
            next_message_id: AtomicU16::new(0),
        }))
    }

    #[must_use]
    pub const fn packet_bytes(&self) -> usize {
        self.packet_bytes
    }

    pub async fn send(
        &self,
        kind: BluetoothGattMessageKind,
        payload: &[u8],
    ) -> Result<(), BluetoothGattError> {
        let message_id = self
            .next_message_id
            .fetch_update(
                AtomicOrdering::Relaxed,
                AtomicOrdering::Relaxed,
                |message_id| {
                    (message_id < MAX_BLUETOOTH_GATT_MESSAGES_PER_DIRECTION)
                        .then_some(message_id + 1)
                },
            )
            .map_err(|_| BluetoothGattError::MessageLimitReached)?;
        let packets = fragment_message(self.packet_bytes, message_id, kind, payload)?;
        for packet in packets {
            self.outgoing
                .send(packet)
                .await
                .map_err(|_| BluetoothGattError::Disconnected)?;
        }
        Ok(())
    }

    async fn receive(&self) -> Result<BluetoothGattMessage, BluetoothGattError> {
        self.incoming
            .lock()
            .await
            .recv()
            .await
            .ok_or(BluetoothGattError::Disconnected)
    }

    pub async fn ingest_packet(&self, packet: &[u8]) -> Result<(), BluetoothGattError> {
        let message = self.reassembler.lock().await.ingest(packet)?;
        if let Some(message) = message {
            self.incoming_sender
                .try_send(message)
                .map_err(|error| match error {
                    mpsc::error::TrySendError::Full(_) => BluetoothGattError::ReceiveQueueFull,
                    mpsc::error::TrySendError::Closed(_) => BluetoothGattError::Disconnected,
                })?;
        }
        Ok(())
    }

    pub async fn close(&self) {
        self.incoming.lock().await.close();
    }
}

fn fragment_message(
    packet_bytes: usize,
    message_id: u16,
    kind: BluetoothGattMessageKind,
    payload: &[u8],
) -> Result<Vec<Vec<u8>>, BluetoothGattError> {
    if payload.is_empty() || payload.len() > MAX_BLUETOOTH_GATT_MESSAGE_BYTES {
        return Err(BluetoothGattError::InvalidMessageLength);
    }
    let payload_capacity = packet_bytes
        .checked_sub(BLUETOOTH_GATT_PACKET_HEADER_BYTES)
        .ok_or(BluetoothGattError::InvalidPacketSize)?;
    let count = payload.len().div_ceil(payload_capacity);
    if count > MAX_BLUETOOTH_GATT_FRAGMENT_COUNT || count > usize::from(u16::MAX) {
        return Err(BluetoothGattError::InvalidFragmentCount);
    }
    let message_length =
        u32::try_from(payload.len()).map_err(|_| BluetoothGattError::InvalidMessageLength)?;
    let count = u16::try_from(count).map_err(|_| BluetoothGattError::InvalidFragmentCount)?;
    let mut packets = Vec::with_capacity(usize::from(count));
    for (index, chunk) in payload.chunks(payload_capacity).enumerate() {
        let index = u16::try_from(index).map_err(|_| BluetoothGattError::InvalidFragmentIndex)?;
        let mut packet = Vec::with_capacity(BLUETOOTH_GATT_PACKET_HEADER_BYTES + chunk.len());
        packet.push(BLUETOOTH_GATT_SCHEMA_VERSION);
        packet.push(kind as u8);
        packet.extend_from_slice(&message_id.to_le_bytes());
        packet.extend_from_slice(&index.to_le_bytes());
        packet.extend_from_slice(&count.to_le_bytes());
        packet.extend_from_slice(&message_length.to_le_bytes());
        packet.extend_from_slice(chunk);
        packets.push(packet);
    }
    Ok(packets)
}

pub struct AuthenticatedBluetoothGattTransport {
    link: Arc<BluetoothGattLink>,
    role: BluetoothGattRole,
}

impl AuthenticatedBluetoothGattTransport {
    pub async fn initiate(
        link: Arc<BluetoothGattLink>,
        local_identity: &IdentityKeypair,
        expected_peer: &IdentityPublicKey,
    ) -> Result<Self, BluetoothGattError> {
        let role = BluetoothGattRole::for_identities(&local_identity.public_key(), expected_peer)?;
        if role != BluetoothGattRole::Central {
            return Err(BluetoothGattError::RoleMismatch);
        }
        let offer = LocalLinkOffer::create(LocalMeshTransportKind::Bluetooth)
            .map_err(BluetoothGattError::Handshake)?;
        link.send(
            BluetoothGattMessageKind::Offer,
            &offer.encode().map_err(BluetoothGattError::Handshake)?,
        )
        .await?;
        let response = receive_response(&link).await?;
        let handshake = LocalLinkHandshake::from_response(&offer, &response)
            .map_err(BluetoothGattError::Handshake)?;
        handshake
            .verify_authentication(
                response.authentication(),
                expected_peer,
                LocalLinkRole::Responder,
            )
            .map_err(BluetoothGattError::Handshake)?;
        let authentication = handshake
            .authentication(local_identity, LocalLinkRole::Initiator)
            .map_err(BluetoothGattError::Handshake)?;
        link.send(
            BluetoothGattMessageKind::Authentication,
            &authentication
                .encode()
                .map_err(BluetoothGattError::Handshake)?,
        )
        .await?;
        Ok(Self { link, role })
    }

    pub async fn respond(
        link: Arc<BluetoothGattLink>,
        local_identity: &IdentityKeypair,
        expected_peer: &IdentityPublicKey,
    ) -> Result<Self, BluetoothGattError> {
        let role = BluetoothGattRole::for_identities(&local_identity.public_key(), expected_peer)?;
        if role != BluetoothGattRole::Peripheral {
            return Err(BluetoothGattError::RoleMismatch);
        }
        let offer = receive_offer(&link).await?;
        let (handshake, response) = LocalLinkHandshake::respond(local_identity, &offer)
            .map_err(BluetoothGattError::Handshake)?;
        link.send(
            BluetoothGattMessageKind::Response,
            &response.encode().map_err(BluetoothGattError::Handshake)?,
        )
        .await?;
        let authentication = receive_authentication(&link).await?;
        handshake
            .verify_authentication(&authentication, expected_peer, LocalLinkRole::Initiator)
            .map_err(BluetoothGattError::Handshake)?;
        Ok(Self { link, role })
    }

    #[must_use]
    pub const fn role(&self) -> BluetoothGattRole {
        self.role
    }

    #[must_use]
    pub fn link(&self) -> &Arc<BluetoothGattLink> {
        &self.link
    }
}

impl LocalTransport for AuthenticatedBluetoothGattTransport {
    type Error = BluetoothGattError;

    fn transport_kind(&self) -> LocalMeshTransportKind {
        LocalMeshTransportKind::Bluetooth
    }

    #[allow(clippy::manual_async_fn)]
    fn send_frame(
        &self,
        frame: &WireEnvelope,
        limits: WireLimits,
    ) -> impl Future<Output = Result<(), Self::Error>> + Send {
        async move {
            let encoded = frame.encode(limits).map_err(BluetoothGattError::Wire)?;
            self.link
                .send(BluetoothGattMessageKind::Envelope, &encoded)
                .await
        }
    }

    #[allow(clippy::manual_async_fn)]
    fn receive_frame(
        &self,
        limits: WireLimits,
    ) -> impl Future<Output = Result<WireEnvelope, Self::Error>> + Send {
        async move {
            let message = self.link.receive().await?;
            if message.kind != BluetoothGattMessageKind::Envelope {
                return Err(BluetoothGattError::UnexpectedMessage);
            }
            WireEnvelope::decode(&message.payload, limits).map_err(BluetoothGattError::Wire)
        }
    }
}

impl BluetoothTransport for AuthenticatedBluetoothGattTransport {}

async fn receive_offer(link: &BluetoothGattLink) -> Result<LocalLinkOffer, BluetoothGattError> {
    let message = link.receive().await?;
    if message.kind != BluetoothGattMessageKind::Offer {
        return Err(BluetoothGattError::UnexpectedMessage);
    }
    LocalLinkOffer::decode(&message.payload).map_err(BluetoothGattError::Handshake)
}

async fn receive_response(
    link: &BluetoothGattLink,
) -> Result<LocalLinkResponse, BluetoothGattError> {
    let message = link.receive().await?;
    if message.kind != BluetoothGattMessageKind::Response {
        return Err(BluetoothGattError::UnexpectedMessage);
    }
    LocalLinkResponse::decode(&message.payload).map_err(BluetoothGattError::Handshake)
}

async fn receive_authentication(
    link: &BluetoothGattLink,
) -> Result<LocalLinkAuthentication, BluetoothGattError> {
    let message = link.receive().await?;
    if message.kind != BluetoothGattMessageKind::Authentication {
        return Err(BluetoothGattError::UnexpectedMessage);
    }
    LocalLinkAuthentication::decode(&message.payload).map_err(BluetoothGattError::Handshake)
}

#[cfg(test)]
mod tests {
    use arachne_protocol::{EnvelopeKind, ProtocolVersion};
    use tokio::{sync::mpsc, task::JoinHandle};

    use super::{
        AuthenticatedBluetoothGattTransport, BLUETOOTH_GATT_PACKET_HEADER_BYTES,
        BluetoothGattError, BluetoothGattLink, BluetoothGattMessageKind, BluetoothGattRole,
        DEFAULT_BLUETOOTH_GATT_PACKET_BYTES, MAX_PENDING_BLUETOOTH_GATT_MESSAGES, fragment_message,
    };
    use crate::LocalTransport;
    use arachne_core::IdentityKeypair;
    use arachne_protocol::{WireEnvelope, WireLimits};

    #[test]
    fn deterministically_assigns_distinct_identities_to_opposite_roles() {
        let first = IdentityKeypair::generate().unwrap();
        let second = IdentityKeypair::generate().unwrap();
        let first_role =
            BluetoothGattRole::for_identities(&first.public_key(), &second.public_key()).unwrap();
        let second_role =
            BluetoothGattRole::for_identities(&second.public_key(), &first.public_key()).unwrap();
        assert_ne!(first_role, second_role);
        assert_eq!(
            BluetoothGattRole::for_identities(&first.public_key(), &first.public_key()),
            Err(BluetoothGattError::IdenticalPeerIdentity)
        );
    }

    #[tokio::test]
    async fn reassembles_only_ordered_bounded_gatt_fragments() {
        let (sender, _receiver) = mpsc::channel(MAX_PENDING_BLUETOOTH_GATT_MESSAGES);
        let link = BluetoothGattLink::new(DEFAULT_BLUETOOTH_GATT_PACKET_BYTES, sender).unwrap();
        let payload = vec![0x5a; BLUETOOTH_GATT_PACKET_HEADER_BYTES + 9];
        let packets = fragment_message(
            DEFAULT_BLUETOOTH_GATT_PACKET_BYTES,
            0,
            BluetoothGattMessageKind::Envelope,
            &payload,
        )
        .unwrap();
        assert!(packets.len() > 1);
        assert_eq!(
            link.ingest_packet(&packets[1]).await,
            Err(BluetoothGattError::FragmentOutOfOrder)
        );
        for packet in packets {
            link.ingest_packet(&packet).await.unwrap();
        }
        let message = link.receive().await.unwrap();
        assert_eq!(message.kind, BluetoothGattMessageKind::Envelope);
        assert_eq!(message.payload, payload);
    }

    #[tokio::test]
    async fn authenticates_then_exchanges_envelopes_over_fragmented_gatt() {
        let (central_outgoing, central_packets) =
            mpsc::channel(MAX_PENDING_BLUETOOTH_GATT_MESSAGES);
        let (peripheral_outgoing, peripheral_packets) =
            mpsc::channel(MAX_PENDING_BLUETOOTH_GATT_MESSAGES);
        let central_link =
            BluetoothGattLink::new(DEFAULT_BLUETOOTH_GATT_PACKET_BYTES, central_outgoing).unwrap();
        let peripheral_link =
            BluetoothGattLink::new(DEFAULT_BLUETOOTH_GATT_PACKET_BYTES, peripheral_outgoing)
                .unwrap();
        let central_pump = pump(central_packets, peripheral_link.clone());
        let peripheral_pump = pump(peripheral_packets, central_link.clone());
        let first = IdentityKeypair::generate().unwrap();
        let second = IdentityKeypair::generate().unwrap();
        let (central_identity, peripheral_identity) =
            if first.public_key().as_bytes() < second.public_key().as_bytes() {
                (&first, &second)
            } else {
                (&second, &first)
            };
        let central_public_key = central_identity.public_key();
        let peripheral_public_key = peripheral_identity.public_key();
        let (central, peripheral) = tokio::join!(
            AuthenticatedBluetoothGattTransport::initiate(
                central_link,
                central_identity,
                &peripheral_public_key,
            ),
            AuthenticatedBluetoothGattTransport::respond(
                peripheral_link,
                peripheral_identity,
                &central_public_key,
            ),
        );
        let central = central.unwrap();
        let peripheral = peripheral.unwrap();
        assert_eq!(central.role(), BluetoothGattRole::Central);
        assert_eq!(peripheral.role(), BluetoothGattRole::Peripheral);
        let frame = WireEnvelope {
            version: ProtocolVersion::INITIAL,
            kind: EnvelopeKind::EncryptedMessage,
            payload: vec![0x42; 128],
        };
        central
            .send_frame(&frame, WireLimits::REFERENCE)
            .await
            .unwrap();
        assert_eq!(
            peripheral
                .receive_frame(WireLimits::REFERENCE)
                .await
                .unwrap(),
            frame
        );
        central_pump.abort();
        peripheral_pump.abort();
    }

    fn pump(
        mut packets: mpsc::Receiver<Vec<u8>>,
        destination: std::sync::Arc<BluetoothGattLink>,
    ) -> JoinHandle<()> {
        tokio::spawn(async move {
            while let Some(packet) = packets.recv().await {
                if destination.ingest_packet(&packet).await.is_err() {
                    break;
                }
            }
        })
    }
}
