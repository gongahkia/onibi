use std::path::{Component, Path, PathBuf};

pub const CLIENT_STATE_DIRECTORY_LAYOUT_VERSION: u8 = 2;
pub const DAEMON_LOCK_FILE: &str = "arachne-daemon.lock";
pub const OUTBOX_DATABASE_FILE: &str = "arachne-outbox.sqlite";
pub const INBOX_DATABASE_FILE: &str = "arachne-inbox.sqlite";
pub const CONTACTS_DATABASE_FILE: &str = "arachne-contacts.sqlite";
pub const RATCHETS_DATABASE_FILE: &str = "arachne-ratchets.sqlite";
pub const ONE_TIME_PREKEY_INVENTORY_DATABASE_FILE: &str = "arachne-one-time-prekeys.sqlite";
pub const ATTACHMENT_UPLOAD_DIRECTORY: &str = "attachment-uploads";
pub const SHARED_IP_MESH_CERTIFICATE_FILE: &str = "arachne-shared-ip-mesh-cert.der";
pub const COURIER_BUNDLES_DATABASE_FILE: &str = "arachne-courier-bundles.sqlite";
pub const COURIER_INBOX_DATABASE_FILE: &str = "arachne-courier-inbox.sqlite";
pub const COURIER_SESSIONS_DATABASE_FILE: &str = "arachne-courier-sessions.sqlite";
pub const COURIER_ONE_TIME_PREKEYS_DATABASE_FILE: &str = "arachne-courier-one-time-prekeys.sqlite";
pub const COURIER_ATTACHMENT_JOBS_DATABASE_FILE: &str = "arachne-courier-attachment-jobs.sqlite";

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct ClientStateDirectory {
    root: PathBuf,
}

impl ClientStateDirectory {
    pub fn new(root: impl AsRef<Path>) -> Result<Self, ClientStateDirectoryError> {
        let root = root.as_ref();
        if root.as_os_str().is_empty() {
            return Err(ClientStateDirectoryError::Empty);
        }
        if !root.is_absolute() {
            return Err(ClientStateDirectoryError::NotAbsolute);
        }
        if root
            .components()
            .any(|component| matches!(component, Component::ParentDir))
        {
            return Err(ClientStateDirectoryError::ParentTraversal);
        }
        if root.file_name().is_none() {
            return Err(ClientStateDirectoryError::FilesystemRoot);
        }
        Ok(Self {
            root: root.to_path_buf(),
        })
    }

    #[must_use]
    pub fn root(&self) -> &Path {
        &self.root
    }

    #[must_use]
    pub fn lock_path(&self) -> PathBuf {
        self.root.join(DAEMON_LOCK_FILE)
    }

    #[must_use]
    pub fn outbox_path(&self) -> PathBuf {
        self.root.join(OUTBOX_DATABASE_FILE)
    }

    #[must_use]
    pub fn inbox_path(&self) -> PathBuf {
        self.root.join(INBOX_DATABASE_FILE)
    }

    #[must_use]
    pub fn contacts_path(&self) -> PathBuf {
        self.root.join(CONTACTS_DATABASE_FILE)
    }

    #[must_use]
    pub fn ratchets_path(&self) -> PathBuf {
        self.root.join(RATCHETS_DATABASE_FILE)
    }

    #[must_use]
    pub fn one_time_prekey_inventory_path(&self) -> PathBuf {
        self.root.join(ONE_TIME_PREKEY_INVENTORY_DATABASE_FILE)
    }

    #[must_use]
    pub fn attachment_uploads_path(&self) -> PathBuf {
        self.root.join(ATTACHMENT_UPLOAD_DIRECTORY)
    }

    #[must_use]
    pub fn shared_ip_mesh_certificate_path(&self) -> PathBuf {
        self.root.join(SHARED_IP_MESH_CERTIFICATE_FILE)
    }

    #[must_use]
    pub fn courier_bundles_path(&self) -> PathBuf {
        self.root.join(COURIER_BUNDLES_DATABASE_FILE)
    }

    #[must_use]
    pub fn courier_inbox_path(&self) -> PathBuf {
        self.root.join(COURIER_INBOX_DATABASE_FILE)
    }

    #[must_use]
    pub fn courier_sessions_path(&self) -> PathBuf {
        self.root.join(COURIER_SESSIONS_DATABASE_FILE)
    }

    #[must_use]
    pub fn courier_one_time_prekeys_path(&self) -> PathBuf {
        self.root.join(COURIER_ONE_TIME_PREKEYS_DATABASE_FILE)
    }

    #[must_use]
    pub fn courier_attachment_jobs_path(&self) -> PathBuf {
        self.root.join(COURIER_ATTACHMENT_JOBS_DATABASE_FILE)
    }
}

