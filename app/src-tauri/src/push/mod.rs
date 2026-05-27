use crate::{approval::store::ApprovalStore, protocol::Approval, secret::VapidKeys};
use anyhow::{Context, Result};
use serde::Deserialize;
use serde_json::json;
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
