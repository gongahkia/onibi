"use strict";

// Secrets stay in memory and the server's HttpOnly cookie. Strip a pairing
// fragment before any asynchronous work or network request can expose it.
let fragmentToken =
  new URLSearchParams(location.hash.slice(1)).get("token") || "";
if (new URLSearchParams(location.hash.slice(1)).has("token"))
  history.replaceState(null, "", location.pathname + location.search);

const byId = (id) => document.getElementById(id);
const state = {
  csrf: "",
  targets: [],
  selectedId: "",
  transport: "checking",
  transportVersion: 0,
  requestVersion: 0,
  nativeState: "checking",
  nativeVersion: 0,
  checkVersion: 0,
  nativeCode: "",
  captureState: "unknown",
  captureDetail: "",
  capabilities: { known: false },
  targetsPromise: null,
  targetTimer: null,
  targetFailures: 0,
  nextInventoryAt: 0,
  readTimer: null,
  expiresAt: 0,
  targetsLoading: false,
  snapshotVersion: 0,
  snapshotTarget: "",
  pending: null,
  receiptTimer: null,
  closeTarget: null,
  controls: [],
  drafts: new Map(),
  filter: "",
  modifier: "ctrl",
  renameAction: null,
  snapshotLoading: false,
  snapshotAbort: null,
  follow: false,
  followTimer: null,
  lastCapturedAt: 0,
  snapshotStale: false,
  authGeneration: 0,
};
const actionButtons = [
  "send-input",
  "paste-input",
  "new-tab",
  "close-tab",
  "focus-terminal",
  "split-right",
  "controls-focus",
];
const pendingStorageKey = "onibi.pending-action.v1";
const rejectedActionCodes = new Set([
  "unauthorized",
  "invalid_csrf",
  "invalid_origin",
  "invalid_host",
  "invalid_action",
  "invalid_action_id",
  "invalid_json",
  "body_too_large",
  "invalid_content_type",
  "action_id_conflict",
]);

function persistPending() {
  const pending = state.pending;
  if (!pending) return;
  const { kind, targetId, windowId, tabId } = pending.action;
  try {
    // Only the opaque receipt and destination identities survive a reload.
    // Terminal input, drafts, snapshots, tokens, and CSRF values never do.
    sessionStorage.setItem(
      pendingStorageKey,
      JSON.stringify({
        id: pending.id,
        action: { kind, targetId, windowId, tabId },
      }),
    );
    pending.persisted = true;
  } catch (_) {
    pending.persisted = false;
  }
}
function forgetPending() {
  state.pending = null;
  clearTimeout(state.receiptTimer);
  try {
    sessionStorage.removeItem(pendingStorageKey);
  } catch (_) {
    /* A stale receipt can only be read, never replayed. */
  }
}
function restorePending() {
  try {
    const saved = JSON.parse(
      sessionStorage.getItem(pendingStorageKey) || "null",
    );
    if (
      !saved ||
      typeof saved.id !== "string" ||
      !/^[0-9a-f-]{36}$/i.test(saved.id) ||
      !saved.action ||
      ![
        "input",
        "key",
        "focus",
        "new_tab",
        "new_window",
        "split",
        "close_tab",
        "close_pane",
        "close_window",
        "rename_tab",
        "rename_pane",
        "control",
      ].includes(saved.action.kind)
    )
      return;
    const action = { kind: saved.action.kind };
    for (const key of ["targetId", "windowId", "tabId"])
      if (
        typeof saved.action[key] === "string" &&
        saved.action[key].length <= 1024
      )
        action[key] = saved.action[key];
    state.pending = {
      id: saved.id,
      action,
      draft: null,
      restored: true,
      uncertain: true,
      persisted: true,
    };
  } catch (_) {
    /* Storage may be unavailable in a private browser session. */
  }
}

class APIError extends Error {
  constructor(message, status = 0, code = "", retryAfter = 0) {
    super(message);
    this.status = status;
    this.code = code;
    this.retryAfter = retryAfter;
  }
}

async function api(path, options = {}) {
  const networkVersion = ++state.requestVersion;
  const generation = state.authGeneration;
  const controller = new AbortController();
  const { signal: upstream, ...requestOptions } = options;
  const abort = () => controller.abort();
  upstream?.addEventListener("abort", abort, { once: true });
  if (upstream?.aborted) controller.abort();
  const timeout = setTimeout(() => controller.abort(), 40000);
  try {
    const response = await fetch(path, {
      ...requestOptions,
      credentials: "same-origin",
      cache: "no-store",
      signal: controller.signal,
      headers: {
        ...(options.body ? { "Content-Type": "application/json" } : {}),
        ...(options.method === "POST" && state.csrf
          ? { "X-Onibi-CSRF": state.csrf }
          : {}),
        ...options.headers,
      },
    });
    if (generation === state.authGeneration)
      setNetwork("reachable", networkVersion);
    let body;
    try {
      body = await response.json();
    } catch (_) {
      throw new APIError(
        "Your Mac returned an unreadable response.",
        response.status,
      );
    }
    if (!response.ok)
      throw new APIError(
        typeof body.error === "string"
          ? body.error
          : "The request could not be completed.",
        response.status,
        body.code || "",
        Math.min(
          60,
          Math.max(0, Number(response.headers.get("Retry-After")) || 0),
        ),
      );
    return body;
  } catch (error) {
    if (error instanceof APIError) throw error;
    if (!upstream?.aborted && generation === state.authGeneration)
      setNetwork("offline", networkVersion);
    throw new APIError("Connection to your Mac was interrupted.");
  } finally {
    clearTimeout(timeout);
    upstream?.removeEventListener("abort", abort);
  }
}

