import { useCallback, useEffect, useMemo, useState } from "react";
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
  ChevronRight,
  Clock3,
  Database,
  FileStack,
  GitBranch,
  Grid2X2,
  History,
  KeyRound,
  Layers3,
  ListChecks,
  Mail,
  MessageCircle,
  Paperclip,
  Play,
  Plus,
  RefreshCw,
  Search,
  Send,
  ShieldCheck,
  SlidersHorizontal,
  Table2,
  Trash2,
  Unplug,
  WandSparkles,
  XCircle
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
  WorkflowDraftEvaluation,
  WorkflowAdapterOperationRef,
  WorkflowApprovedRevision,
  WorkflowGeneratedModuleReuseDecision,
  WorkflowJob,
  JsonValue,
  WorkflowWorkspace,
  WorkflowNode,
  WorkflowNodeKind,
  WorkflowPlanResponse,
  WorkflowPlanSuccessResponse,
  WorkflowPlannerFeedback,
  WorkflowPlannerSuggestion,
  WorkflowPromptTurn,
  WorkflowRunRecord,
  WorkflowSpec,
  WorkflowSpecDiff,
  WorkflowTaskRoute,
  WorkflowValidationIssue,
  WorkflowValidationResult
} from "@kelpclaw/workflow-spec";
import { openClawApi, readOpenClawAdminToken, saveOpenClawAdminToken } from "./api-client.js";
import type {
  DeploymentActivationSummaryResponse,
  IntegrationReadiness,
  SecretMetadata
} from "./api-client.js";
import {
  firstInputPort,
  firstOutputPort,
  nextNodePosition,
  nodeKindLabel,
  workflowNodeTypes,
  workflowToEdges,
  workflowToNodes
} from "./workflow-elements.js";
import type { WorkflowFlowEdge, WorkflowFlowNode } from "./workflow-elements.js";
import "./styles.css";

const nodeKinds: readonly WorkflowNodeKind[] = [
  "trigger",
  "skill",
  "codegen",
  "transform",
  "approval",
  "delivery"
];

