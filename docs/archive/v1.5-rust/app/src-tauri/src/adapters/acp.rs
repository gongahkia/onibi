#![allow(dead_code)]

use crate::{
    adapters::{IntegrationInfo, INTEGRATION_VERSION},
    protocol::{ApprovalRequestBody, Decision, PROTOCOL_VERSION},
    server::{routes, AppState},
};
use anyhow::{anyhow, bail, Context, Result};
use async_trait::async_trait;
use serde_json::{json, Value};
use std::{path::PathBuf, process::Stdio};
use tokio::{
    io::{AsyncBufRead, AsyncBufReadExt, AsyncWrite, AsyncWriteExt, BufReader, Lines},
    process::{Child, ChildStdin, ChildStdout, Command},
};

pub const PROTOCOL_VERSION_ACP: u64 = 1;

#[derive(Debug, Clone)]
pub struct AcpSpawnConfig {
    pub command: String,
    pub args: Vec<String>,
    pub cwd: Option<PathBuf>,
}

#[derive(Debug, Clone)]
pub struct AcpSessionInput {
    pub agent: String,
    pub cwd: PathBuf,
    pub prompt: String,
    pub resume_session_id: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AcpPromptResult {
    pub session_id: String,
    pub stop_reason: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum AcpEvent {
    SessionUpdate(Value),
}

#[async_trait]
pub trait AcpHandler {
    async fn handle_notification(&mut self, _method: &str, _params: Value) -> Result<()> {
        Ok(())
    }

    async fn handle_request(&mut self, method: &str, _params: Value) -> Result<Value> {
        bail!("unsupported ACP client request: {method}")
    }
}

pub struct AcpConnection<R, W>
where
    R: AsyncBufRead + Unpin,
    W: AsyncWrite + Unpin,
{
    reader: Lines<R>,
    writer: W,
    next_id: u64,
}

impl<R, W> AcpConnection<R, W>
where
    R: AsyncBufRead + Unpin,
    W: AsyncWrite + Unpin,
{
    pub fn new(reader: R, writer: W) -> Self {
        Self {
            reader: reader.lines(),
            writer,
            next_id: 1,
        }
    }

    pub async fn request<H>(
        &mut self,
        method: &str,
        params: Value,
        handler: &mut H,
    ) -> Result<Value>
    where
        H: AcpHandler + Send,
    {
        let id = self.next_id;
        self.next_id += 1;
        self.write_json(&json!({
            "jsonrpc": "2.0",
            "id": id,
            "method": method,
            "params": params,
        }))
        .await?;

        loop {
            let message = self.read_message().await?;
            if response_id(&message) == Some(id) {
                if let Some(error) = message.get("error") {
                    bail!("ACP request {method} failed: {error}");
                }
                return Ok(message.get("result").cloned().unwrap_or_else(|| json!({})));
            }
            self.handle_incoming(message, handler).await?;
        }
    }

    pub async fn notify(&mut self, method: &str, params: Value) -> Result<()> {
        self.write_json(&json!({
            "jsonrpc": "2.0",
            "method": method,
            "params": params,
        }))
        .await
    }

    async fn handle_incoming<H>(&mut self, message: Value, handler: &mut H) -> Result<()>
    where
        H: AcpHandler + Send,
    {
        let Some(method) = message.get("method").and_then(Value::as_str) else {
            return Ok(());
        };
        let params = message.get("params").cloned().unwrap_or_else(|| json!({}));
        if let Some(id) = message.get("id").cloned() {
            match handler.handle_request(method, params).await {
                Ok(result) => {
                    self.write_json(&json!({
                        "jsonrpc": "2.0",
                        "id": id,
                        "result": result,
                    }))
                    .await?;
                }
                Err(error) => {
                    self.write_json(&json!({
                        "jsonrpc": "2.0",
                        "id": id,
                        "error": {
                            "code": -32000,
                            "message": error.to_string(),
                        },
                    }))
                    .await?;
                }
            }
        } else {
            handler.handle_notification(method, params).await?;
        }
        Ok(())
    }

    async fn read_message(&mut self) -> Result<Value> {
        let line = self
            .reader
            .next_line()
            .await
            .context("read ACP stdio")?
            .ok_or_else(|| anyhow!("ACP process closed stdout"))?;
        serde_json::from_str(&line).with_context(|| format!("parse ACP JSON-RPC line: {line}"))
    }

    async fn write_json(&mut self, value: &Value) -> Result<()> {
        let mut line = serde_json::to_vec(value)?;
        line.push(b'\n');
        self.writer
            .write_all(&line)
            .await
            .context("write ACP stdio")?;
        self.writer.flush().await.context("flush ACP stdio")
    }
}

pub struct AcpChild {
    child: Child,
    conn: AcpConnection<BufReader<ChildStdout>, ChildStdin>,
}

impl AcpChild {
    pub fn connection(&mut self) -> &mut AcpConnection<BufReader<ChildStdout>, ChildStdin> {
        &mut self.conn
    }

    pub async fn shutdown(mut self) -> Result<()> {
        if self.child.id().is_some() {
            let _ = self.child.kill().await;
        }
        Ok(())
    }
}

pub async fn spawn(config: &AcpSpawnConfig) -> Result<AcpChild> {
    let command_path = crate::util::bin::resolve_binary(&config.command)
        .unwrap_or_else(|| PathBuf::from(&config.command));
    let mut command = Command::new(command_path);
    command.args(&config.args);
    if let Some(cwd) = config.cwd.as_ref() {
        command.current_dir(cwd);
    }
    command
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped());
    let mut child = command
        .spawn()
        .with_context(|| format!("spawn ACP command {}", config.command))?;
    let stdin = child.stdin.take().context("ACP child missing stdin")?;
    let stdout = child.stdout.take().context("ACP child missing stdout")?;
    Ok(AcpChild {
        child,
        conn: AcpConnection::new(BufReader::new(stdout), stdin),
    })
}

pub async fn run_prompt_session(
    state: &AppState,
    spawn_config: AcpSpawnConfig,
    input: AcpSessionInput,
) -> Result<AcpPromptResult> {
    let mut child = spawn(&spawn_config).await?;
    let result = run_prompt_session_on_connection(child.connection(), state, &input).await;
    child.shutdown().await?;
    result
}

pub async fn run_prompt_session_on_connection<R, W>(
    conn: &mut AcpConnection<R, W>,
    state: &AppState,
    input: &AcpSessionInput,
) -> Result<AcpPromptResult>
where
    R: AsyncBufRead + Unpin + Send,
    W: AsyncWrite + Unpin + Send,
{
    let mut handler = OnibiAcpHandler {
        state,
        agent: input.agent.clone(),
        cwd: input.cwd.display().to_string(),
        events: Vec::new(),
    };
    let _initialize = conn
        .request(
            "initialize",
            json!({
                "protocolVersion": PROTOCOL_VERSION_ACP,
                "clientCapabilities": {},
                "clientInfo": {
                    "name": "onibi",
                    "version": env!("CARGO_PKG_VERSION"),
                },
            }),
            &mut handler,
        )
        .await?;
    let _authenticate = conn
        .request("authenticate", json!({ "methodId": "none" }), &mut handler)
        .await
        .ok();
    let session_id = if let Some(session_id) = input.resume_session_id.as_ref() {
        let _loaded = conn
            .request(
                "session/load",
                json!({
                    "sessionId": session_id,
                    "cwd": input.cwd,
                    "mcpServers": [],
                }),
                &mut handler,
            )
            .await?;
        session_id.clone()
    } else {
        let created = conn
            .request(
                "session/new",
                json!({
                    "cwd": input.cwd,
                    "mcpServers": [],
                }),
                &mut handler,
            )
            .await?;
        created
            .get("sessionId")
            .and_then(Value::as_str)
            .map(ToString::to_string)
            .context("ACP session/new did not return sessionId")?
    };
    let prompt = conn
        .request(
            "session/prompt",
            json!({
                "sessionId": session_id,
                "prompt": [{ "type": "text", "text": input.prompt }],
            }),
            &mut handler,
        )
        .await?;
    Ok(AcpPromptResult {
        session_id,
        stop_reason: prompt
            .get("stopReason")
            .and_then(Value::as_str)
            .map(ToString::to_string),
    })
}

pub fn status_info(name: &'static str, command: &str, args: &[String]) -> IntegrationInfo {
    let command_path = crate::util::bin::resolve_binary(command);
    let installed = command_path.is_some();
    let command_line = if args.is_empty() {
        command.to_string()
    } else {
        format!("{command} {}", args.join(" "))
    };
    IntegrationInfo {
        name,
        support: "acp",
        installed,
        installed_version: installed.then(|| INTEGRATION_VERSION.to_string()),
        bundled_version: Some(INTEGRATION_VERSION),
        outdated: false,
        install_path: command_path,
        message: Some(if installed {
            format!("ACP command available: {command_line}")
        } else {
            format!("ACP command not found: {command_line}")
        }),
    }
}

pub fn permission_response_for_decision(decision: Decision, options: &[Value]) -> Value {
    let kind_prefix = if decision == Decision::Allow {
        "allow"
    } else {
        "reject"
    };
    let option_id = options
        .iter()
        .find(|option| {
            option
                .get("kind")
                .and_then(Value::as_str)
                .map(|kind| kind.starts_with(kind_prefix))
                .unwrap_or(false)
        })
        .and_then(|option| option.get("optionId").and_then(Value::as_str))
        .or_else(|| {
            options
                .iter()
                .find_map(|option| option.get("optionId").and_then(Value::as_str))
        })
        .unwrap_or(if decision == Decision::Allow {
            "allow-once"
        } else {
            "reject-once"
        });
    json!({
        "outcome": {
            "outcome": "selected",
            "optionId": option_id,
        }
    })
}

fn response_id(message: &Value) -> Option<u64> {
    message.get("id").and_then(Value::as_u64)
}

struct OnibiAcpHandler<'a> {
    state: &'a AppState,
    agent: String,
    cwd: String,
    events: Vec<AcpEvent>,
}

