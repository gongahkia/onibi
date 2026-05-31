import type { EvidenceRef } from "../types/claim.js";
import type { EventLogRecord } from "../linker/eventlog.js";
import { hashEvidenceRow } from "../linker/hashing.js";

export interface SigmaRule {
  readonly id: string;
  readonly title: string;
  readonly level?: string | undefined;
  readonly status?: string | undefined;
  readonly logsource: Readonly<Record<string, string>>;
  readonly detection: SigmaDetection;
  readonly yaml: string;
}

export interface SigmaDetection {
  readonly selections: Readonly<Record<string, SigmaSelection>>;
  readonly condition: string;
}

export type SigmaSelection = Readonly<Record<string, unknown>>;

export interface SigmaMatch {
  readonly rule: SigmaRule;
  readonly record: EventLogRecord;
  readonly matchedSelections: readonly string[];
}

const curatedRuleYaml = [
  String.raw`title: Windows Service Installed
id: 0b71a2f7-bf1e-4bd6-a5bd-2f61b8977d01
status: stable
level: high
logsource:
  product: windows
  service: system
detection:
  selection:
    Channel: System
    EventID: 7045
  condition: selection`,
  String.raw`title: Security Audit Service Installed
id: 9a1f40c4-9c2d-4a4c-a87e-cf5a5d8c1102
status: stable
level: high
logsource:
  product: windows
  service: security
detection:
  selection:
    Channel: Security
    EventID: 4697
  condition: selection`,
  String.raw`title: PowerShell Encoded Payload In Script Block
id: 17456f3d-f596-47ab-9225-9b5aeb16e603
status: stable
level: high
logsource:
  product: windows
  service: powershell
detection:
  selection_basic:
    EventID: 4104
  selection_payload:
    ScriptBlockText|contains: ['-enc', '-encodedcommand', 'FromBase64String', 'Convert.FromBase64String']
  condition: selection_basic and selection_payload`,
  String.raw`title: PowerShell Download Cradle In Script Block
id: f8e30c60-c031-4f12-b7be-fb6c12a49404
status: stable
level: high
logsource:
  product: windows
  service: powershell
detection:
  selection_basic:
    EventID: 4104
  selection_keyword:
    ScriptBlockText|contains: ['DownloadString', 'DownloadFile', 'Invoke-WebRequest', 'Net.WebClient', 'Start-BitsTransfer']
  selection_network:
    ScriptBlockText|contains: ['http://', 'https://']
  condition: selection_basic and selection_keyword and selection_network`,
  String.raw`title: PowerShell Defender Preference Tampering
id: 58f87c79-319a-4fe0-b205-03c24092aa01
status: stable
level: high
logsource:
  product: windows
  service: powershell
detection:
  selection_basic:
    EventID: 4104
  selection_cmdlet:
    ScriptBlockText|contains: ['Set-MpPreference', 'Add-MpPreference']
  selection_setting:
    ScriptBlockText|contains: ['DisableRealtimeMonitoring', 'ExclusionPath', 'DisableIOAVProtection', 'DisableBehaviorMonitoring']
  condition: selection_basic and selection_cmdlet and selection_setting`,
  String.raw`title: AdFind Active Directory Discovery Execution
id: 02115df1-157b-47ec-91df-30fcc47e9cc0
status: stable
level: high
logsource:
  product: windows
  service: security
detection:
  selection_basic:
    Channel: Security
    EventID: 4688
  selection_image:
    NewProcessName|endswith: ['\adfind.exe', '\adfind64.exe']
  selection_cmd:
    CommandLine|contains: ['adfind.exe', 'adfind64.exe']
  condition: selection_basic and (selection_image or selection_cmd)`,
  String.raw`title: Suspicious Rundll32 LOLBin Execution
id: 12ca9bd2-165c-41da-9a36-572ff6acdbf3
status: stable
level: high
logsource:
  product: windows
  service: security
detection:
  selection_basic:
    Channel: Security
    EventID: 4688
  selection_image:
    NewProcessName|endswith: '\rundll32.exe'
  selection_cmd:
    CommandLine|contains: ['javascript:', 'vbscript:', 'url.dll,FileProtocolHandler', 'shell32.dll,Control_RunDLL', '.sct', 'http://', 'https://']
  condition: selection_basic and selection_image and selection_cmd`,
  String.raw`title: Regsvr32 Scriptlet Execution
id: a5dd48cc-6c75-4574-9d8f-6e57d09eb92d
status: stable
level: high
logsource:
  product: windows
  service: security
detection:
  selection_basic:
    Channel: Security
    EventID: 4688
  selection_image:
    NewProcessName|endswith: '\regsvr32.exe'
  selection_cmd:
    CommandLine|contains: ['scrobj.dll', '/i:', '.sct', 'http://', 'https://']
  condition: selection_basic and selection_image and selection_cmd`,
  String.raw`title: Mshta Remote Or Script Execution
id: 42d6f3f4-5a2a-4b25-a706-bad85c6c0a78
status: stable
level: high
logsource:
  product: windows
  service: security
detection:
  selection_basic:
    Channel: Security
    EventID: 4688
  selection_image:
    NewProcessName|endswith: '\mshta.exe'
  selection_cmd:
    CommandLine|contains: ['http://', 'https://', 'javascript:', 'vbscript:', '.hta']
  condition: selection_basic and selection_image and selection_cmd`,
  String.raw`title: Certutil Download Or Decode
id: 87ecb096-974d-4f33-83b8-a365e9f0e43e
status: stable
level: high
logsource:
  product: windows
  service: security
detection:
  selection_basic:
    Channel: Security
    EventID: 4688
  selection_image:
    NewProcessName|endswith: '\certutil.exe'
  selection_cmd:
    CommandLine|contains: ['-urlcache', '-decode', '-decodehex', 'http://', 'https://']
  condition: selection_basic and selection_image and selection_cmd`,
  String.raw`title: Suspicious Scheduled Task Registration
id: d359a0e2-00f9-43cf-a5ce-dca1a672d2e4
status: stable
level: high
logsource:
  product: windows
  service: security
detection:
  selection_basic:
    Channel: Security
    EventID: [4698, 4702]
  selection_task:
    TaskContent|contains: ['\Users\Public\', '\ProgramData\', 'powershell', 'cmd.exe', 'wscript.exe', 'mshta.exe', 'rundll32.exe']
  condition: selection_basic and selection_task`,
  String.raw`title: LSASS Memory Dump Tool Execution
id: cde50aaa-8548-4d78-a58c-9f8db5d4e814
status: stable
level: critical
logsource:
  product: windows
  service: security
detection:
  selection_basic:
    Channel: Security
    EventID: 4688
  selection_lsass:
    CommandLine|contains: ['lsass.exe', 'lsass']
  selection_dump:
    CommandLine|contains: ['MiniDump', 'comsvcs.dll', 'procdump', '-ma']
  condition: selection_basic and selection_lsass and selection_dump`,
  String.raw`title: WMIC Process Creation Or Remote Execution
id: 03ed9d6f-5f47-42c1-b334-ad35311d63c7
status: stable
level: medium
logsource:
  product: windows
  service: security
detection:
  selection_basic:
    Channel: Security
    EventID: 4688
  selection_image:
    NewProcessName|endswith: '\wmic.exe'
  selection_cmd:
    CommandLine|contains: ['process call create', '/node:', 'shadowcopy', 'startup call']
  condition: selection_basic and selection_image and selection_cmd`,
  String.raw`title: Bitsadmin File Transfer
id: 5af17a26-bd44-41de-8062-67b7672b1a59
status: stable
level: medium
logsource:
  product: windows
  service: security
detection:
  selection_basic:
    Channel: Security
    EventID: 4688
  selection_image:
    NewProcessName|endswith: '\bitsadmin.exe'
  selection_cmd:
    CommandLine|contains: ['/transfer', 'http://', 'https://']
  condition: selection_basic and selection_image and selection_cmd`,
  String.raw`title: Windows Script Host From User Writable Location
id: a198c822-6698-477e-886d-3d8f87890567
status: stable
level: high
logsource:
  product: windows
  service: security
detection:
  selection_basic:
    Channel: Security
    EventID: 4688
  selection_image:
    NewProcessName|endswith: ['\wscript.exe', '\cscript.exe']
  selection_path:
    CommandLine|contains: ['\Users\Public\', '\ProgramData\', '\AppData\Local\Temp\', '.vbs', '.js']
  condition: selection_basic and selection_image and selection_path`
] as const;

