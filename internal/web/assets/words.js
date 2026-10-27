"use strict";

// Splits a command line the way a POSIX shell separates words, without any
// expansion: no variables, globs, tildes or command substitution.
function splitWords(line) {
  const words = [];
  let word = "";
  let started = false;
  let quote = "";
  for (let i = 0; i < line.length; i++) {
    const ch = line[i];
    if (quote === "'") {
      if (ch === "'") quote = "";
      else word += ch;
    } else if (quote === '"') {
      if (ch === '"') quote = "";
      else if (
        ch === "\\" &&
        i + 1 < line.length &&
        '"\\$`\n'.includes(line[i + 1])
      )
        word += line[++i];
      else word += ch;
    } else if (ch === "'" || ch === '"') {
      quote = ch;
      started = true;
    } else if (ch === "\\") {
      if (i + 1 >= line.length)
        throw new Error("Remove the trailing backslash.");
      word += line[++i];
      started = true;
    } else if (/\s/.test(ch)) {
      if (started) words.push(word);
      word = "";
      started = false;
    } else {
      word += ch;
      started = true;
    }
  }
  if (quote)
    throw new Error(
      quote === "'" ? "Close the single quote." : "Close the double quote.",
    );
  if (started) words.push(word);
  return words;
}

if (typeof module !== "undefined") module.exports = { splitWords };
