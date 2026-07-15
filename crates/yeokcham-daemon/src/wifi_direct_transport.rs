use std::future::Future;

use yeokcham_protocol::{LocalMeshTransportKind, WireEnvelope, WireLimits};

use crate::{DirectConnection, DirectTransportError, LocalTransport};

pub struct WifiDirectTransport {
    connection: DirectConnection,
}

impl WifiDirectTransport {
    #[must_use]
    pub const fn new(connection: DirectConnection) -> Self {
        Self { connection }
    }

    #[must_use]
    pub const fn connection(&self) -> &DirectConnection {
        &self.connection
    }

    #[must_use]
    pub fn into_connection(self) -> DirectConnection {
        self.connection
    }
}

impl LocalTransport for WifiDirectTransport {
    type Error = DirectTransportError;

    fn transport_kind(&self) -> LocalMeshTransportKind {
        LocalMeshTransportKind::WifiDirect
    }

    fn send_frame(
        &self,
        frame: &WireEnvelope,
        limits: WireLimits,
    ) -> impl Future<Output = Result<(), Self::Error>> + Send {
        self.connection.send_frame(frame, limits)
    }

    fn receive_frame(
        &self,
        limits: WireLimits,
    ) -> impl Future<Output = Result<WireEnvelope, Self::Error>> + Send {
        self.connection.receive_frame(limits)
    }
}