function selectedTarget() {
  return state.targets.find((target) => target.id === state.selectedId);
}
function targetName(target) {
  return target.title || target.tabTitle || "Untitled pane";
}
function tabName(target) {
  return target.tabTitle || "Untitled tab";
}
function windowName(target) {
  return target.windowTitle || "Ghostty window";
}
function controlReady() {
  return (
    state.transport === "reachable" &&
    state.nativeState === "ready" &&
    !(state.capabilities.known && !state.capabilities.control)
  );
}
function setNetwork(value, version = ++state.requestVersion) {
  if (version < state.transportVersion) return;
  state.transportVersion = version;
  state.transport = value;
  if (value === "offline") {
    clearTimeout(state.followTimer);
    state.follow = false;
  }
  updateControls();
}
function setNative(value, code = "", version = ++state.checkVersion) {
  if (version < state.nativeVersion) return;
  state.nativeVersion = version;
  state.nativeState = value;
  state.nativeCode = code;
  if (value !== "ready") {
    clearTimeout(state.followTimer);
    state.follow = false;
  }
  updateControls();
}
function renderReadiness() {
  let label = "Connected",
    detail = "Your Mac and Ghostty are responding.";
  if (state.transport === "offline") {
    label = "Disconnected";
    detail =
      "The connection to your Mac was lost. Your pane drafts are kept in this page.";
  } else if (state.nativeState === "checking") {
    label = "Checking Ghostty";
    detail = "Checking the Mac’s native terminal inventory…";
  } else if (state.nativeState !== "ready") {
    label = "Ghostty unavailable";
    detail =
      {
        ghostty_not_running:
          "Your Mac is reachable. Open Ghostty on the Mac to continue.",
        automation_permission:
          "Your Mac is reachable. Allow Automation access for the process launching Onibi.",
        ghostty_timeout:
          "Your Mac is reachable, but Ghostty did not respond in time. Automatic checks are slowing down.",
        ghostty_communication:
          "Your Mac is reachable, but communication with Ghostty failed. Check the app on your Mac; automatic checks will retry.",
        unsupported_platform: "Native Ghostty control requires macOS.",
      }[state.nativeCode] ||
      "Your Mac is reachable, but Ghostty’s terminal inventory is unavailable.";
  } else if (state.capabilities.known && !state.capabilities.control) {
    label = "Update Ghostty";
    detail =
      "This Ghostty version does not support the required native controls.";
  } else if (state.captureState === "focus_required") {
    label = "Focus needed";
    detail =
      "Input targets the selected pane. Focus it on your Mac to read output.";
  } else if (state.captureState === "unavailable") {
    label = "Output unavailable";
    detail =
      state.captureDetail ||
      "Terminal control is available, but output could not be read.";
  }
  byId("connection-label").textContent = label;
  byId("connection").classList.toggle("disconnected", !controlReady());
  byId("connection").setAttribute("aria-label", label + ". View Mac readiness");
  byId("readiness-strip").hidden = label === "Connected";
  byId("readiness-summary").textContent = detail;
  byId("health-summary").textContent = detail;
  const waiting = Date.now() < state.nextInventoryAt;
  const retryCapture = controlReady() && state.captureState === "unavailable";
  byId("retry-connection").disabled =
    !state.csrf ||
    (retryCapture ? state.snapshotLoading : state.targetsLoading || waiting);
  byId("retry-connection").hidden =
    controlReady() && state.captureState === "focus_required";
  byId("retry-connection").textContent = retryCapture
    ? state.snapshotLoading
      ? "Reading…"
      : "Retry output"
    : state.targetsLoading
      ? "Checking…"
      : waiting
        ? "Retry shortly"
        : "Check again";
  byId("session-expiry").textContent = state.expiresAt
    ? "Browser session expires " +
      new Date(state.expiresAt).toLocaleString() +
      ". Restarting Onibi also revokes this session."
    : "Session expiry is unavailable.";
}
function scheduleInventory() {
  clearTimeout(state.targetTimer);
  if (!state.csrf || document.hidden) return;
  const delay = Math.max(10000, state.nextInventoryAt - Date.now());
  state.targetTimer = setTimeout(() => {
    if (state.pending) scheduleInventory();
    else refreshTargets();
  }, delay);
}
async function loadHealth() {
  byId("health-refresh").disabled = true;
  const generation = state.authGeneration;
  try {
    const data = await api("/api/health");
    if (generation !== state.authGeneration) return;
    const list = byId("health-checks");
    list.replaceChildren();
    const checks = data.diagnostics?.checks || [];
    for (const check of checks) {
      const row = document.createElement("li");
      row.dataset.state = check.state;
      const name = document.createElement("strong");
      const detail = document.createElement("p");
      const checkNames = {
        installation: "Ghostty app",
        process: "Ghostty process",
        automation: "Automation",
        capture: "Accessibility",
      };
      const checkStates = {
        ready: "Ready",
        running: "Running",
        unknown: "Not checked",
        capture_permission: "Permission needed",
        stopped: "Not running",
        missing: "Not installed",
        ambiguous: "Multiple processes",
        unsupported: "Unsupported",
        unavailable: "Unavailable",
      };
      name.textContent =
        (checkNames[check.id] || check.id.replaceAll("_", " ")) +
        " · " +
        (checkStates[check.state] || check.state.replaceAll("_", " "));
      detail.textContent = check.detail;
      row.append(name, detail);
      list.append(row);
    }
    const version =
      data.diagnostics?.capabilities?.version ||
      state.capabilities.version ||
      "unknown";
    const inventory = data.inventory;
    byId("health-observation").textContent =
      "Ghostty " +
      version +
      (inventory?.state && inventory.state !== "unknown"
        ? " · Last inventory: " +
          inventory.state +
          " (" +
          inventory.durationMs +
          " ms)"
        : " · Native inventory has not been verified.");
    byId("health-error").textContent = "";
  } catch (error) {
    if (generation !== state.authGeneration) return;
    if (error.status === 401)
      requirePairing(
        "Your session expired. Use a fresh pairing link from your Mac.",
      );
    else byId("health-error").textContent = error.message;
  } finally {
    byId("health-refresh").disabled = false;
  }
}
function message(text) {
  byId("workspace-message").textContent = text;
  byId("workspace-message").hidden = !text;
}
function pairMessage(text, error = false) {
  byId("pair-message").textContent = text;
  byId("pair-message").classList.toggle("error", error);
}
function updateControls() {
  const target = selectedTarget();
  const canAct = Boolean(
    state.csrf && target && controlReady() && !state.pending,
  );
  const canCreate = Boolean(state.csrf && controlReady() && !state.pending);
  for (const id of actionButtons) byId(id).disabled = !canAct;
  for (const id of ["new-window", "picker-new-window", "controls-new-window"])
    byId(id).disabled = !canCreate;
  byId("send-input").disabled = !canAct || byId("compose").value.length === 0;
  byId("paste-input").disabled = byId("send-input").disabled;
  byId("compose").disabled = !state.csrf || !target;
  byId("read-output").disabled =
    !state.csrf || !target || state.snapshotLoading;
  byId("follow-output").disabled = !state.csrf || !target || !controlReady();
  byId("follow-output").setAttribute("aria-pressed", String(state.follow));
  byId("logout").disabled = Boolean(state.pending);
  for (const button of document.querySelectorAll(
    "[data-key], [data-letter], [data-split], [data-action], [data-close]",
  ))
    button.disabled = !canAct;
  for (const button of document.querySelectorAll(
    "[data-control], [data-rename]",
  ))
    button.disabled =
      !canAct ||
      !target?.focused ||
      (button.hasAttribute("data-rename") &&
        state.capabilities.known &&
        !state.capabilities.rename);
  byId("controls-focus").hidden = Boolean(target?.focused) || !target;
  byId("control-focus-note").textContent = target?.focused
    ? "These controls affect this focused pane on your Mac."
    : "Focus this pane on your Mac to use layout, scrollback, display, and title controls.";
  byId("title-capability").hidden = !(
    state.capabilities.known && !state.capabilities.rename
  );
  renderReadiness();
}

