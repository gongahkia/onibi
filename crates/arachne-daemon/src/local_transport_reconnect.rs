use std::future::Future;

use arachne_protocol::LocalMeshTransportKind;

use crate::{LocalTransport, LocalTransportAvailability, LocalTransportAvailabilityError};

pub const MAX_LOCAL_TRANSPORT_RECONNECT_ATTEMPTS: u8 = 3;

pub trait LocalTransportConnector: Send + 'static {
    type Error: Send + 'static;
    type Transport: LocalTransport;

    fn connect(&mut self) -> impl Future<Output = Result<Self::Transport, Self::Error>> + Send;
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct LocalTransportReconnectPolicy {
    attempts: u8,
}

impl LocalTransportReconnectPolicy {
    pub fn new(attempts: u8) -> Result<Self, LocalTransportReconnectPolicyError> {
        if attempts == 0 || attempts > MAX_LOCAL_TRANSPORT_RECONNECT_ATTEMPTS {
            return Err(LocalTransportReconnectPolicyError::InvalidAttemptCount);
        }
        Ok(Self { attempts })
    }

    #[must_use]
    pub const fn attempts(self) -> u8 {
        self.attempts
    }

    pub async fn reconnect<C: LocalTransportConnector>(
        self,
        connector: &mut C,
        kind: LocalMeshTransportKind,
        availability: LocalTransportAvailability,
    ) -> Result<C::Transport, LocalTransportReconnectError<C::Error>> {
        availability
            .require_available(kind)
            .map_err(LocalTransportReconnectError::Unavailable)?;
        let mut remaining_attempts = self.attempts;
        loop {
            match connector.connect().await {
                Ok(transport) if transport.transport_kind() == kind => return Ok(transport),
                Ok(transport) => {
                    return Err(LocalTransportReconnectError::TransportKindMismatch {
                        actual: transport.transport_kind(),
                        expected: kind,
                    });
                }
                Err(error) if remaining_attempts == 1 => {
                    return Err(LocalTransportReconnectError::AttemptsExhausted(error));
                }
                Err(_) => remaining_attempts -= 1,
            }
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, thiserror::Error)]
pub enum LocalTransportReconnectPolicyError {
    #[error(
        "local transport reconnect attempts must be between 1 and {MAX_LOCAL_TRANSPORT_RECONNECT_ATTEMPTS}"
    )]
    InvalidAttemptCount,
}

#[derive(Debug)]
pub enum LocalTransportReconnectError<E> {
    Unavailable(LocalTransportAvailabilityError),
    AttemptsExhausted(E),
    TransportKindMismatch {
        actual: LocalMeshTransportKind,
        expected: LocalMeshTransportKind,
    },
}

#[cfg(test)]
mod tests {
    use std::{
        collections::VecDeque,
        convert::Infallible,
        future::{Future, ready},
    };

    use arachne_protocol::{EnvelopeKind, ProtocolVersion, WireEnvelope, WireLimits};

    use super::{
        LocalTransportConnector, LocalTransportReconnectError, LocalTransportReconnectPolicy,
        LocalTransportReconnectPolicyError, MAX_LOCAL_TRANSPORT_RECONNECT_ATTEMPTS,
    };
    use crate::{LocalTransport, LocalTransportAvailability, LocalTransportAvailabilityError};
    use arachne_protocol::LocalMeshTransportKind;

    #[derive(Debug, Eq, PartialEq)]
    enum ConnectError {
        Failed,
    }

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

    struct TestConnector {
        attempts: usize,
        outcomes: VecDeque<Result<TestTransport, ConnectError>>,
    }

    impl LocalTransportConnector for TestConnector {
        type Error = ConnectError;
        type Transport = TestTransport;

        fn connect(&mut self) -> impl Future<Output = Result<Self::Transport, Self::Error>> + Send {
            self.attempts += 1;
            ready(
                self.outcomes
                    .pop_front()
                    .unwrap_or(Err(ConnectError::Failed)),
            )
        }
    }

    #[tokio::test]
    async fn reconnects_with_bounded_attempts_after_availability_validation() {
        let mut connector = TestConnector {
            attempts: 0,
            outcomes: VecDeque::from([
                Err(ConnectError::Failed),
                Ok(TestTransport(LocalMeshTransportKind::WifiDirect)),
            ]),
        };
        let policy = LocalTransportReconnectPolicy::new(2).unwrap();
        let transport = policy
            .reconnect(
                &mut connector,
                LocalMeshTransportKind::WifiDirect,
                LocalTransportAvailability::Available,
            )
            .await
            .unwrap();
        assert_eq!(connector.attempts, 2);
        assert_eq!(
            transport.transport_kind(),
            LocalMeshTransportKind::WifiDirect
        );
    }

    #[tokio::test]
    async fn rejects_unavailable_or_mismatched_local_transport_reconnects() {
        let mut unavailable_connector = TestConnector {
            attempts: 0,
            outcomes: VecDeque::new(),
        };
        assert!(matches!(
            LocalTransportReconnectPolicy::new(1)
                .unwrap()
                .reconnect(
                    &mut unavailable_connector,
                    LocalMeshTransportKind::Bluetooth,
                    LocalTransportAvailability::PermissionDenied,
                )
                .await,
            Err(LocalTransportReconnectError::Unavailable(
                LocalTransportAvailabilityError::PermissionDenied(
                    LocalMeshTransportKind::Bluetooth
                )
            ))
        ));
        assert_eq!(unavailable_connector.attempts, 0);
        let mut mismatched_connector = TestConnector {
            attempts: 0,
            outcomes: VecDeque::from([Ok(TestTransport(LocalMeshTransportKind::Lan))]),
        };
        assert!(matches!(
            LocalTransportReconnectPolicy::new(1)
                .unwrap()
                .reconnect(
                    &mut mismatched_connector,
                    LocalMeshTransportKind::WifiHotspot,
                    LocalTransportAvailability::Available,
                )
                .await,
            Err(LocalTransportReconnectError::TransportKindMismatch {
                actual: LocalMeshTransportKind::Lan,
                expected: LocalMeshTransportKind::WifiHotspot,
            })
        ));
    }

    #[tokio::test]
    async fn stops_after_the_configured_reconnect_attempts() {
        let mut connector = TestConnector {
            attempts: 0,
            outcomes: VecDeque::from([Err(ConnectError::Failed), Err(ConnectError::Failed)]),
        };
        assert!(matches!(
            LocalTransportReconnectPolicy::new(2)
                .unwrap()
                .reconnect(
                    &mut connector,
                    LocalMeshTransportKind::Lan,
                    LocalTransportAvailability::Available,
                )
                .await,
            Err(LocalTransportReconnectError::AttemptsExhausted(
                ConnectError::Failed
            ))
        ));
        assert_eq!(connector.attempts, 2);
    }

    #[test]
    fn validates_reconnect_attempt_bounds() {
        assert_eq!(
            LocalTransportReconnectPolicy::new(0).unwrap_err(),
            LocalTransportReconnectPolicyError::InvalidAttemptCount
        );
        assert_eq!(
            LocalTransportReconnectPolicy::new(MAX_LOCAL_TRANSPORT_RECONNECT_ATTEMPTS + 1)
                .unwrap_err(),
            LocalTransportReconnectPolicyError::InvalidAttemptCount
        );
    }
}
