export type ImperativePatternId =
  | "ignore-previous-instructions"
  | "delete-or-remove"
  | "exfiltrate-or-upload"
  | "pipe-to-shell"
  | "skip-or-stop-analysis"
  | "run-named-script";

export interface ImperativePattern {
  readonly id: ImperativePatternId;
  readonly regex: RegExp;
  readonly rationale: string;
  readonly heuristic?: (text: string) => boolean;
}

export interface ImperativePatternMatch {
  readonly id: ImperativePatternId;
  readonly rationale: string;
  readonly matchedText: string;
}

export const imperativePatterns: readonly ImperativePattern[] = [
  {
    id: "ignore-previous-instructions",
    regex:
      /\b(?:ignore|disregard|forget|override)\s+(?:all\s+)?(?:previous|prior|above|earlier|system|developer)\s+(?:instructions?|rules?|prompts?)\b/iu,
    rationale: "Attempts to override the controlling prompt are evidence text, not authority."
  },
  {
    id: "delete-or-remove",
    regex:
      /(?:^|[\s;&|`])(?:sudo\s+)?(?:rm\s+-[^\n;]*[rf][^\n;]*|del\s+(?:\/[fqsa]\s*)+|rmdir\s+\/s|remove-item\b[^\n]*(?:-recurse|-force)|erase\s+(?:logs?|evidence)|delete\s+(?:all\s+)?(?:logs?|evidence|files?))/iu,
    rationale: "Destructive cleanup language in case artifacts must not become an action."
  },
  {
    id: "exfiltrate-or-upload",
    regex:
      /\b(?:curl\b(?=[^\n]*(?:-f\b|--form\b|--data\b|--data-raw\b|--data-binary\b|-d\b|-x\s+post\b|--request\s+post\b))|wget\b(?=[^\n]*(?:--post-data\b|--method[=\s]+post\b))|scp\b|exfiltrate\b|upload\s+.+\s+to\b)/iu,
    rationale: "Network upload or copy instructions can leak evidence or secrets when followed."
  },
  {
    id: "pipe-to-shell",
    regex: /\b(?:curl|wget)\b[^\n|;]*\|\s*(?:sudo\s+)?(?:sh|bash|zsh|ksh|pwsh|powershell)\b/iu,
    rationale: "Download-and-execute pipelines transfer execution control to untrusted text."
  },
  {
    id: "skip-or-stop-analysis",
    regex:
      /\b(?:skip|stop|abort|halt|cancel|terminate)\s+(?:the\s+)?(?:analysis|investigation|scan|triage|review)\b|\bdo\s+not\s+(?:analy[sz]e|investigate|report|inspect)\b|\bno\s+need\s+to\s+(?:analy[sz]e|investigate|report|inspect)\b/iu,
    rationale: "Instructions to avoid analysis directly undermine the forensic task."
  },
  {
    id: "run-named-script",
    regex:
      /(?:^|[\s;&|])(?:\.\/|\.\.\/)[^\s"'`;|&]+\.(?:sh|bash|zsh|ps1|bat|cmd|exe)\b|\b(?:run|execute|launch|start|invoke)\s+(?:\.\/|\.\.\/)?[^\s"'`;|&]+\.(?:sh|bash|zsh|ps1|bat|cmd|exe)\b/iu,
    rationale:
      "Named scripts from evidence can be payloads or decoys and need analysis, not execution.",
    heuristic: (text) =>
      !/\b(?:contains|found|observed|mentions?|named)\s+[^\n.]*\.(?:sh|exe)\b/iu.test(text)
  }
] as const;

export function detectImperativePattern(text: string): ImperativePatternMatch | undefined {
  for (const pattern of imperativePatterns) {
    if (!matchesImperativePattern(pattern.id, text)) {
      continue;
    }
    const match = pattern.regex.exec(text);
    return {
      id: pattern.id,
      rationale: pattern.rationale,
      matchedText: match?.[0]?.trim() ?? text
    };
  }
  return undefined;
}

export function matchesImperativePattern(id: ImperativePatternId, text: string): boolean {
  const pattern = imperativePatterns.find((candidate) => candidate.id === id);
  if (!pattern) {
    return false;
  }
  pattern.regex.lastIndex = 0;
  return pattern.regex.test(text) && (pattern.heuristic ? pattern.heuristic(text) : true);
}

export function extractScriptTokens(text: string): readonly string[] {
  const scriptPattern = /(?:\.\/|\.\.\/)?[A-Za-z0-9._/-]+\.(?:sh|bash|zsh|ps1|bat|cmd|exe)\b/giu;
  return [...new Set([...text.matchAll(scriptPattern)].map((match) => match[0]))];
}