function rememberDraft() {
  if (state.selectedId)
    state.drafts.set(state.selectedId, byId("compose").value);
}
function stopFollow() {
  state.follow = false;
  clearTimeout(state.followTimer);
  updateControls();
}
function scheduleFollow() {
  clearTimeout(state.followTimer);
  if (
    state.follow &&
    !state.snapshotLoading &&
    !document.hidden &&
    state.csrf &&
    controlReady() &&
    !state.pending &&
    selectedTarget()
  ) {
    state.followTimer = setTimeout(() => readSnapshot(), 2500);
  }
}
function cancelSnapshot() {
  state.snapshotVersion++;
  clearTimeout(state.readTimer);
  state.snapshotAbort?.abort();
  state.snapshotAbort = null;
  state.snapshotLoading = false;
  clearTimeout(state.followTimer);
}
function renderNativeControls() {
  const root = byId("native-controls");
  root.replaceChildren();
  for (const group of [...new Set(state.controls.map((c) => c.group))]) {
    const section = document.createElement("section");
    section.className = "control-group";
    const heading = document.createElement("h3");
    heading.textContent = group;
    const grid = document.createElement("div");
    grid.className = "control-grid";
    for (const control of state.controls.filter((c) => c.group === group)) {
      const button = document.createElement("button");
      button.type = "button";
      button.className = "secondary";
      button.textContent = control.label;
      button.dataset.control = control.id;
      button.addEventListener("click", () => {
        const target = selectedTarget();
        if (target)
          performAction({
            kind: "control",
            targetId: target.id,
            control: control.id,
          });
      });
      grid.append(button);
    }
    section.append(heading, grid);
    root.append(section);
  }
}

function authenticated(session) {
  if (!session || typeof session.csrf !== "string" || !session.csrf)
    throw new APIError("This browser could not establish a session.");
  state.authGeneration++;
  state.targetsLoading = false;
  state.targetsPromise = null;
  state.targetFailures = 0;
  state.nextInventoryAt = 0;
  state.expiresAt = Date.parse(session.expiresAt);
  state.csrf = session.csrf;
  byId("pair-token").value = "";
  byId("pair-view").hidden = true;
  byId("workspace").hidden = false;
  byId("session-controls").hidden = false;
  state.nativeState = "checking";
  setNetwork("reachable");
  refreshTargets();
  if (state.pending) {
    actionNotice(
      "Checking an earlier action",
      "Recovering its receipt after reconnecting. No command will be resent.",
    );
    checkReceipt();
  }
}

function requirePairing(text) {
  rememberDraft();
  cancelSnapshot();
  stopFollow();
  state.authGeneration++;
  state.csrf = "";
  state.expiresAt = 0;
  clearTimeout(state.targetTimer);
  state.captureState = "unknown";
  state.capabilities = { known: false };
  state.nativeState = "checking";
  state.targets = [];
  state.selectedId = "";
  byId("compose").value = "";
  state.snapshotVersion++;
  state.snapshotTarget = "";
  byId("terminal-output").textContent = "";
  byId("snapshot-time").textContent = "";
  byId("snapshot-state").textContent = "No terminal selected";
  showEmpty(
    "Your next move starts here",
    "Select a Ghostty pane to read its output and send input.",
  );
  byId("pair-view").hidden = false;
  byId("workspace").hidden = true;
  byId("session-controls").hidden = true;
  byId("target-picker").close();
  for (const dialog of document.querySelectorAll("dialog")) dialog.close();
  pairMessage(text);
  updateControls();
}

async function pair(token) {
  byId("pair-submit").disabled = true;
  byId("pair-token").value = "";
  pairMessage("Connecting securely to your Mac…");
  try {
    const session = await api("/api/pair", {
      method: "POST",
      body: JSON.stringify({ token }),
    });
    authenticated(session);
  } catch (error) {
    pairMessage(
      error.message + " Use a fresh pairing link or token from your Mac.",
      true,
    );
  } finally {
    token = "";
    byId("pair-submit").disabled = false;
  }
}

