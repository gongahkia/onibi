use std::{
    collections::BTreeMap,
    convert::Infallible,
    net::{SocketAddr, UdpSocket},
    path::PathBuf,
    sync::atomic::{AtomicU64, Ordering},
    time::Duration,
};

use yeokcham_core::{IdentityKeypair, KeystoreEntryName, KeystoreSecret, OsKeystore};
use yeokcham_daemon::{
    ClientStateDirectory, SharedIpMeshConfig, SharedIpMeshEndpoint, SharedIpMeshPeer,
    SharedIpMeshTlsIdentity, SharedIpMeshTransport,
};
use yeokcham_protocol::DirectProfileConfig;

static NEXT_TEST_DIRECTORY: AtomicU64 = AtomicU64::new(0);

#[derive(Default)]
struct MemoryKeystore(BTreeMap<String, Vec<u8>>);

impl OsKeystore for MemoryKeystore {
    type Error = Infallible;

    fn load(&self, entry: &KeystoreEntryName) -> Result<Option<KeystoreSecret>, Self::Error> {
        Ok(self
            .0
            .get(entry.as_str())
            .map(|value| KeystoreSecret::new(value.clone()).unwrap()))
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

#[tokio::test]
async fn authenticated_lan_peers_interoperate_over_shared_ip() {
    interoperate(SharedIpMeshTransport::Lan).await;
}

#[tokio::test]
async fn authenticated_hotspot_peers_interoperate_over_shared_ip() {
    interoperate(SharedIpMeshTransport::WifiHotspot).await;
}

#[tokio::test]
async fn rejects_a_tls_pinned_peer_with_the_wrong_identity() {
    let alice_directory = test_directory("identity-alice");
    let bob_directory = test_directory("identity-bob");
    let alice_layout = ClientStateDirectory::new(&alice_directory).unwrap();
    let bob_layout = ClientStateDirectory::new(&bob_directory).unwrap();
    let mut alice_keystore = MemoryKeystore::default();
    let mut bob_keystore = MemoryKeystore::default();
    let alice_identity = IdentityKeypair::generate().unwrap();
    let bob_identity = IdentityKeypair::generate().unwrap();
    let unexpected_identity = IdentityKeypair::generate().unwrap();
    let alice_tls = SharedIpMeshTlsIdentity::create(&alice_layout, &mut alice_keystore).unwrap();
    let bob_tls = SharedIpMeshTlsIdentity::create(&bob_layout, &mut bob_keystore).unwrap();
    let alice_address = unused_loopback_address();
    let bob_address = unused_loopback_address();
    let mut alice_config = SharedIpMeshConfig::new(
        alice_directory.clone(),
        SharedIpMeshTransport::Lan,
        alice_address,
    )
    .unwrap();
    alice_config
        .add_peer(SharedIpMeshPeer::new(
            unexpected_identity.public_key(),
            bob_tls.certificate_pin(),
        ))
        .unwrap();
    let mut bob_config = SharedIpMeshConfig::new(
        bob_directory.clone(),
        SharedIpMeshTransport::Lan,
        bob_address,
    )
    .unwrap();
    bob_config
        .add_peer(SharedIpMeshPeer::new(
            alice_identity.public_key(),
            alice_tls.certificate_pin(),
        ))
        .unwrap();
    let alice = SharedIpMeshEndpoint::start(
        alice_config.clone(),
        alice_identity.public_key(),
        &alice_tls,
    )
    .unwrap();
    let bob = SharedIpMeshEndpoint::start(bob_config, bob_identity.public_key(), &bob_tls).unwrap();
    let peer = alice_config.peer(unexpected_identity.public_key()).unwrap();
    let (connected, accepted) = tokio::join!(
        alice.connect_to(
            peer,
            DirectProfileConfig::new(bob_address).unwrap(),
            &alice_identity,
            &alice_tls,
        ),
        bob.accept(&bob_identity),
    );
    assert!(connected.is_err());
    drop(accepted.unwrap());
    alice.shutdown().unwrap();
    bob.shutdown().unwrap();
    std::fs::remove_dir_all(alice_directory).unwrap();
    std::fs::remove_dir_all(bob_directory).unwrap();
}

async fn interoperate(transport: SharedIpMeshTransport) {
    let alice_directory = test_directory("alice");
    let bob_directory = test_directory("bob");
    let alice_layout = ClientStateDirectory::new(&alice_directory).unwrap();
    let bob_layout = ClientStateDirectory::new(&bob_directory).unwrap();
    let mut alice_keystore = MemoryKeystore::default();
    let mut bob_keystore = MemoryKeystore::default();
    let alice_identity = IdentityKeypair::generate().unwrap();
    let bob_identity = IdentityKeypair::generate().unwrap();
    let alice_tls = SharedIpMeshTlsIdentity::create(&alice_layout, &mut alice_keystore).unwrap();
    let bob_tls = SharedIpMeshTlsIdentity::create(&bob_layout, &mut bob_keystore).unwrap();
    let alice_address = unused_loopback_address();
    let bob_address = unused_loopback_address();
    let mut alice_config =
        SharedIpMeshConfig::new(alice_directory.clone(), transport, alice_address).unwrap();
    alice_config
        .add_peer(SharedIpMeshPeer::new(
            bob_identity.public_key(),
            bob_tls.certificate_pin(),
        ))
        .unwrap();
    let mut bob_config =
        SharedIpMeshConfig::new(bob_directory.clone(), transport, bob_address).unwrap();
    bob_config
        .add_peer(SharedIpMeshPeer::new(
            alice_identity.public_key(),
            alice_tls.certificate_pin(),
        ))
        .unwrap();
    let alice = SharedIpMeshEndpoint::start(
        alice_config.clone(),
        alice_identity.public_key(),
        &alice_tls,
    )
    .unwrap();
    let bob = SharedIpMeshEndpoint::start(bob_config, bob_identity.public_key(), &bob_tls).unwrap();
    let discovered = alice
        .discover_trusted_peer(bob_identity.public_key(), Duration::from_secs(5))
        .unwrap();
    let (connected, accepted) = tokio::join!(
        alice.connect(discovered, &alice_identity, &alice_tls),
        bob.accept(&bob_identity),
    );
    let connected = connected.unwrap();
    let accepted = accepted.unwrap();
    assert_eq!(connected.peer().identity(), bob_identity.public_key());
    assert_eq!(accepted.peer().identity(), alice_identity.public_key());
    assert_eq!(
        connected.peer().transport(),
        transport.local_mesh_transport()
    );
    drop(connected);
    drop(accepted);
    alice.shutdown().unwrap();
    bob.shutdown().unwrap();
    std::fs::remove_dir_all(alice_directory).unwrap();
    std::fs::remove_dir_all(bob_directory).unwrap();
}

fn unused_loopback_address() -> SocketAddr {
    let socket = UdpSocket::bind("127.0.0.1:0").unwrap();
    let address = socket.local_addr().unwrap();
    drop(socket);
    address
}

fn test_directory(label: &str) -> PathBuf {
    let number = NEXT_TEST_DIRECTORY.fetch_add(1, Ordering::Relaxed);
    std::env::temp_dir().join(format!(
        "yeokcham-shared-ip-mesh-{label}-{}-{number}",
        std::process::id()
    ))
}
