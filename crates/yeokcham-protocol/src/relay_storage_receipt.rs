use minicbor::{Decoder, Encoder};
use yeokcham_core::{ED25519_SIGNATURE_BYTES, RelayPublicKey, RelaySigningKeypair};

use crate::{CryptoDomain, MAILBOX_IDENTIFIER_BYTES};

pub const RELAY_STORAGE_RECEIPT_SCHEMA_VERSION: u8 = 1;
const RECEIPT_FIELDS: u64 = 6;
const SIGNING_INPUT_FIELDS: u64 = 2;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct RelayStorageReceipt {
    relay: RelayPublicKey,
    mailbox_id: [u8; MAILBOX_IDENTIFIER_BYTES],
    sequence: u64,
    received_at: u64,
    expires_at: u64,
    signature: [u8; ED25519_SIGNATURE_BYTES],
}

impl RelayStorageReceipt {
    pub fn issue(
        relay: &RelaySigningKeypair,
        mailbox_id: [u8; MAILBOX_IDENTIFIER_BYTES],
        sequence: u64,
        received_at: u64,
        expires_at: u64,
    ) -> Result<Self, RelayStorageReceiptError> {
        validate_time(received_at, expires_at)?;
        let relay_public_key = relay.public_key();
        let signature = relay.sign(&signing_input(
            &relay_public_key,
            &mailbox_id,
            sequence,
            received_at,
            expires_at,
        )?);
        Ok(Self {
            relay: relay_public_key,
            mailbox_id,
            sequence,
            received_at,
            expires_at,
            signature,
        })
    }

    #[must_use]
    pub const fn relay(&self) -> &RelayPublicKey {
        &self.relay
    }

    #[must_use]
    pub const fn mailbox_id(&self) -> &[u8; MAILBOX_IDENTIFIER_BYTES] {
        &self.mailbox_id
    }

    #[must_use]
    pub const fn sequence(&self) -> u64 {
        self.sequence
    }

    #[must_use]
    pub const fn received_at(&self) -> u64 {
        self.received_at
    }

    #[must_use]
    pub const fn expires_at(&self) -> u64 {
        self.expires_at
    }

    pub fn verify(&self) -> Result<(), RelayStorageReceiptError> {
        validate_time(self.received_at, self.expires_at)?;
        self.relay
            .verify(
                &signing_input(
                    &self.relay,
                    &self.mailbox_id,
                    self.sequence,
                    self.received_at,
                    self.expires_at,
                )?,
                &self.signature,
            )
            .map_err(|_| RelayStorageReceiptError::InvalidSignature)
    }

    pub fn verify_for(
        &self,
        expected_relay: &RelayPublicKey,
        expected_mailbox_id: &[u8; MAILBOX_IDENTIFIER_BYTES],
        now: u64,
    ) -> Result<(), RelayStorageReceiptError> {
        self.verify()?;
        if self.relay != *expected_relay {
            return Err(RelayStorageReceiptError::UnexpectedRelay);
        }
        if self.mailbox_id != *expected_mailbox_id {
            return Err(RelayStorageReceiptError::UnexpectedMailboxIdentifier);
        }
        if now >= self.expires_at {
            return Err(RelayStorageReceiptError::Expired);
        }
        Ok(())
    }

