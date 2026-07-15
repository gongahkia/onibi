use minicbor::{Decoder, Encoder};
use yeokcham_core::{
    ED25519_PUBLIC_KEY_BYTES, ED25519_SIGNATURE_BYTES, IdentityKeypair, IdentityPublicKey,
};

use crate::CryptoDomain;

pub const DIRECT_PEER_PROOF_SCHEMA_VERSION: u8 = 1;
pub const DIRECT_PEER_PROOF_BYTES: usize = 102;
const PROOF_FIELDS: u64 = 3;
const SIGNING_FIELDS: u64 = 3;
const SIGNING_INPUT_FIELDS: u64 = 2;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct DirectPeerProof {
    identity: IdentityPublicKey,
    signature: [u8; ED25519_SIGNATURE_BYTES],
}

impl DirectPeerProof {
    pub fn create(
        identity: &IdentityKeypair,
        connection_binding: &[u8; 32],
    ) -> Result<Self, DirectPeerProofError> {
        let identity_public = identity.public_key();
        let signing_input = signing_input(&identity_public, connection_binding)?;
        Ok(Self {
            identity: identity_public,
            signature: identity.sign(&signing_input),
        })
    }

    #[must_use]
    pub const fn identity(&self) -> &IdentityPublicKey {
        &self.identity
    }

    pub fn verify(&self, connection_binding: &[u8; 32]) -> Result<(), DirectPeerProofError> {
        let signing_input = signing_input(&self.identity, connection_binding)?;
        self.identity
            .verify(&signing_input, &self.signature)
            .map_err(|_| DirectPeerProofError::InvalidSignature)
    }

    pub fn encode(&self) -> Result<Vec<u8>, DirectPeerProofError> {
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(PROOF_FIELDS)
            .map_err(|_| DirectPeerProofError::Encode)?
            .u8(DIRECT_PEER_PROOF_SCHEMA_VERSION)
            .map_err(|_| DirectPeerProofError::Encode)?
            .bytes(self.identity.as_bytes())
            .map_err(|_| DirectPeerProofError::Encode)?
            .bytes(&self.signature)
            .map_err(|_| DirectPeerProofError::Encode)?;
        let encoded = encoder.into_writer();
        if encoded.len() != DIRECT_PEER_PROOF_BYTES {
            return Err(DirectPeerProofError::Encode);
        }
        Ok(encoded)
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, DirectPeerProofError> {
        if encoded.len() != DIRECT_PEER_PROOF_BYTES {
            return Err(DirectPeerProofError::InvalidLength);
        }
        let mut decoder = Decoder::new(encoded);
        if decoder.array().map_err(|_| DirectPeerProofError::Decode)? != Some(PROOF_FIELDS) {
            return Err(DirectPeerProofError::InvalidShape);
        }
        if decoder.u8().map_err(|_| DirectPeerProofError::Decode)?
            != DIRECT_PEER_PROOF_SCHEMA_VERSION
        {
            return Err(DirectPeerProofError::UnsupportedSchemaVersion);
        }
        let identity = decode_identity(decoder.bytes().map_err(|_| DirectPeerProofError::Decode)?)?;
        let signature =
            decode_signature(decoder.bytes().map_err(|_| DirectPeerProofError::Decode)?)?;
        if decoder.position() != encoded.len() {
            return Err(DirectPeerProofError::TrailingBytes);
        }
        let proof = Self {
            identity,
            signature,
        };
        if proof.encode()? != encoded {
            return Err(DirectPeerProofError::NonCanonicalEncoding);
        }
        Ok(proof)
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum DirectPeerProofError {
    #[error("direct peer proof encoding failed")]
    Encode,
    #[error("direct peer proof decoding failed")]
    Decode,
    #[error("direct peer proof has an invalid length")]
    InvalidLength,
    #[error("direct peer proof has an invalid shape")]
    InvalidShape,
    #[error("direct peer proof schema version is unsupported")]
    UnsupportedSchemaVersion,
    #[error("direct peer proof contains an invalid identity")]
    InvalidIdentity,
    #[error("direct peer proof signature is invalid")]
    InvalidSignature,
    #[error("direct peer proof has trailing bytes")]
    TrailingBytes,
    #[error("direct peer proof is not canonically encoded")]
    NonCanonicalEncoding,
}

fn decode_identity(encoded: &[u8]) -> Result<IdentityPublicKey, DirectPeerProofError> {
    let bytes: [u8; ED25519_PUBLIC_KEY_BYTES] = encoded
        .try_into()
        .map_err(|_| DirectPeerProofError::InvalidIdentity)?;
    IdentityPublicKey::from_bytes(bytes).map_err(|_| DirectPeerProofError::InvalidIdentity)
}

fn decode_signature(encoded: &[u8]) -> Result<[u8; ED25519_SIGNATURE_BYTES], DirectPeerProofError> {
    encoded
        .try_into()
        .map_err(|_| DirectPeerProofError::InvalidSignature)
}

fn signing_input(
    identity: &IdentityPublicKey,
    connection_binding: &[u8; 32],
) -> Result<Vec<u8>, DirectPeerProofError> {
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(SIGNING_FIELDS)
        .map_err(|_| DirectPeerProofError::Encode)?
        .u8(DIRECT_PEER_PROOF_SCHEMA_VERSION)
        .map_err(|_| DirectPeerProofError::Encode)?
        .bytes(identity.as_bytes())
        .map_err(|_| DirectPeerProofError::Encode)?
        .bytes(connection_binding)
        .map_err(|_| DirectPeerProofError::Encode)?;
    let unsigned = encoder.into_writer();
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(SIGNING_INPUT_FIELDS)
        .map_err(|_| DirectPeerProofError::Encode)?
        .bytes(CryptoDomain::DirectPeerAuthentication.context())
        .map_err(|_| DirectPeerProofError::Encode)?
        .bytes(&unsigned)
        .map_err(|_| DirectPeerProofError::Encode)?;
    Ok(encoder.into_writer())
}

#[cfg(test)]
mod tests {
    use super::{DirectPeerProof, DirectPeerProofError};
    use yeokcham_core::IdentityKeypair;

    #[test]
    fn binds_identity_proofs_to_the_connection() {
        let identity = IdentityKeypair::generate().unwrap();
        let binding = [7; 32];
        let proof = DirectPeerProof::create(&identity, &binding).unwrap();
        let encoded = proof.encode().unwrap();

        assert_eq!(DirectPeerProof::decode(&encoded).unwrap(), proof);
        assert_eq!(proof.verify(&binding), Ok(()));
        assert_eq!(
            proof.verify(&[8; 32]),
            Err(DirectPeerProofError::InvalidSignature)
        );
    }
}
