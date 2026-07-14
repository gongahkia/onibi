use minicbor::{Decoder, Encoder};
use yeokcham_core::{Error, Result};

use crate::{ProtocolVersion, VersionRange};

const OFFER: u8 = 1;
const ACCEPT: u8 = 2;
const REJECT: u8 = 3;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum VersionNegotiation {
    Offer(VersionRange),
    Accept(ProtocolVersion),
    Reject(VersionRange),
}

impl VersionNegotiation {
    #[must_use]
    pub const fn offer(supported: VersionRange) -> Self {
        Self::Offer(supported)
    }

    pub fn accept(local: VersionRange, offer: Self) -> Result<Self> {
        Self::respond(local, offer)
    }

    pub fn respond(local: VersionRange, offer: Self) -> Result<Self> {
        let Self::Offer(peer) = offer else {
            return Err(Error::State("version negotiation requires an offer"));
        };
        match local.negotiate(peer) {
            Ok(version) => Ok(Self::Accept(version)),
            Err(Error::UnsupportedVersion(_)) => Ok(Self::Reject(local)),
            Err(error) => Err(error),
        }
    }

    pub fn encode(self) -> Result<Vec<u8>, VersionNegotiationError> {
        let mut encoder = Encoder::new(Vec::new());
        match self {
            Self::Offer(range) => {
                encoder
                    .array(3)
                    .map_err(|_| VersionNegotiationError::Encode)?
                    .u8(OFFER)
                    .map_err(|_| VersionNegotiationError::Encode)?
                    .u16(range.minimum().get())
                    .map_err(|_| VersionNegotiationError::Encode)?
                    .u16(range.maximum().get())
                    .map_err(|_| VersionNegotiationError::Encode)?;
            }
            Self::Accept(version) => {
                encoder
                    .array(2)
                    .map_err(|_| VersionNegotiationError::Encode)?
                    .u8(ACCEPT)
                    .map_err(|_| VersionNegotiationError::Encode)?
                    .u16(version.get())
                    .map_err(|_| VersionNegotiationError::Encode)?;
            }
            Self::Reject(range) => {
                encoder
                    .array(3)
                    .map_err(|_| VersionNegotiationError::Encode)?
                    .u8(REJECT)
                    .map_err(|_| VersionNegotiationError::Encode)?
                    .u16(range.minimum().get())
                    .map_err(|_| VersionNegotiationError::Encode)?
                    .u16(range.maximum().get())
                    .map_err(|_| VersionNegotiationError::Encode)?;
            }
        }
        Ok(encoder.into_writer())
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, VersionNegotiationError> {
        let mut decoder = Decoder::new(encoded);
        let fields = decoder
            .array()
            .map_err(|_| VersionNegotiationError::Decode)?;
        let kind = decoder.u8().map_err(|_| VersionNegotiationError::Decode)?;
        let result = match (fields, kind) {
            (Some(3), OFFER) => {
                let minimum = protocol_version(&mut decoder)?;
                let maximum = protocol_version(&mut decoder)?;
                Self::Offer(
                    VersionRange::new(minimum, maximum)
                        .map_err(|_| VersionNegotiationError::InvalidRange)?,
                )
            }
            (Some(2), ACCEPT) => Self::Accept(protocol_version(&mut decoder)?),
            (Some(3), REJECT) => {
                let minimum = protocol_version(&mut decoder)?;
                let maximum = protocol_version(&mut decoder)?;
                Self::Reject(
                    VersionRange::new(minimum, maximum)
                        .map_err(|_| VersionNegotiationError::InvalidRange)?,
                )
            }
            (_, OFFER | ACCEPT | REJECT) => return Err(VersionNegotiationError::InvalidShape),
            (_, value) => return Err(VersionNegotiationError::UnknownKind(value)),
        };
        if decoder.position() != encoded.len() {
            return Err(VersionNegotiationError::TrailingBytes);
        }
        Ok(result)
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum VersionNegotiationError {
    #[error("unknown version-negotiation kind: {0}")]
    UnknownKind(u8),
    #[error("version-negotiation frame has an invalid shape")]
    InvalidShape,
    #[error("version range is invalid")]
    InvalidRange,
    #[error("version is invalid")]
    InvalidVersion,
    #[error("CBOR decoding failed")]
    Decode,
    #[error("CBOR encoding failed")]
    Encode,
    #[error("trailing bytes after version negotiation")]
    TrailingBytes,
}

fn protocol_version(decoder: &mut Decoder<'_>) -> Result<ProtocolVersion, VersionNegotiationError> {
    ProtocolVersion::new(decoder.u16().map_err(|_| VersionNegotiationError::Decode)?)
        .map_err(|_| VersionNegotiationError::InvalidVersion)
}

#[cfg(test)]
mod tests {
    use super::{VersionNegotiation, VersionNegotiationError};
    use crate::{ProtocolVersion, VersionRange};

    fn range(minimum: u16, maximum: u16) -> VersionRange {
        VersionRange::new(
            ProtocolVersion::new(minimum).unwrap(),
            ProtocolVersion::new(maximum).unwrap(),
        )
        .unwrap()
    }

    #[test]
    fn offer_and_accept_are_canonical() {
        let offer = VersionNegotiation::offer(range(1, 3));
        assert_eq!(offer.encode().unwrap(), [0x83, 1, 1, 3]);
        assert_eq!(
            VersionNegotiation::decode(&offer.encode().unwrap()).unwrap(),
            offer
        );
        let accept = VersionNegotiation::accept(range(2, 4), offer).unwrap();
        assert_eq!(
            accept,
            VersionNegotiation::Accept(ProtocolVersion::new(3).unwrap())
        );
        assert_eq!(accept.encode().unwrap(), [0x82, 2, 3]);
    }

    #[test]
    fn rejects_invalid_state_and_frames() {
        assert!(
            VersionNegotiation::accept(
                range(1, 1),
                VersionNegotiation::Accept(ProtocolVersion::INITIAL)
            )
            .is_err()
        );
        assert_eq!(
            VersionNegotiation::decode(&[0x82, 4, 1]).unwrap_err(),
            VersionNegotiationError::UnknownKind(4)
        );
        assert_eq!(
            VersionNegotiation::decode(&[0x82, 1, 1]).unwrap_err(),
            VersionNegotiationError::InvalidShape
        );
    }

    #[test]
    fn rejects_nonoverlapping_offers_without_downgrading() {
        let response =
            VersionNegotiation::respond(range(1, 1), VersionNegotiation::offer(range(2, 3)))
                .unwrap();
        assert_eq!(response, VersionNegotiation::Reject(range(1, 1)));
        assert_eq!(response.encode().unwrap(), [0x83, 3, 1, 1]);
        assert_eq!(
            VersionNegotiation::decode(&response.encode().unwrap()).unwrap(),
            response
        );
    }
}