function renderTargets() {
  for (const containerId of ["target-list", "picker-list"]) {
    const container = byId(containerId);
    const focusedId = container.contains(document.activeElement)
      ? document.activeElement.dataset.targetId
      : "";
    container.replaceChildren();
    if (!state.targets.length) {
      const empty = document.createElement("p");
      empty.className = "list-empty";
      empty.textContent = state.targetsLoading
        ? "Finding Ghostty terminals…"
        : "No panes yet. Create a Ghostty window to get started.";
      container.append(empty);
      continue;
    }
    const visible = state.targets.filter((target) =>
      [target.title, target.tabTitle, target.windowTitle, target.cwd]
        .join(" ")
        .toLocaleLowerCase()
        .includes(state.filter.toLocaleLowerCase()),
    );
    if (!visible.length) {
      const empty = document.createElement("p");
      empty.className = "list-empty";
      empty.textContent = "No matching panes. Try another name or folder.";
      container.append(empty);
      continue;
    }
    const windows = new Map();
    for (const target of visible) {
      if (!windows.has(target.windowId))
        windows.set(target.windowId, new Map());
      const tabs = windows.get(target.windowId);
      if (!tabs.has(target.tabId)) tabs.set(target.tabId, []);
      tabs.get(target.tabId).push(target);
    }
    for (const tabs of windows.values()) {
      const group = document.createElement("section");
      group.className = "window-group";
      const heading = document.createElement("h3");
      heading.textContent = windowName(tabs.values().next().value[0]);
      group.append(heading);
      for (const panes of tabs.values()) {
        const tab = document.createElement("div");
        tab.className = "tab-group";
        const label = document.createElement("p");
        label.className = "tab-label";
        label.textContent = tabName(panes[0]);
        tab.append(label);
        for (const target of panes) {
          const button = document.createElement("button");
          button.type = "button";
          button.className = "target";
          button.dataset.targetId = target.id;
          button.setAttribute(
            "aria-pressed",
            String(target.id === state.selectedId),
          );
          button.setAttribute(
            "aria-label",
            `${targetName(target)} ${target.cwd || "Ghostty pane"}${target.focused ? " ON MAC" : ""}, ${tabName(target)}, ${windowName(target)}`,
          );
          button.title = target.cwd || targetName(target);
          const mark = document.createElement("span");
          mark.className = "target-mark";
          mark.textContent = target.id === state.selectedId ? "›" : "·";
          mark.setAttribute("aria-hidden", "true");
          const copy = document.createElement("span");
          copy.className = "target-copy";
          const name = document.createElement("span");
          name.className = "target-name";
          name.textContent = targetName(target);
          const cwd = document.createElement("span");
          cwd.className = "target-cwd";
          cwd.textContent = target.cwd || "Ghostty pane";
          copy.append(name, cwd);
          button.append(mark, copy);
          if (target.focused) {
            const focus = document.createElement("span");
            focus.className = "target-focus";
            focus.textContent = "ON MAC";
            button.append(focus);
          }
          button.addEventListener("click", () => selectTarget(target.id));
          tab.append(button);
        }
        group.append(tab);
      }
      container.append(group);
    }
    if (focusedId)
      Array.from(container.querySelectorAll(".target"))
        .find((button) => button.dataset.targetId === focusedId)
        ?.focus({ preventScroll: true });
  }
}

function renderDestination() {
  const target = selectedTarget();
  byId("focus-terminal").hidden = !target || Boolean(target.focused);
  byId("destination-context").textContent = target
    ? target.focused
      ? "FOCUSED ON YOUR MAC"
      : "YOUR SELECTED PANE"
    : "READY WHEN YOU ARE";
  byId("focus-hint").textContent = target?.focused
    ? "Your input goes to this exact pane. Follow refreshes visible text."
    : "Focus & read brings this pane forward on your Mac.";
  byId("control-destination").textContent = target
    ? `${targetName(target)} · ${tabName(target)} · ${windowName(target)}`
    : "No pane selected. You can still create a new Ghostty window.";
  byId("keyboard-destination").textContent = target
    ? `Keys go to ${targetName(target)} · ${tabName(target)}`
    : "Choose a pane before sending keys.";
  byId("fleet-count").textContent =
    `${new Set(state.targets.map((t) => t.windowId)).size} windows · ${new Set(state.targets.map((t) => t.tabId)).size} tabs · ${state.targets.length} panes`;
  byId("destination-title").textContent = target
    ? targetName(target)
    : "Choose a terminal";
  byId("destination-detail").textContent = target
    ? `${windowName(target)} / ${tabName(target)}${target.cwd ? " · " + target.cwd : ""}`
    : "An existing Ghostty pane on your Mac";
  byId("compose-destination").textContent = target
    ? targetName(target)
    : "No destination";
  byId("compose-destination").title = target
    ? `${windowName(target)} / ${tabName(target)} / ${target.id}`
    : "";
  byId("new-tab").title = target
    ? `Create a tab in ${windowName(target)}`
    : "Choose a terminal first";
  updateControls();
}

async function refreshTargets(options = {}) {
  if (!state.csrf) return;
  const force = options.force === true;
  if (state.targetsPromise) {
    await state.targetsPromise;
    if (force && state.csrf) return refreshTargets({ force: true });
    return;
  }
  if (!force && Date.now() < state.nextInventoryAt) {
    scheduleInventory();
    return;
  }
  const pending = refreshTargetsRequest();
  state.targetsPromise = pending;
  try {
    await pending;
  } finally {
    if (state.targetsPromise === pending) state.targetsPromise = null;
  }
}
async function refreshTargetsRequest() {
  const evidence = ++state.checkVersion;
  const generation = state.authGeneration;
  state.targetsLoading = true;
  if (!state.targets.length) renderTargets();
  try {
    const data = await api("/api/targets");
    if (!state.csrf || generation !== state.authGeneration) return;
    if (!Array.isArray(data.targets))
      throw new APIError("The terminal list is unavailable.");
    state.capabilities = data.capabilities || { known: false };
    state.targets = data.targets.filter(
      (target) =>
        typeof target.id === "string" &&
        target.id &&
        target.windowId &&
        target.tabId,
    );
    if (
      Array.isArray(data.controls) &&
      JSON.stringify(data.controls) !== JSON.stringify(state.controls)
    ) {
      state.controls = data.controls.filter(
        (c) =>
          typeof c.id === "string" &&
          typeof c.label === "string" &&
          typeof c.group === "string",
      );
      renderNativeControls();
    }
    if (state.selectedId && !selectedTarget()) {
      rememberDraft();
      cancelSnapshot();
      stopFollow();
      state.selectedId = "";
      state.captureState = "unknown";
      state.captureDetail = "";
      byId("compose").value = "";
      state.snapshotVersion++;
      state.snapshotTarget = "";
      byId("terminal-output").textContent = "";
      showEmpty(
        "That pane is no longer available",
        "Choose another destination. Its draft stays with the original pane.",
      );
      byId("snapshot-state").textContent = "Destination unavailable";
      byId("snapshot-time").textContent = "";
    }
    state.targetFailures = 0;
    state.nextInventoryAt = 0;
    setNative("ready", "", evidence);
    message("");
  } catch (error) {
    if (generation !== state.authGeneration) return;
    if (error.status === 401)
      requirePairing(
        "Your session expired. Reconnect with a fresh pairing link from your Mac.",
      );
    else {
      state.targetFailures = Math.min(state.targetFailures + 1, 4);
      state.nextInventoryAt =
        Date.now() +
        Math.max(
          error.retryAfter * 1000,
          Math.min(10000 * 2 ** (state.targetFailures - 1), 60000),
        );
      setNative("unavailable", error.code, evidence);
    }
  } finally {
    if (generation === state.authGeneration) {
      state.targetsLoading = false;
      renderTargets();
      renderDestination();
      scheduleInventory();
    }
  }
}

