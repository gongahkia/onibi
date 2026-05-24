import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Background,
  Controls,
  MiniMap,
  ReactFlow,
  useEdgesState,
  useNodesState
} from "@xyflow/react";
import type { Connection, Edge, EdgeChange, NodeChange } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import {
  CheckCircle2,
  FileStack,
  Info,
  ListChecks,
  PanelRightOpen,
  Play,
  Plus,
  Search,
  Trash2,
  WandSparkles,
  X
} from "lucide-react";
import {
  createWorkflowEdge,
  createWorkflowNode,
  createWorkflowSpec,
  stableWorkflowStringify,
  validateWorkflowSpec
} from "@kelpclaw/workflow-spec";
import type {
  JsonRecord,
  WorkflowBranch,
  WorkflowBranchMergeConflict,
  WorkflowBranchMergePreview,
  WorkflowBranchMergeResolution,
  WorkflowBranchPlanResponse,
  WorkflowClarificationRequest,
  WorkflowConnectorOperation,
  WorkflowConnectorRecord,
  WorkflowDraftEvaluation,
  WorkflowAdapterOperationRef,
  WorkflowApprovedRevision,
  WorkflowAgentMemoryRecord,
  WorkflowAgentTimelineEvent,
  WorkflowGeneratedModuleReuseDecision,
  WorkflowBudgetLedger,
  WorkflowBudgetPolicy,
  WorkflowJob,
  JsonValue,
  WorkflowDeploymentRecord,
  WorkflowWorkspace,
  WorkflowProviderRuntimeConfig,
  WorkflowNode,
  WorkflowNodeDecisionTrace,
  WorkflowNodeKind,
  WorkflowOpsHealth,
  WorkflowPlanResponse,
  WorkflowPlanSuccessResponse,
  WorkflowPlannerFeedback,
  WorkflowPromptTurn,
  WorkflowRunRecord,
  WorkflowRuntimeTruthSnapshot,
  WorkflowRouterEvalCase,
  WorkflowRouterEvalRun,
  WorkflowScheduleRecord,
  WorkflowSpec,
  WorkflowSpecDiff,
  WorkflowTaskRoute,
  WorkflowValidationIssue,
  WorkflowValidationResult
} from "@kelpclaw/workflow-spec";
import { kelpClawApi, readKelpClawAdminToken, saveKelpClawAdminToken } from "./api-client.js";
import type {
  AgentRunRecord,
  AgentStepEvent,
  DeploymentActivationSummaryResponse,
  IntegrationReadiness,
  SecretMetadata
} from "./api-client.js";
import {
  firstInputPort,
  firstOutputPort,
  nextNodePosition,
  workflowNodeTypes,
  workflowToEdges,
  workflowToNodes
} from "./workflow-elements.js";
import type { WorkflowFlowEdge, WorkflowFlowNode, WorkflowNodeData } from "./workflow-elements.js";
import { ProviderIcon, providerIconKeyForAdapter } from "./provider-icons.js";
import { TrajectoryView } from "./trajectory-view.js";
import "./styles.css";

const defaultPrompt = "";
const defaultBranchName = "Experiment";
const emptyWorkflowDraft = createWorkflowSpec({
  id: "workflow.kelpclaw-draft",
  name: "Untitled Workflow",
  prompt: defaultPrompt,
  nodes: [],
  edges: [],
  createdAt: "1970-01-01T00:00:00.000Z",
  updatedAt: "1970-01-01T00:00:00.000Z"
});

function isEmptyStarterWorkflow(workflow: WorkflowSpec): boolean {
  return (
    workflow.id === emptyWorkflowDraft.id &&
    workflow.prompt.trim().length === 0 &&
    workflow.nodes.length === 0 &&
    workflow.edges.length === 0
  );
}

interface AdapterSkillPreset {
  readonly id: string;
  readonly label: string;
  readonly nodeKinds: readonly WorkflowNodeKind[];
  readonly adapterIds: readonly string[];
  readonly adapterOperations: readonly WorkflowAdapterOperationRef[];
  readonly secretRefs: Readonly<Record<string, string>>;
  readonly config: JsonRecord;
}

type NodeActionHandlers = {
  readonly onInlineEdit: NonNullable<WorkflowNodeData["onInlineEdit"]>;
  readonly onSelectNode: NonNullable<WorkflowNodeData["onSelectNode"]>;
  readonly onOpenDetails: NonNullable<WorkflowNodeData["onOpenDetails"]>;
  readonly onDeleteNode: NonNullable<WorkflowNodeData["onDeleteNode"]>;
  readonly onRepromptNode: NonNullable<WorkflowNodeData["onRepromptNode"]>;
  readonly onAddNextNode: NonNullable<WorkflowNodeData["onAddNextNode"]>;
};

const adapterSkillPresets: readonly AdapterSkillPreset[] = [
  {
    id: "skill.gmail.receipts.read",
    label: "Gmail receipts",
    nodeKinds: ["skill"],
    adapterIds: ["adapter.gmail"],
    adapterOperations: [
      {
        adapterId: "adapter.gmail",
        operation: "gmail.receipts.search",
        operationVersion: "1.0.0"
      }
    ],
    secretRefs: { "gmail.oauth": "secret:google.oauth.default" },
    config: {
      query: "from:(receipts OR orders) newer_than:30d",
      allowedHosts: ["oauth2.googleapis.com", "gmail.googleapis.com"]
    }
  },
  {
    id: "skill.sheets.rows.append",
    label: "Sheets append",
    nodeKinds: ["delivery"],
    adapterIds: ["adapter.sheets"],
    adapterOperations: [
      {
        adapterId: "adapter.sheets",
        operation: "sheets.rows.append",
        operationVersion: "1.0.0"
      }
    ],
    secretRefs: { "sheets.oauth": "secret:google.oauth.default" },
    config: {
      channel: "sheets",
      channels: ["sheets"],
      range: "Receipts!A:D",
      allowedHosts: ["oauth2.googleapis.com", "sheets.googleapis.com"]
    }
  },
  {
    id: "skill.email.results.deliver",
    label: "Email results",
    nodeKinds: ["delivery"],
    adapterIds: ["adapter.email"],
    adapterOperations: [
      {
        adapterId: "adapter.email",
        operation: "email.results.send",
        operationVersion: "1.0.0"
      }
    ],
    secretRefs: { "email.delivery": "secret:email.smtp.default" },
    config: {
      channel: "email",
      channels: ["email"],
      to: "owner@example.com",
      allowedHosts: ["smtp"]
    }
  },
  {
    id: "skill.alert.push.dispatch",
    label: "Push alerts",
    nodeKinds: ["delivery"],
    adapterIds: ["adapter.whatsapp", "adapter.telegram"],
    adapterOperations: [
      {
        adapterId: "adapter.whatsapp",
        operation: "whatsapp.alert.send",
        operationVersion: "1.0.0"
      },
      {
        adapterId: "adapter.telegram",
        operation: "telegram.alert.send",
        operationVersion: "1.0.0"
      }
    ],
    secretRefs: {
      "whatsapp.apiKey": "secret:whatsapp.cloud.default",
      "telegram.botToken": "secret:telegram.bot.default"
    },
    config: {
      channel: "email",
      channels: ["whatsapp", "telegram"],
      timeSensitive: true,
      allowedHosts: ["graph.facebook.com", "api.telegram.org"]
    }
  }
];

const componentCategoryLabels = {
  "input-output": "Input & Output",
  "data-sources": "Data Sources",
  "models-agents": "Models & Agents",
  "llm-operations": "LLM Operations",
  "files-knowledge": "Files & Knowledge",
  processing: "Processing",
  "flow-control": "Flow Control"
} as const;

type ComponentCategoryId = keyof typeof componentCategoryLabels;
type ComponentPaletteFilter = ComponentCategoryId | "all";

interface ComponentPaletteItem {
  readonly id: string;
  readonly category: ComponentCategoryId;
  readonly label: string;
  readonly description: string;
  readonly kind: WorkflowNodeKind;
  readonly inputs?: WorkflowNode["inputs"] | undefined;
  readonly outputs?: WorkflowNode["outputs"] | undefined;
  readonly config?: JsonRecord | undefined;
  readonly skillId?: string | undefined;
  readonly adapterId?: string | undefined;
  readonly adapterIds?: readonly string[] | undefined;
  readonly adapterOperations?: readonly WorkflowAdapterOperationRef[] | undefined;
  readonly secretRefs?: Readonly<Record<string, string>> | undefined;
  readonly agentic?: WorkflowNode["agentic"] | undefined;
  readonly iconKey?: string | undefined;
}

const objectPort = { type: "object" as const, additionalProperties: true };
const arrayPort = { type: "array" as const, items: objectPort };
const gmailReceiptsPreset = adapterSkillPresets[0]!;
const sheetsAppendPreset = adapterSkillPresets[1]!;

const componentPaletteItems: readonly ComponentPaletteItem[] = [
  {
    id: "manual-input",
    category: "input-output",
    label: "Manual Input",
    description: "Starts the workflow from operator-supplied payloads.",
    kind: "trigger",
    outputs: { request: objectPort },
    config: { trigger: "manual", promptSource: "operator" }
  },
  {
    id: "webhook-input",
    category: "input-output",
    label: "Webhook Input",
    description: "Starts the workflow from an incoming HTTP payload.",
    kind: "trigger",
    outputs: { request: objectPort },
    config: { trigger: "webhook", path: "/webhooks/kelpclaw" }
  },
  {
    id: "email-delivery",
    category: "input-output",
    label: "Email Delivery",
    description: "Delivers a prepared result through SMTP metadata.",
    kind: "delivery",
    inputs: { rows: arrayPort, approved: objectPort },
    outputs: { delivery: objectPort },
    skillId: "skill.email.results.deliver",
    adapterId: "adapter.email",
    adapterIds: ["adapter.email"],
    adapterOperations: [
      {
        adapterId: "adapter.email",
        operation: "email.results.send",
        operationVersion: "1.0.0"
      }
    ],
    secretRefs: { "email.delivery": "secret:email.smtp.default" },
    config: {
      channel: "email",
      channels: ["email"],
      destination: "owner@example.com",
      allowedHosts: ["smtp"]
    }
  },
  {
    id: "gmail-receipts",
    category: "data-sources",
    label: "Gmail Receipts",
    description: "Reads matching Gmail receipts with the Gmail adapter.",
    kind: "skill",
    inputs: { request: objectPort },
    outputs: { receipts: arrayPort },
    skillId: "skill.gmail.receipts.read",
    adapterId: "adapter.gmail",
    adapterIds: ["adapter.gmail"],
    adapterOperations: gmailReceiptsPreset.adapterOperations,
    secretRefs: gmailReceiptsPreset.secretRefs,
    config: {
      skillMode: "adapter",
      ...gmailReceiptsPreset.config
    }
  },
  {
    id: "sheets-append",
    category: "data-sources",
    label: "Sheets Append",
    description: "Appends structured rows to Google Sheets.",
    kind: "delivery",
    inputs: { rows: arrayPort },
    outputs: { delivery: objectPort },
    skillId: "skill.sheets.rows.append",
    adapterId: "adapter.sheets",
    adapterIds: ["adapter.sheets"],
    adapterOperations: sheetsAppendPreset.adapterOperations,
    secretRefs: sheetsAppendPreset.secretRefs,
    config: {
      ...sheetsAppendPreset.config
    }
  },
  {
    id: "github-issue-create",
    category: "data-sources",
    label: "GitHub Issue",
    description: "Creates a GitHub issue in a repository.",
    kind: "delivery",
    inputs: { payload: objectPort },
    outputs: { delivery: objectPort },
    skillId: "skill.github.issue.create",
    adapterId: "adapter.github",
    adapterIds: ["adapter.github"],
    adapterOperations: [
      {
        adapterId: "adapter.github",
        operation: "github.issue.create",
        operationVersion: "1.0.0"
      }
    ],
    secretRefs: { "github.token": "secret:github.token.default" },
    config: {
      owner: "org",
      repo: "repo",
      title: "Workflow alert",
      body: "Generated by KelpClaw.",
      allowedHosts: ["api.github.com"]
    }
  },
  {
    id: "slack-message-send",
    category: "input-output",
    label: "Slack Message",
    description: "Sends a Slack bot message to a channel.",
    kind: "delivery",
    inputs: { payload: objectPort },
    outputs: { delivery: objectPort },
    skillId: "skill.slack.message.send",
    adapterId: "adapter.slack",
    adapterIds: ["adapter.slack"],
    adapterOperations: [
      {
        adapterId: "adapter.slack",
        operation: "slack.message.send",
        operationVersion: "1.0.0"
      }
    ],
    secretRefs: { "slack.botToken": "secret:slack.bot.default" },
    config: {
      channel: "C0123456789",
      text: "Workflow completed.",
      allowedHosts: ["slack.com"]
    }
  },
  {
    id: "discord-message-send",
    category: "input-output",
    label: "Discord Message",
    description: "Sends a Discord bot message to a channel.",
    kind: "delivery",
    inputs: { payload: objectPort },
    outputs: { delivery: objectPort },
    skillId: "skill.discord.message.send",
    adapterId: "adapter.discord",
    adapterIds: ["adapter.discord"],
    adapterOperations: [
      {
        adapterId: "adapter.discord",
        operation: "discord.message.send",
        operationVersion: "1.0.0"
      }
    ],
    secretRefs: { "discord.botToken": "secret:discord.bot.default" },
    config: {
      channelId: "000000000000000000",
      content: "Workflow completed.",
      allowedHosts: ["discord.com"]
    }
  },
  {
    id: "notion-page-create",
    category: "data-sources",
    label: "Notion Page",
    description: "Creates a Notion page or database entry.",
    kind: "delivery",
    inputs: { payload: objectPort },
    outputs: { delivery: objectPort },
    skillId: "skill.notion.page.create",
    adapterId: "adapter.notion",
    adapterIds: ["adapter.notion"],
    adapterOperations: [
      {
        adapterId: "adapter.notion",
        operation: "notion.page.create",
        operationVersion: "1.0.0"
      }
    ],
    secretRefs: { "notion.apiKey": "secret:notion.api.default" },
    config: {
      parent: { database_id: "notion-database-id" },
      properties: {},
      allowedHosts: ["api.notion.com"]
    }
  },
  {
    id: "linear-issue-create",
    category: "data-sources",
    label: "Linear Issue",
    description: "Creates a Linear issue through GraphQL.",
    kind: "delivery",
    inputs: { payload: objectPort },
    outputs: { delivery: objectPort },
    skillId: "skill.linear.issue.create",
    adapterId: "adapter.linear",
    adapterIds: ["adapter.linear"],
    adapterOperations: [
      {
        adapterId: "adapter.linear",
        operation: "linear.issue.create",
        operationVersion: "1.0.0"
      }
    ],
    secretRefs: { "linear.apiKey": "secret:linear.api.default" },
    config: {
      query:
        "mutation IssueCreate($input: IssueCreateInput!) { issueCreate(input: $input) { success issue { id url } } }",
      variables: { input: { title: "Workflow alert" } },
      allowedHosts: ["api.linear.app"]
    }
  },
  {
    id: "jira-issue-create",
    category: "data-sources",
    label: "Jira Issue",
    description: "Creates a Jira Cloud issue.",
    kind: "delivery",
    inputs: { payload: objectPort },
    outputs: { delivery: objectPort },
    skillId: "skill.jira.issue.create",
    adapterId: "adapter.jira",
    adapterIds: ["adapter.jira"],
    adapterOperations: [
      {
        adapterId: "adapter.jira",
        operation: "jira.issue.create",
        operationVersion: "1.0.0"
      }
    ],
    secretRefs: { "jira.basicAuth": "secret:jira.basic.default" },
    config: {
      siteHost: "your-site.atlassian.net",
      fields: {
        project: { key: "OPS" },
        summary: "Workflow alert",
        issuetype: { name: "Task" }
      },
      allowedHosts: ["*.atlassian.net"]
    }
  },
  {
    id: "airtable-record-create",
    category: "data-sources",
    label: "Airtable Record",
    description: "Creates an Airtable record.",
    kind: "delivery",
    inputs: { payload: objectPort },
    outputs: { delivery: objectPort },
    skillId: "skill.airtable.record.create",
    adapterId: "adapter.airtable",
    adapterIds: ["adapter.airtable"],
    adapterOperations: [
      {
        adapterId: "adapter.airtable",
        operation: "airtable.record.create",
        operationVersion: "1.0.0"
      }
    ],
    secretRefs: { "airtable.apiKey": "secret:airtable.api.default" },
    config: {
      baseId: "appXXXXXXXXXXXXXX",
      tableName: "Tasks",
      fields: { Name: "Workflow alert" },
      allowedHosts: ["api.airtable.com"]
    }
  },
  {
    id: "webhook-post",
    category: "input-output",
    label: "Webhook POST",
    description: "Posts JSON to a configured HTTPS webhook.",
    kind: "delivery",
    inputs: { payload: objectPort },
    outputs: { delivery: objectPort },
    skillId: "skill.webhook.post",
    adapterId: "adapter.webhook",
    adapterIds: ["adapter.webhook"],
    adapterOperations: [
      {
        adapterId: "adapter.webhook",
        operation: "webhook.post",
        operationVersion: "1.0.0"
      }
    ],
    secretRefs: { "webhook.token": "secret:webhook.token.default" },
    config: {
      url: "https://hooks.example.com/kelpclaw",
      body: { event: "workflow.completed" },
      allowedHosts: ["*"]
    }
  },
  {
    id: "database-query",
    category: "data-sources",
    label: "DB Query",
    description: "Reads rows through the configured database runtime client.",
    kind: "skill",
    inputs: { request: objectPort },
    outputs: { rows: arrayPort },
    skillId: "skill.database.query",
    adapterId: "adapter.database",
    adapterIds: ["adapter.database"],
    adapterOperations: [
      {
        adapterId: "adapter.database",
        operation: "database.query",
        operationVersion: "1.0.0"
      }
    ],
    secretRefs: { "database.connection": "secret:database.connection.default" },
    config: {
      statement: "SELECT * FROM receipts LIMIT 100",
      parameters: [],
      maxRows: 100,
      allowedHosts: ["database"]
    }
  },
  {
    id: "database-execute",
    category: "processing",
    label: "DB Execute",
    description: "Writes rows through the configured database runtime client.",
    kind: "skill",
    inputs: { payload: objectPort },
    outputs: { result: objectPort },
    skillId: "skill.database.execute",
    adapterId: "adapter.database",
    adapterIds: ["adapter.database"],
    adapterOperations: [
      {
        adapterId: "adapter.database",
        operation: "database.execute",
        operationVersion: "1.0.0"
      }
    ],
    secretRefs: { "database.connection": "secret:database.connection.default" },
    config: {
      statement: "INSERT INTO workflow_events (id, status) VALUES (?1, ?2)",
      parameters: ["event-id", "processed"],
      maxRows: 100,
      allowedHosts: ["database"]
    }
  },
  {
    id: "research-agent",
    category: "models-agents",
    label: "Research Agent",
    description: "Runs bounded live web research with source and limitation capture.",
    kind: "skill",
    inputs: { request: objectPort },
    outputs: { result: objectPort },
    config: {
      skillMode: "agentic",
      plannerRationale: "Manual component palette insertion for bounded research."
    },
    agentic: {
      tools: ["web-search", "summarizer"],
      memoryScope: "workspace",
      stopConditions: ["research-summary-ready", "source-confidence-recorded"],
      humanApprovalBoundaries: ["Before external delivery or publication."],
      networkPolicy: "declared",
      allowedHosts: ["*"],
      secretRefs: [],
      evalContract: {
        requiredFields: ["summary", "sources", "limitations"]
      },
      budget: {
        maxIterations: 3,
        maxWallClockSeconds: 300,
        maxModelCostUsd: 2,
        maxDockerRuntimeSeconds: 120,
        maxRetries: 1
      }
    }
  },
  {
    id: "generated-code",
    category: "models-agents",
    label: "Generated Code",
    description: "Creates a codegen node for custom deterministic logic.",
    kind: "codegen",
    inputs: { request: objectPort },
    outputs: { artifact: objectPort },
    config: { sandboxPolicy: "network-none", artifactStatus: "draft" }
  },
  {
    id: "llm-summary",
    category: "llm-operations",
    label: "Summarize Text",
    description: "Summarizes upstream content into a compact result object.",
    kind: "skill",
    inputs: { request: objectPort },
    outputs: { result: objectPort },
    config: { skillMode: "deterministic", task: "summarize" }
  },
  {
    id: "file-ingest",
    category: "files-knowledge",
    label: "File Intake",
    description: "Accepts uploaded or workspace file metadata as workflow input.",
    kind: "trigger",
    outputs: { file: objectPort },
    config: { trigger: "file", source: "workspace" }
  },
  {
    id: "transform-data",
    category: "processing",
    label: "Transform Data",
    description: "Maps upstream payloads into a downstream shape.",
    kind: "transform",
    inputs: { input: objectPort },
    outputs: { output: objectPort },
    config: { mode: "map" }
  },
  {
    id: "approval-gate",
    category: "flow-control",
    label: "Approval Gate",
    description: "Requires owner approval before downstream execution.",
    kind: "approval",
    inputs: { input: objectPort },
    outputs: { approved: objectPort },
    config: { requiredRole: "owner" }
  }
] as const;

const integrationSetups = [
  {
    id: "google",
    label: "Google",
    secretName: "google.oauth.default",
    placeholder: '{"refreshToken":"...","clientId":"...","clientSecret":"..."}'
  },
  {
    id: "smtp",
    label: "SMTP",
    secretName: "email.smtp.default",
    placeholder: '{"host":"smtp.example.com","port":587,"username":"...","password":"..."}'
  },
  {
    id: "whatsapp",
    label: "WhatsApp",
    secretName: "whatsapp.cloud.default",
    placeholder: '{"accessToken":"...","phoneNumberId":"...","apiVersion":"v20.0"}'
  },
  {
    id: "telegram",
    label: "Telegram",
    secretName: "telegram.bot.default",
    placeholder: '{"botToken":"...","chatId":"..."}'
  },
  {
    id: "github",
    label: "GitHub",
    secretName: "github.token.default",
    placeholder: "github_pat_..."
  },
  {
    id: "slack",
    label: "Slack",
    secretName: "slack.bot.default",
    placeholder: "xoxb-..."
  },
  {
    id: "discord",
    label: "Discord",
    secretName: "discord.bot.default",
    placeholder: "Discord bot token"
  },
  {
    id: "notion",
    label: "Notion",
    secretName: "notion.api.default",
    placeholder: "secret_..."
  },
  {
    id: "linear",
    label: "Linear",
    secretName: "linear.api.default",
    placeholder: "lin_api_..."
  },
  {
    id: "jira",
    label: "Jira",
    secretName: "jira.basic.default",
    placeholder: "email@example.com:api-token"
  },
  {
    id: "airtable",
    label: "Airtable",
    secretName: "airtable.api.default",
    placeholder: "pat..."
  },
  {
    id: "webhook",
    label: "Webhook",
    secretName: "webhook.token.default",
    placeholder: "Bearer token sent to the webhook"
  },
  {
    id: "database",
    label: "Database",
    secretName: "database.connection.default",
    placeholder: '{"engine":"sqlite","databasePath":"/absolute/path/app.db","allowWrites":false}'
  }
] as const;