export function loadCuratedRuleset(): SigmaRule[] {
  return curatedRuleYaml.map(parseSigmaRuleYaml);
}

export function matchEventLogAgainstSigma(
  records: readonly EventLogRecord[],
  rules: readonly SigmaRule[]
): SigmaMatch[] {
  return records.flatMap((record) =>
    rules.flatMap((rule) => {
      const matchedSelections = matchedSelectionNames(rule, record);
      if (
        !evaluateCondition(
          rule.detection.condition,
          Object.keys(rule.detection.selections),
          matchedSelections
        )
      ) {
        return [];
      }
      return [{ rule, record, matchedSelections }];
    })
  );
}

export function sigmaMatchesAsEvidence(matches: readonly SigmaMatch[]): EvidenceRef[] {
  return matches.map((match) => ({
    artifact: match.record.artifact,
    locator: `evtx:channel=${match.record.channel}:record=${match.record.recordId}:sigma=${match.rule.id}`,
    supports: "sigma_rule_match",
    hash: hashEvidenceRow({
      supports: "sigma_rule_match",
      ruleId: match.rule.id,
      ruleTitle: match.rule.title,
      ruleLevel: match.rule.level,
      eventId: match.record.eventId,
      channel: match.record.channel,
      recordId: match.record.recordId,
      matchedSelections: match.matchedSelections,
      eventData: match.record.eventData,
      raw: match.record.raw
    })
  }));
}

