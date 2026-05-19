import { randomUUID } from "node:crypto";
import { Socket, connect as connectTcp } from "node:net";
import { TLSSocket, connect as connectTls } from "node:tls";
import { assertAdapterCredentialRefs } from "./credentials.js";
import { builtinAdapterMetadata } from "./builtins.js";
import type {
  Adapter,
  AdapterAuditEvent,
  AdapterInvocation,
  AdapterKind,
  AdapterMetadata,
  AdapterProviderMetadata,
  AdapterResult
} from "./types.js";
import type { JsonRecord, JsonValue } from "@kelpclaw/workflow-spec";

export interface LiveAdapterHttpOptions {
  readonly fetch?: typeof fetch | undefined;
  readonly googleApiBaseUrl?: string | undefined;
  readonly googleTokenUrl?: string | undefined;
  readonly whatsappApiBaseUrl?: string | undefined;
  readonly telegramApiBaseUrl?: string | undefined;
}

export interface SmtpTransportOptions {
  readonly host?: string | undefined;
  readonly port?: number | undefined;
  readonly secure?: boolean | undefined;
  readonly username?: string | undefined;
  readonly password?: string | undefined;
  readonly from?: string | undefined;
}

interface ResolvedSmtpTransportOptions {
  readonly host: string;
  readonly port: number;
  readonly secure: boolean;
  readonly username: string;
  readonly password: string;
  readonly from: string;
}

export interface LiveAdapterOptions extends LiveAdapterHttpOptions {
  readonly smtp?: SmtpTransportOptions | undefined;
}

export function createDefaultLiveAdapters(options: LiveAdapterOptions = {}): Map<string, Adapter> {
  const metadata = new Map(builtinAdapterMetadata.map((adapter) => [adapter.id, adapter]));
  return new Map<string, Adapter>([
    ["adapter.gmail", new GmailLiveAdapter(requireMetadata(metadata, "adapter.gmail"), options)],
    ["adapter.sheets", new SheetsLiveAdapter(requireMetadata(metadata, "adapter.sheets"), options)],
    [
      "adapter.email",
      new SmtpEmailAdapter(requireMetadata(metadata, "adapter.email"), options.smtp)
    ],
    [
      "adapter.whatsapp",
      new WhatsAppLiveAdapter(requireMetadata(metadata, "adapter.whatsapp"), options)
    ],
    [
      "adapter.telegram",
      new TelegramLiveAdapter(requireMetadata(metadata, "adapter.telegram"), options)
    ]
  ]);
}

class GmailLiveAdapter implements Adapter {
  public constructor(
    public readonly metadata: AdapterMetadata,
    private readonly options: LiveAdapterHttpOptions = {}
  ) {}

  public async invoke(invocation: AdapterInvocation): Promise<AdapterResult> {
    assertInvocation(this.metadata, invocation);
    const accessToken = await googleAccessToken(invocation, "gmail.oauth", this.options);
    const apiBase = this.options.googleApiBaseUrl ?? "https://gmail.googleapis.com";
    const fetchImpl = this.options.fetch ?? fetch;
    const query = stringValue(invocation.payload.query, "from:(receipts OR orders)");
    const maxResults = Math.min(numberValue(invocation.payload.maxResults, 25), 100);
    const listUrl = new URL(`${apiBase}/gmail/v1/users/me/messages`);
    listUrl.searchParams.set("q", query);
    listUrl.searchParams.set("maxResults", String(maxResults));
    const listed = await readJson<GmailListResponse>(
      fetchImpl(listUrl, authRequest(accessToken)),
      "GMAIL_LIST_FAILED"
    );
    const messages = listed.messages ?? [];
    const receipts: JsonRecord[] = [];

    for (const message of messages) {
      const getUrl = new URL(`${apiBase}/gmail/v1/users/me/messages/${message.id}`);
      getUrl.searchParams.set("format", "full");
      const detail = await readJson<GmailMessage>(
        fetchImpl(getUrl, authRequest(accessToken)),
        "GMAIL_GET_FAILED"
      );
      receipts.push(receiptFromMessage(detail));
    }

    return succeededResult({
      invocation,
      provider: "gmail",
      providerResponseId: listed.nextPageToken ?? `gmail.${messages.length}.${randomUUID()}`,
      output: {
        receipts,
        query,
        resultSizeEstimate: listed.resultSizeEstimate ?? receipts.length
      },
      message: `Gmail returned ${receipts.length} receipt candidate(s).`
    });
  }
}

