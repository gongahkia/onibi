use std::fmt;

use getrandom::{SysRng, rand_core::TryRng};
use minicbor::{Decoder, Encoder};
use yeokcham_core::{
    ED25519_PUBLIC_KEY_BYTES, ED25519_SIGNATURE_BYTES, IdentityPublicKey, RelayPublicKey,
    RelaySigningKeypair,
};

use crate::{CryptoDomain, MailboxCapability, TorMaildropProfileConfig};

pub const RELAY_INVITATION_SCHEMA_VERSION: u8 = 1;
pub const RELAY_INVITATION_GRANT_ID_BYTES: usize = 16;
pub const MAX_RELAY_INVITATION_TTL_SECONDS: u32 = 30 * 24 * 60 * 60;
pub const MAX_RELAY_INVITATION_BYTES: usize = 512;
const RELAY_INVITATION_FIELDS: u64 = 9;
const RELAY_INVITATION_UNSIGNED_FIELDS: u64 = 8;
const SIGNING_INPUT_FIELDS: u64 = 2;

pub struct RelayInvitation {
    relay: RelayPublicKey,
    recipient: IdentityPublicKey,
    endpoint: TorMaildropProfileConfig,
    mailbox_capability: MailboxCapability,
    grant_id: [u8; RELAY_INVITATION_GRANT_ID_BYTES],
    issued_at: u64,
    ttl_seconds: u32,
    signature: [u8; ED25519_SIGNATURE_BYTES],
}

impl RelayInvitation {
    pub fn create(
        relay: &RelaySigningKeypair,
        recipient: IdentityPublicKey,
        endpoint: TorMaildropProfileConfig,
        mailbox_capability: MailboxCapability,
        issued_at: u64,
        ttl_seconds: u32,
    ) -> Result<Self, RelayInvitationError> {
        validate_time(issued_at, ttl_seconds)?;
        let mut grant_id = [0; RELAY_INVITATION_GRANT_ID_BYTES];
        fill_random(&mut grant_id)?;
        if grant_id.iter().all(|byte| *byte == 0) {
            return Err(RelayInvitationError::Randomness);
        }
        let relay_public = relay.public_key();
        let signing_input = signing_input(
            &relay_public,
            &recipient,
            endpoint,
            &mailbox_capability,
            &grant_id,
            issued_at,
            ttl_seconds,
        )?;
        Ok(Self {
            relay: relay_public,
            recipient,
            endpoint,
            mailbox_capability,
            grant_id,
            issued_at,
            ttl_seconds,
            signature: relay.sign(&signing_input),
        })
    }

    #[must_use]
    pub const fn relay(&self) -> &RelayPublicKey {
        &self.relay
    }

    #[must_use]
    pub const fn recipient(&self) -> &IdentityPublicKey {
        &self.recipient
    }

    #[must_use]
    pub const fn endpoint(&self) -> TorMaildropProfileConfig {
        self.endpoint
    }

    #[must_use]
    pub const fn mailbox_capability(&self) -> &MailboxCapability {
        &self.mailbox_capability
    }

    #[must_use]
    pub const fn grant_id(&self) -> &[u8; RELAY_INVITATION_GRANT_ID_BYTES] {
        &self.grant_id
    }

    #[must_use]
    pub const fn issued_at(&self) -> u64 {
        self.issued_at
    }

    #[must_use]
    pub const fn ttl_seconds(&self) -> u32 {
        self.ttl_seconds
    }

    #[must_use]
    pub const fn signature(&self) -> &[u8; ED25519_SIGNATURE_BYTES] {
        &self.signature
    }

    pub fn validate(
        &self,
        expected_recipient: &IdentityPublicKey,
        now_unix_seconds: u64,
    ) -> Result<(), RelayInvitationError> {
        self.validate_signature()?;
        if self.recipient != *expected_recipient {
            return Err(RelayInvitationError::RecipientMismatch);
        }
        validate_time(self.issued_at, self.ttl_seconds)?;
        if now_unix_seconds < self.issued_at {
            return Err(RelayInvitationError::NotYetValid);
        }
        let expires_at = self.issued_at + u64::from(self.ttl_seconds);
        if now_unix_seconds > expires_at {
            return Err(RelayInvitationError::Expired);
        }
        Ok(())
    }

