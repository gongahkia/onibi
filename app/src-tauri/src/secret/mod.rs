use anyhow::{Context, Result};
use base64::{engine::general_purpose::URL_SAFE_NO_PAD, Engine as _};
use directories::BaseDirs;
use rand::{rngs::OsRng, RngCore};
use serde::{Deserialize, Serialize};
use std::{
    fs::{self, OpenOptions},
    io::{Read, Write},
    path::PathBuf,
};

const SERVICE: &str = "onibi";
const TOKEN_USER: &str = "approval-bearer-token";
const TOKEN_FILE: &str = "token.txt";
const VAPID_FILE: &str = "vapid.json";

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum SecretSource {
    Keyring,
    File(PathBuf),
}

#[derive(Debug, Clone)]
pub struct LoadedToken {
    pub token: String,
    pub source: SecretSource,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct VapidKeys {
    pub public_key: String,
    pub private_key: String,
}

pub fn config_dir() -> Result<PathBuf> {
    if let Ok(path) = std::env::var("ONIBI_CONFIG_DIR") {
        return Ok(PathBuf::from(path));
    }
    let home = BaseDirs::new()
        .context("resolve user directories")?
        .home_dir()
        .to_path_buf();
    Ok(home.join(".config").join("onibi"))
}

pub fn db_path() -> Result<PathBuf> {
    Ok(config_dir()?.join("onibi.db"))
}

pub fn token_path() -> Result<PathBuf> {
    Ok(config_dir()?.join(TOKEN_FILE))
}

pub fn load_or_create_token() -> Result<LoadedToken> {
    if !keyring_disabled() {
        if let Some(token) = read_keyring_token()? {
            return Ok(LoadedToken {
                token,
                source: SecretSource::Keyring,
            });
        }
    }

    let path = token_path()?;
    if let Some(token) = read_token_file(&path)? {
        return Ok(LoadedToken {
            token,
            source: SecretSource::File(path),
        });
    }

    let token = generate_token();
    if !keyring_disabled() && write_keyring_token(&token).is_ok() {
        return Ok(LoadedToken {
            token,
            source: SecretSource::Keyring,
        });
    }

    write_token_file(&path, &token)?;
    Ok(LoadedToken {
        token,
        source: SecretSource::File(path),
    })
}

pub fn rotate_token() -> Result<LoadedToken> {
    let token = generate_token();
    if !keyring_disabled() && write_keyring_token(&token).is_ok() {
        if let Ok(path) = token_path() {
            let _ = fs::remove_file(path);
        }
        return Ok(LoadedToken {
            token,
            source: SecretSource::Keyring,
        });
    }
    let path = token_path()?;
    write_token_file(&path, &token)?;
    Ok(LoadedToken {
        token,
        source: SecretSource::File(path),
    })
}

pub fn load_or_create_vapid_keys() -> Result<VapidKeys> {
    let path = config_dir()?.join(VAPID_FILE);
    if path.exists() {
        let mut raw = String::new();
        OpenOptions::new()
            .read(true)
            .open(&path)
            .with_context(|| format!("open {}", path.display()))?
            .read_to_string(&mut raw)
            .with_context(|| format!("read {}", path.display()))?;
        return serde_json::from_str(&raw).with_context(|| format!("parse {}", path.display()));
    }

    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent)
            .with_context(|| format!("create config directory {}", parent.display()))?;
    }
    let keys = VapidKeys {
        public_key: generate_token(),
        private_key: generate_token(),
    };
    write_secret_file(&path, serde_json::to_string_pretty(&keys)?.as_bytes())?;
    Ok(keys)
}

pub fn generate_token() -> String {
    let mut bytes = [0u8; 32];
    OsRng.fill_bytes(&mut bytes);
    URL_SAFE_NO_PAD.encode(bytes)
}

fn read_keyring_token() -> Result<Option<String>> {
    let entry = match keyring::Entry::new(SERVICE, TOKEN_USER) {
        Ok(entry) => entry,
        Err(_) => return Ok(None),
    };
    match entry.get_password() {
        Ok(token) if !token.trim().is_empty() => Ok(Some(token)),
        Ok(_) => Ok(None),
        Err(_) => Ok(None),
    }
}

fn keyring_disabled() -> bool {
    std::env::var("ONIBI_DISABLE_KEYRING").is_ok_and(|value| value == "1")
}

fn write_keyring_token(token: &str) -> Result<()> {
    let entry = keyring::Entry::new(SERVICE, TOKEN_USER).context("open keyring entry")?;
    entry.set_password(token).context("write token to keyring")
}

fn read_token_file(path: &PathBuf) -> Result<Option<String>> {
    if !path.exists() {
        return Ok(None);
    }
    let mut token = String::new();
    OpenOptions::new()
        .read(true)
        .open(path)
        .with_context(|| format!("open {}", path.display()))?
        .read_to_string(&mut token)
        .with_context(|| format!("read {}", path.display()))?;
    let token = token.trim().to_string();
    Ok((!token.is_empty()).then_some(token))
}

fn write_token_file(path: &PathBuf, token: &str) -> Result<()> {
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent)
            .with_context(|| format!("create config directory {}", parent.display()))?;
    }
    write_secret_file(path, format!("{token}\n").as_bytes())
}

fn write_secret_file(path: &PathBuf, bytes: &[u8]) -> Result<()> {
    #[cfg(unix)]
    {
        use std::os::unix::fs::{OpenOptionsExt, PermissionsExt};
        let mut file = OpenOptions::new()
            .create(true)
            .truncate(true)
            .write(true)
            .mode(0o600)
            .open(path)
            .with_context(|| format!("open {}", path.display()))?;
        file.write_all(bytes)
            .with_context(|| format!("write {}", path.display()))?;
        fs::set_permissions(path, fs::Permissions::from_mode(0o600))
            .with_context(|| format!("chmod 0600 {}", path.display()))?;
    }
    #[cfg(not(unix))]
    {
        let mut file = OpenOptions::new()
            .create(true)
            .truncate(true)
            .write(true)
            .open(path)
            .with_context(|| format!("open {}", path.display()))?;
        file.write_all(bytes)
            .with_context(|| format!("write {}", path.display()))?;
    }
    Ok(())
}