class SheetsLiveAdapter implements Adapter {
  public constructor(
    public readonly metadata: AdapterMetadata,
    private readonly options: LiveAdapterHttpOptions = {}
  ) {}

  public async invoke(invocation: AdapterInvocation): Promise<AdapterResult> {
    assertInvocation(this.metadata, invocation);
    const accessToken = await googleAccessToken(invocation, "sheets.oauth", this.options);
    const apiBase = this.options.googleApiBaseUrl ?? "https://sheets.googleapis.com";
    const fetchImpl = this.options.fetch ?? fetch;
    const spreadsheetId = requiredString(invocation.payload.spreadsheetId, "spreadsheetId");
    const range = requiredString(invocation.payload.range, "range");

    if (invocation.operation === "sheets.rows.append") {
      const rows = arrayValue(invocation.payload.rows).filter(isRecord);
      const values = rowsToValues(rows, invocation.payload.columns);
      const url = new URL(
        `${apiBase}/v4/spreadsheets/${encodeURIComponent(spreadsheetId)}/values/${encodeURIComponent(range)}:append`
      );
      url.searchParams.set("valueInputOption", "USER_ENTERED");
      url.searchParams.set("insertDataOption", "INSERT_ROWS");
      const result = await readJson<SheetsAppendResponse>(
        fetchImpl(url, {
          ...authRequest(accessToken),
          method: "POST",
          headers: {
            ...authHeaders(accessToken),
            "content-type": "application/json"
          },
          body: JSON.stringify({ values })
        }),
        "SHEETS_APPEND_FAILED"
      );
      return succeededResult({
        invocation,
        provider: "sheets",
        providerResponseId: result.updates?.updatedRange ?? `sheets.${randomUUID()}`,
        output: {
          spreadsheetId,
          range,
          appendedRows: result.updates?.updatedRows ?? values.length,
          ...(result.updates?.updatedRange ? { updatedRange: result.updates.updatedRange } : {}),
          rows
        },
        message: `Google Sheets appended ${values.length} row(s).`
      });
    }

    if (invocation.operation === "sheets.rows.lookup") {
      const url = new URL(
        `${apiBase}/v4/spreadsheets/${encodeURIComponent(spreadsheetId)}/values/${encodeURIComponent(range)}`
      );
      const result = await readJson<SheetsValuesResponse>(
        fetchImpl(url, authRequest(accessToken)),
        "SHEETS_LOOKUP_FAILED"
      );
      return succeededResult({
        invocation,
        provider: "sheets",
        providerResponseId: result.range ?? `sheets.${randomUUID()}`,
        output: {
          spreadsheetId,
          range: result.range ?? range,
          rows: valuesToObjects(result.values ?? [])
        },
        message: "Google Sheets lookup completed."
      });
    }

    const rows = arrayValue(invocation.payload.rows).filter(isRecord);
    const values = rowsToValues(rows, invocation.payload.columns);
    const url = new URL(
      `${apiBase}/v4/spreadsheets/${encodeURIComponent(spreadsheetId)}/values/${encodeURIComponent(range)}`
    );
    url.searchParams.set("valueInputOption", "USER_ENTERED");
    const result = await readJson<SheetsValuesResponse>(
      fetchImpl(url, {
        ...authRequest(accessToken),
        method: "PUT",
        headers: {
          ...authHeaders(accessToken),
          "content-type": "application/json"
        },
        body: JSON.stringify({ values })
      }),
      "SHEETS_UPDATE_FAILED"
    );
    return succeededResult({
      invocation,
      provider: "sheets",
      providerResponseId: result.updatedRange ?? result.range ?? `sheets.${randomUUID()}`,
      output: {
        spreadsheetId,
        range,
        updatedRows: result.updatedRows ?? values.length
      },
      message: `Google Sheets updated ${values.length} row(s).`
    });
  }
}

