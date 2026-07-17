use std::time::Duration;

pub const MAX_SDK_ASYNC_DEADLINE: Duration = Duration::from_secs(60);

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct SdkAsyncPolicy {
    deadline: Duration,
}

impl SdkAsyncPolicy {
    pub fn new(deadline: Duration) -> Result<Self, SdkAsyncPolicyError> {
        if deadline.is_zero() {
            return Err(SdkAsyncPolicyError::ZeroDeadline);
        }
        if deadline > MAX_SDK_ASYNC_DEADLINE {
            return Err(SdkAsyncPolicyError::DeadlineExceedsMaximum);
        }
        Ok(Self { deadline })
    }

    #[must_use]
    pub const fn deadline(self) -> Duration {
        self.deadline
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, thiserror::Error)]
pub enum SdkAsyncPolicyError {
    #[error("SDK async deadline must be nonzero")]
    ZeroDeadline,
    #[error("SDK async deadline exceeds the supported maximum")]
    DeadlineExceedsMaximum,
}
