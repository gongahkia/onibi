use crate::{approval::store::ApprovalStore, protocol::Approval, secret::VapidKeys};
use anyhow::{Context, Result};
use parking_lot::RwLock;
use serde::Deserialize;
use serde_json::json;
use std::sync::Arc;
use web_push::{
    ContentEncoding, IsahcWebPushClient, SubscriptionInfo, VapidSignatureBuilder, WebPushClient,
    WebPushMessageBuilder,
};

#[derive(Debug, Deserialize)]
struct BrowserSubscription {
    endpoint: String,
    keys: BrowserSubscriptionKeys,
}

#[derive(Debug, Deserialize)]
struct BrowserSubscriptionKeys {
    p256dh: String,
    auth: String,
}

#[derive(Clone)]
pub struct PushBridge {
    pub store: ApprovalStore,
    pub vapid: VapidKeys,
}

static GLOBAL_BRIDGE: RwLock<Option<Arc<PushBridge>>> = RwLock::new(None);

pub fn register_bridge(store: ApprovalStore, vapid: VapidKeys) {
    *GLOBAL_BRIDGE.write() = Some(Arc::new(PushBridge { store, vapid }));
}

pub fn bridge() -> Option<Arc<PushBridge>> {
    GLOBAL_BRIDGE.read().clone()
}

pub async fn fanout_approval_pending(store: ApprovalStore, vapid: VapidKeys, approval: Approval) {
    let subscriptions = match store.list_push_subscriptions() {
        Ok(subscriptions) => subscriptions,
        Err(error) => {
            tracing::debug!(%error, "failed to load push subscriptions");
            return;
        }
    };

    if subscriptions.is_empty() {
        return;
    }

    let payload = json!({
        "type": "approval-pending",
        "approval_id": approval.approval_id,
        "agent": approval.agent,
        "tool": approval.tool,
        "cwd": approval.cwd,
    });

    for subscription in subscriptions {
        if let Err(error) = send_push(&vapid, subscription, &payload).await {
            tracing::debug!(%error, "web push fanout failed");
        }
    }
}

pub async fn fanout_pty_notification(
    store: ApprovalStore,
    vapid: VapidKeys,
    session_id: String,
    notification: app_lib::pty::OscNotification,
) {
    let subscriptions = match store.list_push_subscriptions() {
        Ok(subscriptions) => subscriptions,
        Err(error) => {
            tracing::debug!(%error, "failed to load push subscriptions for pty notification");
            return;
        }
    };

    if subscriptions.is_empty() {
        return;
    }

    let payload = json!({
        "type": "pty-notification",
        "session_id": session_id,
        "title": notification.title,
        "body": notification.body,
        "urgency": notification.urgency,
        "source": notification.source,
    });

    for subscription in subscriptions {
        if let Err(error) = send_push(&vapid, subscription, &payload).await {
            tracing::debug!(%error, "pty notification fanout failed");
        }
    }
}

async fn send_push(
    vapid: &VapidKeys,
    subscription: serde_json::Value,
    payload: &serde_json::Value,
) -> Result<()> {
    let subscription: BrowserSubscription =
        serde_json::from_value(subscription).context("parse browser push subscription")?;
    let subscription_info = SubscriptionInfo::new(
        subscription.endpoint,
        subscription.keys.p256dh,
        subscription.keys.auth,
    );
    let mut signature =
        VapidSignatureBuilder::from_pem(vapid.private_key.as_bytes(), &subscription_info)
            .context("create VAPID signature")?;
    signature.add_claim("sub", "mailto:security@onibi.sh");
    let signature = signature.build().context("sign VAPID claims")?;

    let mut message = WebPushMessageBuilder::new(&subscription_info);
    let payload = serde_json::to_vec(payload).context("serialize push payload")?;
    message.set_payload(ContentEncoding::Aes128Gcm, &payload);
    message.set_vapid_signature(signature);

    let client = IsahcWebPushClient::new().context("create web push client")?;
    client
        .send(message.build().context("build web push message")?)
        .await
        .context("send web push notification")?;
    Ok(())
}