export function parseSigmaRuleYaml(yaml: string): SigmaRule {
  const parsed = parseYamlObject(yaml);
  const detection = asObject(parsed.detection);
  if (!detection) {
    throw new Error("Sigma rule is missing detection");
  }
  const condition = stringValue(detection.condition);
  if (!condition) {
    throw new Error("Sigma rule is missing detection.condition");
  }
  const selections = Object.fromEntries(
    Object.entries(detection).filter(([key]) => key !== "condition")
  ) as Record<string, SigmaSelection>;
  const id = stringValue(parsed.id);
  const title = stringValue(parsed.title);
  if (!id || !title) {
    throw new Error("Sigma rule is missing id or title");
  }
  return {
    id,
    title,
    ...(stringValue(parsed.level) ? { level: stringValue(parsed.level) } : {}),
    ...(stringValue(parsed.status) ? { status: stringValue(parsed.status) } : {}),
    logsource: stringRecord(asObject(parsed.logsource)),
    detection: { selections, condition },
    yaml
  };
}

function matchedSelectionNames(rule: SigmaRule, record: EventLogRecord): string[] {
  return Object.entries(rule.detection.selections)
    .filter(([, selection]) => selectionMatches(selection, record))
    .map(([name]) => name);
}

function selectionMatches(selection: SigmaSelection, record: EventLogRecord): boolean {
  const alternatives = Array.isArray(selection) ? selection : [selection];
  return alternatives.some((candidate) => criteriaMatches(candidate, record));
}

function criteriaMatches(criteria: SigmaSelection, record: EventLogRecord): boolean {
  return Object.entries(criteria).every(([field, expected]) =>
    fieldMatches(record, field, expected)
  );
}

function fieldMatches(record: EventLogRecord, fieldExpression: string, expected: unknown): boolean {
  const [field, ...modifiers] = fieldExpression.split("|");
  if (!field) {
    return false;
  }
  const values = valuesForField(record, field);
  if (expected === null) {
    return values.length === 0;
  }
  if (Array.isArray(expected)) {
    const expectedValues = expected;
    return modifiers.includes("all")
      ? expectedValues.every((value) =>
          values.some((actual) => valueMatches(actual, value, modifiers))
        )
      : expectedValues.some((value) =>
          values.some((actual) => valueMatches(actual, value, modifiers))
        );
  }
  return values.some((actual) => valueMatches(actual, expected, modifiers));
}

function valueMatches(actual: string, expected: unknown, modifiers: readonly string[]): boolean {
  const expectedValue = stringValue(expected);
  if (expectedValue === undefined) {
    return false;
  }
  if (modifiers.includes("re")) {
    return new RegExp(expectedValue, modifiers.includes("cased") ? "u" : "iu").test(actual);
  }
  const cased = modifiers.includes("cased");
  const actualValue = cased ? actual : actual.toLowerCase();
  const matcherValue = cased ? expectedValue : expectedValue.toLowerCase();
  if (modifiers.includes("contains")) {
    return actualValue.includes(matcherValue);
  }
  if (modifiers.includes("startswith")) {
    return actualValue.startsWith(matcherValue);
  }
  if (modifiers.includes("endswith")) {
    return actualValue.endsWith(matcherValue);
  }
  if (/[?*]/u.test(matcherValue)) {
    return wildcardRegex(matcherValue, cased).test(actual);
  }
  return actualValue === matcherValue;
}

