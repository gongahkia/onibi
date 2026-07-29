use std::error::Error;

use arachne_core::{OsKeystore, X25519IdentityKeypair};
use arachne_protocol::{
    X3dhError, X3dhInitialMessage, X3dhPrekeyBundle, X3dhSession, X25519IdentityBinding,
    initiate_x3dh, respond_x3dh,
};

use crate::{OneTimePrekeyReplenisher, OneTimePrekeyReplenisherError, SignedPrekeyLifecycle};

pub struct X3dhSessionEstablishmentService<'a, K> {
    local_identity: &'a X25519IdentityKeypair,
    local_binding: &'a X25519IdentityBinding,
    signed_prekey: &'a SignedPrekeyLifecycle,
    one_time_prekeys: &'a mut OneTimePrekeyReplenisher<K>,
}

impl<'a, K> X3dhSessionEstablishmentService<'a, K>
where
    K: OsKeystore,
{
    #[must_use]
    pub const fn new(
        local_identity: &'a X25519IdentityKeypair,
        local_binding: &'a X25519IdentityBinding,
        signed_prekey: &'a SignedPrekeyLifecycle,
        one_time_prekeys: &'a mut OneTimePrekeyReplenisher<K>,
    ) -> Self {
        Self {
            local_identity,
            local_binding,
            signed_prekey,
            one_time_prekeys,
        }
    }

    pub fn respond(
        &mut self,
        initial: &X3dhInitialMessage,
    ) -> Result<X3dhSession, X3dhSessionEstablishmentError<K::Error>> {
        let selected = initial
            .one_time_prekey()
            .map(|identifier| {
                self.one_time_prekeys
                    .load(identifier)
                    .map(|prekey| (identifier, prekey))
            })
            .transpose()
            .map_err(X3dhSessionEstablishmentError::OneTimePrekey)?;
        let session = respond_x3dh(
            self.local_identity,
            self.local_binding,
            self.signed_prekey.signed_prekey(),
            selected
                .as_ref()
                .map(|(identifier, prekey)| (*identifier, prekey)),
            initial,
        )
        .map_err(X3dhSessionEstablishmentError::X3dh)?;
        if let Some(identifier) = initial.one_time_prekey() {
            self.one_time_prekeys
                .take(identifier)
                .map_err(X3dhSessionEstablishmentError::OneTimePrekey)?;
        }
        Ok(session)
    }
}

pub fn initiate_x3dh_session(
    local_identity: &X25519IdentityKeypair,
    local_binding: X25519IdentityBinding,
    remote_bundle: &X3dhPrekeyBundle,
) -> Result<(X3dhInitialMessage, X3dhSession), X3dhError> {
    initiate_x3dh(local_identity, local_binding, remote_bundle)
}

#[derive(Debug, thiserror::Error)]
pub enum X3dhSessionEstablishmentError<E>
where
    E: Error + Send + Sync + 'static,
{
    #[error("X3DH session establishment failed")]
    X3dh(#[source] X3dhError),
    #[error("one-time prekey operation failed during X3DH")]
    OneTimePrekey(#[source] OneTimePrekeyReplenisherError<E>),
}
