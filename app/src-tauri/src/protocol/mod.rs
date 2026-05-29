pub mod version;

use serde::{Deserialize, Serialize};
use serde_json::Value;

pub use version::PROTOCOL_VERSION;

#[derive(Debug, Clone, Copy, Deserialize, Serialize, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
pub enum Decision {
    Allow,
    Deny,
}

impl Decision {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Allow => "allow",
            Self::Deny => "deny",
        }
    }
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct ApprovalRequestBody {
    #[serde(default)]
    pub protocol_version: Option<String>,
    #[serde(default)]
    pub machine_id: Option<String>,
    #[serde(default)]
    pub session_id: Option<String>,
    pub agent: String,
    pub tool: String,
    #[serde(default = "default_input")]
    pub input: Value,
    #[serde(default)]
    pub cwd: String,
    #[serde(default)]
    pub metadata: Option<Value>,
}

fn default_input() -> Value {
    Value::Object(Default::default())
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct Approval {
    pub protocol_version: String,
    pub approval_id: String,
    pub machine_id: String,
    pub session_id: String,
    pub agent: String,
    pub tool: String,
    pub input: Value,
    pub cwd: String,
    pub metadata: Option<Value>,
    pub decision: Option<Decision>,
    #[serde(rename = "updatedInput")]
    pub updated_input: Option<Value>,
    pub reason: Option<String>,
    pub decided_by: Option<String>,
    pub created_at: i64,
    pub decided_at: Option<i64>,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct ApprovalDecisionBody {
    pub decision: Decision,
    #[serde(default, rename = "updatedInput")]
    pub updated_input: Option<Value>,
    #[serde(default)]
    pub reason: Option<String>,
    #[serde(default)]
    pub by: Option<String>,
}

#[derive(Debug, Clone, Deserialize, Serialize, PartialEq)]
pub struct ApprovalDecisionResponse {
    pub protocol_version: String,
    pub approval_id: String,
    pub decision: Decision,
    #[serde(rename = "updatedInput")]
    pub updated_input: Option<Value>,
    pub reason: Option<String>,
}

impl ApprovalDecisionResponse {
    pub fn denied_timeout(approval_id: String) -> Self {
        Self {
            protocol_version: PROTOCOL_VERSION.to_string(),
            approval_id,
            decision: Decision::Deny,
            updated_input: None,
            reason: Some("timeout".to_string()),
        }
    }
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct RunEventBody {
    #[serde(default)]
    pub protocol_version: Option<String>,
    #[serde(default)]
    pub machine_id: Option<String>,
    pub session_id: String,
    pub kind: String,
    #[serde(default)]
    pub timestamp: Option<String>,
    #[serde(default = "default_input")]
    pub payload: Value,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct RunEvent {
    pub id: i64,
    pub protocol_version: String,
    pub machine_id: String,
    pub session_id: String,
    pub kind: String,
    pub payload: Value,
    pub ts: i64,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct DesktopCommandBlock {
    pub id: String,
    #[serde(default)]
    pub protocol_version: Option<String>,
    #[serde(rename = "sessionId")]
    pub session_id: String,
    #[serde(rename = "workspaceId")]
    pub workspace_id: String,
    pub agent: String,
    pub command: String,
    pub cwd: String,
    #[serde(rename = "startedAt")]
    pub started_at: i64,
    #[serde(default, rename = "endedAt")]
    pub ended_at: Option<i64>,
    #[serde(default, rename = "exitCode")]
    pub exit_code: Option<i64>,
    pub status: String,
    #[serde(default, rename = "outputPreview")]
    pub output_preview: String,
    #[serde(default, rename = "previewUrl")]
    pub preview_url: Option<String>,
    #[serde(default, rename = "changedFiles")]
    pub changed_files: Vec<String>,
    #[serde(default)]
    pub attention: Option<String>,
    #[serde(default)]
    pub source: Option<String>,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct PtyOutputBody {
    #[serde(default)]
    pub protocol_version: Option<String>,
    #[serde(default)]
    pub machine_id: Option<String>,
    pub session_id: String,
    pub data: String,
}

#[derive(Debug, Clone, Deserialize, Serialize, Default)]
pub struct DesktopNamedRef {
    pub id: String,
    pub name: String,
}

#[derive(Debug, Clone, Deserialize, Serialize, Default)]
pub struct DesktopSessionControlSnapshot {
    pub owner: String,
    #[serde(default, rename = "externalInputBlocked")]
    pub external_input_blocked: bool,
    #[serde(default, rename = "updatedAt")]
    pub updated_at: Option<i64>,
    #[serde(default)]
    pub reason: Option<String>,
}

#[derive(Debug, Clone, Deserialize, Serialize, Default)]
pub struct DesktopSessionSnapshot {
    pub id: String,
    pub title: String,
    pub agent: String,
    #[serde(rename = "workspaceId")]
    pub workspace_id: String,
    #[serde(default)]
    pub cwd: Option<String>,
    pub status: String,
    pub attention: String,
    #[serde(default, rename = "previewUrl")]
    pub preview_url: Option<String>,
    #[serde(default, rename = "commandBlockCount")]
    pub command_block_count: Option<usize>,
    #[serde(default, rename = "lastCommandBlockId")]
    pub last_command_block_id: Option<String>,
    #[serde(default)]
    pub control: Option<DesktopSessionControlSnapshot>,
}

#[derive(Debug, Clone, Deserialize, Serialize, Default)]
pub struct DesktopSnapshotBody {
    #[serde(default)]
    pub protocol_version: Option<String>,
    #[serde(default)]
    pub sessions: Vec<DesktopSessionSnapshot>,
    #[serde(default)]
    pub arrangements: Vec<DesktopNamedRef>,
    #[serde(default)]
    pub profiles: Vec<DesktopNamedRef>,
    #[serde(default, rename = "updatedAt")]
    pub updated_at: Option<i64>,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct DesktopSessionLaunchBody {
    #[serde(default)]
    pub protocol_version: Option<String>,
    pub profile: String,
    pub workspace: String,
    #[serde(default)]
    pub prompt: Option<String>,
    #[serde(default)]
    pub cwd: Option<String>,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct DesktopSessionInputBody {
    #[serde(default)]
    pub protocol_version: Option<String>,
    pub text: String,
}

#[derive(Debug, Clone, Serialize)]
pub struct DesktopCommandResponse {
    pub ok: bool,
    #[serde(rename = "protocolVersion")]
    pub protocol_version: String,
    #[serde(rename = "commandId")]
    pub command_id: String,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct PairRequest {
    #[serde(rename = "deviceLabel")]
    pub device_label: String,
    #[serde(default, rename = "pushSubscription")]
    pub push_subscription: Option<Value>,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct PairResponse {
    pub protocol_version: String,
    #[serde(rename = "deviceId")]
    pub device_id: String,
    #[serde(rename = "machineId")]
    pub machine_id: String,
}

#[derive(Debug, Clone, Deserialize, Serialize)]
#[serde(tag = "type", rename_all = "kebab-case")]
pub enum ServerMessage {
    ApprovalPending {
        protocol_version: String,
        approval_id: String,
        machine_id: String,
        session_id: String,
        agent: String,
        tool: String,
        input: Value,
        cwd: String,
        metadata: Option<Value>,
    },
    ApprovalResolved {
        protocol_version: String,
        approval_id: String,
        machine_id: String,
        decision: Decision,
        by: Option<String>,
        reason: Option<String>,
    },
    RunEvent {
        protocol_version: String,
        machine_id: String,
        session_id: String,
        kind: String,
        payload: Value,
    },
    PtyOutput {
        protocol_version: String,
        machine_id: String,
        session_id: String,
        data: String,
    },
    DesktopCommand {
        protocol_version: String,
        machine_id: String,
        command_id: String,
        kind: String,
        payload: Value,
    },
    Ping {
        protocol_version: String,
        machine_id: String,
    },
}

impl From<&Approval> for ServerMessage {
    fn from(approval: &Approval) -> Self {
        Self::ApprovalPending {
            protocol_version: PROTOCOL_VERSION.to_string(),
            approval_id: approval.approval_id.clone(),
            machine_id: approval.machine_id.clone(),
            session_id: approval.session_id.clone(),
            agent: approval.agent.clone(),
            tool: approval.tool.clone(),
            input: approval.input.clone(),
            cwd: approval.cwd.clone(),
            metadata: approval.metadata.clone(),
        }
    }
}

#[derive(Debug, Clone, Serialize)]
pub struct ApiError {
    pub protocol_version: &'static str,
    pub error: String,
}

impl ApiError {
    pub fn new(error: impl Into<String>) -> Self {
        Self {
            protocol_version: PROTOCOL_VERSION,
            error: error.into(),
        }
    }
}
