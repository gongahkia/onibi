#![allow(clippy::similar_names)]

mod support;

use arachne_daemon::{
    AuthenticatedDirectHandoff, DirectTransportError, LanDirectTransport, LocalMeshSessionError,
    LocalTransport, WifiDirectTransport,
};
use arachne_protocol::{
    LocalMeshProfileConfig, LocalMeshProfileConstraintError, LocalMeshProfileConstraints,
    LocalMeshTransportKind, RecipientCapability,
};
use support::{assert_transport_conformance, establish_authenticated_sessions};

trait LocalMeshAdapter: LocalTransport<Error = DirectTransportError> + Sized {
    fn from_authenticated_handoff(
        handoff: AuthenticatedDirectHandoff,
    ) -> Result<Self, LocalMeshSessionError>;

    fn close(&self);
}

impl LocalMeshAdapter for LanDirectTransport {
    fn from_authenticated_handoff(
        handoff: AuthenticatedDirectHandoff,
    ) -> Result<Self, LocalMeshSessionError> {
        Self::from_authenticated_handoff(handoff)
    }

    fn close(&self) {
        self.connection().close();
    }
}

impl LocalMeshAdapter for WifiDirectTransport {
    fn from_authenticated_handoff(
        handoff: AuthenticatedDirectHandoff,
    ) -> Result<Self, LocalMeshSessionError> {
        Self::from_authenticated_handoff(handoff)
    }

    fn close(&self) {
        self.connection().close();
    }
}

#[tokio::test]
async fn two_nodes_exchange_lan_frames_with_a_lan_capability() {
    exchange_local_mesh_frames::<LanDirectTransport>(LocalMeshTransportKind::Lan).await;
}

#[tokio::test]
async fn two_nodes_exchange_wifi_direct_frames_with_a_wifi_direct_capability() {
    exchange_local_mesh_frames::<WifiDirectTransport>(LocalMeshTransportKind::WifiDirect).await;
}

async fn exchange_local_mesh_frames<T>(transport: LocalMeshTransportKind)
where
    T: LocalMeshAdapter,
{
    let capability = RecipientCapability::LocalMesh(LocalMeshProfileConfig::new(transport));
    let decoded_capability = RecipientCapability::decode(&capability.encode().unwrap()).unwrap();
    let advertised_transport = match decoded_capability {
        RecipientCapability::LocalMesh(config) => config.transport(),
        _ => unreachable!(),
    };
    assert_eq!(advertised_transport, transport);
    let disallowed_transport = match transport {
        LocalMeshTransportKind::Lan => LocalMeshTransportKind::WifiDirect,
        LocalMeshTransportKind::WifiDirect => LocalMeshTransportKind::Lan,
        LocalMeshTransportKind::WifiHotspot | LocalMeshTransportKind::Bluetooth => unreachable!(),
    };
    let constraints = LocalMeshProfileConstraints::new(
        transport == LocalMeshTransportKind::Lan,
        false,
        transport == LocalMeshTransportKind::WifiDirect,
        false,
    )
    .unwrap();
    assert!(
        constraints
            .validate(LocalMeshProfileConfig::new(transport))
            .is_ok()
    );
    assert_eq!(
        constraints
            .validate(LocalMeshProfileConfig::new(disallowed_transport))
            .unwrap_err(),
        LocalMeshProfileConstraintError::DisallowedTransport(disallowed_transport)
    );

    let mut sessions = establish_authenticated_sessions(transport).await;
    let node_a_adapter =
        T::from_authenticated_handoff(sessions.node_a_session.handoff_for(transport).unwrap())
            .unwrap();
    let node_b_adapter =
        T::from_authenticated_handoff(sessions.node_b_session.handoff_for(transport).unwrap())
            .unwrap();
    assert_transport_conformance(&node_a_adapter, &node_b_adapter, advertised_transport).await;
    node_a_adapter.close();
    node_b_adapter.close();
    sessions.shutdown().await;
}