interface PaletteCommand {
  readonly id: string;
  readonly group: string;
  readonly label: string;
  readonly detail?: string | undefined;
  readonly keywords?: readonly string[] | undefined;
  readonly iconKey?: string | undefined;
  readonly disabled?: boolean | undefined;
  readonly closeOnSelect?: boolean | undefined;
  readonly onSelect: () => void;
}

type CommandPaletteMode =
  | { readonly kind: "commands"; readonly scope?: "all" | "node-create" | undefined }
  | { readonly kind: "plan"; readonly value: string }
  | { readonly kind: "clarification" }
  | { readonly kind: "fork-branch"; readonly value: string }
  | { readonly kind: "rename-branch"; readonly value: string }
  | { readonly kind: "admin-token"; readonly value: string }
  | {
      readonly kind: "secret";
      readonly label: string;
      readonly secretName: string;
      readonly value: string;
    };

type DetailsTab = "node" | "config" | "trace" | "runtime" | "ops";
type SurfaceMode = "governance" | "edit" | "trajectory" | "policy";

interface SkillGovernanceCommand {
  readonly id: string;
  readonly label: string;
  readonly command: string;
}

interface PendingNodeConnection {
  readonly sourceNodeId: string;
  readonly outputPort?: string | undefined;
}

const detailTabs: readonly { readonly id: DetailsTab; readonly label: string }[] = [
  { id: "node", label: "Node" },
  { id: "config", label: "Config" },
  { id: "trace", label: "Trace" },
  { id: "runtime", label: "Runtime" },
  { id: "ops", label: "Ops" }
];

