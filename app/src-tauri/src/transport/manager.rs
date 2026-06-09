use super::{
    cloudflared::CloudflareTunnel, lan::LanTransport, tailscale::TailscaleFunnel, Transport,
    TransportHandle, TransportStatus,
};
use crate::protocol::ClientScope;
use anyhow::{anyhow, Result};
use serde::Serialize;
use std::{collections::HashMap, sync::Arc};
use tokio::sync::RwLock;
use ts_rs::TS;

const LOOPBACK_NAME: &str = "loopback";

pub fn default_transport_names() -> [&'static str; 3] {
    ["tailscale-funnel", "cloudflared", "lan"]
}

#[derive(Clone)]
pub struct TransportManager {
    local_port: u16,
    machine_id: String,
    token: String,
    vapid_public_key: String,
    transports: Arc<Vec<Arc<dyn Transport>>>,
    handles: Arc<RwLock<HashMap<String, TransportHandle>>>,
}

impl TransportManager {
    pub fn new(
        local_port: u16,
        machine_id: String,
        token: String,
        vapid_public_key: String,
    ) -> Self {
        Self::with_transports(
            local_port,
            machine_id,
            token,
            vapid_public_key,
            vec![
                Arc::new(TailscaleFunnel::default()),
                Arc::new(CloudflareTunnel::default()),
                Arc::new(LanTransport),
            ],
        )
    }

    pub fn with_transports(
        local_port: u16,
        machine_id: String,
        token: String,
        vapid_public_key: String,
        transports: Vec<Arc<dyn Transport>>,
    ) -> Self {
        Self {
            local_port,
            machine_id,
            token,
            vapid_public_key,
            transports: Arc::new(transports),
            handles: Arc::new(RwLock::new(HashMap::new())),
        }
    }

    pub async fn enable(&self, name: &str) -> Result<TransportSnapshot> {
        if let Some(snapshot) = self.running_snapshot(name).await {
            return Ok(snapshot);
        }

        let transport = self
            .transport(name)
            .ok_or_else(|| anyhow!("unknown transport: {name}"))?;
        let handle = transport.start(self.local_port).await?;
        let snapshot = snapshot_from_handle(transport.as_ref(), &handle);
        self.handles.write().await.insert(name.to_string(), handle);
        Ok(snapshot)
    }

    pub async fn disable(&self, name: &str) -> Result<()> {
        let mut handles = self.handles.write().await;
        let Some(handle) = handles.remove(name) else {
            return Ok(());
        };
        let _ = handle.shutdown.send(());
        Ok(())
    }

    pub async fn status_snapshot(&self) -> Vec<TransportSnapshot> {
        let handles = self.handles.read().await;
        let mut snapshots = Vec::with_capacity(self.transports.len());
        for transport in self.transports.iter() {
            if let Some(handle) = handles.get(transport.name()) {
                snapshots.push(snapshot_from_handle(transport.as_ref(), handle));
            } else {
                snapshots.push(TransportSnapshot {
                    name: transport.name().to_string(),
                    label: transport.label().to_string(),
                    requires_external_dep: transport.requires_external_dep().map(str::to_string),
                    enabled: false,
                    status: transport.status().await,
                    url: None,
                    fingerprint: None,
                });
            }
        }
        snapshots
    }

    pub async fn pairing_payload(&self) -> PairingPayload {
        let handles = self.handles.read().await;
        let mut transports = vec![TransportEndpoint {
            name: LOOPBACK_NAME.to_string(),
            url: format!("http://127.0.0.1:{}/", self.local_port),
            fingerprint: None,
        }];

        for transport in self.transports.iter() {
            if let Some(handle) = handles.get(transport.name()) {
                if !handle.is_alive() {
                    continue;
                }
                if let Some(url) = handle.public_url.clone() {
                    transports.push(TransportEndpoint {
                        name: transport.name().to_string(),
                        url,
                        fingerprint: handle.fingerprint.clone(),
                    });
                }
            }
        }

        PairingPayload {
            protocol_version: crate::protocol::PROTOCOL_VERSION.to_string(),
            machine_id: self.machine_id.clone(),
            host: "127.0.0.1".to_string(),
            port: self.local_port,
            token: self.token.clone(),
            scope: ClientScope::Full,
            vapid_public_key: self.vapid_public_key.clone(),
            cert_fingerprint: None,
            transports,
        }
    }

