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
            support: "native-blocking",
            installed: false,
            installed_version: None,
            bundled_version: Some(INTEGRATION_VERSION),
            outdated: false,
            install_path: Some(path),
            message: Some(error.to_string()),
        }),
        Err(error) => AdapterInfo {
            name: AGENT,
            support: "native-blocking",
            installed: false,
            installed_version: None,
            bundled_version: Some(INTEGRATION_VERSION),
            outdated: false,
            install_path: None,
            message: Some(error.to_string()),
        },
    }
}

pub fn install(token: &str) -> Result<String> {
    install_at(&extension_path()?, token)?;
    Ok("OMP native-blocking extension installed".to_string())
}

pub fn uninstall() -> Result<String> {
    uninstall_at(&extension_path()?)?;
    Ok("OMP native-blocking extension uninstalled".to_string())
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

fn install_at(path: &Path, token: &str) -> Result<()> {
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent).with_context(|| format!("create {}", parent.display()))?;
    }
    fs::write(path, extension_source(token)).with_context(|| format!("write {}", path.display()))
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
            support: "native-blocking",
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
    let installed = source.contains("ONIBI_AGENT = \"omp\"") && source.contains("tool_call");
    Ok(AdapterInfo {
        name: AGENT,
        support: "native-blocking",
        installed,
        installed_version: installed_version.clone(),
        bundled_version: Some(INTEGRATION_VERSION),
        outdated: installed && installed_version.as_deref() != Some(INTEGRATION_VERSION),
        install_path: Some(path.to_path_buf()),
        message: installed.then_some("OMP native-blocking extension installed".to_string()),
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

fn extension_source(token: &str) -> String {
    format!(
        r#"import type {{ ExtensionAPI }} from "@oh-my-pi/pi-coding-agent";

const ONIBI_INTEGRATION_VERSION = "{version}";
const ONIBI_AGENT = "omp";

const env = typeof process !== "undefined" ? process.env ?? {{}} : {{}};
const ONIBI_PORT = env.ONIBI_PORT ?? "17893";
const ONIBI_TOKEN = env.ONIBI_TOKEN ?? {token_json};

function onibiHeaders(): Record<string, string> {{
  const headers: Record<string, string> = {{
    "content-type": "application/json",
    "X-Onibi-Integration-Version": ONIBI_INTEGRATION_VERSION,
  }};
  if (ONIBI_TOKEN) headers.authorization = `Bearer ${{ONIBI_TOKEN}}`;
  return headers;
}}

async function onibiEmit(event: string, payload: any = {{}}, cwd?: string) {{
  await fetch(`http://127.0.0.1:${{ONIBI_PORT}}/v1/adapters/${{ONIBI_AGENT}}/event`, {{
    method: "POST",
    headers: onibiHeaders(),
    body: JSON.stringify({{
      protocol_version: "1.0",
      event,
      status: payload?.status,
      sessionId: payload?.sessionId ?? payload?.session_id ?? payload?.session?.id,
      cwd: payload?.cwd ?? payload?.directory ?? cwd,
      input: payload ?? {{}},
      raw: {{ agent: ONIBI_AGENT, payload }},
    }}),
  }});
}}

async function onibiApproval(event: any, cwd?: string) {{
  if (!ONIBI_TOKEN) {{
    return {{ decision: "deny", reason: "Onibi approval token unavailable" }};
  }}
  const response = await fetch(`http://127.0.0.1:${{ONIBI_PORT}}/v1/approval/request`, {{
    method: "POST",
    headers: onibiHeaders(),
    body: JSON.stringify({{
      protocol_version: "1.0",
      session_id: event?.sessionId ?? event?.session_id ?? event?.session?.id ?? null,
      agent: ONIBI_AGENT,
      tool: event?.toolName ?? event?.tool_name ?? event?.tool ?? "tool_call",
      input: event?.input ?? event?.args ?? {{}},
      cwd: event?.cwd ?? event?.directory ?? cwd ?? "",
      metadata: {{
        source: ONIBI_AGENT,
        providerEvent: "tool_call",
        supportsUpdatedInput: false,
        raw: event,
      }},
    }}),
  }});
  if (!response.ok) {{
    return {{ decision: "deny", reason: `Onibi approval failed: HTTP ${{response.status}}` }};
  }}
  return await response.json();
}}

export default function (pi: ExtensionAPI) {{
  pi.on("session_start", async (event: any, ctx: any) => onibiEmit("session_start", event, ctx?.cwd));
  pi.on("input", async (event: any, ctx: any) => onibiEmit("input", event, ctx?.cwd));
  pi.on("tool_call", async (event: any, ctx: any) => {{
    await onibiEmit("tool_call", event, ctx?.cwd);
    const decision = await onibiApproval(event, ctx?.cwd);
    if (decision?.decision !== "allow") {{
      return {{ block: true, reason: decision?.reason ?? "Denied by Onibi" }};
    }}
    return undefined;
  }});
  pi.on("tool_result", async (event: any, ctx: any) => onibiEmit("tool_result", event, ctx?.cwd));
  pi.on("session_shutdown", async (event: any, ctx: any) => onibiEmit("session_shutdown", event, ctx?.cwd));
}}
"#,
        version = INTEGRATION_VERSION,
        token_json = serde_json::to_string(token).unwrap_or_else(|_| "\"\"".to_string())
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
        install_at(&path, "token").unwrap();

        let source = fs::read_to_string(&path).unwrap();
        assert!(source.contains("ONIBI_AGENT = \"omp\""));
        assert!(source.contains("pi.on(\"tool_call\""));
        assert!(source.contains("/v1/approval/request"));
        assert!(source.contains("supportsUpdatedInput: false"));
        assert!(source.contains("/v1/adapters/${ONIBI_AGENT}/event"));
        let status = status_at(&path).unwrap();
        assert!(status.installed);
        assert_eq!(status.support, "native-blocking");
        assert_eq!(
            status.installed_version.as_deref(),
            Some(INTEGRATION_VERSION)
        );

        uninstall_at(&path).unwrap();
        assert!(!path.exists());
    }
}