const defaultPrompt = "";
const emptyWorkflowDraft = createWorkflowSpec({
  id: "workflow.openclaw-draft",
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

const componentCategories = [
  { id: "input-output", label: "Input & Output", icon: Unplug },
  { id: "data-sources", label: "Data Sources", icon: Database },
  { id: "models-agents", label: "Models & Agents", icon: Layers3 },
  { id: "llm-operations", label: "LLM Operations", icon: WandSparkles },
  { id: "files-knowledge", label: "Files & Knowledge", icon: FileStack },
  { id: "processing", label: "Processing", icon: SlidersHorizontal },
  { id: "flow-control", label: "Flow Control", icon: GitBranch }
] as const;

type ComponentCategoryId = (typeof componentCategories)[number]["id"];
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
    config: { trigger: "webhook", path: "/webhooks/openclaw" }
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

const railItems = [
  { label: "Search", icon: Search },
  { label: "Components", icon: Grid2X2 },
  { label: "Attachments", icon: Paperclip },
  { label: "History", icon: History }
] as const;

const integrationSetups = [
  {
    id: "google",
    label: "Google",
    icon: Table2,
    secretName: "google.oauth.default",
    placeholder: '{"refreshToken":"...","clientId":"...","clientSecret":"..."}'
  },
  {
    id: "smtp",
    label: "SMTP",
    icon: Mail,
    secretName: "email.smtp.default",
    placeholder: '{"host":"smtp.example.com","port":587,"username":"...","password":"..."}'
  },
  {
    id: "whatsapp",
    label: "WhatsApp",
    icon: MessageCircle,
    secretName: "whatsapp.cloud.default",
    placeholder: '{"accessToken":"...","phoneNumberId":"...","apiVersion":"v20.0"}'
  },
  {
    id: "telegram",
    label: "Telegram",
    icon: Send,
    secretName: "telegram.bot.default",
    placeholder: '{"botToken":"...","chatId":"..."}'
  }
] as const;

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
  const [deploymentNotice, setDeploymentNotice] = useState<string | null>(null);
  const [planAcceptedNotice, setPlanAcceptedNotice] = useState<string | null>(null);
  const [deploymentActivations, setDeploymentActivations] =
    useState<DeploymentActivationSummaryResponse | null>(null);
  const [branches, setBranches] = useState<readonly WorkflowBranch[]>([]);
  const [activeBranchId, setActiveBranchId] = useState<string | null>(null);
  const [promptTurns, setPromptTurns] = useState<readonly WorkflowPromptTurn[]>([]);
  const [branchNameDraft, setBranchNameDraft] = useState("Experiment");
  const [branchRenameDraft, setBranchRenameDraft] = useState("");
  const [showArchivedBranches, setShowArchivedBranches] = useState(false);
  const [branchNotice, setBranchNotice] = useState<string | null>(null);
  const [mergeSourceBranchId, setMergeSourceBranchId] = useState<string>("");
  const [mergeMode, setMergeMode] = useState<"merge" | "cherry-pick">("merge");
  const [mergePreview, setMergePreview] = useState<WorkflowBranchMergePreview | null>(null);
  const [mergeResolutionModes, setMergeResolutionModes] = useState<
    Readonly<Record<string, "source" | "target" | "manual">>
  >({});
  const [mergeManualJson, setMergeManualJson] = useState<Readonly<Record<string, string>>>({});
  const [reuseDecisions, setReuseDecisions] = useState<
    readonly WorkflowGeneratedModuleReuseDecision[]
  >([]);
  const [dirtyNodeIds, setDirtyNodeIds] = useState<ReadonlySet<string>>(new Set());
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [selectedEdgeId, setSelectedEdgeId] = useState<string | null>(null);
  const [nodePrompt, setNodePrompt] = useState("");
  const [jsonError, setJsonError] = useState<string | null>(null);
  const [busyAction, setBusyAction] = useState<string | null>(null);
  const [apiError, setApiError] = useState<string | null>(null);
  const [promotionNotice, setPromotionNotice] = useState<string | null>(null);
  const [adminToken, setAdminToken] = useState(readOpenClawAdminToken);
  const [integrationReadiness, setIntegrationReadiness] = useState<readonly IntegrationReadiness[]>(
    []
  );
  const [secretMetadata, setSecretMetadata] = useState<readonly SecretMetadata[]>([]);
  const [googleConnected, setGoogleConnected] = useState<boolean | null>(null);
  const [secretDrafts, setSecretDrafts] = useState<Readonly<Record<string, string>>>({});
  const [componentSearch, setComponentSearch] = useState("");
  const [selectedComponentCategory, setSelectedComponentCategory] =
    useState<ComponentPaletteFilter>("input-output");

  const validationIssues = validation.ok ? [] : validation.errors;
  const [nodes, setNodes, onNodesChangeBase] = useNodesState<WorkflowFlowNode>(
    workflowToNodes(workflow, validationIssues)
  );
  const [edges, setEdges, onEdgesChangeBase] = useEdgesState<WorkflowFlowEdge>(
    workflowToEdges(workflow, validationIssues)
  );

  const selectedNode = useMemo(
    () => workflow.nodes.find((node) => node.id === selectedNodeId) ?? workflow.nodes[0] ?? null,
    [selectedNodeId, workflow.nodes]
  );
  const selectedEdge = useMemo(
    () => workflow.edges.find((edge) => edge.id === selectedEdgeId) ?? null,
    [selectedEdgeId, workflow.edges]
  );
  const activeBranch = useMemo(
    () => branches.find((branch) => branch.id === activeBranchId) ?? null,
    [activeBranchId, branches]
  );
  const branchLifecycleLocked = activeBranch?.status === "archived";
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
  const canApprove =
    workflowHasGraph &&
    validation.ok &&
    draftEvaluation?.readyForApproval === true &&
    !branchLifecycleLocked;
  const canRun = approvedRevision !== null && !branchLifecycleLocked;
  const visibleComponentItems = useMemo(
    () =>
      componentPaletteItems.filter((item) => {
        const search = componentSearch.trim().toLowerCase();
        const matchesCategory =
          selectedComponentCategory === "all" || item.category === selectedComponentCategory;
        const matchesSearch =
          search.length === 0 ||
          [item.label, item.description, item.kind, categoryLabel(item.category)]
            .join(" ")
            .toLowerCase()
            .includes(search);

        return matchesCategory && matchesSearch;
      }),
    [componentSearch, selectedComponentCategory]
  );

  const refreshIntegrations = useCallback(async () => {
    try {
      const [secrets, google] = await Promise.all([
        openClawApi.listSecrets(),
        openClawApi.googleStatus()
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
        const response = await openClawApi.listBranches(workflowId);
        setBranches(response.branches);
        const nextActive =
          response.branches.find((branch) => branch.id === preferredBranchId) ??
          response.branches.find((branch) => branch.name.toLowerCase() === "main") ??
          response.branches[0] ??
          null;
        setActiveBranchId(nextActive?.id ?? null);
        setBranchRenameDraft(nextActive?.name ?? "");
        if (nextActive) {
          const branchResponse = await openClawApi.fetchBranch(workflowId, nextActive.id);
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

  useEffect(() => {
    const timeout = window.setTimeout(() => {
      void refreshIntegrations();
    }, 0);
    return () => window.clearTimeout(timeout);
  }, [adminToken, refreshIntegrations]);

  useEffect(() => {
    const timeout = window.setTimeout(() => {
      void refreshBranches(workflow.id);
    }, 0);
    return () => window.clearTimeout(timeout);
  }, [refreshBranches, workflow.id]);

  const loadWorkflow = useCallback(
    (
      nextWorkflow: WorkflowSpec,
      nextValidation: WorkflowValidationResult = validateWorkflowSpec(nextWorkflow)
    ) => {
      const issues = nextValidation.ok ? [] : nextValidation.errors;
      setWorkflow(nextWorkflow);
      setValidation(nextValidation);
      setNodes(workflowToNodes(nextWorkflow, issues));
      setEdges(workflowToEdges(nextWorkflow, issues));
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
      const response = await openClawApi.createJob(request);
      setActiveJob(response.job);
      void openClawApi.streamJobEvents(response.job.id, (event) => {
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
        const response = await openClawApi.feedback(
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
      setApiError(error instanceof Error ? error.message : "OpenClaw request failed.");
    } finally {
      setBusyAction(null);
    }
  }

  function updateAdminToken(value: string) {
    setAdminToken(value);
    saveOpenClawAdminToken(value);
  }

  function updateSecretDraft(secretName: string, value: string) {
    setSecretDrafts((previous) => ({
      ...previous,
      [secretName]: value
    }));
  }

  function saveSecret(secretName: string) {
    const value = secretDrafts[secretName]?.trim() ?? "";
    if (!value) {
      setApiError(`Secret '${secretName}' requires a value.`);
      return;
    }

    void executeApiAction(`secret-${secretName}`, async () => {
      await openClawApi.upsertSecret(secretName, value);
      setSecretDrafts((previous) => ({
        ...previous,
        [secretName]: ""
      }));
      await refreshIntegrations();
    });
  }

  function deleteSecret(secretName: string) {
    void executeApiAction(`delete-secret-${secretName}`, async () => {
      await openClawApi.deleteSecret(secretName);
      await refreshIntegrations();
    });
  }

  function switchBranch(branchId: string) {
    void executeApiAction("switch-branch", async () => {
      const response = await openClawApi.fetchBranch(workflow.id, branchId);
      setActiveBranchId(response.branch.id);
      setBranchRenameDraft(response.branch.name);
      setPromptTurns(response.promptTurns);
      setMergePreview(null);
      setMergeResolutionModes({});
      setMergeManualJson({});
      setReuseDecisions([]);
      loadWorkflow(response.headDraftRevision.workflow, response.headDraftRevision.validation);
      setApprovedRevision(null);
      setApprovalDiff(null);
      setDraftEvaluation(null);
      setRun(null);
      setDeploymentNotice(null);
      setPlanAcceptedNotice(null);
      setBranchNotice(`Switched to ${response.branch.name}`);
    });
  }

  function forkBranch() {
    const name = branchNameDraft.trim();
    if (!name) {
      setApiError("Branch name is required.");
      return;
    }
    if (branchLifecycleLocked) {
      setApiError("Archived branches are read-only.");
      return;
    }

    void executeApiAction("fork-branch", async () => {
      const response = await openClawApi.createBranch(workflow.id, {
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
      setMergeManualJson({});
      setReuseDecisions([]);
      loadWorkflow(response.draftRevision.workflow, response.draftRevision.validation);
      setBranchNotice(`Forked ${response.branch.name}`);
    });
  }

  function renameBranch() {
    if (!activeBranch) {
      setApiError("Select a branch before renaming it.");
      return;
    }
    const name = branchRenameDraft.trim();
    if (!name) {
      setApiError("Branch name is required.");
      return;
    }

    void executeApiAction("rename-branch", async () => {
      const response = await openClawApi.updateBranch(workflow.id, activeBranch.id, {
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
      const response = await openClawApi.updateBranch(workflow.id, activeBranch.id, {
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

  function previewMerge() {
    if (!activeBranchId || !mergeSourceBranchId) {
      setApiError("Choose an active branch and a source branch before previewing a merge.");
      return;
    }
    if (branchLifecycleLocked) {
      setApiError("Archived branches are read-only.");
      return;
    }
    if (mergeSources.find((branch) => branch.id === mergeSourceBranchId)?.status === "archived") {
      setApiError("Archived branches cannot be merged.");
      return;
    }

    void executeApiAction("merge-preview", async () => {
      const response = await openClawApi.previewBranchMerge(workflow.id, mergeSourceBranchId, {
        targetBranchId: activeBranchId,
        mode: mergeMode
      });
      setMergePreview(response.preview);
      setMergeResolutionModes({});
      setMergeManualJson({});
    });
  }

  function updateMergeResolutionMode(conflictId: string, mode: "source" | "target" | "manual") {
    setMergeResolutionModes((previous) => ({
      ...previous,
      [conflictId]: mode
    }));
  }

  function updateMergeManualJson(conflictId: string, value: string) {
    setMergeManualJson((previous) => ({
      ...previous,
      [conflictId]: value
    }));
  }

  function applyMerge() {
    if (!activeBranchId || !mergePreview) {
      return;
    }
    if (branchLifecycleLocked) {
      setApiError("Archived branches are read-only.");
      return;
    }

    void executeApiAction("branch-merge", async () => {
      const resolutions = mergePreview.conflicts.map((conflict) =>
        mergeResolutionForConflict(conflict, mergeResolutionModes, mergeManualJson)
      );
      const response = await openClawApi.mergeBranch(workflow.id, mergePreview.sourceBranchId, {
        targetBranchId: activeBranchId,
        mode: mergePreview.mode,
        appliedBy: "owner@example.com",
        resolutions
      });
      loadWorkflow(response.workflow, response.validation);
      setActiveBranchId(response.branch.id);
      setMergePreview(null);
      setMergeResolutionModes({});
      setMergeManualJson({});
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
      const response = await openClawApi.fetchReuseCandidates(workflow.id, activeBranchId);
      setReuseDecisions(response.decisions);
    });
  }

  function connectGoogle() {
    void executeApiAction("google-connect", async () => {
      const response = await openClawApi.googleConnect();
      globalThis.location.assign(response.url);
    });
  }

  function revokeGoogle() {
    void executeApiAction("google-revoke", async () => {
      await openClawApi.googleRevoke();
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

  function addNode(kind: WorkflowNodeKind) {
    const id = uniqueNodeId(kind, workflow.nodes);
    const position = nextNodePosition(nodes);
    const node = createWorkflowNode({
      id,
      kind,
      config: {
        canvas: position
      }
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

  function deleteSelection() {
    if (selectedNodeId) {
      updateLocalWorkflow({
        ...workflow,
        approval: null,
        nodes: workflow.nodes.filter((node) => node.id !== selectedNodeId),
        edges: workflow.edges.filter(
          (edge) => edge.source.nodeId !== selectedNodeId && edge.target.nodeId !== selectedNodeId
        )
      });
      setSelectedNodeId(null);
      return;
    }

    if (selectedEdgeId) {
      updateLocalWorkflow({
        ...workflow,
        approval: null,
        edges: workflow.edges.filter((edge) => edge.id !== selectedEdgeId)
      });
      setSelectedEdgeId(null);
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
  }

  function onEdgesDelete(deletedEdges: Edge[]) {
    const deletedIds = new Set(deletedEdges.map((edge) => edge.id));
    updateLocalWorkflow({
      ...workflow,
      approval: null,
      edges: workflow.edges.filter((edge) => !deletedIds.has(edge.id))
    });
  }

  function selectIssue(issue: WorkflowValidationIssue) {
    const [collection, index] = issue.path;
    if (collection === "nodes" && typeof index === "number") {
      setSelectedNodeId(workflow.nodes[index]?.id ?? null);
      setSelectedEdgeId(null);
    } else if (collection === "edges" && typeof index === "number") {
      setSelectedEdgeId(workflow.edges[index]?.id ?? null);
      setSelectedNodeId(null);
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

  function updatePrompt(value: string) {
    setPrompt(value);
    setClarification(null);
    setClarificationAnswers({});
  }

  function updateClarificationAnswer(questionId: string, value: string) {
    setClarificationAnswers((previous) => ({
      ...previous,
      [questionId]: value
    }));
  }

  function planDraft() {
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
        ? await openClawApi.planBranch(
            workflow.id,
            activeBranchId,
            {
              prompt,
              ...(currentWorkflow ? { currentWorkflow } : {}),
              preserveNodeIds: [...dirtyNodeIds],
              ...(clarification
                ? {
                    clarificationRequestId: clarification.id,
                    clarificationAnswers: clarification.questions.map((question) => ({
                      questionId: question.id,
                      answer: clarificationAnswers[question.id] ?? ""
                    }))
                  }
                : {}),
              actor: "owner@example.com"
            },
            job.id
          )
        : await openClawApi.plan(
            {
              prompt,
              ...(currentWorkflow ? { currentWorkflow } : {}),
              preserveNodeIds: [...dirtyNodeIds],
              ...(clarification
                ? {
                    clarificationRequestId: clarification.id,
                    clarificationAnswers: clarification.questions.map((question) => ({
                      questionId: question.id,
                      answer: clarificationAnswers[question.id] ?? ""
                    }))
                  }
                : {})
            },
            job.id
          );
      if (response.status === "clarification-required") {
        setClarification(response.clarification);
        setClarificationAnswers({});
        setTaskRoute(response.route);
        setPlannerFeedback(null);
        setReuseDecisions([]);
        return;
      }
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
      } else {
        await refreshBranches(response.workflow.id);
      }
      setDraftEvaluation(null);
      const nextSelectedNode =
        response.workflow.nodes.find((node) => node.kind !== "trigger") ??
        response.workflow.nodes[0] ??
        null;
      setSelectedNodeId(nextSelectedNode?.id ?? null);
      setSelectedEdgeId(null);
      setDirtyNodeIds(new Set());
      setApprovedRevision(null);
      setApprovalDiff(null);
      setRun(null);
      setPromotionNotice(null);
      setNodePrompt(nextSelectedNode?.description ?? "");
    });
  }

  function validateDraft() {
    if (branchLifecycleLocked) {
      setApiError("Archived branches are read-only.");
      return;
    }
    void executeApiAction("validate", async () => {
      const response = await openClawApi.validate(workflow.id, { workflow });
      setValidation(response.validation);
      if (response.workflow) {
        loadWorkflow(response.workflow, response.validation);
      }
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
      const response = await openClawApi.evaluateDraft(
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
    });
  }

  function updateSuggestionDecision(suggestionId: string, status: "accepted" | "rejected") {
    void executeApiAction("feedback-decision", async () => {
      if (!plannerFeedback) {
        return;
      }
      const response = await openClawApi.decideSuggestion(
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
        ? await openClawApi.repromptBranchNode(workflow.id, activeBranchId, {
            nodeId: selectedNode.id,
            prompt: nodePrompt,
            currentWorkflow: workflow,
            actor: "owner@example.com"
          })
        : await openClawApi.repromptNode(workflow.id, {
            nodeId: selectedNode.id,
            prompt: nodePrompt,
            currentWorkflow: workflow
          });
      loadWorkflow(response.workflow, response.validation);
      setApprovalDiff(response.diff);
      if ("branch" in response) {
        const branchResponse = response as Awaited<
          ReturnType<typeof openClawApi.repromptBranchNode>
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
      const response = await openClawApi.reviewCodegen(workflow.id, selectedNode.id, {
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
      const response = await openClawApi.promoteCodegen(workflow.id, selectedNode.id);
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
      const response = await openClawApi.buildCodegen(
        workflow.id,
        selectedNode.id,
        {
          maxIterations: 3,
          maxReimplementationAttempts: 2,
          maxWallClockSeconds: 600,
          maxModelCostUsd: 2,
          runTestsInDocker: false,
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
    });
  }

  function approveWorkflow() {
    if (branchLifecycleLocked) {
      setApiError("Archived branches are read-only.");
      return;
    }
    void executeApiAction("approve", async () => {
      const response = await openClawApi.approve(workflow.id, {
        workflow,
        approvedBy: "owner@example.com",
        ...(activeBranchId ? { branchId: activeBranchId } : {})
      });
      setApprovedRevision(response.approvedRevision);
      setApprovalDiff(response.diff);
      loadWorkflow(response.workflow, validateWorkflowSpec(response.workflow));
    });
  }

  function acceptPlanShape() {
    if (branchLifecycleLocked) {
      setApiError("Archived branches are read-only.");
      return;
    }
    void executeApiAction("accept-plan", async () => {
      const response = activeBranchId
        ? await openClawApi.acceptBranchPlan(workflow.id, activeBranchId, {
            workflow,
            acceptedBy: "owner@example.com"
          })
        : await openClawApi.acceptPlan(workflow.id, {
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
    });
  }

  function runWorkflow() {
    if (!approvedRevision) {
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
      const response = await openClawApi.startRun(
        workflow.id,
        {
          approvedRevisionId: approvedRevision.id,
          ...(activeBranchId ? { branchId: activeBranchId } : {})
        },
        job.id
      );
      const fetched = await openClawApi.fetchRun(workflow.id, response.run.id);
      setRun(fetched.run);
    });
  }

  function deployWorkflow() {
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
      const response = await openClawApi.deployWorkflow(
        workflow.id,
        {
          approvedRevisionId: approvedRevision.id,
          kind: "workflow.bundle",
          createdBy: "owner@example.com",
          rollbackPlan: `Rollback to ${approvedRevision.id}.`,
          ...(activeBranchId ? { branchId: activeBranchId } : {}),
          metadata: {
            source: "openclaw"
          }
        },
        job.id
      );
      const active = await openClawApi.fetchActiveDeployments(workflow.id);
      setDeploymentNotice(`Deployment ${response.deployment.status}: ${response.deployment.kind}`);
      setDeploymentActivations(active);
    });
  }

  function cancelActiveJob() {
    if (!activeJob || ["succeeded", "failed", "cancelled"].includes(activeJob.status)) {
      return;
    }

    void executeApiAction("cancel-job", async () => {
      const response = await openClawApi.cancelJob(activeJob.id, "Stopped from OpenClaw.");
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
    setBranches([]);
    setActiveBranchId(null);
    setPromptTurns([]);
    setBranchNotice(null);
    setMergeSourceBranchId("");
    setMergeMode("merge");
    setMergePreview(null);
    setMergeResolutionModes({});
    setMergeManualJson({});
    setReuseDecisions([]);
    setPromotionNotice(null);
    loadWorkflow(emptyWorkflowDraft, validateWorkflowSpec(emptyWorkflowDraft));
  }

  return (
    <main className="app-shell">
      <section className="workspace">
        <aside className="nav-rail" aria-label="Workspace navigation">
          {railItems.map((item, index) => {
            const Icon = item.icon;
            return (
              <button
                key={item.label}
                className={index === 1 ? "rail-button rail-button-active" : "rail-button"}
                type="button"
                title={item.label}
              >
                <Icon size={19} />
              </button>
            );
          })}
        </aside>

        <aside className="panel planner-panel" aria-label="Workflow planner">
          <div className="sidebar-search">
            <Search size={18} />
            <input
              aria-label="Search components"
              placeholder="Search"
              value={componentSearch}
              onChange={(event) => setComponentSearch(event.target.value)}
            />
            <kbd>/</kbd>
          </div>

          <section className="component-browser" aria-label="Component categories">
            <div className="component-heading">
              <h2>Components</h2>
              <SlidersHorizontal size={16} />
            </div>
            <div className="component-list">
              {componentCategories.map((category) => {
                const Icon = category.icon;
                const selected = selectedComponentCategory === category.id;
                return (
                  <button
                    key={category.label}
                    aria-pressed={selected}
                    className={selected ? "component-row component-row-active" : "component-row"}
                    type="button"
                    onClick={() => setSelectedComponentCategory(category.id)}
                  >
                    <Icon size={18} />
                    <span>{category.label}</span>
                    <ChevronRight size={16} />
                  </button>
                );
              })}
            </div>
            <div className="component-palette" aria-label="Available components">
              <div className="component-palette-heading">
                <span>{categoryLabel(selectedComponentCategory)}</span>
                <span>{visibleComponentItems.length}</span>
              </div>
              {visibleComponentItems.length > 0 ? (
                visibleComponentItems.map((item) => (
                  <button
                    key={item.id}
                    className="component-option"
                    type="button"
                    aria-label={`Add ${item.label}`}
                    disabled={branchLifecycleLocked}
                    onClick={() => addComponentNode(item)}
                  >
                    <span>
                      <strong>{item.label}</strong>
                      <small>{item.description}</small>
                    </span>
                    <Plus size={16} />
                  </button>
                ))
              ) : (
                <p className="muted-text">No matching components</p>
              )}
            </div>
            <button
              className="discover-button"
              type="button"
              aria-pressed={selectedComponentCategory === "all"}
              onClick={() => setSelectedComponentCategory("all")}
            >
              <Grid2X2 size={18} />
              Discover more components
            </button>
          </section>

          <form
            className="prompt-form"
            onSubmit={(event) => {
              event.preventDefault();
              planDraft();
            }}
          >
            <label htmlFor="workflow-prompt">Workflow Prompt</label>
            <textarea
              id="workflow-prompt"
              value={prompt}
              onChange={(event) => updatePrompt(event.target.value)}
              rows={4}
            />
            <ClarificationPanel
              clarification={clarification}
              answers={clarificationAnswers}
              onAnswerChange={updateClarificationAnswer}
            />
            <button
              type="submit"
              disabled={
                busyAction !== null ||
                prompt.trim().length === 0 ||
                branchLifecycleLocked ||
                !clarificationReady
              }
            >
              <WandSparkles size={18} />
              {clarification ? "Plan With Answers" : "Plan"}
            </button>
          </form>

          <section aria-label="Workflow summary">
            <div className="panel-heading">
              <GitBranch size={18} />
              <h2>{workflow.name}</h2>
            </div>
            <dl className="metric-grid">
              <div>
                <dt>Nodes</dt>
                <dd>{workflow.nodes.length}</dd>
              </div>
              <div>
                <dt>Edges</dt>
                <dd>{workflow.edges.length}</dd>
              </div>
              <div>
                <dt>Revision</dt>
                <dd>{workflow.revision}</dd>
              </div>
            </dl>
          </section>

          <BranchPanel
            branches={visibleBranches}
            activeBranch={activeBranch}
            activeBranchId={activeBranchId}
            promptTurns={promptTurns}
            branchNameDraft={branchNameDraft}
            branchRenameDraft={branchRenameDraft}
            showArchivedBranches={showArchivedBranches}
            branchNotice={branchNotice}
            busyAction={busyAction}
            onBranchNameChange={setBranchNameDraft}
            onBranchRenameChange={setBranchRenameDraft}
            onShowArchivedChange={setShowArchivedBranches}
            onFork={forkBranch}
            onSwitch={switchBranch}
            onRename={renameBranch}
            onArchiveToggle={toggleBranchArchive}
          />
          <BranchMergeReusePanel
            activeBranch={activeBranch}
            mergeSources={mergeSources}
            mergeSourceBranchId={mergeSourceBranchId}
            mergeMode={mergeMode}
            mergePreview={mergePreview}
            mergeResolutionModes={mergeResolutionModes}
            mergeManualJson={mergeManualJson}
            reuseDecisions={reuseDecisions}
            busyAction={busyAction}
            branchLifecycleLocked={branchLifecycleLocked}
            onMergeSourceChange={setMergeSourceBranchId}
            onMergeModeChange={setMergeMode}
            onPreviewMerge={previewMerge}
            onApplyMerge={applyMerge}
            onResolutionModeChange={updateMergeResolutionMode}
            onManualResolutionChange={updateMergeManualJson}
            onRefreshReuse={refreshReuseCandidates}
          />

          <RoutePanel route={taskRoute} />
          <DraftEvaluationPanel evaluation={draftEvaluation} />
          <FeedbackPanel feedback={plannerFeedback} onDecision={updateSuggestionDecision} />

          <section aria-label="Validation panel" className="validation-panel">
            <div className="panel-heading">
              <ListChecks size={18} />
              <h2>Validation</h2>
            </div>
            <StatusRow
              label="Graph"
              value={validation.ok ? "valid" : "blocked"}
              tone={validation.ok ? "valid" : "blocked"}
            />
            {validationIssues.length > 0 ? (
              <div className="issue-list">
                {validationIssues.map((issue) => (
                  <button
                    key={`${issue.code}-${issue.path.join(".")}`}
                    className="issue-button"
                    onClick={() => selectIssue(issue)}
                    type="button"
                  >
                    <strong>{issue.code}</strong>
                    <span>{issue.message}</span>
                  </button>
                ))}
              </div>
            ) : null}
          </section>

          <IntegrationPanel
            adminToken={adminToken}
            integrations={integrationReadiness}
            secrets={secretMetadata}
            googleConnected={googleConnected}
            secretDrafts={secretDrafts}
            busyAction={busyAction}
            onAdminTokenChange={updateAdminToken}
            onRefresh={refreshIntegrations}
            onSecretDraftChange={updateSecretDraft}
            onSaveSecret={saveSecret}
            onDeleteSecret={deleteSecret}
            onConnectGoogle={connectGoogle}
            onRevokeGoogle={revokeGoogle}
          />

          {apiError ? <p className="error-text">{apiError}</p> : null}
        </aside>

        <section className="canvas-panel" aria-label="Workflow graph">
          <header className="topbar">
            <div>
              <p className="eyebrow">KelpClaw</p>
              <h1>OpenClaw</h1>
            </div>
            <div className="topbar-actions" aria-label="Workflow actions">
              <button
                title="Validate workflow"
                onClick={validateDraft}
                disabled={busyAction !== null || branchLifecycleLocked}
              >
                <ShieldCheck size={18} />
                Validate
              </button>
              <button
                title="Accept plan shape"
                onClick={acceptPlanShape}
                disabled={
                  !workflowHasGraph ||
                  !validation.ok ||
                  busyAction !== null ||
                  branchLifecycleLocked
                }
              >
                <CheckCircle2 size={18} />
                Accept Plan
              </button>
              <button
                title="Evaluate draft"
                onClick={evaluateDraft}
                disabled={
                  !workflowHasGraph ||
                  !validation.ok ||
                  busyAction !== null ||
                  branchLifecycleLocked
                }
              >
                <ListChecks size={18} />
                Evaluate
              </button>
              <button
                title="Approve workflow"
                onClick={approveWorkflow}
                disabled={!canApprove || busyAction !== null}
              >
                <CheckCircle2 size={18} />
                Approve
              </button>
              <button
                title="Run workflow"
                onClick={runWorkflow}
                disabled={!canRun || busyAction !== null}
              >
                <Play size={18} />
                Run
              </button>
              <button
                title="Deploy workflow"
                onClick={deployWorkflow}
                disabled={
                  !approvedRevision ||
                  !draftEvaluation?.readyForApproval ||
                  busyAction !== null ||
                  branchLifecycleLocked
                }
              >
                <Send size={18} />
                Deploy
              </button>
              <button
                title="Stop active job"
                onClick={cancelActiveJob}
                disabled={
                  !activeJob ||
                  ["succeeded", "failed", "cancelled"].includes(activeJob.status) ||
                  busyAction === "cancel-job"
                }
              >
                <XCircle size={18} />
                Stop
              </button>
              <button className="icon-button" title="Reset workflow" onClick={resetWorkflow}>
                <RefreshCw size={18} />
              </button>
            </div>
          </header>
          <div className="canvas-toolbar" aria-label="Canvas controls">
            <div className="node-kind-actions">
              {nodeKinds.map((kind) => (
                <button
                  key={kind}
                  type="button"
                  onClick={() => addNode(kind)}
                  title={`Add ${kind} node`}
                >
                  <Plus size={16} />
                  {nodeKindLabel(kind)}
                </button>
              ))}
            </div>
            <button
              className="icon-button"
              type="button"
              onClick={deleteSelection}
              title="Delete selected"
              disabled={!selectedNodeId && !selectedEdgeId}
            >
              <Trash2 size={18} />
            </button>
          </div>
          <ReactFlow
            nodes={nodes}
            edges={edges}
            nodeTypes={workflowNodeTypes}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onNodeDragStop={onNodeDragStop}
            onConnect={onConnect}
            onNodesDelete={onNodesDelete}
            onEdgesDelete={onEdgesDelete}
            onSelectionChange={({ nodes: selectedNodes, edges: selectedEdges }) => {
              if (selectedNodes[0]) {
                setSelectedNodeId(selectedNodes[0].id);
                setSelectedEdgeId(null);
                setNodePrompt(
                  workflow.nodes.find((node) => node.id === selectedNodes[0]?.id)?.description ?? ""
                );
                setJsonError(null);
              } else if (selectedEdges[0]) {
                setSelectedNodeId(null);
                setSelectedEdgeId(selectedEdges[0].id);
                setNodePrompt("");
                setJsonError(null);
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
          <div className="canvas-footer" aria-label="Canvas status">
            <span className="canvas-wave">~</span>
            <span>63%</span>
            <Clock3 size={18} />
            <span>{workflow.nodes.length} nodes</span>
          </div>
        </section>

        <aside className="panel inspector-panel" aria-label="Workflow inspector">
          <Inspector
            workflow={workflow}
            selectedNode={selectedNode}
            selectedEdgeId={selectedEdge?.id ?? null}
            nodePrompt={nodePrompt}
            jsonError={jsonError}
            approvalDiff={approvalDiff}
            approvedRevision={approvedRevision}
            run={run}
            activeJob={activeJob}
            workspace={workspace}
            agentRuns={agentRuns}
            deploymentNotice={deploymentNotice}
            planAcceptedNotice={planAcceptedNotice}
            deploymentActivations={deploymentActivations}
            busyAction={busyAction}
            branchLifecycleLocked={branchLifecycleLocked}
            onNodePromptChange={setNodePrompt}
            onReprompt={repromptNode}
            onBuildCodegen={buildCodegenNode}
            onReviewCodegen={reviewCodegenNode}
            onPromoteCodegen={promoteCodegenNode}
            onUpdateNode={updateNode}
            onUpdateJsonField={updateJsonField}
            promotionNotice={promotionNotice}
          />
        </aside>
      </section>
    </main>
  );
}

function Inspector(props: {
  readonly workflow: WorkflowSpec;
  readonly selectedNode: WorkflowNode | null;
  readonly selectedEdgeId: string | null;
  readonly nodePrompt: string;
  readonly jsonError: string | null;
  readonly approvalDiff: WorkflowSpecDiff | null;
  readonly approvedRevision: WorkflowApprovedRevision | null;
  readonly run: WorkflowRunRecord | null;
  readonly activeJob: WorkflowJob | null;
  readonly workspace: WorkflowWorkspace | null;
  readonly agentRuns: readonly unknown[];
  readonly deploymentNotice: string | null;
  readonly planAcceptedNotice: string | null;
  readonly deploymentActivations: DeploymentActivationSummaryResponse | null;
  readonly busyAction: string | null;
  readonly branchLifecycleLocked: boolean;
  readonly promotionNotice: string | null;
  readonly onNodePromptChange: (value: string) => void;
  readonly onReprompt: () => void;
  readonly onBuildCodegen: () => void;
  readonly onReviewCodegen: () => void;
  readonly onPromoteCodegen: () => void;
  readonly onUpdateNode: (nodeId: string, updater: (node: WorkflowNode) => WorkflowNode) => void;
  readonly onUpdateJsonField: (
    nodeId: string,
    field: "config" | "inputs" | "outputs",
    value: string
  ) => void;
}) {
  const node = props.selectedNode;

  return (
    <>
      <h2>Inspector</h2>
      {node ? (
        <div className="inspector-stack">
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
          <label>
            Config
            <textarea
              key={`config-${node.id}-${props.workflow.revision}`}
              defaultValue={formatJson(node.config)}
              rows={6}
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
                rows={4}
                onBlur={(event) => props.onUpdateJsonField(node.id, "inputs", event.target.value)}
              />
            </label>
            <label>
              Outputs
              <textarea
                key={`outputs-${node.id}-${props.workflow.revision}`}
                defaultValue={formatJson(node.outputs)}
                rows={4}
                onBlur={(event) => props.onUpdateJsonField(node.id, "outputs", event.target.value)}
              />
            </label>
          </div>
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
                        toggleSecondaryDeliveryChannel(current, "whatsapp", event.target.checked)
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
                        toggleSecondaryDeliveryChannel(current, "telegram", event.target.checked)
                      )
                    }
                  />
                  Telegram
                </label>
              </div>
              <label>
                Adapter
                <input
                  value={(node.adapterIds ?? (node.adapterId ? [node.adapterId] : [])).join(", ")}
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
                <StatusRow label="Reuse" value={node.config.reusedFromBranchId} tone="pending" />
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
              {props.promotionNotice ? (
                <p className="success-text">{props.promotionNotice}</p>
              ) : null}
            </section>
          ) : null}
          {props.jsonError ? <p className="error-text">{props.jsonError}</p> : null}
        </div>
      ) : (
        <dl className="detail-list">
          <div>
            <dt>Workflow ID</dt>
            <dd>{props.workflow.id}</dd>
          </div>
          <div>
            <dt>Selected Edge</dt>
            <dd>{props.selectedEdgeId ?? "none"}</dd>
          </div>
          <div>
            <dt>Frozen Approval</dt>
            <dd>{props.workflow.approval?.status ?? "draft"}</dd>
          </div>
        </dl>
      )}

      <ApprovalPanel diff={props.approvalDiff} approvedRevision={props.approvedRevision} />
      {props.planAcceptedNotice ? <p className="success-text">{props.planAcceptedNotice}</p> : null}
      <JobPanel job={props.activeJob} />
      <WorkspacePanel workspace={props.workspace} agentRuns={props.agentRuns} />
      {props.deploymentNotice ? <p className="success-text">{props.deploymentNotice}</p> : null}
      <DeploymentPanel activations={props.deploymentActivations} />
      <RunPanel run={props.run} />
    </>
  );
}

function ClarificationPanel(props: {
  readonly clarification: WorkflowClarificationRequest | null;
  readonly answers: Readonly<Record<string, string>>;
  readonly onAnswerChange: (questionId: string, value: string) => void;
}) {
  if (!props.clarification) {
    return null;
  }

  return (
    <section aria-label="Clarification questions" className="clarification-panel">
      <div className="panel-heading">
        <MessageCircle size={18} />
        <h2>Clarify First</h2>
      </div>
      <p className="muted-text">{props.clarification.reason}</p>
      {props.clarification.questions.map((question) => (
        <label key={question.id}>
          {question.question}
          <textarea
            value={props.answers[question.id] ?? ""}
            placeholder={question.placeholder}
            rows={2}
            required={question.required}
            onChange={(event) => props.onAnswerChange(question.id, event.target.value)}
          />
        </label>
      ))}
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

function BranchPanel(props: {
  readonly branches: readonly WorkflowBranch[];
  readonly activeBranch: WorkflowBranch | null;
  readonly activeBranchId: string | null;
  readonly promptTurns: readonly WorkflowPromptTurn[];
  readonly branchNameDraft: string;
  readonly branchRenameDraft: string;
  readonly showArchivedBranches: boolean;
  readonly branchNotice: string | null;
  readonly busyAction: string | null;
  readonly onBranchNameChange: (value: string) => void;
  readonly onBranchRenameChange: (value: string) => void;
  readonly onShowArchivedChange: (value: boolean) => void;
  readonly onFork: () => void;
  readonly onSwitch: (branchId: string) => void;
  readonly onRename: () => void;
  readonly onArchiveToggle: () => void;
}) {
  const activeBranchIsDefault = props.activeBranch?.id.endsWith(".main") === true;

  return (
    <section aria-label="Branch tree" className="validation-panel">
      <div className="panel-heading">
        <GitBranch size={18} />
        <h2>Branches</h2>
      </div>
      <StatusRow
        label="Active"
        value={props.activeBranch?.name ?? "none"}
        tone={props.activeBranchId ? "valid" : "idle"}
      />
      <label className="inline-control">
        <input
          type="checkbox"
          checked={props.showArchivedBranches}
          onChange={(event) => props.onShowArchivedChange(event.target.checked)}
        />
        Show archived
      </label>
      <div className="branch-list">
        {props.branches.map((branch) => (
          <button
            key={branch.id}
            className={
              branch.id === props.activeBranchId ? "branch-row branch-row-active" : "branch-row"
            }
            type="button"
            onClick={() => props.onSwitch(branch.id)}
            disabled={props.busyAction !== null || branch.id === props.activeBranchId}
          >
            <span>{branch.name}</span>
            <strong>{branch.status}</strong>
          </button>
        ))}
      </div>
      <label>
        Rename active branch
        <input
          value={props.branchRenameDraft}
          onChange={(event) => props.onBranchRenameChange(event.target.value)}
          disabled={!props.activeBranch}
        />
      </label>
      <div className="integration-actions">
        <button
          type="button"
          onClick={props.onRename}
          disabled={
            props.busyAction !== null ||
            !props.activeBranch ||
            props.branchRenameDraft.trim().length === 0
          }
        >
          <CheckCircle2 size={16} />
          Rename
        </button>
        <button
          type="button"
          onClick={props.onArchiveToggle}
          disabled={props.busyAction !== null || !props.activeBranch || activeBranchIsDefault}
        >
          <Trash2 size={16} />
          {props.activeBranch?.status === "archived" ? "Restore" : "Archive"}
        </button>
      </div>
      <label>
        Fork name
        <input
          value={props.branchNameDraft}
          onChange={(event) => props.onBranchNameChange(event.target.value)}
        />
      </label>
      <button
        type="button"
        onClick={props.onFork}
        disabled={
          props.busyAction !== null ||
          !props.activeBranch ||
          props.branchNameDraft.trim().length === 0 ||
          props.activeBranch?.status === "archived"
        }
      >
        <GitBranch size={16} />
        Fork Branch
      </button>
      {props.branchNotice ? <p className="success-text">{props.branchNotice}</p> : null}
      {props.promptTurns.length > 0 ? (
        <ul className="event-list">
          {props.promptTurns.slice(-4).map((turn) => (
            <li key={turn.id}>
              <strong>{turn.source}</strong>
              <span>{turn.prompt}</span>
            </li>
          ))}
        </ul>
      ) : null}
    </section>
  );
}

function BranchMergeReusePanel(props: {
  readonly activeBranch: WorkflowBranch | null;
  readonly mergeSources: readonly WorkflowBranch[];
  readonly mergeSourceBranchId: string;
  readonly mergeMode: "merge" | "cherry-pick";
  readonly mergePreview: WorkflowBranchMergePreview | null;
  readonly mergeResolutionModes: Readonly<Record<string, "source" | "target" | "manual">>;
  readonly mergeManualJson: Readonly<Record<string, string>>;
  readonly reuseDecisions: readonly WorkflowGeneratedModuleReuseDecision[];
  readonly busyAction: string | null;
  readonly branchLifecycleLocked: boolean;
  readonly onMergeSourceChange: (branchId: string) => void;
  readonly onMergeModeChange: (mode: "merge" | "cherry-pick") => void;
  readonly onPreviewMerge: () => void;
  readonly onApplyMerge: () => void;
  readonly onResolutionModeChange: (
    conflictId: string,
    mode: "source" | "target" | "manual"
  ) => void;
  readonly onManualResolutionChange: (conflictId: string, value: string) => void;
  readonly onRefreshReuse: () => void;
}) {
  const conflictsResolved =
    props.mergePreview?.conflicts.every((conflict) => props.mergeResolutionModes[conflict.id]) ??
    true;
  const selectedSourceArchived =
    props.mergeSources.find((branch) => branch.id === props.mergeSourceBranchId)?.status ===
    "archived";

  return (
    <section aria-label="Branch merge and reuse" className="validation-panel">
      <div className="panel-heading">
        <GitBranch size={18} />
        <h2>Merge & Reuse</h2>
      </div>
      <label>
        Source branch
        <select
          value={props.mergeSourceBranchId}
          onChange={(event) => props.onMergeSourceChange(event.target.value)}
          disabled={!props.activeBranch || props.branchLifecycleLocked}
        >
          <option value="">Choose branch</option>
          {props.mergeSources.map((branch) => (
            <option key={branch.id} value={branch.id} disabled={branch.status === "archived"}>
              {branch.name}
              {branch.status === "archived" ? " (archived)" : ""}
            </option>
          ))}
        </select>
      </label>
      <label>
        Mode
        <select
          value={props.mergeMode}
          onChange={(event) =>
            props.onMergeModeChange(event.target.value as "merge" | "cherry-pick")
          }
          disabled={!props.activeBranch || props.branchLifecycleLocked}
        >
          <option value="merge">Merge</option>
          <option value="cherry-pick">Cherry-pick</option>
        </select>
      </label>
      <div className="integration-actions">
        <button
          type="button"
          onClick={props.onPreviewMerge}
          disabled={
            props.busyAction !== null ||
            !props.activeBranch ||
            props.branchLifecycleLocked ||
            props.mergeSourceBranchId.length === 0 ||
            selectedSourceArchived
          }
        >
          Preview
        </button>
        <button
          type="button"
          onClick={props.onApplyMerge}
          disabled={
            props.busyAction !== null ||
            !props.mergePreview ||
            props.mergePreview.status === "blocked" ||
            props.branchLifecycleLocked ||
            selectedSourceArchived ||
            !conflictsResolved
          }
        >
          Apply
        </button>
        <button
          type="button"
          onClick={props.onRefreshReuse}
          disabled={props.busyAction !== null || !props.activeBranch || props.branchLifecycleLocked}
        >
          Reuse Candidates
        </button>
      </div>
      {props.mergePreview ? (
        <div className="merge-preview">
          <StatusRow
            label="Preview"
            value={props.mergePreview.status}
            tone={props.mergePreview.status}
          />
          <ul className="diff-summary">
            {props.mergePreview.summary.map((item) => (
              <li key={item}>{item}</li>
            ))}
          </ul>
          {props.mergePreview.conflicts.map((conflict) => (
            <MergeConflictItem
              key={conflict.id}
              conflict={conflict}
              mode={props.mergeResolutionModes[conflict.id] ?? ""}
              manualJson={props.mergeManualJson[conflict.id] ?? ""}
              onModeChange={props.onResolutionModeChange}
              onManualChange={props.onManualResolutionChange}
            />
          ))}
        </div>
      ) : null}
      {props.reuseDecisions.length > 0 ? (
        <ul className="event-list">
          {props.reuseDecisions.map((decision) => (
            <li key={decision.id}>
              <strong>{decision.status}</strong>
              <span>
                {decision.nodeId}
                {decision.sourceBranchId ? ` from ${decision.sourceBranchId}` : ""}
                {decision.gates.length ? ` (${decision.gates.join(", ")})` : ""}
              </span>
            </li>
          ))}
        </ul>
      ) : null}
    </section>
  );
}

function MergeConflictItem(props: {
  readonly conflict: WorkflowBranchMergeConflict;
  readonly mode: "source" | "target" | "manual" | "";
  readonly manualJson: string;
  readonly onModeChange: (conflictId: string, mode: "source" | "target" | "manual") => void;
  readonly onManualChange: (conflictId: string, value: string) => void;
}) {
  return (
    <div className="issue-button">
      <strong>{props.conflict.kind}</strong>
      <span>{props.conflict.message}</span>
      <div className="integration-actions">
        <button type="button" onClick={() => props.onModeChange(props.conflict.id, "source")}>
          Use Source
        </button>
        <button type="button" onClick={() => props.onModeChange(props.conflict.id, "target")}>
          Keep Target
        </button>
        <button type="button" onClick={() => props.onModeChange(props.conflict.id, "manual")}>
          Manual
        </button>
      </div>
      <StatusRow
        label="Resolution"
        value={props.mode || "unresolved"}
        tone={props.mode ? "valid" : "blocked"}
      />
      {props.mode === "manual" ? (
        <textarea
          aria-label={`Manual resolution for ${props.conflict.id}`}
          value={props.manualJson}
          rows={4}
          onChange={(event) => props.onManualChange(props.conflict.id, event.target.value)}
        />
      ) : null}
    </div>
  );
}

function RoutePanel(props: { readonly route: WorkflowTaskRoute | null }) {
  return (
    <section aria-label="Task route" className="validation-panel">
      <div className="panel-heading">
        <GitBranch size={18} />
        <h2>Route</h2>
      </div>
      <StatusRow
        label="Mode"
        value={props.route?.route ?? "unrouted"}
        tone={props.route ? "valid" : "pending"}
      />
      <StatusRow
        label="Model"
        value={props.route?.requiredModel.mode ?? "none"}
        tone={props.route?.requiredModel.mode === "live" ? "pending" : "valid"}
      />
      <StatusRow
        label="Production"
        value={props.route?.productionDeterministic === false ? "agentic" : "deterministic"}
        tone={props.route?.productionDeterministic === false ? "pending" : "valid"}
      />
      {props.route ? <p className="muted-text">{props.route.rationale}</p> : null}
    </section>
  );
}

function DraftEvaluationPanel(props: { readonly evaluation: WorkflowDraftEvaluation | null }) {
  return (
    <section aria-label="Draft evaluation" className="validation-panel">
      <div className="panel-heading">
        <ListChecks size={18} />
        <h2>Draft Eval</h2>
      </div>
      <StatusRow
        label="Status"
        value={props.evaluation?.status ?? "not run"}
        tone={props.evaluation?.readyForApproval ? "valid" : "pending"}
      />
      <StatusRow
        label="Approval"
        value={props.evaluation?.readyForApproval ? "ready" : "blocked"}
        tone={props.evaluation?.readyForApproval ? "approved" : "blocked"}
      />
      {props.evaluation?.findings.length ? (
        <div className="issue-list">
          {props.evaluation.findings.map((finding) => (
            <div className="issue-button" key={finding.id}>
              <strong>{finding.severity}</strong>
              <span>{finding.message}</span>
            </div>
          ))}
        </div>
      ) : null}
    </section>
  );
}

function FeedbackPanel(props: {
  readonly feedback: WorkflowPlannerFeedback | null;
  readonly onDecision: (suggestionId: string, status: "accepted" | "rejected") => void;
}) {
  if (!props.feedback) {
    return null;
  }

  return (
    <section aria-label="Planner feedback" className="validation-panel">
      <div className="panel-heading">
        <WandSparkles size={18} />
        <h2>Suggestions</h2>
      </div>
      <StatusRow label="Status" value={props.feedback.status} tone={props.feedback.status} />
      <div className="issue-list">
        {props.feedback.suggestions.map((suggestion) => (
          <SuggestionItem
            key={suggestion.id}
            suggestion={suggestion}
            onDecision={props.onDecision}
          />
        ))}
      </div>
    </section>
  );
}

function SuggestionItem(props: {
  readonly suggestion: WorkflowPlannerSuggestion;
  readonly onDecision: (suggestionId: string, status: "accepted" | "rejected") => void;
}) {
  return (
    <div className="issue-button">
      <strong>{props.suggestion.title}</strong>
      <span>{props.suggestion.message}</span>
      <div className="integration-actions">
        <button
          type="button"
          onClick={() => props.onDecision(props.suggestion.id, "accepted")}
          disabled={props.suggestion.status !== "suggested"}
        >
          Accept
        </button>
        <button
          type="button"
          onClick={() => props.onDecision(props.suggestion.id, "rejected")}
          disabled={props.suggestion.status !== "suggested"}
        >
          Reject
        </button>
      </div>
    </div>
  );
}

function IntegrationPanel(props: {
  readonly adminToken: string;
  readonly integrations: readonly IntegrationReadiness[];
  readonly secrets: readonly SecretMetadata[];
  readonly googleConnected: boolean | null;
  readonly secretDrafts: Readonly<Record<string, string>>;
  readonly busyAction: string | null;
  readonly onAdminTokenChange: (value: string) => void;
  readonly onRefresh: () => Promise<void>;
  readonly onSecretDraftChange: (secretName: string, value: string) => void;
  readonly onSaveSecret: (secretName: string) => void;
  readonly onDeleteSecret: (secretName: string) => void;
  readonly onConnectGoogle: () => void;
  readonly onRevokeGoogle: () => void;
}) {
  const secretMap = new Map(props.secrets.map((secret) => [secret.name, secret]));

  return (
    <section className="integration-panel" aria-label="Integration setup">
      <div className="panel-heading">
        <KeyRound size={18} />
        <h2>Integrations</h2>
        <button
          className="icon-button"
          type="button"
          title="Refresh integrations"
          onClick={() => {
            void props.onRefresh();
          }}
        >
          <RefreshCw size={16} />
        </button>
      </div>
      <label>
        Admin token
        <input
          type="password"
          value={props.adminToken}
          onChange={(event) => props.onAdminTokenChange(event.target.value)}
          autoComplete="off"
        />
      </label>
      <div className="integration-list">
        {integrationSetups.map((setup) => {
          const Icon = setup.icon;
          const status = integrationStatus(setup.id, props.integrations, props.googleConnected);
          const secret = secretMap.get(setup.secretName);
          const draft = props.secretDrafts[setup.secretName] ?? "";
          return (
            <section className="integration-row" key={setup.id}>
              <div className="integration-row-header">
                <span>
                  <Icon size={16} />
                  {setup.label}
                </span>
                <strong className={`status-pill status-${status.tone}`}>{status.label}</strong>
              </div>
              <div className="secret-meta">
                <span>{setup.secretName}</span>
                <span>{secret ? "stored" : "missing"}</span>
              </div>
              <textarea
                aria-label={`${setup.label} secret`}
                value={draft}
                placeholder={setup.placeholder}
                rows={2}
                onChange={(event) =>
                  props.onSecretDraftChange(setup.secretName, event.target.value)
                }
              />
              <div className="integration-actions">
                {setup.id === "google" ? (
                  <>
                    <button
                      type="button"
                      onClick={props.onConnectGoogle}
                      disabled={props.busyAction !== null}
                    >
                      <Table2 size={16} />
                      Connect
                    </button>
                    <button
                      type="button"
                      onClick={props.onRevokeGoogle}
                      disabled={props.busyAction !== null || !secret}
                    >
                      Revoke
                    </button>
                  </>
                ) : null}
                <button
                  type="button"
                  onClick={() => props.onSaveSecret(setup.secretName)}
                  disabled={props.busyAction !== null || draft.trim().length === 0}
                >
                  <CheckCircle2 size={16} />
                  Save
                </button>
                <button
                  type="button"
                  onClick={() => props.onDeleteSecret(setup.secretName)}
                  disabled={props.busyAction !== null || !secret}
                >
                  <Trash2 size={16} />
                  Delete
                </button>
              </div>
            </section>
          );
        })}
      </div>
    </section>
  );
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

function DeploymentPanel(props: {
  readonly activations: DeploymentActivationSummaryResponse | null;
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

function RunPanel(props: { readonly run: WorkflowRunRecord | null }) {
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

  return componentCategories.find((candidate) => candidate.id === category)?.label ?? "Components";
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

function uniqueNodeId(kind: WorkflowNodeKind, nodes: readonly WorkflowNode[]): string {
  const prefix = `${kind}-node`;
  const existing = new Set(nodes.map((node) => node.id));
  let index = nodes.length + 1;
  let id = `${prefix}-${index}`;
  while (existing.has(id)) {
    index += 1;
    id = `${prefix}-${index}`;
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
