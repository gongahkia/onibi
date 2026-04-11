import escapeHtml from "escape-html";

export class ViewService {
  renderTerm(term: string) {
    const safe = escapeHtml(term);
    return `<div class="term">${safe}</div>`;
  }
}