class SmtpEmailAdapter implements Adapter {
  public constructor(
    public readonly metadata: AdapterMetadata,
    private readonly options: SmtpTransportOptions = {}
  ) {}

  public async invoke(invocation: AdapterInvocation): Promise<AdapterResult> {
    assertInvocation(this.metadata, invocation);
    const config = smtpConfig(invocation, this.options);
    const to = requiredString(invocation.payload.to, "to");
    const subject = stringValue(invocation.payload.subject, "KelpClaw notification");
    const body = stringValue(invocation.payload.body, stableBody(invocation.payload.summary));
    const messageId = `<kelpclaw-${randomUUID()}@localhost>`;
    await sendSmtp({
      ...config,
      to,
      subject,
      body,
      messageId
    });
    return succeededResult({
      invocation,
      provider: "email",
      providerResponseId: messageId,
      output: {
        messageId,
        channel: "email",
        delivered: true,
        to
      },
      message: `SMTP email accepted for ${to}.`
    });
  }
}

class WhatsAppLiveAdapter implements Adapter {
  public constructor(
    public readonly metadata: AdapterMetadata,
    private readonly options: LiveAdapterHttpOptions = {}
  ) {}

  public async invoke(invocation: AdapterInvocation): Promise<AdapterResult> {
    assertInvocation(this.metadata, invocation);
    const apiKey = requiredSecret(invocation, "whatsapp.apiKey");
    const secretConfig = parseSecretJson(apiKey);
    const phoneNumberId = stringValue(
      invocation.payload.phoneNumberId,
      stringValue(secretConfig.phoneNumberId, process.env.WHATSAPP_PHONE_NUMBER_ID ?? "")
    );
    if (!phoneNumberId) {
      throw new Error("WHATSAPP_PHONE_NUMBER_ID or payload.phoneNumberId is required.");
    }
    const apiVersion = stringValue(
      invocation.payload.apiVersion,
      stringValue(secretConfig.apiVersion, process.env.WHATSAPP_API_VERSION ?? "v20.0")
    );
    const apiBase = this.options.whatsappApiBaseUrl ?? "https://graph.facebook.com";
    const fetchImpl = this.options.fetch ?? fetch;
    const to = requiredString(invocation.payload.to, "to");
    const text = stringValue(invocation.payload.text, stringValue(invocation.payload.body, ""));
    const result = await readJson<WhatsAppMessageResponse>(
      fetchImpl(`${apiBase}/${apiVersion}/${phoneNumberId}/messages`, {
        method: "POST",
        headers: {
          authorization: `Bearer ${stringValue(secretConfig.accessToken, apiKey)}`,
          "content-type": "application/json"
        },
        body: JSON.stringify({
          messaging_product: "whatsapp",
          recipient_type: "individual",
          to,
          type: "text",
          text: {
            preview_url: false,
            body: text
          }
        })
      }),
      "WHATSAPP_SEND_FAILED"
    );
    const messageId = result.messages?.[0]?.id ?? `whatsapp.${randomUUID()}`;
    return succeededResult({
      invocation,
      provider: "whatsapp",
      providerResponseId: messageId,
      output: {
        messageId,
        channel: "whatsapp",
        delivered: true
      },
      message: `WhatsApp message accepted for ${to}.`
    });
  }
}

class TelegramLiveAdapter implements Adapter {
  public constructor(
    public readonly metadata: AdapterMetadata,
    private readonly options: LiveAdapterHttpOptions = {}
  ) {}

