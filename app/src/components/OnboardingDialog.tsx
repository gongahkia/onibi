import { useEffect, useMemo, useState } from "react";
import {
  AGENT_KINDS,
  agentDisplayLabel,
  resolveAgentBinary,
  type AgentKind,
  useSessionStore,
} from "../lib/sessions";
import { ensureApprovalConnectionConfig } from "../lib/approval-client";

export interface OnboardingDialogProps {
  open: boolean;
  onClose: () => void;
}

type CanaryState = "idle" | "waiting" | "allowed" | "denied" | "error";

export function OnboardingDialog({ open, onClose }: OnboardingDialogProps) {
  const settings = useSessionStore((state) => state.settings);
  const updateSettings = useSessionStore((state) => state.updateSettings);
  const [step, setStep] = useState(0);
  const [agent, setAgent] = useState<AgentKind>(settings.defaultAgent);
  const [binaryPath, setBinaryPath] = useState<string | null>(null);
  const [checkingBinary, setCheckingBinary] = useState(false);
  const [qrUrl, setQrUrl] = useState<string | null>(null);
  const [qrError, setQrError] = useState("");
  const [canaryState, setCanaryState] = useState<CanaryState>("idle");
  const [canaryMessage, setCanaryMessage] = useState("");

  const agentLabel = useMemo(
    () => agentDisplayLabel(agent, settings),
    [agent, settings],
  );

  useEffect(() => {
    if (!open || agent === "shell") {
      setBinaryPath(null);
      setCheckingBinary(false);
      return;
    }
    let cancelled = false;
    setCheckingBinary(true);
    void resolveAgentBinary(agent, settings)
      .then((path) => {
        if (!cancelled) {
          setBinaryPath(path);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setBinaryPath(null);
        }
      })
      .finally(() => {
        if (!cancelled) {
          setCheckingBinary(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [agent, open, settings]);

  useEffect(() => {
    if (!open || step !== 2) {
      return;
    }
    let disposed = false;
    let objectUrl: string | null = null;
    setQrError("");
    void ensureApprovalConnectionConfig()
      .then(async ({ token, port }) => {
        const response = await fetch(`http://127.0.0.1:${port ?? 17893}/v1/qr`, {
          headers: token ? { authorization: `Bearer ${token}` } : {},
        });
        if (!response.ok) {
          throw new Error(`QR fetch failed: HTTP ${response.status}`);
        }
        objectUrl = URL.createObjectURL(await response.blob());
        if (!disposed) {
          setQrUrl(objectUrl);
        }
      })
      .catch((caught) => {
        if (!disposed) {
          setQrError(caught instanceof Error ? caught.message : String(caught));
        }
      });
    return () => {
      disposed = true;
      if (objectUrl) {
        URL.revokeObjectURL(objectUrl);
      }
      setQrUrl(null);
    };
  }, [open, step]);

  if (!open) {
    return null;
  }

  function finish() {
    updateSettings({ defaultAgent: agent });
    window.localStorage.setItem("onibi.onboarding.dismissed", "1");
    onClose();
  }

  async function sendCanaryApproval() {
    setCanaryState("waiting");
    setCanaryMessage("Waiting for approval decision...");
    try {
      const { token, port } = await ensureApprovalConnectionConfig();
      const response = await fetch(`http://127.0.0.1:${port ?? 17893}/v1/approval/request`, {
        method: "POST",
        headers: {
          "content-type": "application/json",
          ...(token ? { authorization: `Bearer ${token}` } : {}),
        },
        body: JSON.stringify({
          protocol_version: "1.0",
          session_id: "onibi-onboarding-canary",
          agent,
          tool: "Bash",
          input: { command: "echo onibi-canary" },
          cwd: "~",
          metadata: { source: "onboarding" },
        }),
      });
      if (!response.ok) {
        throw new Error(`Canary approval failed: HTTP ${response.status}`);
      }
      const decision = (await response.json()) as { decision?: string; reason?: string };
      setCanaryState(decision.decision === "allow" ? "allowed" : "denied");
      setCanaryMessage(decision.reason ?? `Decision: ${decision.decision ?? "unknown"}`);
    } catch (caught) {
      setCanaryState("error");
      setCanaryMessage(caught instanceof Error ? caught.message : String(caught));
    }
  }

  const installCommand = `onibi integration install ${agent}`;

  return (
    <div className="modal-backdrop onboarding-dialog" role="presentation">
      <section
        className="modal-panel"
        role="dialog"
        aria-modal="true"
        aria-labelledby="onboarding-title"
      >
        <header className="modal-header">
          <h2 className="modal-title" id="onboarding-title">
            Set up Onibi
          </h2>
          <button type="button" className="icon-button" aria-label="Skip onboarding" onClick={finish}>
            x
          </button>
        </header>
        <div className="modal-body">
          <div className="activity-filter-row" role="tablist" aria-label="Onboarding steps">
            {["Agent", "Adapter", "Phone", "Test"].map((label, index) => (
              <button
                key={label}
                type="button"
                className={step === index ? "active" : ""}
                aria-selected={step === index}
                role="tab"
                onClick={() => setStep(index)}
              >
                {label}
              </button>
            ))}
          </div>

          {step === 0 ? (
            <label className="field-label">
              Default agent
              <select
                className="settings-select"
                value={agent}
                onChange={(event) => setAgent(event.target.value as AgentKind)}
              >
                {AGENT_KINDS.map((kind) => (
                  <option key={kind} value={kind}>
                    {agentDisplayLabel(kind, settings)}
                  </option>
                ))}
              </select>
            </label>
          ) : null}

          {step === 1 ? (
            <div className="settings-code-block">
              <strong>{agentLabel}</strong>
              <p>
                {checkingBinary
                  ? "Checking PATH..."
                  : binaryPath
                    ? `Binary found at ${binaryPath}`
                    : "Binary not found on PATH."}
              </p>
              <p>Install or refresh the adapter with:</p>
              <code>{installCommand}</code>
            </div>
          ) : null}

          {step === 2 ? (
            <div className="onboarding-qr">
              {qrUrl ? <img src={qrUrl} alt="Onibi pairing QR" /> : null}
              {qrError ? <div className="tree-error">{qrError}</div> : null}
              {!qrUrl && !qrError ? <div className="source-control-empty">Loading QR...</div> : null}
            </div>
          ) : null}

          {step === 3 ? (
            <div className="settings-code-block">
              <p>
                Send a safe approval request for <code>echo onibi-canary</code>.
                Approve it from desktop or phone to confirm the gate.
              </p>
              <button
                type="button"
                className="text-button primary"
                disabled={canaryState === "waiting"}
                onClick={() => void sendCanaryApproval()}
              >
                Send Canary Approval
              </button>
              {canaryMessage ? <p>{canaryMessage}</p> : null}
            </div>
          ) : null}
        </div>
        <footer className="modal-footer">
          <button type="button" className="text-button" onClick={finish}>
            Skip
          </button>
          {step > 0 ? (
            <button type="button" className="text-button" onClick={() => setStep((value) => value - 1)}>
              Back
            </button>
          ) : null}
          {step < 3 ? (
            <button type="button" className="text-button primary" onClick={() => setStep((value) => value + 1)}>
              Next
            </button>
          ) : (
            <button type="button" className="text-button primary" onClick={finish}>
              Finish
            </button>
          )}
        </footer>
      </section>
    </div>
  );
}