function valuesForField(record: EventLogRecord, field: string): string[] {
  const values = eventLogFieldMap(record);
  const normalized = normalizeFieldName(field);
  const pathLeaf = field.split(".").at(-1);
  const aliases = fieldAliases(normalized);
  const found = [
    normalized,
    ...(pathLeaf ? [normalizeFieldName(pathLeaf)] : []),
    ...aliases
  ].flatMap((name) => values.get(name) ?? []);
  return [...new Set(found)];
}

function eventLogFieldMap(record: EventLogRecord): Map<string, string[]> {
  const values = new Map<string, string[]>();
  addFieldValue(values, "EventID", record.eventId);
  addFieldValue(values, "EventId", record.eventId);
  addFieldValue(values, "EventRecordID", record.recordId);
  addFieldValue(values, "RecordID", record.recordId);
  addFieldValue(values, "Channel", record.channel);
  addFieldValue(values, "Provider", record.provider);
  addFieldValue(values, "ProviderName", record.provider);
  addFieldValue(values, "TimeCreated", record.timeCreated);
  addFieldValue(values, "Message", record.message);
  for (const [key, value] of Object.entries(record.eventData)) {
    addFieldValue(values, key, value);
  }
  return values;
}

function addFieldValue(values: Map<string, string[]>, field: string, value: unknown): void {
  const text = stringValue(value);
  if (!text) {
    return;
  }
  const key = normalizeFieldName(field);
  values.set(key, [...(values.get(key) ?? []), text]);
}

function fieldAliases(normalized: string): string[] {
  const aliases: Record<string, readonly string[]> = {
    commandline: ["processcommandline", "cmdline"],
    processcommandline: ["commandline", "cmdline"],
    image: ["newprocessname", "processname", "processpath"],
    processname: ["newprocessname", "image"],
    parentimage: ["parentprocessname"],
    servicefilename: ["imagepath", "servicefilepath"],
    imagepath: ["servicefilename", "servicefilepath"],
    scriptblocktext: ["message", "payload"],
    payload: ["message", "scriptblocktext"],
    providername: ["provider", "providername"],
    eventcode: ["eventid"]
  };
  return [...(aliases[normalized] ?? [])];
}

function evaluateCondition(
  condition: string,
  selectionNames: readonly string[],
  matchedSelections: readonly string[]
): boolean {
  const parser = new ConditionParser(condition, selectionNames, new Set(matchedSelections));
  return parser.parse();
}

class ConditionParser {
  private readonly tokens: readonly string[];
  private offset = 0;

  constructor(
    condition: string,
    private readonly selectionNames: readonly string[],
    private readonly matchedSelections: ReadonlySet<string>
  ) {
    this.tokens = condition.match(/\d+|[A-Za-z_][A-Za-z0-9_*]*|[()]/gu) ?? [];
  }

  parse(): boolean {
    const value = this.parseOr();
    if (this.peek() !== undefined) {
      throw new Error(`unsupported Sigma condition token: ${this.peek()}`);
    }
    return value;
  }

  private parseOr(): boolean {
    let value = this.parseAnd();
    while (this.consume("or")) {
      value = this.parseAnd() || value;
    }
    return value;
  }

  private parseAnd(): boolean {
    let value = this.parseNot();
    while (this.consume("and")) {
      value = this.parseNot() && value;
    }
    return value;
  }

  private parseNot(): boolean {
    if (this.consume("not")) {
      return !this.parseNot();
    }
    return this.parsePrimary();
  }

  private parsePrimary(): boolean {
    if (this.consume("(")) {
      const value = this.parseOr();
      this.expect(")");
      return value;
    }
    const token = this.next();
    if (token === undefined) {
      throw new Error("unexpected end of Sigma condition");
    }
    if (token === "all" || token === "any" || /^\d+$/u.test(token)) {
      this.expect("of");
      const pattern = this.next();
      if (pattern === undefined) {
        throw new Error("Sigma condition is missing an 'of' pattern");
      }
      return this.evaluateOf(token, pattern);
    }
    return this.matchedSelections.has(token);
  }

