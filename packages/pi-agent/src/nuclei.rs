use std::fmt::{Display, Formatter};

pub const PINNED_NUCLEI_TEMPLATES_REVISION: &str = "cce82b61d26bed35074cd57bc9d0aebd703a81d3";
pub const PINNED_NUCLEI_BINARY_VERSION: &str = "v3.10.0";
pub const PINNED_NUCLEI_LINUX_ARM64_ASSET: &str = "nuclei_3.10.0_linux_arm64.zip";
pub const PINNED_NUCLEI_LINUX_ARM64_SHA256: &str =
    "b0ddb1f0cc894b7fa79e45043d00a5ffd2cc9fc15e169bf567d1a384eae51427";
pub const DEFAULT_NUCLEI_BINARY_PATH: &str = "/opt/kelp-pi/bin/nuclei";

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct NucleiTemplatesPin {
    pub revision: &'static str,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct NucleiTemplatesError {
    pub expected: &'static str,
    pub found: String,
}

pub fn enforce_nuclei_templates_pin(
    requested_revision: Option<&str>,
) -> Result<NucleiTemplatesPin, NucleiTemplatesError> {
    if let Some(found) = requested_revision {
        if found != PINNED_NUCLEI_TEMPLATES_REVISION {
            return Err(NucleiTemplatesError {
                expected: PINNED_NUCLEI_TEMPLATES_REVISION,
                found: found.to_string(),
            });
        }
    }
    Ok(NucleiTemplatesPin {
        revision: PINNED_NUCLEI_TEMPLATES_REVISION,
    })
}

impl Display for NucleiTemplatesError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        write!(
            formatter,
            "nuclei templates revision mismatch: expected {}, found {}",
            self.expected, self.found
        )
    }
}

impl std::error::Error for NucleiTemplatesError {}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn enforces_nuclei_templates_pin() {
        assert!(enforce_nuclei_templates_pin(None).is_ok());
        assert!(enforce_nuclei_templates_pin(Some(PINNED_NUCLEI_TEMPLATES_REVISION)).is_ok());
        assert!(
            enforce_nuclei_templates_pin(Some("0000000000000000000000000000000000000000")).is_err()
        );
    }
}
