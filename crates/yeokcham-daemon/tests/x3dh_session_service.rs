use std::{
    collections::BTreeMap,
    convert::Infallible,
    fs,
    path::PathBuf,
    sync::atomic::{AtomicU64, Ordering},
};

use yeokcham_core::{
    IdentityKeypair, KeystoreEntryName, KeystoreSecret, OneTimePrekeyId, OsKeystore,
    X25519IdentityKeypair, X25519Prekey,
};
use yeokcham_daemon::{
    OneTimePrekeyReplenisher, SignedPrekeyLifecycle, X3dhSessionEstablishmentError,
    X3dhSessionEstablishmentService, initiate_x3dh_session,
};
use yeokcham_protocol::{OneTimePrekeyPublic, X3dhPrekeyBundle, X25519IdentityBinding};

static NEXT_TEST_PATH: AtomicU64 = AtomicU64::new(0);

#[derive(Default)]
struct MemoryKeystore(BTreeMap<String, Vec<u8>>);

impl OsKeystore for MemoryKeystore {
    type Error = Infallible;

    fn load(&self, entry: &KeystoreEntryName) -> Result<Option<KeystoreSecret>, Self::Error> {
        Ok(self
            .0
            .get(entry.as_str())
            .map(|secret| KeystoreSecret::new(secret.clone()).unwrap()))
    }

    fn store(
        &mut self,
        entry: &KeystoreEntryName,
        secret: &KeystoreSecret,
    ) -> Result<(), Self::Error> {
        self.0
            .insert(entry.as_str().to_owned(), secret.as_bytes().to_vec());
        Ok(())
    }

    fn delete(&mut self, entry: &KeystoreEntryName) -> Result<(), Self::Error> {
        self.0.remove(entry.as_str());
        Ok(())
    }
}

fn inventory_path() -> PathBuf {
    let number = NEXT_TEST_PATH.fetch_add(1, Ordering::Relaxed);
    std::env::temp_dir()
        .join(format!(
            "yeokcham-x3dh-session-service-{}-{number}",
            std::process::id()
        ))
        .join("prekeys.sqlite")
}

#[test]
fn establishes_matching_sessions_and_consumes_the_selected_one_time_prekey() {
    let path = inventory_path();
    fs::create_dir(path.parent().unwrap()).unwrap();
    let responder_signing = IdentityKeypair::generate().unwrap();
    let responder_exchange = X25519IdentityKeypair::generate().unwrap();
    let mut keystore = MemoryKeystore::default();
    let signed_prekey = SignedPrekeyLifecycle::create(&responder_signing, &mut keystore).unwrap();
    let mut one_time_prekeys = OneTimePrekeyReplenisher::open(&path, keystore).unwrap();
    one_time_prekeys.replenish(1).unwrap();
    let responder_binding =
        X25519IdentityBinding::create(&responder_signing, &responder_exchange).unwrap();
    let bundle = X3dhPrekeyBundle::create(
        &responder_signing,
        &responder_exchange,
        signed_prekey.public(),
        one_time_prekeys.available_public().unwrap(),
    )
    .unwrap();
    let initiator_signing = IdentityKeypair::generate().unwrap();
    let initiator_exchange = X25519IdentityKeypair::generate().unwrap();
    let initiator_binding =
        X25519IdentityBinding::create(&initiator_signing, &initiator_exchange).unwrap();
    let (initial, initiated) =
        initiate_x3dh_session(&initiator_exchange, initiator_binding, &bundle).unwrap();
    let responded = X3dhSessionEstablishmentService::new(
        &responder_exchange,
        &responder_binding,
        &signed_prekey,
        &mut one_time_prekeys,
    )
    .respond(&initial)
    .unwrap();
    assert_eq!(initiated.root_key(), responded.root_key());
    assert_eq!(initiated.associated_data(), responded.associated_data());
    assert_eq!(initiated.used_one_time_prekey(), initial.one_time_prekey());
    assert_eq!(one_time_prekeys.available().len(), 0);
    drop(one_time_prekeys);
    fs::remove_dir_all(path.parent().unwrap()).unwrap();
}

#[test]
fn rejects_an_unavailable_one_time_prekey_without_consuming_an_available_key() {
    let path = inventory_path();
    fs::create_dir(path.parent().unwrap()).unwrap();
    let responder_signing = IdentityKeypair::generate().unwrap();
    let responder_exchange = X25519IdentityKeypair::generate().unwrap();
    let mut keystore = MemoryKeystore::default();
    let signed_prekey = SignedPrekeyLifecycle::create(&responder_signing, &mut keystore).unwrap();
    let mut one_time_prekeys = OneTimePrekeyReplenisher::open(&path, keystore).unwrap();
    one_time_prekeys.replenish(1).unwrap();
    let unavailable = OneTimePrekeyPublic::new(
        OneTimePrekeyId::new(2).unwrap(),
        &X25519Prekey::generate().unwrap(),
    );
    let bundle = X3dhPrekeyBundle::create(
        &responder_signing,
        &responder_exchange,
        signed_prekey.public(),
        vec![unavailable],
    )
    .unwrap();
    let initiator_signing = IdentityKeypair::generate().unwrap();
    let initiator_exchange = X25519IdentityKeypair::generate().unwrap();
    let initiator_binding =
        X25519IdentityBinding::create(&initiator_signing, &initiator_exchange).unwrap();
    let (initial, _) =
        initiate_x3dh_session(&initiator_exchange, initiator_binding, &bundle).unwrap();
    let responder_binding =
        X25519IdentityBinding::create(&responder_signing, &responder_exchange).unwrap();
    assert!(matches!(
        X3dhSessionEstablishmentService::new(
            &responder_exchange,
            &responder_binding,
            &signed_prekey,
            &mut one_time_prekeys,
        )
        .respond(&initial),
        Err(X3dhSessionEstablishmentError::OneTimePrekey(_))
    ));
    assert_eq!(
        one_time_prekeys
            .available()
            .map(OneTimePrekeyId::get)
            .collect::<Vec<_>>(),
        vec![1]
    );
    drop(one_time_prekeys);
    fs::remove_dir_all(path.parent().unwrap()).unwrap();
}
