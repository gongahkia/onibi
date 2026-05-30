import { randomUUID } from "node:crypto";
import { connect as connectTcp } from "node:net";
import { connect as connectTls } from "node:tls";
import { assertAdapterCredentialRefs } from "./credentials.js";
import { builtinAdapterMetadata } from "./builtins.js";
import { DatabaseAdapter } from "./database-adapter.js";
import { HttpAdapter } from "./http-adapter.js";
export function createDefaultLiveAdapters(options = {}) {
    const metadata = new Map(builtinAdapterMetadata.map((adapter) => [adapter.id, adapter]));
    return new Map([
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
        ],
        [
            "adapter.github",
            new HttpAdapter(requireMetadata(metadata, "adapter.github"), githubRoutes(), {
                fetch: options.fetch
            })
        ],
        [
            "adapter.slack",
            new HttpAdapter(requireMetadata(metadata, "adapter.slack"), slackRoutes(), {
                fetch: options.fetch
            })
        ],
        [
            "adapter.discord",
            new HttpAdapter(requireMetadata(metadata, "adapter.discord"), discordRoutes(), {
                fetch: options.fetch
            })
        ],
        [
            "adapter.notion",
            new HttpAdapter(requireMetadata(metadata, "adapter.notion"), notionRoutes(), {
                fetch: options.fetch
            })
        ],
        [
            "adapter.linear",
            new HttpAdapter(requireMetadata(metadata, "adapter.linear"), linearRoutes(), {
                fetch: options.fetch
            })
        ],
        [
            "adapter.jira",
            new HttpAdapter(requireMetadata(metadata, "adapter.jira"), jiraRoutes(), {
                fetch: options.fetch
            })
        ],
        [
            "adapter.airtable",
            new HttpAdapter(requireMetadata(metadata, "adapter.airtable"), airtableRoutes(), {
                fetch: options.fetch
            })
        ],
        [
            "adapter.webhook",
            new HttpAdapter(requireMetadata(metadata, "adapter.webhook"), webhookRoutes(), {
                fetch: options.fetch
            })
        ],
        [
            "adapter.database",
            new DatabaseAdapter(requireMetadata(metadata, "adapter.database"), {
                client: options.database,
                sqliteBin: options.sqliteBin
            })
        ]
    ]);
}
function githubRoutes() {
    return [
        httpRoute("github.issue.create", "POST", "https://api.github.com/repos/{owner}/{repo}/issues", {
            secretName: "github.token",
            scheme: "bearer"
        }, {}, ["owner", "repo"]),
        httpRoute("github.issue.comment", "POST", "https://api.github.com/repos/{owner}/{repo}/issues/{issueNumber}/comments", {
            secretName: "github.token",
            scheme: "bearer"
        }, {}, ["owner", "repo", "issueNumber"])
    ];
}
function slackRoutes() {
    return [
        httpRoute("slack.message.send", "POST", "https://slack.com/api/chat.postMessage", {
            secretName: "slack.botToken",
            scheme: "bearer"
        })
    ];
}
function discordRoutes() {
    return [
        httpRoute("discord.message.send", "POST", "https://discord.com/api/channels/{channelId}/messages", {
            secretName: "discord.botToken",
            scheme: "bearer"
        }, {}, ["channelId"])
    ];
}
function notionRoutes() {
    return [
        httpRoute("notion.page.create", "POST", "https://api.notion.com/v1/pages", {
            secretName: "notion.apiKey",
            scheme: "bearer"
        }, {
            "Notion-Version": "2022-06-28"
        })
    ];
}
function linearRoutes() {
    return [
        httpRoute("linear.issue.create", "POST", "https://api.linear.app/graphql", {
            secretName: "linear.apiKey",
            scheme: "bearer"
        })
    ];
}
function jiraRoutes() {
    return [
        httpRoute("jira.issue.create", "POST", "https://{siteHost}/rest/api/3/issue", {
            secretName: "jira.basicAuth",
            scheme: "basic"
        }, {}, ["siteHost"])
    ];
}
function airtableRoutes() {
    return [
        httpRoute("airtable.record.create", "POST", "https://api.airtable.com/v0/{baseId}/{tableName}", {
            secretName: "airtable.apiKey",
            scheme: "bearer"
        }, {}, ["baseId", "tableName"])
    ];
}
function webhookRoutes() {
    return [
        {
            ...httpRoute("webhook.post", "POST", "", {
                secretName: "webhook.token",
                scheme: "bearer"
            }),
            bodyPayloadKey: "body",
            urlPayloadKey: "url"
        }
    ];
}
function httpRoute(operation, method, url, auth, headers = {}, pathKeys = []) {
    return {
        operation,
        version: "1.0.0",
        method,
        url,
        auth,
        pathKeys,
        headers: {
            accept: "application/json",
            "content-type": "application/json",
            ...headers
        }
    };
}
class GmailLiveAdapter {
    metadata;
    options;
    constructor(metadata, options = {}) {
        this.metadata = metadata;
        this.options = options;
    }
    async invoke(invocation) {
        assertInvocation(this.metadata, invocation);
        const accessToken = await googleAccessToken(invocation, "gmail.oauth", this.options);
        const apiBase = this.options.googleApiBaseUrl ?? "https://gmail.googleapis.com";
        const fetchImpl = this.options.fetch ?? fetch;
        const query = stringValue(invocation.payload.query, "from:(receipts OR orders)");
        const maxResults = Math.min(numberValue(invocation.payload.maxResults, 25), 100);
        const listUrl = new URL(`${apiBase}/gmail/v1/users/me/messages`);
        listUrl.searchParams.set("q", query);
        listUrl.searchParams.set("maxResults", String(maxResults));
        const listed = await readJson(fetchImpl(listUrl, authRequest(accessToken)), "GMAIL_LIST_FAILED");
        const messages = listed.messages ?? [];
        const receipts = [];
        for (const message of messages) {
            const getUrl = new URL(`${apiBase}/gmail/v1/users/me/messages/${message.id}`);
            getUrl.searchParams.set("format", "full");
            const detail = await readJson(fetchImpl(getUrl, authRequest(accessToken)), "GMAIL_GET_FAILED");
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
class SheetsLiveAdapter {
    metadata;
    options;
    constructor(metadata, options = {}) {
        this.metadata = metadata;
        this.options = options;
    }
    async invoke(invocation) {
        assertInvocation(this.metadata, invocation);
        const accessToken = await googleAccessToken(invocation, "sheets.oauth", this.options);
        const apiBase = this.options.googleApiBaseUrl ?? "https://sheets.googleapis.com";
        const fetchImpl = this.options.fetch ?? fetch;
        const spreadsheetId = requiredString(invocation.payload.spreadsheetId, "spreadsheetId");
        const range = requiredString(invocation.payload.range, "range");
        if (invocation.operation === "sheets.rows.append") {
            const rows = arrayValue(invocation.payload.rows).filter(isRecord);
            const values = rowsToValues(rows, invocation.payload.columns);
            const url = new URL(`${apiBase}/v4/spreadsheets/${encodeURIComponent(spreadsheetId)}/values/${encodeURIComponent(range)}:append`);
            url.searchParams.set("valueInputOption", "USER_ENTERED");
            url.searchParams.set("insertDataOption", "INSERT_ROWS");
            const result = await readJson(fetchImpl(url, {
                ...authRequest(accessToken),
                method: "POST",
                headers: {
                    ...authHeaders(accessToken),
                    "content-type": "application/json"
                },
                body: JSON.stringify({ values })
            }), "SHEETS_APPEND_FAILED");
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
            const url = new URL(`${apiBase}/v4/spreadsheets/${encodeURIComponent(spreadsheetId)}/values/${encodeURIComponent(range)}`);
            const result = await readJson(fetchImpl(url, authRequest(accessToken)), "SHEETS_LOOKUP_FAILED");
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
        const url = new URL(`${apiBase}/v4/spreadsheets/${encodeURIComponent(spreadsheetId)}/values/${encodeURIComponent(range)}`);
        url.searchParams.set("valueInputOption", "USER_ENTERED");
        const result = await readJson(fetchImpl(url, {
            ...authRequest(accessToken),
            method: "PUT",
            headers: {
                ...authHeaders(accessToken),
                "content-type": "application/json"
            },
            body: JSON.stringify({ values })
        }), "SHEETS_UPDATE_FAILED");
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
class SmtpEmailAdapter {
    metadata;
    options;
    constructor(metadata, options = {}) {
        this.metadata = metadata;
        this.options = options;
    }
    async invoke(invocation) {
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
class WhatsAppLiveAdapter {
    metadata;
    options;
    constructor(metadata, options = {}) {
        this.metadata = metadata;
        this.options = options;
    }
    async invoke(invocation) {
        assertInvocation(this.metadata, invocation);
        const apiKey = requiredSecret(invocation, "whatsapp.apiKey");
        const secretConfig = parseSecretJson(apiKey);
        const phoneNumberId = stringValue(invocation.payload.phoneNumberId, stringValue(secretConfig.phoneNumberId, process.env.WHATSAPP_PHONE_NUMBER_ID ?? ""));
        if (!phoneNumberId) {
            throw new Error("WHATSAPP_PHONE_NUMBER_ID or payload.phoneNumberId is required.");
        }
        const apiVersion = stringValue(invocation.payload.apiVersion, stringValue(secretConfig.apiVersion, process.env.WHATSAPP_API_VERSION ?? "v20.0"));
        const apiBase = this.options.whatsappApiBaseUrl ?? "https://graph.facebook.com";
        const fetchImpl = this.options.fetch ?? fetch;
        const to = requiredString(invocation.payload.to, "to");
        const text = stringValue(invocation.payload.text, stringValue(invocation.payload.body, ""));
        const result = await readJson(fetchImpl(`${apiBase}/${apiVersion}/${phoneNumberId}/messages`, {
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
        }), "WHATSAPP_SEND_FAILED");
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
class TelegramLiveAdapter {
    metadata;
    options;
    constructor(metadata, options = {}) {
        this.metadata = metadata;
        this.options = options;
    }
    async invoke(invocation) {
        assertInvocation(this.metadata, invocation);
        const botTokenSecret = requiredSecret(invocation, "telegram.botToken");
        const secretConfig = parseSecretJson(botTokenSecret);
        const botToken = stringValue(secretConfig.botToken, botTokenSecret);
        const chatId = stringValue(invocation.payload.chatId, stringValue(secretConfig.chatId, process.env.TELEGRAM_DEFAULT_CHAT_ID ?? ""));
        if (!chatId) {
            throw new Error("Telegram chat id is required in payload.chatId or TELEGRAM_DEFAULT_CHAT_ID.");
        }
        const text = stringValue(invocation.payload.text, stringValue(invocation.payload.body, ""));
        const apiBase = this.options.telegramApiBaseUrl ?? "https://api.telegram.org";
        const fetchImpl = this.options.fetch ?? fetch;
        const result = await readJson(fetchImpl(`${apiBase}/bot${botToken}/sendMessage`, {
            method: "POST",
            headers: {
                "content-type": "application/json"
            },
            body: JSON.stringify({
                chat_id: chatId,
                text
            })
        }), "TELEGRAM_SEND_FAILED");
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
function assertInvocation(metadata, invocation) {
    if (invocation.adapterId !== metadata.id) {
        throw new Error(`Invocation targeted adapter '${invocation.adapterId}' but adapter is '${metadata.id}'.`);
    }
    assertAdapterCredentialRefs(metadata, invocation.secretRefs, { requireLiveCredentials: true });
    const operation = metadata.operations.find((candidate) => candidate.name === invocation.operation && candidate.version === invocation.operationVersion);
    if (!operation) {
        throw new Error(`Adapter '${metadata.id}' does not support operation '${invocation.operation}' version '${invocation.operationVersion}'.`);
    }
}
async function googleAccessToken(invocation, secretName, options) {
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
        throw new Error(`${secretName} must resolve to an access token or refresh token with Google OAuth client credentials.`);
    }
    const fetchImpl = options.fetch ?? fetch;
    const tokenUrl = options.googleTokenUrl ?? "https://oauth2.googleapis.com/token";
    const body = new URLSearchParams({
        client_id: clientId,
        client_secret: clientSecret,
        refresh_token: refreshToken,
        grant_type: "refresh_token"
    });
    const response = await readJson(fetchImpl(tokenUrl, {
        method: "POST",
        headers: {
            "content-type": "application/x-www-form-urlencoded"
        },
        body
    }), "GOOGLE_TOKEN_REFRESH_FAILED");
    if (!response.access_token) {
        throw new Error("Google OAuth token response did not include an access token.");
    }
    return response.access_token;
}
function requiredSecret(invocation, secretName) {
    const value = invocation.secrets?.[secretName];
    if (!value) {
        throw new Error(`Resolved secret '${secretName}' is required for adapter '${invocation.adapterId}'.`);
    }
    return value;
}
async function readJson(responsePromise, code) {
    const response = await responsePromise;
    const text = await response.text();
    const parsed = text.length > 0 ? JSON.parse(text) : {};
    if (!response.ok) {
        const message = isRecord(parsed) && typeof parsed.error === "object" ? JSON.stringify(parsed.error) : text;
        throw new Error(`${code}: ${message || response.statusText}`);
    }
    return parsed;
}
function authHeaders(accessToken) {
    return {
        authorization: `Bearer ${accessToken}`
    };
}
function authRequest(accessToken) {
    return {
        headers: authHeaders(accessToken)
    };
}
function succeededResult(input) {
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
function providerMetadata(invocation, provider, providerResponseId) {
    return {
        adapterId: invocation.adapterId,
        provider,
        providerResponseId,
        mock: false,
        sequence: 1,
        operation: invocation.operation
    };
}
function auditEvent(providerResponseId, message) {
    return {
        id: `audit.${providerResponseId}`,
        timestamp: new Date().toISOString(),
        level: "info",
        message
    };
}
function requireMetadata(metadata, id) {
    const adapter = metadata.get(id);
    if (!adapter) {
        throw new Error(`Live adapter metadata '${id}' is missing.`);
    }
    return adapter;
}
function parseSecretJson(value) {
    try {
        const parsed = JSON.parse(value);
        return isRecord(parsed) ? parsed : {};
    }
    catch {
        return {};
    }
}
function receiptFromMessage(message) {
    const headers = message.payload?.headers ?? [];
    const subject = headerValue(headers, "Subject") ?? "";
    const date = headerValue(headers, "Date");
    const text = `${subject}\n${message.snippet ?? ""}\n${decodeBody(message.payload)}`;
    const totalMatch = text.match(/(?:total|amount|paid)[^\d$€£¥]*(?:[$€£¥])?\s*(\d+(?:[.,]\d{2})?)/iu);
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
function merchantFromSubject(subject) {
    return (subject
        .replace(/\b(your|receipt|order|invoice|confirmation|from|for)\b/giu, " ")
        .replace(/[^a-z0-9&'. -]+/giu, " ")
        .trim()
        .replace(/\s+/gu, " ")
        .slice(0, 80) || "Unknown merchant");
}
function currencyFromMatch(value) {
    if (!value) {
        return null;
    }
    if (value === "$")
        return "USD";
    if (value === "€")
        return "EUR";
    if (value === "£")
        return "GBP";
    if (value === "¥")
        return "JPY";
    return value.toUpperCase();
}
function headerValue(headers, name) {
    return headers.find((header) => header.name.toLowerCase() === name.toLowerCase())?.value;
}
function decodeBody(part) {
    if (!part) {
        return "";
    }
    const direct = part.body?.data ? base64UrlDecode(part.body.data) : "";
    const nested = (part.parts ?? []).map(decodeBody).join("\n");
    return `${direct}\n${nested}`.trim();
}
function base64UrlDecode(value) {
    return Buffer.from(value.replace(/-/gu, "+").replace(/_/gu, "/"), "base64").toString("utf8");
}
function rowsToValues(rows, columnsInput) {
    const columns = Array.isArray(columnsInput)
        ? columnsInput.filter((column) => typeof column === "string")
        : stableColumns(rows);
    return rows.map((row) => columns.map((column) => row[column] ?? ""));
}
function stableColumns(rows) {
    return [...new Set(rows.flatMap((row) => Object.keys(row)))].sort();
}
function valuesToObjects(values) {
    const [headers, ...rows] = values;
    const keys = headers?.map((header, index) => typeof header === "string" ? header : `column_${index + 1}`) ?? [];
    return rows.map((row) => Object.fromEntries(keys.map((key, index) => [key, row[index] ?? ""])));
}
function smtpConfig(invocation, options) {
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
async function sendSmtp(input) {
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
        await session.writeData([
            `Message-ID: ${input.messageId}`,
            `From: ${input.from}`,
            `To: ${input.to}`,
            `Subject: ${input.subject}`,
            "Content-Type: text/plain; charset=utf-8",
            "",
            input.body
        ].join("\r\n"));
        await session.expect(250);
        await session.command("QUIT", 221);
    }
    finally {
        socket.end();
    }
}
function openSmtpSocket(input) {
    return new Promise((resolve, reject) => {
        const socket = input.secure
            ? connectTls({ host: input.host, port: input.port })
            : connectTcp({ host: input.host, port: input.port });
        socket.once("connect", () => resolve(socket));
        socket.once("error", reject);
    });
}
class SmtpSession {
    socket;
    buffer = "";
    waiters = [];
    constructor(socket) {
        this.socket = socket;
        socket.on("data", (chunk) => {
            this.buffer += chunk.toString("utf8");
            this.flush();
        });
    }
    async command(command, expected) {
        this.socket.write(`${command}\r\n`);
        await this.expect(expected);
    }
    async writeData(data) {
        this.socket.write(`${data.replace(/\r?\n\./gu, "\r\n..")}\r\n.\r\n`);
    }
    async expect(expected) {
        const codes = Array.isArray(expected) ? expected : [expected];
        const line = await this.readLine();
        const code = Number(line.slice(0, 3));
        if (!codes.includes(code)) {
            throw new Error(`SMTP expected ${codes.join("/")} but received '${line}'.`);
        }
    }
    readLine() {
        return new Promise((resolve) => {
            this.waiters.push(resolve);
            this.flush();
        });
    }
    flush() {
        const terminal = this.buffer.match(/(?:^|\r\n)(\d{3}) (.*)\r\n/u);
        if (!terminal || this.waiters.length === 0) {
            return;
        }
        const line = terminal[0].trim();
        this.buffer = this.buffer.slice(this.buffer.indexOf(terminal[0]) + terminal[0].length);
        this.waiters.shift()?.(line);
    }
}
function stableBody(value) {
    return value === undefined ? "KelpClaw workflow completed." : JSON.stringify(value, null, 2);
}
function requiredString(value, field) {
    if (typeof value !== "string" || value.length === 0) {
        throw new Error(`Payload field '${field}' is required.`);
    }
    return value;
}
function stringValue(value, fallback) {
    return typeof value === "string" ? value : fallback;
}
function numberValue(value, fallback) {
    return typeof value === "number" ? value : fallback;
}
function booleanValue(value, fallback) {
    return typeof value === "boolean" ? value : fallback;
}
function arrayValue(value) {
    return Array.isArray(value) ? value : [];
}
function isRecord(value) {
    return typeof value === "object" && value !== null && !Array.isArray(value);
}
//# sourceMappingURL=live-adapters.js.map