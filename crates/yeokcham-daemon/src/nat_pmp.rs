use natpmp::{Protocol, Response, new_tokio_natpmp};

pub const MAX_NAT_PMP_LEASE_SECONDS: u32 = 3_600;

pub struct NatPmpMappingRequest {
    internal_port: u16,
    external_port: u16,
    lease_seconds: u32,
}

pub struct NatPmpMapping {
    internal_port: u16,
    external_port: u16,
}

impl NatPmpMappingRequest {
    pub fn new(
        internal_port: u16,
        external_port: u16,
        lease_seconds: u32,
    ) -> Result<Self, NatPmpMappingError> {
        if internal_port == 0 {
            return Err(NatPmpMappingError::ZeroInternalPort);
        }
        if external_port == 0 {
            return Err(NatPmpMappingError::ZeroExternalPort);
        }
        if !(1..=MAX_NAT_PMP_LEASE_SECONDS).contains(&lease_seconds) {
            return Err(NatPmpMappingError::InvalidLease);
        }
        Ok(Self {
            internal_port,
            external_port,
            lease_seconds,
        })
    }

    pub async fn create(self) -> Result<NatPmpMapping, NatPmpMappingError> {
        let client = new_tokio_natpmp()
            .await
            .map_err(NatPmpMappingError::Client)?;
        client
            .send_port_mapping_request(
                Protocol::UDP,
                self.internal_port,
                self.external_port,
                self.lease_seconds,
            )
            .await
            .map_err(NatPmpMappingError::Client)?;
        verify_response(
            client
                .read_response_or_retry()
                .await
                .map_err(NatPmpMappingError::Client)?,
            self.internal_port,
            self.external_port,
            true,
        )?;
        Ok(NatPmpMapping {
            internal_port: self.internal_port,
            external_port: self.external_port,
        })
    }
}

impl NatPmpMapping {
    #[must_use]
    pub const fn external_port(&self) -> u16 {
        self.external_port
    }

    pub async fn remove(&self) -> Result<(), NatPmpMappingError> {
        let client = new_tokio_natpmp()
            .await
            .map_err(NatPmpMappingError::Client)?;
        client
            .send_port_mapping_request(Protocol::UDP, self.internal_port, self.external_port, 0)
            .await
            .map_err(NatPmpMappingError::Client)?;
        verify_response(
            client
                .read_response_or_retry()
                .await
                .map_err(NatPmpMappingError::Client)?,
            self.internal_port,
            self.external_port,
            false,
        )
    }
}

#[derive(Debug, thiserror::Error)]
pub enum NatPmpMappingError {
    #[error("NAT-PMP internal port must be nonzero")]
    ZeroInternalPort,
    #[error("NAT-PMP external port must be nonzero")]
    ZeroExternalPort,
    #[error("NAT-PMP lease must be between one second and {MAX_NAT_PMP_LEASE_SECONDS} seconds")]
    InvalidLease,
    #[error("NAT-PMP client operation failed: {0}")]
    Client(#[source] natpmp::Error),
    #[error("NAT-PMP response is not a UDP mapping response")]
    UnexpectedResponse,
    #[error("NAT-PMP response ports do not match the requested mapping")]
    MismatchedResponse,
    #[error("NAT-PMP response has an invalid mapping lease")]
    InvalidResponseLease,
}

fn verify_response(
    response: Response,
    internal_port: u16,
    external_port: u16,
    mapping: bool,
) -> Result<(), NatPmpMappingError> {
    let Response::UDP(response) = response else {
        return Err(NatPmpMappingError::UnexpectedResponse);
    };
    if response.private_port() != internal_port || response.public_port() != external_port {
        return Err(NatPmpMappingError::MismatchedResponse);
    }
    let lease_seconds = response.lifetime().as_secs();
    if (mapping && !(1..=u64::from(MAX_NAT_PMP_LEASE_SECONDS)).contains(&lease_seconds))
        || (!mapping && lease_seconds != 0)
    {
        return Err(NatPmpMappingError::InvalidResponseLease);
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::{MAX_NAT_PMP_LEASE_SECONDS, NatPmpMappingError, NatPmpMappingRequest};

    #[test]
    fn validates_explicit_bounded_udp_mapping_requests() {
        assert!(NatPmpMappingRequest::new(4242, 4242, 600).is_ok());
        assert!(matches!(
            NatPmpMappingRequest::new(0, 4242, 600),
            Err(NatPmpMappingError::ZeroInternalPort)
        ));
        assert!(matches!(
            NatPmpMappingRequest::new(4242, 0, 600),
            Err(NatPmpMappingError::ZeroExternalPort)
        ));
        assert!(matches!(
            NatPmpMappingRequest::new(4242, 4242, 0),
            Err(NatPmpMappingError::InvalidLease)
        ));
        assert!(matches!(
            NatPmpMappingRequest::new(4242, 4242, MAX_NAT_PMP_LEASE_SECONDS + 1),
            Err(NatPmpMappingError::InvalidLease)
        ));
    }
}