    pub fn encode(&self) -> Result<Vec<u8>, RelayInvitationError> {
        validate_time(self.issued_at, self.ttl_seconds)?;
        let endpoint = self
            .endpoint
            .encode()
            .map_err(|_| RelayInvitationError::InvalidEndpoint)?;
        let mailbox_capability = self
            .mailbox_capability
            .encode()
            .map_err(|_| RelayInvitationError::InvalidMailboxCapability)?;
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(RELAY_INVITATION_FIELDS)
            .map_err(|_| RelayInvitationError::Encode)?
            .u8(RELAY_INVITATION_SCHEMA_VERSION)
            .map_err(|_| RelayInvitationError::Encode)?
            .bytes(self.relay.as_bytes())
            .map_err(|_| RelayInvitationError::Encode)?
            .bytes(self.recipient.as_bytes())
            .map_err(|_| RelayInvitationError::Encode)?
            .bytes(&endpoint)
            .map_err(|_| RelayInvitationError::Encode)?
            .bytes(&mailbox_capability)
            .map_err(|_| RelayInvitationError::Encode)?
            .bytes(&self.grant_id)
            .map_err(|_| RelayInvitationError::Encode)?
            .u64(self.issued_at)
            .map_err(|_| RelayInvitationError::Encode)?
            .u32(self.ttl_seconds)
            .map_err(|_| RelayInvitationError::Encode)?
            .bytes(&self.signature)
            .map_err(|_| RelayInvitationError::Encode)?;
        let output = encoder.into_writer();
        if output.len() > MAX_RELAY_INVITATION_BYTES {
            return Err(RelayInvitationError::PayloadTooLarge);
        }
        Ok(output)
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, RelayInvitationError> {
        if encoded.len() > MAX_RELAY_INVITATION_BYTES {
            return Err(RelayInvitationError::PayloadTooLarge);
        }
        let mut decoder = Decoder::new(encoded);
        if decoder.array().map_err(|_| RelayInvitationError::Decode)?
            != Some(RELAY_INVITATION_FIELDS)
        {
            return Err(RelayInvitationError::InvalidShape);
        }
        let version = decoder.u8().map_err(|_| RelayInvitationError::Decode)?;
        if version != RELAY_INVITATION_SCHEMA_VERSION {
            return Err(RelayInvitationError::UnsupportedSchemaVersion(version));
        }
        let relay = decode_relay(decoder.bytes().map_err(|_| RelayInvitationError::Decode)?)?;
        let recipient =
            decode_recipient(decoder.bytes().map_err(|_| RelayInvitationError::Decode)?)?;
        let endpoint = TorMaildropProfileConfig::decode(
            decoder.bytes().map_err(|_| RelayInvitationError::Decode)?,
        )
        .map_err(|_| RelayInvitationError::InvalidEndpoint)?;
        let mailbox_capability =
            MailboxCapability::decode(decoder.bytes().map_err(|_| RelayInvitationError::Decode)?)
                .map_err(|_| RelayInvitationError::InvalidMailboxCapability)?;
        let grant_id = decode_grant_id(decoder.bytes().map_err(|_| RelayInvitationError::Decode)?)?;
        let issued_at = decoder.u64().map_err(|_| RelayInvitationError::Decode)?;
        let ttl_seconds = decoder.u32().map_err(|_| RelayInvitationError::Decode)?;
        validate_time(issued_at, ttl_seconds)?;
        let signature =
            decode_signature(decoder.bytes().map_err(|_| RelayInvitationError::Decode)?)?;
        if decoder.position() != encoded.len() {
            return Err(RelayInvitationError::TrailingBytes);
        }
        let invitation = Self {
            relay,
            recipient,
            endpoint,
            mailbox_capability,
            grant_id,
            issued_at,
            ttl_seconds,
            signature,
        };
        invitation.validate_signature()?;
        if invitation.encode()? != encoded {
            return Err(RelayInvitationError::NonCanonicalEncoding);
        }
        Ok(invitation)
    }

    fn validate_signature(&self) -> Result<(), RelayInvitationError> {
        let input = signing_input(
            &self.relay,
            &self.recipient,
            self.endpoint,
            &self.mailbox_capability,
            &self.grant_id,
            self.issued_at,
            self.ttl_seconds,
        )?;
        self.relay
            .verify(&input, &self.signature)
            .map_err(|_| RelayInvitationError::InvalidSignature)
    }
}

impl fmt::Debug for RelayInvitation {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter
            .debug_struct("RelayInvitation")
            .field("relay", &self.relay)
            .field("recipient", &self.recipient)
            .field("endpoint", &self.endpoint)
            .field("mailbox_capability", &"REDACTED")
            .field("grant_id", &self.grant_id)
            .field("issued_at", &self.issued_at)
            .field("ttl_seconds", &self.ttl_seconds)
            .field("signature", &self.signature)
            .finish()
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum RelayInvitationError {
    #[error("operating-system random source failed")]
    Randomness,
    #[error("relay invitation exceeds the configured limit")]
    PayloadTooLarge,
    #[error("unsupported relay-invitation schema version: {0}")]
    UnsupportedSchemaVersion(u8),
    #[error("relay invitation must be a nine-element definite-length CBOR array")]
    InvalidShape,
    #[error("relay invitation contains an invalid relay identity")]
    InvalidRelay,
    #[error("relay invitation contains an invalid recipient identity")]
    InvalidRecipient,
    #[error("relay invitation contains an invalid Tor endpoint")]
    InvalidEndpoint,
    #[error("relay invitation contains an invalid mailbox capability")]
    InvalidMailboxCapability,
    #[error("relay invitation grant ID is invalid")]
    InvalidGrantId,
    #[error("relay invitation TTL is invalid")]
    InvalidTtl,
    #[error("relay invitation timestamp is invalid")]
    InvalidTime,
    #[error("relay invitation signature is invalid")]
    InvalidSignature,
    #[error("relay invitation recipient does not match")]
    RecipientMismatch,
    #[error("relay invitation is not yet valid")]
    NotYetValid,
    #[error("relay invitation has expired")]
    Expired,
    #[error("CBOR decoding failed")]
    Decode,
    #[error("CBOR encoding failed")]
    Encode,
    #[error("trailing bytes after relay invitation")]
    TrailingBytes,
    #[error("relay invitation is not canonically encoded")]
    NonCanonicalEncoding,
}

fn signing_input(
    relay: &RelayPublicKey,
    recipient: &IdentityPublicKey,
    endpoint: TorMaildropProfileConfig,
    mailbox_capability: &MailboxCapability,
    grant_id: &[u8; RELAY_INVITATION_GRANT_ID_BYTES],
    issued_at: u64,
    ttl_seconds: u32,
) -> Result<Vec<u8>, RelayInvitationError> {
    let unsigned = encode_unsigned(
        relay,
        recipient,
        endpoint,
        mailbox_capability,
        grant_id,
        issued_at,
        ttl_seconds,
    )?;
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(SIGNING_INPUT_FIELDS)
        .map_err(|_| RelayInvitationError::Encode)?
        .bytes(CryptoDomain::RelayInvitationSignature.context())
        .map_err(|_| RelayInvitationError::Encode)?
        .bytes(&unsigned)
        .map_err(|_| RelayInvitationError::Encode)?;
    Ok(encoder.into_writer())
}

fn encode_unsigned(
    relay: &RelayPublicKey,
    recipient: &IdentityPublicKey,
    endpoint: TorMaildropProfileConfig,
    mailbox_capability: &MailboxCapability,
    grant_id: &[u8; RELAY_INVITATION_GRANT_ID_BYTES],
    issued_at: u64,
    ttl_seconds: u32,
) -> Result<Vec<u8>, RelayInvitationError> {
    validate_time(issued_at, ttl_seconds)?;
    let endpoint = endpoint
        .encode()
        .map_err(|_| RelayInvitationError::InvalidEndpoint)?;
    let mailbox_capability = mailbox_capability
        .encode()
        .map_err(|_| RelayInvitationError::InvalidMailboxCapability)?;
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(RELAY_INVITATION_UNSIGNED_FIELDS)
        .map_err(|_| RelayInvitationError::Encode)?
        .u8(RELAY_INVITATION_SCHEMA_VERSION)
        .map_err(|_| RelayInvitationError::Encode)?
        .bytes(relay.as_bytes())
        .map_err(|_| RelayInvitationError::Encode)?
        .bytes(recipient.as_bytes())
        .map_err(|_| RelayInvitationError::Encode)?
        .bytes(&endpoint)
        .map_err(|_| RelayInvitationError::Encode)?
        .bytes(&mailbox_capability)
        .map_err(|_| RelayInvitationError::Encode)?
        .bytes(grant_id)
        .map_err(|_| RelayInvitationError::Encode)?
        .u64(issued_at)
        .map_err(|_| RelayInvitationError::Encode)?
        .u32(ttl_seconds)
        .map_err(|_| RelayInvitationError::Encode)?;
    Ok(encoder.into_writer())
}

fn decode_relay(encoded: &[u8]) -> Result<RelayPublicKey, RelayInvitationError> {
    let bytes: [u8; ED25519_PUBLIC_KEY_BYTES] = encoded
        .try_into()
        .map_err(|_| RelayInvitationError::InvalidRelay)?;
    RelayPublicKey::from_bytes(bytes).map_err(|_| RelayInvitationError::InvalidRelay)
}

fn decode_recipient(encoded: &[u8]) -> Result<IdentityPublicKey, RelayInvitationError> {
    let bytes: [u8; ED25519_PUBLIC_KEY_BYTES] = encoded
        .try_into()
        .map_err(|_| RelayInvitationError::InvalidRecipient)?;
    IdentityPublicKey::from_bytes(bytes).map_err(|_| RelayInvitationError::InvalidRecipient)
}

fn decode_grant_id(
    encoded: &[u8],
) -> Result<[u8; RELAY_INVITATION_GRANT_ID_BYTES], RelayInvitationError> {
    let grant_id: [u8; RELAY_INVITATION_GRANT_ID_BYTES] = encoded
        .try_into()
        .map_err(|_| RelayInvitationError::InvalidGrantId)?;
    if grant_id.iter().all(|byte| *byte == 0) {
        return Err(RelayInvitationError::InvalidGrantId);
    }
    Ok(grant_id)
}

fn decode_signature(encoded: &[u8]) -> Result<[u8; ED25519_SIGNATURE_BYTES], RelayInvitationError> {
    encoded
        .try_into()
        .map_err(|_| RelayInvitationError::InvalidSignature)
}

fn validate_ttl(ttl_seconds: u32) -> Result<(), RelayInvitationError> {
    if ttl_seconds == 0 || ttl_seconds > MAX_RELAY_INVITATION_TTL_SECONDS {
        return Err(RelayInvitationError::InvalidTtl);
    }
    Ok(())
}

fn validate_time(issued_at: u64, ttl_seconds: u32) -> Result<(), RelayInvitationError> {
    validate_ttl(ttl_seconds)?;
    issued_at
        .checked_add(u64::from(ttl_seconds))
        .ok_or(RelayInvitationError::InvalidTime)?;
    Ok(())
}

fn fill_random(bytes: &mut [u8]) -> Result<(), RelayInvitationError> {
    let mut random_source = SysRng;
    random_source
        .try_fill_bytes(bytes)
        .map_err(|_| RelayInvitationError::Randomness)
}

#[cfg(test)]
mod tests {
    use yeokcham_core::{IdentityKeypair, RelaySigningKeypair};

