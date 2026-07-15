use crate::{DeliveryProfile, DeliveryProfileKind, TorMaildropProfileConfig};

pub const MAX_RELAY_REPLICAS: usize = 3;

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct RelayReplicaSelection {
    replicas: Vec<TorMaildropProfileConfig>,
}

impl RelayReplicaSelection {
    pub fn for_profile(
        profile: DeliveryProfile,
        replicas: Vec<TorMaildropProfileConfig>,
    ) -> Result<Self, RelayReplicaSelectionError> {
        if profile.kind() != DeliveryProfileKind::TorMaildrop {
            return Err(RelayReplicaSelectionError::UnsupportedProfile);
        }
        if replicas.is_empty() {
            return Err(RelayReplicaSelectionError::Empty);
        }
        if replicas.len() > MAX_RELAY_REPLICAS {
            return Err(RelayReplicaSelectionError::TooMany);
        }
        if replicas
            .iter()
            .enumerate()
            .any(|(index, replica)| replicas[..index].contains(replica))
        {
            return Err(RelayReplicaSelectionError::Duplicate);
        }
        Ok(Self { replicas })
    }

    #[must_use]
    pub fn replicas(&self) -> &[TorMaildropProfileConfig] {
        &self.replicas
    }
}

#[derive(Debug, Eq, PartialEq, thiserror::Error)]
pub enum RelayReplicaSelectionError {
    #[error("relay replicas require the Tor-maildrop delivery profile")]
    UnsupportedProfile,
    #[error("relay replica selection is empty")]
    Empty,
    #[error("relay replica selection exceeds the configured limit")]
    TooMany,
    #[error("relay replica selection contains a duplicate endpoint")]
    Duplicate,
}

#[cfg(test)]
mod tests {
    use super::{MAX_RELAY_REPLICAS, RelayReplicaSelection, RelayReplicaSelectionError};
    use crate::{DeliveryProfile, TOR_ONION_SERVICE_PUBLIC_KEY_BYTES, TorMaildropProfileConfig};

    fn replica(byte: u8) -> TorMaildropProfileConfig {
        TorMaildropProfileConfig::new([byte; TOR_ONION_SERVICE_PUBLIC_KEY_BYTES], 4444).unwrap()
    }

    #[test]
    fn selects_explicit_unique_tor_maildrop_replicas() {
        let selection = RelayReplicaSelection::for_profile(
            DeliveryProfile::tor_maildrop(),
            vec![replica(0x11), replica(0x22)],
        )
        .unwrap();

        assert_eq!(selection.replicas(), &[replica(0x11), replica(0x22)]);
    }

    #[test]
    fn rejects_invalid_or_non_maildrop_replica_selection() {
        assert_eq!(
            RelayReplicaSelection::for_profile(DeliveryProfile::local_mesh(), vec![replica(0x11)])
                .unwrap_err(),
            RelayReplicaSelectionError::UnsupportedProfile
        );
        assert_eq!(
            RelayReplicaSelection::for_profile(DeliveryProfile::tor_maildrop(), Vec::new())
                .unwrap_err(),
            RelayReplicaSelectionError::Empty
        );
        assert_eq!(
            RelayReplicaSelection::for_profile(
                DeliveryProfile::tor_maildrop(),
                vec![replica(0x11), replica(0x11)],
            )
            .unwrap_err(),
            RelayReplicaSelectionError::Duplicate
        );
        assert_eq!(
            RelayReplicaSelection::for_profile(
                DeliveryProfile::tor_maildrop(),
                vec![replica(0x11); MAX_RELAY_REPLICAS + 1],
            )
            .unwrap_err(),
            RelayReplicaSelectionError::TooMany
        );
    }
}
