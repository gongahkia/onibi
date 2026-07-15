use std::future::Future;

use yeokcham_protocol::{LocalMeshTransportKind, WireEnvelope, WireLimits};

pub trait LocalTransport: Send + Sync + 'static {
    type Error: Send + 'static;

    fn transport_kind(&self) -> LocalMeshTransportKind;

    fn send_frame(
        &self,
        frame: &WireEnvelope,
        limits: WireLimits,
    ) -> impl Future<Output = Result<(), Self::Error>> + Send;

    fn receive_frame(
        &self,
        limits: WireLimits,
    ) -> impl Future<Output = Result<WireEnvelope, Self::Error>> + Send;
}
