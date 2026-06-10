import type { AgentKind } from "./sessions";
import claudeIcon from "../assets/agent-logos/anthropic.png";
import codexIcon from "../assets/agent-logos/codex.png";
import opencodeIcon from "../assets/agent-logos/opencode.png";
import geminiIcon from "../assets/agent-logos/gemini.png";
import aiderIcon from "../assets/agent-logos/aider.png";
import cursorIcon from "../assets/agent-logos/cursor.png";
import gooseIcon from "../assets/agent-logos/goose.png";
import copilotIcon from "../assets/agent-logos/copilot.png";
import ampIcon from "../assets/agent-logos/amp.jpeg";
import piIcon from "../assets/agent-logos/pi.png";
import clineIcon from "../assets/agent-logos/cline.png";
import shellIcon from "../assets/agents/shell.svg";

const AGENT_ICONS: Partial<Record<AgentKind, string>> = {
  "claude-code": claudeIcon,
  codex: codexIcon,
  opencode: opencodeIcon,
  gemini: geminiIcon,
  aider: aiderIcon,
  cursor: cursorIcon,
  goose: gooseIcon,
  copilot: copilotIcon,
  amp: ampIcon,
  pi: piIcon,
  cline: clineIcon,
  shell: shellIcon,
};

function fallbackAgentIcon(agent: AgentKind): string {
  const label = agent.slice(0, 1).toUpperCase();
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" rx="14" fill="#21252b"/><text x="32" y="41" text-anchor="middle" font-family="ui-sans-serif, system-ui" font-size="28" font-weight="800" fill="#f2c14e">${label}</text></svg>`;
  return `data:image/svg+xml,${encodeURIComponent(svg)}`;
}

export function agentIconUrl(agent: AgentKind): string {
  return AGENT_ICONS[agent] ?? fallbackAgentIcon(agent);
}