    pub fn encode(&self) -> Result<Vec<u8>, RelayStorageReceiptError> {
        self.verify()?;
        encode_receipt(
            &self.relay,
            &self.mailbox_id,
            self.sequence,
            self.received_at,
            self.expires_at,
            &self.signature,
        )
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, RelayStorageReceiptError> {
        let mut decoder = Decoder::new(encoded);
        if decoder
            .array()
            .map_err(|_| RelayStorageReceiptError::Decode)?
            != Some(RECEIPT_FIELDS)
        {
            return Err(RelayStorageReceiptError::InvalidShape);
        }
        let version = decoder.u8().map_err(|_| RelayStorageReceiptError::Decode)?;
        if version != RELAY_STORAGE_RECEIPT_SCHEMA_VERSION {
            return Err(RelayStorageReceiptError::UnsupportedSchemaVersion(version));
        }
        let relay = RelayPublicKey::from_bytes(
            decoder
                .bytes()
                .map_err(|_| RelayStorageReceiptError::Decode)?
                .try_into()
                .map_err(|_| RelayStorageReceiptError::InvalidRelay)?,
        )
        .map_err(|_| RelayStorageReceiptError::InvalidRelay)?;
        let mailbox_id = decoder
            .bytes()
            .map_err(|_| RelayStorageReceiptError::Decode)?
            .try_into()
            .map_err(|_| RelayStorageReceiptError::InvalidMailboxIdentifier)?;
        let sequence = decoder
            .u64()
            .map_err(|_| RelayStorageReceiptError::Decode)?;
        let received_at = decoder
            .u64()
            .map_err(|_| RelayStorageReceiptError::Decode)?;
        let expires_at = decoder
            .u64()
            .map_err(|_| RelayStorageReceiptError::Decode)?;
        let signature = decoder
            .bytes()
            .map_err(|_| RelayStorageReceiptError::Decode)?
            .try_into()
            .map_err(|_| RelayStorageReceiptError::InvalidSignature)?;
        if decoder.position() != encoded.len() {
            return Err(RelayStorageReceiptError::TrailingBytes);
        }
        let receipt = Self {
            relay,
            mailbox_id,
            sequence,
            received_at,
            expires_at,
            signature,
        };
        receipt.verify()?;
        if receipt.encode()? != encoded {
            return Err(RelayStorageReceiptError::NonCanonicalEncoding);
        }
        Ok(receipt)
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum RelayStorageReceiptError {
    #[error("unsupported relay-storage-receipt schema version: {0}")]
    UnsupportedSchemaVersion(u8),
    #[error("relay-storage receipt has an invalid relay public key")]
    InvalidRelay,
    #[error("relay-storage receipt has an invalid mailbox identifier")]
    InvalidMailboxIdentifier,
    #[error("relay-storage receipt expiry must be after storage time")]
    InvalidTime,
    #[error("relay-storage receipt signature is invalid")]
    InvalidSignature,
    #[error("relay-storage receipt was issued by an unexpected relay")]
    UnexpectedRelay,
    #[error("relay-storage receipt was issued for an unexpected mailbox")]
    UnexpectedMailboxIdentifier,
    #[error("relay-storage receipt has expired")]
    Expired,
    #[error("relay-storage receipt must be a six-element definite-length CBOR array")]
    InvalidShape,
    #[error("CBOR decoding failed")]
    Decode,
    #[error("CBOR encoding failed")]
    Encode,
    #[error("trailing bytes after relay-storage receipt")]
    TrailingBytes,
    #[error("relay-storage receipt is not canonically encoded")]
    NonCanonicalEncoding,
}

fn signing_input(
    relay: &RelayPublicKey,
    mailbox_id: &[u8; MAILBOX_IDENTIFIER_BYTES],
    sequence: u64,
    received_at: u64,
    expires_at: u64,
) -> Result<Vec<u8>, RelayStorageReceiptError> {
    let unsigned = encode_unsigned(relay, mailbox_id, sequence, received_at, expires_at)?;
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(SIGNING_INPUT_FIELDS)
        .map_err(|_| RelayStorageReceiptError::Encode)?
        .bytes(CryptoDomain::RelayStorageReceiptSignature.context())
        .map_err(|_| RelayStorageReceiptError::Encode)?
        .bytes(&unsigned)
        .map_err(|_| RelayStorageReceiptError::Encode)?;
    Ok(encoder.into_writer())
}

fn encode_unsigned(
    relay: &RelayPublicKey,
    mailbox_id: &[u8; MAILBOX_IDENTIFIER_BYTES],
    sequence: u64,
    received_at: u64,
    expires_at: u64,
) -> Result<Vec<u8>, RelayStorageReceiptError> {
    validate_time(received_at, expires_at)?;
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(5)
        .map_err(|_| RelayStorageReceiptError::Encode)?
        .u8(RELAY_STORAGE_RECEIPT_SCHEMA_VERSION)
        .map_err(|_| RelayStorageReceiptError::Encode)?
        .bytes(relay.as_bytes())
        .map_err(|_| RelayStorageReceiptError::Encode)?
        .bytes(mailbox_id)
        .map_err(|_| RelayStorageReceiptError::Encode)?
        .u64(sequence)
        .map_err(|_| RelayStorageReceiptError::Encode)?
        .u64(received_at)
        .map_err(|_| RelayStorageReceiptError::Encode)?
        .u64(expires_at)
        .map_err(|_| RelayStorageReceiptError::Encode)?;
    Ok(encoder.into_writer())
}

fn encode_receipt(
    relay: &RelayPublicKey,
    mailbox_id: &[u8; MAILBOX_IDENTIFIER_BYTES],
    sequence: u64,
    received_at: u64,
    expires_at: u64,
    signature: &[u8; ED25519_SIGNATURE_BYTES],
) -> Result<Vec<u8>, RelayStorageReceiptError> {
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(RECEIPT_FIELDS)
        .map_err(|_| RelayStorageReceiptError::Encode)?
        .u8(RELAY_STORAGE_RECEIPT_SCHEMA_VERSION)
        .map_err(|_| RelayStorageReceiptError::Encode)?
        .bytes(relay.as_bytes())
        .map_err(|_| RelayStorageReceiptError::Encode)?
        .bytes(mailbox_id)
        .map_err(|_| RelayStorageReceiptError::Encode)?
        .u64(sequence)
        .map_err(|_| RelayStorageReceiptError::Encode)?
        .u64(received_at)
        .map_err(|_| RelayStorageReceiptError::Encode)?
        .u64(expires_at)
        .map_err(|_| RelayStorageReceiptError::Encode)?
        .bytes(signature)
        .map_err(|_| RelayStorageReceiptError::Encode)?;
    Ok(encoder.into_writer())
}

fn validate_time(received_at: u64, expires_at: u64) -> Result<(), RelayStorageReceiptError> {
    if expires_at <= received_at {
        return Err(RelayStorageReceiptError::InvalidTime);
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::{RelayStorageReceipt, RelayStorageReceiptError};
    use yeokcham_core::RelaySigningKeypair;

    #[test]
    fn issues_verifies_and_canonically_round_trips_receipts() {
        let relay = RelaySigningKeypair::generate().unwrap();
        let receipt = RelayStorageReceipt::issue(&relay, [0x11; 16], 7, 100, 110).unwrap();
        let encoded = receipt.encode().unwrap();

        assert_eq!(RelayStorageReceipt::decode(&encoded).unwrap(), receipt);
        assert_eq!(receipt.mailbox_id(), &[0x11; 16]);
        assert_eq!(receipt.sequence(), 7);
    }

    #[test]
    fn rejects_invalid_times_and_tampered_receipts() {
        let relay = RelaySigningKeypair::generate().unwrap();
        assert_eq!(
            RelayStorageReceipt::issue(&relay, [0x11; 16], 7, 100, 100).unwrap_err(),
            RelayStorageReceiptError::InvalidTime
        );
        let receipt = RelayStorageReceipt::issue(&relay, [0x11; 16], 7, 100, 110).unwrap();
        let mut encoded = receipt.encode().unwrap();
        let last = encoded.len() - 1;
        encoded[last] ^= 1;
        assert_eq!(
            RelayStorageReceipt::decode(&encoded).unwrap_err(),
            RelayStorageReceiptError::InvalidSignature
        );
    }

    #[test]
    fn client_verification_pins_the_relay_mailbox_and_expiry() {
        let relay = RelaySigningKeypair::generate().unwrap();
        let other_relay = RelaySigningKeypair::generate().unwrap();
        let receipt = RelayStorageReceipt::issue(&relay, [0x11; 16], 7, 100, 110).unwrap();

        assert_eq!(
            receipt.verify_for(&relay.public_key(), &[0x11; 16], 109),
            Ok(())
        );
        assert_eq!(
            receipt
                .verify_for(&other_relay.public_key(), &[0x11; 16], 109)
                .unwrap_err(),
            RelayStorageReceiptError::UnexpectedRelay
        );
        assert_eq!(
            receipt
                .verify_for(&relay.public_key(), &[0x22; 16], 109)
                .unwrap_err(),
            RelayStorageReceiptError::UnexpectedMailboxIdentifier
        );
        assert_eq!(
            receipt
                .verify_for(&relay.public_key(), &[0x11; 16], 110)
                .unwrap_err(),
            RelayStorageReceiptError::Expired
        );
    }
}
