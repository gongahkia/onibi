const test = require("node:test");
const assert = require("node:assert/strict");
const { splitWords } = require("../internal/web/assets/words.js");

test("spaces separate words and runs of whitespace collapse", () => {
  assert.deepEqual(splitWords("  claude   --model  opus "), [
    "claude",
    "--model",
    "opus",
  ]);
  assert.deepEqual(splitWords(""), []);
  assert.deepEqual(splitWords("shell"), ["shell"]);
});

test("quotes keep spaces and join adjacent pieces", () => {
  assert.deepEqual(splitWords(`codex "fix the build" 'it''s'`), [
    "codex",
    "fix the build",
    "its",
  ]);
  assert.deepEqual(splitWords(`a"b c"d`), ["ab cd"]);
  assert.deepEqual(splitWords(`run "" ''`), ["run", "", ""]);
});

test("single quotes are literal; double quotes allow a few escapes", () => {
  assert.deepEqual(splitWords(`'$HOME \\n "x"'`), [`$HOME \\n "x"`]);
  assert.deepEqual(splitWords(`"say \\"hi\\" \\\\ \\$x \\n"`), [
    `say "hi" \\ $x \\n`,
  ]);
});

test("backslash outside quotes escapes the next character", () => {
  assert.deepEqual(splitWords(`git commit -m it\\'s\\ done`), [
    "git",
    "commit",
    "-m",
    "it's done",
  ]);
});

test("nothing is expanded", () => {
  assert.deepEqual(splitWords("echo $HOME ~ *.go $(id) `id`"), [
    "echo",
    "$HOME",
    "~",
    "*.go",
    "$(id)",
    "`id`",
  ]);
});

test("unfinished quoting is an error, not a guess", () => {
  assert.throws(() => splitWords(`say "hi`), /double quote/);
  assert.throws(() => splitWords(`say 'hi`), /single quote/);
  assert.throws(() => splitWords(`say hi\\`), /trailing backslash/);
});
