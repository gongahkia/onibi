use arachne_core::IdentityKeypair;
use arachne_protocol::{LocalMeshPeer, LocalMeshTransportKind};
use quinn::ClientConfig;

use crate::{
    DirectConnection, DirectConnectionAttempts, DirectTransport, DirectTransportError,
    LocalTransportAvailability, LocalTransportAvailabilityError,
};

pub struct AuthenticatedLocalMeshSession {
    peer: LocalMeshPeer,
    connection: Option<DirectConnection>,
}

impl AuthenticatedLocalMeshSession {
    pub async fn initiate(
        endpoint: &DirectTransport,
        availability: LocalTransportAvailability,
        peer: LocalMeshPeer,
        local_identity: &IdentityKeypair,
        client_config: ClientConfig,
        server_name: &str,
        attempts: DirectConnectionAttempts,
    ) -> Result<Self, LocalMeshSessionError> {
        let connection = connect_initiator(
            endpoint,
            availability,
            peer,
            local_identity,
            client_config,
            server_name,
            attempts,
        )
        .await?;
        Ok(Self {
            peer,
            connection: Some(connection),
        })
    }

    pub async fn respond(
        endpoint: &DirectTransport,
        availability: LocalTransportAvailability,
        peer: LocalMeshPeer,
        local_identity: &IdentityKeypair,
    ) -> Result<Self, LocalMeshSessionError> {
        let connection = accept_responder(endpoint, availability, peer, local_identity).await?;
        Ok(Self {
            peer,
            connection: Some(connection),
        })
    }

    #[must_use]
    pub const fn peer(&self) -> LocalMeshPeer {
        self.peer
    }

    #[must_use]
    pub const fn is_active(&self) -> bool {
        self.connection.is_some()
    }

    pub fn cancel(&mut self) {
        if let Some(connection) = self.connection.take() {
            connection.close();
        }
    }

    pub async fn reconnect_initiator(
        &mut self,
        endpoint: &DirectTransport,
        availability: LocalTransportAvailability,
        local_identity: &IdentityKeypair,
        client_config: ClientConfig,
        server_name: &str,
        attempts: DirectConnectionAttempts,
    ) -> Result<(), LocalMeshSessionError> {
        self.cancel();
        let connection = connect_initiator(
            endpoint,
            availability,
            self.peer,
            local_identity,
            client_config,
            server_name,
            attempts,
        )
        .await?;
        self.connection = Some(connection);
        Ok(())
    }

    pub async fn reconnect_responder(
        &mut self,
        endpoint: &DirectTransport,
        availability: LocalTransportAvailability,
        local_identity: &IdentityKeypair,
    ) -> Result<(), LocalMeshSessionError> {
        self.cancel();
        let connection =
            accept_responder(endpoint, availability, self.peer, local_identity).await?;
        self.connection = Some(connection);
        Ok(())
    }

    pub fn handoff_for(
        &mut self,
        expected_transport: LocalMeshTransportKind,
    ) -> Result<AuthenticatedDirectHandoff, LocalMeshSessionError> {
        if self.peer.transport() != expected_transport {
            return Err(LocalMeshSessionError::TransportMismatch {
                actual: self.peer.transport(),
                expected: expected_transport,
            });
        }
        let connection = self
            .connection
            .take()
            .ok_or(LocalMeshSessionError::Inactive)?;
        Ok(AuthenticatedDirectHandoff {
            peer: self.peer,
            connection: Some(connection),
        })
    }
}

impl Drop for AuthenticatedLocalMeshSession {
    fn drop(&mut self) {
        self.cancel();
    }
}

pub struct AuthenticatedDirectHandoff {
    peer: LocalMeshPeer,
    connection: Option<DirectConnection>,
}

impl AuthenticatedDirectHandoff {
    #[must_use]
    pub const fn peer(&self) -> LocalMeshPeer {
        self.peer
    }

    pub fn into_connection_for(
        mut self,
        expected_transport: LocalMeshTransportKind,
    ) -> Result<DirectConnection, LocalMeshSessionError> {
        if self.peer.transport() != expected_transport {
            return Err(LocalMeshSessionError::TransportMismatch {
                actual: self.peer.transport(),
                expected: expected_transport,
            });
        }
        self.connection
            .take()
            .ok_or(LocalMeshSessionError::Inactive)
    }

    pub fn cancel(&mut self) {
        if let Some(connection) = self.connection.take() {
            connection.close();
        }
    }
}

impl Drop for AuthenticatedDirectHandoff {
    fn drop(&mut self) {
        self.cancel();
    }
}

#[derive(Debug, thiserror::Error)]
pub enum LocalMeshSessionError {
    #[error("local-mesh transport is unavailable: {0}")]
    Unavailable(#[source] LocalTransportAvailabilityError),
    #[error("Bluetooth peers cannot hand off to a direct connection")]
    MissingDirectProfile,
    #[error("authenticated local-mesh session is inactive")]
    Inactive,
    #[error("authenticated local-mesh route changed from {actual:?} to {expected:?}")]
    TransportMismatch {
        actual: LocalMeshTransportKind,
        expected: LocalMeshTransportKind,
    },
    #[error("authenticated direct connection failed: {0}")]
    Direct(#[source] DirectTransportError),
}

async fn connect_initiator(
    endpoint: &DirectTransport,
    availability: LocalTransportAvailability,
    peer: LocalMeshPeer,
    local_identity: &IdentityKeypair,
    client_config: ClientConfig,
    server_name: &str,
    attempts: DirectConnectionAttempts,
) -> Result<DirectConnection, LocalMeshSessionError> {
    require_direct_peer(availability, peer)?;
    let profile = peer
        .direct_profile()
        .ok_or(LocalMeshSessionError::MissingDirectProfile)?;
    let connection = endpoint
        .connect_with_attempts(profile, client_config, server_name, attempts)
        .await
        .map_err(LocalMeshSessionError::Direct)?;
    authenticate_initiator(connection, local_identity, peer).await
}

async fn accept_responder(
    endpoint: &DirectTransport,
    availability: LocalTransportAvailability,
    peer: LocalMeshPeer,
    local_identity: &IdentityKeypair,
) -> Result<DirectConnection, LocalMeshSessionError> {
    require_direct_peer(availability, peer)?;
    let connection = endpoint
        .accept()
        .await
        .map_err(LocalMeshSessionError::Direct)?;
    authenticate_responder(connection, local_identity, peer).await
}

fn require_direct_peer(
    availability: LocalTransportAvailability,
    peer: LocalMeshPeer,
) -> Result<(), LocalMeshSessionError> {
    availability
        .require_available(peer.transport())
        .map_err(LocalMeshSessionError::Unavailable)?;
    peer.direct_profile()
        .ok_or(LocalMeshSessionError::MissingDirectProfile)?;
    Ok(())
}

async fn authenticate_initiator(
    connection: DirectConnection,
    local_identity: &IdentityKeypair,
    peer: LocalMeshPeer,
) -> Result<DirectConnection, LocalMeshSessionError> {
    if let Err(error) = connection
        .authenticate_initiator(local_identity, &peer.identity())
        .await
    {
        connection.close();
        return Err(LocalMeshSessionError::Direct(error));
    }
    Ok(connection)
}

async fn authenticate_responder(
    connection: DirectConnection,
    local_identity: &IdentityKeypair,
    peer: LocalMeshPeer,
) -> Result<DirectConnection, LocalMeshSessionError> {
    if let Err(error) = connection
        .authenticate_responder(local_identity, &peer.identity())
        .await
    {
        connection.close();
        return Err(LocalMeshSessionError::Direct(error));
    }
    Ok(connection)
}
