#[derive(Clone, Copy, Debug, Eq, PartialEq)]
#[repr(u16)]
pub enum ProtocolErrorCode {
    MalformedFrame = 1,
    ResourceLimit = 2,
    UnsupportedVersion = 3,
    PolicyViolation = 4,
    Unauthorized = 5,
    ReplayDetected = 6,
    Unavailable = 7,
    Internal = 8,
}

impl ProtocolErrorCode {
    pub const ALL: &[Self] = &[
        Self::MalformedFrame,
        Self::ResourceLimit,
        Self::UnsupportedVersion,
        Self::PolicyViolation,
        Self::Unauthorized,
        Self::ReplayDetected,
        Self::Unavailable,
        Self::Internal,
    ];

    #[must_use]
    pub const fn code(self) -> u16 {
        self as u16
    }
}

impl TryFrom<u16> for ProtocolErrorCode {
    type Error = ProtocolErrorCodeError;

    fn try_from(value: u16) -> Result<Self, Self::Error> {
        match value {
            1 => Ok(Self::MalformedFrame),
            2 => Ok(Self::ResourceLimit),
            3 => Ok(Self::UnsupportedVersion),
            4 => Ok(Self::PolicyViolation),
            5 => Ok(Self::Unauthorized),
            6 => Ok(Self::ReplayDetected),
            7 => Ok(Self::Unavailable),
            8 => Ok(Self::Internal),
            _ => Err(ProtocolErrorCodeError::Unknown(value)),
        }
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq, thiserror::Error)]
pub enum ProtocolErrorCodeError {
    #[error("unknown protocol error code: {0}")]
    Unknown(u16),
}

#[cfg(test)]
mod tests {
    use super::{ProtocolErrorCode, ProtocolErrorCodeError};

    #[test]
    fn codes_are_stable_and_round_trip() {
        assert_eq!(
            ProtocolErrorCode::ALL
                .iter()
                .map(|code| code.code())
                .collect::<Vec<_>>(),
            vec![1, 2, 3, 4, 5, 6, 7, 8]
        );
        for code in ProtocolErrorCode::ALL {
            assert_eq!(ProtocolErrorCode::try_from(code.code()), Ok(*code));
        }
    }

    #[test]
    fn rejects_unknown_codes() {
        assert_eq!(
            ProtocolErrorCode::try_from(0),
            Err(ProtocolErrorCodeError::Unknown(0))
        );
        assert_eq!(
            ProtocolErrorCode::try_from(9),
            Err(ProtocolErrorCodeError::Unknown(9))
        );
    }
}
