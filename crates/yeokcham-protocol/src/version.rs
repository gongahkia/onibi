use yeokcham_core::{Error, Result};

#[derive(Clone, Copy, Debug, Eq, Ord, PartialEq, PartialOrd)]
#[repr(transparent)]
pub struct ProtocolVersion(u16);

impl ProtocolVersion {
    pub const INITIAL: Self = Self(1);

    #[must_use]
    pub const fn get(self) -> u16 {
        self.0
    }

    pub fn new(value: u16) -> Result<Self> {
        if value == 0 {
            return Err(Error::InvalidInput("protocol version must be nonzero"));
        }
        Ok(Self(value))
    }
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct VersionRange {
    minimum: ProtocolVersion,
    maximum: ProtocolVersion,
}

impl VersionRange {
    pub fn new(minimum: ProtocolVersion, maximum: ProtocolVersion) -> Result<Self> {
        if minimum > maximum {
            return Err(Error::InvalidInput(
                "minimum version exceeds maximum version",
            ));
        }
        Ok(Self { minimum, maximum })
    }

    #[must_use]
    pub const fn contains(self, version: ProtocolVersion) -> bool {
        self.minimum.get() <= version.get() && version.get() <= self.maximum.get()
    }

    pub fn negotiate(self, peer: Self) -> Result<ProtocolVersion> {
        let maximum = self.maximum.min(peer.maximum);
        if self.contains(maximum) && peer.contains(maximum) {
            return Ok(maximum);
        }
        Err(Error::UnsupportedVersion(maximum.get()))
    }
}

#[cfg(test)]
mod tests {
    use super::{ProtocolVersion, VersionRange};

    #[test]
    fn rejects_zero_version() {
        assert!(ProtocolVersion::new(0).is_err());
    }

    #[test]
    fn negotiates_highest_common_version() {
        let local = VersionRange::new(
            ProtocolVersion::new(1).unwrap(),
            ProtocolVersion::new(3).unwrap(),
        )
        .unwrap();
        let peer = VersionRange::new(
            ProtocolVersion::new(2).unwrap(),
            ProtocolVersion::new(4).unwrap(),
        )
        .unwrap();
        assert_eq!(local.negotiate(peer).unwrap().get(), 3);
    }

    #[test]
    fn rejects_nonoverlapping_ranges() {
        let local = VersionRange::new(
            ProtocolVersion::new(1).unwrap(),
            ProtocolVersion::new(1).unwrap(),
        )
        .unwrap();
        let peer = VersionRange::new(
            ProtocolVersion::new(2).unwrap(),
            ProtocolVersion::new(2).unwrap(),
        )
        .unwrap();
        assert!(local.negotiate(peer).is_err());
    }
}
