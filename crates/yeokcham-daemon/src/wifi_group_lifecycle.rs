use yeokcham_protocol::WifiGroupHandoff;

pub trait WifiGroupLifecycle: Send + 'static {
    type Error: Send + 'static;

    fn establish(&mut self, handoff: &WifiGroupHandoff) -> Result<(), Self::Error>;

    fn teardown(&mut self) -> Result<(), Self::Error>;
}

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
    pub fn establish(mut backend: B, handoff: &WifiGroupHandoff) -> Result<Self, B::Error> {
        backend.establish(handoff)?;
        Ok(Self {
            backend: Some(backend),
            active: true,
        })
    }

    #[must_use]
    pub const fn is_active(&self) -> bool {
        self.active
    }

    pub fn cancel(&mut self) -> Result<(), B::Error> {
        if self.active {
            self.backend
                .as_mut()
                .expect("active Wi-Fi group always has a backend")
                .teardown()?;
            self.active = false;
        }
        Ok(())
    }

    pub fn shutdown(mut self) -> Result<B, B::Error> {
        self.cancel()?;
        Ok(self
            .backend
            .take()
            .expect("managed Wi-Fi group always has a backend"))
    }
}

impl<B> Drop for ManagedWifiGroup<B>
where
    B: WifiGroupLifecycle,
{
    fn drop(&mut self) {
        let _ = self.cancel();
    }
}

#[cfg(test)]
mod tests {
    use std::sync::{Arc, Mutex};

    use yeokcham_protocol::{
        DirectProfileConfig, LocalMeshTransportKind, WifiGroupConfiguration, WifiGroupCredential,
        WifiGroupHandoff, WifiGroupRole,
    };

    use super::{ManagedWifiGroup, WifiGroupLifecycle};

    #[derive(Clone)]
    struct TestGroupBackend(Arc<Mutex<Vec<&'static str>>>);

    impl WifiGroupLifecycle for TestGroupBackend {
        type Error = ();

        fn establish(&mut self, _: &WifiGroupHandoff) -> Result<(), Self::Error> {
            self.0.lock().unwrap().push("establish");
            Ok(())
        }

        fn teardown(&mut self) -> Result<(), Self::Error> {
            self.0.lock().unwrap().push("teardown");
            Ok(())
        }
    }

    fn handoff() -> WifiGroupHandoff {
        WifiGroupHandoff::new(
            WifiGroupConfiguration::new(
                LocalMeshTransportKind::WifiDirect,
                WifiGroupRole::Owner,
                "yeokcham-p2p".to_owned(),
                DirectProfileConfig::new("192.0.2.1:4444".parse().unwrap()).unwrap(),
            )
            .unwrap(),
            WifiGroupCredential::new(b"group-secret".to_vec()).unwrap(),
        )
    }

    #[test]
    fn owns_wifi_group_teardown_across_cancel_and_drop() {
        let events = Arc::new(Mutex::new(Vec::new()));
        let backend = TestGroupBackend(Arc::clone(&events));
        let mut group = ManagedWifiGroup::establish(backend, &handoff()).unwrap();
        assert!(group.is_active());
        group.cancel().unwrap();
        assert!(!group.is_active());
        drop(group);
        assert_eq!(events.lock().unwrap().as_slice(), ["establish", "teardown"]);
    }
}
