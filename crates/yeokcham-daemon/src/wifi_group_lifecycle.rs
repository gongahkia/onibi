use std::{error::Error, future::Future};

use yeokcham_protocol::{
    DirectProfileConfig, WifiGroupBootstrap, WifiGroupConfigurationError, WifiGroupHandoff,
};

pub trait WifiGroupLifecycle: Send + 'static {
    type Error: Error + Send + Sync + 'static;

    fn activate_owner(
        &mut self,
        bootstrap: &WifiGroupBootstrap,
    ) -> impl Future<Output = Result<DirectProfileConfig, Self::Error>> + Send;

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
}

impl<B> ManagedWifiGroup<B>
where
    B: WifiGroupLifecycle,
{
    pub async fn activate_owner(
        mut backend: B,
        bootstrap: &WifiGroupBootstrap,
    ) -> Result<(Self, WifiGroupHandoff), WifiGroupActivationError<B::Error>> {
        let direct_profile = backend
            .activate_owner(bootstrap)
            .await
            .map_err(WifiGroupActivationError::Backend)?;
        let handoff = bootstrap
            .activate(direct_profile)
            .map_err(WifiGroupActivationError::Bootstrap)?;
        Ok((
            Self {
                backend: Some(backend),
                active: true,
            },
            handoff,
        ))
    }

    pub async fn join_client(mut backend: B, handoff: &WifiGroupHandoff) -> Result<Self, B::Error> {
        backend.join_client(handoff).await?;
        Ok(Self {
            backend: Some(backend),
            active: true,
        })
    }

    #[must_use]
    pub const fn is_active(&self) -> bool {
        self.active
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

#[derive(Debug, thiserror::Error)]
pub enum WifiGroupActivationError<E>
where
    E: Error + Send + Sync + 'static,
{
    #[error("Wi-Fi group backend activation failed: {0}")]
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

    use super::{ManagedWifiGroup, WifiGroupLifecycle};

    #[derive(Clone)]
    struct TestGroupBackend(Arc<Mutex<Vec<&'static str>>>);

    impl WifiGroupLifecycle for TestGroupBackend {
        type Error = std::convert::Infallible;

        fn activate_owner(
            &mut self,
            _: &WifiGroupBootstrap,
        ) -> impl std::future::Future<Output = Result<DirectProfileConfig, Self::Error>> + Send
        {
            self.0.lock().unwrap().push("activate_owner");
            std::future::ready(Ok(DirectProfileConfig::new(
                "192.0.2.1:4444".parse().unwrap(),
            )
            .unwrap()))
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

    #[tokio::test]
    async fn derives_the_handoff_after_owner_activation_and_tears_down_once() {
        let events = Arc::new(Mutex::new(Vec::new()));
        let backend = TestGroupBackend(Arc::clone(&events));
        let (mut group, handoff) = ManagedWifiGroup::activate_owner(backend, &bootstrap())
            .await
            .unwrap();

        assert!(group.is_active());
        assert_eq!(
            handoff.configuration().direct_profile().endpoint(),
            "192.0.2.1:4444".parse().unwrap()
        );
        group.cancel().await.unwrap();
        assert!(!group.is_active());
        group.cancel().await.unwrap();
        assert_eq!(
            events.lock().unwrap().as_slice(),
            ["activate_owner", "teardown"]
        );
    }

    #[tokio::test]
    async fn joins_and_explicitly_shuts_down_client_groups() {
        let events = Arc::new(Mutex::new(Vec::new()));
        let owner_backend = TestGroupBackend(Arc::clone(&events));
        let (_, handoff) = ManagedWifiGroup::activate_owner(owner_backend, &bootstrap())
            .await
            .unwrap();
        let client_backend = TestGroupBackend(Arc::clone(&events));
        let group = ManagedWifiGroup::join_client(client_backend, &handoff)
            .await
            .unwrap();
        let _backend = group.shutdown().await.unwrap();

        assert_eq!(
            events.lock().unwrap().as_slice(),
            ["activate_owner", "join_client", "teardown"]
        );
    }
}