function showEmpty(title, description) {
  byId("terminal-output").hidden = true;
  byId("output-empty").hidden = false;
  byId("output-empty-title").textContent = title;
  byId("output-empty-description").textContent = description;
  byId("choose-terminal").hidden = Boolean(selectedTarget());
}

function selectTarget(id) {
  if (!state.targets.some((target) => target.id === id)) return;
  if (state.selectedId === id) {
    byId("target-picker").close();
    return;
  }
  rememberDraft();
  cancelSnapshot();
  stopFollow();
  state.selectedId = id;
  state.captureState = "unknown";
  state.captureDetail = "";
  byId("compose").value = state.drafts.get(id) || "";
  state.snapshotTarget = "";
  state.snapshotVersion++;
  byId("terminal-output").textContent = "";
  byId("snapshot-time").textContent = "";
  byId("snapshot-state").textContent = "Reading…";
  byId("focus-terminal").hidden = true;
  showEmpty(
    "Reading this pane…",
    "Fetching a fresh text snapshot from your Mac.",
  );
  byId("target-picker").close();
  renderTargets();
  renderDestination();
  readSnapshot();
}

async function readSnapshot() {
  const target = selectedTarget();
  if (!state.csrf || !target || state.snapshotLoading || document.hidden)
    return;
  const evidence = ++state.checkVersion;
  const version = ++state.snapshotVersion;
  const generation = state.authGeneration;
  state.snapshotLoading = true;
  state.snapshotAbort = new AbortController();
  const signal = state.snapshotAbort.signal;
  byId("read-output").disabled = true;
  try {
    const snapshot = await api(
      "/api/snapshot?targetId=" + encodeURIComponent(target.id),
      { signal },
    );
    if (
      version !== state.snapshotVersion ||
      target.id !== state.selectedId ||
      !state.csrf ||
      generation !== state.authGeneration
    )
      return;
    if (snapshot.targetId !== target.id || typeof snapshot.text !== "string")
      throw new APIError("The snapshot destination could not be verified.");
    state.captureState = "ready";
    state.captureDetail = "";
    state.snapshotTarget = target.id;
    state.snapshotStale = false;
    const output = byId("terminal-output");
    const atBottom =
      output.scrollHeight - output.scrollTop - output.clientHeight < 32;
    if (output.textContent !== (snapshot.text || "(No visible output.)"))
      output.textContent = snapshot.text || "(No visible output.)";
    if (state.follow && atBottom) output.scrollTop = output.scrollHeight;
    byId("terminal-output").hidden = false;
    byId("output-empty").hidden = true;
    byId("focus-terminal").hidden = true;
    byId("snapshot-state").textContent = "Read on your Mac";
    const captured = new Date(snapshot.capturedAt);
    state.lastCapturedAt = captured.getTime();
    byId("snapshot-time").textContent = Number.isNaN(captured.getTime())
      ? "Time unavailable"
      : captured.toLocaleTimeString([], {
          hour: "2-digit",
          minute: "2-digit",
          second: "2-digit",
        });
    byId("snapshot-time").dateTime = Number.isNaN(captured.getTime())
      ? ""
      : captured.toISOString();
    byId("snapshot-time").title = Number.isNaN(captured.getTime())
      ? ""
      : captured.toLocaleString();
    byId("snapshot-state").parentElement.parentElement.classList.remove(
      "stale",
    );
    message("");
    updateControls();
  } catch (error) {
    if (
      signal.aborted ||
      generation !== state.authGeneration ||
      version !== state.snapshotVersion ||
      target.id !== state.selectedId
    )
      return;
    stopFollow();
    if (error.status === 401) {
      requirePairing(
        "Your session expired. Your draft is kept; reconnect to continue.",
      );
      return;
    }
    state.snapshotStale = true;
    const focusRequired = error.code === "focus_required";
    byId("focus-terminal").hidden = !focusRequired;
    byId("snapshot-state").textContent =
      state.snapshotTarget === target.id
        ? "Last snapshot · not live"
        : "Output unavailable";
    byId("snapshot-state").parentElement.parentElement.classList.add("stale");
    const description = focusRequired
      ? "This pane needs to be focused on your Mac before its output can be read. Use Focus & read to switch the Mac’s active pane."
      : error.message;
    if (state.snapshotTarget !== target.id)
      showEmpty(
        focusRequired
          ? "Focus this pane to read it"
          : "Output isn’t available yet",
        description,
      );
    else message(description);
    state.captureState = focusRequired ? "focus_required" : "unavailable";
    state.captureDetail = description;
    if (
      [
        "ghostty_not_running",
        "ghostty_timeout",
        "ghostty_communication",
        "automation_permission",
        "unsupported_platform",
      ].includes(error.code)
    )
      setNative("unavailable", error.code, evidence);
    updateControls();
  } finally {
    if (version === state.snapshotVersion) {
      state.snapshotLoading = false;
      state.snapshotAbort = null;
      updateControls();
      scheduleFollow();
    }
  }
}

function actionNotice(title, detail, unresolved = false) {
  byId("action-notice").hidden = false;
  byId("action-title").textContent = title;
  byId("action-detail").textContent = detail;
  byId("check-receipt").hidden = !unresolved;
  byId("acknowledge-action").hidden = !unresolved;
}

