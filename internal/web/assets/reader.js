"use strict";

// Only harmless display preferences persist. Terminal text remains in memory.
const readerPreferenceKey = "onibi.reader.v1";
const readerSizes = [12, 13, 14, 16, 18, 20, 22];
const readerPreference = { size: 13, wrap: false };
try {
  const stored = JSON.parse(
    localStorage.getItem(readerPreferenceKey) || "null",
  );
  if (readerSizes.includes(stored?.size)) readerPreference.size = stored.size;
  if (typeof stored?.wrap === "boolean") readerPreference.wrap = stored.wrap;
} catch {
  /* Private storage can be unavailable. */
}
function applyReaderPreferences(save = false) {
  byId("reader-size").value = String(readerPreference.size);
  byId("terminal-output").dataset.font = String(readerPreference.size);
  byId("terminal-output").classList.toggle("wrap-lines", readerPreference.wrap);
  byId("reader-wrap").setAttribute(
    "aria-pressed",
    String(readerPreference.wrap),
  );
  if (save)
    try {
      localStorage.setItem(
        readerPreferenceKey,
        JSON.stringify(readerPreference),
      );
    } catch {
      /* Keep working in memory. */
    }
}
applyReaderPreferences();
byId("reader-size").addEventListener("change", () => {
  const size = Number(byId("reader-size").value);
  if (readerSizes.includes(size)) {
    readerPreference.size = size;
    applyReaderPreferences(true);
  }
});
byId("reader-wrap").addEventListener("click", () => {
  readerPreference.wrap = !readerPreference.wrap;
  applyReaderPreferences(true);
});
byId("reader-top").addEventListener("click", () => {
  byId("terminal-output").scrollTop = 0;
});
byId("reader-bottom").addEventListener("click", () => {
  const output = byId("terminal-output");
  output.scrollTop = output.scrollHeight;
});
const readerHome = document.createComment("output card location");
byId("output-card").before(readerHome);
byId("reader-expand").addEventListener("click", () => {
  if (!state.snapshotTarget || state.snapshotTarget !== state.selectedId)
    return;
  renderDestination();
  byId("reader-mount").append(byId("output-card"));
  byId("reader-dialog").showModal();
  byId("reader-close").focus();
});
byId("reader-close").addEventListener("click", () =>
  byId("reader-dialog").close(),
);
byId("reader-dialog").addEventListener("close", () => {
  readerHome.after(byId("output-card"));
  if (state.csrf) byId("reader-expand").focus();
});
byId("reader-copy").addEventListener("click", async () => {
  if (!state.snapshotText || state.snapshotTarget !== state.selectedId) return;
  // Freeze the snapshot for selection/copy; a scheduled read must not replace
  // the text during a long press or while the browser asks for permission.
  stopFollow();
  cancelSnapshot();
  const text = state.snapshotText;
  const generation = state.authGeneration,
    target = state.snapshotTarget;
  byId("reader-copy").disabled = true;
  let detail;
  try {
    if (!navigator.clipboard?.writeText)
      throw new Error("Clipboard unavailable");
    await navigator.clipboard.writeText(text);
    recordRecall(text, "Copied");
    detail = "Snapshot copied. Follow is paused.";
  } catch {
    if (generation !== state.authGeneration || target !== state.snapshotTarget)
      return;
    const range = document.createRange();
    range.selectNodeContents(byId("terminal-output"));
    const selection = window.getSelection();
    selection.removeAllRanges();
    selection.addRange(range);
    detail =
      "Clipboard access was unavailable. Text is selected; use your browser’s Copy command. Follow is paused.";
  } finally {
    if (
      generation === state.authGeneration &&
      target === state.snapshotTarget
    ) {
      if (detail) {
        byId("reader-message").textContent = detail;
        byId("reader-message").hidden = false;
      }
      updateControls();
    }
  }
});
byId("terminal-output").addEventListener("pointerdown", () => {
  if (!state.follow) return;
  stopFollow();
  cancelSnapshot();
  byId("reader-message").textContent =
    "Follow paused while you read or select text.";
  byId("reader-message").hidden = false;
});
