pub const SDK_API_VERSION_MAJOR: u16 = 0;
pub const SDK_API_VERSION_MINOR: u16 = 1;
pub const SDK_API_VERSION: SdkApiVersion =
    SdkApiVersion::new(SDK_API_VERSION_MAJOR, SDK_API_VERSION_MINOR);

#[derive(Clone, Copy, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub struct SdkApiVersion {
    major: u16,
    minor: u16,
}

impl SdkApiVersion {
    #[must_use]
    pub const fn new(major: u16, minor: u16) -> Self {
        Self { major, minor }
    }

    #[must_use]
    pub const fn major(self) -> u16 {
        self.major
    }

    #[must_use]
    pub const fn minor(self) -> u16 {
        self.minor
    }

    #[must_use]
    pub const fn is_pre_release(self) -> bool {
        self.major == 0
    }
}

#[cfg(test)]
mod tests {
    use super::{SDK_API_VERSION, SDK_API_VERSION_MAJOR, SDK_API_VERSION_MINOR, SdkApiVersion};

    #[test]
    fn public_sdk_starts_as_an_explicit_pre_release() {
        assert_eq!(SDK_API_VERSION.major(), SDK_API_VERSION_MAJOR);
        assert_eq!(SDK_API_VERSION.minor(), SDK_API_VERSION_MINOR);
        assert!(SDK_API_VERSION.is_pre_release());
        assert!(!SdkApiVersion::new(1, 0).is_pre_release());
    }
}