  public async invoke(invocation: AdapterInvocation): Promise<AdapterResult> {
    assertInvocation(this.metadata, invocation);
    const botTokenSecret = requiredSecret(invocation, "telegram.botToken");
    const secretConfig = parseSecretJson(botTokenSecret);
    const botToken = stringValue(secretConfig.botToken, botTokenSecret);
    const chatId = stringValue(
      invocation.payload.chatId,
      stringValue(secretConfig.chatId, process.env.TELEGRAM_DEFAULT_CHAT_ID ?? "")
    );
    if (!chatId) {
      throw new Error(
        "Telegram chat id is required in payload.chatId or TELEGRAM_DEFAULT_CHAT_ID."
      );
    }
    const text = stringValue(invocation.payload.text, stringValue(invocation.payload.body, ""));
    const apiBase = this.options.telegramApiBaseUrl ?? "https://api.telegram.org";
    const fetchImpl = this.options.fetch ?? fetch;
    const result = await readJson<TelegramSendMessageResponse>(
      fetchImpl(`${apiBase}/bot${botToken}/sendMessage`, {
        method: "POST",
        headers: {
          "content-type": "application/json"
        },
        body: JSON.stringify({
          chat_id: chatId,
          text
        })
      }),
      "TELEGRAM_SEND_FAILED"
    );
    const messageId = String(result.result?.message_id ?? randomUUID());
    return succeededResult({
      invocation,
      provider: "telegram",
      providerResponseId: messageId,
      output: {
        messageId,
        channel: "telegram",
        delivered: result.ok
      },
      message: `Telegram message accepted for ${chatId}.`
    });
  }
}

function assertInvocation(metadata: AdapterMetadata, invocation: AdapterInvocation): void {
  if (invocation.adapterId !== metadata.id) {
    throw new Error(
      `Invocation targeted adapter '${invocation.adapterId}' but adapter is '${metadata.id}'.`
    );
  }
  assertAdapterCredentialRefs(metadata, invocation.secretRefs, { requireLiveCredentials: true });
  const operation = metadata.operations.find(
    (candidate) =>
      candidate.name === invocation.operation && candidate.version === invocation.operationVersion
  );
  if (!operation) {
    throw new Error(
      `Adapter '${metadata.id}' does not support operation '${invocation.operation}' version '${invocation.operationVersion}'.`
    );
  }
}

async function googleAccessToken(
  invocation: AdapterInvocation,
  secretName: string,
  options: LiveAdapterHttpOptions
): Promise<string> {
  const secret = requiredSecret(invocation, secretName);
  const parsed = parseSecretJson(secret);
  const accessToken = stringValue(parsed.accessToken, "");
  if (accessToken) {
    return accessToken;
  }
  const refreshToken = stringValue(parsed.refreshToken, secret);
  const clientId = stringValue(parsed.clientId, process.env.GOOGLE_CLIENT_ID ?? "");
  const clientSecret = stringValue(parsed.clientSecret, process.env.GOOGLE_CLIENT_SECRET ?? "");
  if (!refreshToken || !clientId || !clientSecret) {
    throw new Error(
      `${secretName} must resolve to an access token or refresh token with Google OAuth client credentials.`
    );
  }

  const fetchImpl = options.fetch ?? fetch;
  const tokenUrl = options.googleTokenUrl ?? "https://oauth2.googleapis.com/token";
  const body = new URLSearchParams({
    client_id: clientId,
    client_secret: clientSecret,
    refresh_token: refreshToken,
    grant_type: "refresh_token"
  });
  const response = await readJson<GoogleTokenResponse>(
    fetchImpl(tokenUrl, {
      method: "POST",
      headers: {
        "content-type": "application/x-www-form-urlencoded"
      },
      body
    }),
    "GOOGLE_TOKEN_REFRESH_FAILED"
  );
  if (!response.access_token) {
    throw new Error("Google OAuth token response did not include an access token.");
  }

  return response.access_token;
}

function requiredSecret(invocation: AdapterInvocation, secretName: string): string {
  const value = invocation.secrets?.[secretName];
  if (!value) {
    throw new Error(
      `Resolved secret '${secretName}' is required for adapter '${invocation.adapterId}'.`
    );
  }

  return value;
}

