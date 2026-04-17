import { describe, expect, it } from "vitest";
import {
  appendChunk,
  mergeBufferSnapshot,
  replaceBuffer,
  renderOutput,
  renderOutputPreview,
  type OutputBySession
} from "../src/store/sessionStore";
import type { SessionOutputChunk } from "../src/types";

function chunk(id: string, sessionId: string, text: string): SessionOutputChunk {
  return {
    id,
    sessionId,
    stream: "stdout",
    timestamp: new Date().toISOString(),
    data: Buffer.from(text, "utf8").toString("base64")
  };
}

function chunkWithBase64Data(id: string, sessionId: string, base64Data: string): SessionOutputChunk {
  return {
    id,
    sessionId,
    stream: "stdout",
    timestamp: new Date().toISOString(),
    data: base64Data
  };
}

describe("sessionStore", () => {
  it("deduplicates output chunks by chunk id", () => {
    const first = chunk("c1", "s1", "pwd\n");
    let outputBySession: OutputBySession = {};
    outputBySession = appendChunk(outputBySession, first);
    outputBySession = appendChunk(outputBySession, first);

    expect(outputBySession.s1).toHaveLength(1);
    expect(outputBySession.s1?.[0]?.id).toBe("c1");
  });

  it("merges snapshot with newer local tail using cursor", () => {
    const existingSnapshot = [chunk("c1", "s1", "a"), chunk("c2", "s1", "b"), chunk("c3", "s1", "c")];
    let outputBySession: OutputBySession = {};
    outputBySession = replaceBuffer(outputBySession, "s1", existingSnapshot);

    const incomingSnapshot = [chunk("c1", "s1", "a"), chunk("c2", "s1", "b")];
    const merged = mergeBufferSnapshot(outputBySession, "s1", incomingSnapshot, "c2");
    const mergedIDs = (merged.s1 ?? []).map((entry) => entry.id);

    expect(mergedIDs).toEqual(["c1", "c2", "c3"]);
  });

  it("ignores stale snapshot replacement when cursor already matches current tail", () => {
    const existingSnapshot = [chunk("c1", "s1", "a"), chunk("c2", "s1", "b")];
    let outputBySession: OutputBySession = {};
    outputBySession = replaceBuffer(outputBySession, "s1", existingSnapshot);

    const incomingSnapshot = [chunk("c0", "s1", "z")];
    const merged = mergeBufferSnapshot(outputBySession, "s1", incomingSnapshot, "c2");

    expect((merged.s1 ?? []).map((entry) => entry.id)).toEqual(["c1", "c2"]);
  });

  it("decodes split utf-8 sequences across chunk boundaries", () => {
    const fireBytes = Buffer.from("🔥", "utf8");
    const head = fireBytes.subarray(0, 2).toString("base64");
    const tail = fireBytes.subarray(2).toString("base64");

    let outputBySession: OutputBySession = {};
    outputBySession = appendChunk(outputBySession, chunkWithBase64Data("c1", "s1", head));
    outputBySession = appendChunk(outputBySession, chunkWithBase64Data("c2", "s1", tail));

    expect(renderOutput(outputBySession.s1 ?? [])).toBe("🔥");
  });

  it("renders sanitized output preview text", () => {
    const chunks = [
      chunk("c1", "s1", "\u001b[31mERROR\u001b[0m "),
      chunk("c2", "s1", "build failed at step 3")
    ];
    const outputBySession = replaceBuffer({}, "s1", chunks);
    const preview = renderOutputPreview(outputBySession.s1 ?? []);

    expect(preview).toBe("ERROR build failed at step 3");
  });
});
