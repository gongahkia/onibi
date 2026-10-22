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
  connected: false,
  targetsLoading: false,
  snapshotVersion: 0,
  snapshotTarget: "",
  pending: null,
  receiptTimer: null,
  closeTarget: null,
};
const actionButtons = [
  "send-input",
  "paste-input",
  "new-tab",
  "close-tab",
  "focus-terminal",
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
      !["input", "key", "focus", "new_tab", "close_tab"].includes(
        saved.action.kind,
      )
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
  constructor(message, status = 0, code = "") {
    super(message);
    this.status = status;
    this.code = code;
  }
}

async function api(path, options = {}) {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 40000);
  try {
    const response = await fetch(path, {
      ...options,
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
      );
    return body;
  } catch (error) {
    if (error instanceof APIError) throw error;
    throw new APIError("Connection to your Mac was interrupted.");
  } finally {
    clearTimeout(timeout);
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
function setConnected(connected) {
  state.connected = connected;
  byId("connection").classList.toggle("disconnected", !connected);
  byId("connection-label").textContent = connected
    ? "Connected"
    : "Disconnected";
  updateControls();
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
    state.csrf && target && state.connected && !state.pending,
  );
  for (const id of actionButtons) byId(id).disabled = !canAct;
  byId("send-input").disabled = !canAct || byId("compose").value.length === 0;
  byId("paste-input").disabled = !canAct || byId("compose").value.length === 0;
  // Preserve and permit editing a draft during an outage. Never submit it then.
  byId("compose").disabled = !state.csrf || !target;
  byId("read-output").disabled = !state.csrf || !target;
  byId("logout").disabled = Boolean(state.pending);
  for (const button of document.querySelectorAll("[data-key]"))
    button.disabled = !canAct;
}

function authenticated(session) {
  if (!session || typeof session.csrf !== "string" || !session.csrf)
    throw new APIError("This browser could not establish a session.");
  state.csrf = session.csrf;
  byId("pair-token").value = "";
  byId("pair-view").hidden = true;
  byId("workspace").hidden = false;
  byId("session-controls").hidden = false;
  setConnected(true);
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
  state.csrf = "";
  state.targets = [];
  state.selectedId = "";
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
  byId("close-confirmation").close();
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
        : "No Ghostty panes available. Open a terminal on your Mac, then refresh.";
      container.append(empty);
      continue;
    }
    const windows = new Map();
    for (const target of state.targets) {
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
            `${targetName(target)}, ${tabName(target)}, ${windowName(target)}`,
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
  if (!target) byId("focus-terminal").hidden = true;
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

async function refreshTargets() {
  if (!state.csrf || state.targetsLoading) return;
  state.targetsLoading = true;
  if (!state.targets.length) renderTargets();
  try {
    const data = await api("/api/targets");
    if (!state.csrf) return;
    if (!Array.isArray(data.targets))
      throw new APIError("The terminal list is unavailable.");
    state.targets = data.targets.filter(
      (target) =>
        typeof target.id === "string" &&
        target.id &&
        target.windowId &&
        target.tabId,
    );
    if (state.selectedId && !selectedTarget()) {
      state.selectedId = "";
      state.snapshotVersion++;
      state.snapshotTarget = "";
      byId("terminal-output").textContent = "";
      showEmpty(
        "That pane is no longer available",
        "Choose another destination. Your draft has been kept.",
      );
      byId("snapshot-state").textContent = "Destination unavailable";
      byId("snapshot-time").textContent = "";
    }
    setConnected(true);
    message("");
  } catch (error) {
    if (error.status === 401)
      requirePairing(
        "Your session expired. Reconnect with a fresh pairing link from your Mac.",
      );
    else {
      setConnected(false);
      message(
        error.message + " Refresh terminals to reconnect. Your draft is kept.",
      );
    }
  } finally {
    state.targetsLoading = false;
    renderTargets();
    renderDestination();
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
  state.selectedId = id;
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
  if (!state.csrf || !target) return;
  const version = ++state.snapshotVersion;
  byId("read-output").disabled = true;
  try {
    const snapshot = await api(
      "/api/snapshot?targetId=" + encodeURIComponent(target.id),
    );
    if (
      version !== state.snapshotVersion ||
      target.id !== state.selectedId ||
      !state.csrf
    )
      return;
    if (snapshot.targetId !== target.id || typeof snapshot.text !== "string")
      throw new APIError("The snapshot destination could not be verified.");
    state.snapshotTarget = target.id;
    byId("terminal-output").textContent =
      snapshot.text || "(No visible output.)";
    byId("terminal-output").hidden = false;
    byId("output-empty").hidden = true;
    byId("focus-terminal").hidden = true;
    byId("snapshot-state").textContent = "Read on your Mac";
    const captured = new Date(snapshot.capturedAt);
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
    byId("snapshot-state").parentElement.classList.remove("stale");
    message("");
    setConnected(true);
  } catch (error) {
    if (version !== state.snapshotVersion || target.id !== state.selectedId)
      return;
    if (error.status === 401) {
      requirePairing(
        "Your session expired. Your draft is kept; reconnect to continue.",
      );
      return;
    }
    const focusRequired =
      error.code === "focus_required" || error.status === 409;
    byId("focus-terminal").hidden = !focusRequired;
    byId("snapshot-state").textContent =
      state.snapshotTarget === target.id
        ? "Last snapshot · not live"
        : "Output unavailable";
    byId("snapshot-state").parentElement.classList.add("stale");
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
    // Snapshot support and terminal control are separate capabilities. An
    // HTTP error still proves the Mac is reachable; only transport loss does not.
    setConnected(error.status !== 0);
  } finally {
    if (version === state.snapshotVersion) updateControls();
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
  } else {
    if (
      pending.action.kind === "input" &&
      state.selectedId === pending.action.targetId &&
      byId("compose").value === pending.draft
    )
      byId("compose").value = "";
    actionNotice(
      "Done",
      receipt.result && receipt.result.message
        ? receipt.result.message
        : "Your Mac confirmed the action.",
    );
    await refreshTargets();
    if (
      pending.action.kind === "new_tab" &&
      receipt.result &&
      state.targets.some((target) => target.id === receipt.result.targetId)
    )
      selectTarget(receipt.result.targetId);
    else if (state.selectedId === pending.action.targetId) readSnapshot();
  }
  updateControls();
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
      if (!error.status) setConnected(false);
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
  if (!state.csrf || !state.connected || !target || state.pending) return;
  if (!crypto.randomUUID) {
    message(
      "This browser needs a private HTTPS connection to control Ghostty.",
    );
    return;
  }
  const pending = { id: crypto.randomUUID(), action, draft };
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
      setConnected(false);
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
byId("compose").addEventListener("input", updateControls);
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
byId("close-tab").addEventListener("click", () => {
  const target = selectedTarget();
  if (!target || state.pending) return;
  state.closeTarget = { ...target };
  byId("close-title").textContent = `Close “${tabName(target)}”?`;
  const panes = state.targets.filter(
    (pane) => pane.windowId === target.windowId && pane.tabId === target.tabId,
  );
  byId("close-description").textContent =
    `This closes the whole tab in ${windowName(target)}, including ${panes.length} ${panes.length === 1 ? "pane" : "panes"}:`;
  byId("close-pane-list").replaceChildren(
    ...panes.map((pane) => {
      const li = document.createElement("li");
      li.textContent = targetName(pane);
      return li;
    }),
  );
  byId("close-confirmation").showModal();
  byId("cancel-close").focus();
});
byId("cancel-close").addEventListener("click", () =>
  byId("close-confirmation").close(),
);
byId("confirm-close").addEventListener("click", () => {
  const target = state.closeTarget;
  byId("close-confirmation").close();
  state.closeTarget = null;
  if (
    !target ||
    !state.targets.some(
      (pane) =>
        pane.windowId === target.windowId && pane.tabId === target.tabId,
    )
  ) {
    message("That tab is no longer available.");
    return;
  }
  performAction({
    kind: "close_tab",
    windowId: target.windowId,
    tabId: target.tabId,
  });
});
byId("logout").addEventListener("click", async () => {
  if (state.pending) return;
  try {
    await api("/api/logout", { method: "POST", body: "{}" });
    byId("compose").value = "";
    byId("action-notice").hidden = true;
    requirePairing(
      "Disconnected. Open a new pairing link from your Mac to reconnect.",
    );
  } catch (error) {
    message("Disconnect could not be confirmed. " + error.message);
  }
});

window.addEventListener("offline", () => {
  setConnected(false);
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
  if (!document.hidden && state.csrf) {
    refreshTargets();
    if (state.pending) checkReceipt();
    else readSnapshot();
  }
});
setInterval(() => {
  if (!document.hidden && state.csrf) refreshTargets();
}, 10000);

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