#[async_trait]
impl AcpHandler for OnibiAcpHandler<'_> {
    async fn handle_notification(&mut self, method: &str, params: Value) -> Result<()> {
        if method == "session/update" {
            self.events.push(AcpEvent::SessionUpdate(params.clone()));
            apply_acp_update(self.state, &self.agent, params).await;
        }
        Ok(())
    }

    async fn handle_request(&mut self, method: &str, params: Value) -> Result<Value> {
        if method != "session/request_permission" {
            bail!("unsupported ACP client request: {method}");
        }
        let response = routes::wait_for_approval_decision(
            self.state,
            ApprovalRequestBody {
                protocol_version: Some(PROTOCOL_VERSION.to_string()),
                machine_id: Some(self.state.machine_id.clone()),
                session_id: params
                    .get("sessionId")
                    .and_then(Value::as_str)
                    .map(ToString::to_string),
                agent: self.agent.clone(),
                tool: acp_tool_name(&params),
                input: params
                    .get("toolCall")
                    .and_then(|tool| tool.get("rawInput"))
                    .cloned()
                    .unwrap_or_else(|| params.get("toolCall").cloned().unwrap_or(params.clone())),
                cwd: self.cwd.clone(),
                metadata: Some(json!({
                    "source": "acp",
                    "hook": "session/request_permission",
                    "raw": params,
                })),
            },
        )
        .await?;
        let options = params
            .get("options")
            .and_then(Value::as_array)
            .cloned()
            .unwrap_or_default();
        Ok(permission_response_for_decision(
            response.decision,
            &options,
        ))
    }
}

