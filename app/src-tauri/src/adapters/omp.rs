use super::{AdapterInfo, INTEGRATION_VERSION};
use anyhow::{Context, Result};
use std::{
    fs,
    path::{Path, PathBuf},
};

const AGENT: &str = "omp";
const EXTENSION_ENV: &str = "ONIBI_OMP_EXTENSION";
const AGENT_DIR_ENV: &str = "OMP_CODING_AGENT_DIR";

pub fn info() -> AdapterInfo {
    match extension_path() {
        Ok(path) => status_at(&path).unwrap_or_else(|error| AdapterInfo {
            name: AGENT,
            support: "native-observe",
            installed: false,
            installed_version: None,
            bundled_version: Some(INTEGRATION_VERSION),
            outdated: false,
            install_path: Some(path),
            message: Some(error.to_string()),
        }),
        Err(error) => AdapterInfo {
            name: AGENT,
            support: "native-observe",
            installed: false,
            installed_version: None,
            bundled_version: Some(INTEGRATION_VERSION),
            outdated: false,
            install_path: None,
            message: Some(error.to_string()),
        },
    }
}

pub fn install() -> Result<String> {
    install_at(&extension_path()?)?;
    Ok("OMP native-observe extension installed; blocking hook API unverified".to_string())
}

pub fn uninstall() -> Result<String> {
    uninstall_at(&extension_path()?)?;
    Ok("OMP native-observe extension uninstalled".to_string())
}

fn extension_path() -> Result<PathBuf> {
    if let Ok(path) = std::env::var(EXTENSION_ENV) {
        return Ok(PathBuf::from(path));
    }
    if let Ok(path) = std::env::var(AGENT_DIR_ENV) {
        return Ok(PathBuf::from(path).join("extensions").join("onibi.ts"));
    }
    let home = directories::BaseDirs::new()
        .context("resolve home directory")?
        .home_dir()
        .to_path_buf();
    Ok(home
        .join(".omp")
        .join("agent")
        .join("extensions")
        .join("onibi.ts"))
}

fn install_at(path: &Path) -> Result<()> {
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent).with_context(|| format!("create {}", parent.display()))?;
    }
    fs::write(path, extension_source()).with_context(|| format!("write {}", path.display()))
}

fn uninstall_at(path: &Path) -> Result<()> {
    if path.exists() {
        fs::remove_file(path).with_context(|| format!("remove {}", path.display()))?;
    }
    Ok(())
}

fn status_at(path: &Path) -> Result<AdapterInfo> {
    if !path.exists() {
        return Ok(AdapterInfo {
            name: AGENT,
            support: "native-observe",
            installed: false,
            installed_version: None,
            bundled_version: Some(INTEGRATION_VERSION),
            outdated: false,
            install_path: Some(path.to_path_buf()),
            message: Some("not installed".to_string()),
        });
    }
    let source = fs::read_to_string(path).with_context(|| format!("read {}", path.display()))?;
    let installed_version = installed_version(&source);
    let installed = source.contains("ONIBI_AGENT = \"omp\"");
    Ok(AdapterInfo {
        name: AGENT,
        support: "native-observe",
        installed,
        installed_version: installed_version.clone(),
        bundled_version: Some(INTEGRATION_VERSION),
        outdated: installed && installed_version.as_deref() != Some(INTEGRATION_VERSION),
        install_path: Some(path.to_path_buf()),
        message: installed
            .then_some("OMP native-observe extension installed; blocking hook API unverified".to_string()),
    })
}

fn installed_version(source: &str) -> Option<String> {
    source
        .lines()
        .find_map(|line| {
            line.trim()
                .strip_prefix("const ONIBI_INTEGRATION_VERSION = ")
        })
        .and_then(|value| {
            value
                .trim()
                .trim_end_matches(';')
                .trim_matches('"')
                .split('"')
                .next()
        })
        .filter(|value| !value.is_empty())
        .map(ToString::to_string)
}

fn extension_source() -> String {
    format!(
        r#"const ONIBI_INTEGRATION_VERSION = "{version}";
const ONIBI_AGENT = "omp";

const env = typeof process !== "undefined" ? process.env ?? {{}} : {{}};
const ONIBI_PORT = env.ONIBI_PORT ?? "17893";
const ONIBI_TOKEN = env.ONIBI_TOKEN ?? "";

async function onibiEmit(event: string, payload: any = {{}}) {{
  const headers: Record<string, string> = {{
    "content-type": "application/json",
    "X-Onibi-Integration-Version": ONIBI_INTEGRATION_VERSION,
  }};
  if (ONIBI_TOKEN) headers.authorization = `Bearer ${{ONIBI_TOKEN}}`;
  await fetch(`http://127.0.0.1:${{ONIBI_PORT}}/v1/adapters/${{ONIBI_AGENT}}/event`, {{
    method: "POST",
    headers,
    body: JSON.stringify({{
      protocol_version: "1.0",
      event,
      status: payload?.status,
      sessionId: payload?.sessionId ?? payload?.session_id,
      cwd: payload?.cwd ?? payload?.directory,
      input: payload ?? {{}},
      raw: {{ agent: ONIBI_AGENT, payload }},
    }}),
  }});
}}

export default {{
  name: "onibi",
  version: ONIBI_INTEGRATION_VERSION,
  async onSessionStart(ctx: any) {{ await onibiEmit("SessionStart", ctx); }},
  async onUserPromptSubmit(ctx: any) {{ await onibiEmit("UserPromptSubmit", ctx); }},
  async onToolBefore(ctx: any) {{ await onibiEmit("ToolBefore", ctx); }},
  async onToolAfter(ctx: any) {{ await onibiEmit("ToolAfter", ctx); }},
  async onStop(ctx: any) {{ await onibiEmit("Stop", ctx); }},
}};
"#,
        version = INTEGRATION_VERSION
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::tempdir;

    #[test]
    fn install_writes_versioned_native_observe_extension() {
        let dir = tempdir().unwrap();
        let path = dir.path().join("onibi.ts");
        install_at(&path).unwrap();

        let source = fs::read_to_string(&path).unwrap();
        assert!(source.contains("ONIBI_AGENT = \"omp\""));
        assert!(source.contains("/v1/adapters/${ONIBI_AGENT}/event"));
        let status = status_at(&path).unwrap();
        assert!(status.installed);
        assert_eq!(status.support, "native-observe");
        assert_eq!(
            status.installed_version.as_deref(),
            Some(INTEGRATION_VERSION)
        );

        uninstall_at(&path).unwrap();
        assert!(!path.exists());
    }
}
