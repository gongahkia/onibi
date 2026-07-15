#[allow(dead_code)]
mod local_transport;

#[allow(unused_imports)]
pub use local_transport::{
    InMemoryTransport, InMemoryTransportError, assert_transport_conformance,
};
