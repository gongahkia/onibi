use std::fmt;

use getrandom::{SysRng, rand_core::TryRng};
use minicbor::{Decoder, Encoder};
use sha2::{Digest, Sha256};
use subtle::ConstantTimeEq;
use yeokcham_core::{IdentityKeypair, IdentityPublicKey};

use crate::{CryptoDomain, DirectPeerProof, DirectPeerProofError, LocalMeshTransportKind};

pub const LOCAL_LINK_HANDSHAKE_SCHEMA_VERSION: u8 = 1;
pub const LOCAL_LINK_NONCE_BYTES: usize = 32;
pub const LOCAL_LINK_TRANSCRIPT_HASH_BYTES: usize = 32;
pub const LOCAL_LINK_OFFER_BYTES: usize = 37;
pub const LOCAL_LINK_AUTHENTICATION_BYTES: usize = 107;
pub const LOCAL_LINK_RESPONSE_BYTES: usize = 145;
const OFFER_FIELDS: u64 = 3;
const AUTHENTICATION_FIELDS: u64 = 3;
const RESPONSE_FIELDS: u64 = 3;
const TRANSCRIPT_FIELDS: u64 = 4;

#[derive(Clone, Copy, Eq, PartialEq)]
pub struct LocalLinkNonce([u8; LOCAL_LINK_NONCE_BYTES]);

impl LocalLinkNonce {
    pub fn generate() -> Result<Self, LocalLinkHandshakeError> {
        let mut bytes = [0; LOCAL_LINK_NONCE_BYTES];
        let mut random_source = SysRng;
        random_source
            .try_fill_bytes(&mut bytes)
            .map_err(|_| LocalLinkHandshakeError::Randomness)?;
        Self::from_bytes(bytes)
    }

    pub fn from_bytes(
        bytes: [u8; LOCAL_LINK_NONCE_BYTES],
    ) -> Result<Self, LocalLinkHandshakeError> {
        if bytes.iter().all(|byte| *byte == 0) {
            return Err(LocalLinkHandshakeError::InvalidNonce);
        }
        Ok(Self(bytes))
    }

    #[must_use]
    pub const fn as_bytes(&self) -> &[u8; LOCAL_LINK_NONCE_BYTES] {
        &self.0
    }
}

impl fmt::Debug for LocalLinkNonce {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("LocalLinkNonce(REDACTED)")
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum LocalLinkRole {
    Initiator = 1,
    Responder = 2,
}

impl TryFrom<u8> for LocalLinkRole {
    type Error = LocalLinkHandshakeError;