    use super::{MAX_RELAY_INVITATION_TTL_SECONDS, RelayInvitation, RelayInvitationError};
    use crate::{
        MAILBOX_CAPABILITY_TOKEN_BYTES, MAILBOX_IDENTIFIER_BYTES, MailboxCapability,
        TOR_ONION_SERVICE_PUBLIC_KEY_BYTES, TorMaildropProfileConfig,
    };

    fn mailbox_capability() -> MailboxCapability {
        MailboxCapability::new(
            [0x22; MAILBOX_IDENTIFIER_BYTES],
            [0x33; MAILBOX_CAPABILITY_TOKEN_BYTES],
        )
        .unwrap()
    }

    fn endpoint() -> TorMaildropProfileConfig {
        TorMaildropProfileConfig::new([0x11; TOR_ONION_SERVICE_PUBLIC_KEY_BYTES], 4444).unwrap()
    }

    #[test]
    fn creates_parses_and_validates_recipient_bound_relay_grants() {
        let relay = RelaySigningKeypair::generate().unwrap();
        let recipient = IdentityKeypair::generate().unwrap();
        let invitation = RelayInvitation::create(
            &relay,
            recipient.public_key(),
            endpoint(),
            mailbox_capability(),
            1_700_000_000,
            3_600,
        )
        .unwrap();
        let encoded = invitation.encode().unwrap();
        let decoded = RelayInvitation::decode(&encoded).unwrap();

        assert_eq!(decoded.relay(), &relay.public_key());
        assert_eq!(decoded.recipient(), &recipient.public_key());
        assert_eq!(decoded.endpoint(), endpoint());
        assert_ne!(decoded.grant_id(), &[0; 16]);
        assert_eq!(decoded.encode().unwrap(), encoded);
        assert!(
            decoded
                .validate(&recipient.public_key(), 1_700_003_600)
                .is_ok()
        );
        assert!(format!("{decoded:?}").contains("REDACTED"));
    }

