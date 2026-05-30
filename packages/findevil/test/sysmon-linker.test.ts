import { describe, expect, it } from "vitest";
import { matchSysmonProcessCreate, parseSysmonJson } from "../src/linker/sysmon.js";

describe("sysmon linker", () => {
  it("returns one execution ref when the claim mentions the matching Image", () => {
    const events = parseSysmonJson(inlineProcessCreateSysmon());
    const refs = matchSysmonProcessCreate({ text: "evil.exe executed from Downloads" }, events);

    expect(refs).toHaveLength(1);
    expect(refs[0]).toMatchObject({
      artifact: "sysmon.json",
      locator: "sysmon:eventid=1:record=103",
      supports: "sysmon_process_create"
    });
  });

  it("returns no execution refs when the claim mentions an unrelated Image", () => {
    const events = parseSysmonJson(inlineProcessCreateSysmon());
    const refs = matchSysmonProcessCreate({ text: "rundll32.exe executed" }, events);

    expect(refs).toHaveLength(0);
  });

  it("does not treat Event IDs 11 or 13 as execution refs", () => {
    const events = parseSysmonJson(inlineFileAndRegistrySysmon());
    const refs = matchSysmonProcessCreate({ text: "evil.exe executed" }, events);

    expect(refs).toHaveLength(0);
  });
});

function inlineProcessCreateSysmon(): string {
  return JSON.stringify([
    processCreate(101, "C:\\Windows\\System32\\cmd.exe"),
    processCreate(102, "C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe"),
    processCreate(103, "C:\\Users\\Public\\Downloads\\evil.exe"),
    processCreate(104, "C:\\Windows\\System32\\whoami.exe"),
    processCreate(105, "C:\\Windows\\System32\\notepad.exe")
  ]);
}

function inlineFileAndRegistrySysmon(): string {
  return JSON.stringify([
    {
      EventID: 11,
      EventRecordID: 201,
      Image: "C:\\Users\\Public\\Downloads\\evil.exe",
      TargetFilename: "C:\\Users\\Public\\Downloads\\payload.dat"
    },
    {
      EventID: 13,
      EventRecordID: 202,
      Image: "C:\\Users\\Public\\Downloads\\evil.exe",
      TargetObject: "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Run\\Updater",
      Details: "C:\\Users\\Public\\Downloads\\evil.exe"
    }
  ]);
}

function processCreate(record: number, image: string): Record<string, string | number> {
  return {
    EventID: 1,
    EventRecordID: record,
    UtcTime: "2026-05-30 10:00:00.000",
    Image: image,
    CommandLine: `"${image}"`
  };
}