    fn try_from(value: u8) -> Result<Self, Self::Error> {
        match value {
            1 => Ok(Self::Initiator),
            2 => Ok(Self::Responder),
            _ => Err(LocalLinkHandshakeError::InvalidRole(value)),
        }
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct LocalLinkOffer {
    transport: LocalMeshTransportKind,
    nonce: LocalLinkNonce,
}

impl LocalLinkOffer {
    pub fn create(transport: LocalMeshTransportKind) -> Result<Self, LocalLinkHandshakeError> {
        Self::new(transport, LocalLinkNonce::generate()?)
    }

    pub fn new(
        transport: LocalMeshTransportKind,
        nonce: LocalLinkNonce,
    ) -> Result<Self, LocalLinkHandshakeError> {
        if !matches!(
            transport,
            LocalMeshTransportKind::WifiHotspot
                | LocalMeshTransportKind::WifiDirect
                | LocalMeshTransportKind::Bluetooth
        ) {
            return Err(LocalLinkHandshakeError::NonLocalLinkTransport(transport));
        }
        Ok(Self { transport, nonce })
    }

    #[must_use]
    pub const fn transport(&self) -> LocalMeshTransportKind {
        self.transport
    }

    #[must_use]
    pub const fn nonce(&self) -> &LocalLinkNonce {
        &self.nonce
    }

    pub fn encode(&self) -> Result<Vec<u8>, LocalLinkHandshakeError> {
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(OFFER_FIELDS)
            .map_err(|_| LocalLinkHandshakeError::Encode)?
            .u8(LOCAL_LINK_HANDSHAKE_SCHEMA_VERSION)
            .map_err(|_| LocalLinkHandshakeError::Encode)?
            .u8(self.transport as u8)
            .map_err(|_| LocalLinkHandshakeError::Encode)?
            .bytes(self.nonce.as_bytes())
            .map_err(|_| LocalLinkHandshakeError::Encode)?;
        let bytes = encoder.into_writer();
        if bytes.len() != LOCAL_LINK_OFFER_BYTES {
            return Err(LocalLinkHandshakeError::Encode);
        }
        Ok(bytes)
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, LocalLinkHandshakeError> {
        if encoded.len() != LOCAL_LINK_OFFER_BYTES {
            return Err(LocalLinkHandshakeError::InvalidLength);
        }
        let mut decoder = Decoder::new(encoded);
        if decoder
            .array()
            .map_err(|_| LocalLinkHandshakeError::Decode)?
            != Some(OFFER_FIELDS)
        {
            return Err(LocalLinkHandshakeError::InvalidOfferShape);
        }
        decode_schema_version(&mut decoder)?;
        let transport = decode_transport(&mut decoder)?;
        let nonce = decode_nonce(
            decoder
                .bytes()
                .map_err(|_| LocalLinkHandshakeError::Decode)?,
        )?;
        if decoder.position() != encoded.len() {
            return Err(LocalLinkHandshakeError::TrailingBytes);
        }
        let offer = Self::new(transport, nonce)?;
        if offer.encode()? != encoded {
            return Err(LocalLinkHandshakeError::NonCanonicalEncoding);
        }
        Ok(offer)
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct LocalLinkAuthentication {
    role: LocalLinkRole,
    proof: DirectPeerProof,
}

impl LocalLinkAuthentication {
    #[must_use]
    pub const fn role(&self) -> LocalLinkRole {
        self.role
    }

    #[must_use]
    pub const fn identity(&self) -> &IdentityPublicKey {
        self.proof.identity()
    }

    pub fn encode(&self) -> Result<Vec<u8>, LocalLinkHandshakeError> {
        let proof = self
            .proof
            .encode()
            .map_err(LocalLinkHandshakeError::Proof)?;
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(AUTHENTICATION_FIELDS)
            .map_err(|_| LocalLinkHandshakeError::Encode)?
            .u8(LOCAL_LINK_HANDSHAKE_SCHEMA_VERSION)
            .map_err(|_| LocalLinkHandshakeError::Encode)?
            .u8(self.role as u8)
            .map_err(|_| LocalLinkHandshakeError::Encode)?
            .bytes(&proof)
            .map_err(|_| LocalLinkHandshakeError::Encode)?;
        let bytes = encoder.into_writer();
        if bytes.len() != LOCAL_LINK_AUTHENTICATION_BYTES {
            return Err(LocalLinkHandshakeError::Encode);
        }
        Ok(bytes)
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, LocalLinkHandshakeError> {
        if encoded.len() != LOCAL_LINK_AUTHENTICATION_BYTES {
            return Err(LocalLinkHandshakeError::InvalidLength);
        }
        let mut decoder = Decoder::new(encoded);
        if decoder
            .array()
            .map_err(|_| LocalLinkHandshakeError::Decode)?
            != Some(AUTHENTICATION_FIELDS)
        {
            return Err(LocalLinkHandshakeError::InvalidAuthenticationShape);
        }
        decode_schema_version(&mut decoder)?;
        let role =
            LocalLinkRole::try_from(decoder.u8().map_err(|_| LocalLinkHandshakeError::Decode)?)?;
        let proof = DirectPeerProof::decode(
            decoder
                .bytes()
                .map_err(|_| LocalLinkHandshakeError::Decode)?,
        )
        .map_err(LocalLinkHandshakeError::Proof)?;
        if decoder.position() != encoded.len() {
            return Err(LocalLinkHandshakeError::TrailingBytes);
        }
        let authentication = Self { role, proof };
        if authentication.encode()? != encoded {
            return Err(LocalLinkHandshakeError::NonCanonicalEncoding);
        }
        Ok(authentication)
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct LocalLinkResponse {
    nonce: LocalLinkNonce,
    authentication: LocalLinkAuthentication,
}

impl LocalLinkResponse {
    #[must_use]
    pub const fn nonce(&self) -> &LocalLinkNonce {
        &self.nonce
    }

    #[must_use]
    pub const fn authentication(&self) -> &LocalLinkAuthentication {
        &self.authentication
    }

    pub fn encode(&self) -> Result<Vec<u8>, LocalLinkHandshakeError> {
        let authentication = self.authentication.encode()?;
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(RESPONSE_FIELDS)
            .map_err(|_| LocalLinkHandshakeError::Encode)?
            .u8(LOCAL_LINK_HANDSHAKE_SCHEMA_VERSION)
            .map_err(|_| LocalLinkHandshakeError::Encode)?
            .bytes(self.nonce.as_bytes())
            .map_err(|_| LocalLinkHandshakeError::Encode)?
            .bytes(&authentication)
            .map_err(|_| LocalLinkHandshakeError::Encode)?;
        let bytes = encoder.into_writer();
        if bytes.len() != LOCAL_LINK_RESPONSE_BYTES {
            return Err(LocalLinkHandshakeError::Encode);
        }
        Ok(bytes)
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, LocalLinkHandshakeError> {
        if encoded.len() != LOCAL_LINK_RESPONSE_BYTES {
            return Err(LocalLinkHandshakeError::InvalidLength);
        }
        let mut decoder = Decoder::new(encoded);
        if decoder
            .array()
            .map_err(|_| LocalLinkHandshakeError::Decode)?
            != Some(RESPONSE_FIELDS)
        {
            return Err(LocalLinkHandshakeError::InvalidResponseShape);
        }
        decode_schema_version(&mut decoder)?;
        let nonce = decode_nonce(
            decoder
                .bytes()
                .map_err(|_| LocalLinkHandshakeError::Decode)?,
        )?;
        let authentication = LocalLinkAuthentication::decode(
            decoder
                .bytes()
                .map_err(|_| LocalLinkHandshakeError::Decode)?,
        )?;
        if decoder.position() != encoded.len() {
            return Err(LocalLinkHandshakeError::TrailingBytes);
        }
        let response = Self {
            nonce,
            authentication,
        };
        if response.encode()? != encoded {
            return Err(LocalLinkHandshakeError::NonCanonicalEncoding);
        }
        Ok(response)
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct LocalLinkHandshake {
    transport: LocalMeshTransportKind,
    initiator_nonce: LocalLinkNonce,
    responder_nonce: LocalLinkNonce,
    transcript_hash: [u8; LOCAL_LINK_TRANSCRIPT_HASH_BYTES],
}

impl LocalLinkHandshake {
    pub fn respond(
        identity: &IdentityKeypair,
        offer: &LocalLinkOffer,
    ) -> Result<(Self, LocalLinkResponse), LocalLinkHandshakeError> {
        let responder_nonce = LocalLinkNonce::generate()?;
        let handshake = Self::new(offer, responder_nonce)?;
        let response = LocalLinkResponse {
            nonce: responder_nonce,
            authentication: handshake.authentication(identity, LocalLinkRole::Responder)?,
        };
        Ok((handshake, response))
    }

    pub fn from_response(
        offer: &LocalLinkOffer,
        response: &LocalLinkResponse,
    ) -> Result<Self, LocalLinkHandshakeError> {
        Self::new(offer, response.nonce)
    }

    pub fn authentication(
        &self,
        identity: &IdentityKeypair,
        role: LocalLinkRole,
    ) -> Result<LocalLinkAuthentication, LocalLinkHandshakeError> {
        Ok(LocalLinkAuthentication {
            role,
            proof: DirectPeerProof::create(identity, &self.proof_binding(role)?)
                .map_err(LocalLinkHandshakeError::Proof)?,
        })
    }

    pub fn verify_authentication(
        &self,
        authentication: &LocalLinkAuthentication,
        expected_identity: &IdentityPublicKey,
        expected_role: LocalLinkRole,
    ) -> Result<(), LocalLinkHandshakeError> {
        if authentication.role != expected_role {
            return Err(LocalLinkHandshakeError::UnexpectedRole {
                expected: expected_role,
                received: authentication.role,
            });
        }
        if authentication
            .identity()
            .as_bytes()
            .ct_eq(expected_identity.as_bytes())
            .unwrap_u8()
            != 1
        {
            return Err(LocalLinkHandshakeError::UnexpectedIdentity);
        }
        authentication
            .proof
            .verify(&self.proof_binding(expected_role)?)
            .map_err(LocalLinkHandshakeError::Proof)
    }

    #[must_use]
    pub const fn transport(&self) -> LocalMeshTransportKind {
        self.transport
    }

    #[must_use]
    pub const fn initiator_nonce(&self) -> &LocalLinkNonce {
        &self.initiator_nonce
    }

    #[must_use]
    pub const fn responder_nonce(&self) -> &LocalLinkNonce {
        &self.responder_nonce
    }

    #[must_use]
    pub const fn transcript_hash(&self) -> &[u8; LOCAL_LINK_TRANSCRIPT_HASH_BYTES] {
        &self.transcript_hash
    }

    fn new(
        offer: &LocalLinkOffer,
        responder_nonce: LocalLinkNonce,
    ) -> Result<Self, LocalLinkHandshakeError> {
        let transcript_hash = transcript_hash(offer.transport, &offer.nonce, &responder_nonce)?;
        Ok(Self {
            transport: offer.transport,
            initiator_nonce: offer.nonce,
            responder_nonce,
            transcript_hash,
        })
    }

    fn proof_binding(
        &self,
        role: LocalLinkRole,
    ) -> Result<[u8; LOCAL_LINK_TRANSCRIPT_HASH_BYTES], LocalLinkHandshakeError> {
        let mut hasher = Sha256::new();
        hasher.update(CryptoDomain::LocalLinkProofBinding.context());
        hasher.update([role as u8]);
        hasher.update(self.transcript_hash);
        hasher
            .finalize()
            .as_slice()
            .try_into()
            .map_err(|_| LocalLinkHandshakeError::Hash)
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum LocalLinkHandshakeError {
    #[error("operating-system random source failed")]
    Randomness,
    #[error("local-link nonce is invalid")]
    InvalidNonce,
    #[error("unsupported local-link handshake schema version: {0}")]
    UnsupportedSchemaVersion(u8),
    #[error("local-link offer must be a three-element definite-length CBOR array")]
    InvalidOfferShape,
    #[error("local-link authentication must be a three-element definite-length CBOR array")]
    InvalidAuthenticationShape,
    #[error("local-link response must be a three-element definite-length CBOR array")]
    InvalidResponseShape,
    #[error("local-link transport kind is invalid: {0}")]
    InvalidTransportKind(u8),
    #[error("local-link transport is unsupported: {0:?}")]
    NonLocalLinkTransport(LocalMeshTransportKind),
    #[error("local-link role is invalid: {0}")]
    InvalidRole(u8),
    #[error(
        "local-link authentication has unexpected role: expected {expected:?}, received {received:?}"
    )]
    UnexpectedRole {
        expected: LocalLinkRole,
        received: LocalLinkRole,
    },
    #[error("local-link authentication identity does not match the expected peer")]
    UnexpectedIdentity,
    #[error("local-link handshake has an invalid length")]
    InvalidLength,
    #[error("local-link handshake proof is invalid: {0}")]
    Proof(#[source] DirectPeerProofError),
    #[error("local-link handshake transcript hash failed")]
    Hash,
    #[error("local-link handshake encoding failed")]
    Encode,
    #[error("local-link handshake decoding failed")]
    Decode,
    #[error("trailing bytes after local-link handshake message")]
    TrailingBytes,
    #[error("local-link handshake message is not canonically encoded")]
    NonCanonicalEncoding,
}

fn decode_schema_version(decoder: &mut Decoder<'_>) -> Result<(), LocalLinkHandshakeError> {
    let version = decoder.u8().map_err(|_| LocalLinkHandshakeError::Decode)?;
    if version != LOCAL_LINK_HANDSHAKE_SCHEMA_VERSION {
        return Err(LocalLinkHandshakeError::UnsupportedSchemaVersion(version));
    }
    Ok(())
}

fn decode_transport(
    decoder: &mut Decoder<'_>,
) -> Result<LocalMeshTransportKind, LocalLinkHandshakeError> {
    let value = decoder.u8().map_err(|_| LocalLinkHandshakeError::Decode)?;
    LocalMeshTransportKind::try_from(value)
        .map_err(|_| LocalLinkHandshakeError::InvalidTransportKind(value))
}

fn decode_nonce(encoded: &[u8]) -> Result<LocalLinkNonce, LocalLinkHandshakeError> {
    let bytes: [u8; LOCAL_LINK_NONCE_BYTES] = encoded
        .try_into()
        .map_err(|_| LocalLinkHandshakeError::InvalidNonce)?;
    LocalLinkNonce::from_bytes(bytes)
}

fn transcript_hash(
    transport: LocalMeshTransportKind,
    initiator_nonce: &LocalLinkNonce,
    responder_nonce: &LocalLinkNonce,
) -> Result<[u8; LOCAL_LINK_TRANSCRIPT_HASH_BYTES], LocalLinkHandshakeError> {
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(TRANSCRIPT_FIELDS)
        .map_err(|_| LocalLinkHandshakeError::Encode)?
        .u8(LOCAL_LINK_HANDSHAKE_SCHEMA_VERSION)
        .map_err(|_| LocalLinkHandshakeError::Encode)?
        .u8(transport as u8)
        .map_err(|_| LocalLinkHandshakeError::Encode)?
        .bytes(initiator_nonce.as_bytes())
        .map_err(|_| LocalLinkHandshakeError::Encode)?
        .bytes(responder_nonce.as_bytes())
        .map_err(|_| LocalLinkHandshakeError::Encode)?;
    let mut hasher = Sha256::new();
    hasher.update(CryptoDomain::LocalLinkTranscript.context());
    hasher.update(encoder.into_writer());
    hasher
        .finalize()
        .as_slice()
        .try_into()
        .map_err(|_| LocalLinkHandshakeError::Hash)
}

#[cfg(test)]
mod tests {
    use super::{
        LOCAL_LINK_AUTHENTICATION_BYTES, LOCAL_LINK_HANDSHAKE_SCHEMA_VERSION,
        LOCAL_LINK_OFFER_BYTES, LOCAL_LINK_RESPONSE_BYTES, LocalLinkHandshake,
        LocalLinkHandshakeError, LocalLinkNonce, LocalLinkOffer, LocalLinkRole,
    };
    use crate::LocalMeshTransportKind;
    use yeokcham_core::IdentityKeypair;

    fn nonce(byte: u8) -> LocalLinkNonce {
        LocalLinkNonce::from_bytes([byte; 32]).unwrap()
    }

    #[test]
    fn mutually_authenticates_a_domain_separated_link_transcript() {
        let initiator = IdentityKeypair::generate().unwrap();
        let responder = IdentityKeypair::generate().unwrap();
        let offer = LocalLinkOffer::new(LocalMeshTransportKind::Bluetooth, nonce(1)).unwrap();
        let (responder_handshake, response) =
            LocalLinkHandshake::respond(&responder, &offer).unwrap();
        let initiator_handshake = LocalLinkHandshake::from_response(&offer, &response).unwrap();
        let confirmation = initiator_handshake
            .authentication(&initiator, LocalLinkRole::Initiator)
            .unwrap();

        assert_eq!(
            initiator_handshake.transport(),
            LocalMeshTransportKind::Bluetooth
        );
        assert_eq!(
            initiator_handshake.transcript_hash(),
            responder_handshake.transcript_hash()
        );
        assert_eq!(
            initiator_handshake.verify_authentication(
                response.authentication(),
                &responder.public_key(),
                LocalLinkRole::Responder,
            ),
            Ok(())
        );
        assert_eq!(
            responder_handshake.verify_authentication(
                &confirmation,
                &initiator.public_key(),
                LocalLinkRole::Initiator,
            ),
            Ok(())
        );
    }

    #[test]
    fn rejects_role_identity_and_transcript_confusion() {
        let initiator = IdentityKeypair::generate().unwrap();
        let responder = IdentityKeypair::generate().unwrap();
        let other = IdentityKeypair::generate().unwrap();
        let offer = LocalLinkOffer::new(LocalMeshTransportKind::WifiDirect, nonce(2)).unwrap();
        let (responder_handshake, response) =
            LocalLinkHandshake::respond(&responder, &offer).unwrap();
        let initiator_handshake = LocalLinkHandshake::from_response(&offer, &response).unwrap();

        assert_eq!(
            initiator_handshake.verify_authentication(
                response.authentication(),
                &other.public_key(),
                LocalLinkRole::Responder,
            ),
            Err(LocalLinkHandshakeError::UnexpectedIdentity)
        );
        assert_eq!(
            initiator_handshake.verify_authentication(
                response.authentication(),
                &responder.public_key(),
                LocalLinkRole::Initiator,
            ),
            Err(LocalLinkHandshakeError::UnexpectedRole {
                expected: LocalLinkRole::Initiator,
                received: LocalLinkRole::Responder,
            })
        );
        let changed_offer =
            LocalLinkOffer::new(LocalMeshTransportKind::WifiDirect, nonce(3)).unwrap();
        let changed_handshake =
            LocalLinkHandshake::from_response(&changed_offer, &response).unwrap();
        assert!(matches!(
            changed_handshake.verify_authentication(
                response.authentication(),
                &responder.public_key(),
                LocalLinkRole::Responder,
            ),
            Err(LocalLinkHandshakeError::Proof(_))
        ));
        assert_ne!(
            responder_handshake.transcript_hash(),
            changed_handshake.transcript_hash()
        );
        assert_ne!(initiator.public_key(), responder.public_key());
    }

    #[test]
    fn encodes_fixed_size_canonical_messages() {
        let identity = IdentityKeypair::generate().unwrap();
        let offer = LocalLinkOffer::new(LocalMeshTransportKind::WifiHotspot, nonce(4)).unwrap();
        let (handshake, response) = LocalLinkHandshake::respond(&identity, &offer).unwrap();
        let authentication = handshake
            .authentication(&identity, LocalLinkRole::Initiator)
            .unwrap();
        let encoded_offer = offer.encode().unwrap();
        let encoded_response = response.encode().unwrap();
        let encoded_authentication = authentication.encode().unwrap();

        assert_eq!(encoded_offer.len(), LOCAL_LINK_OFFER_BYTES);
        assert_eq!(encoded_response.len(), LOCAL_LINK_RESPONSE_BYTES);
        assert_eq!(
            encoded_authentication.len(),
            LOCAL_LINK_AUTHENTICATION_BYTES
        );
        assert_eq!(encoded_offer[0], 0x83);
        assert_eq!(encoded_offer[1], LOCAL_LINK_HANDSHAKE_SCHEMA_VERSION);
        assert_eq!(LocalLinkOffer::decode(&encoded_offer).unwrap(), offer);
        assert_eq!(
            super::LocalLinkResponse::decode(&encoded_response).unwrap(),
            response
        );
        assert_eq!(
            super::LocalLinkAuthentication::decode(&encoded_authentication).unwrap(),
            authentication
        );
    }

    #[test]
    fn rejects_invalid_nonce_version_and_shapes() {
        assert_eq!(
            LocalLinkNonce::from_bytes([0; 32]).unwrap_err(),
            LocalLinkHandshakeError::InvalidNonce
        );
        let offer = LocalLinkOffer::new(LocalMeshTransportKind::WifiDirect, nonce(5)).unwrap();
        let mut invalid_nonce = offer.encode().unwrap();
        invalid_nonce[5..].fill(0);
        let mut unsupported_version = offer.encode().unwrap();
        unsupported_version[1] = LOCAL_LINK_HANDSHAKE_SCHEMA_VERSION + 1;
        let mut invalid_shape = offer.encode().unwrap();
        invalid_shape[0] = 0x82;

        assert_eq!(
            LocalLinkOffer::decode(&invalid_nonce).unwrap_err(),
            LocalLinkHandshakeError::InvalidNonce
        );
        assert_eq!(
            LocalLinkOffer::decode(&unsupported_version).unwrap_err(),
            LocalLinkHandshakeError::UnsupportedSchemaVersion(2)
        );
        assert_eq!(
            LocalLinkOffer::decode(&invalid_shape).unwrap_err(),
            LocalLinkHandshakeError::InvalidOfferShape
        );
        assert_eq!(
            LocalLinkOffer::new(LocalMeshTransportKind::Lan, nonce(6)).unwrap_err(),
            LocalLinkHandshakeError::NonLocalLinkTransport(LocalMeshTransportKind::Lan)
        );
    }
}
