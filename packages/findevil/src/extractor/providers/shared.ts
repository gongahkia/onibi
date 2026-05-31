import {
  claimExtractorSystemPrompt,
  claimExtractorToolName,
  claimLedgerJsonSchema
} from "../prompts.js";

export type RawClaimsJson = unknown;

export interface ResponsesClient {
  readonly responses: {
    create(input: Record<string, unknown>): Promise<unknown>;
  };
}

export function anthropicMessageRequest(prompt: string, model: string): Record<string, unknown> {
  return {
    model,
    max_tokens: 4096,
    temperature: 0,
    system: claimExtractorSystemPrompt,
    messages: [
      {
        role: "user",
        content: prompt
      }
    ],
    tools: [
      {
        name: claimExtractorToolName,
        description: "Emit the extracted KelpClaw Find Evil claim ledger.",
        input_schema: claimLedgerJsonSchema
      }
    ],
    tool_choice: {
      type: "tool",
      name: claimExtractorToolName
    }
  };
}

export function openAiResponsesRequest(prompt: string, model: string): Record<string, unknown> {
  return {
    model,
    instructions: claimExtractorSystemPrompt,
    input: prompt,
    text: {
      format: {
        type: "json_schema",
        name: claimExtractorToolName,
        strict: true,
        schema: claimLedgerJsonSchema
      }
    },
    store: false,
    tools: []
  };
}

export function geminiGenerateContentRequest(prompt: string): Record<string, unknown> {
  return {
    systemInstruction: {
      parts: [{ text: claimExtractorSystemPrompt }]
    },
    contents: [
      {
        role: "user",
        parts: [{ text: prompt }]
      }
    ],
    generationConfig: {
      temperature: 0,
      maxOutputTokens: 4096,
      responseMimeType: "application/json",
      responseJsonSchema: claimLedgerJsonSchema
    }
  };
}

export function parseAnthropicClaims(response: unknown): RawClaimsJson {
  const content = recordValue(response).content;
  if (Array.isArray(content)) {
    const toolUse = content.find(
      (block) =>
        isRecord(block) &&
        block.type === "tool_use" &&
        block.name === claimExtractorToolName &&
        "input" in block
    );
    if (isRecord(toolUse)) {
      return toolUse.input;
    }
    const text = content
      .filter(
        (block): block is { readonly text: string } =>
          isRecord(block) && typeof block.text === "string"
      )
      .map((block) => block.text)
      .join("\n")
      .trim();
    if (text.length > 0) {
      return parseJsonText(text);
    }
  }
  return response;
}

export function parseOpenAiClaims(response: unknown): RawClaimsJson {
  const parsed = parsedOpenAiOutput(response);
  if (parsed !== undefined) {
    return parsed;
  }
  const text = openAiOutputText(response);
  if (text) {
    return parseJsonText(text);
  }
  return response;
}

export function parseGeminiClaims(response: unknown): RawClaimsJson {
  const text = geminiOutputText(response);
  if (text) {
    return parseJsonText(text);
  }
  return response;
}

export function requireEnv(name: string, provider: string): string {
  const value = stringEnv(name);
  if (!value) {
    throw new Error(`${name} is required for ${provider} claim extraction.`);
  }
  return value;
}

export function stringEnv(name: string): string | undefined {
  const value = process.env[name]?.trim();
  return value ? value : undefined;
}

export function trimTrailingSlashes(value: string): string {
  return value.replace(/\/+$/u, "");
}

function parsedOpenAiOutput(response: unknown): unknown {
  const items = outputContentItems(recordValue(response).output);
  for (const item of items) {
    if ("parsed" in item && item.parsed !== undefined) {
      return item.parsed;
    }
  }
  return undefined;
}

function openAiOutputText(response: unknown): string | undefined {
  const direct = recordValue(response).output_text;
  if (typeof direct === "string" && direct.trim().length > 0) {
    return direct.trim();
  }
  const text = outputContentItems(recordValue(response).output)
    .map((item) => (typeof item.text === "string" ? item.text : ""))
    .filter((value) => value.length > 0)
    .join("\n")
    .trim();
  return text.length > 0 ? text : undefined;
}

function outputContentItems(output: unknown): readonly Record<string, unknown>[] {
  if (!Array.isArray(output)) {
    return [];
  }
  return output.flatMap((item) => {
    if (!isRecord(item)) {
      return [];
    }
    const content = item.content;
    return Array.isArray(content) ? content.filter(isRecord) : [];
  });
}

function geminiOutputText(response: unknown): string | undefined {
  const candidates = recordValue(response).candidates;
  if (!Array.isArray(candidates)) {
    return undefined;
  }
  const text = candidates
    .flatMap((candidate) => {
      const parts = recordValue(recordValue(candidate).content).parts;
      return Array.isArray(parts) ? parts : [];
    })
    .map((part) => (isRecord(part) && typeof part.text === "string" ? part.text : ""))
    .filter((value) => value.length > 0)
    .join("\n")
    .trim();
  return text.length > 0 ? text : undefined;
}

function parseJsonText(text: string): RawClaimsJson {
  return JSON.parse(text.trim()) as RawClaimsJson;
}

function recordValue(input: unknown): Record<string, unknown> {
  return isRecord(input) ? input : {};
}

function isRecord(input: unknown): input is Record<string, unknown> {
  return typeof input === "object" && input !== null && !Array.isArray(input);
}