async function readJson<T>(responsePromise: Promise<Response>, code: string): Promise<T> {
  const response = await responsePromise;
  const text = await response.text();
  const parsed = text.length > 0 ? (JSON.parse(text) as unknown) : {};
  if (!response.ok) {
    const message =
      isRecord(parsed) && typeof parsed.error === "object" ? JSON.stringify(parsed.error) : text;
    throw new Error(`${code}: ${message || response.statusText}`);
  }

  return parsed as T;
}

function authHeaders(accessToken: string): Record<string, string> {
  return {
    authorization: `Bearer ${accessToken}`
  };
}

function authRequest(accessToken: string): RequestInit {
  return {
    headers: authHeaders(accessToken)
  };
}

function succeededResult(input: {
  readonly invocation: AdapterInvocation;
  readonly provider: AdapterKind;
  readonly providerResponseId: string;
  readonly output: JsonRecord;
  readonly message: string;
}): AdapterResult {
  return {
    adapterId: input.invocation.adapterId,
    operation: input.invocation.operation,
    operationVersion: input.invocation.operationVersion,
    status: "succeeded",
    output: input.output,
    providerMetadata: providerMetadata(input.invocation, input.provider, input.providerResponseId),
    auditEvents: [auditEvent(input.providerResponseId, input.message)]
  };
}

function providerMetadata(
  invocation: AdapterInvocation,
  provider: AdapterKind,
  providerResponseId: string
): AdapterProviderMetadata {
  return {
    adapterId: invocation.adapterId,
    provider,
    providerResponseId,
    mock: false,
    sequence: 1,
    operation: invocation.operation
  };
}

function auditEvent(providerResponseId: string, message: string): AdapterAuditEvent {
  return {
    id: `audit.${providerResponseId}`,
    timestamp: new Date().toISOString(),
    level: "info",
    message
  };
}

function requireMetadata(
  metadata: ReadonlyMap<string, AdapterMetadata>,
  id: string
): AdapterMetadata {
  const adapter = metadata.get(id);
  if (!adapter) {
    throw new Error(`Live adapter metadata '${id}' is missing.`);
  }

  return adapter;
}

function parseSecretJson(value: string): JsonRecord {
  try {
    const parsed: unknown = JSON.parse(value);
    return isRecord(parsed) ? parsed : {};
  } catch {
    return {};
  }
}

function receiptFromMessage(message: GmailMessage): JsonRecord {
  const headers = message.payload?.headers ?? [];
  const subject = headerValue(headers, "Subject") ?? "";
  const date = headerValue(headers, "Date");
  const text = `${subject}\n${message.snippet ?? ""}\n${decodeBody(message.payload)}`;
  const totalMatch = text.match(
    /(?:total|amount|paid)[^\d$€£¥]*(?:[$€£¥])?\s*(\d+(?:[.,]\d{2})?)/iu
  );
  const currencyMatch = text.match(/\b(USD|EUR|GBP|SGD|JPY|AUD|CAD)\b/u) ?? text.match(/([$€£¥])/u);
  return {
    messageId: message.id,
    threadId: message.threadId,
    receivedAt: message.internalDate
      ? new Date(Number(message.internalDate)).toISOString()
      : date
        ? new Date(date).toISOString()
        : new Date().toISOString(),
    merchant: merchantFromSubject(subject),
    total: totalMatch ? Number(totalMatch[1]?.replace(",", ".")) : null,
    currency: currencyFromMatch(currencyMatch?.[1]),
    subject,
    snippet: message.snippet ?? ""
  };
}