function unresolved(detail) {
  if (!state.pending) return;
  state.pending.uncertain = true;
  const draftNote = state.pending.restored
    ? " Drafts aren’t stored between page loads."
    : " Your draft is kept.";
  actionNotice(
    "Delivery needs checking",
    detail +
      draftNote +
      " Read the terminal or check your Mac before continuing; this action will not be sent again.",
    true,
  );
  updateControls();
}

async function applyReceipt(receipt) {
  const pending = state.pending;
  if (!pending || receipt.id !== pending.id) return;
  if (receipt.state === "pending") {
    actionNotice(
      "Waiting for your Mac",
      "The action has a receipt. Waiting for its result without sending it again.",
    );
    state.receiptTimer = setTimeout(checkReceipt, 1500);
    return;
  }
  if (receipt.state === "uncertain") {
    unresolved(
      receipt.error ||
        "The Mac could not confirm whether the action completed.",
    );
    return;
  }
  if (receipt.state !== "completed" && receipt.state !== "rejected") {
    unresolved("The receipt has an unknown state.");
    return;
  }
  forgetPending();
  if (receipt.state === "rejected") {
    actionNotice(
      "Action wasn’t sent",
      receipt.error ||
        "Your Mac rejected this action. Your draft has been kept.",
    );
    // A precondition can fail before the next scheduled poll (closed pane,
    // lost native connection, changed focus). Reconcile before offering input.
    setNative("checking");
    await refreshTargets({ force: true });
  } else {
    if (pending.action.kind === "input") {
      if (
        state.selectedId === pending.action.targetId &&
        byId("compose").value === pending.draft
      ) {
        byId("compose").value = "";
        state.drafts.delete(pending.action.targetId);
      } else if (state.drafts.get(pending.action.targetId) === pending.draft)
        state.drafts.delete(pending.action.targetId);
    }
    actionNotice(
      {
        input: "Input delivered",
        key: "Key delivered",
        close_pane: "Close requested",
        close_tab: "Close requested",
        close_window: "Close requested",
      }[pending.action.kind] || "Done",
      receipt.result && receipt.result.message
        ? receipt.result.message
        : "Your Mac confirmed the action.",
    );
    if (["input", "key"].includes(pending.action.kind)) {
      // Collapse post-input observation during repeated key taps. The server
      // still performs fresh destination checks before every mutation.
      if (state.selectedId === pending.action.targetId) {
        clearTimeout(state.readTimer);
        state.readTimer = setTimeout(readSnapshot, 400);
      }
      updateControls();
      scheduleFollow();
      return;
    }
    await refreshTargets({ force: true });
    if (
      ["new_tab", "new_window", "split"].includes(pending.action.kind) &&
      receipt.result &&
      state.targets.some((target) => target.id === receipt.result.targetId)
    )
      selectTarget(receipt.result.targetId);
    else if (state.selectedId === pending.action.targetId) readSnapshot();
  }
  updateControls();
  scheduleFollow();
}

async function checkReceipt() {
  if (!state.pending || !state.csrf) return;
  const id = state.pending.id;
  clearTimeout(state.receiptTimer);
  byId("check-receipt").disabled = true;
  try {
    await applyReceipt(await api("/api/actions/" + encodeURIComponent(id)));
  } catch (error) {
    if (!state.pending || state.pending.id !== id) return;
    if (error.status === 401) {
      requirePairing(
        "Your session expired while an action was being checked. Reconnect to check its receipt; it will not be replayed.",
      );
    } else {
      unresolved(
        error.status === 404
          ? "No receipt was found. The action may have reached your Mac before the connection was lost."
          : error.message,
      );
    }
  } finally {
    byId("check-receipt").disabled = false;
  }
}

async function performAction(action, draft = "") {
  const target = selectedTarget();
  if (
    !state.csrf ||
    !controlReady() ||
    (action.kind !== "new_window" && !target) ||
    state.pending
  )
    return;
  if (!crypto.randomUUID) {
    message(
      "This browser needs a private HTTPS connection to control Ghostty.",
    );
    return;
  }
  const pending = { id: crypto.randomUUID(), action, draft };
  cancelSnapshot();
  state.pending = pending;
  persistPending();
  updateControls();
  const detail =
    action.kind === "input"
      ? `Destination: ${targetName(target)} · ${tabName(target)}`
      : "Waiting for the selected action to be confirmed.";
  actionNotice(
    "Sending to your Mac",
    detail +
      (pending.persisted
        ? ""
        : " Keep this tab open until the action is confirmed."),
  );
  try {
    const receipt = await api("/api/actions", {
      method: "POST",
      body: JSON.stringify({ id: pending.id, action }),
    });
    if (!receipt || receipt.id !== pending.id)
      throw new APIError("The action receipt could not be verified.");
    await applyReceipt(receipt);
  } catch (error) {
    if (!state.pending || state.pending.id !== pending.id) return;
    if (
      error.status >= 400 &&
      error.status < 500 &&
      rejectedActionCodes.has(error.code)
    ) {
      forgetPending();
      actionNotice(
        "Action wasn’t accepted",
        error.message + " Your draft is kept.",
      );
      if (error.status === 401)
        requirePairing(
          "Your session expired. Reconnect to continue; your draft has been kept.",
        );
    } else {
      actionNotice(
        "Checking delivery",
        "The connection was interrupted. Checking the receipt; the action will not be sent again.",
      );
      await checkReceipt();
    }
    updateControls();
  }
}

function sendInput(enter) {
  const target = selectedTarget();
  const text = byId("compose").value;
  if (!target || !text) return;
  if (new TextEncoder().encode(text).length > 16 * 1024) {
    message(
      "This draft exceeds the 16 KiB text limit. Shorten it before sending; nothing has been sent.",
    );
    return;
  }
  performAction({ kind: "input", targetId: target.id, text, enter }, text);
}