export function App() {
  const [workflow, setWorkflow] = useState<WorkflowSpec>(emptyWorkflowDraft);
  const [prompt, setPrompt] = useState(defaultPrompt);
  const [validation, setValidation] = useState<WorkflowValidationResult>(
    validateWorkflowSpec(emptyWorkflowDraft)
  );
  const [approvedRevision, setApprovedRevision] = useState<WorkflowApprovedRevision | null>(null);
  const [approvalDiff, setApprovalDiff] = useState<WorkflowSpecDiff | null>(null);
  const [run, setRun] = useState<WorkflowRunRecord | null>(null);
  const [taskRoute, setTaskRoute] = useState<WorkflowTaskRoute | null>(null);
  const [plannerFeedback, setPlannerFeedback] = useState<WorkflowPlannerFeedback | null>(null);
  const [draftEvaluation, setDraftEvaluation] = useState<WorkflowDraftEvaluation | null>(null);
  const [clarification, setClarification] = useState<WorkflowClarificationRequest | null>(null);
  const [clarificationAnswers, setClarificationAnswers] = useState<
    Readonly<Record<string, string>>
  >({});
  const [activeJob, setActiveJob] = useState<WorkflowJob | null>(null);
  const [workspace, setWorkspace] = useState<WorkflowWorkspace | null>(null);
  const [agentRuns, setAgentRuns] = useState<readonly unknown[]>([]);
  const [nodeDecisionTraces, setNodeDecisionTraces] = useState<
    readonly WorkflowNodeDecisionTrace[]
  >([]);
  const [decisionTraceExportNotice, setDecisionTraceExportNotice] = useState<string | null>(null);
  const [deploymentNotice, setDeploymentNotice] = useState<string | null>(null);
  const [planAcceptedNotice, setPlanAcceptedNotice] = useState<string | null>(null);
  const [deploymentActivations, setDeploymentActivations] =
    useState<DeploymentActivationSummaryResponse | null>(null);
  const [runtimeTruth, setRuntimeTruth] = useState<WorkflowRuntimeTruthSnapshot | null>(null);
  const [providerConfigs, setProviderConfigs] = useState<readonly WorkflowProviderRuntimeConfig[]>(
    []
  );
  const [budgetPolicy, setBudgetPolicy] = useState<WorkflowBudgetPolicy | null>(null);
  const [budgetLedgers, setBudgetLedgers] = useState<readonly WorkflowBudgetLedger[]>([]);
  const [agentTimeline, setAgentTimeline] = useState<readonly WorkflowAgentTimelineEvent[]>([]);
  const [connectors, setConnectors] = useState<readonly WorkflowConnectorRecord[]>([]);
  const [workflowRuns, setWorkflowRuns] = useState<readonly WorkflowRunRecord[]>([]);
  const [workflowSchedules, setWorkflowSchedules] = useState<readonly WorkflowScheduleRecord[]>([]);
  const [opsHealth, setOpsHealth] = useState<WorkflowOpsHealth | null>(null);
  const [routerEvalCases, setRouterEvalCases] = useState<readonly WorkflowRouterEvalCase[]>([]);
  const [routerEvalRun, setRouterEvalRun] = useState<WorkflowRouterEvalRun | null>(null);
  const [agentMemory, setAgentMemory] = useState<readonly WorkflowAgentMemoryRecord[]>([]);
  const [auditExportNotice, setAuditExportNotice] = useState<string | null>(null);
  const [branches, setBranches] = useState<readonly WorkflowBranch[]>([]);
  const [activeBranchId, setActiveBranchId] = useState<string | null>(null);
  const [promptTurns, setPromptTurns] = useState<readonly WorkflowPromptTurn[]>([]);
  const branchNameDraft = defaultBranchName;
  const [branchRenameDraft, setBranchRenameDraft] = useState("");
  const [showArchivedBranches, setShowArchivedBranches] = useState(false);
  const [branchNotice, setBranchNotice] = useState<string | null>(null);
  const [mergeSourceBranchId, setMergeSourceBranchId] = useState<string>("");
  const [mergeMode, setMergeMode] = useState<"merge" | "cherry-pick">("merge");
  const [mergePreview, setMergePreview] = useState<WorkflowBranchMergePreview | null>(null);
  const [mergeResolutionModes, setMergeResolutionModes] = useState<
    Readonly<Record<string, "source" | "target" | "manual">>
  >({});
  const mergeResolutionModesRef = useRef<Readonly<Record<string, "source" | "target" | "manual">>>(
    {}
  );
  const mergeManualJsonRef = useRef<Readonly<Record<string, string>>>({});
  const [reuseDecisions, setReuseDecisions] = useState<
    readonly WorkflowGeneratedModuleReuseDecision[]
  >([]);
  const [dirtyNodeIds, setDirtyNodeIds] = useState<ReadonlySet<string>>(new Set());
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [selectedEdgeId, setSelectedEdgeId] = useState<string | null>(null);
  const [detailsOpen, setDetailsOpen] = useState(false);
  const [detailsTab, setDetailsTab] = useState<DetailsTab>("node");
  const [surfaceMode, setSurfaceMode] = useState<SurfaceMode>("governance");
  const [governanceSkillPath, setGovernanceSkillPath] = useState("./SKILL.md");
  const [governanceInputPath, setGovernanceInputPath] = useState("input.json");
  const [governancePolicy, setGovernancePolicy] = useState("sg-agentic-ai-baseline");
  const [governanceRunId, setGovernanceRunId] = useState("skill-run.local-review");
  const [governanceAgent, setGovernanceAgent] = useState("audit-only");
  const [governanceReplayAgents, setGovernanceReplayAgents] =
    useState("codex-cli,claude-code,goose");
  const [trajectoryRuns, setTrajectoryRuns] = useState<readonly AgentRunRecord[]>([]);
  const [selectedTrajectoryRunId, setSelectedTrajectoryRunId] = useState<string | null>(null);
  const [policyYaml, setPolicyYaml] = useState("rules:\n");
  const [policyNotice, setPolicyNotice] = useState<string | null>(null);
  const [trajectoryNotice, setTrajectoryNotice] = useState<string | null>(null);
  const [statusPopoverOpen, setStatusPopoverOpen] = useState(false);
  const [pendingNodeConnection, setPendingNodeConnection] = useState<PendingNodeConnection | null>(
    null
  );
  const [nodePrompt, setNodePrompt] = useState("");
  const [jsonError, setJsonError] = useState<string | null>(null);
  const [busyAction, setBusyAction] = useState<string | null>(null);
  const [apiError, setApiError] = useState<string | null>(null);
  const [promotionNotice, setPromotionNotice] = useState<string | null>(null);
  const [adminToken, setAdminToken] = useState(readKelpClawAdminToken);
  const [integrationReadiness, setIntegrationReadiness] = useState<readonly IntegrationReadiness[]>(
    []
  );
  const [secretMetadata, setSecretMetadata] = useState<readonly SecretMetadata[]>([]);
  const [googleConnected, setGoogleConnected] = useState<boolean | null>(null);
  const [secretDrafts, setSecretDrafts] = useState<Readonly<Record<string, string>>>({});
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [paletteQuery, setPaletteQuery] = useState("");
  const [paletteSelection, setPaletteSelection] = useState(0);
  const [paletteMode, setPaletteMode] = useState<CommandPaletteMode>({ kind: "commands" });
  const commandPaletteInputRef = useRef<HTMLInputElement | HTMLTextAreaElement>(null);
  const selectedNodeIdRef = useRef<string | null>(null);
  const selectedEdgeIdRef = useRef<string | null>(null);
  const deleteSelectionRef = useRef<() => void>(() => undefined);
  const nodeActionHandlersRef = useRef<NodeActionHandlers>({
    onInlineEdit: () => undefined,
    onSelectNode: () => undefined,
    onOpenDetails: () => undefined,
    onDeleteNode: () => undefined,
    onRepromptNode: () => undefined,
    onAddNextNode: () => undefined
  });

  nodeActionHandlersRef.current = {
    onInlineEdit: updateNodeInline,
    onSelectNode: selectNodeById,
    onOpenDetails: openNodeDetails,
    onDeleteNode: deleteNodeById,
    onRepromptNode: repromptSelectedNode,
    onAddNextNode: openConnectedNodePalette
  };
  deleteSelectionRef.current = deleteSelection;
  selectedNodeIdRef.current = selectedNodeId;
  selectedEdgeIdRef.current = selectedEdgeId;

  const validationIssues = validation.ok ? [] : validation.errors;
  const [nodes, setNodes, onNodesChangeBase] = useNodesState<WorkflowFlowNode>(
    workflowToNodes(workflow, validationIssues)
  );
  const [edges, setEdges, onEdgesChangeBase] = useEdgesState<WorkflowFlowEdge>(
    workflowToEdges(workflow, validationIssues)
  );

  const selectedNode = useMemo(
    () => workflow.nodes.find((node) => node.id === selectedNodeId) ?? null,
    [selectedNodeId, workflow.nodes]
  );
  const selectedEdge = useMemo(
    () => workflow.edges.find((edge) => edge.id === selectedEdgeId) ?? null,
    [selectedEdgeId, workflow.edges]
  );
  const selectedTrajectoryRun = useMemo(
    () => trajectoryRuns.find((runRecord) => runRecord.id === selectedTrajectoryRunId) ?? null,
    [selectedTrajectoryRunId, trajectoryRuns]
  );
  const skillGovernanceCommands = useMemo(
    () =>
      buildSkillGovernanceCommands({
        skillPath: governanceSkillPath,
        inputPath: governanceInputPath,
        policy: governancePolicy,
        runId: governanceRunId,
        agent: governanceAgent,
        replayAgents: governanceReplayAgents
      }),
    [
      governanceAgent,
      governanceInputPath,
      governancePolicy,
      governanceReplayAgents,
      governanceRunId,
      governanceSkillPath
    ]
  );
  const selectedTrajectoryRunStreamId = selectedTrajectoryRun?.id;
  const selectedTrajectoryRunStatus = selectedTrajectoryRun?.status;
  const flowNodes = nodes;
  const flowEdges = edges;
  const activeBranch = useMemo(
    () => branches.find((branch) => branch.id === activeBranchId) ?? null,
    [activeBranchId, branches]
  );
  const branchLifecycleLocked = activeBranch?.status === "archived";
  const activeRunnerDeployment = useMemo(
    () =>
      deploymentActivations?.activeDeployments.find(
        (deployment) =>
          deployment.kind === "runner.configuration" &&
          deployment.status === "deployed" &&
          (!approvedRevision || deployment.approvedRevisionId === approvedRevision.id)
      ) ?? null,
    [approvedRevision, deploymentActivations]
  );
  const visibleBranches = useMemo(
    () =>
      showArchivedBranches ? branches : branches.filter((branch) => branch.status !== "archived"),
    [branches, showArchivedBranches]
  );
  const mergeSources = useMemo(
    () =>
      branches.filter(
        (branch) =>
          branch.id !== activeBranchId && (showArchivedBranches || branch.status !== "archived")
      ),
    [activeBranchId, branches, showArchivedBranches]
  );
  const workflowHasGraph = workflow.nodes.length > 0;
  const clarificationReady =
    !clarification ||
    clarification.questions.every(
      (question) =>
        !question.required || (clarificationAnswers[question.id]?.trim().length ?? 0) > 0
    );
  const planDisabledReason = actionBlockedReason(busyAction, branchLifecycleLocked);
  const validateDisabledReason = actionBlockedReason(busyAction, branchLifecycleLocked);
  const acceptPlanDisabledReason = !workflowHasGraph
    ? "Plan or add nodes before accepting."
    : !validation.ok
      ? "Fix validation issues before accepting."
      : actionBlockedReason(busyAction, branchLifecycleLocked);
  const evaluateDisabledReason = !workflowHasGraph
    ? "Plan or add nodes before evaluating."
    : !validation.ok
      ? "Fix validation issues before evaluating."
      : actionBlockedReason(busyAction, branchLifecycleLocked);
  const approveDisabledReason = !workflowHasGraph
    ? "Plan or add nodes before approval."
    : !validation.ok
      ? "Fix validation issues before approval."
      : draftEvaluation?.readyForApproval !== true
        ? "Run a passing draft evaluation before approval."
        : actionBlockedReason(busyAction, branchLifecycleLocked);
  const deployDisabledReason = !approvedRevision
    ? "Approve a revision before deployment."
    : draftEvaluation?.readyForApproval !== true
      ? "Run a passing draft evaluation before deployment."
      : actionBlockedReason(busyAction, branchLifecycleLocked);
  const runDisabledReason = !approvedRevision
    ? "Approve a revision before running."
    : !activeRunnerDeployment
      ? "Deploy an active runner.configuration before running."
      : actionBlockedReason(busyAction, branchLifecycleLocked);
  const refreshIntegrations = useCallback(async () => {
    try {
      const [secrets, google] = await Promise.all([
        kelpClawApi.listSecrets(),
        kelpClawApi.googleStatus()
      ]);
      setSecretMetadata(secrets.secrets);
      setIntegrationReadiness(secrets.integrations);
      setGoogleConnected(google.connected);
    } catch (error) {
      setApiError(error instanceof Error ? error.message : "Integration status request failed.");
    }
  }, []);

  const refreshBranches = useCallback(
    async (workflowId: string, preferredBranchId?: string | undefined) => {
      if (workflowId === emptyWorkflowDraft.id) {
        setBranches([]);
        setActiveBranchId(null);
        setPromptTurns([]);
        return;
      }

      try {
        const response = await kelpClawApi.listBranches(workflowId);
        setBranches(response.branches);
        const nextActive =
          response.branches.find((branch) => branch.id === preferredBranchId) ??
          response.branches.find((branch) => branch.name.toLowerCase() === "main") ??
          response.branches[0] ??
          null;
        setActiveBranchId(nextActive?.id ?? null);
        setBranchRenameDraft(nextActive?.name ?? "");
        if (nextActive) {
          const branchResponse = await kelpClawApi.fetchBranch(workflowId, nextActive.id);
          setPromptTurns(branchResponse.promptTurns);
        }
      } catch {
        setBranches([]);
        setActiveBranchId(null);
        setPromptTurns([]);
      }
    },
    []
  );

  const refreshRuntimeStatus = useCallback(
    async (workflowId: string, branchId?: string | null | undefined) => {
      if (workflowId === emptyWorkflowDraft.id) {
        setRuntimeTruth(null);
        setBudgetPolicy(null);
        setBudgetLedgers([]);
        setAgentTimeline([]);
        setDeploymentActivations(null);
        setWorkflowRuns([]);
        setWorkflowSchedules([]);
        setOpsHealth(null);
        setRouterEvalCases([]);
        setRouterEvalRun(null);
        setAgentMemory([]);
        return;
      }

      try {
        const [
          truth,
          budget,
          timeline,
          active,
          runs,
          schedules,
          health,
          connectorList,
          routerEvals,
          memory
        ] = await Promise.all([
          kelpClawApi.fetchRuntimeTruth(workflowId, branchId ?? undefined),
          kelpClawApi.fetchBudget(workflowId, branchId ?? undefined),
          kelpClawApi.fetchAgentTimeline(workflowId),
          kelpClawApi.fetchActiveDeployments(workflowId),
          kelpClawApi.fetchRuns(workflowId),
          kelpClawApi.fetchSchedules(workflowId),
          kelpClawApi.fetchOpsHealth(),
          kelpClawApi.fetchConnectors(),
          kelpClawApi.fetchRouterEvals(),
          kelpClawApi.fetchAgentMemory(workflowId)
        ]);
        setRuntimeTruth(truth.truth);
        setBudgetPolicy(budget.policy);
        setBudgetLedgers(budget.ledgers);
        setAgentTimeline(timeline.events);
        setDeploymentActivations(active);
        setWorkflowRuns(runs.runs);
        setWorkflowSchedules(schedules.schedules);
        setOpsHealth(health.health);
        setConnectors(connectorList.connectors);
        setRouterEvalCases(routerEvals.cases);
        setRouterEvalRun(routerEvals.latestRun ?? null);
        setAgentMemory(memory.memories);
      } catch (error) {
        setApiError(error instanceof Error ? error.message : "Runtime status request failed.");
      }
    },
    []
  );

  const refreshTrajectoryRuns = useCallback(async () => {
    try {
      const response = await kelpClawApi.fetchAgentRuns();
      setTrajectoryRuns(response.runs);
      setSelectedTrajectoryRunId((current) => current ?? response.runs[0]?.id ?? null);
    } catch {
      setTrajectoryRuns([]);
    }
  }, []);

  const refreshPolicies = useCallback(async () => {
    try {
      const response = await kelpClawApi.fetchPolicies();
      const rules = Array.isArray(response.ruleset.rules) ? response.ruleset.rules : [];
      setPolicyYaml(
        rules.length > 0
          ? `rules:\n${rules
              .map((rule) =>
                [
                  `  - id: ${String((rule as JsonRecord).id ?? "")}`,
                  `    when: ${String((rule as JsonRecord).when ?? "")}`,
                  `    action: ${String((rule as JsonRecord).action ?? "")}`,
                  (rule as JsonRecord).approverRole
                    ? `    approverRole: ${String((rule as JsonRecord).approverRole)}`
                    : ""
                ]
                  .filter(Boolean)
                  .join("\n")
              )
              .join("\n")}\n`
          : "rules:\n"
      );
    } catch {
      setPolicyYaml("rules:\n");
    }
  }, []);

  useEffect(() => {
    const timeout = window.setTimeout(() => {
      void refreshIntegrations();
    }, 0);
    return () => window.clearTimeout(timeout);
  }, [adminToken, refreshIntegrations]);

  useEffect(() => {
    let cancelled = false;
    void kelpClawApi
      .fetchRuntimeProviders()
      .then((response) => {
        if (!cancelled) {
          setProviderConfigs(response.providers);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setProviderConfigs([]);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [adminToken]);

  useEffect(() => {
    const timeout = window.setTimeout(() => {
      void refreshBranches(workflow.id);
    }, 0);
    return () => window.clearTimeout(timeout);
  }, [refreshBranches, workflow.id]);

  useEffect(() => {
    const timeout = window.setTimeout(() => {
      void refreshRuntimeStatus(workflow.id, activeBranchId);
    }, 0);
    return () => window.clearTimeout(timeout);
  }, [activeBranchId, refreshRuntimeStatus, workflow.id, workflow.revision]);

  useEffect(() => {
    const timeout = window.setTimeout(() => {
      void refreshTrajectoryRuns();
    }, 0);
    return () => window.clearTimeout(timeout);
  }, [adminToken, refreshTrajectoryRuns]);

  useEffect(() => {
    const timeout = window.setTimeout(() => {
      void refreshPolicies();
    }, 0);
    return () => window.clearTimeout(timeout);
  }, [adminToken, refreshPolicies]);

  useEffect(() => {
    if (!selectedTrajectoryRunStreamId || selectedTrajectoryRunStatus !== "recording") {
      return;
    }
    let cancelled = false;
    void kelpClawApi.streamAgentRunEvents(selectedTrajectoryRunStreamId, (event) => {
      if (cancelled) {
        return;
      }
      setTrajectoryRuns((current) =>
        current.map((candidate) => {
          if (candidate.id !== selectedTrajectoryRunStreamId) {
            return candidate;
          }
          if ("events" in event) {
            return event;
          }
          if (candidate.events.some((existing) => existing.id === event.id)) {
            return candidate;
          }
          return { ...candidate, events: [...candidate.events, event] };
        })
      );
    });
    return () => {
      cancelled = true;
    };
  }, [selectedTrajectoryRunStatus, selectedTrajectoryRunStreamId]);

  useEffect(() => {
    if (!selectedNodeId || workflow.id === emptyWorkflowDraft.id) {
      const timeout = window.setTimeout(() => setNodeDecisionTraces([]), 0);
      return () => window.clearTimeout(timeout);
    }

    let cancelled = false;
    void kelpClawApi
      .fetchNodeDecisionTraces(workflow.id, selectedNodeId)
      .then((response) => {
        if (!cancelled) {
          setNodeDecisionTraces(response.traces);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setNodeDecisionTraces([]);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [selectedNodeId, workflow.id, workflow.revision]);

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "p") {
        event.preventDefault();
        openPalette();
        return;
      }

      if (paletteOpen || isEditableTarget(event.target)) {
        return;
      }

      if (event.key === "Enter" && (selectedNodeId || selectedEdgeId)) {
        event.preventDefault();
        setDetailsTab("node");
        setDetailsOpen(true);
        return;
      }

      if (
        (event.key === "Delete" || event.key === "Backspace") &&
        (selectedNodeId || selectedEdgeId)
      ) {
        event.preventDefault();
        deleteSelectionRef.current();
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [paletteOpen, selectedEdgeId, selectedNodeId, workflow]);

  useEffect(() => {
    const selectedNodeStillExists =
      !selectedNodeId || workflow.nodes.some((node) => node.id === selectedNodeId);
    const selectedEdgeStillExists =
      !selectedEdgeId || workflow.edges.some((edge) => edge.id === selectedEdgeId);
    if (selectedNodeStillExists && selectedEdgeStillExists) {
      return;
    }

    const timeout = window.setTimeout(() => {
      if (!selectedNodeStillExists) {
        setSelectedNodeId(null);
        setNodePrompt("");
      }

      if (!selectedEdgeStillExists) {
        setSelectedEdgeId(null);
      }

      if (
        (!selectedNodeStillExists && selectedNodeId) ||
        (!selectedEdgeStillExists && selectedEdgeId)
      ) {
        setDetailsOpen(false);
      }
    }, 0);
    return () => window.clearTimeout(timeout);
  }, [selectedEdgeId, selectedNodeId, workflow.edges, workflow.nodes]);

  useEffect(() => {
    if (!paletteOpen) {
      return;
    }

    const timeout = window.setTimeout(() => {
      commandPaletteInputRef.current?.focus();
      commandPaletteInputRef.current?.select();
    }, 0);
    return () => window.clearTimeout(timeout);
  }, [paletteMode.kind, paletteOpen]);

  const loadWorkflow = useCallback(
    (
      nextWorkflow: WorkflowSpec,
      nextValidation: WorkflowValidationResult = validateWorkflowSpec(nextWorkflow)
    ) => {
      const issues = nextValidation.ok ? [] : nextValidation.errors;
      setWorkflow(nextWorkflow);
      setValidation(nextValidation);
      setNodes(
        applyNodeSelection(
          attachNodeCallbacks(workflowToNodes(nextWorkflow, issues)),
          selectedNodeIdRef.current
        )
      );
      setEdges(
        applyEdgeSelection(workflowToEdges(nextWorkflow, issues), selectedEdgeIdRef.current)
      );
    },
    [setEdges, setNodes]
  );

  const startTrackedJob = useCallback(
    async (request: {
      readonly type: WorkflowJob["type"];
      readonly workflowId?: string;
      readonly revisionId?: string;
      readonly nodeId?: string;
      readonly maxAttempts?: number;
    }) => {
      const response = await kelpClawApi.createJob(request);
      setActiveJob(response.job);
      void kelpClawApi.streamJobEvents(response.job.id, (event) => {
        if ("status" in event) {
          setActiveJob(event);
        } else {
          setActiveJob((current) =>
            current ? { ...current, events: [...current.events, event] } : current
          );
        }
      });
      return response.job;
    },
    []
  );

  const requestPlannerFeedback = useCallback(
    async (baseWorkflow: WorkflowSpec, editedWorkflow: WorkflowSpec) => {
      if (baseWorkflow.id !== editedWorkflow.id) {
        return;
      }

      try {
        const job = await startTrackedJob({
          type: "feedback.graph",
          workflowId: editedWorkflow.id
        });
        const response = await kelpClawApi.feedback(
          editedWorkflow.id,
          {
            baseWorkflow,
            editedWorkflow,
            prompt
          },
          job.id
        );
        setPlannerFeedback(response.feedback);
        setTaskRoute(response.feedback.route);
      } catch {
        // Local edits can occur before the draft has been persisted through the API.
      }
    },
    [prompt, startTrackedJob]
  );

  const updateLocalWorkflow = useCallback(
    (nextWorkflow: WorkflowSpec) => {
      const previousWorkflow = workflow;
      setApprovedRevision(null);
      setApprovalDiff(null);
      setRun(null);
      setPromotionNotice(null);
      setDraftEvaluation(null);
      setDeploymentNotice(null);
      loadWorkflow(nextWorkflow);
      void requestPlannerFeedback(previousWorkflow, nextWorkflow);
    },
    [loadWorkflow, requestPlannerFeedback, workflow]
  );

  async function executeApiAction(action: string, work: () => Promise<void>) {
    setBusyAction(action);
    setApiError(null);
    try {
      await work();
    } catch (error) {
      setApiError(error instanceof Error ? error.message : "KelpClaw request failed.");
    } finally {
      setBusyAction(null);
    }
  }

  function updateAdminToken(value: string) {
    setAdminToken(value);
    saveKelpClawAdminToken(value);
  }

  function savePolicyYaml() {
    void executeApiAction("policy-save", async () => {
      const response = await kelpClawApi.updatePolicyYaml(policyYaml);
      setPolicyNotice(
        `${Array.isArray(response.ruleset.rules) ? response.ruleset.rules.length : 0} rules saved.`
      );
      await refreshPolicies();
    });
  }

  function approveTrajectoryEvent(runId: string, event: AgentStepEvent) {
    void executeApiAction(`agent-approve-${event.id}`, async () => {
      const response = await kelpClawApi.approveAgentRunEvent(runId, event.id, {
        reviewedBy: "kelpclaw"
      });
      replaceTrajectoryRun(response.run);
    });
  }

  function denyTrajectoryEvent(runId: string, event: AgentStepEvent) {
    void executeApiAction(`agent-deny-${event.id}`, async () => {
      const response = await kelpClawApi.denyAgentRunEvent(runId, event.id, {
        reviewedBy: "kelpclaw"
      });
      replaceTrajectoryRun(response.run);
    });
  }

  function anchorTrajectoryRun(runId: string) {
    void executeApiAction(`agent-anchor-${runId}`, async () => {
      const response = await kelpClawApi.anchorAgentRun(runId);
      replaceTrajectoryRun(response.run);
      setTrajectoryNotice(
        response.externalAnchor.enabled
          ? `Anchored ${response.anchor.chainHead.slice(0, 24)} · ${response.externalAnchor.status}`
          : `Anchored ${response.anchor.chainHead.slice(0, 24)}`
      );
    });
  }

  function replaceTrajectoryRun(runRecord: AgentRunRecord) {
    setTrajectoryRuns((current) =>
      current.some((candidate) => candidate.id === runRecord.id)
        ? current.map((candidate) => (candidate.id === runRecord.id ? runRecord : candidate))
        : [runRecord, ...current]
    );
  }

  function saveSecret(secretName: string, valueOverride?: string | undefined) {
    const value = (valueOverride ?? secretDrafts[secretName] ?? "").trim();
    if (!value) {
      setApiError(`Secret '${secretName}' requires a value.`);
      return;
    }

    void executeApiAction(`secret-${secretName}`, async () => {
      await kelpClawApi.upsertSecret(secretName, value);
      setSecretDrafts((previous) => ({
        ...previous,
        [secretName]: ""
      }));
      await refreshIntegrations();
    });
  }

  function deleteSecret(secretName: string) {
    void executeApiAction(`delete-secret-${secretName}`, async () => {
      await kelpClawApi.deleteSecret(secretName);
      await refreshIntegrations();
    });
  }

  function switchBranch(branchId: string) {
    void executeApiAction("switch-branch", async () => {
      const response = await kelpClawApi.fetchBranch(workflow.id, branchId);
      setActiveBranchId(response.branch.id);
      setBranchRenameDraft(response.branch.name);
      setPromptTurns(response.promptTurns);
      setMergePreview(null);
      setMergeResolutionModes({});
      mergeResolutionModesRef.current = {};
      mergeManualJsonRef.current = {};
      setReuseDecisions([]);
      loadWorkflow(response.headDraftRevision.workflow, response.headDraftRevision.validation);
      setApprovedRevision(null);
      setApprovalDiff(null);
      setDraftEvaluation(null);
      setRun(null);
      setDeploymentNotice(null);
      setPlanAcceptedNotice(null);
      setBranchNotice(`Switched to ${response.branch.name}`);
      await refreshRuntimeStatus(response.branch.workflowId, response.branch.id);
    });
  }

  function forkBranch(nameOverride?: string | undefined) {
    const name = (nameOverride ?? branchNameDraft).trim();
    if (!name) {
      setApiError("Branch name is required.");
      return;
    }
    if (branchLifecycleLocked) {
      setApiError("Archived branches are read-only.");
      return;
    }

    void executeApiAction("fork-branch", async () => {
      const response = await kelpClawApi.createBranch(workflow.id, {
        name,
        createdBy: "owner@example.com",
        ...(activeBranchId ? { fromBranchId: activeBranchId } : {})
      });
      await refreshBranches(workflow.id, response.branch.id);
      setActiveBranchId(response.branch.id);
      setBranchRenameDraft(response.branch.name);
      setPromptTurns([]);
      setMergePreview(null);
      setMergeResolutionModes({});
      mergeResolutionModesRef.current = {};
      mergeManualJsonRef.current = {};
      setReuseDecisions([]);
      loadWorkflow(response.draftRevision.workflow, response.draftRevision.validation);
      setBranchNotice(`Forked ${response.branch.name}`);
      await refreshRuntimeStatus(response.branch.workflowId, response.branch.id);
    });
  }

  function renameBranch(nameOverride?: string | undefined) {
    if (!activeBranch) {
      setApiError("Select a branch before renaming it.");
      return;
    }
    const name = (nameOverride ?? branchRenameDraft).trim();
    if (!name) {
      setApiError("Branch name is required.");
      return;
    }

    void executeApiAction("rename-branch", async () => {
      const response = await kelpClawApi.updateBranch(workflow.id, activeBranch.id, {
        name,
        updatedBy: "owner@example.com"
      });
      setBranches((previous) =>
        previous.map((branch) => (branch.id === response.branch.id ? response.branch : branch))
      );
      setBranchRenameDraft(response.branch.name);
      setBranchNotice(`Renamed branch to ${response.branch.name}`);
    });
  }

  function toggleBranchArchive() {
    if (!activeBranch) {
      setApiError("Select a branch before changing archive status.");
      return;
    }

    void executeApiAction("archive-branch", async () => {
      const nextStatus = activeBranch.status === "archived" ? "active" : "archived";
      const response = await kelpClawApi.updateBranch(workflow.id, activeBranch.id, {
        status: nextStatus,
        updatedBy: "owner@example.com"
      });
      setBranches((previous) =>
        previous.map((branch) => (branch.id === response.branch.id ? response.branch : branch))
      );
      setBranchRenameDraft(response.branch.name);
      if (nextStatus === "archived") {
        setShowArchivedBranches(true);
      }
      setBranchNotice(
        `${nextStatus === "archived" ? "Archived" : "Restored"} ${response.branch.name}`
      );
    });
  }

  function previewMerge(
    sourceBranchIdOverride?: string | undefined,
    modeOverride?: "merge" | "cherry-pick" | undefined
  ) {
    const sourceBranchId = sourceBranchIdOverride ?? mergeSourceBranchId;
    const selectedMergeMode = modeOverride ?? mergeMode;
    if (!activeBranchId || !sourceBranchId) {
      setApiError("Choose an active branch and a source branch before previewing a merge.");
      return;
    }
    if (branchLifecycleLocked) {
      setApiError("Archived branches are read-only.");
      return;
    }
    if (mergeSources.find((branch) => branch.id === sourceBranchId)?.status === "archived") {
      setApiError("Archived branches cannot be merged.");
      return;
    }

    void executeApiAction("merge-preview", async () => {
      const response = await kelpClawApi.previewBranchMerge(workflow.id, sourceBranchId, {
        targetBranchId: activeBranchId,
        mode: selectedMergeMode
      });
      setMergeSourceBranchId(sourceBranchId);
      setMergeMode(selectedMergeMode);
      setMergePreview(response.preview);
      setBranchNotice(
        `${selectedMergeMode === "cherry-pick" ? "Cherry-pick" : "Merge"} preview ${response.preview.status}: ${response.preview.summary.join("; ")}`
      );
      setMergeResolutionModes({});
      mergeResolutionModesRef.current = {};
      mergeManualJsonRef.current = {};
    });
  }

  function updateMergeResolutionMode(conflictId: string, mode: "source" | "target" | "manual") {
    const next = {
      ...mergeResolutionModesRef.current,
      [conflictId]: mode
    };
    mergeResolutionModesRef.current = next;
    setMergeResolutionModes(next);
  }

  function applyMerge() {
    if (!activeBranchId || !mergePreview) {
      return;
    }
    if (branchLifecycleLocked) {
      setApiError("Archived branches are read-only.");
      return;
    }
    const resolutionModes = mergeResolutionModesRef.current;
    const manualJson = mergeManualJsonRef.current;
    if (!mergePreview.conflicts.every((conflict) => resolutionModes[conflict.id])) {
      setApiError("Resolve every merge conflict before applying.");
      return;
    }

    void executeApiAction("branch-merge", async () => {
      const resolutions = mergePreview.conflicts.map((conflict) =>
        mergeResolutionForConflict(conflict, resolutionModes, manualJson)
      );
      const response = await kelpClawApi.mergeBranch(workflow.id, mergePreview.sourceBranchId, {
        targetBranchId: activeBranchId,
        mode: mergePreview.mode,
        appliedBy: "owner@example.com",
        resolutions
      });
      loadWorkflow(response.workflow, response.validation);
      setActiveBranchId(response.branch.id);
      setMergePreview(null);
      setMergeResolutionModes({});
      mergeResolutionModesRef.current = {};
      mergeManualJsonRef.current = {};
      setReuseDecisions([]);
      setDraftEvaluation(null);
      setApprovedRevision(null);
      setApprovalDiff(null);
      await refreshBranches(response.workflow.id, response.branch.id);
      setBranchNotice(
        `${response.merge.mode === "cherry-pick" ? "Cherry-picked" : "Merged"} ${response.merge.sourceBranchId}`
      );
    });
  }

  function refreshReuseCandidates() {
    if (!activeBranchId) {
      setApiError("Select a branch before checking generated module reuse.");
      return;
    }

    void executeApiAction("reuse-candidates", async () => {
      const response = await kelpClawApi.fetchReuseCandidates(workflow.id, activeBranchId);
      setReuseDecisions(response.decisions);
      setBranchNotice(`Reuse candidates: ${response.decisions.length}`);
    });
  }

  function connectGoogle() {
    void executeApiAction("google-connect", async () => {
      const response = await kelpClawApi.googleConnect();
      globalThis.location.assign(response.url);
    });
  }

  function revokeGoogle() {
    void executeApiAction("google-revoke", async () => {
      await kelpClawApi.googleRevoke();
      await refreshIntegrations();
    });
  }

  function markDirty(nodeId: string) {
    setDirtyNodeIds((previous) => new Set([...previous, nodeId]));
  }

  function updateNode(nodeId: string, updater: (node: WorkflowNode) => WorkflowNode) {
    const nextWorkflow: WorkflowSpec = {
      ...workflow,
      approval: null,
      nodes: workflow.nodes.map((node) => (node.id === nodeId ? updater(node) : node))
    };
    markDirty(nodeId);
    updateLocalWorkflow(nextWorkflow);
  }

  function attachNodeCallbacks(nextNodes: readonly WorkflowFlowNode[]): WorkflowFlowNode[] {
    return nextNodes.map((node) => ({
      ...node,
      data: {
        ...node.data,
        onInlineEdit: (nodeId, patch) => nodeActionHandlersRef.current.onInlineEdit(nodeId, patch),
        onSelectNode: (nodeId) => nodeActionHandlersRef.current.onSelectNode(nodeId),
        onOpenDetails: (nodeId) => nodeActionHandlersRef.current.onOpenDetails(nodeId),
        onDeleteNode: (nodeId) => nodeActionHandlersRef.current.onDeleteNode(nodeId),
        onRepromptNode: (nodeId) => nodeActionHandlersRef.current.onRepromptNode(nodeId),
        onAddNextNode: (nodeId, outputPort) =>
          nodeActionHandlersRef.current.onAddNextNode(nodeId, outputPort)
      }
    }));
  }

  function updateFlowSelection(nodeId: string | null, edgeId: string | null) {
    setNodes((currentNodes) => applyNodeSelection(currentNodes, nodeId));
    setEdges((currentEdges) => applyEdgeSelection(currentEdges, edgeId));
  }

  function updateNodeInline(nodeId: string, patch: Pick<WorkflowNode, "label" | "description">) {
    updateNode(nodeId, (node) => ({
      ...node,
      label: patch.label,
      description: patch.description
    }));
  }

  function deleteNodeById(nodeId: string) {
    updateLocalWorkflow({
      ...workflow,
      approval: null,
      nodes: workflow.nodes.filter((node) => node.id !== nodeId),
      edges: workflow.edges.filter(
        (edge) => edge.source.nodeId !== nodeId && edge.target.nodeId !== nodeId
      )
    });
    if (selectedNodeId === nodeId) {
      setSelectedNodeId(null);
      setNodePrompt("");
      setDetailsOpen(false);
    }
  }

  function repromptSelectedNode(nodeId: string) {
    const node = workflow.nodes.find((candidate) => candidate.id === nodeId);
    if (!node) {
      return;
    }
    if (branchLifecycleLocked) {
      setApiError("Archived branches are read-only.");
      return;
    }

    setSelectedNodeId(nodeId);
    setSelectedEdgeId(null);
    setNodePrompt(node.description);

    void executeApiAction("reprompt", async () => {
      const response = activeBranchId
        ? await kelpClawApi.repromptBranchNode(workflow.id, activeBranchId, {
            nodeId: node.id,
            prompt: node.description,
            currentWorkflow: workflow,
            actor: "owner@example.com"
          })
        : await kelpClawApi.repromptNode(workflow.id, {
            nodeId: node.id,
            prompt: node.description,
            currentWorkflow: workflow
          });
      loadWorkflow(response.workflow, response.validation);
      setApprovalDiff(response.diff);
      if ("branch" in response) {
        const branchResponse = response as Awaited<
          ReturnType<typeof kelpClawApi.repromptBranchNode>
        >;
        setPromptTurns((previous) => [...previous, branchResponse.promptTurn]);
        await refreshBranches(branchResponse.workflow.id, branchResponse.branch.id);
      }
      markDirty(node.id);
      setPromotionNotice(null);
    });
  }

  function onNodesChange(changes: NodeChange<WorkflowFlowNode>[]) {
    onNodesChangeBase(changes);
  }

  function onEdgesChange(changes: EdgeChange<WorkflowFlowEdge>[]) {
    onEdgesChangeBase(changes);
  }

  function onNodeDragStop(_: unknown, node: WorkflowFlowNode) {
    updateNode(node.id, (workflowNode) => ({
      ...workflowNode,
      config: {
        ...workflowNode.config,
        canvas: {
          x: Math.round(node.position.x),
          y: Math.round(node.position.y)
        }
      }
    }));
  }

  function onConnect(connection: Connection) {
    if (!connection.source || !connection.target) {
      return;
    }

    const sourceNode = workflow.nodes.find((node) => node.id === connection.source);
    const targetNode = workflow.nodes.find((node) => node.id === connection.target);
    const sourcePort =
      connection.sourceHandle ?? (sourceNode ? firstOutputPort(sourceNode) : undefined);
    const targetPort =
      connection.targetHandle ?? (targetNode ? firstInputPort(targetNode) : undefined);
    if (!sourceNode || !targetNode || !sourcePort || !targetPort) {
      return;
    }

    const edge = createWorkflowEdge({
      sourceNodeId: sourceNode.id,
      sourcePort,
      targetNodeId: targetNode.id,
      targetPort,
      id: uniqueEdgeId(sourceNode.id, targetNode.id, workflow.edges)
    });
    updateLocalWorkflow({
      ...workflow,
      approval: null,
      edges: [...workflow.edges, edge]
    });
  }

  function pendingEdgeForNewNode(node: WorkflowNode): WorkflowSpec["edges"][number] | null {
    if (!pendingNodeConnection) {
      return null;
    }

    const sourceNode = workflow.nodes.find(
      (candidate) => candidate.id === pendingNodeConnection.sourceNodeId
    );
    const sourcePort =
      pendingNodeConnection.outputPort ?? (sourceNode ? firstOutputPort(sourceNode) : undefined);
    const targetPort = firstInputPort(node);
    if (!sourceNode || !sourcePort || !targetPort) {
      return null;
    }

    return createWorkflowEdge({
      sourceNodeId: sourceNode.id,
      sourcePort,
      targetNodeId: node.id,
      targetPort,
      id: uniqueEdgeId(sourceNode.id, node.id, workflow.edges)
    });
  }

  function addComponentNode(item: ComponentPaletteItem) {
    if (branchLifecycleLocked) {
      setApiError("Archived branches are read-only.");
      return;
    }

    const id = uniqueComponentNodeId(item.id, workflow.nodes);
    const position = nextNodePosition(nodes);
    const node = createWorkflowNode({
      id,
      kind: item.kind,
      label: item.label,
      description: item.description,
      ...(item.inputs ? { inputs: item.inputs } : {}),
      ...(item.outputs ? { outputs: item.outputs } : {}),
      config: {
        ...(item.config ?? {}),
        canvas: position
      },
      ...(item.skillId ? { skillId: item.skillId } : {}),
      ...(item.adapterId ? { adapterId: item.adapterId } : {}),
      ...(item.adapterIds ? { adapterIds: item.adapterIds } : {}),
      ...(item.adapterOperations ? { adapterOperations: item.adapterOperations } : {}),
      ...(item.secretRefs ? { secretRefs: item.secretRefs } : {}),
      ...(item.agentic ? { agentic: item.agentic } : {})
    });
    const pendingEdge = pendingEdgeForNewNode(node);
    updateLocalWorkflow({
      ...workflow,
      approval: null,
      nodes: [...workflow.nodes, node],
      edges: pendingEdge ? [...workflow.edges, pendingEdge] : workflow.edges
    });
    setSelectedNodeId(id);
    setSelectedEdgeId(null);
    setNodePrompt(node.description);
    setJsonError(null);
    markDirty(id);
  }

  function deleteSelection() {
    if (selectedNodeId) {
      deleteNodeById(selectedNodeId);
      return;
    }

    if (selectedEdgeId) {
      updateLocalWorkflow({
        ...workflow,
        approval: null,
        edges: workflow.edges.filter((edge) => edge.id !== selectedEdgeId)
      });
      setSelectedEdgeId(null);
      setDetailsOpen(false);
    }
  }

  function onNodesDelete(deletedNodes: WorkflowFlowNode[]) {
    const deletedIds = new Set(deletedNodes.map((node) => node.id));
    updateLocalWorkflow({
      ...workflow,
      approval: null,
      nodes: workflow.nodes.filter((node) => !deletedIds.has(node.id)),
      edges: workflow.edges.filter(
        (edge) => !deletedIds.has(edge.source.nodeId) && !deletedIds.has(edge.target.nodeId)
      )
    });
    if (selectedNodeId && deletedIds.has(selectedNodeId)) {
      setSelectedNodeId(null);
      setNodePrompt("");
      setDetailsOpen(false);
    }
  }

  function onEdgesDelete(deletedEdges: Edge[]) {
    const deletedIds = new Set(deletedEdges.map((edge) => edge.id));
    updateLocalWorkflow({
      ...workflow,
      approval: null,
      edges: workflow.edges.filter((edge) => !deletedIds.has(edge.id))
    });
    if (selectedEdgeId && deletedIds.has(selectedEdgeId)) {
      setSelectedEdgeId(null);
      setDetailsOpen(false);
    }
  }

  function selectIssue(issue: WorkflowValidationIssue) {
    const [collection, index] = issue.path;
    if (collection === "nodes" && typeof index === "number") {
      setSelectedNodeId(workflow.nodes[index]?.id ?? null);
      setSelectedEdgeId(null);
      setDetailsTab("config");
      setDetailsOpen(true);
    } else if (collection === "edges" && typeof index === "number") {
      setSelectedEdgeId(workflow.edges[index]?.id ?? null);
      setSelectedNodeId(null);
      setDetailsTab("node");
      setDetailsOpen(true);
    }
  }

  function updateJsonField(nodeId: string, field: "config" | "inputs" | "outputs", value: string) {
    const parsed = parseJsonRecord(value);
    if (!parsed.ok) {
      setJsonError(parsed.error);
      return;
    }

    setJsonError(null);
    updateNode(nodeId, (node) => ({
      ...node,
      [field]: parsed.value
    }));
  }

  function updateClarificationAnswer(questionId: string, value: string) {
    setClarificationAnswers((previous) => ({
      ...previous,
      [questionId]: value
    }));
  }

  function planDraft(
    promptOverride?: string | undefined,
    clarificationAnswersOverride?: Readonly<Record<string, string>> | undefined
  ) {
    const nextPrompt = (promptOverride ?? prompt).trim();
    const nextClarificationAnswers = clarificationAnswersOverride ?? clarificationAnswers;
    if (!nextPrompt) {
      setApiError("Workflow prompt is required.");
      return;
    }
    if (branchLifecycleLocked) {
      setApiError("Archived branches are read-only.");
      return;
    }
    void executeApiAction("plan", async () => {
      const currentWorkflow = isEmptyStarterWorkflow(workflow) ? undefined : workflow;
      const job = await startTrackedJob({
        type: "plan.workflow",
        ...(currentWorkflow ? { workflowId: currentWorkflow.id } : {})
      });
      const response = activeBranchId
        ? await kelpClawApi.planBranch(
            workflow.id,
            activeBranchId,
            {
              prompt: nextPrompt,
              ...(currentWorkflow ? { currentWorkflow } : {}),
              preserveNodeIds: [...dirtyNodeIds],
              ...(clarification
                ? {
                    clarificationRequestId: clarification.id,
                    clarificationAnswers: clarification.questions.map((question) => ({
                      questionId: question.id,
                      answer: nextClarificationAnswers[question.id] ?? ""
                    }))
                  }
                : {}),
              actor: "owner@example.com"
            },
            job.id
          )
        : await kelpClawApi.plan(
            {
              prompt: nextPrompt,
              ...(currentWorkflow ? { currentWorkflow } : {}),
              preserveNodeIds: [...dirtyNodeIds],
              ...(clarification
                ? {
                    clarificationRequestId: clarification.id,
                    clarificationAnswers: clarification.questions.map((question) => ({
                      questionId: question.id,
                      answer: nextClarificationAnswers[question.id] ?? ""
                    }))
                  }
                : {})
            },
            job.id
          );
      if (response.status === "clarification-required") {
        setPrompt(nextPrompt);
        setClarification(response.clarification);
        setClarificationAnswers({});
        setTaskRoute(response.route);
        setPlannerFeedback(null);
        setReuseDecisions([]);
        setPaletteMode({ kind: "clarification" });
        setPaletteOpen(true);
        return;
      }
      setPrompt(nextPrompt);
      loadWorkflow(response.workflow, response.validation);
      setClarification(null);
      setClarificationAnswers({});
      setTaskRoute(response.route);
      setPlannerFeedback(null);
      setReuseDecisions([]);
      if (isBranchPlanSuccessResponse(response)) {
        setActiveBranchId(response.branch.id);
        setPromptTurns((previous) => [...previous, response.promptTurn]);
        await refreshBranches(response.workflow.id, response.branch.id);
        await refreshRuntimeStatus(response.workflow.id, response.branch.id);
      } else {
        await refreshBranches(response.workflow.id);
        await refreshRuntimeStatus(response.workflow.id, activeBranchId);
      }
      setDraftEvaluation(null);
      setSelectedNodeId(null);
      setSelectedEdgeId(null);
      setDirtyNodeIds(new Set());
      setApprovedRevision(null);
      setApprovalDiff(null);
      setRun(null);
      setPromotionNotice(null);
      setNodePrompt("");
      setDetailsOpen(false);
    });
  }

  function validateDraft() {
    if (branchLifecycleLocked) {
      setApiError("Archived branches are read-only.");
      return;
    }
    void executeApiAction("validate", async () => {
      const response = await kelpClawApi.validate(workflow.id, { workflow });
      setValidation(response.validation);
      if (response.workflow) {
        loadWorkflow(response.workflow, response.validation);
      }
      await refreshRuntimeStatus(workflow.id, activeBranchId);
    });
  }

  function evaluateDraft() {
    if (branchLifecycleLocked) {
      setApiError("Archived branches are read-only.");
      return;
    }
    void executeApiAction("evaluate-draft", async () => {
      const job = await startTrackedJob({
        type: "evaluate.draft",
        workflowId: workflow.id
      });
      const response = await kelpClawApi.evaluateDraft(
        workflow.id,
        {
          workflow,
          mockOnly: true,
          ...(activeBranchId ? { branchId: activeBranchId } : {})
        },
        job.id
      );
      setDraftEvaluation(response.evaluation);
      setPlannerFeedback((previous) =>
        previous
          ? {
              ...previous,
              suggestions: [...previous.suggestions, ...response.evaluation.suggestions]
            }
          : previous
      );
      await refreshRuntimeStatus(workflow.id, activeBranchId);
    });
  }

  function updateSuggestionDecision(suggestionId: string, status: "accepted" | "rejected") {
    void executeApiAction("feedback-decision", async () => {
      if (!plannerFeedback) {
        return;
      }
      const response = await kelpClawApi.decideSuggestion(
        workflow.id,
        plannerFeedback.id,
        suggestionId,
        {
          suggestionId,
          decision: status
        }
      );
      setPlannerFeedback(response.feedback);
    });
  }

  function repromptNode() {
    if (!selectedNode) {
      return;
    }
    if (branchLifecycleLocked) {
      setApiError("Archived branches are read-only.");
      return;
    }

    void executeApiAction("reprompt", async () => {
      const response = activeBranchId
        ? await kelpClawApi.repromptBranchNode(workflow.id, activeBranchId, {
            nodeId: selectedNode.id,
            prompt: nodePrompt,
            currentWorkflow: workflow,
            actor: "owner@example.com"
          })
        : await kelpClawApi.repromptNode(workflow.id, {
            nodeId: selectedNode.id,
            prompt: nodePrompt,
            currentWorkflow: workflow
          });
      loadWorkflow(response.workflow, response.validation);
      setApprovalDiff(response.diff);
      if ("branch" in response) {
        const branchResponse = response as Awaited<
          ReturnType<typeof kelpClawApi.repromptBranchNode>
        >;
        setPromptTurns((previous) => [...previous, branchResponse.promptTurn]);
        await refreshBranches(branchResponse.workflow.id, branchResponse.branch.id);
      }
      markDirty(selectedNode.id);
      setPromotionNotice(null);
    });
  }

  function reviewCodegenNode() {
    if (!selectedNode || selectedNode.kind !== "codegen") {
      return;
    }
    if (branchLifecycleLocked) {
      setApiError("Archived branches are read-only.");
      return;
    }

    void executeApiAction("review-codegen", async () => {
      const response = await kelpClawApi.reviewCodegen(workflow.id, selectedNode.id, {
        status: "approved",
        reviewedBy: "owner@example.com",
        ...(activeBranchId ? { branchId: activeBranchId } : {})
      });
      loadWorkflow(response.workflow, response.validation);
      setApprovedRevision(null);
      setApprovalDiff(null);
      setRun(null);
      setPromotionNotice(null);
    });
  }

  function promoteCodegenNode() {
    if (!selectedNode || selectedNode.kind !== "codegen") {
      return;
    }
    if (branchLifecycleLocked) {
      setApiError("Archived branches are read-only.");
      return;
    }

    void executeApiAction("promote-codegen", async () => {
      const response = await kelpClawApi.promoteCodegen(workflow.id, selectedNode.id);
      setPromotionNotice(`Promoted ${response.skill.name}`);
    });
  }

  function buildCodegenNode() {
    if (!selectedNode || selectedNode.kind !== "codegen") {
      return;
    }
    if (branchLifecycleLocked) {
      setApiError("Archived branches are read-only.");
      return;
    }

    void executeApiAction("build-codegen", async () => {
      const job = await startTrackedJob({
        type: "build.codegen-node",
        workflowId: workflow.id,
        nodeId: selectedNode.id
      });
      const response = await kelpClawApi.buildCodegen(
        workflow.id,
        selectedNode.id,
        {
          maxIterations: 3,
          maxReimplementationAttempts: 2,
          maxWallClockSeconds: 600,
          maxModelCostUsd: 2,
          runTestsInDocker: true,
          ...(activeBranchId ? { branchId: activeBranchId } : {})
        },
        job.id
      );
      loadWorkflow(response.workflow, response.validation);
      setActiveJob(response.job);
      setWorkspace(response.workspace);
      setAgentRuns(response.agentRuns);
      setDraftEvaluation(null);
      setPromotionNotice(null);
      await refreshRuntimeStatus(response.workflow.id, activeBranchId);
    });
  }

  function approveWorkflow() {
    if (branchLifecycleLocked) {
      setApiError("Archived branches are read-only.");
      return;
    }
    void executeApiAction("approve", async () => {
      const response = await kelpClawApi.approve(workflow.id, {
        workflow,
        approvedBy: "owner@example.com",
        ...(activeBranchId ? { branchId: activeBranchId } : {})
      });
      setApprovedRevision(response.approvedRevision);
      setApprovalDiff(response.diff);
      loadWorkflow(response.workflow, validateWorkflowSpec(response.workflow));
      await refreshRuntimeStatus(response.workflow.id, activeBranchId);
    });
  }

  function acceptPlanShape() {
    if (branchLifecycleLocked) {
      setApiError("Archived branches are read-only.");
      return;
    }
    void executeApiAction("accept-plan", async () => {
      const response = activeBranchId
        ? await kelpClawApi.acceptBranchPlan(workflow.id, activeBranchId, {
            workflow,
            acceptedBy: "owner@example.com"
          })
        : await kelpClawApi.acceptPlan(workflow.id, {
            workflow,
            acceptedBy: "owner@example.com"
          });
      loadWorkflow(response.workflow, response.validation);
      setPlanAcceptedNotice(`Plan accepted: ${response.draftRevision.id}`);
      if (activeBranchId) {
        await refreshBranches(response.workflow.id, activeBranchId);
      }
      setDraftEvaluation(null);
      setApprovedRevision(null);
      setApprovalDiff(null);
      await refreshRuntimeStatus(response.workflow.id, activeBranchId);
    });
  }

  function runWorkflow() {
    if (!approvedRevision) {
      return;
    }
    if (!activeRunnerDeployment) {
      setApiError("Production runs require an active runner.configuration deployment.");
      return;
    }
    if (branchLifecycleLocked) {
      setApiError("Archived branches are read-only.");
      return;
    }

    void executeApiAction("run", async () => {
      const job = await startTrackedJob({
        type: "run.workflow",
        workflowId: workflow.id,
        revisionId: approvedRevision.id
      });
      const response = await kelpClawApi.startRun(
        workflow.id,
        {
          approvedRevisionId: approvedRevision.id,
          deploymentId: activeRunnerDeployment.id,
          ...(activeBranchId ? { branchId: activeBranchId } : {})
        },
        job.id
      );
      setRun(response.run);
      const fetched = await kelpClawApi.fetchRun(workflow.id, response.run.id);
      setRun(fetched.run);
      await refreshRuntimeStatus(workflow.id, activeBranchId);
    });
  }

  function deployWorkflow(kind: WorkflowDeploymentRecord["kind"] = "runner.configuration") {
    if (!approvedRevision || !draftEvaluation) {
      return;
    }
    if (branchLifecycleLocked) {
      setApiError("Archived branches are read-only.");
      return;
    }

    void executeApiAction("deploy", async () => {
      const job = await startTrackedJob({
        type: "deploy.workflow",
        workflowId: workflow.id,
        revisionId: approvedRevision.id
      });
      const response = await kelpClawApi.deployWorkflow(
        workflow.id,
        {
          approvedRevisionId: approvedRevision.id,
          kind,
          createdBy: "owner@example.com",
          rollbackPlan: `Rollback to ${approvedRevision.id}.`,
          ...(activeBranchId ? { branchId: activeBranchId } : {}),
          metadata: {
            source: "kelpclaw"
          }
        },
        job.id
      );
      const active = await kelpClawApi.fetchActiveDeployments(workflow.id);
      setDeploymentNotice(`Deployment ${response.deployment.status}: ${response.deployment.kind}`);
      setDeploymentActivations(active);
      await refreshRuntimeStatus(workflow.id, activeBranchId);
    });
  }

  function exportDecisionTraces() {
    if (workflow.id === emptyWorkflowDraft.id) {
      setApiError("Plan a workflow before exporting decision traces.");
      return;
    }

    void executeApiAction("export-decision-traces", async () => {
      const response = await kelpClawApi.exportDecisionTraces(workflow.id);
      setDecisionTraceExportNotice(
        `Decision trace export ${response.export.id}: ${response.export.lineCount} JSONL line(s).`
      );
    });
  }

  function exportAudit() {
    if (workflow.id === emptyWorkflowDraft.id) {
      setApiError("Plan a workflow before exporting audit records.");
      return;
    }

    void executeApiAction("export-audit", async () => {
      const response = await kelpClawApi.exportAudit(workflow.id);
      setAuditExportNotice(
        `Audit export ${response.export.id}: ${response.export.lineCount} JSONL line(s).`
      );
    });
  }

  function undeployActiveRunner() {
    if (!activeRunnerDeployment) {
      setApiError("No active runner.configuration deployment is available.");
      return;
    }

    void executeApiAction("undeploy", async () => {
      const response = await kelpClawApi.undeployDeployment(workflow.id, activeRunnerDeployment.id);
      setDeploymentActivations(response.active);
      setDeploymentNotice(`Deployment ${response.deployment.status}: ${response.deployment.kind}`);
      await refreshRuntimeStatus(workflow.id, activeBranchId);
    });
  }

  function rollbackActiveRunner() {
    if (!activeRunnerDeployment) {
      setApiError("No active runner.configuration deployment is available.");
      return;
    }

    void executeApiAction("rollback", async () => {
      const response = await kelpClawApi.rollbackDeployment(workflow.id, activeRunnerDeployment.id);
      setDeploymentActivations(response.active);
      setDeploymentNotice(
        `Rollback target ${response.rollbackTarget.deploymentId}: ${response.deployment.status}`
      );
      await refreshRuntimeStatus(workflow.id, activeBranchId);
    });
  }

  function importOpenApiConnector(input: { readonly name: string; readonly sourceUrl: string }) {
    if (!input.sourceUrl.trim()) {
      setApiError("OpenAPI import requires a URL.");
      return;
    }

    void executeApiAction("connector-import", async () => {
      const response = await kelpClawApi.importOpenApiConnector({
        name: input.name.trim() || undefined,
        sourceUrl: input.sourceUrl.trim()
      });
      setConnectors((current) => [
        response.connector,
        ...current.filter((connector) => connector.id !== response.connector.id)
      ]);
      await refreshRuntimeStatus(workflow.id, activeBranchId);
    });
  }

  function registerMcpConnector(input: { readonly name: string; readonly endpointUrl: string }) {
    if (!input.endpointUrl.trim()) {
      setApiError("MCP registration requires an endpoint URL.");
      return;
    }

    void executeApiAction("connector-mcp", async () => {
      const response = await kelpClawApi.registerMcpConnector({
        name: input.name.trim() || undefined,
        endpointUrl: input.endpointUrl.trim()
      });
      setConnectors((current) => [
        response.connector,
        ...current.filter((connector) => connector.id !== response.connector.id)
      ]);
      await refreshRuntimeStatus(workflow.id, activeBranchId);
    });
  }

  function testConnector(connectorId: string) {
    void executeApiAction("connector-test", async () => {
      const response = await kelpClawApi.testConnector(connectorId);
      setConnectors((current) =>
        current.map((connector) =>
          connector.id === response.connector.id ? response.connector : connector
        )
      );
      await refreshRuntimeStatus(workflow.id, activeBranchId);
    });
  }

  function deleteConnector(connectorId: string) {
    void executeApiAction("connector-delete", async () => {
      await kelpClawApi.deleteConnector(connectorId);
      setConnectors((current) => current.filter((connector) => connector.id !== connectorId));
      await refreshRuntimeStatus(workflow.id, activeBranchId);
    });
  }

  function runRouterEvals() {
    void executeApiAction("router-evals", async () => {
      const response = await kelpClawApi.runRouterEvals();
      setRouterEvalRun(response.run);
      setRouterEvalCases(
        response.run.results.map((result) => ({
          id: result.id,
          prompt: result.prompt,
          expectedRoute: result.expectedRoute,
          minConfidence: result.route.confidence,
          expectedNodeKinds: result.route.expectedNodeKinds
        }))
      );
    });
  }

  function addConnectorOperationNode(
    connector: WorkflowConnectorRecord,
    operation: WorkflowConnectorOperation
  ) {
    if (branchLifecycleLocked) {
      setApiError("Archived branches are read-only.");
      return;
    }
    const id = uniqueComponentNodeId(`connector-${connector.id}-${operation.name}`, workflow.nodes);
    const position = nextNodePosition(nodes);
    const node = createWorkflowNode({
      id,
      kind: "skill",
      label: operation.name,
      description: operation.description || `${connector.name} operation.`,
      inputs: {
        request: operation.inputSchema
      },
      outputs: {
        response: operation.outputSchema
      },
      config: {
        canvas: position,
        connectorId: connector.id,
        operation: operation.name,
        allowedHosts: [...connector.allowedHosts]
      },
      adapterId: connector.adapterId,
      adapterOperations: [
        {
          adapterId: connector.adapterId,
          operation: operation.name,
          operationVersion: operation.version
        }
      ],
      secretRefs: connector.secretRefs
    });
    updateLocalWorkflow({
      ...workflow,
      approval: null,
      nodes: [...workflow.nodes, node]
    });
    setSelectedNodeId(id);
    setSelectedEdgeId(null);
    setNodePrompt(node.description);
    setJsonError(null);
    markDirty(id);
  }

  function replayRun(runId: string) {
    void executeApiAction("run-replay", async () => {
      const response = await kelpClawApi.replayRun(workflow.id, runId);
      setRun(response.run);
      await refreshRuntimeStatus(workflow.id, activeBranchId);
    });
  }

  function pauseSchedule(scheduleId: string) {
    void executeApiAction("schedule-pause", async () => {
      const response = await kelpClawApi.pauseSchedule(workflow.id, scheduleId);
      setWorkflowSchedules((current) =>
        current.map((schedule) =>
          schedule.id === response.schedule.id ? response.schedule : schedule
        )
      );
      await refreshRuntimeStatus(workflow.id, activeBranchId);
    });
  }

  function resumeSchedule(scheduleId: string) {
    void executeApiAction("schedule-resume", async () => {
      const response = await kelpClawApi.resumeSchedule(workflow.id, scheduleId);
      setWorkflowSchedules((current) =>
        current.map((schedule) =>
          schedule.id === response.schedule.id ? response.schedule : schedule
        )
      );
      await refreshRuntimeStatus(workflow.id, activeBranchId);
    });
  }

  function cancelActiveJob() {
    if (!activeJob || ["succeeded", "failed", "cancelled"].includes(activeJob.status)) {
      return;
    }

    void executeApiAction("cancel-job", async () => {
      const response = await kelpClawApi.cancelJob(activeJob.id, "Stopped from KelpClaw.");
      setActiveJob(response.job);
    });
  }

  function resetWorkflow() {
    setPrompt(defaultPrompt);
    setDirtyNodeIds(new Set());
    setSelectedNodeId(null);
    setSelectedEdgeId(null);
    setNodePrompt("");
    setJsonError(null);
    setApprovedRevision(null);
    setApprovalDiff(null);
    setRun(null);
    setTaskRoute(null);
    setPlannerFeedback(null);
    setDraftEvaluation(null);
    setClarification(null);
    setClarificationAnswers({});
    setActiveJob(null);
    setWorkspace(null);
    setAgentRuns([]);
    setDeploymentNotice(null);
    setPlanAcceptedNotice(null);
    setDeploymentActivations(null);
    setRuntimeTruth(null);
    setBudgetPolicy(null);
    setBudgetLedgers([]);
    setAgentTimeline([]);
    setAuditExportNotice(null);
    setBranches([]);
    setActiveBranchId(null);
    setPromptTurns([]);
    setBranchNotice(null);
    setMergeSourceBranchId("");
    setMergeMode("merge");
    setMergePreview(null);
    setMergeResolutionModes({});
    mergeResolutionModesRef.current = {};
    mergeManualJsonRef.current = {};
    setReuseDecisions([]);
    setPromotionNotice(null);
    loadWorkflow(emptyWorkflowDraft, validateWorkflowSpec(emptyWorkflowDraft));
  }

  function openPalette(mode: CommandPaletteMode = { kind: "commands" }) {
    if (mode.kind !== "commands" || mode.scope !== "node-create") {
      setPendingNodeConnection(null);
    }
    setPaletteMode(mode);
    setPaletteQuery("");
    setPaletteSelection(0);
    setPaletteOpen(true);
  }

  function closePalette() {
    setPaletteOpen(false);
    setPaletteMode({ kind: "commands" });
    setPaletteQuery("");
    setPaletteSelection(0);
    setPendingNodeConnection(null);
  }

  function openNodeCreatePalette() {
    setPendingNodeConnection(null);
    openPalette({ kind: "commands", scope: "node-create" });
  }

  function openConnectedNodePalette(nodeId: string, outputPort: string | undefined) {
    setSelectedNodeId(nodeId);
    setSelectedEdgeId(null);
    updateFlowSelection(nodeId, null);
    setPendingNodeConnection({ sourceNodeId: nodeId, outputPort });
    openPalette({ kind: "commands", scope: "node-create" });
  }

  function selectNodeById(nodeId: string) {
    const node = workflow.nodes.find((candidate) => candidate.id === nodeId);
    setSelectedNodeId(nodeId);
    setSelectedEdgeId(null);
    setNodePrompt(node?.description ?? "");
    setJsonError(null);
    updateFlowSelection(nodeId, null);
  }

  function openNodeDetails(nodeId: string) {
    const node = workflow.nodes.find((candidate) => candidate.id === nodeId);
    setSelectedNodeId(nodeId);
    setSelectedEdgeId(null);
    updateFlowSelection(nodeId, null);
    setNodePrompt(node?.description ?? "");
    setJsonError(null);
    setDetailsTab("node");
    setDetailsOpen(true);
  }

  function openEdgeDetails(edgeId: string) {
    setSelectedNodeId(null);
    setSelectedEdgeId(edgeId);
    updateFlowSelection(null, edgeId);
    setNodePrompt("");
    setJsonError(null);
    setDetailsTab("node");
    setDetailsOpen(true);
  }

  function openSelectedDetails(tab: DetailsTab = "node") {
    if (!selectedNodeId && !selectedEdgeId) {
      return;
    }
    setDetailsTab(tab);
    setDetailsOpen(true);
  }

  function executePaletteCommand(command: PaletteCommand | undefined) {
    if (!command || command.disabled) {
      return;
    }

    command.onSelect();
    if (command.closeOnSelect !== false) {
      closePalette();
    }
  }

  function submitPalettePlan(value: string) {
    const nextPrompt = value.trim();
    planDraft(nextPrompt);
    if (nextPrompt) {
      closePalette();
    }
  }

  function submitPaletteClarification() {
    planDraft(prompt, clarificationAnswers);
    closePalette();
  }

  function submitPaletteFork(value: string) {
    forkBranch(value);
    if (value.trim()) {
      closePalette();
    }
  }

  function submitPaletteRename(value: string) {
    renameBranch(value);
    if (value.trim()) {
      closePalette();
    }
  }

  function submitPaletteAdminToken(value: string) {
    updateAdminToken(value);
    closePalette();
  }

  function submitPaletteSecret(secretName: string, value: string) {
    saveSecret(secretName, value);
    if (value.trim()) {
      closePalette();
    }
  }

  const storedSecretNames = new Set(secretMetadata.map((secret) => secret.name));
  const activeBranchIsDefault = activeBranch?.id.endsWith(".main") === true;
  const mergeConflictsResolved =
    mergePreview?.conflicts.every((conflict) => mergeResolutionModes[conflict.id]) ?? true;
  const paletteCommands: PaletteCommand[] = [
    ...componentPaletteItems.map((item) => ({
      id: `component-${item.id}`,
      group: "Components",
      label: `Add ${item.label}`,
      detail: item.description,
      keywords: [item.kind, categoryLabel(item.category)],
      iconKey: item.iconKey ?? providerIconKeyForAdapter(item.adapterId),
      disabled: branchLifecycleLocked,
      onSelect: () => addComponentNode(item)
    })),
    {
      id: "workflow-plan",
      group: "Workflow",
      label: "Plan Workflow",
      detail: planDisabledReason ?? "Draft or revise the workflow from a prompt.",
      disabled: Boolean(planDisabledReason),
      closeOnSelect: false,
      onSelect: () => openPalette({ kind: "plan", value: prompt })
    },
    {
      id: "workflow-validate",
      group: "Workflow",
      label: "Validate Workflow",
      detail: validateDisabledReason ?? "Run graph validation against the current draft.",
      disabled: Boolean(validateDisabledReason),
      onSelect: validateDraft
    },
    {
      id: "workflow-accept-plan",
      group: "Workflow",
      label: "Accept Plan",
      detail: acceptPlanDisabledReason ?? "Record the current draft plan shape.",
      disabled: Boolean(acceptPlanDisabledReason),
      onSelect: acceptPlanShape
    },
    {
      id: "workflow-evaluate",
      group: "Workflow",
      label: "Evaluate Draft",
      detail: evaluateDisabledReason ?? "Run the draft evaluator before approval.",
      disabled: Boolean(evaluateDisabledReason),
      onSelect: evaluateDraft
    },
    {
      id: "workflow-approve",
      group: "Workflow",
      label: "Approve Workflow",
      detail: approveDisabledReason ?? "Freeze an approval revision for the current draft.",
      disabled: Boolean(approveDisabledReason),
      onSelect: approveWorkflow
    },
    {
      id: "workflow-run",
      group: "Workflow",
      label: "Run Workflow",
      detail: runDisabledReason ?? "Start a run from the active deployed runner config.",
      disabled: Boolean(runDisabledReason),
      onSelect: runWorkflow
    },
    {
      id: "workflow-deploy",
      group: "Workflow",
      label: "Deploy Workflow",
      detail: deployDisabledReason ?? "Deploy an active runner.configuration.",
      disabled: Boolean(deployDisabledReason),
      onSelect: () => deployWorkflow("runner.configuration")
    },
    {
      id: "workflow-export-bundle",
      group: "Workflow",
      label: "Export Workflow Bundle",
      detail: deployDisabledReason ?? "Export a local workflow bundle artifact.",
      disabled: Boolean(deployDisabledReason),
      onSelect: () => deployWorkflow("workflow.bundle")
    },
    {
      id: "workflow-export-audit",
      group: "Workflow",
      label: "Export Audit JSONL",
      detail:
        workflow.id === emptyWorkflowDraft.id
          ? "Plan a workflow before exporting audit records."
          : "Export redacted audit and decision trace records.",
      disabled: busyAction !== null || workflow.id === emptyWorkflowDraft.id,
      onSelect: exportAudit
    },
    {
      id: "workflow-stop",
      group: "Workflow",
      label: "Stop Job",
      detail: "Cancel the active worker job.",
      disabled:
        !activeJob ||
        ["succeeded", "failed", "cancelled"].includes(activeJob.status) ||
        busyAction === "cancel-job",
      onSelect: cancelActiveJob
    },
    {
      id: "workflow-reset",
      group: "Workflow",
      label: "Reset Workflow",
      detail: "Clear the local draft and start over.",
      onSelect: resetWorkflow
    },
    {
      id: "selection-delete",
      group: "Workflow",
      label: "Delete Selection",
      detail: "Remove the selected node or edge.",
      disabled: !selectedNodeId && !selectedEdgeId,
      onSelect: deleteSelection
    },
    {
      id: "selection-details",
      group: "Workflow",
      label: "Open Details",
      detail: selectedNodeId
        ? "Open the selected node drawer."
        : selectedEdgeId
          ? "Open the selected edge drawer."
          : "Select a node or edge first.",
      disabled: !selectedNodeId && !selectedEdgeId,
      onSelect: () => openSelectedDetails("node")
    },
    ...detailTabs.map((tab) => ({
      id: `selection-details-${tab.id}`,
      group: "Details",
      label: `Show ${tab.label}`,
      detail:
        selectedNodeId || selectedEdgeId
          ? `Open the ${tab.label.toLowerCase()} details tab.`
          : "Select a node or edge first.",
      disabled: !selectedNodeId && !selectedEdgeId,
      onSelect: () => openSelectedDetails(tab.id)
    })),
    {
      id: "status-open",
      group: "Status",
      label: "Show Status Popover",
      detail: "Show workflow, validation, deployment, and runtime status.",
      onSelect: () => setStatusPopoverOpen(true)
    },
    {
      id: "branch-fork",
      group: "Branches",
      label: "Fork Branch",
      detail: activeBranch ? `Create a branch from ${activeBranch.name}.` : "No branch selected.",
      disabled: busyAction !== null || !activeBranch || branchLifecycleLocked,
      closeOnSelect: false,
      onSelect: () => openPalette({ kind: "fork-branch", value: branchNameDraft })
    },
    {
      id: "branch-rename",
      group: "Branches",
      label: "Rename Active Branch",
      detail: activeBranch ? `Rename ${activeBranch.name}.` : "No branch selected.",
      disabled: busyAction !== null || !activeBranch,
      closeOnSelect: false,
      onSelect: () => openPalette({ kind: "rename-branch", value: branchRenameDraft })
    },
    {
      id: "branch-archive",
      group: "Branches",
      label:
        activeBranch?.status === "archived" ? "Restore Active Branch" : "Archive Active Branch",
      detail: activeBranch ? activeBranch.name : "No branch selected.",
      disabled: busyAction !== null || !activeBranch || activeBranchIsDefault,
      onSelect: toggleBranchArchive
    },
    {
      id: "branch-archived-toggle",
      group: "Branches",
      label: showArchivedBranches ? "Hide Archived Branches" : "Show Archived Branches",
      detail: "Toggle archived branch visibility for branch commands.",
      onSelect: () => setShowArchivedBranches(!showArchivedBranches)
    },
    ...visibleBranches.map((branch) => ({
      id: `branch-switch-${branch.id}`,
      group: "Branches",
      label: `Switch To ${branch.name}`,
      detail: branch.status,
      disabled: busyAction !== null || branch.id === activeBranchId,
      onSelect: () => switchBranch(branch.id)
    })),
    ...mergeSources.flatMap((branch) => [
      {
        id: `branch-preview-merge-${branch.id}`,
        group: "Branches",
        label: `Preview Merge From ${branch.name}`,
        detail: `Merge into ${activeBranch?.name ?? "active branch"}.`,
        disabled:
          busyAction !== null ||
          !activeBranch ||
          branchLifecycleLocked ||
          branch.status === "archived",
        onSelect: () => previewMerge(branch.id, "merge")
      },
      {
        id: `branch-preview-cherry-pick-${branch.id}`,
        group: "Branches",
        label: `Preview Cherry-pick From ${branch.name}`,
        detail: `Cherry-pick into ${activeBranch?.name ?? "active branch"}.`,
        disabled:
          busyAction !== null ||
          !activeBranch ||
          branchLifecycleLocked ||
          branch.status === "archived",
        onSelect: () => previewMerge(branch.id, "cherry-pick")
      }
    ]),
    ...(mergePreview
      ? mergePreview.conflicts.flatMap((conflict) => [
          {
            id: `merge-source-${conflict.id}`,
            group: "Branches",
            label: `Use Source For ${conflict.kind}`,
            detail: conflict.message,
            onSelect: () => updateMergeResolutionMode(conflict.id, "source")
          },
          {
            id: `merge-target-${conflict.id}`,
            group: "Branches",
            label: `Keep Target For ${conflict.kind}`,
            detail: conflict.message,
            onSelect: () => updateMergeResolutionMode(conflict.id, "target")
          }
        ])
      : []),
    {
      id: "branch-apply-merge",
      group: "Branches",
      label: "Apply Merge Preview",
      detail: mergePreview
        ? `${mergePreview.mode} from ${mergePreview.sourceBranchId}`
        : "No merge preview.",
      disabled:
        busyAction !== null ||
        !mergePreview ||
        mergePreview.status === "blocked" ||
        branchLifecycleLocked ||
        !mergeConflictsResolved,
      onSelect: applyMerge
    },
    {
      id: "branch-reuse",
      group: "Branches",
      label: "Refresh Reuse Candidates",
      detail: "Check generated modules that can be reused on this branch.",
      disabled: busyAction !== null || !activeBranch || branchLifecycleLocked,
      onSelect: refreshReuseCandidates
    },
    ...reuseDecisions.map((decision) => ({
      id: `reuse-decision-${decision.id}`,
      group: "Branches",
      label: `Reuse ${decision.nodeId}: ${decision.status}`,
      detail: `${decision.reason}${decision.sourceBranchId ? ` Source: ${decision.sourceBranchId}.` : ""}`,
      keywords: [decision.nodeId, decision.status, decision.sourceBranchId ?? ""],
      disabled: true,
      onSelect: () => {}
    })),
    ...(plannerFeedback?.suggestions.flatMap((suggestion) => [
      {
        id: `suggestion-accept-${suggestion.id}`,
        group: "Suggestions",
        label: `Accept Suggestion: ${suggestion.title}`,
        detail: suggestion.message,
        keywords: [suggestion.status, suggestion.conflict],
        disabled: busyAction !== null || suggestion.status !== "suggested",
        onSelect: () => updateSuggestionDecision(suggestion.id, "accepted")
      },
      {
        id: `suggestion-reject-${suggestion.id}`,
        group: "Suggestions",
        label: `Reject Suggestion: ${suggestion.title}`,
        detail: suggestion.message,
        keywords: [suggestion.status, suggestion.conflict],
        disabled: busyAction !== null || suggestion.status !== "suggested",
        onSelect: () => updateSuggestionDecision(suggestion.id, "rejected")
      }
    ]) ?? []),
    ...promptTurns.slice(-6).map((turn) => ({
      id: `prompt-turn-${turn.id}`,
      group: "Branches",
      label: `Prompt Turn: ${turn.source}`,
      detail: turn.prompt,
      disabled: true,
      onSelect: () => {}
    })),
    {
      id: "integration-refresh",
      group: "Integrations",
      label: "Refresh Integration Status",
      detail: "Reload secrets and provider readiness.",
      disabled: busyAction !== null,
      onSelect: () => {
        void refreshIntegrations();
      }
    },
    {
      id: "integration-admin-token",
      group: "Integrations",
      label: "Set Admin Token",
      detail: "Save the bearer token used for KelpClaw admin API calls.",
      closeOnSelect: false,
      onSelect: () => openPalette({ kind: "admin-token", value: adminToken })
    },
    {
      id: "integration-google-connect",
      group: "Integrations",
      label: "Connect Google",
      detail: "Start Google OAuth for Gmail and Sheets.",
      iconKey: "google",
      disabled: busyAction !== null,
      onSelect: connectGoogle
    },
    {
      id: "integration-google-revoke",
      group: "Integrations",
      label: "Revoke Google",
      detail: "Remove the stored Google OAuth secret.",
      iconKey: "google",
      disabled: busyAction !== null || !storedSecretNames.has("google.oauth.default"),
      onSelect: revokeGoogle
    },
    ...integrationSetups.flatMap((setup) => [
      {
        id: `integration-status-${setup.id}`,
        group: "Integrations",
        label: `${setup.label} Integration`,
        detail: `${setup.secretName} is ${storedSecretNames.has(setup.secretName) ? "stored" : "missing"}; ${integrationStatus(setup.id, integrationReadiness, googleConnected).label}.`,
        iconKey: setup.id,
        disabled: true,
        onSelect: () => {}
      },
      {
        id: `integration-save-${setup.id}`,
        group: "Integrations",
        label: `Save ${setup.label} Secret`,
        detail: setup.secretName,
        iconKey: setup.id,
        disabled: busyAction !== null,
        closeOnSelect: false,
        onSelect: () =>
          openPalette({
            kind: "secret",
            label: setup.label,
            secretName: setup.secretName,
            value: secretDrafts[setup.secretName] ?? ""
          })
      },
      {
        id: `integration-delete-${setup.id}`,
        group: "Integrations",
        label: `Delete ${setup.label} Secret`,
        detail: setup.secretName,
        iconKey: setup.id,
        disabled: busyAction !== null || !storedSecretNames.has(setup.secretName),
        onSelect: () => deleteSecret(setup.secretName)
      }
    ]),
    {
      id: "status-workflow",
      group: "Status",
      label: `Workflow: ${workflow.name}`,
      detail: `${workflow.nodes.length} nodes, ${workflow.edges.length} edges, revision ${workflow.revision}.`,
      disabled: true,
      onSelect: () => {}
    },
    {
      id: "status-branch",
      group: "Status",
      label: `Active Branch: ${activeBranch?.name ?? "none"}`,
      detail: activeBranch?.status ?? "No branch selected.",
      disabled: true,
      onSelect: () => {}
    },
    {
      id: "status-route",
      group: "Status",
      label: `Route: ${taskRoute?.route ?? "unrouted"}`,
      detail: taskRoute?.rationale ?? "No route has been calculated.",
      disabled: true,
      onSelect: () => {}
    },
    {
      id: "status-evaluation",
      group: "Status",
      label: `Draft Eval: ${draftEvaluation?.status ?? "not run"}`,
      detail: draftEvaluation?.readyForApproval ? "Ready for approval." : "Approval is blocked.",
      disabled: true,
      onSelect: () => {}
    },
    ...(validationIssues.length > 0
      ? validationIssues.map((issue, index) => ({
          id: `validation-${index}-${issue.code}`,
          group: "Validation",
          label: issue.code,
          detail: issue.message,
          keywords: [issue.path.join(".")],
          onSelect: () => selectIssue(issue)
        }))
      : [
          {
            id: "validation-valid",
            group: "Validation",
            label: "Validation: valid",
            detail: "No graph validation issues.",
            disabled: true,
            onSelect: () => {}
          }
        ])
  ];
  const filteredPaletteCommands =
    paletteMode.kind === "commands"
      ? paletteCommands
          .filter((command) =>
            paletteMode.scope === "node-create" ? command.group === "Components" : true
          )
          .filter((command) => commandMatchesQuery(command, paletteQuery))
      : [];

  return (
    <main className="app-shell">
      <section className="workspace">
        <section
          className={detailsOpen ? "canvas-panel canvas-panel-details-open" : "canvas-panel"}
          aria-label="Workflow graph"
        >
          <header className="canvas-header">
            <div className="workflow-header-card">
              <img className="app-logo-mark" src="/app-logo.png" alt="KelpClaw logo" />
              <div>
                <h1>KelpClaw</h1>
                <p className="topbar-workflow">Revision {workflow.revision}</p>
              </div>
            </div>
            <div className="mode-toggle" aria-label="Workspace mode">
              {(["edit", "trajectory", "policy"] as const).map((mode) => (
                <button
                  key={mode}
                  type="button"
                  className={surfaceMode === mode ? "mode-toggle-active" : ""}
                  onClick={() => setSurfaceMode(mode)}
                >
                  {mode}
                </button>
              ))}
            </div>
            <div className="canvas-command-cluster" aria-label="Workflow actions">
              <button
                className="command-entry-button"
                type="button"
                title="Open command palette"
                onClick={() => openPalette()}
              >
                <Search size={18} />
                Commands
                <kbd>⌘P</kbd>
              </button>
              <button
                className="status-popover-button"
                type="button"
                title="Show workflow status"
                onClick={() => setStatusPopoverOpen((open) => !open)}
              >
                <Info size={18} />
                {validationIssues.length > 0
                  ? `${validationIssues.length} issues`
                  : activeJob
                    ? activeJob.status
                    : (run?.status ?? "ready")}
              </button>
              <button
                className="run-control-button"
                title={runDisabledReason ?? "Run workflow"}
                onClick={runWorkflow}
                disabled={Boolean(runDisabledReason)}
              >
                <Play size={18} />
                Run
              </button>
            </div>
          </header>

          {selectedEdge ? (
            <div className="edge-selection-bar" aria-label="Selected edge actions">
              <span>
                Edge {selectedEdge.source.nodeId} → {selectedEdge.target.nodeId}
              </span>
              <button type="button" onClick={() => openSelectedDetails("node")}>
                <PanelRightOpen size={16} />
                Details
              </button>
              <button
                className="icon-button"
                type="button"
                title="Delete edge"
                onClick={deleteSelection}
              >
                <Trash2 size={16} />
              </button>
            </div>
          ) : null}

          {statusPopoverOpen ? (
            <StatusPopover
              workflow={workflow}
              activeBranch={activeBranch}
              validationIssues={validationIssues}
              runtimeTruth={runtimeTruth}
              run={run}
              activeJob={activeJob}
              deploymentNotice={deploymentNotice}
              planAcceptedNotice={planAcceptedNotice}
              apiError={apiError}
              branchNotice={branchNotice}
              onClose={() => setStatusPopoverOpen(false)}
            />
          ) : null}

          <button
            className="floating-add-button"
            type="button"
            aria-label="Add node"
            title="Add node"
            onClick={openNodeCreatePalette}
          >
            <Plus size={26} />
          </button>

          {detailsOpen && (selectedNode || selectedEdge) ? (
            <aside className="details-drawer" aria-label="Details drawer">
              <Inspector
                workflow={workflow}
                selectedNode={selectedNode}
                selectedEdge={selectedEdge}
                activeTab={detailsTab}
                tabs={detailTabs}
                nodePrompt={nodePrompt}
                jsonError={jsonError}
                approvalDiff={approvalDiff}
                approvedRevision={approvedRevision}
                run={run}
                taskRoute={taskRoute}
                activeJob={activeJob}
                workspace={workspace}
                agentRuns={agentRuns}
                runtimeTruth={runtimeTruth}
                providerConfigs={providerConfigs}
                budgetPolicy={budgetPolicy}
                budgetLedgers={budgetLedgers}
                agentTimeline={agentTimeline}
                connectors={connectors}
                workflowRuns={workflowRuns}
                workflowSchedules={workflowSchedules}
                opsHealth={opsHealth}
                routerEvalCases={routerEvalCases}
                routerEvalRun={routerEvalRun}
                agentMemory={agentMemory}
                nodeDecisionTraces={nodeDecisionTraces}
                decisionTraceExportNotice={decisionTraceExportNotice}
                planAcceptedNotice={planAcceptedNotice}
                deploymentActivations={deploymentActivations}
                activeRunnerDeployment={activeRunnerDeployment}
                busyAction={busyAction}
                branchLifecycleLocked={branchLifecycleLocked}
                onClose={() => setDetailsOpen(false)}
                onTabChange={setDetailsTab}
                onNodePromptChange={setNodePrompt}
                onReprompt={repromptNode}
                onBuildCodegen={buildCodegenNode}
                onReviewCodegen={reviewCodegenNode}
                onPromoteCodegen={promoteCodegenNode}
                onExportDecisionTraces={exportDecisionTraces}
                onExportAudit={exportAudit}
                onDeployRunner={() => deployWorkflow("runner.configuration")}
                onDeployBundle={() => deployWorkflow("workflow.bundle")}
                onUndeployRunner={undeployActiveRunner}
                onRollbackRunner={rollbackActiveRunner}
                onImportOpenApiConnector={importOpenApiConnector}
                onRegisterMcpConnector={registerMcpConnector}
                onTestConnector={testConnector}
                onDeleteConnector={deleteConnector}
                onAddConnectorOperation={addConnectorOperationNode}
                onRunRouterEvals={runRouterEvals}
                onReplayRun={replayRun}
                onPauseSchedule={pauseSchedule}
                onResumeSchedule={resumeSchedule}
                onUpdateNode={updateNode}
                onUpdateJsonField={updateJsonField}
              />
            </aside>
          ) : null}

          {surfaceMode === "trajectory" ? (
            <TrajectoryView
              runs={trajectoryRuns}
              selectedRunId={selectedTrajectoryRunId}
              onSelectRun={setSelectedTrajectoryRunId}
              onRefresh={refreshTrajectoryRuns}
              onAnchorRun={anchorTrajectoryRun}
              onApproveEvent={approveTrajectoryEvent}
              onDenyEvent={denyTrajectoryEvent}
              notice={trajectoryNotice}
            />
          ) : surfaceMode === "policy" ? (
            <section className="policy-editor-panel" aria-label="Policy editor">
              <textarea
                value={policyYaml}
                onChange={(event) => setPolicyYaml(event.target.value)}
                spellCheck={false}
              />
              <div>
                <button
                  type="button"
                  className="policy-save-button"
                  disabled={busyAction !== null}
                  onClick={savePolicyYaml}
                >
                  <CheckCircle2 size={16} />
                  Save Policy
                </button>
                <span>{policyNotice ?? ""}</span>
              </div>
            </section>
          ) : (
            <ReactFlow
              nodes={flowNodes}
              edges={flowEdges}
              nodeTypes={workflowNodeTypes}
              onNodesChange={onNodesChange}
              onEdgesChange={onEdgesChange}
              onNodeDragStop={onNodeDragStop}
              onConnect={onConnect}
              onNodesDelete={onNodesDelete}
              onEdgesDelete={onEdgesDelete}
              onNodeDoubleClick={(_, node) => openNodeDetails(node.id)}
              onEdgeDoubleClick={(_, edge) => openEdgeDetails(edge.id)}
              onPaneClick={() => {
                setSelectedNodeId(null);
                setSelectedEdgeId(null);
                updateFlowSelection(null, null);
                setNodePrompt("");
                setJsonError(null);
                setDetailsOpen(false);
              }}
              onSelectionChange={({ nodes: selectedNodes, edges: selectedEdges }) => {
                if (selectedNodes[0]) {
                  setSelectedNodeId(selectedNodes[0].id);
                  setSelectedEdgeId(null);
                  setNodePrompt(
                    workflow.nodes.find((node) => node.id === selectedNodes[0]?.id)?.description ??
                      ""
                  );
                  setJsonError(null);
                } else if (selectedEdges[0]) {
                  setSelectedNodeId(null);
                  setSelectedEdgeId(selectedEdges[0].id);
                  setNodePrompt("");
                  setJsonError(null);
                } else {
                  setSelectedNodeId(null);
                  setSelectedEdgeId(null);
                  setNodePrompt("");
                  setJsonError(null);
                  setDetailsOpen(false);
                }
              }}
              fitView
              minZoom={0.5}
              maxZoom={1.35}
            >
              <Background color="#272a32" gap={18} size={1.15} />
              <MiniMap pannable zoomable />
              <Controls showInteractive={false} />
            </ReactFlow>
          )}
          <div className="canvas-footer" aria-label="Canvas status">
            <span>{workflow.nodes.length} nodes</span>
            <span>{workflow.edges.length} edges</span>
            <span>
              {validationIssues.length > 0 ? `${validationIssues.length} issues` : "valid"}
            </span>
          </div>
        </section>
      </section>
      <CommandPalette
        open={paletteOpen}
        mode={paletteMode}
        query={paletteQuery}
        commands={filteredPaletteCommands}
        selectedIndex={paletteSelection}
        clarification={clarification}
        clarificationAnswers={clarificationAnswers}
        clarificationReady={clarificationReady}
        busyAction={busyAction}
        inputRef={commandPaletteInputRef}
        onClose={closePalette}
        onModeChange={setPaletteMode}
        onQueryChange={(value) => {
          setPaletteQuery(value);
          setPaletteSelection(0);
        }}
        onSelectionChange={setPaletteSelection}
        onExecuteCommand={executePaletteCommand}
        onSubmitPlan={submitPalettePlan}
        onSubmitClarification={submitPaletteClarification}
        onClarificationAnswerChange={updateClarificationAnswer}
        onSubmitFork={submitPaletteFork}
        onSubmitRename={submitPaletteRename}
        onSubmitAdminToken={submitPaletteAdminToken}
        onSubmitSecret={submitPaletteSecret}
      />
      <ToastStack
        apiError={apiError}
        branchNotice={branchNotice}
        deploymentNotice={deploymentNotice}
        planAcceptedNotice={planAcceptedNotice}
        auditExportNotice={auditExportNotice}
        decisionTraceExportNotice={decisionTraceExportNotice}
        promotionNotice={promotionNotice}
        run={run}
        onDismissApiError={() => setApiError(null)}
        onDismissBranchNotice={() => setBranchNotice(null)}
      />
    </main>
  );
}

/* eslint-disable react-hooks/refs -- Palette children forward one focus ref through typed props. */
function CommandPalette(props: {
  readonly open: boolean;
  readonly mode: CommandPaletteMode;
  readonly query: string;
  readonly commands: readonly PaletteCommand[];
  readonly selectedIndex: number;
  readonly clarification: WorkflowClarificationRequest | null;
  readonly clarificationAnswers: Readonly<Record<string, string>>;
  readonly clarificationReady: boolean;
  readonly busyAction: string | null;
  readonly inputRef: { readonly current: HTMLInputElement | HTMLTextAreaElement | null };
  readonly onClose: () => void;
  readonly onModeChange: (mode: CommandPaletteMode) => void;
  readonly onQueryChange: (value: string) => void;
  readonly onSelectionChange: (index: number) => void;
  readonly onExecuteCommand: (command: PaletteCommand | undefined) => void;
  readonly onSubmitPlan: (value: string) => void;
  readonly onSubmitClarification: () => void;
  readonly onClarificationAnswerChange: (questionId: string, value: string) => void;
  readonly onSubmitFork: (value: string) => void;
  readonly onSubmitRename: (value: string) => void;
  readonly onSubmitAdminToken: (value: string) => void;
  readonly onSubmitSecret: (secretName: string, value: string) => void;
}) {
  if (!props.open) {
    return null;
  }

  function handleShellKeyDown(event: React.KeyboardEvent) {
    if (event.key === "Escape") {
      event.preventDefault();
      if (props.mode.kind === "commands") {
        props.onClose();
      } else {
        props.onModeChange({ kind: "commands" });
      }
      return;
    }

    if (props.mode.kind !== "commands") {
      return;
    }

    if (event.key === "ArrowDown") {
      event.preventDefault();
      props.onSelectionChange(Math.min(props.selectedIndex + 1, props.commands.length - 1));
      return;
    }

    if (event.key === "ArrowUp") {
      event.preventDefault();
      props.onSelectionChange(Math.max(props.selectedIndex - 1, 0));
      return;
    }

    if (event.key === "Enter") {
      event.preventDefault();
      props.onExecuteCommand(props.commands[props.selectedIndex]);
    }
  }

  return (
    <div
      className="command-palette-backdrop"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) {
          props.onClose();
        }
      }}
    >
      <section
        aria-label="Command palette"
        aria-modal="true"
        className="command-palette"
        role="dialog"
        onKeyDown={handleShellKeyDown}
      >
        {props.mode.kind === "commands" ? (
          <>
            <div className="command-palette-input-row">
              <Search size={18} />
              <input
                ref={props.inputRef as React.RefObject<HTMLInputElement>}
                aria-label="Command palette"
                value={props.query}
                placeholder={
                  props.mode.scope === "node-create"
                    ? "Search nodes to add"
                    : "Type a command or component"
                }
                onChange={(event) => props.onQueryChange(event.target.value)}
              />
              <kbd>⌘P</kbd>
            </div>
            <div className="command-palette-list" role="listbox">
              {props.commands.length > 0 ? (
                props.commands.map((command, index) => (
                  <button
                    key={command.id}
                    aria-selected={index === props.selectedIndex}
                    className={
                      index === props.selectedIndex
                        ? "command-palette-command command-palette-command-active"
                        : "command-palette-command"
                    }
                    disabled={command.disabled}
                    role="option"
                    type="button"
                    onMouseEnter={() => props.onSelectionChange(index)}
                    onClick={() => props.onExecuteCommand(command)}
                  >
                    <span className="command-palette-command-main">
                      {command.iconKey ? (
                        <ProviderIcon
                          className="command-palette-command-icon"
                          provider={command.iconKey}
                          size={20}
                        />
                      ) : null}
                      <span className="command-palette-command-copy">
                        <strong>{command.label}</strong>
                        {command.detail ? <small>{command.detail}</small> : null}
                      </span>
                    </span>
                    <em>{command.group}</em>
                  </button>
                ))
              ) : (
                <p className="muted-text">No commands found</p>
              )}
            </div>
          </>
        ) : null}

        {props.mode.kind === "plan" ? (
          <PaletteTextForm
            inputRef={props.inputRef}
            label="Workflow prompt"
            title="Plan Workflow"
            value={props.mode.value}
            placeholder="Describe the workflow to build"
            submitLabel="Submit Plan"
            disabled={props.busyAction !== null || props.mode.value.trim().length === 0}
            onBack={() => props.onModeChange({ kind: "commands" })}
            onChange={(value) => props.onModeChange({ kind: "plan", value })}
            onSubmit={() =>
              props.onSubmitPlan(
                (props.mode as Extract<CommandPaletteMode, { kind: "plan" }>).value
              )
            }
          />
        ) : null}

        {props.mode.kind === "clarification" && props.clarification ? (
          <form
            className="command-palette-form"
            onSubmit={(event) => {
              event.preventDefault();
              props.onSubmitClarification();
            }}
          >
            <h2>Clarify First</h2>
            <p className="muted-text">{props.clarification.reason}</p>
            {props.clarification.questions.map((question, index) => (
              <label key={question.id}>
                {question.question}
                <textarea
                  ref={
                    index === 0
                      ? (props.inputRef as React.RefObject<HTMLTextAreaElement>)
                      : undefined
                  }
                  value={props.clarificationAnswers[question.id] ?? ""}
                  placeholder={question.placeholder}
                  rows={3}
                  required={question.required}
                  onChange={(event) =>
                    props.onClarificationAnswerChange(question.id, event.target.value)
                  }
                />
              </label>
            ))}
            <div className="command-palette-form-actions">
              <button type="button" onClick={() => props.onModeChange({ kind: "commands" })}>
                Back
              </button>
              <button
                type="submit"
                disabled={props.busyAction !== null || !props.clarificationReady}
              >
                Plan With Answers
              </button>
            </div>
          </form>
        ) : null}

        {props.mode.kind === "fork-branch" ? (
          <PaletteTextForm
            inputRef={props.inputRef}
            label="Fork name"
            title="Fork Branch"
            value={props.mode.value}
            placeholder="Experiment"
            submitLabel="Fork Branch"
            disabled={props.busyAction !== null || props.mode.value.trim().length === 0}
            onBack={() => props.onModeChange({ kind: "commands" })}
            onChange={(value) => props.onModeChange({ kind: "fork-branch", value })}
            onSubmit={() =>
              props.onSubmitFork(
                (props.mode as Extract<CommandPaletteMode, { kind: "fork-branch" }>).value
              )
            }
          />
        ) : null}

        {props.mode.kind === "rename-branch" ? (
          <PaletteTextForm
            inputRef={props.inputRef}
            label="Branch name"
            title="Rename Active Branch"
            value={props.mode.value}
            placeholder="Branch name"
            submitLabel="Rename"
            disabled={props.busyAction !== null || props.mode.value.trim().length === 0}
            onBack={() => props.onModeChange({ kind: "commands" })}
            onChange={(value) => props.onModeChange({ kind: "rename-branch", value })}
            onSubmit={() =>
              props.onSubmitRename(
                (props.mode as Extract<CommandPaletteMode, { kind: "rename-branch" }>).value
              )
            }
          />
        ) : null}

        {props.mode.kind === "admin-token" ? (
          <PaletteTextForm
            inputRef={props.inputRef}
            label="Admin token"
            title="Set Admin Token"
            type="password"
            value={props.mode.value}
            placeholder="Admin bearer token"
            submitLabel="Save Token"
            disabled={props.busyAction !== null}
            onBack={() => props.onModeChange({ kind: "commands" })}
            onChange={(value) => props.onModeChange({ kind: "admin-token", value })}
            onSubmit={() =>
              props.onSubmitAdminToken(
                (props.mode as Extract<CommandPaletteMode, { kind: "admin-token" }>).value
              )
            }
          />
        ) : null}

        {props.mode.kind === "secret" ? (
          <PaletteTextForm
            inputRef={props.inputRef}
            label={`${props.mode.label} secret`}
            title={`Save ${props.mode.label} Secret`}
            value={props.mode.value}
            placeholder="Paste secret JSON"
            submitLabel="Save Secret"
            disabled={props.busyAction !== null || props.mode.value.trim().length === 0}
            onBack={() => props.onModeChange({ kind: "commands" })}
            onChange={(value) => {
              const mode = props.mode as Extract<CommandPaletteMode, { kind: "secret" }>;
              props.onModeChange({
                kind: "secret",
                label: mode.label,
                secretName: mode.secretName,
                value
              });
            }}
            onSubmit={() => {
              const mode = props.mode as Extract<CommandPaletteMode, { kind: "secret" }>;
              props.onSubmitSecret(mode.secretName, mode.value);
            }}
          />
        ) : null}
      </section>
    </div>
  );
}

function PaletteTextForm(props: {
  readonly inputRef: { readonly current: HTMLInputElement | HTMLTextAreaElement | null };
  readonly title: string;
  readonly label: string;
  readonly value: string;
  readonly placeholder: string;
  readonly submitLabel: string;
  readonly disabled: boolean;
  readonly type?: "password" | "text" | undefined;
  readonly onBack: () => void;
  readonly onChange: (value: string) => void;
  readonly onSubmit: () => void;
}) {
  return (
    <form
      className="command-palette-form"
      onSubmit={(event) => {
        event.preventDefault();
        props.onSubmit();
      }}
    >
      <h2>{props.title}</h2>
      <label>
        {props.label}
        <input
          ref={props.inputRef as React.RefObject<HTMLInputElement>}
          type={props.type ?? "text"}
          value={props.value}
          placeholder={props.placeholder}
          onChange={(event) => props.onChange(event.target.value)}
        />
      </label>
      <div className="command-palette-form-actions">
        <button type="button" onClick={props.onBack}>
          Back
        </button>
        <button type="submit" disabled={props.disabled}>
          {props.submitLabel}
        </button>
      </div>
    </form>
  );
}
/* eslint-enable react-hooks/refs */

function StatusPopover(props: {
  readonly workflow: WorkflowSpec;
  readonly activeBranch: WorkflowBranch | null;
  readonly validationIssues: readonly WorkflowValidationIssue[];
  readonly runtimeTruth: WorkflowRuntimeTruthSnapshot | null;
  readonly run: WorkflowRunRecord | null;
  readonly activeJob: WorkflowJob | null;
  readonly deploymentNotice: string | null;
  readonly planAcceptedNotice: string | null;
  readonly apiError: string | null;
  readonly branchNotice: string | null;
  readonly onClose: () => void;
}) {
  return (
    <aside className="status-popover" aria-label="Workflow status">
      <div className="drawer-heading">
        <div>
          <p className="eyebrow">Status</p>
          <h2>{props.workflow.name}</h2>
        </div>
        <button className="icon-button" type="button" title="Close status" onClick={props.onClose}>
          <X size={16} />
        </button>
      </div>
      <dl className="detail-list compact-detail-list">
        <StatusRow label="Revision" value={String(props.workflow.revision)} tone="pending" />
        <StatusRow label="Branch" value={props.activeBranch?.name ?? "none"} tone="idle" />
        <StatusRow
          label="Validation"
          value={
            props.validationIssues.length > 0 ? `${props.validationIssues.length} issues` : "valid"
          }
          tone={props.validationIssues.length > 0 ? "blocked" : "valid"}
        />
        <StatusRow label="Runtime" value={props.runtimeTruth?.stage ?? "empty"} tone="pending" />
        <StatusRow
          label="Run"
          value={props.run?.status ?? props.activeJob?.status ?? "idle"}
          tone={props.run?.status ?? props.activeJob?.status ?? "idle"}
        />
      </dl>
      {props.validationIssues.length > 0 ? (
        <ul className="issue-list compact-issue-list">
          {props.validationIssues.slice(0, 4).map((issue, index) => (
            <li key={`${issue.code}-${index}`}>
              <strong>{issue.code}</strong>
              <span>{issue.message}</span>
            </li>
          ))}
        </ul>
      ) : null}
      {props.apiError ? <p className="error-text">{props.apiError}</p> : null}
      {props.branchNotice ? <p className="success-text">{props.branchNotice}</p> : null}
      {props.deploymentNotice ? <p className="success-text">{props.deploymentNotice}</p> : null}
      {props.planAcceptedNotice ? <p className="success-text">{props.planAcceptedNotice}</p> : null}
    </aside>
  );
}

function ToastStack(props: {
  readonly apiError: string | null;
  readonly branchNotice: string | null;
  readonly deploymentNotice: string | null;
  readonly planAcceptedNotice: string | null;
  readonly auditExportNotice: string | null;
  readonly decisionTraceExportNotice: string | null;
  readonly promotionNotice: string | null;
  readonly run: WorkflowRunRecord | null;
  readonly onDismissApiError: () => void;
  readonly onDismissBranchNotice: () => void;
}) {
  const latestRunNotice = props.run?.events.at(-1)?.message ?? null;
  const passiveNotices = [
    props.deploymentNotice,
    props.planAcceptedNotice,
    props.auditExportNotice,
    props.decisionTraceExportNotice,
    props.promotionNotice,
    latestRunNotice
  ].filter((notice): notice is string => Boolean(notice));

  if (!props.apiError && !props.branchNotice && passiveNotices.length === 0) {
    return null;
  }

  return (
    <div className="app-toast-stack" aria-live="polite">
      {props.apiError ? (
        <div className="toast-message toast-message-error">
          <p className="error-text">{props.apiError}</p>
          <button
            className="icon-button"
            type="button"
            title="Dismiss error"
            onClick={props.onDismissApiError}
          >
            <X size={14} />
          </button>
        </div>
      ) : null}
      {props.branchNotice ? (
        <div className="toast-message">
          <p className="success-text">{props.branchNotice}</p>
          <button
            className="icon-button"
            type="button"
            title="Dismiss notice"
            onClick={props.onDismissBranchNotice}
          >
            <X size={14} />
          </button>
        </div>
      ) : null}
      {passiveNotices.map((notice) => (
        <p className="success-text" key={notice}>
          {notice}
        </p>
      ))}
    </div>
  );
}

function Inspector(props: {
  readonly workflow: WorkflowSpec;
  readonly selectedNode: WorkflowNode | null;
  readonly selectedEdge: WorkflowSpec["edges"][number] | null;
  readonly activeTab: DetailsTab;
  readonly tabs: readonly { readonly id: DetailsTab; readonly label: string }[];
  readonly nodePrompt: string;
  readonly jsonError: string | null;
  readonly approvalDiff: WorkflowSpecDiff | null;
  readonly approvedRevision: WorkflowApprovedRevision | null;
  readonly run: WorkflowRunRecord | null;
  readonly taskRoute: WorkflowTaskRoute | null;
  readonly activeJob: WorkflowJob | null;
  readonly workspace: WorkflowWorkspace | null;
  readonly agentRuns: readonly unknown[];
  readonly runtimeTruth: WorkflowRuntimeTruthSnapshot | null;
  readonly providerConfigs: readonly WorkflowProviderRuntimeConfig[];
  readonly budgetPolicy: WorkflowBudgetPolicy | null;
  readonly budgetLedgers: readonly WorkflowBudgetLedger[];
  readonly agentTimeline: readonly WorkflowAgentTimelineEvent[];
  readonly connectors: readonly WorkflowConnectorRecord[];
  readonly workflowRuns: readonly WorkflowRunRecord[];
  readonly workflowSchedules: readonly WorkflowScheduleRecord[];
  readonly opsHealth: WorkflowOpsHealth | null;
  readonly routerEvalCases: readonly WorkflowRouterEvalCase[];
  readonly routerEvalRun: WorkflowRouterEvalRun | null;
  readonly agentMemory: readonly WorkflowAgentMemoryRecord[];
  readonly nodeDecisionTraces: readonly WorkflowNodeDecisionTrace[];
  readonly decisionTraceExportNotice: string | null;
  readonly planAcceptedNotice: string | null;
  readonly deploymentActivations: DeploymentActivationSummaryResponse | null;
  readonly activeRunnerDeployment: WorkflowDeploymentRecord | null;
  readonly busyAction: string | null;
  readonly branchLifecycleLocked: boolean;
  readonly onClose: () => void;
  readonly onTabChange: (tab: DetailsTab) => void;
  readonly onNodePromptChange: (value: string) => void;
  readonly onReprompt: () => void;
  readonly onBuildCodegen: () => void;
  readonly onReviewCodegen: () => void;
  readonly onPromoteCodegen: () => void;
  readonly onExportDecisionTraces: () => void;
  readonly onExportAudit: () => void;
  readonly onDeployRunner: () => void;
  readonly onDeployBundle: () => void;
  readonly onUndeployRunner: () => void;
  readonly onRollbackRunner: () => void;
  readonly onImportOpenApiConnector: (input: {
    readonly name: string;
    readonly sourceUrl: string;
  }) => void;
  readonly onRegisterMcpConnector: (input: {
    readonly name: string;
    readonly endpointUrl: string;
  }) => void;
  readonly onTestConnector: (connectorId: string) => void;
  readonly onDeleteConnector: (connectorId: string) => void;
  readonly onAddConnectorOperation: (
    connector: WorkflowConnectorRecord,
    operation: WorkflowConnectorOperation
  ) => void;
  readonly onRunRouterEvals: () => void;
  readonly onReplayRun: (runId: string) => void;
  readonly onPauseSchedule: (scheduleId: string) => void;
  readonly onResumeSchedule: (scheduleId: string) => void;
  readonly onUpdateNode: (nodeId: string, updater: (node: WorkflowNode) => WorkflowNode) => void;
  readonly onUpdateJsonField: (
    nodeId: string,
    field: "config" | "inputs" | "outputs",
    value: string
  ) => void;
}) {
  const node = props.selectedNode;
  const edge = props.selectedEdge;
  const drawerTitle = node ? node.label : edge ? "Selected Edge" : "Details";

  return (
    <>
      <div className="drawer-heading">
        <div>
          <p className="eyebrow">Details</p>
          <h2>{drawerTitle}</h2>
        </div>
        <button className="icon-button" type="button" title="Close details" onClick={props.onClose}>
          <X size={16} />
        </button>
      </div>
      <div className="details-tabs" role="tablist" aria-label="Details tabs">
        {props.tabs.map((tab) => (
          <button
            key={tab.id}
            type="button"
            role="tab"
            aria-selected={props.activeTab === tab.id}
            className={
              props.activeTab === tab.id ? "details-tab details-tab-active" : "details-tab"
            }
            onClick={() => props.onTabChange(tab.id)}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {props.activeTab === "node" ? (
        <div className="inspector-stack">
          {node ? (
            <>
              <label>
                Label
                <input
                  value={node.label}
                  onChange={(event) =>
                    props.onUpdateNode(node.id, (current) => ({
                      ...current,
                      label: event.target.value
                    }))
                  }
                />
              </label>
              <label>
                Description
                <textarea
                  value={node.description}
                  rows={3}
                  onChange={(event) =>
                    props.onUpdateNode(node.id, (current) => ({
                      ...current,
                      description: event.target.value
                    }))
                  }
                />
              </label>
              <StatusRow label="Kind" value={node.kind} tone="pending" />
              <StatusRow
                label="Inputs"
                value={String(Object.keys(node.inputs).length)}
                tone="idle"
              />
              <StatusRow
                label="Outputs"
                value={String(Object.keys(node.outputs).length)}
                tone="idle"
              />
              {node.kind === "skill" || node.kind === "delivery" ? (
                <label>
                  Adapter-backed skill
                  <select
                    value={node.skillId ?? ""}
                    onChange={(event) =>
                      props.onUpdateNode(node.id, (current) =>
                        applyAdapterSkillPreset(current, event.target.value)
                      )
                    }
                  >
                    <option value="">Unassigned</option>
                    {adapterSkillPresets
                      .filter((preset) => preset.nodeKinds.includes(node.kind))
                      .map((preset) => (
                        <option key={preset.id} value={preset.id}>
                          {preset.label}
                        </option>
                      ))}
                  </select>
                </label>
              ) : null}
              {node.kind === "delivery" ? (
                <div className="delivery-controls">
                  <label>
                    Primary channel
                    <select
                      value={String(node.config.channel ?? "sheets")}
                      onChange={(event) =>
                        props.onUpdateNode(node.id, (current) =>
                          updatePrimaryDeliveryChannel(current, event.target.value)
                        )
                      }
                    >
                      <option value="sheets">Sheets</option>
                      <option value="email">Email</option>
                    </select>
                  </label>
                  <div className="channel-checkboxes" aria-label="Secondary push channels">
                    <label>
                      <input
                        type="checkbox"
                        checked={deliveryChannels(node).has("whatsapp")}
                        onChange={(event) =>
                          props.onUpdateNode(node.id, (current) =>
                            toggleSecondaryDeliveryChannel(
                              current,
                              "whatsapp",
                              event.target.checked
                            )
                          )
                        }
                      />
                      WhatsApp
                    </label>
                    <label>
                      <input
                        type="checkbox"
                        checked={deliveryChannels(node).has("telegram")}
                        onChange={(event) =>
                          props.onUpdateNode(node.id, (current) =>
                            toggleSecondaryDeliveryChannel(
                              current,
                              "telegram",
                              event.target.checked
                            )
                          )
                        }
                      />
                      Telegram
                    </label>
                  </div>
                  <label>
                    Adapter
                    <input
                      value={(node.adapterIds ?? (node.adapterId ? [node.adapterId] : [])).join(
                        ", "
                      )}
                      onChange={(event) =>
                        props.onUpdateNode(node.id, (current) =>
                          updateAdapterIds(current, event.target.value)
                        )
                      }
                    />
                  </label>
                </div>
              ) : null}
              <label>
                Node Prompt
                <textarea
                  value={props.nodePrompt}
                  rows={3}
                  onChange={(event) => props.onNodePromptChange(event.target.value)}
                />
              </label>
              <button
                type="button"
                onClick={props.onReprompt}
                disabled={props.busyAction !== null || props.branchLifecycleLocked}
              >
                <WandSparkles size={18} />
                Reprompt Node
              </button>
              {node.kind === "codegen" ? (
                <section className="codegen-panel" aria-label="Generated code controls">
                  <StatusRow
                    label="Review"
                    value={node.codegen?.review.status ?? "missing"}
                    tone={node.codegen?.review.status ?? "blocked"}
                  />
                  <StatusRow
                    label="Replay"
                    value={node.codegen?.replay.mode ?? "missing"}
                    tone="pending"
                  />
                  {typeof node.config.reusedFromBranchId === "string" ? (
                    <StatusRow
                      label="Reuse"
                      value={node.config.reusedFromBranchId}
                      tone="pending"
                    />
                  ) : null}
                  <button
                    type="button"
                    onClick={props.onBuildCodegen}
                    disabled={props.busyAction !== null || props.branchLifecycleLocked}
                  >
                    <WandSparkles size={18} />
                    Build Generated Node
                  </button>
                  <button
                    type="button"
                    onClick={props.onReviewCodegen}
                    disabled={
                      props.busyAction !== null ||
                      props.branchLifecycleLocked ||
                      node.codegen?.review.status === "approved"
                    }
                  >
                    <CheckCircle2 size={18} />
                    Review Generated Code
                  </button>
                  <button
                    type="button"
                    onClick={props.onPromoteCodegen}
                    disabled={
                      props.busyAction !== null ||
                      props.branchLifecycleLocked ||
                      node.codegen?.review.status !== "approved"
                    }
                  >
                    <WandSparkles size={18} />
                    Promote Skill
                  </button>
                </section>
              ) : null}
            </>
          ) : edge ? (
            <dl className="detail-list">
              <div>
                <dt>Edge ID</dt>
                <dd>{edge.id}</dd>
              </div>
              <div>
                <dt>Source</dt>
                <dd>
                  {edge.source.nodeId}.{edge.source.port}
                </dd>
              </div>
              <div>
                <dt>Target</dt>
                <dd>
                  {edge.target.nodeId}.{edge.target.port}
                </dd>
              </div>
            </dl>
          ) : null}
        </div>
      ) : null}

      {props.activeTab === "config" ? (
        <div className="inspector-stack">
          {node ? (
            <>
              <label>
                Config
                <textarea
                  key={`config-${node.id}-${props.workflow.revision}`}
                  defaultValue={formatJson(node.config)}
                  rows={8}
                  onBlur={(event) => props.onUpdateJsonField(node.id, "config", event.target.value)}
                />
              </label>
              <div className="inline-grid">
                <label>
                  Timeout
                  <input
                    type="number"
                    min={1}
                    value={node.runtime.timeoutSeconds}
                    onChange={(event) =>
                      props.onUpdateNode(node.id, (current) => ({
                        ...current,
                        runtime: {
                          ...current.runtime,
                          timeoutSeconds: Number(event.target.value)
                        }
                      }))
                    }
                  />
                </label>
                <label>
                  Retries
                  <input
                    type="number"
                    min={0}
                    value={node.runtime.retry.maxAttempts}
                    onChange={(event) =>
                      props.onUpdateNode(node.id, (current) => ({
                        ...current,
                        runtime: {
                          ...current.runtime,
                          retry: {
                            ...current.runtime.retry,
                            maxAttempts: Number(event.target.value)
                          }
                        }
                      }))
                    }
                  />
                </label>
              </div>
              <div className="inline-grid">
                <label>
                  Inputs
                  <textarea
                    key={`inputs-${node.id}-${props.workflow.revision}`}
                    defaultValue={formatJson(node.inputs)}
                    rows={5}
                    onBlur={(event) =>
                      props.onUpdateJsonField(node.id, "inputs", event.target.value)
                    }
                  />
                </label>
                <label>
                  Outputs
                  <textarea
                    key={`outputs-${node.id}-${props.workflow.revision}`}
                    defaultValue={formatJson(node.outputs)}
                    rows={5}
                    onBlur={(event) =>
                      props.onUpdateJsonField(node.id, "outputs", event.target.value)
                    }
                  />
                </label>
              </div>
              {props.jsonError ? <p className="error-text">{props.jsonError}</p> : null}
            </>
          ) : edge ? (
            <pre className="result-view">{formatJson(edge)}</pre>
          ) : null}
        </div>
      ) : null}

      {props.activeTab === "trace" ? (
        <div className="inspector-stack">
          <DecisionTracePanel
            traces={props.nodeDecisionTraces}
            exportNotice={props.decisionTraceExportNotice}
            busyAction={props.busyAction}
            onExport={props.onExportDecisionTraces}
          />
          <ApprovalPanel diff={props.approvalDiff} approvedRevision={props.approvedRevision} />
          {props.planAcceptedNotice ? (
            <p className="success-text">{props.planAcceptedNotice}</p>
          ) : null}
        </div>
      ) : null}

      {props.activeTab === "runtime" ? (
        <div className="inspector-stack">
          <LifecycleTruthPanel truth={props.runtimeTruth} />
          <AgentRuntimePanel
            route={props.taskRoute}
            evalCases={props.routerEvalCases}
            evalRun={props.routerEvalRun}
            memories={props.agentMemory}
            traces={props.nodeDecisionTraces}
            busyAction={props.busyAction}
            onRunEvals={props.onRunRouterEvals}
          />
          <ProviderStatusPanel providers={props.providerConfigs} />
          <BudgetPanel policy={props.budgetPolicy} ledgers={props.budgetLedgers} />
          <JobPanel job={props.activeJob} />
          <WorkspacePanel workspace={props.workspace} agentRuns={props.agentRuns} />
          <RunPanel run={props.run} runs={props.workflowRuns} onReplay={props.onReplayRun} />
        </div>
      ) : null}

      {props.activeTab === "ops" ? (
        <div className="inspector-stack">
          <OpsHealthPanel health={props.opsHealth} />
          <ConnectorPanel
            connectors={props.connectors}
            busyAction={props.busyAction}
            onImportOpenApi={props.onImportOpenApiConnector}
            onRegisterMcp={props.onRegisterMcpConnector}
            onTest={props.onTestConnector}
            onDelete={props.onDeleteConnector}
            onAddOperation={props.onAddConnectorOperation}
          />
          <AgentTimelinePanel events={props.agentTimeline} activeJob={props.activeJob} />
          <DeploymentPanel
            activations={props.deploymentActivations}
            activeRunnerDeployment={props.activeRunnerDeployment}
            busyAction={props.busyAction}
            branchLifecycleLocked={props.branchLifecycleLocked}
            onDeployRunner={props.onDeployRunner}
            onDeployBundle={props.onDeployBundle}
            onUndeployRunner={props.onUndeployRunner}
            onRollbackRunner={props.onRollbackRunner}
            onExportAudit={props.onExportAudit}
          />
          <SchedulePanel
            schedules={props.workflowSchedules}
            busyAction={props.busyAction}
            onPause={props.onPauseSchedule}
            onResume={props.onResumeSchedule}
          />
        </div>
      ) : null}
    </>
  );
}

const lifecycleStages: readonly {
  readonly key: keyof Pick<
    WorkflowRuntimeTruthSnapshot,
    "planned" | "accepted" | "generated" | "evaluated" | "approved" | "deployed" | "runnable"
  >;
  readonly label: string;
}[] = [
  { key: "planned", label: "Planned" },
  { key: "accepted", label: "Accepted" },
  { key: "generated", label: "Generated" },
  { key: "evaluated", label: "Evaluated" },
  { key: "approved", label: "Approved" },
  { key: "deployed", label: "Deployed" },
  { key: "runnable", label: "Runnable" }
];

function LifecycleTruthPanel(props: { readonly truth: WorkflowRuntimeTruthSnapshot | null }) {
  return (
    <section className="run-panel" aria-label="Lifecycle truth">
      <h2>Runtime Truth</h2>
      <div className="lifecycle-rail">
        {lifecycleStages.map((stage) => (
          <span
            key={stage.key}
            className={
              props.truth?.[stage.key]
                ? "lifecycle-stage lifecycle-stage-active"
                : "lifecycle-stage"
            }
          >
            {stage.label}
          </span>
        ))}
      </div>
      <StatusRow
        label="Stage"
        value={props.truth?.stage ?? "empty"}
        tone={props.truth?.stage ?? "idle"}
      />
      {props.truth?.runnerDeploymentId ? (
        <StatusRow label="Runner" value={props.truth.runnerDeploymentId} tone="valid" />
      ) : null}
      {props.truth?.blockingReasons.length ? (
        <ul className="event-list">
          {props.truth.blockingReasons.map((reason) => (
            <li key={reason}>{reason}</li>
          ))}
        </ul>
      ) : null}
    </section>
  );
}

function ProviderStatusPanel(props: {
  readonly providers: readonly WorkflowProviderRuntimeConfig[];
}) {
  return (
    <section className="run-panel" aria-label="Provider status">
      <h2>Providers</h2>
      {props.providers.length === 0 ? (
        <StatusRow label="Status" value="unknown" tone="idle" />
      ) : (
        <ul className="event-list">
          {props.providers.map((provider) => (
            <li key={`${provider.role}-${provider.provider}`}>
              <strong>{provider.role}</strong> {provider.provider}/{provider.model}{" "}
              <span className={provider.configured ? "success-inline" : "warning-inline"}>
                {provider.configured ? "configured" : (provider.missingCredential ?? "missing key")}
              </span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function BudgetPanel(props: {
  readonly policy: WorkflowBudgetPolicy | null;
  readonly ledgers: readonly WorkflowBudgetLedger[];
}) {
  const actualCost = props.ledgers.reduce((sum, ledger) => sum + ledger.actualCostUsd, 0);
  const projectedCost = props.ledgers.reduce((sum, ledger) => sum + ledger.projectedCostUsd, 0);
  const remaining =
    props.ledgers[0]?.remainingCostUsd ??
    Math.max((props.policy?.maxWorkflowCostUsd ?? 0) - actualCost, 0);
  return (
    <section className="run-panel" aria-label="Budget ledger">
      <h2>Budget</h2>
      <StatusRow
        label="Workflow Max"
        value={props.policy ? formatUsd(props.policy.maxWorkflowCostUsd) : "unset"}
        tone={props.policy ? "valid" : "idle"}
      />
      <StatusRow label="Projected" value={formatUsd(projectedCost)} tone="pending" />
      <StatusRow label="Actual" value={formatUsd(actualCost)} tone="running" />
      <StatusRow
        label="Remaining"
        value={formatUsd(remaining)}
        tone={remaining > 0 ? "valid" : "blocked"}
      />
      {props.ledgers.length ? (
        <ul className="event-list">
          {props.ledgers.slice(0, 6).map((ledger) => (
            <li key={ledger.id}>
              <strong>{ledger.scope}</strong> {ledger.status} · actual{" "}
              {formatUsd(ledger.actualCostUsd)}
              {ledger.stopReason ? ` · ${ledger.stopReason}` : ""}
            </li>
          ))}
        </ul>
      ) : null}
    </section>
  );
}

function DecisionTracePanel(props: {
  readonly traces: readonly WorkflowNodeDecisionTrace[];
  readonly exportNotice: string | null;
  readonly busyAction: string | null;
  readonly onExport: () => void;
}) {
  const latestEvents = props.traces
    .flatMap((trace) => trace.events.map((event) => ({ trace, event })))
    .sort((left, right) => right.event.createdAt.localeCompare(left.event.createdAt))
    .slice(0, 8);
  const totalCostUsd = latestEvents.reduce((sum, item) => sum + (item.event.costUsd ?? 0), 0);
  const totalTokens = latestEvents.reduce((sum, item) => sum + (item.event.totalTokens ?? 0), 0);

  return (
    <section className="decision-trace-panel" aria-label="Node decision trace">
      <div className="panel-heading">
        <ListChecks size={18} />
        <h2>Decision Trace</h2>
      </div>
      <StatusRow
        label="Events"
        value={String(latestEvents.length)}
        tone={latestEvents.length > 0 ? "valid" : "idle"}
      />
      <StatusRow
        label="Tokens"
        value={formatTokenCount(totalTokens)}
        tone={totalTokens > 0 ? "valid" : "idle"}
      />
      <StatusRow
        label="Cost"
        value={formatUsd(totalCostUsd)}
        tone={totalCostUsd > 0 ? "valid" : "idle"}
      />
      <button
        type="button"
        onClick={props.onExport}
        disabled={props.busyAction === "export-decision-traces"}
      >
        <FileStack size={18} />
        Export Trace JSONL
      </button>
      {props.exportNotice ? <p className="success-text">{props.exportNotice}</p> : null}
      {latestEvents.length > 0 ? (
        <ul className="event-list">
          {latestEvents.map(({ trace, event }) => (
            <li key={event.id}>
              <strong>{event.role}</strong>
              <span>
                {event.selectedAction} · {trace.kind} · {event.evalOutcome ?? "not-run"}
              </span>
              <span>{event.rationale}</span>
              {event.alternativesConsidered.length > 0 ? (
                <span>Alternatives: {event.alternativesConsidered.join("; ")}</span>
              ) : null}
            </li>
          ))}
        </ul>
      ) : (
        <p className="muted-text">No decision trace has been recorded for this node yet.</p>
      )}
    </section>
  );
}

type WorkflowBranchPlanSuccessResponse = WorkflowPlanSuccessResponse & {
  readonly branch: WorkflowBranch;
  readonly promptTurn: WorkflowPromptTurn;
};

function isBranchPlanSuccessResponse(
  response: WorkflowPlanResponse | WorkflowBranchPlanResponse
): response is WorkflowBranchPlanSuccessResponse {
  return "branch" in response && "promptTurn" in response;
}

function integrationStatus(
  id: string,
  integrations: readonly IntegrationReadiness[],
  googleConnected: boolean | null
): { readonly label: string; readonly tone: string } {
  const readiness = integrations.find((candidate) => candidate.id === id);
  const ready = id === "google" ? (googleConnected ?? readiness?.ready ?? false) : readiness?.ready;
  return ready ? { label: "ready", tone: "valid" } : { label: "blocked", tone: "blocked" };
}

function commandMatchesQuery(command: PaletteCommand, query: string): boolean {
  const normalized = query.trim().toLowerCase();
  if (!normalized) {
    return true;
  }

  return [command.group, command.label, command.detail ?? "", ...(command.keywords ?? [])]
    .join(" ")
    .toLowerCase()
    .includes(normalized);
}

function ApprovalPanel(props: {
  readonly diff: WorkflowSpecDiff | null;
  readonly approvedRevision: WorkflowApprovedRevision | null;
}) {
  return (
    <section className="approval-panel" aria-label="Approval diff">
      <h2>Approval</h2>
      <StatusRow
        label="Revision"
        value={props.approvedRevision ? `r${props.approvedRevision.revision}` : "draft"}
        tone={props.approvedRevision ? "approved" : "pending"}
      />
      {props.diff ? (
        <>
          <ul className="diff-summary">
            {props.diff.summary.map((item) => (
              <li key={item}>{item}</li>
            ))}
          </ul>
          <pre className="diff-view" data-testid="approval-diff">
            {props.diff.lines
              .filter((line) => line.kind !== "same")
              .slice(0, 80)
              .map((line) => `${line.kind === "added" ? "+" : "-"} ${line.text}`)
              .join("\n")}
          </pre>
        </>
      ) : null}
    </section>
  );
}

function JobPanel(props: { readonly job: WorkflowJob | null }) {
  return (
    <section className="run-panel" aria-label="Job activity">
      <h2>Job</h2>
      <StatusRow
        label="Status"
        value={props.job?.status ?? "idle"}
        tone={props.job?.status ?? "idle"}
      />
      <StatusRow
        label="Worker"
        value={props.job?.workerId ?? "unclaimed"}
        tone={props.job?.workerId ? "valid" : "idle"}
      />
      <StatusRow
        label="Attempt"
        value={String(props.job?.retry.attempt ?? 0)}
        tone={props.job?.status === "failed" ? "blocked" : "pending"}
      />
      {props.job ? (
        <ul className="event-list">
          {props.job.events.slice(-8).map((event) => (
            <li key={event.id}>
              <strong>{event.level}</strong>
              <span>{event.message}</span>
            </li>
          ))}
        </ul>
      ) : null}
    </section>
  );
}

function WorkspacePanel(props: {
  readonly workspace: WorkflowWorkspace | null;
  readonly agentRuns: readonly unknown[];
}) {
  const agentRuns = props.agentRuns.map(agentRunView);
  const usageSummary = summarizeAgentUsage(agentRuns);
  return (
    <section className="run-panel" aria-label="Workspace artifacts">
      <h2>Workspace</h2>
      <StatusRow
        label="Artifacts"
        value={String(props.workspace?.artifactsProduced.length ?? 0)}
        tone={props.workspace ? "valid" : "idle"}
      />
      <StatusRow label="Agents" value={String(props.agentRuns.length)} tone="pending" />
      {agentRuns.length > 0 ? (
        <>
          <StatusRow
            label="Total Tokens"
            value={formatTokenCount(usageSummary.totalTokens)}
            tone={usageSummary.totalTokens > 0 ? "valid" : "idle"}
          />
          <StatusRow
            label="Total Cost"
            value={formatUsd(usageSummary.costUsd)}
            tone={usageSummary.costUsd > 0 ? "valid" : "idle"}
          />
        </>
      ) : null}
      {props.workspace ? (
        <ul className="event-list">
          {props.workspace.fileHashes.slice(0, 6).map((file) => (
            <li key={file.path}>
              <strong>{file.path}</strong>
              <span>{file.checksum.slice(0, 19)}</span>
            </li>
          ))}
        </ul>
      ) : null}
      {agentRuns.length > 0 ? (
        <ul className="event-list">
          {agentRuns.map((run, index) => (
            <li key={`${run.role}-${index}`}>
              <strong>{run.role}</strong>
              <span>
                {run.status} · {run.model} ·{" "}
                {run.hasUsage
                  ? `${formatTokenCount(run.totalTokens)} tokens · ${formatUsd(run.costUsd)}${formatTokenSplit(run)}`
                  : "usage n/a"}
              </span>
            </li>
          ))}
        </ul>
      ) : null}
      {props.workspace ? <pre className="result-view">{formatJson(props.workspace)}</pre> : null}
    </section>
  );
}

function AgentTimelinePanel(props: {
  readonly events: readonly WorkflowAgentTimelineEvent[];
  readonly activeJob: WorkflowJob | null;
}) {
  const totalCost = props.events.reduce((sum, event) => sum + (event.costUsd ?? 0), 0);
  return (
    <section className="run-panel" aria-label="Agent timeline">
      <h2>Agent Timeline</h2>
      <StatusRow
        label="Events"
        value={String(props.events.length)}
        tone={props.events.length ? "valid" : "idle"}
      />
      <StatusRow
        label="Cost"
        value={formatUsd(totalCost)}
        tone={totalCost > 0 ? "running" : "idle"}
      />
      <StatusRow
        label="Worker"
        value={props.activeJob?.workerId ?? "unclaimed"}
        tone={props.activeJob?.workerId ? "valid" : "idle"}
      />
      {props.events.length ? (
        <ul className="event-list">
          {props.events.slice(0, 8).map((event) => (
            <li key={event.id}>
              <strong>{event.role}</strong> {event.title} · {event.status}
              {event.decision ? ` · ${event.decision}` : ""}
              {event.fixTriageAction ? ` · ${event.fixTriageAction}` : ""}
              {event.totalTokens ? ` · ${formatTokenCount(event.totalTokens)} tokens` : ""}
              {event.costUsd ? ` · ${formatUsd(event.costUsd)}` : ""}
            </li>
          ))}
        </ul>
      ) : null}
    </section>
  );
}

function OpsHealthPanel(props: { readonly health: WorkflowOpsHealth | null }) {
  return (
    <section className="run-panel" aria-label="Operations health">
      <h2>Ops Health</h2>
      <StatusRow
        label="Status"
        value={props.health?.status ?? "unknown"}
        tone={props.health?.status === "ok" ? "valid" : "pending"}
      />
      <StatusRow
        label="Jobs"
        value={
          props.health
            ? `${props.health.worker.queuedJobs} queued / ${props.health.worker.failedJobs} failed`
            : "none"
        }
        tone={props.health?.worker.failedJobs ? "error" : "idle"}
      />
      <StatusRow
        label="Schedules"
        value={
          props.health
            ? `${props.health.scheduler.activeSchedules} active / ${props.health.scheduler.dueSchedules} due`
            : "none"
        }
        tone={props.health?.scheduler.dueSchedules ? "pending" : "idle"}
      />
      <StatusRow
        label="Connectors"
        value={
          props.health
            ? `${props.health.connectors.total} total / ${props.health.connectors.failedTests} failed`
            : "none"
        }
        tone={props.health?.connectors.failedTests ? "error" : "idle"}
      />
      <StatusRow
        label="Memory"
        value={
          props.health
            ? `${props.health.memory.total} records / ${props.health.memory.expired} expired`
            : "none"
        }
        tone={props.health?.memory.expired ? "pending" : "idle"}
      />
      <StatusRow
        label="Router"
        value={
          props.health
            ? `${props.health.router.classifierVersion} / ${props.health.router.evalCases} evals`
            : "unknown"
        }
        tone={props.health?.router.lastEvalPassed === false ? "error" : "idle"}
      />
    </section>
  );
}

function AgentRuntimePanel(props: {
  readonly route: WorkflowTaskRoute | null;
  readonly evalCases: readonly WorkflowRouterEvalCase[];
  readonly evalRun: WorkflowRouterEvalRun | null;
  readonly memories: readonly WorkflowAgentMemoryRecord[];
  readonly traces: readonly WorkflowNodeDecisionTrace[];
  readonly busyAction: string | null;
  readonly onRunEvals: () => void;
}) {
  const runtimeEvents = props.traces
    .flatMap((trace) => trace.events)
    .filter((event) => event.kind.startsWith("runtime."))
    .slice(-6)
    .reverse();
  const failedEvalCount = props.evalRun?.failed ?? 0;

  return (
    <section className="run-panel" aria-label="Agent runtime diagnostics">
      <h2>Agent Runtime</h2>
      <StatusRow
        label="Route"
        value={
          props.route ? `${props.route.route} / ${formatPercent(props.route.confidence)}` : "none"
        }
        tone={props.route ? "valid" : "idle"}
      />
      <StatusRow
        label="Classifier"
        value={props.route?.classifierVersion ?? props.evalRun?.classifierVersion ?? "unknown"}
        tone="idle"
      />
      <StatusRow
        label="Router Evals"
        value={
          props.evalRun
            ? `${props.evalRun.total - props.evalRun.failed}/${props.evalRun.total} passed`
            : `${props.evalCases.length} cases`
        }
        tone={failedEvalCount > 0 ? "error" : props.evalRun ? "valid" : "pending"}
      />
      <StatusRow
        label="Memory"
        value={`${props.memories.length} scoped record${props.memories.length === 1 ? "" : "s"}`}
        tone={props.memories.length > 0 ? "valid" : "idle"}
      />
      <button type="button" disabled={props.busyAction !== null} onClick={props.onRunEvals}>
        <ListChecks size={18} />
        Run Router Evals
      </button>
      {props.route?.scores.length ? (
        <ul className="event-list">
          {props.route.scores.slice(0, 5).map((score) => (
            <li key={score.route}>
              <strong>{score.route}</strong>
              <span>
                score {score.score} · {score.positiveSignals.join(", ") || "no signals"}
              </span>
            </li>
          ))}
        </ul>
      ) : null}
      {runtimeEvents.length > 0 ? (
        <ul className="event-list">
          {runtimeEvents.map((event) => (
            <li key={event.id}>
              <strong>{event.kind.replace("runtime.", "")}</strong>
              <span>{event.selectedAction}</span>
              <span>{event.rationale}</span>
            </li>
          ))}
        </ul>
      ) : null}
      {props.memories.length > 0 ? (
        <ul className="event-list">
          {props.memories.slice(0, 4).map((memory) => (
            <li key={memory.id}>
              <strong>{memory.scope}</strong>
              <span>{memory.tags.join(", ") || memory.namespace}</span>
              <span>{formatJson(memory.content).slice(0, 160)}</span>
            </li>
          ))}
        </ul>
      ) : null}
    </section>
  );
}

function ConnectorPanel(props: {
  readonly connectors: readonly WorkflowConnectorRecord[];
  readonly busyAction: string | null;
  readonly onImportOpenApi: (input: { readonly name: string; readonly sourceUrl: string }) => void;
  readonly onRegisterMcp: (input: { readonly name: string; readonly endpointUrl: string }) => void;
  readonly onTest: (connectorId: string) => void;
  readonly onDelete: (connectorId: string) => void;
  readonly onAddOperation: (
    connector: WorkflowConnectorRecord,
    operation: WorkflowConnectorOperation
  ) => void;
}) {
  const [openApiName, setOpenApiName] = useState("");
  const [openApiUrl, setOpenApiUrl] = useState("");
  const [mcpName, setMcpName] = useState("");
  const [mcpUrl, setMcpUrl] = useState("");

  return (
    <section className="run-panel" aria-label="Connectors">
      <h2>Connectors</h2>
      <div className="inline-grid">
        <label>
          OpenAPI Name
          <input value={openApiName} onChange={(event) => setOpenApiName(event.target.value)} />
        </label>
        <label>
          OpenAPI URL
          <input value={openApiUrl} onChange={(event) => setOpenApiUrl(event.target.value)} />
        </label>
      </div>
      <button
        type="button"
        disabled={props.busyAction !== null}
        onClick={() => props.onImportOpenApi({ name: openApiName, sourceUrl: openApiUrl })}
      >
        Import OpenAPI
      </button>
      <div className="inline-grid">
        <label>
          MCP Name
          <input value={mcpName} onChange={(event) => setMcpName(event.target.value)} />
        </label>
        <label>
          MCP Endpoint
          <input value={mcpUrl} onChange={(event) => setMcpUrl(event.target.value)} />
        </label>
      </div>
      <button
        type="button"
        disabled={props.busyAction !== null}
        onClick={() => props.onRegisterMcp({ name: mcpName, endpointUrl: mcpUrl })}
      >
        Register MCP
      </button>
      {props.connectors.length > 0 ? (
        <ul className="event-list">
          {props.connectors.map((connector) => (
            <li key={connector.id}>
              <strong>{connector.name}</strong>
              <span>
                {connector.kind} · {connector.operations.length} ops · {connector.lastTest.status}
              </span>
              <div className="deployment-actions">
                <button
                  type="button"
                  disabled={props.busyAction !== null}
                  onClick={() => props.onTest(connector.id)}
                >
                  Test
                </button>
                <button
                  type="button"
                  disabled={props.busyAction !== null}
                  onClick={() => props.onDelete(connector.id)}
                >
                  Delete
                </button>
              </div>
              {connector.operations.slice(0, 6).map((operation) => (
                <button
                  key={`${connector.id}-${operation.name}`}
                  type="button"
                  disabled={props.busyAction !== null}
                  onClick={() => props.onAddOperation(connector, operation)}
                >
                  Add {operation.name}
                </button>
              ))}
            </li>
          ))}
        </ul>
      ) : (
        <p className="muted-text">No connectors registered.</p>
      )}
    </section>
  );
}

function DeploymentPanel(props: {
  readonly activations: DeploymentActivationSummaryResponse | null;
  readonly activeRunnerDeployment: WorkflowDeploymentRecord | null;
  readonly busyAction: string | null;
  readonly branchLifecycleLocked: boolean;
  readonly onDeployRunner: () => void;
  readonly onDeployBundle: () => void;
  readonly onUndeployRunner: () => void;
  readonly onRollbackRunner: () => void;
  readonly onExportAudit: () => void;
}) {
  return (
    <section className="run-panel" aria-label="Deployment activations">
      <h2>Deployments</h2>
      <StatusRow
        label="Active"
        value={String(props.activations?.activeDeployments.length ?? 0)}
        tone={props.activations?.activeDeployments.length ? "valid" : "idle"}
      />
      <StatusRow
        label="Schedules"
        value={String(props.activations?.activeSchedules.length ?? 0)}
        tone={props.activations?.activeSchedules.length ? "valid" : "idle"}
      />
      <StatusRow
        label="Runners"
        value={String(props.activations?.runnerConfigurations.length ?? 0)}
        tone={props.activations?.runnerConfigurations.length ? "valid" : "idle"}
      />
      <StatusRow
        label="Active Runner"
        value={props.activeRunnerDeployment?.id ?? "none"}
        tone={props.activeRunnerDeployment ? "valid" : "blocked"}
      />
      <div className="deployment-actions">
        <button
          type="button"
          onClick={props.onDeployRunner}
          disabled={props.busyAction !== null || props.branchLifecycleLocked}
          title={
            props.branchLifecycleLocked
              ? "Archived branches are read-only."
              : "Create a runner.configuration deployment for production runs."
          }
        >
          Deploy Runner
        </button>
        <button
          type="button"
          onClick={props.onDeployBundle}
          disabled={props.busyAction !== null || props.branchLifecycleLocked}
          title="Export a local workflow bundle artifact."
        >
          Export Bundle
        </button>
        <button
          type="button"
          onClick={props.onUndeployRunner}
          disabled={
            props.busyAction !== null ||
            props.branchLifecycleLocked ||
            !props.activeRunnerDeployment
          }
        >
          Undeploy Runner
        </button>
        <button
          type="button"
          onClick={props.onRollbackRunner}
          disabled={
            props.busyAction !== null ||
            props.branchLifecycleLocked ||
            !props.activeRunnerDeployment
          }
        >
          Rollback Runner
        </button>
        <button type="button" onClick={props.onExportAudit} disabled={props.busyAction !== null}>
          Export Audit JSONL
        </button>
      </div>
      {props.activations ? (
        <pre className="result-view">
          {formatJson(deploymentActivationPreview(props.activations))}
        </pre>
      ) : null}
    </section>
  );
}

function agentRunView(run: unknown): {
  readonly role: string;
  readonly status: string;
  readonly model: string;
  readonly inputTokens: number;
  readonly outputTokens: number;
  readonly cacheReadInputTokens: number;
  readonly cacheCreationInputTokens: number;
  readonly totalTokens: number;
  readonly costUsd: number;
  readonly hasUsage: boolean;
} {
  const record = jsonObject(run);
  const invocations = Array.isArray(record.modelInvocations)
    ? record.modelInvocations.map(jsonObject)
    : [];
  const inputTokens = usageNumber(record, invocations, "inputTokens");
  const outputTokens = usageNumber(record, invocations, "outputTokens");
  const cacheReadInputTokens = usageNumber(record, invocations, "cacheReadInputTokens");
  const cacheCreationInputTokens = usageNumber(record, invocations, "cacheCreationInputTokens");
  const invocationTotalTokens = usageNumber(record, invocations, "totalTokens");
  const totalTokens =
    invocationTotalTokens > 0 ? invocationTotalTokens : inputTokens + outputTokens;
  const costUsd = usageNumber(record, invocations, "costUsd");
  const hasUsage =
    inputTokens > 0 ||
    outputTokens > 0 ||
    cacheReadInputTokens > 0 ||
    cacheCreationInputTokens > 0 ||
    totalTokens > 0 ||
    costUsd > 0;

  return {
    role: typeof record.role === "string" ? record.role : "agent",
    status: typeof record.status === "string" ? record.status : "unknown",
    model: typeof record.model === "string" ? record.model : "none",
    inputTokens,
    outputTokens,
    cacheReadInputTokens,
    cacheCreationInputTokens,
    totalTokens,
    costUsd,
    hasUsage
  };
}

function usageNumber(
  record: Record<string, unknown>,
  invocations: readonly Record<string, unknown>[],
  field: string
): number {
  const recordValue = record[field];
  if (typeof recordValue === "number" && Number.isFinite(recordValue)) {
    return recordValue;
  }

  return invocations.reduce((total, invocation) => {
    const value = invocation[field];
    return typeof value === "number" && Number.isFinite(value) ? total + value : total;
  }, 0);
}

function summarizeAgentUsage(
  agentRuns: readonly ReturnType<typeof agentRunView>[]
): ReturnType<typeof agentRunView> {
  return agentRuns.reduce(
    (summary, run) => ({
      role: "all-agents",
      status: "summary",
      model: "all-models",
      inputTokens: summary.inputTokens + run.inputTokens,
      outputTokens: summary.outputTokens + run.outputTokens,
      cacheReadInputTokens: summary.cacheReadInputTokens + run.cacheReadInputTokens,
      cacheCreationInputTokens: summary.cacheCreationInputTokens + run.cacheCreationInputTokens,
      totalTokens: summary.totalTokens + run.totalTokens,
      costUsd: summary.costUsd + run.costUsd,
      hasUsage: summary.hasUsage || run.hasUsage
    }),
    {
      role: "all-agents",
      status: "summary",
      model: "all-models",
      inputTokens: 0,
      outputTokens: 0,
      cacheReadInputTokens: 0,
      cacheCreationInputTokens: 0,
      totalTokens: 0,
      costUsd: 0,
      hasUsage: false
    }
  );
}

function formatTokenSplit(run: ReturnType<typeof agentRunView>): string {
  const parts = [
    run.inputTokens > 0 ? `${formatTokenCount(run.inputTokens)} in` : "",
    run.outputTokens > 0 ? `${formatTokenCount(run.outputTokens)} out` : "",
    run.cacheReadInputTokens > 0 ? `${formatTokenCount(run.cacheReadInputTokens)} cache read` : "",
    run.cacheCreationInputTokens > 0
      ? `${formatTokenCount(run.cacheCreationInputTokens)} cache write`
      : ""
  ].filter(Boolean);

  return parts.length > 0 ? ` (${parts.join(", ")})` : "";
}

function formatTokenCount(value: number): string {
  return new Intl.NumberFormat("en-US", { maximumFractionDigits: 0 }).format(value);
}

function formatUsd(value: number): string {
  return `$${value.toFixed(4)}`;
}

function formatPercent(value: number): string {
  return `${Math.round(value * 100)}%`;
}

function formatDateTime(value: string): string {
  const timestamp = Date.parse(value);
  if (!Number.isFinite(timestamp)) {
    return value;
  }

  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "short",
    timeStyle: "short"
  }).format(new Date(timestamp));
}

function jsonObject(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}

function deploymentActivationPreview(activations: DeploymentActivationSummaryResponse): JsonRecord {
  return {
    activeDeployments: activations.activeDeployments.map((deployment) => ({
      id: deployment.id,
      kind: deployment.kind,
      status: deployment.status
    })),
    activeSchedules: [...activations.activeSchedules],
    runnerConfigurations: [...activations.runnerConfigurations],
    skillPublications: [...activations.skillPublications],
    integrationBindings: [...activations.integrationBindings],
    generatedServices: [...activations.generatedServices]
  };
}

function SchedulePanel(props: {
  readonly schedules: readonly WorkflowScheduleRecord[];
  readonly busyAction: string | null;
  readonly onPause: (scheduleId: string) => void;
  readonly onResume: (scheduleId: string) => void;
}) {
  return (
    <section className="run-panel" aria-label="Schedules">
      <h2>Schedules</h2>
      <StatusRow
        label="Active"
        value={String(props.schedules.filter((schedule) => schedule.status === "active").length)}
        tone={props.schedules.some((schedule) => schedule.status === "active") ? "valid" : "idle"}
      />
      {props.schedules.length > 0 ? (
        <ul className="event-list">
          {props.schedules.map((schedule) => (
            <li key={schedule.id}>
              <strong>{schedule.label}</strong>
              <span>
                {schedule.status} · {schedule.cron} · next {formatDateTime(schedule.nextFireAt)}
              </span>
              <div className="deployment-actions">
                <button
                  type="button"
                  disabled={props.busyAction !== null || schedule.status !== "active"}
                  onClick={() => props.onPause(schedule.id)}
                >
                  Pause
                </button>
                <button
                  type="button"
                  disabled={props.busyAction !== null || schedule.status === "active"}
                  onClick={() => props.onResume(schedule.id)}
                >
                  Resume
                </button>
              </div>
            </li>
          ))}
        </ul>
      ) : (
        <p className="muted-text">No deployed schedules.</p>
      )}
    </section>
  );
}

function RunPanel(props: {
  readonly run: WorkflowRunRecord | null;
  readonly runs: readonly WorkflowRunRecord[];
  readonly onReplay: (runId: string) => void;
}) {
  return (
    <section className="run-panel" aria-label="Run status">
      <h2>Run</h2>
      <StatusRow
        label="Status"
        value={props.run?.status ?? "idle"}
        tone={props.run?.status ?? "idle"}
      />
      {props.run ? (
        <>
          <ul className="event-list">
            {props.run.events.map((event) => (
              <li key={event.id}>
                <strong>{event.level}</strong>
                <span>{event.message}</span>
              </li>
            ))}
          </ul>
          <pre className="result-view">{formatJson(props.run.result ?? {})}</pre>
        </>
      ) : null}
      {props.runs.length > 0 ? (
        <ul className="event-list">
          {props.runs.slice(0, 8).map((run) => (
            <li key={run.id}>
              <strong>{run.status}</strong>
              <span>
                {run.id} · {formatDateTime(run.finishedAt)}
              </span>
              {run.status === "failed" ? (
                <button type="button" onClick={() => props.onReplay(run.id)}>
                  Replay
                </button>
              ) : null}
            </li>
          ))}
        </ul>
      ) : null}
    </section>
  );
}

function StatusRow(props: {
  readonly label: string;
  readonly value: string;
  readonly tone: string;
}) {
  return (
    <div className="status-row">
      <span>{props.label}</span>
      <strong className={`status-pill status-${props.tone}`}>{props.value}</strong>
    </div>
  );
}

function isEditableTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) {
    return false;
  }

  return Boolean(target.closest("input, textarea, select, [contenteditable='true']"));
}

function actionBlockedReason(
  busyAction: string | null,
  branchLifecycleLocked: boolean
): string | null {
  if (branchLifecycleLocked) {
    return "Archived branches are read-only.";
  }
  if (busyAction) {
    return `Wait for ${busyAction} to finish.`;
  }
  return null;
}

function formatJson(value: unknown): string {
  return JSON.stringify(value, null, 2);
}

function parseJsonRecord(
  value: string
):
  | { readonly ok: true; readonly value: JsonRecord }
  | { readonly ok: false; readonly error: string } {
  try {
    const parsed = JSON.parse(value) as unknown;
    if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
      return { ok: false, error: "JSON value must be an object." };
    }

    return { ok: true, value: parsed as JsonRecord };
  } catch (error) {
    return {
      ok: false,
      error: error instanceof Error ? error.message : "Invalid JSON."
    };
  }
}

function mergeResolutionForConflict(
  conflict: WorkflowBranchMergeConflict,
  modes: Readonly<Record<string, "source" | "target" | "manual">>,
  manualJson: Readonly<Record<string, string>>
): WorkflowBranchMergeResolution {
  const mode = modes[conflict.id] ?? "target";
  if (mode === "manual") {
    return {
      conflictId: conflict.id,
      choice: "manual",
      value: JSON.parse(manualJson[conflict.id] ?? "null") as JsonValue
    };
  }

  return {
    conflictId: conflict.id,
    choice: mode
  };
}

function applyAdapterSkillPreset(node: WorkflowNode, skillId: string): WorkflowNode {
  const preset = adapterSkillPresets.find((candidate) => candidate.id === skillId);
  if (!preset) {
    return withoutAdapterSkill(node);
  }

  return {
    ...node,
    skillId: preset.id,
    adapterId: preset.adapterIds[0],
    adapterIds: preset.adapterIds,
    adapterOperations: preset.adapterOperations,
    secretRefs: preset.secretRefs,
    config: {
      ...node.config,
      ...preset.config
    }
  };
}

function withoutAdapterSkill(node: WorkflowNode): WorkflowNode {
  return {
    id: node.id,
    kind: node.kind,
    label: node.label,
    description: node.description,
    inputs: node.inputs,
    outputs: node.outputs,
    config: node.config,
    runtime: node.runtime,
    determinism: node.determinism,
    ...(node.codegen ? { codegen: node.codegen } : {})
  };
}

function updatePrimaryDeliveryChannel(node: WorkflowNode, channel: string): WorkflowNode {
  const channels = deliveryChannels(node);
  const nextChannels = new Set(
    [...channels].filter((candidate) => candidate !== "email" && candidate !== "sheets")
  );
  nextChannels.add(channel);

  return withDeliveryAdapters({
    ...node,
    config: {
      ...node.config,
      channel,
      channels: [...nextChannels].sort()
    }
  });
}

function toggleSecondaryDeliveryChannel(
  node: WorkflowNode,
  channel: "whatsapp" | "telegram",
  enabled: boolean
): WorkflowNode {
  const channels = new Set(deliveryChannels(node));
  if (enabled) {
    channels.add(channel);
  } else {
    channels.delete(channel);
  }

  return withDeliveryAdapters({
    ...node,
    config: {
      ...node.config,
      channels: [...channels].sort()
    }
  });
}

function updateAdapterIds(node: WorkflowNode, value: string): WorkflowNode {
  const adapterIds = value
    .split(",")
    .map((adapterId) => adapterId.trim())
    .filter(Boolean);

  return {
    ...node,
    ...(adapterIds[0] ? { adapterId: adapterIds[0] } : {}),
    adapterIds,
    adapterOperations: node.adapterOperations?.filter((operation) =>
      adapterIds.includes(operation.adapterId)
    )
  };
}

function withDeliveryAdapters(node: WorkflowNode): WorkflowNode {
  const declarations = adapterDeclarationsForChannels(deliveryChannels(node));

  return {
    ...node,
    adapterId: declarations.adapterIds[0],
    adapterIds: declarations.adapterIds,
    adapterOperations: declarations.adapterOperations,
    secretRefs: {
      ...(node.secretRefs ?? {}),
      ...declarations.secretRefs
    },
    config: {
      ...node.config,
      allowedHosts: mergeAllowedHosts(node.config.allowedHosts, declarations.allowedHosts)
    }
  };
}

function adapterDeclarationsForChannels(channels: ReadonlySet<string>): {
  readonly adapterIds: readonly string[];
  readonly adapterOperations: readonly WorkflowAdapterOperationRef[];
  readonly secretRefs: Readonly<Record<string, string>>;
  readonly allowedHosts: readonly string[];
} {
  const adapterIds: string[] = [];
  const adapterOperations: WorkflowAdapterOperationRef[] = [];
  const secretRefs: Record<string, string> = {};
  const allowedHosts = new Set<string>();

  for (const channel of [...channels].sort()) {
    const declaration = adapterDeclarationForChannel(channel);
    if (!declaration) {
      continue;
    }
    adapterIds.push(declaration.adapterId);
    adapterOperations.push(declaration.operation);
    Object.assign(secretRefs, declaration.secretRefs);
    for (const host of declaration.allowedHosts) {
      allowedHosts.add(host);
    }
  }

  return {
    adapterIds,
    adapterOperations,
    secretRefs,
    allowedHosts: [...allowedHosts].sort()
  };
}

function adapterDeclarationForChannel(channel: string):
  | {
      readonly adapterId: string;
      readonly operation: WorkflowAdapterOperationRef;
      readonly secretRefs: Readonly<Record<string, string>>;
      readonly allowedHosts: readonly string[];
    }
  | undefined {
  switch (channel) {
    case "email":
      return {
        adapterId: "adapter.email",
        operation: {
          adapterId: "adapter.email",
          operation: "email.results.send",
          operationVersion: "1.0.0"
        },
        secretRefs: { "email.delivery": "secret:email.smtp.default" },
        allowedHosts: ["smtp"]
      };
    case "sheets":
      return {
        adapterId: "adapter.sheets",
        operation: {
          adapterId: "adapter.sheets",
          operation: "sheets.rows.append",
          operationVersion: "1.0.0"
        },
        secretRefs: { "sheets.oauth": "secret:google.oauth.default" },
        allowedHosts: ["oauth2.googleapis.com", "sheets.googleapis.com"]
      };
    case "whatsapp":
      return {
        adapterId: "adapter.whatsapp",
        operation: {
          adapterId: "adapter.whatsapp",
          operation: "whatsapp.alert.send",
          operationVersion: "1.0.0"
        },
        secretRefs: { "whatsapp.apiKey": "secret:whatsapp.cloud.default" },
        allowedHosts: ["graph.facebook.com"]
      };
    case "telegram":
      return {
        adapterId: "adapter.telegram",
        operation: {
          adapterId: "adapter.telegram",
          operation: "telegram.alert.send",
          operationVersion: "1.0.0"
        },
        secretRefs: { "telegram.botToken": "secret:telegram.bot.default" },
        allowedHosts: ["api.telegram.org"]
      };
    default:
      return undefined;
  }
}

function mergeAllowedHosts(
  existing: JsonRecord[string] | undefined,
  additional: readonly string[]
): string[] {
  const hosts = new Set<string>();
  if (Array.isArray(existing)) {
    for (const host of existing) {
      if (typeof host === "string") {
        hosts.add(host);
      }
    }
  }
  for (const host of additional) {
    hosts.add(host);
  }

  return [...hosts].sort();
}

function applyNodeSelection(
  nodes: WorkflowFlowNode[],
  selectedNodeId: string | null
): WorkflowFlowNode[] {
  let changed = false;
  const nextNodes = nodes.map((node) => {
    const selected = node.id === selectedNodeId;
    if (Boolean(node.selected) === selected) {
      return node;
    }
    changed = true;
    return {
      ...node,
      selected
    };
  });

  return changed ? nextNodes : nodes;
}

function applyEdgeSelection(
  edges: WorkflowFlowEdge[],
  selectedEdgeId: string | null
): WorkflowFlowEdge[] {
  let changed = false;
  const nextEdges = edges.map((edge) => {
    const selected = edge.id === selectedEdgeId;
    if (Boolean(edge.selected) === selected) {
      return edge;
    }
    changed = true;
    return {
      ...edge,
      selected
    };
  });

  return changed ? nextEdges : edges;
}

function deliveryChannels(node: WorkflowNode): ReadonlySet<string> {
  const channels = new Set<string>();
  const configuredChannels = node.config.channels;
  if (Array.isArray(configuredChannels)) {
    for (const channel of configuredChannels) {
      if (typeof channel === "string") {
        channels.add(channel);
      }
    }
  }
  if (typeof node.config.channel === "string") {
    channels.add(node.config.channel);
  }
  if (channels.size === 0 && node.kind === "delivery") {
    channels.add("email");
  }

  return channels;
}

function categoryLabel(category: ComponentPaletteFilter): string {
  if (category === "all") {
    return "All components";
  }

  return componentCategoryLabels[category] ?? "Components";
}

function uniqueComponentNodeId(baseId: string, nodes: readonly WorkflowNode[]): string {
  const existing = new Set(nodes.map((node) => node.id));
  let index = 1;
  let id = baseId;
  while (existing.has(id)) {
    index += 1;
    id = `${baseId}-${index}`;
  }

  return id;
}

function uniqueEdgeId(
  sourceId: string,
  targetId: string,
  edges: readonly { readonly id: string }[]
): string {
  const prefix = `edge.${sourceId}.${targetId}`;
  const existing = new Set(edges.map((edge) => edge.id));
  let index = 1;
  let id = `${prefix}.${index}`;
  while (existing.has(id)) {
    index += 1;
    id = `${prefix}.${index}`;
  }

  return id;
}

export function workflowJsonForDiff(workflow: WorkflowSpec): string {
  return stableWorkflowStringify(workflow);
}