function merchantFromSubject(subject: string): string {
  return (
    subject
      .replace(/\b(your|receipt|order|invoice|confirmation|from|for)\b/giu, " ")
      .replace(/[^a-z0-9&'. -]+/giu, " ")
      .trim()
      .replace(/\s+/gu, " ")
      .slice(0, 80) || "Unknown merchant"
  );
}

function currencyFromMatch(value: string | undefined): string | null {
  if (!value) {
    return null;
  }
  if (value === "$") return "USD";
  if (value === "€") return "EUR";
  if (value === "£") return "GBP";
  if (value === "¥") return "JPY";
  return value.toUpperCase();
}

function headerValue(headers: readonly GmailHeader[], name: string): string | undefined {
  return headers.find((header) => header.name.toLowerCase() === name.toLowerCase())?.value;
}

function decodeBody(part: GmailMessagePart | undefined): string {
  if (!part) {
    return "";
  }
  const direct = part.body?.data ? base64UrlDecode(part.body.data) : "";
  const nested = (part.parts ?? []).map(decodeBody).join("\n");
  return `${direct}\n${nested}`.trim();
}

function base64UrlDecode(value: string): string {
  return Buffer.from(value.replace(/-/gu, "+").replace(/_/gu, "/"), "base64").toString("utf8");
}

function rowsToValues(
  rows: readonly JsonRecord[],
  columnsInput: JsonValue | undefined
): JsonValue[][] {
  const columns = Array.isArray(columnsInput)
    ? columnsInput.filter((column): column is string => typeof column === "string")
    : stableColumns(rows);
  return rows.map((row) => columns.map((column) => row[column] ?? ""));
}

function stableColumns(rows: readonly JsonRecord[]): readonly string[] {
  return [...new Set(rows.flatMap((row) => Object.keys(row)))].sort();
}

function valuesToObjects(values: readonly JsonValue[][]): JsonRecord[] {
  const [headers, ...rows] = values;
  const keys =
    headers?.map((header, index) =>
      typeof header === "string" ? header : `column_${index + 1}`
    ) ?? [];
  return rows.map((row) => Object.fromEntries(keys.map((key, index) => [key, row[index] ?? ""])));
}

function smtpConfig(
  invocation: AdapterInvocation,
  options: SmtpTransportOptions
): ResolvedSmtpTransportOptions {
  const secret = parseSecretJson(requiredSecret(invocation, "email.delivery"));
  const host = stringValue(secret.host, options.host ?? process.env.SMTP_HOST ?? "");
  if (!host) {
    throw new Error("SMTP host is required.");
  }
  return {
    host,
    port: numberValue(secret.port, options.port ?? Number(process.env.SMTP_PORT ?? 587)),
    secure: booleanValue(secret.secure, options.secure ?? process.env.SMTP_SECURE === "true"),
    username: stringValue(secret.username, options.username ?? process.env.SMTP_USERNAME ?? ""),
    password: stringValue(secret.password, options.password ?? process.env.SMTP_PASSWORD ?? ""),
    from: stringValue(secret.from, options.from ?? process.env.SMTP_FROM ?? "kelpclaw@localhost")
  };
}

async function sendSmtp(
  input: ResolvedSmtpTransportOptions & {
    readonly to: string;
    readonly subject: string;
    readonly body: string;
    readonly messageId: string;
  }
): Promise<void> {
  const socket = await openSmtpSocket(input);
  const session = new SmtpSession(socket);
  try {
    await session.expect(220);
    await session.command(`EHLO localhost`, 250);
    if (input.username && input.password) {
      const auth = Buffer.from(`\0${input.username}\0${input.password}`, "utf8").toString("base64");
      await session.command(`AUTH PLAIN ${auth}`, 235);
    }
    await session.command(`MAIL FROM:<${input.from}>`, 250);
    await session.command(`RCPT TO:<${input.to}>`, [250, 251]);
    await session.command("DATA", 354);
    await session.writeData(
      [
        `Message-ID: ${input.messageId}`,
        `From: ${input.from}`,
        `To: ${input.to}`,
        `Subject: ${input.subject}`,
        "Content-Type: text/plain; charset=utf-8",
        "",
        input.body
      ].join("\r\n")
    );
    await session.expect(250);
    await session.command("QUIT", 221);
  } finally {
    socket.end();
  }
}

function openSmtpSocket(input: ResolvedSmtpTransportOptions): Promise<Socket | TLSSocket> {
  return new Promise((resolve, reject) => {
    const socket = input.secure
      ? connectTls({ host: input.host, port: input.port })
      : connectTcp({ host: input.host, port: input.port });
    socket.once("connect", () => resolve(socket));
    socket.once("error", reject);
  });
}

class SmtpSession {
  private buffer = "";
  private readonly waiters: Array<(line: string) => void> = [];

  public constructor(private readonly socket: Socket | TLSSocket) {
    socket.on("data", (chunk: Buffer) => {
      this.buffer += chunk.toString("utf8");
      this.flush();
    });
  }

  public async command(command: string, expected: number | readonly number[]): Promise<void> {
    this.socket.write(`${command}\r\n`);
    await this.expect(expected);
  }

  public async writeData(data: string): Promise<void> {
    this.socket.write(`${data.replace(/\r?\n\./gu, "\r\n..")}\r\n.\r\n`);
  }

  public async expect(expected: number | readonly number[]): Promise<void> {
    const codes = Array.isArray(expected) ? expected : [expected];
    const line = await this.readLine();
    const code = Number(line.slice(0, 3));
    if (!codes.includes(code)) {
      throw new Error(`SMTP expected ${codes.join("/")} but received '${line}'.`);
    }
  }

  private readLine(): Promise<string> {
    return new Promise((resolve) => {
      this.waiters.push(resolve);
      this.flush();
    });
  }

  private flush(): void {
    const terminal = this.buffer.match(/(?:^|\r\n)(\d{3}) (.*)\r\n/u);
    if (!terminal || this.waiters.length === 0) {
      return;
    }
    const line = terminal[0].trim();
    this.buffer = this.buffer.slice(this.buffer.indexOf(terminal[0]) + terminal[0].length);
    this.waiters.shift()?.(line);
  }
}

function stableBody(value: JsonValue | undefined): string {
  return value === undefined ? "KelpClaw workflow completed." : JSON.stringify(value, null, 2);
}

function requiredString(value: JsonValue | undefined, field: string): string {
  if (typeof value !== "string" || value.length === 0) {
    throw new Error(`Payload field '${field}' is required.`);
  }

  return value;
}

function stringValue(value: JsonValue | undefined, fallback: string): string {
  return typeof value === "string" ? value : fallback;
}

function numberValue(value: JsonValue | undefined, fallback: number): number {
  return typeof value === "number" ? value : fallback;
}

function booleanValue(value: JsonValue | undefined, fallback: boolean): boolean {
  return typeof value === "boolean" ? value : fallback;
}

function arrayValue(value: JsonValue | undefined): JsonValue[] {
  return Array.isArray(value) ? value : [];
}

function isRecord(value: unknown): value is JsonRecord {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

interface GoogleTokenResponse {
  readonly access_token?: string;
}

interface GmailListResponse {
  readonly messages?: readonly { readonly id: string; readonly threadId: string }[];
  readonly nextPageToken?: string;
  readonly resultSizeEstimate?: number;
}

interface GmailHeader {
  readonly name: string;
  readonly value: string;
}

interface GmailMessagePart {
  readonly headers?: readonly GmailHeader[];
  readonly body?: { readonly data?: string };
  readonly parts?: readonly GmailMessagePart[];
}

interface GmailMessage {
  readonly id: string;
  readonly threadId: string;
  readonly snippet?: string;
  readonly internalDate?: string;
  readonly payload?: GmailMessagePart;
}

interface SheetsAppendResponse {
  readonly updates?: {
    readonly updatedRange?: string;
    readonly updatedRows?: number;
  };
}

interface SheetsValuesResponse {
  readonly range?: string;
  readonly values?: readonly JsonValue[][];
  readonly updatedRange?: string;
  readonly updatedRows?: number;
}

interface WhatsAppMessageResponse {
  readonly messages?: readonly { readonly id: string }[];
}

interface TelegramSendMessageResponse {
  readonly ok: boolean;
  readonly result?: {
    readonly message_id?: number;
  };
}