function openPicker() {
  byId("target-picker").showModal();
}
byId("pair-form").addEventListener("submit", (event) => {
  event.preventDefault();
  const token = byId("pair-token").value.trim();
  if (token) pair(token);
});
byId("compose").addEventListener("input", () => {
  rememberDraft();
  updateControls();
});
byId("compose").addEventListener("keydown", (event) => {
  if (
    event.key === "Enter" &&
    (event.metaKey || event.ctrlKey) &&
    !event.isComposing
  ) {
    event.preventDefault();
    sendInput(true);
  }
});
byId("send-input").addEventListener("click", () => sendInput(true));
byId("paste-input").addEventListener("click", () => sendInput(false));
byId("read-output").addEventListener("click", readSnapshot);
byId("focus-terminal").addEventListener("click", () => {
  const target = selectedTarget();
  if (target) performAction({ kind: "focus", targetId: target.id });
});
byId("open-picker").addEventListener("click", openPicker);
byId("choose-terminal").addEventListener("click", openPicker);
byId("close-picker").addEventListener("click", () =>
  byId("target-picker").close(),
);
byId("refresh-targets").addEventListener("click", refreshTargets);
byId("refresh-picker").addEventListener("click", refreshTargets);
byId("check-receipt").addEventListener("click", checkReceipt);
byId("acknowledge-action").addEventListener("click", () => {
  if (!state.pending || !state.pending.uncertain) return;
  forgetPending();
  actionNotice(
    "Ready when you are",
    "Previous delivery remains uncertain. Nothing has been resent. Review your draft before submitting a new action.",
  );
  refreshTargets();
  updateControls();
});
for (const button of document.querySelectorAll("[data-key]"))
  button.addEventListener("click", () => {
    const target = selectedTarget();
    if (target)
      performAction({
        kind: "key",
        targetId: target.id,
        key: button.dataset.key,
      });
  });
byId("new-tab").addEventListener("click", () => {
  const target = selectedTarget();
  if (target) performAction({ kind: "new_tab", windowId: target.windowId });
});
function requestClose(kind) {
  const target = selectedTarget();
  if (!target || state.pending) return;
  const panes = state.targets.filter((p) =>
    kind === "close_window"
      ? p.windowId === target.windowId
      : kind === "close_tab"
        ? p.windowId === target.windowId && p.tabId === target.tabId
        : p.id === target.id,
  );
  const noun =
    kind === "close_window" ? "window" : kind === "close_tab" ? "tab" : "pane";
  const name =
    kind === "close_window"
      ? windowName(target)
      : kind === "close_tab"
        ? tabName(target)
        : targetName(target);
  state.closeTarget = { ...target, kind };
  byId("close-title").textContent = `Close “${name}”?`;
  byId("close-description").textContent =
    `This closes the ${noun} in ${windowName(target)}, including ${panes.length} ${panes.length === 1 ? "pane" : "panes"}:`;
  byId("close-pane-list").replaceChildren(
    ...panes.map((p) => {
      const li = document.createElement("li");
      li.textContent = targetName(p);
      return li;
    }),
  );
  byId("confirm-close").textContent = `Close ${noun}`;
  byId("controls-dialog").close();
  byId("close-confirmation").showModal();
  byId("cancel-close").focus();
}
byId("close-tab").addEventListener("click", () => requestClose("close_tab"));
byId("cancel-close").addEventListener("click", () =>
  byId("close-confirmation").close(),
);
byId("confirm-close").addEventListener("click", () => {
  const t = state.closeTarget;
  state.closeTarget = null;
  byId("close-confirmation").close();
  if (
    !t ||
    !state.targets.some(
      (p) => p.id === t.id && p.windowId === t.windowId && p.tabId === t.tabId,
    )
  ) {
    message("That destination changed. Refresh and review it again.");
    return;
  }
  const action =
    t.kind === "close_pane"
      ? { kind: t.kind, targetId: t.id }
      : t.kind === "close_window"
        ? { kind: t.kind, windowId: t.windowId }
        : { kind: t.kind, windowId: t.windowId, tabId: t.tabId };
  performAction(action);
});
byId("logout").addEventListener("click", async () => {
  if (state.pending) return;
  try {
    await api("/api/logout", { method: "POST", body: "{}" });
    byId("compose").value = "";
    state.drafts.clear();
    byId("action-notice").hidden = true;
    requirePairing(
      "Disconnected. Open a new pairing link from your Mac to reconnect.",
    );
  } catch (error) {
    message("Disconnect could not be confirmed. " + error.message);
  }
});

window.addEventListener("offline", () => {
  cancelSnapshot();
  stopFollow();
  setNetwork("offline");
  if (state.csrf)
    message(
      "You’re offline. Your draft is kept. Reconnect before sending input.",
    );
});
window.addEventListener("beforeunload", (event) => {
  if (state.pending && !state.pending.persisted) {
    event.preventDefault();
    event.returnValue = "";
  }
});
window.addEventListener("hashchange", () => {
  const fragment = new URLSearchParams(location.hash.slice(1));
  const token = fragment.get("token") || "";
  if (fragment.has("token"))
    history.replaceState(null, "", location.pathname + location.search);
  if (token) pair(token);
});
window.addEventListener("online", () => {
  if (state.csrf) {
    refreshTargets();
    if (state.pending) checkReceipt();
  }
});
document.addEventListener("visibilitychange", () => {
  if (document.hidden) {
    clearTimeout(state.targetTimer);
    cancelSnapshot();
    return;
  }
  if (!document.hidden && state.csrf) {
    refreshTargets();
    if (state.pending) checkReceipt();
    else readSnapshot();
  }
});
byId("connection").addEventListener("click", () => {
  byId("health-dialog").showModal();
  loadHealth();
});
byId("close-health").addEventListener("click", () =>
  byId("health-dialog").close(),
);
byId("health-refresh").addEventListener("click", () => {
  loadHealth();
  refreshTargets();
});
byId("retry-connection").addEventListener("click", () => {
  if (controlReady() && state.captureState === "unavailable") readSnapshot();
  else refreshTargets({ force: true });
});

for (const id of ["new-window", "picker-new-window", "controls-new-window"])
  byId(id).addEventListener("click", () => {
    byId("target-picker").close();
    byId("controls-dialog").close();
    performAction({ kind: "new_window" });
  });
byId("split-right").addEventListener("click", () => {
  const t = selectedTarget();
  if (t) performAction({ kind: "split", targetId: t.id, direction: "right" });
});
byId("open-controls").addEventListener("click", () => {
  renderDestination();
  byId("controls-dialog").showModal();
});
byId("close-controls").addEventListener("click", () =>
  byId("controls-dialog").close(),
);
byId("controls-focus").addEventListener("click", () => {
  const t = selectedTarget();
  if (t) {
    byId("controls-dialog").close();
    performAction({ kind: "focus", targetId: t.id });
  }
});
for (const button of document.querySelectorAll("[data-split]"))
  button.addEventListener("click", () => {
    const t = selectedTarget();
    if (t) {
      byId("controls-dialog").close();
      performAction({
        kind: "split",
        targetId: t.id,
        direction: button.dataset.split,
      });
    }
  });
