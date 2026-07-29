use arachne_protocol::LocalMeshTransportKind;

use crate::LocalTransport;

pub trait BluetoothTransport: LocalTransport {
    fn verify_bluetooth_kind(&self) -> Result<(), BluetoothTransportError> {
        if self.transport_kind() == LocalMeshTransportKind::Bluetooth {
            Ok(())
        } else {
            Err(BluetoothTransportError::NonBluetoothTransport)
        }
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum BluetoothTransportError {
    #[error("Bluetooth transport must use the Bluetooth local-mesh profile")]
    NonBluetoothTransport,
}

#[cfg(test)]
mod tests {
    use std::{
        convert::Infallible,
        future::{Future, ready},
    };

    use arachne_protocol::{EnvelopeKind, ProtocolVersion, WireEnvelope, WireLimits};

    use super::{BluetoothTransport, BluetoothTransportError, LocalMeshTransportKind};
    use crate::LocalTransport;

    struct TestTransport(LocalMeshTransportKind);

    impl LocalTransport for TestTransport {
        type Error = Infallible;

        fn transport_kind(&self) -> LocalMeshTransportKind {
            self.0
        }

        fn send_frame(
            &self,
            _: &WireEnvelope,
            _: WireLimits,
        ) -> impl Future<Output = Result<(), Self::Error>> + Send {
            ready(Ok(()))
        }

        fn receive_frame(
            &self,
            _: WireLimits,
        ) -> impl Future<Output = Result<WireEnvelope, Self::Error>> + Send {
            ready(Ok(WireEnvelope {
                version: ProtocolVersion::INITIAL,
                kind: EnvelopeKind::EncryptedMessage,
                payload: Vec::new(),
            }))
        }
    }

    impl BluetoothTransport for TestTransport {}

    #[test]
    fn bluetooth_transport_requires_the_bluetooth_profile() {
        assert!(
            TestTransport(LocalMeshTransportKind::Bluetooth)
                .verify_bluetooth_kind()
                .is_ok()
        );
        assert_eq!(
            TestTransport(LocalMeshTransportKind::Lan)
                .verify_bluetooth_kind()
                .unwrap_err(),
            BluetoothTransportError::NonBluetoothTransport
        );
    }
}
