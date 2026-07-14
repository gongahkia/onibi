use minicbor::Encoder;

use crate::{CryptoDomain, WireEnvelope, WireError, WireLimits};

const SIGNING_INPUT_FIELDS: u64 = 2;

impl WireEnvelope {
    pub fn signing_input(&self, limits: WireLimits) -> Result<Vec<u8>, SigningInputError> {
        let frame = self.encode(limits)?;
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(SIGNING_INPUT_FIELDS)
            .map_err(|_| SigningInputError::Encode)?
            .bytes(CryptoDomain::EnvelopeSignature.context())
            .map_err(|_| SigningInputError::Encode)?
            .bytes(&frame)
            .map_err(|_| SigningInputError::Encode)?;
        Ok(encoder.into_writer())
    }
}

#[derive(Debug, thiserror::Error)]
pub enum SigningInputError {
    #[error("wire envelope cannot be signed: {0}")]
    Wire(#[from] WireError),
    #[error("CBOR encoding failed")]
    Encode,
}

#[cfg(test)]
mod tests {
    use super::SigningInputError;
    use crate::{EnvelopeKind, ProtocolVersion, WireEnvelope, WireError, WireLimits};

    fn envelope() -> WireEnvelope {
        WireEnvelope {
            version: ProtocolVersion::INITIAL,
            kind: EnvelopeKind::EncryptedMessage,
            payload: vec![1, 2, 3],
        }
    }

    #[test]
    fn signing_input_is_canonical_and_domain_separated() {
        let mut expected = vec![0x82, 0x58, 0x1e];
        expected.extend(b"yeokcham/v1/envelope-signature");
        expected.extend([0x47, 0x83, 0x01, 0x02, 0x43, 0x01, 0x02, 0x03]);

        assert_eq!(
            envelope().signing_input(WireLimits::REFERENCE).unwrap(),
            expected
        );
    }

    #[test]
    fn signing_input_enforces_wire_limits() {
        assert!(matches!(
            envelope().signing_input(WireLimits::new(64, 2).unwrap()),
            Err(SigningInputError::Wire(WireError::PayloadTooLarge))
        ));
    }
}