#[derive(Clone, Debug, Eq, PartialEq, thiserror::Error)]
pub enum ClientStateDirectoryError {
    #[error("client state directory must not be empty")]
    Empty,
    #[error("client state directory must be absolute")]
    NotAbsolute,
    #[error("client state directory must not contain parent traversal")]
    ParentTraversal,
    #[error("client state directory must not be a filesystem root")]
    FilesystemRoot,
}

#[cfg(test)]
mod tests {
    use std::path::Path;

    use super::{
        ATTACHMENT_UPLOAD_DIRECTORY, CLIENT_STATE_DIRECTORY_LAYOUT_VERSION, CONTACTS_DATABASE_FILE,
        COURIER_ATTACHMENT_JOBS_DATABASE_FILE, COURIER_BUNDLES_DATABASE_FILE,
        COURIER_INBOX_DATABASE_FILE, COURIER_ONE_TIME_PREKEYS_DATABASE_FILE,
        COURIER_SESSIONS_DATABASE_FILE, ClientStateDirectory, ClientStateDirectoryError,
        DAEMON_LOCK_FILE, INBOX_DATABASE_FILE, ONE_TIME_PREKEY_INVENTORY_DATABASE_FILE,
        OUTBOX_DATABASE_FILE, RATCHETS_DATABASE_FILE, SHARED_IP_MESH_CERTIFICATE_FILE,
    };

    #[test]
    fn maps_every_v2_client_state_path_under_the_validated_root() {
        let layout = ClientStateDirectory::new("/var/lib/arachne/alice").unwrap();
        assert_eq!(CLIENT_STATE_DIRECTORY_LAYOUT_VERSION, 2);
        assert_eq!(layout.root(), Path::new("/var/lib/arachne/alice"));
        assert_eq!(
            layout.lock_path(),
            Path::new("/var/lib/arachne/alice").join(DAEMON_LOCK_FILE)
        );
        assert_eq!(
            layout.outbox_path(),
            Path::new("/var/lib/arachne/alice").join(OUTBOX_DATABASE_FILE)
        );
        assert_eq!(
            layout.inbox_path(),
            Path::new("/var/lib/arachne/alice").join(INBOX_DATABASE_FILE)
        );
        assert_eq!(
            layout.contacts_path(),
            Path::new("/var/lib/arachne/alice").join(CONTACTS_DATABASE_FILE)
        );
        assert_eq!(
            layout.ratchets_path(),
            Path::new("/var/lib/arachne/alice").join(RATCHETS_DATABASE_FILE)
        );
        assert_eq!(
            layout.one_time_prekey_inventory_path(),
            Path::new("/var/lib/arachne/alice").join(ONE_TIME_PREKEY_INVENTORY_DATABASE_FILE)
        );
        assert_eq!(
            layout.attachment_uploads_path(),
            Path::new("/var/lib/arachne/alice").join(ATTACHMENT_UPLOAD_DIRECTORY)
        );
        assert_eq!(
            layout.shared_ip_mesh_certificate_path(),
            Path::new("/var/lib/arachne/alice").join(SHARED_IP_MESH_CERTIFICATE_FILE)
        );
        assert_eq!(
            layout.courier_bundles_path(),
            Path::new("/var/lib/arachne/alice").join(COURIER_BUNDLES_DATABASE_FILE)
        );
        assert_eq!(
            layout.courier_inbox_path(),
            Path::new("/var/lib/arachne/alice").join(COURIER_INBOX_DATABASE_FILE)
        );
        assert_eq!(
            layout.courier_sessions_path(),
            Path::new("/var/lib/arachne/alice").join(COURIER_SESSIONS_DATABASE_FILE)
        );
        assert_eq!(
            layout.courier_one_time_prekeys_path(),
            Path::new("/var/lib/arachne/alice").join(COURIER_ONE_TIME_PREKEYS_DATABASE_FILE)
        );
        assert_eq!(
            layout.courier_attachment_jobs_path(),
            Path::new("/var/lib/arachne/alice").join(COURIER_ATTACHMENT_JOBS_DATABASE_FILE)
        );
    }

    #[test]
    fn rejects_empty_relative_traversing_and_filesystem_root_paths() {
        assert_eq!(
            ClientStateDirectory::new(""),
            Err(ClientStateDirectoryError::Empty)
        );
        assert_eq!(
            ClientStateDirectory::new("arachne"),
            Err(ClientStateDirectoryError::NotAbsolute)
        );
        assert_eq!(
            ClientStateDirectory::new("/var/lib/arachne/../other"),
            Err(ClientStateDirectoryError::ParentTraversal)
        );
        assert_eq!(
            ClientStateDirectory::new("/"),
            Err(ClientStateDirectoryError::FilesystemRoot)
        );
    }
}
