use std::future::Future;

use arachne_protocol::{LocalMeshTransportKind, WireEnvelope, WireLimits};

use crate::{
    AuthenticatedDirectHandoff, DirectConnection, DirectTransportError, LocalMeshSessionError,
    LocalTransport,
};

pub struct LanDirectTransport {
    connection: Option<DirectConnection>,
}

impl LanDirectTransport {
    #[must_use]
    const fn new(connection: DirectConnection) -> Self {
        Self {
            connection: Some(connection),
        }
    }

    pub fn from_authenticated_handoff(
        handoff: AuthenticatedDirectHandoff,
    ) -> Result<Self, LocalMeshSessionError> {
        Ok(Self::new(
            handoff.into_connection_for(LocalMeshTransportKind::Lan)?,
        ))
    }

    #[must_use]
    pub fn connection(&self) -> &DirectConnection {
        self.connection
            .as_ref()
            .unwrap_or_else(|| unreachable!("direct connection was transferred"))
    }

    #[must_use]
    pub fn into_connection(mut self) -> DirectConnection {
        self.connection
            .take()
            .unwrap_or_else(|| unreachable!("direct connection was transferred"))
    }
}

impl Drop for LanDirectTransport {
    fn drop(&mut self) {
        if let Some(connection) = self.connection.take() {
            connection.close();
        }
    }
}

impl LocalTransport for LanDirectTransport {
    type Error = DirectTransportError;

    fn transport_kind(&self) -> LocalMeshTransportKind {
        LocalMeshTransportKind::Lan
    }

    fn send_frame(
        &self,
        frame: &WireEnvelope,
        limits: WireLimits,
    ) -> impl Future<Output = Result<(), Self::Error>> + Send {
        self.connection().send_frame(frame, limits)
    }

    fn receive_frame(
        &self,
        limits: WireLimits,
    ) -> impl Future<Output = Result<WireEnvelope, Self::Error>> + Send {
        self.connection().receive_frame(limits)
    }
}
