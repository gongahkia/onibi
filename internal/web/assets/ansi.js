"use strict";

// Terminal captures arrive with SGR escapes. Text is only ever inserted as
// text nodes; styles use classes for the 16-colour palette and CSSOM
// properties for 256-colour and truecolour, which the style-src policy allows.
const ansiCube = [0, 95, 135, 175, 215, 255];

function ansiColour(n) {
  if (n < 16) return { index: n };
  if (n < 232) {
    const v = n - 16;
    return {
      rgb: [
        ansiCube[Math.floor(v / 36)],
        ansiCube[Math.floor(v / 6) % 6],
        ansiCube[v % 6],
      ],
    };
  }
  const grey = 8 + 10 * (n - 232);
  return { rgb: [grey, grey, grey] };
}

function ansiExtended(params, i) {
  const mode = params[i + 1];
  if (mode === 5 && params[i + 2] >= 0 && params[i + 2] <= 255)
    return { colour: ansiColour(params[i + 2]), next: i + 2 };
  if (
    mode === 2 &&
    params.slice(i + 2, i + 5).every((v) => v >= 0 && v <= 255) &&
    params.length >= i + 5
  )
    return { colour: { rgb: params.slice(i + 2, i + 5) }, next: i + 4 };
  return { colour: undefined, next: params.length };
}

function applySGR(style, raw) {
  // Colon sub-parameters (38:2::r:g:b) are flattened; an empty colour-space
  // slot after "2" is dropped so both spellings parse alike.
  const params = [];
  for (const part of (raw === "" ? "0" : raw).split(";")) {
    const sub = part.split(":");
    if (sub.length > 1 && sub[1] === "2" && sub.length === 6) sub.splice(2, 1);
    for (const value of sub) params.push(value === "" ? 0 : Number(value));
  }
  let next = { ...style };
  for (let i = 0; i < params.length; i++) {
    const p = params[i];
    if (!Number.isInteger(p)) return style;
    if (p === 0) next = {};
    else if (p === 1) next.bold = true;
    else if (p === 2) next.dim = true;
    else if (p === 3) next.italic = true;
    else if (p === 4) next.underline = true;
    else if (p === 7) next.inverse = true;
    else if (p === 9) next.strike = true;
    else if (p === 22) {
      delete next.bold;
      delete next.dim;
    } else if (p === 23) delete next.italic;
    else if (p === 24) delete next.underline;
    else if (p === 27) delete next.inverse;
    else if (p === 29) delete next.strike;
    else if (p >= 30 && p <= 37) next.fg = { index: p - 30 };
    else if (p >= 90 && p <= 97) next.fg = { index: p - 82 };
    else if (p === 39) delete next.fg;
    else if (p >= 40 && p <= 47) next.bg = { index: p - 40 };
    else if (p >= 100 && p <= 107) next.bg = { index: p - 92 };
    else if (p === 49) delete next.bg;
    else if (p === 38 || p === 48) {
      const { colour, next: end } = ansiExtended(params, i);
      if (colour) next[p === 38 ? "fg" : "bg"] = colour;
      i = end;
    }
  }
  return next;
}

// parseAnsi splits text into runs of uniformly styled text. Non-SGR control
// sequences (cursor movement, OSC titles and links, charset selection) and
// stray control characters are removed.
function parseAnsi(input) {
  const runs = [];
  let style = {};
  let text = "";
  const flush = () => {
    if (!text) return;
    const last = runs[runs.length - 1];
    if (last && JSON.stringify(last.style) === JSON.stringify(style))
      last.text += text;
    else runs.push({ text, style });
    text = "";
  };
  for (let i = 0; i < input.length; i++) {
    const ch = input[i];
    if (ch === "\x1b") {
      const kind = input[i + 1];
      if (kind === "[") {
        let j = i + 2;
        while (j < input.length && /[0-?]/.test(input[j])) j++;
        while (j < input.length && /[ -/]/.test(input[j])) j++;
        if (j >= input.length) break;
        if (input[j] === "m") {
          const raw = input.slice(i + 2, j);
          if (/^[0-9;:]*$/.test(raw)) {
            flush();
            style = applySGR(style, raw);
          }
        }
        i = j;
      } else if (kind === "]" || kind === "P" || kind === "_" || kind === "^") {
        let j = i + 2;
        while (
          j < input.length &&
          input[j] !== "\x07" &&
          !(input[j] === "\x1b" && input[j + 1] === "\\")
        )
          j++;
        i = input[j] === "\x1b" ? j + 1 : j;
      } else if (kind === "(" || kind === ")" || kind === "#") {
        i += 2;
      } else {
        i += 1;
      }
      continue;
    }
    if (ch === "\n" || ch === "\t" || ch >= " ") {
      if (ch !== "\x7f" && !(ch >= "\x80" && ch <= "\x9f")) text += ch;
    }
  }
  flush();
  return runs;
}

function ansiRGB(colour) {
  return `rgb(${colour.rgb[0]}, ${colour.rgb[1]}, ${colour.rgb[2]})`;
}

function renderAnsi(input) {
  const fragment = document.createDocumentFragment();
  for (const { text, style } of parseAnsi(input)) {
    if (!Object.keys(style).length) {
      fragment.append(document.createTextNode(text));
      continue;
    }
    const span = document.createElement("span");
    span.textContent = text;
    const fg = style.inverse ? style.bg : style.fg;
    const bg = style.inverse ? style.fg : style.bg;
    if (style.inverse) span.classList.add("ansi-inverse");
    for (const [colour, layer] of [
      [fg, "fg"],
      [bg, "bg"],
    ]) {
      if (!colour) continue;
      if (colour.index !== undefined)
        span.classList.add(`ansi-${layer}-${colour.index}`);
      else
        span.style.setProperty(
          layer === "fg" ? "color" : "background-color",
          ansiRGB(colour),
        );
    }
    for (const flag of ["bold", "dim", "italic", "underline", "strike"])
      if (style[flag]) span.classList.add("ansi-" + flag);
    fragment.append(span);
  }
  return fragment;
}

if (typeof module !== "undefined") module.exports = { parseAnsi };
