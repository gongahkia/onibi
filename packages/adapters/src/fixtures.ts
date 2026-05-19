import type { JsonRecord } from "@kelpclaw/workflow-spec";

export const gmailReceiptSearchInputFixture: JsonRecord = {
  query: "from:(receipts OR orders) newer_than:30d",
  maxResults: 25
};

export const gmailReceiptPayloadFixture: JsonRecord = {
  receipts: [
    {
      messageId: "gmail-msg-001",
      threadId: "gmail-thread-001",
      receivedAt: "2026-05-18T03:12:00.000Z",
      merchant: "Tidepool Market",
      total: 42.18,
      currency: "USD",
      subject: "Your Tidepool Market receipt"
    },
    {
      messageId: "gmail-msg-002",
      threadId: "gmail-thread-002",
      receivedAt: "2026-05-18T08:45:00.000Z",
      merchant: "Harbor Books",
      total: 18.4,
      currency: "USD",
      subject: "Receipt for your Harbor Books order"
    }
  ]
};

export const sheetsReceiptRowsFixture: JsonRecord = {
  spreadsheetId: "sheet.receipts",
  range: "Receipts!A:D",
  rows: [
    {
      date: "2026-05-18",
      merchant: "Tidepool Market",
      total: 42.18,
      currency: "USD"
    },
    {
      date: "2026-05-18",
      merchant: "Harbor Books",
      total: 18.4,
      currency: "USD"
    }
  ]
};

export const emailResultDeliveryFixture: JsonRecord = {
  to: "owner@example.com",
  subject: "Receipt sync completed",
  body: "2 receipt rows appended to Receipts!A:D.",
  summary: {
    appendedRows: 2,
    spreadsheetId: "sheet.receipts"
  }
};

export const receiptExtractionToSheetsFixture: JsonRecord = {
  gmail: {
    input: gmailReceiptSearchInputFixture,
    output: gmailReceiptPayloadFixture
  },
  sheets: {
    input: sheetsReceiptRowsFixture,
    output: {
      spreadsheetId: "sheet.receipts",
      range: "Receipts!A:D",
      appendedRows: 2
    }
  },
  email: {
    input: emailResultDeliveryFixture,
    output: {
      delivered: true,
      channel: "email"
    }
  }
};