    #[test]
    fn rejects_invalid_timing_recipient_signature_and_encodings() {
        let relay = RelaySigningKeypair::generate().unwrap();
        let recipient = IdentityKeypair::generate().unwrap();
        let other = IdentityKeypair::generate().unwrap();
        assert_eq!(
            RelayInvitation::create(
                &relay,
                recipient.public_key(),
                endpoint(),
                mailbox_capability(),
                1,
                MAX_RELAY_INVITATION_TTL_SECONDS + 1,
            )
            .unwrap_err(),
            RelayInvitationError::InvalidTtl
        );
        let invitation = RelayInvitation::create(
            &relay,
            recipient.public_key(),
            endpoint(),
            mailbox_capability(),
            100,
            10,
        )
        .unwrap();
        assert_eq!(
            invitation.validate(&other.public_key(), 100).unwrap_err(),
            RelayInvitationError::RecipientMismatch
        );
        assert_eq!(
            invitation
                .validate(&recipient.public_key(), 99)
                .unwrap_err(),
            RelayInvitationError::NotYetValid
        );
        assert_eq!(
            invitation
                .validate(&recipient.public_key(), 111)
                .unwrap_err(),
            RelayInvitationError::Expired
        );
        let mut tampered = invitation.encode().unwrap();
        let last = tampered.len() - 1;
        tampered[last] ^= 1;
        assert_eq!(
            RelayInvitation::decode(&tampered).unwrap_err(),
            RelayInvitationError::InvalidSignature
        );
        let mut trailing = invitation.encode().unwrap();
        trailing.push(0);
        assert_eq!(
            RelayInvitation::decode(&trailing).unwrap_err(),
            RelayInvitationError::TrailingBytes
        );
    }
}
