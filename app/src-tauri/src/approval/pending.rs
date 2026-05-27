use crate::protocol::ApprovalDecisionResponse;
use std::{collections::HashMap, sync::Arc};
use tokio::sync::{oneshot, Mutex};

#[derive(Debug, Default, Clone)]
pub struct PendingApprovals {
    waiters: Arc<Mutex<HashMap<String, oneshot::Sender<ApprovalDecisionResponse>>>>,
}

impl PendingApprovals {
    pub async fn insert(
        &self,
        approval_id: String,
    ) -> oneshot::Receiver<ApprovalDecisionResponse> {
        let (tx, rx) = oneshot::channel();
        self.waiters.lock().await.insert(approval_id, tx);
        rx
    }

    pub async fn resolve(&self, approval_id: &str, response: ApprovalDecisionResponse) -> bool {
        if let Some(tx) = self.waiters.lock().await.remove(approval_id) {
            let _ = tx.send(response);
            true
        } else {
            false
        }
    }

    pub async fn remove(&self, approval_id: &str) {
        self.waiters.lock().await.remove(approval_id);
    }
}
