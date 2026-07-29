use arachne_core::{
    ED25519_PUBLIC_KEY_BYTES, ED25519_SIGNATURE_BYTES, IdentityKeypair, IdentityPublicKey,
};
use getrandom::{SysRng, rand_core::TryRng};
use minicbor::{Decoder, Encoder};

use crate::CryptoDomain;

pub const CONTACT_INVITATION_SCHEMA_VERSION: u8 = 1;
pub const CONTACT_INVITATION_NONCE_BYTES: usize = 32;
pub const CONTACT_INVITATION_BYTES: usize = 136;
const CONTACT_INVITATION_FIELDS: u64 = 4;
const CONTACT_INVITATION_SIGNING_FIELDS: u64 = 3;
const CONTACT_INVITATION_SIGNING_INPUT_FIELDS: u64 = 2;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct ContactInvitation {
    inviter: IdentityPublicKey,
    nonce: [u8; CONTACT_INVITATION_NONCE_BYTES],
    signature: [u8; ED25519_SIGNATURE_BYTES],
}

impl ContactInvitation {
    pub fn create(identity: &IdentityKeypair) -> Result<Self, ContactInvitationError> {
        let inviter = identity.public_key();
        let mut nonce = [0; CONTACT_INVITATION_NONCE_BYTES];
        let mut random_source = SysRng;
        random_source
            .try_fill_bytes(&mut nonce)
            .map_err(|_| ContactInvitationError::Randomness)?;
        if nonce.iter().all(|byte| *byte == 0) {
            return Err(ContactInvitationError::Randomness);
        }
        let signing_input = signing_input(&inviter, &nonce)?;
        let signature = identity.sign(&signing_input);
        Ok(Self {
            inviter,
            nonce,
            signature,
        })
    }

    #[must_use]
    pub const fn inviter(&self) -> &IdentityPublicKey {
        &self.inviter
    }

    #[must_use]
    pub const fn nonce(&self) -> &[u8; CONTACT_INVITATION_NONCE_BYTES] {
        &self.nonce
    }

    #[must_use]
    pub const fn signature(&self) -> &[u8; ED25519_SIGNATURE_BYTES] {
        &self.signature
    }

    pub fn encode(&self) -> Result<Vec<u8>, ContactInvitationError> {
        let mut encoder = Encoder::new(Vec::new());
        encoder
            .array(CONTACT_INVITATION_FIELDS)
            .map_err(|_| ContactInvitationError::Encode)?
            .u8(CONTACT_INVITATION_SCHEMA_VERSION)
            .map_err(|_| ContactInvitationError::Encode)?
            .bytes(self.inviter.as_bytes())
            .map_err(|_| ContactInvitationError::Encode)?
            .bytes(&self.nonce)
            .map_err(|_| ContactInvitationError::Encode)?
            .bytes(&self.signature)
            .map_err(|_| ContactInvitationError::Encode)?;
        let bytes = encoder.into_writer();
        if bytes.len() != CONTACT_INVITATION_BYTES {
            return Err(ContactInvitationError::Encode);
        }
        Ok(bytes)
    }

