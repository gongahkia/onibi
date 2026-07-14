use minicbor::{Decoder, Encoder};
use yeokcham_core::Error;

use crate::ProtocolVersion;

pub const MAX_FRAME_BYTES: usize = 1_048_576;
const ENVELOPE_FIELDS: u64 = 3;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u8)]
pub enum EnvelopeKind {
    VersionNegotiation = 1,
    EncryptedMessage = 2,
    DeliveryAcknowledgement = 3,
}

impl TryFrom<u8> for EnvelopeKind {
    type Error = WireError;

    fn try_from(value: u8) -> Result<Self, Self::Error> {
        match value {
            1 => Ok(Self::VersionNegotiation),
            2 => Ok(Self::EncryptedMessage),
            3 => Ok(Self::DeliveryAcknowledgement),
            _ => Err(WireError::InvalidKind(value)),
        }
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct WireEnvelope {
    pub version: ProtocolVersion,
    pub kind: EnvelopeKind,
    pub payload: Vec<u8>,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct WireLimits {
    maximum_frame_bytes: usize,
    maximum_payload_bytes: usize,
}

impl WireLimits {
    pub const REFERENCE: Self = Self {
        maximum_frame_bytes: MAX_FRAME_BYTES,
        maximum_payload_bytes: MAX_FRAME_BYTES - 32,
    };

    pub fn new(maximum_frame_bytes: usize, maximum_payload_bytes: usize) -> Result<Self, Error> {
        if maximum_frame_bytes == 0 || maximum_frame_bytes > MAX_FRAME_BYTES {
            return Err(Error::ResourceLimit("invalid maximum frame size"));
        }
        if maximum_payload_bytes > maximum_frame_bytes {
            return Err(Error::ResourceLimit("payload limit exceeds frame limit"));
        }
        Ok(Self {
            maximum_frame_bytes,
            maximum_payload_bytes,
        })
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum WireError {
    #[error("frame exceeds configured limit")]
    FrameTooLarge,
    #[error("payload exceeds configured limit")]
    PayloadTooLarge,
    #[error("envelope must be a three-element definite-length CBOR array")]
    InvalidEnvelopeShape,
    #[error("invalid envelope kind: {0}")]
    InvalidKind(u8),
    #[error("unsupported protocol version: {0}")]
    UnsupportedVersion(u16),
    #[error("CBOR decoding failed")]
    Decode,
    #[error("CBOR encoding failed")]
    Encode,
    #[error("trailing bytes after envelope")]
    TrailingBytes,
}

impl WireEnvelope {
    pub fn encode(&self, limits: WireLimits) -> Result<Vec<u8>, WireError> {
        if self.payload.len() > limits.maximum_payload_bytes {
            return Err(WireError::PayloadTooLarge);
        }
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(ENVELOPE_FIELDS)
            .map_err(|_| WireError::Encode)?
            .u16(self.version.get())
            .map_err(|_| WireError::Encode)?
            .u8(self.kind as u8)
            .map_err(|_| WireError::Encode)?
            .bytes(&self.payload)
            .map_err(|_| WireError::Encode)?;
        let frame = encoder.into_writer();
        if frame.len() > limits.maximum_frame_bytes {
            return Err(WireError::FrameTooLarge);
        }
        Ok(frame)
    }

    pub fn decode(frame: &[u8], limits: WireLimits) -> Result<Self, WireError> {
        if frame.len() > limits.maximum_frame_bytes {
            return Err(WireError::FrameTooLarge);
        }
        let mut decoder = Decoder::new(frame);
        if decoder.array().map_err(|_| WireError::Decode)? != Some(ENVELOPE_FIELDS) {
            return Err(WireError::InvalidEnvelopeShape);
        }
        let version = ProtocolVersion::new(decoder.u16().map_err(|_| WireError::Decode)?).map_err(
            |error| match error {
                Error::UnsupportedVersion(version) => WireError::UnsupportedVersion(version),
                _ => WireError::Decode,
            },
        )?;
        let kind = EnvelopeKind::try_from(decoder.u8().map_err(|_| WireError::Decode)?)?;
        let payload = decoder.bytes().map_err(|_| WireError::Decode)?.to_vec();
        if payload.len() > limits.maximum_payload_bytes {
            return Err(WireError::PayloadTooLarge);
        }
        if decoder.position() != frame.len() {
            return Err(WireError::TrailingBytes);
        }
        Ok(Self {
            version,
            kind,
            payload,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::{EnvelopeKind, WireEnvelope, WireError, WireLimits};
    use crate::ProtocolVersion;

    fn envelope() -> WireEnvelope {
        WireEnvelope {
            version: ProtocolVersion::INITIAL,
            kind: EnvelopeKind::EncryptedMessage,
            payload: vec![1, 2, 3],
        }
    }

    #[test]
    fn canonical_envelope_round_trip() {
        let encoded = envelope().encode(WireLimits::REFERENCE).unwrap();
        assert_eq!(encoded, vec![0x83, 0x01, 0x02, 0x43, 0x01, 0x02, 0x03]);
        assert_eq!(
            WireEnvelope::decode(&encoded, WireLimits::REFERENCE).unwrap(),
            envelope()
        );
    }

    #[test]
    fn rejects_indefinite_or_wrong_array_shape() {
        assert_eq!(
            WireEnvelope::decode(&[0x9f, 0x01, 0x02, 0x40, 0xff], WireLimits::REFERENCE)
                .unwrap_err(),
            WireError::InvalidEnvelopeShape
        );
    }

    #[test]
    fn rejects_trailing_data() {
        let mut encoded = envelope().encode(WireLimits::REFERENCE).unwrap();
        encoded.push(0);
        assert_eq!(
            WireEnvelope::decode(&encoded, WireLimits::REFERENCE).unwrap_err(),
            WireError::TrailingBytes
        );
    }

    #[test]
    fn enforces_payload_limit() {
        let limits = WireLimits::new(64, 2).unwrap();
        assert_eq!(
            envelope().encode(limits).unwrap_err(),
            WireError::PayloadTooLarge
        );
    }
}
