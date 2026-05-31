import { readdir, readFile } from "node:fs/promises";
import { dirname, join, relative, sep } from "node:path";
import { fileURLToPath } from "node:url";
import { type JsonRecord } from "@kelpclaw/workflow-spec";
import { describe, expect, it } from "vitest";
import { classifyToolCall, type FirewallDecisionValue } from "../src/firewall/index.js";
import { taintLedgerEntrySchema, type TaintLedgerEntry } from "../src/types/taint.js";

interface CorpusFixture {
  readonly category: string;
  readonly expected: FirewallDecisionValue;
  readonly path: string;
  readonly payload: string;
}

interface CategorySummary {
  readonly category: string;
  total: number;
  blocked: number;
  falsePositives: number;
  falseNegatives: number;
}

const corpusRoot = join(
  dirname(fileURLToPath(import.meta.url)),
  "..",
  "..",
  "..",
  "fixtures",
  "adversarial-injections"
);
const sha256 = `sha256:${"e".repeat(64)}`;

describe("firewall adversarial corpus", () => {
  it("matches expected block/allow outcomes and emits category coverage", async () => {
    const fixtures = await readCorpus();
    expect(fixtures.length).toBeGreaterThanOrEqual(50);

    const summaries = new Map<string, CategorySummary>();
    let falsePositives = 0;
    let falseNegatives = 0;

    for (const fixture of fixtures) {
      const decision = classifyToolCall(argsFor(fixture), [entry(fixture)]);
      const summary = summaryFor(summaries, fixture.category);
      summary.total += 1;
      if (decision.decision === "block") {
        summary.blocked += 1;
      }
      if (fixture.expected === "allow" && decision.decision === "block") {
        falsePositives += 1;
        summary.falsePositives += 1;
      }
      if (fixture.expected === "block" && decision.decision === "allow") {
        falseNegatives += 1;
        summary.falseNegatives += 1;
      }

      expect(decision.decision, `${fixture.path}: ${decision.reason}`).toBe(fixture.expected);
    }

    process.stdout.write(
      `${formatCoverageSummary([...summaries.values()], falsePositives, falseNegatives)}\n`
    );
    expect(falsePositives).toBe(0);
    expect(falseNegatives).toBe(0);
  });
});

async function readCorpus(): Promise<readonly CorpusFixture[]> {
  const files = await fixtureFiles(corpusRoot);
  return Promise.all(files.map(readFixture));
}

async function fixtureFiles(dir: string): Promise<readonly string[]> {
  const entries = await readdir(dir, { withFileTypes: true });
  const nested = await Promise.all(
    entries.map((entryInDir) => {
      const path = join(dir, entryInDir.name);
      if (entryInDir.isDirectory()) {
        return fixtureFiles(path);
      }
      return Promise.resolve(entryInDir.isFile() && entryInDir.name.endsWith(".md") ? [path] : []);
    })
  );
  return nested.flat().sort();
}

async function readFixture(path: string): Promise<CorpusFixture> {
  const raw = await readFile(path, "utf8");
  const relativePath = relative(corpusRoot, path).split(sep).join("/");
  const frontmatterMatch = /^---\r?\n(?<frontmatter>[\s\S]*?)\r?\n---\r?\n(?<body>[\s\S]*)$/u.exec(
    raw
  );
  const frontmatter = frontmatterMatch?.groups?.frontmatter;
  const body = frontmatterMatch?.groups?.body;
  if (!frontmatter || body === undefined) {
    throw new Error(`${relativePath} must use YAML frontmatter.`);
  }

  const { category, expected } = parseFrontmatter(frontmatter, relativePath);
  const [context, separator, payload, ...extra] = body.trim().split(/\r?\n/u);
  if (!context || separator !== "" || !payload || extra.length > 0) {
    throw new Error(`${relativePath} must contain one context paragraph and one payload line.`);
  }

  return { category, expected, path: relativePath, payload };
}

function parseFrontmatter(
  frontmatter: string,
  relativePath: string
): Pick<CorpusFixture, "category" | "expected"> {
  const metadata = new Map<string, string>();
  for (const line of frontmatter.split(/\r?\n/u)) {
    const separator = line.indexOf(":");
    if (separator === -1) {
      throw new Error(`${relativePath} has invalid frontmatter line: ${line}`);
    }
    metadata.set(line.slice(0, separator).trim(), line.slice(separator + 1).trim());
  }

  const category = metadata.get("category");
  const expected = metadata.get("expected");
  const categoryFromPath = relativePath.split("/")[0];
  if (!category || category !== categoryFromPath) {
    throw new Error(`${relativePath} category must match its directory.`);
  }
  if (expected !== "block" && expected !== "allow") {
    throw new Error(`${relativePath} expected must be block or allow.`);
  }
  return { category, expected };
}

function argsFor(fixture: CorpusFixture): JsonRecord {
  if (fixture.category === "legitimate-quote") {
    return { evidence: fixture.payload };
  }
  return { command: fixture.payload };
}

function entry(fixture: CorpusFixture): TaintLedgerEntry {
  return taintLedgerEntrySchema.parse({
    id: `taint-${Buffer.from(fixture.path).toString("hex").slice(0, 24)}`,
    source: {
      kind: "case_artifact",
      path: `fixtures/adversarial-injections/${fixture.path}`,
      sha256,
      locator: "payload:1"
    },
    text: fixture.payload,
    extractionTool: "adversarial-corpus",
    extractedAt: "2026-05-31T00:00:00.000Z",
    sensitivity: "case-data"
  });
}

function summaryFor(summaries: Map<string, CategorySummary>, category: string): CategorySummary {
  const existing = summaries.get(category);
  if (existing) {
    return existing;
  }
  const created = {
    category,
    total: 0,
    blocked: 0,
    falsePositives: 0,
    falseNegatives: 0
  };
  summaries.set(category, created);
  return created;
}

function formatCoverageSummary(
  summaries: readonly CategorySummary[],
  falsePositives: number,
  falseNegatives: number
): string {
  const lines = ["[firewall corpus] coverage summary"];
  for (const summary of summaries.toSorted((left, right) =>
    left.category.localeCompare(right.category)
  )) {
    const blockRate = summary.total === 0 ? 0 : summary.blocked / summary.total;
    lines.push(
      `- ${summary.category}: ${summary.blocked}/${summary.total} blocked (${blockRate.toFixed(
        3
      )}); false positives=${summary.falsePositives}; false negatives=${summary.falseNegatives}`
    );
  }
  lines.push(`- total false positives: ${falsePositives}`);
  lines.push(`- total false negatives: ${falseNegatives}`);
  return lines.join("\n");
}
