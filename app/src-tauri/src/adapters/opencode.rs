use super::{home_path, static_info, AdapterInfo, INTEGRATION_VERSION};
use anyhow::{Context, Result};
use std::{fs, path::PathBuf};

pub fn info() -> AdapterInfo {
    match plugin_path() {
        Ok(path) => {
            let installed_version = fs::read_to_string(&path).ok().and_then(|raw| {
                raw.contains(&version_marker())
                    .then(|| INTEGRATION_VERSION.to_string())
            });
            let installed = installed_version.is_some();
            let mut info = static_info(
                "opencode",
                "event-bridge",
                installed,
                Some(path),
                installed.then_some("OpenCode provider-event plugin installed".to_string()),
            );
            info.installed_version = installed_version;
            info.outdated =
                installed && info.installed_version.as_deref() != Some(INTEGRATION_VERSION);
            info
        }
        Err(error) => static_info(
            "opencode",
            "event-bridge",
            false,
            None,
            Some(error.to_string()),
        ),
    }
}

pub fn install(token: &str) -> Result<String> {
    let path = plugin_path()?;
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent).with_context(|| format!("create {}", parent.display()))?;
    }
    fs::write(&path, plugin_source(token)).with_context(|| format!("write {}", path.display()))?;
    Ok("opencode provider-event plugin installed".to_string())
}

pub fn uninstall() -> Result<String> {
    let path = plugin_path()?;
    if path.exists() {
        fs::remove_file(&path).with_context(|| format!("remove {}", path.display()))?;
    }
    Ok("opencode provider-event plugin uninstalled".to_string())
}

fn plugin_path() -> Result<PathBuf> {
    home_path(
        "ONIBI_OPENCODE_PLUGIN",
        &[".config", "opencode", "plugins", "onibi-provider-events.js"],
    )
}

fn version_marker() -> String {
    format!("ONIBI_INTEGRATION_VERSION={INTEGRATION_VERSION}")
}

fn plugin_source(token: &str) -> String {
    format!(
        r#"// {marker}
const ONIBI_PORT = Number(process.env.ONIBI_PORT || "17893")
const ONIBI_TOKEN = {token_json}

async function post(event, payload) {{
  if (!ONIBI_TOKEN) return
  try {{
    await fetch(`http://127.0.0.1:${{ONIBI_PORT}}/v1/adapters/opencode/event`, {{
      method: "POST",
      headers: {{
        "Authorization": `Bearer ${{ONIBI_TOKEN}}`,
        "Content-Type": "application/json"
      }},
      body: JSON.stringify({{
        event,
        providerSessionId: payload?.sessionID || payload?.sessionId || payload?.session?.id,
        cwd: payload?.cwd || payload?.directory,
        status: payload?.status,
        raw: payload
      }})
    }})
  }} catch (_) {{}}
}}

export const OnibiProviderEvents = async () => ({{
  event: async (input) => {{
    const event = input?.event
    await post(event?.type || "event", event)
  }},
  "tool.execute.before": async (input, output) => {{
    await post("tool.execute.before", {{ ...input, output }})
  }},
  "tool.execute.after": async (input, output) => {{
    await post("tool.execute.after", {{ ...input, output }})
  }}
}})
"#,
        marker = version_marker(),
        token_json = serde_json::to_string(token).unwrap_or_else(|_| "\"\"".to_string()),
    )
}
