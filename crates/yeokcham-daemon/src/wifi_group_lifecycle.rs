use std::{error::Error, future::Future, net::SocketAddr};

use yeokcham_protocol::{
    DirectProfileConfig, WifiGroupBootstrap, WifiGroupConfigurationError, WifiGroupHandoff,
};

pub trait WifiGroupLifecycle: Send + 'static {
    type Error: Error + Send + Sync + 'static;

    fn activate_owner(
        &mut self,
        bootstrap: &WifiGroupBootstrap,
    ) -> impl Future<Output = Result<(), Self::Error>> + Send;

    fn validate_owner_endpoint(&self, endpoint: SocketAddr) -> Result<(), Self::Error>;

    fn join_client(
        &mut self,
        handoff: &WifiGroupHandoff,
    ) -> impl Future<Output = Result<(), Self::Error>> + Send;

    fn teardown(&mut self) -> impl Future<Output = Result<(), Self::Error>> + Send;
}

#[must_use = "call cancel or shutdown to tear down the active Wi-Fi group"]
pub struct ManagedWifiGroup<B>
where
    B: WifiGroupLifecycle,
{
    backend: Option<B>,
    active: bool,
    owner: bool,
}

impl<B> ManagedWifiGroup<B>
where
    B: WifiGroupLifecycle,
{
    pub async fn activate_owner(
        mut backend: B,
        bootstrap: &WifiGroupBootstrap,
    ) -> Result<Self, WifiGroupActivationError<B::Error>> {
        backend
            .activate_owner(bootstrap)
            .await
            .map_err(WifiGroupActivationError::Backend)?;
        Ok(Self {
            backend: Some(backend),
            active: true,
            owner: true,
        })
    }

    pub async fn join_client(mut backend: B, handoff: &WifiGroupHandoff) -> Result<Self, B::Error> {
        backend.join_client(handoff).await?;
        Ok(Self {
            backend: Some(backend),
            active: true,
            owner: false,
        })
    }

    #[must_use]
    pub const fn is_active(&self) -> bool {
        self.active
    }

    pub fn backend_mut(&mut self) -> &mut B {
        self.backend
            .as_mut()
            .expect("managed Wi-Fi group always has a backend")
    }

    pub fn owner_handoff(
        &self,
        bootstrap: &WifiGroupBootstrap,
        direct_profile: DirectProfileConfig,
    ) -> Result<WifiGroupHandoff, WifiGroupHandoffError<B::Error>> {
        if !self.active {
            return Err(WifiGroupHandoffError::Inactive);
        }
        if !self.owner {
            return Err(WifiGroupHandoffError::ClientGroup);
        }
        let endpoint = direct_profile.endpoint();
        self.backend
            .as_ref()
            .expect("active Wi-Fi group always has a backend")
            .validate_owner_endpoint(endpoint)
            .map_err(WifiGroupHandoffError::Backend)?;
        bootstrap
            .activate(direct_profile)
            .map_err(WifiGroupHandoffError::Bootstrap)
    }

    pub async fn cancel(&mut self) -> Result<(), B::Error> {
        if self.active {
            self.backend
                .as_mut()
                .expect("active Wi-Fi group always has a backend")
                .teardown()
                .await?;
            self.active = false;
        }
        Ok(())
    }

    pub async fn shutdown(mut self) -> Result<B, B::Error> {
        self.cancel().await?;
        Ok(self
            .backend
            .take()
            .expect("managed Wi-Fi group always has a backend"))
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum WifiGroupActivationError<E>
where
    E: Error + Send + Sync + 'static,
{
    #[error("Wi-Fi group backend activation failed: {0}")]
    Backend(#[source] E),
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum WifiGroupHandoffError<E>
where
    E: Error + Send + Sync + 'static,
{
    #[error("Wi-Fi group is inactive")]
    Inactive,
    #[error("a client Wi-Fi group cannot emit an owner handoff")]
    ClientGroup,
    #[error("direct endpoint does not bind the active Wi-Fi group: {0}")]
    Backend(#[source] E),
    #[error("Wi-Fi group bootstrap is invalid: {0}")]
    Bootstrap(#[source] WifiGroupConfigurationError),
}

#[cfg(test)]
mod tests {
    use std::sync::{Arc, Mutex};

    use yeokcham_protocol::{
        DirectProfileConfig, LocalMeshTransportKind, WifiGroupBootstrap, WifiGroupCredential,
        WifiGroupHandoff,
    };

    use super::{ManagedWifiGroup, WifiGroupHandoffError, WifiGroupLifecycle};

    #[derive(Clone)]
    struct TestGroupBackend(Arc<Mutex<Vec<&'static str>>>);

    #[derive(Debug, Eq, PartialEq, thiserror::Error)]
    enum TestGroupError {
        #[error("direct endpoint is not bound to the group")]
        Endpoint,
    }

    impl WifiGroupLifecycle for TestGroupBackend {
        type Error = TestGroupError;

        fn activate_owner(
            &mut self,
            _: &WifiGroupBootstrap,
        ) -> impl std::future::Future<Output = Result<(), Self::Error>> + Send {
            self.0.lock().unwrap().push("activate_owner");
            std::future::ready(Ok(()))
        }

        fn validate_owner_endpoint(
            &self,
            endpoint: std::net::SocketAddr,
        ) -> Result<(), Self::Error> {
            if endpoint.ip() == "192.0.2.1".parse::<std::net::IpAddr>().unwrap() {
                Ok(())
            } else {
                Err(TestGroupError::Endpoint)
            }
        }

        fn join_client(
            &mut self,
            _: &WifiGroupHandoff,
        ) -> impl std::future::Future<Output = Result<(), Self::Error>> + Send {
            self.0.lock().unwrap().push("join_client");
            std::future::ready(Ok(()))
        }

        fn teardown(
            &mut self,
        ) -> impl std::future::Future<Output = Result<(), Self::Error>> + Send {
            self.0.lock().unwrap().push("teardown");
            std::future::ready(Ok(()))
        }
    }

    fn bootstrap() -> WifiGroupBootstrap {
        WifiGroupBootstrap::new(
            LocalMeshTransportKind::WifiDirect,
            "yeokcham-p2p".to_owned(),
            WifiGroupCredential::new(b"group-secret".to_vec()).unwrap(),
        )
        .unwrap()
    }

    fn profile(address: &str) -> DirectProfileConfig {
        DirectProfileConfig::new(address.parse().unwrap()).unwrap()
    }

    #[tokio::test]
    async fn emits_owner_handoff_only_for_the_activated_interface() {
        let events = Arc::new(Mutex::new(Vec::new()));
        let backend = TestGroupBackend(Arc::clone(&events));
        let mut group = ManagedWifiGroup::activate_owner(backend, &bootstrap())
            .await
            .unwrap();
        let handoff = group
            .owner_handoff(&bootstrap(), profile("192.0.2.1:4444"))
            .unwrap();

        assert!(group.is_active());
        assert_eq!(
            handoff.configuration().direct_profile(),
            profile("192.0.2.1:4444")
        );
        assert_eq!(
            group.owner_handoff(&bootstrap(), profile("192.0.2.2:4444")),
            Err(WifiGroupHandoffError::Backend(TestGroupError::Endpoint))
        );
        group.cancel().await.unwrap();
        assert!(!group.is_active());
        assert_eq!(
            events.lock().unwrap().as_slice(),
            ["activate_owner", "teardown"]
        );
    }

    #[tokio::test]
    async fn joins_and_explicitly_shuts_down_client_groups() {
        let events = Arc::new(Mutex::new(Vec::new()));
        let owner_backend = TestGroupBackend(Arc::clone(&events));
        let owner = ManagedWifiGroup::activate_owner(owner_backend, &bootstrap())
            .await
            .unwrap();
        let handoff = owner
            .owner_handoff(&bootstrap(), profile("192.0.2.1:4444"))
            .unwrap();
        let client_backend = TestGroupBackend(Arc::clone(&events));
        let group = ManagedWifiGroup::join_client(client_backend, &handoff)
            .await
            .unwrap();
        assert_eq!(
            group.owner_handoff(&bootstrap(), profile("192.0.2.1:4444")),
            Err(WifiGroupHandoffError::ClientGroup)
        );
        let _backend = group.shutdown().await.unwrap();

        assert_eq!(
            events.lock().unwrap().as_slice(),
            ["activate_owner", "join_client", "teardown"]
        );
    }
}
