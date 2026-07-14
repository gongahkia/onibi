use getrandom::{SysRng, rand_core::TryRng};
use minicbor::Encoder;
use yeokcham_core::{ED25519_SIGNATURE_BYTES, IdentityKeypair, IdentityPublicKey};

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
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum ContactInvitationError {
    #[error("operating-system random source failed")]
    Randomness,
    #[error("contact invitation encoding failed")]
    Encode,
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
        CONTACT_INVITATION_SCHEMA_VERSION, ContactInvitation, signing_input,
    };
    use yeokcham_core::{ED25519_PUBLIC_KEY_BYTES, IdentityKeypair};

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
    }
}
