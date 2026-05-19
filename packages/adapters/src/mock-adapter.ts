import { createHash } from "node:crypto";
import { redactJsonRecord, redactedValue, stableJsonStringify } from "@kelpclaw/workflow-spec";
import {
  emailResultDeliveryFixture,
  gmailReceiptPayloadFixture,
  sheetsReceiptRowsFixture
} from "./fixtures.js";
import { assertAdapterCredentialRefs } from "./credentials.js";
import type {
  Adapter,
  AdapterInvocation,
  AdapterMetadata,
  AdapterResult,
  RecordedAdapterInvocation
} from "./types.js";
import type { JsonRecord, JsonValue } from "@kelpclaw/workflow-spec";

export class MockAdapter implements Adapter {
  readonly metadata: AdapterMetadata;
  readonly invocations: RecordedAdapterInvocation[] = [];

  public constructor(metadata: AdapterMetadata) {
    this.metadata = metadata;
  }

  public async invoke(invocation: AdapterInvocation): Promise<AdapterResult> {
    if (invocation.adapterId !== this.metadata.id) {
      throw new Error(
        `Invocation targeted adapter '${invocation.adapterId}' but mock adapter is '${this.metadata.id}'.`
      );
    }

    const operation = this.metadata.operations.find(
      (candidate) =>
        candidate.name === invocation.operation && candidate.version === invocation.operationVersion
    );
    if (!operation) {
      throw new Error(
        `Adapter '${this.metadata.id}' does not support operation '${invocation.operation}' version '${invocation.operationVersion}'.`
      );
    }

    assertAdapterCredentialRefs(this.metadata, invocation.secretRefs);

    const recorded: RecordedAdapterInvocation = {
      ...invocation,
      payload: redactJsonRecord(invocation.payload, {
        secretRefs: Object.values(invocation.secretRefs)
      }),
      secretRefs: redactSecretRefs(invocation.secretRefs),
      sequence: this.invocations.length + 1
    };
    this.invocations.push(recorded);

    const providerResponseId = deterministicResponseId(invocation);
    const output = createMockOperationOutput(invocation, providerResponseId);
    const timestamp = "2026-05-18T00:00:00.000Z";

    return {
      adapterId: invocation.adapterId,
      operation: invocation.operation,
      operationVersion: invocation.operationVersion,
      status: "succeeded",
      output,
      providerMetadata: {
        adapterId: invocation.adapterId,
        provider: this.metadata.kind,
        providerResponseId,
        mock: true,
        sequence: recorded.sequence,
        operation: invocation.operation
      },
      auditEvents: [
        {
          id: `audit.${providerResponseId}`,
          timestamp,
          level: "info",
          message: `Mock adapter '${this.metadata.id}' recorded '${invocation.operation}'.`
        }
      ]
    };
  }
}

function redactSecretRefs(
  secretRefs: Readonly<Record<string, string>>
): Readonly<Record<string, string>> {
  return Object.fromEntries(Object.keys(secretRefs).map((key) => [key, redactedValue]));
}

export function createMockAdapter(metadata: AdapterMetadata): MockAdapter {
  return new MockAdapter(metadata);
}

export const FakeAdapter = MockAdapter;
export const createFakeAdapter = createMockAdapter;

function deterministicResponseId(invocation: AdapterInvocation): string {
  const idempotencyKey =
    invocation.idempotencyKey ??
    stableJsonStringify({
      adapterId: invocation.adapterId,
      operation: invocation.operation,
      operationVersion: invocation.operationVersion,
      payload: invocation.payload,
      context: {
        workflowId: invocation.context.workflowId,
        nodeId: invocation.context.nodeId,
        runId: invocation.context.runId,
        attempt: invocation.context.attempt
      }
    } as JsonRecord);
  const digest = createHash("sha256").update(idempotencyKey, "utf8").digest("hex").slice(0, 16);

  return `mock.${sanitize(invocation.adapterId)}.${sanitize(invocation.operation)}.${digest}`;
}

function createMockOperationOutput(
  invocation: AdapterInvocation,
  providerResponseId: string
): JsonRecord {
  switch (invocation.operation) {
    case "gmail.trigger.poll":
      return {
        messages: arrayOrDefault(gmailReceiptPayloadFixture.receipts, []),
        providerResponseId
      };
    case "gmail.receipts.search":
      return {
        ...gmailReceiptPayloadFixture,
        query: stringOrDefault(invocation.payload.query, ""),
        providerResponseId
      };
    case "sheets.rows.append": {
      const rows = arrayOrDefault(invocation.payload.rows, sheetsReceiptRowsFixture.rows);
      return {
        spreadsheetId: stringOrDefault(
          invocation.payload.spreadsheetId,
          stringOrDefault(sheetsReceiptRowsFixture.spreadsheetId, "sheet.receipts")
        ),
        range: stringOrDefault(
          invocation.payload.range,
          stringOrDefault(sheetsReceiptRowsFixture.range, "Sheet1!A:Z")
        ),
        appendedRows: rows.length,
        rows,
        providerResponseId
      };
    }
    case "sheets.rows.update":
      return {
        updatedRows: arrayOrDefault(invocation.payload.rows, []).length,
        providerResponseId
      };
    case "sheets.rows.lookup":
      return {
        rows: arrayOrDefault(invocation.payload.rows, sheetsReceiptRowsFixture.rows),
        providerResponseId
      };
    case "email.approval.request":
      return {
        approvalRequestId: providerResponseId,
        channel: "email",
        delivered: true,
        providerResponseId
      };
    case "email.results.send":
      return {
        messageId: providerResponseId,
        channel: "email",
        delivered: true,
        to: stringOrDefault(
          invocation.payload.to,
          stringOrDefault(emailResultDeliveryFixture.to, "")
        ),
        providerResponseId
      };
    case "whatsapp.alert.send":
      return {
        messageId: providerResponseId,
        channel: "whatsapp",
        delivered: true,
        providerResponseId
      };
    case "telegram.alert.send":
      return {
        messageId: providerResponseId,
        channel: "telegram",
        delivered: true,
        providerResponseId
      };
    default:
      return {
        recorded: true,
        providerResponseId
      };
  }
}

function stringOrDefault(value: JsonValue | undefined, fallback: string): string {
  return typeof value === "string" ? value : fallback;
}

function arrayOrDefault(
  value: JsonValue | undefined,
  fallback: JsonValue | undefined
): JsonValue[] {
  if (Array.isArray(value)) {
    return value;
  }
  if (Array.isArray(fallback)) {
    return fallback;
  }

  return [];
}

function sanitize(value: string): string {
  return value.replace(/[^a-z0-9]+/giu, "-").replace(/^-+|-+$/gu, "");
}