  private evaluateOf(quantity: string, pattern: string): boolean {
    const regex = wildcardRegex(pattern, true);
    const candidates =
      pattern === "them"
        ? this.selectionNames
        : this.selectionNames.filter((name) => regex.test(name));
    const matchedCount = candidates.filter((name) => this.matchedSelections.has(name)).length;
    if (quantity === "all") {
      return candidates.length > 0 && matchedCount === candidates.length;
    }
    if (quantity === "any") {
      return matchedCount > 0;
    }
    return matchedCount >= Number(quantity);
  }

  private consume(token: string): boolean {
    if (this.peek() !== token) {
      return false;
    }
    this.offset += 1;
    return true;
  }

  private expect(token: string): void {
    if (!this.consume(token)) {
      throw new Error(`expected Sigma condition token: ${token}`);
    }
  }

  private next(): string | undefined {
    const token = this.peek();
    this.offset += 1;
    return token;
  }

  private peek(): string | undefined {
    return this.tokens[this.offset];
  }
}

function wildcardRegex(pattern: string, cased: boolean): RegExp {
  const escaped = pattern
    .replace(/[.+^${}()|[\]\\]/gu, "\\$&")
    .replace(/\*/gu, ".*")
    .replace(/\?/gu, ".");
  return new RegExp(`^${escaped}$`, cased ? "u" : "iu");
}

function parseYamlObject(yaml: string): Record<string, unknown> {
  const root: Record<string, unknown> = {};
  const stack: { readonly indent: number; readonly object: Record<string, unknown> }[] = [
    { indent: -1, object: root }
  ];
  for (const rawLine of yaml.split(/\r?\n/u)) {
    const line = rawLine.replace(/\s+#.*$/u, "");
    if (line.trim().length === 0) {
      continue;
    }
    const indent = line.match(/^ */u)?.[0].length ?? 0;
    const match = line.trim().match(/^([^:]+):(.*)$/u);
    if (!match) {
      throw new Error(`unsupported Sigma YAML line: ${rawLine}`);
    }
    while (stack.length > 1 && indent <= stack[stack.length - 1]!.indent) {
      stack.pop();
    }
    const key = match[1]!.trim();
    const rawValue = match[2]!.trim();
    const parent = stack[stack.length - 1]!.object;
    if (rawValue.length === 0) {
      const child: Record<string, unknown> = {};
      parent[key] = child;
      stack.push({ indent, object: child });
      continue;
    }
    parent[key] = parseYamlScalar(rawValue);
  }
  return root;
}

function parseYamlScalar(value: string): unknown {
  if (value.startsWith("[") && value.endsWith("]")) {
    return splitInlineArray(value.slice(1, -1)).map(parseYamlScalar);
  }
  if (
    (value.startsWith("'") && value.endsWith("'")) ||
    (value.startsWith('"') && value.endsWith('"'))
  ) {
    return value.slice(1, -1);
  }
  if (value === "null") {
    return null;
  }
  if (value === "true" || value === "false") {
    return value === "true";
  }
  if (/^-?\d+(?:\.\d+)?$/u.test(value)) {
    return Number(value);
  }
  return value;
}

function splitInlineArray(value: string): string[] {
  const items: string[] = [];
  let current = "";
  let quote: "'" | '"' | undefined;
  for (const char of value) {
    if ((char === "'" || char === '"') && quote === undefined) {
      quote = char;
      current += char;
      continue;
    }
    if (char === quote) {
      quote = undefined;
      current += char;
      continue;
    }
    if (char === "," && quote === undefined) {
      items.push(current.trim());
      current = "";
      continue;
    }
    current += char;
  }
  if (current.trim().length > 0) {
    items.push(current.trim());
  }
  return items;
}

function stringRecord(
  input: Readonly<Record<string, unknown>> | undefined
): Record<string, string> {
  if (!input) {
    return {};
  }
  return Object.fromEntries(
    Object.entries(input).flatMap(([key, value]) => {
      const text = stringValue(value);
      return text ? [[key, text]] : [];
    })
  );
}

function stringValue(input: unknown): string | undefined {
  if (typeof input === "string") {
    return input.trim();
  }
  if (typeof input === "number" || typeof input === "boolean") {
    return String(input);
  }
  return undefined;
}

function asObject(input: unknown): Record<string, unknown> | undefined {
  return input !== null && typeof input === "object" && !Array.isArray(input)
    ? (input as Record<string, unknown>)
    : undefined;
}

function normalizeFieldName(value: string): string {
  return value.toLowerCase().replace(/[^a-z0-9]+/gu, "");
}
