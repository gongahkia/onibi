const { test } = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");
const fs = require("node:fs");
const path = require("node:path");
const source = fs.readFileSync(
  path.join(__dirname, "../internal/ghostty/control.js"),
  "utf8",
);

function bridge(request, options = {}) {
  const calls = [];
  const terminal = {
    id: () => "pane-a",
    name: () => "Agent",
    workingDirectory: () => "/work",
  };
  const created = { id: () => "pane-new" };
  const tab = {
    id: () => "tab-a",
    name: () => "Work",
    terminals: () => [terminal],
    focusedTerminal: () => terminal,
  };
  const win = {
    id: () => "window-a",
    name: () => "Mac",
    tabs: () => [tab],
    selectedTab: () => tab,
  };
  const app = {
    windows: () => (options.empty ? [] : [win]),
    terminals: () => [terminal],
    frontWindow: () => ({
      selectedTab: () => ({
        focusedTerminal: () => ({ id: () => options.focus || "pane-a" }),
      }),
    }),
    inputText: (text, to) => calls.push(["input", text, to.to.id()]),
    sendKey: (key, args) => calls.push(["key", key, args.to.id()]),
    newWindow: () => {
      calls.push(["new_window"]);
      return { selectedTab: () => ({ focusedTerminal: () => created }) };
    },
    newTab: (args) => {
      calls.push(["new_tab", args.in.id()]);
      return { focusedTerminal: () => created };
    },
    split: (target, args) => {
      calls.push(["split", target.id(), args.direction]);
      return created;
    },
    close: (target) => calls.push(["close_pane", target.id()]),
    closeTab: (target) => calls.push(["close_tab", target.id()]),
    closeWindow: (target) => calls.push(["close_window", target.id()]),
    performAction: (value, args) => {
      calls.push(["control", value, args.on.id()]);
      return options.accept !== false;
    },
  };
  const payload = JSON.stringify(request);
  const sandbox = {
    ObjC: { import() {}, unwrap: (value) => value },
    $: {
      NSFileHandle: {
        fileHandleWithStandardInput: { readDataToEndOfFile: payload },
      },
      NSString: { alloc: { initWithDataEncoding: (value) => value } },
      NSUTF8StringEncoding: 4,
      NSRunningApplication: {
        runningApplicationsWithBundleIdentifier: () => ({
          count: "1",
          objectAtIndex: () => ({
            processIdentifier: 123,
            launchDate: { timeIntervalSince1970: 42.5 },
          }),
        }),
      },
    },
    Application: () => app,
    delay() {},
  };
  vm.createContext(sandbox);
  vm.runInContext(source, sandbox);
  return { result: JSON.parse(sandbox.run()), calls };
}
const scope = { pid: 123, started: "42.5", targetId: "pane-a" };

test("native script preserves literal input and exact identity", () => {
  const text = ' "; $(never)\n<script>literal</script>';
  const { result, calls } = bridge({
    ...scope,
    operation: "input",
    text,
    enter: true,
  });
  assert.equal(result.status, "ok");
  assert.deepEqual(calls, [
    ["input", text, "pane-a"],
    ["key", "enter", "pane-a"],
  ]);
});
test("stale process, missing window, and wrong tab reject before mutation", () => {
  for (const request of [
    { ...scope, pid: 456, operation: "input", text: "no" },
    { ...scope, operation: "close_window", windowId: "missing" },
    {
      ...scope,
      operation: "close_tab",
      windowId: "window-a",
      tabId: "missing",
    },
  ]) {
    const { result, calls } = bridge(request);
    assert.equal(result.status, "rejected");
    assert.deepEqual(calls, []);
  }
});
test("new windows work with no existing surfaces", () => {
  const { result, calls } = bridge(
    { operation: "new_window" },
    { empty: true },
  );
  assert.equal(result.targetId, "pane-new");
  assert.equal(result.pid, 123);
  assert.deepEqual(calls, [["new_window"]]);
});
test("splits and close scopes reach only their explicit native object", () => {
  for (const [request, want] of [
    [
      { ...scope, operation: "split", direction: "down" },
      ["split", "pane-a", "down"],
    ],
    [{ ...scope, operation: "close_pane" }, ["close_pane", "pane-a"]],
    [
      { operation: "close_tab", windowId: "window-a", tabId: "tab-a" },
      ["close_tab", "tab-a"],
    ],
    [
      { operation: "close_window", windowId: "window-a" },
      ["close_window", "window-a"],
    ],
  ]) {
    const { result, calls } = bridge(request);
    assert.equal(result.status, "ok");
    assert.deepEqual(calls, [want]);
  }
});
test("layout and titles never operate on a different focused pane", () => {
  for (const operation of ["control", "rename_tab", "rename_pane"]) {
    const { result, calls } = bridge(
      { ...scope, operation, nativeAction: "equalize_splits" },
      { focus: "another-pane" },
    );
    assert.equal(result.status, "rejected");
    assert.equal(result.code, "focus_required");
    assert.deepEqual(calls, []);
  }
});
test("native rejection remains uncertain after an action attempt", () => {
  const { result, calls } = bridge(
    { ...scope, operation: "control", nativeAction: "toggle_split_zoom" },
    { accept: false },
  );
  assert.equal(result.status, "uncertain");
  assert.equal(calls.length, 1);
});