    fn transport(&self, name: &str) -> Option<Arc<dyn Transport>> {
        self.transports
            .iter()
            .find(|transport| transport.name() == name)
            .cloned()
    }

    async fn running_snapshot(&self, name: &str) -> Option<TransportSnapshot> {
        let transport = self.transport(name)?;
        let handles = self.handles.read().await;
        handles
            .get(name)
            .map(|handle| snapshot_from_handle(transport.as_ref(), handle))
    }
}

fn snapshot_from_handle(transport: &dyn Transport, handle: &TransportHandle) -> TransportSnapshot {
    if !handle.is_alive() {
        return TransportSnapshot {
            name: transport.name().to_string(),
            label: transport.label().to_string(),
            requires_external_dep: transport.requires_external_dep().map(str::to_string),
            enabled: false,
            status: TransportStatus::failed("transport process exited"),
            url: handle.public_url.clone(),
            fingerprint: handle.fingerprint.clone(),
        };
    }

    TransportSnapshot {
        name: transport.name().to_string(),
        label: transport.label().to_string(),
        requires_external_dep: transport.requires_external_dep().map(str::to_string),
        enabled: true,
        status: TransportStatus::Running {
            url: handle.public_url.clone(),
            fingerprint: handle.fingerprint.clone(),
        },
        url: handle.public_url.clone(),
        fingerprint: handle.fingerprint.clone(),
    }
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq, TS)]
#[serde(rename_all = "camelCase")]
pub struct TransportSnapshot {
    pub name: String,
    pub label: String,
    pub requires_external_dep: Option<String>,
    pub enabled: bool,
    pub status: TransportStatus,
    pub url: Option<String>,
    pub fingerprint: Option<String>,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct PairingPayload {
    pub protocol_version: String,
    pub machine_id: String,
    pub host: String,
    pub port: u16,
    pub token: String,
    pub scope: ClientScope,
    pub vapid_public_key: String,
    pub cert_fingerprint: Option<String>,
    pub transports: Vec<TransportEndpoint>,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct TransportEndpoint {
    pub name: String,
    pub url: String,
    pub fingerprint: Option<String>,
}

#[cfg(test)]
mod tests {
    use super::*;
    use anyhow::Result;
    use tokio::sync::oneshot;

    struct MockTransport {
        name: &'static str,
        url: &'static str,
    }

    #[async_trait::async_trait]
    impl Transport for MockTransport {
        fn name(&self) -> &'static str {
            self.name
        }

        fn label(&self) -> &'static str {
            self.name
        }

        fn requires_external_dep(&self) -> Option<&'static str> {
            None
        }

        async fn start(&self, _local_port: u16) -> Result<TransportHandle> {
            let (shutdown, _rx) = oneshot::channel();
            Ok(TransportHandle::new(
                Some(self.url.to_string()),
                None,
                shutdown,
            ))
        }

        async fn status(&self) -> TransportStatus {
            TransportStatus::Stopped
        }
    }

    #[tokio::test]
    async fn aggregate_pairing() {
        let manager = TransportManager::with_transports(
            17893,
            "machine".to_string(),
            "token".to_string(),
            "vapid".to_string(),
            vec![
                Arc::new(MockTransport {
                    name: "tailscale-funnel",
                    url: "https://host.tailnet.ts.net/",
                }),
                Arc::new(MockTransport {
                    name: "cloudflared",
                    url: "https://random.trycloudflare.com/",
                }),
            ],
        );

        manager.enable("tailscale-funnel").await.unwrap();
        manager.enable("cloudflared").await.unwrap();
        let payload = manager.pairing_payload().await;

        assert_eq!(payload.machine_id, "machine");
        assert_eq!(payload.transports.len(), 3);
        assert_eq!(payload.transports[0].name, "loopback");
        assert!(payload
            .transports
            .iter()
            .any(|transport| transport.url == "https://host.tailnet.ts.net/"));
        assert!(payload
            .transports
            .iter()
            .any(|transport| transport.url == "https://random.trycloudflare.com/"));
    }
}