    pub fn decode(encoded: &[u8]) -> Result<Self, ContactInvitationError> {
        if encoded.len() != CONTACT_INVITATION_BYTES {
            return Err(ContactInvitationError::InvalidLength);
        }
        let mut decoder = Decoder::new(encoded);
        if decoder
            .array()
            .map_err(|_| ContactInvitationError::Decode)?
            != Some(CONTACT_INVITATION_FIELDS)
        {
            return Err(ContactInvitationError::InvalidShape);
        }
        let version = decoder.u8().map_err(|_| ContactInvitationError::Decode)?;
        if version != CONTACT_INVITATION_SCHEMA_VERSION {
            return Err(ContactInvitationError::UnsupportedSchemaVersion(version));
        }
        let inviter = decode_identity(
            decoder
                .bytes()
                .map_err(|_| ContactInvitationError::Decode)?,
        )?;
        let nonce = decode_nonce(
            decoder
                .bytes()
                .map_err(|_| ContactInvitationError::Decode)?,
        )?;
        let signature = decode_signature(
            decoder
                .bytes()
                .map_err(|_| ContactInvitationError::Decode)?,
        )?;
        if decoder.position() != encoded.len() {
            return Err(ContactInvitationError::TrailingBytes);
        }
        let invitation = Self {
            inviter,
            nonce,
            signature,
        };
        let signing_input = signing_input(&invitation.inviter, &invitation.nonce)?;
        invitation
            .inviter
            .verify(&signing_input, &invitation.signature)
            .map_err(|_| ContactInvitationError::InvalidSignature)?;
        if invitation.encode()? != encoded {
            return Err(ContactInvitationError::NonCanonicalEncoding);
        }
        Ok(invitation)
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum ContactInvitationError {
    #[error("operating-system random source failed")]
    Randomness,
    #[error("contact invitation encoding failed")]
    Encode,
    #[error("contact invitation has an invalid length")]
    InvalidLength,
    #[error("unsupported contact invitation schema version: {0}")]
    UnsupportedSchemaVersion(u8),
    #[error("contact invitation must be a four-element definite-length CBOR array")]
    InvalidShape,
    #[error("contact invitation contains an invalid inviter identity")]
    InvalidIdentity,
    #[error("contact invitation nonce is invalid")]
    InvalidNonce,
    #[error("contact invitation signature is invalid")]
    InvalidSignature,
    #[error("CBOR decoding failed")]
    Decode,
    #[error("trailing bytes after contact invitation")]
    TrailingBytes,
    #[error("contact invitation is not canonically encoded")]
    NonCanonicalEncoding,
}

fn decode_identity(encoded: &[u8]) -> Result<IdentityPublicKey, ContactInvitationError> {
    if encoded.len() != ED25519_PUBLIC_KEY_BYTES {
        return Err(ContactInvitationError::InvalidIdentity);
    }
    let mut bytes = [0; ED25519_PUBLIC_KEY_BYTES];
    bytes.copy_from_slice(encoded);
    IdentityPublicKey::from_bytes(bytes).map_err(|_| ContactInvitationError::InvalidIdentity)
}

fn decode_nonce(
    encoded: &[u8],
) -> Result<[u8; CONTACT_INVITATION_NONCE_BYTES], ContactInvitationError> {
    if encoded.len() != CONTACT_INVITATION_NONCE_BYTES {
        return Err(ContactInvitationError::InvalidNonce);
    }
    let mut nonce = [0; CONTACT_INVITATION_NONCE_BYTES];
    nonce.copy_from_slice(encoded);
    if nonce.iter().all(|byte| *byte == 0) {
        return Err(ContactInvitationError::InvalidNonce);
    }
    Ok(nonce)
}

fn decode_signature(
    encoded: &[u8],
) -> Result<[u8; ED25519_SIGNATURE_BYTES], ContactInvitationError> {
    if encoded.len() != ED25519_SIGNATURE_BYTES {
        return Err(ContactInvitationError::InvalidSignature);
    }
    let mut signature = [0; ED25519_SIGNATURE_BYTES];
    signature.copy_from_slice(encoded);
    Ok(signature)
}

fn signing_input(
    inviter: &IdentityPublicKey,
    nonce: &[u8; CONTACT_INVITATION_NONCE_BYTES],
) -> Result<Vec<u8>, ContactInvitationError> {
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(CONTACT_INVITATION_SIGNING_FIELDS)
        .map_err(|_| ContactInvitationError::Encode)?
        .u8(CONTACT_INVITATION_SCHEMA_VERSION)
        .map_err(|_| ContactInvitationError::Encode)?
        .bytes(inviter.as_bytes())
        .map_err(|_| ContactInvitationError::Encode)?
        .bytes(nonce)
        .map_err(|_| ContactInvitationError::Encode)?;
    let unsigned = encoder.into_writer();
    let mut encoder = Encoder::new(Vec::new());
    encoder
        .array(CONTACT_INVITATION_SIGNING_INPUT_FIELDS)
        .map_err(|_| ContactInvitationError::Encode)?
        .bytes(CryptoDomain::ContactInvitationSignature.context())
        .map_err(|_| ContactInvitationError::Encode)?
        .bytes(&unsigned)
        .map_err(|_| ContactInvitationError::Encode)?;
    Ok(encoder.into_writer())
}

#[cfg(test)]
mod tests {
    use super::{
        CONTACT_INVITATION_BYTES, CONTACT_INVITATION_NONCE_BYTES,
        CONTACT_INVITATION_SCHEMA_VERSION, ContactInvitation, ContactInvitationError,
        signing_input,
    };
    use arachne_core::{ED25519_PUBLIC_KEY_BYTES, IdentityKeypair};

    #[test]
    fn creates_canonical_domain_separated_signed_invitations() {
        let identity = IdentityKeypair::generate().unwrap();
        let invitation = ContactInvitation::create(&identity).unwrap();
        let encoded = invitation.encode().unwrap();
        let input = signing_input(invitation.inviter(), invitation.nonce()).unwrap();

        assert_eq!(encoded.len(), CONTACT_INVITATION_BYTES);
        assert_eq!(encoded[0], 0x84);
        assert_eq!(encoded[1], CONTACT_INVITATION_SCHEMA_VERSION);
        assert_eq!(encoded[2], 0x58);
        assert_eq!(encoded[3], u8::try_from(ED25519_PUBLIC_KEY_BYTES).unwrap());
        assert_eq!(invitation.inviter(), &identity.public_key());
        assert_eq!(invitation.nonce().len(), CONTACT_INVITATION_NONCE_BYTES);
        assert_eq!(input[0], 0x82);
        assert_eq!(invitation.signature(), &identity.sign(&input));
        assert_eq!(ContactInvitation::decode(&encoded).unwrap(), invitation);
    }

    #[test]
    fn rejects_invalid_contact_invitations() {
        let invitation = ContactInvitation::create(&IdentityKeypair::generate().unwrap()).unwrap();
        let encoded = invitation.encode().unwrap();
        let mut invalid_identity = encoded.clone();
        invalid_identity[4..36].fill(0);
        let mut invalid_nonce = encoded.clone();
        invalid_nonce[38..70].fill(0);
        let mut invalid_signature = encoded.clone();
        let last = invalid_signature.len() - 1;
        invalid_signature[last] ^= 1;
        let mut unsupported_version = encoded.clone();
        unsupported_version[1] = CONTACT_INVITATION_SCHEMA_VERSION + 1;
        let mut invalid_shape = encoded.clone();
        invalid_shape[0] = 0x83;

        assert_eq!(
            ContactInvitation::decode(&invalid_identity).unwrap_err(),
            ContactInvitationError::InvalidIdentity
        );
        assert_eq!(
            ContactInvitation::decode(&invalid_nonce).unwrap_err(),
            ContactInvitationError::InvalidNonce
        );
        assert_eq!(
            ContactInvitation::decode(&invalid_signature).unwrap_err(),
            ContactInvitationError::InvalidSignature
        );
        assert_eq!(
            ContactInvitation::decode(&unsupported_version).unwrap_err(),
            ContactInvitationError::UnsupportedSchemaVersion(2)
        );
        assert_eq!(
            ContactInvitation::decode(&invalid_shape).unwrap_err(),
            ContactInvitationError::InvalidShape
        );
        assert_eq!(
            ContactInvitation::decode(&encoded[..encoded.len() - 1]).unwrap_err(),
            ContactInvitationError::InvalidLength
        );
    }
}
