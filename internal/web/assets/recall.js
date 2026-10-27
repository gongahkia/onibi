"use strict";

// Recent sends and copies live in page memory only, like drafts. They are
// never written to storage and vanish when the page closes.
const recallLimit = 20;
const recallTextLimit = 16 * 1024;
const recallItems = [];

function recordRecall(text, source) {
  if (typeof text !== "string" || !text.trim()) return;
  if (new TextEncoder().encode(text).length > recallTextLimit) return;
  const existing = recallItems.findIndex((item) => item.text === text);
  if (existing >= 0) recallItems.splice(existing, 1);
  recallItems.unshift({ text, source });
  recallItems.length = Math.min(recallItems.length, recallLimit);
  renderRecall();
}
function clearRecall() {
  recallItems.length = 0;
  renderRecall();
}
function renderRecall() {
  const compose = byId("compose");
  byId("recall").hidden = recallItems.length === 0;
  byId("recall-count").textContent = String(recallItems.length);
  byId("recall-list").replaceChildren(
    ...recallItems.map((item) => {
      const row = document.createElement("li");
      const button = document.createElement("button");
      button.type = "button";
      button.className = "secondary recall-item";
      button.disabled = compose.disabled;
      const source = document.createElement("span");
      source.className = "recall-source";
      source.textContent = item.source;
      const text = document.createElement("span");
      text.className = "recall-text";
      text.textContent = item.text;
      button.append(source, text);
      button.addEventListener("click", () => insertRecall(item.text));
      row.append(button);
      return row;
    }),
  );
}
function insertRecall(text) {
  const compose = byId("compose");
  if (compose.disabled) return;
  compose.setRangeText(
    text,
    compose.selectionStart,
    compose.selectionEnd,
    "end",
  );
  compose.dispatchEvent(new Event("input"));
  compose.focus();
}
byId("recall-clear").addEventListener("click", clearRecall);
document.addEventListener("copy", () => {
  const selection = window.getSelection();
  if (byId("terminal-output").contains(selection.anchorNode))
    recordRecall(selection.toString(), "Copied");
});
new MutationObserver(renderRecall).observe(byId("compose"), {
  attributes: true,
  attributeFilter: ["disabled"],
});