for (const button of document.querySelectorAll("[data-action]"))
  button.addEventListener("click", () => {
    const t = selectedTarget();
    if (t) {
      byId("controls-dialog").close();
      performAction({ kind: "new_tab", windowId: t.windowId });
    }
  });
for (const button of document.querySelectorAll("[data-close]"))
  button.addEventListener("click", () => requestClose(button.dataset.close));
for (const id of ["target-search", "picker-search"])
  byId(id).addEventListener("input", () => {
    state.filter = byId(id).value;
    byId(id === "target-search" ? "picker-search" : "target-search").value =
      state.filter;
    renderTargets();
  });
byId("follow-output").addEventListener("click", () => {
  if (state.follow) stopFollow();
  else {
    state.follow = true;
    updateControls();
    readSnapshot();
    scheduleFollow();
  }
});
for (const button of document.querySelectorAll("[data-rename]"))
  button.addEventListener("click", () => {
    const t = selectedTarget();
    if (!t || !t.focused || state.pending) return;
    state.renameAction = { kind: button.dataset.rename, targetId: t.id };
    byId("rename-title").textContent =
      button.dataset.rename === "rename_tab" ? "Rename tab" : "Rename pane";
    byId("rename-input").value =
      button.dataset.rename === "rename_tab" ? t.tabTitle : t.title;
    byId("rename-message").textContent = "Leave blank to clear the title.";
    byId("controls-dialog").close();
    byId("rename-dialog").showModal();
    byId("rename-input").focus();
  });
byId("cancel-rename").addEventListener("click", () =>
  byId("rename-dialog").close(),
);
byId("rename-form").addEventListener("submit", (event) => {
  event.preventDefault();
  const action = state.renameAction;
  const name = byId("rename-input").value;
  if (
    new TextEncoder().encode(name).length > 256 ||
    /[\u0000-\u001f\u007f-\u009f]/.test(name)
  ) {
    byId("rename-message").textContent =
      "Use a shorter title without control characters (256 bytes maximum).";
    return;
  }
  if (!action || !state.targets.some((t) => t.id === action.targetId)) {
    byId("rename-message").textContent = "This pane is no longer available.";
    return;
  }
  byId("rename-dialog").close();
  state.renameAction = null;
  performAction({ ...action, name });
});
function sendKey(key) {
  const t = selectedTarget();
  if (t) performAction({ kind: "key", targetId: t.id, key });
}
for (const letter of "abcdefghijklmnopqrstuvwxyz") {
  const button = document.createElement("button");
  button.type = "button";
  button.dataset.letter = letter;
  button.textContent = letter.toUpperCase();
  button.addEventListener("click", () =>
    sendKey(state.modifier + "-" + letter),
  );
  byId("letter-keys").append(button);
}
for (const button of document.querySelectorAll("[data-modifier]"))
  button.addEventListener("click", () => {
    state.modifier = button.dataset.modifier;
    for (const item of document.querySelectorAll("[data-modifier]"))
      item.setAttribute("aria-pressed", String(item === button));
    labelLetters();
  });
function labelLetters() {
  for (const button of document.querySelectorAll("[data-letter]"))
    button.setAttribute(
      "aria-label",
      `${state.modifier === "ctrl" ? "Control" : "Alt"} ${button.dataset.letter.toUpperCase()}`,
    );
}
labelLetters();
for (const [label, key] of [
  ["Esc", "esc"],
  ["Tab", "tab"],
  ["Shift Tab", "shift-tab"],
  ["Enter", "enter"],
  ["←", "left"],
  ["↑", "up"],
  ["↓", "down"],
  ["→", "right"],
  ["Home", "home"],
  ["End", "end"],
  ["Page ↑", "pgup"],
  ["Page ↓", "pgdn"],
  ["Backspace", "backspace"],
  ["Delete", "delete"],
  ["Space", "space"],
]) {
  const button = document.createElement("button");
  button.type = "button";
  button.dataset.key = key;
  button.textContent = label;
  button.setAttribute(
    "aria-label",
    {
      pgup: "Page up",
      pgdn: "Page down",
      left: "Left arrow",
      right: "Right arrow",
      up: "Up arrow",
      down: "Down arrow",
    }[key] || label,
  );
  button.addEventListener("click", () => sendKey(key));
  byId("navigation-keys").append(button);
}
for (let n = 1; n <= 12; n++) {
  const button = document.createElement("button");
  button.type = "button";
  button.dataset.key = `f${n}`;
  button.textContent = `F${n}`;
  button.addEventListener("click", () => sendKey(`f${n}`));
  byId("function-keys").append(button);
}
byId("open-keyboard").addEventListener("click", () => {
  renderDestination();
  byId("keyboard-dialog").showModal();
});
byId("close-keyboard").addEventListener("click", () =>
  byId("keyboard-dialog").close(),
);
setInterval(() => {
  if (state.csrf && !document.hidden) renderReadiness();
  if (
    state.snapshotTarget &&
    Number.isFinite(state.lastCapturedAt) &&
    state.lastCapturedAt > 0
  ) {
    const age = Math.max(
      0,
      Math.floor((Date.now() - state.lastCapturedAt) / 1000),
    );
    byId("snapshot-time").textContent = age < 2 ? "just now" : `${age}s ago`;
    byId("snapshot-time").parentElement.parentElement.classList.toggle(
      "stale",
      state.snapshotStale || age > 15 || !controlReady(),
    );
  }
}, 1000);

(async function start() {
  restorePending();
  if (fragmentToken) {
    const token = fragmentToken;
    fragmentToken = "";
    await pair(token);
    return;
  }
  try {
    authenticated(await api("/api/session"));
  } catch (error) {
    pairMessage(
      error.status === 401
        ? "A one-time pairing keeps this browser connected securely."
        : error.message,
      error.status !== 401,
    );
  }
})();
