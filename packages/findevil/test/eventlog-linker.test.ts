import { mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { linkEvidence } from "../src/linker/index.js";
import {
  matchEventLogLogon,
  matchEventLogProcessCreate,
  matchEventLogScheduledTask,
  matchEventLogServiceInstall,
  parseEvtxJson
} from "../src/linker/eventlog.js";
import type { Claim } from "../src/types/claim.js";

describe("event log linker", () => {
  it("parses Security Event Log JSON and matches process, logon, and task events", async () => {
    const file = await writeFixture("security.evtx.json", securityEvents());
    const records = parseEvtxJson(file, "security.evtx.json");

    expect(records.map((record) => record.eventId)).toEqual([4688, 4624, 4625, 4698, 4702]);

    expect(matchEventLogProcessCreate(claim("evil.exe executed"), records)).toMatchObject([
      {
        artifact: "security.evtx.json",
        locator: "evtx:channel=Security:record=1001",
        supports: "security_4688_process_create"
      }
    ]);
    expect(matchEventLogProcessCreate(claim("notepad.exe executed"), records)).toEqual([]);

    expect(
      matchEventLogLogon(claim("LAB\\analyst01 logged on from 10.0.0.5"), records)
    ).toMatchObject([
      {
        locator: "evtx:channel=Security:record=1002",
        supports: "security_4624_type_3"
      }
    ]);
    expect(matchEventLogLogon(claim("failed logon for LAB\\svc-backup"), records)).toMatchObject([
      {
        locator: "evtx:channel=Security:record=1003",
        supports: "security_4625_logon"
      }
    ]);

    expect(
      matchEventLogScheduledTask(
        claim("DailyUpdater scheduled task launched taskdrop.exe"),
        records
      )
    ).toMatchObject([
      {
        locator: "evtx:channel=Security:record=1004",
        supports: "security_4698_scheduled_task"
      },
      {
        locator: "evtx:channel=Security:record=1005",
        supports: "security_4702_scheduled_task"
      }
    ]);
  });

  it("parses System Event Log JSON and matches service installs", async () => {
    const file = await writeFixture("system.evtx.json", systemEvents());
    const records = parseEvtxJson(file, "system.evtx.json");

    expect(records.map((record) => record.eventId)).toEqual([7045]);
    expect(
      matchEventLogServiceInstall(claim("EvilUpdater service installed svc.exe"), records)
    ).toMatchObject([
      {
        artifact: "system.evtx.json",
        locator: "evtx:channel=System:record=2001",
        supports: "system_7045_service_create"
      }
    ]);
  });

  it("links Event Log evidence through execution and persistence dispatch", async () => {
    const caseDir = await mkdtemp(join(tmpdir(), "findevil-eventlog-linker-"));
    await writeFile(join(caseDir, "security.evtx.json"), JSON.stringify(securityEvents()), "utf8");
    await writeFile(join(caseDir, "system.evtx.json"), JSON.stringify(systemEvents()), "utf8");

    const execution = linkEvidence(
      baseClaim({
        text: "evil.exe executed from C:\\Users\\Public\\evil.exe",
        type: "program_execution",
        missingEvidence: ["security_4688_process_create"]
      }),
      caseDir
    );
    expect(execution.evidenceRefs.map((ref) => ref.supports)).toContain(
      "security_4688_process_create"
    );
    expect(execution.missingEvidence).toEqual([]);

    const persistence = linkEvidence(
      baseClaim({
        text: "DailyUpdater scheduled task persisted taskdrop.exe",
        type: "persistence",
        missingEvidence: ["scheduled-task", "service-create"]
      }),
      caseDir
    );
    expect(persistence.evidenceRefs.map((ref) => ref.supports)).toEqual([
      "security_4698_scheduled_task",
      "security_4702_scheduled_task"
    ]);
    expect(persistence.missingEvidence).toEqual([]);
  });
});

async function writeFixture(name: string, records: readonly unknown[]): Promise<string> {
  const directory = await mkdtemp(join(tmpdir(), "findevil-eventlog-linker-"));
  const file = join(directory, name);
  await writeFile(file, JSON.stringify(records), "utf8");
  return file;
}

function securityEvents(): unknown[] {
  return [
    eventRecord("Security", 4688, 1001, {
      NewProcessName: "C:\\Users\\Public\\evil.exe",
      CommandLine: '"C:\\Users\\Public\\evil.exe" -q',
      ParentProcessName: "C:\\Windows\\explorer.exe",
      SubjectDomainName: "LAB",
      SubjectUserName: "analyst01"
    }),
    eventRecord("Security", 4624, 1002, {
      LogonType: "3",
      TargetDomainName: "LAB",
      TargetUserName: "analyst01",
      IpAddress: "10.0.0.5"
    }),
    eventRecord("Security", 4625, 1003, {
      TargetDomainName: "LAB",
      TargetUserName: "svc-backup",
      IpAddress: "10.0.0.6"
    }),
    eventRecord("Security", 4698, 1004, {
      TaskName: "\\DailyUpdater",
      TaskContent: "<Command>C:\\ProgramData\\Updater\\taskdrop.exe</Command>"
    }),
    eventRecord("Security", 4702, 1005, {
      TaskName: "\\DailyUpdater",
      TaskContent: "<Command>C:\\ProgramData\\Updater\\taskdrop.exe</Command>"
    })
  ];
}

function systemEvents(): unknown[] {
  return [
    eventRecord("System", 7045, 2001, {
      ServiceName: "EvilUpdater",
      ServiceFileName: "C:\\ProgramData\\Updater\\svc.exe",
      ServiceType: "user mode service",
      StartType: "auto start"
    })
  ];
}

function eventRecord(
  channel: "Security" | "System",
  eventId: number,
  recordId: number,
  data: Record<string, string>
): unknown {
  return {
    Event: {
      System: {
        Provider: { Name: "Microsoft-Windows-Security-Auditing" },
        EventID: eventId,
        EventRecordID: recordId,
        Channel: channel,
        TimeCreated: { SystemTime: "2026-05-30T00:00:00.000Z" }
      },
      EventData: {
        Data: Object.entries(data).map(([Name, value]) => ({ Name, "#text": value }))
      }
    }
  };
}

function claim(text: string): Pick<Claim, "text"> {
  return { text };
}

function baseClaim(overrides: Partial<Claim>): Claim {
  return {
    id: "claim-eventlog",
    text: "eventlog claim",
    type: "program_execution",
    severity: "high",
    status: "unverifiable",
    confidence: 0.5,
    evidenceRefs: [],
    missingEvidence: [],
    ...overrides
  };
}
