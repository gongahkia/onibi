use std::sync::{
    Arc,
    atomic::{AtomicBool, Ordering},
};

use tonic::{Request, Response, Status};
use yeokcham_daemon_api::v1::{
    GetStatusRequest, GetStatusResponse, daemon_service_server::DaemonService,
};
use yeokcham_protocol::ProtocolVersion;

#[derive(Clone, Debug)]
pub struct DaemonGrpcService {
    version: ProtocolVersion,
    running: Arc<AtomicBool>,
}

impl DaemonGrpcService {
    #[must_use]
    pub fn new(version: ProtocolVersion) -> Self {
        Self {
            version,
            running: Arc::new(AtomicBool::new(true)),
        }
    }

    pub fn shutdown(&self) {
        self.running.store(false, Ordering::Release);
    }
}

#[tonic::async_trait]
impl DaemonService for DaemonGrpcService {
    async fn get_status(
        &self,
        _request: Request<GetStatusRequest>,
    ) -> Result<Response<GetStatusResponse>, Status> {
        Ok(Response::new(GetStatusResponse {
            api_major: u32::from(self.version.get()),
            api_minor: 0,
            running: self.running.load(Ordering::Acquire),
        }))
    }
}

#[cfg(test)]
mod tests {
    use tonic::Request;
    use yeokcham_daemon_api::v1::{GetStatusRequest, daemon_service_server::DaemonService};
    use yeokcham_protocol::ProtocolVersion;

    use super::DaemonGrpcService;

    #[tokio::test]
    async fn status_rpc_reports_protocol_version_and_lifecycle() {
        let service = DaemonGrpcService::new(ProtocolVersion::INITIAL);
        let running = service
            .get_status(Request::new(GetStatusRequest {}))
            .await
            .unwrap()
            .into_inner();
        assert_eq!(running.api_major, u32::from(ProtocolVersion::INITIAL.get()));
        assert_eq!(running.api_minor, 0);
        assert!(running.running);
        service.shutdown();
        let stopped = service
            .get_status(Request::new(GetStatusRequest {}))
            .await
            .unwrap()
            .into_inner();
        assert!(!stopped.running);
    }
}