async fn apply_acp_update(state: &AppState, agent: &str, params: Value) {
    let session_id = params
        .get("sessionId")
        .and_then(Value::as_str)
        .map(ToString::to_string);
    let status = match params
        .get("update")
        .and_then(|update| update.get("sessionUpdate"))
        .and_then(Value::as_str)
    {
        Some("tool_call") | Some("tool_call_update") | Some("agent_thought_chunk") => {
            Some(crate::orchestration::AgentStatus::Working)
        }
        Some("agent_message_chunk") | Some("usage_update") | Some("session_info_update") => {
            Some(crate::orchestration::AgentStatus::Idle)
        }
        _ => None,
    };
    if session_id.is_none() && status.is_none() {
        return;
    }
    state
        .orchestration
        .apply_provider_event(crate::orchestration::ProviderEventUpdate {
            agent: agent.to_string(),
            session_id,
            provider_session_id: params
                .get("sessionId")
                .and_then(Value::as_str)
                .map(ToString::to_string),
            conversation_id: None,
            cwd: None,
            status,
            resume: params
                .get("sessionId")
                .and_then(Value::as_str)
                .and_then(|id| super::resume_metadata_for_agent(agent, id)),
        })
        .await;
}

fn acp_tool_name(params: &Value) -> String {
    params
        .get("toolCall")
        .and_then(|tool| tool.get("title").and_then(Value::as_str))
        .or_else(|| {
            params
                .get("toolCall")
                .and_then(|tool| tool.get("kind").and_then(Value::as_str))
        })
        .unwrap_or("ACP tool")
        .to_string()
}

