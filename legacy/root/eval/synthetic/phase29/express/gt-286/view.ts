import escapeHtml from "escape-html";

export function renderNotice(notice: string) {
  const safe = escapeHtml(notice);
  return `<p class="flash">${safe}</p>`;
}
