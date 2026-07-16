use std::sync::{
    Arc,
    atomic::{AtomicBool, Ordering},
};

use tonic::{Request, Response, Status};
use yeokcham_daemon_api::v1::{
    GetStatusRequest, GetStatusResponse, StartClientRequest, StartClientResponse,
    daemon_service_server::DaemonService,
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

    fn status_response(&self) -> (u32, u32, bool) {
        (
            u32::from(self.version.get()),
            0,
            self.running.load(Ordering::Acquire),
        )
    }
}

#[tonic::async_trait]
impl DaemonService for DaemonGrpcService {
    async fn start_client(
        &self,
        _request: Request<StartClientRequest>,
    ) -> Result<Response<StartClientResponse>, Status> {
        let (api_major, api_minor, running) = self.status_response();
        Ok(Response::new(StartClientResponse {
            api_major,
            api_minor,
            running,
        }))
    }

    async fn get_status(
        &self,
        _request: Request<GetStatusRequest>,
    ) -> Result<Response<GetStatusResponse>, Status> {
        let (api_major, api_minor, running) = self.status_response();
        Ok(Response::new(GetStatusResponse {
            api_major,
            api_minor,
            running,
        }))
    }
}

#[cfg(test)]
mod tests {
    use tonic::Request;
    use yeokcham_daemon_api::v1::{
        GetStatusRequest, StartClientRequest, daemon_service_server::DaemonService,
    };
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

    #[tokio::test]
    async fn client_start_rpc_is_idempotent_and_reports_lifecycle() {
        let service = DaemonGrpcService::new(ProtocolVersion::INITIAL);
        for _ in 0..2 {
            let response = service
                .start_client(Request::new(StartClientRequest {}))
                .await
                .unwrap()
                .into_inner();
            assert_eq!(
                response.api_major,
                u32::from(ProtocolVersion::INITIAL.get())
            );
            assert_eq!(response.api_minor, 0);
            assert!(response.running);
        }
        service.shutdown();
        assert!(
            !service
                .start_client(Request::new(StartClientRequest {}))
                .await
                .unwrap()
                .into_inner()
                .running
        );
    }
}