#[cfg(test)]
mod tests {
    use super::*;
    use tokio::io::{duplex, split, AsyncBufReadExt, AsyncWriteExt};

    struct TestHandler {
        permission: Decision,
        updates: usize,
    }

    #[async_trait]
    impl AcpHandler for TestHandler {
        async fn handle_notification(&mut self, method: &str, _params: Value) -> Result<()> {
            if method == "session/update" {
                self.updates += 1;
            }
            Ok(())
        }

        async fn handle_request(&mut self, method: &str, params: Value) -> Result<Value> {
            assert_eq!(method, "session/request_permission");
            Ok(permission_response_for_decision(
                self.permission,
                params.get("options").and_then(Value::as_array).unwrap(),
            ))
        }
    }

    #[tokio::test]
    async fn request_handles_permission_before_response() {
        let (client, peer) = duplex(4096);
        let (client_read, client_write) = split(client);
        let (peer_read, mut peer_write) = split(peer);
        let mut conn = AcpConnection::new(BufReader::new(client_read), client_write);

        let peer_task = tokio::spawn(async move {
            let mut lines = BufReader::new(peer_read).lines();
            let request: Value =
                serde_json::from_str(&lines.next_line().await.unwrap().unwrap()).unwrap();
            assert_eq!(request["method"], "session/prompt");
            peer_write
                .write_all(
                    br#"{"jsonrpc":"2.0","id":9,"method":"session/request_permission","params":{"sessionId":"s1","options":[{"kind":"allow_once","name":"Allow","optionId":"allow-1"},{"kind":"reject_once","name":"Deny","optionId":"deny-1"}],"toolCall":{"toolCallId":"t1","title":"Bash","rawInput":{"command":"ls"}}}}"#,
                )
                .await
                .unwrap();
            peer_write.write_all(b"\n").await.unwrap();
            let permission_response: Value =
                serde_json::from_str(&lines.next_line().await.unwrap().unwrap()).unwrap();
            assert_eq!(
                permission_response["result"]["outcome"]["optionId"],
                "allow-1"
            );
            peer_write
                .write_all(
                    format!(
                        "{{\"jsonrpc\":\"2.0\",\"id\":{},\"result\":{{\"stopReason\":\"end_turn\"}}}}\n",
                        request["id"]
                    )
                    .as_bytes(),
                )
                .await
                .unwrap();
        });

        let mut handler = TestHandler {
            permission: Decision::Allow,
            updates: 0,
        };
        let result = conn
            .request(
                "session/prompt",
                json!({ "sessionId": "s1", "prompt": [] }),
                &mut handler,
            )
            .await
            .unwrap();
        assert_eq!(result["stopReason"], "end_turn");
        peer_task.await.unwrap();
    }

    #[test]
    fn permission_response_selects_matching_decision_option() {
        let options = vec![
            json!({"kind": "allow_once", "optionId": "allow"}),
            json!({"kind": "reject_once", "optionId": "deny"}),
        ];
        assert_eq!(
            permission_response_for_decision(Decision::Deny, &options)["outcome"]["optionId"],
            "deny"
        );
    }
}
