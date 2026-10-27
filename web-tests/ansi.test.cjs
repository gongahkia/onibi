const test = require("node:test");
const assert = require("node:assert/strict");
const { parseAnsi } = require("../internal/web/assets/ansi.js");

test("SGR runs keep text literal and track 16, 256 and truecolour", () => {
  assert.deepEqual(parseAnsi("plain \x1b[1;31mbold red\x1b[0m <b>&amp;"), [
    { text: "plain ", style: {} },
    { text: "bold red", style: { bold: true, fg: { index: 1 } } },
    { text: " <b>&amp;", style: {} },
  ]);
  assert.deepEqual(parseAnsi("\x1b[38;5;196mx")[0].style, {
    fg: { rgb: [255, 0, 0] },
  });
  assert.deepEqual(parseAnsi("\x1b[48;5;244mx")[0].style, {
    bg: { rgb: [128, 128, 128] },
  });
  assert.deepEqual(parseAnsi("\x1b[38;2;1;2;3mx")[0].style, {
    fg: { rgb: [1, 2, 3] },
  });
  assert.deepEqual(parseAnsi("\x1b[38:2::1:2:3mx")[0].style, {
    fg: { rgb: [1, 2, 3] },
  });
  assert.deepEqual(parseAnsi("\x1b[94;7mx")[0].style, {
    fg: { index: 12 },
    inverse: true,
  });
  assert.deepEqual(parseAnsi("\x1b[1;2mx\x1b[22my"), [
    { text: "x", style: { bold: true, dim: true } },
    { text: "y", style: {} },
  ]);
});

test("non-SGR sequences and control characters are dropped", () => {
  assert.deepEqual(
    parseAnsi(
      "a\x1b[2J\x1b[?25lb\x1b]8;;https://x.test\x07link\x1b]8;;\x1b\\c\x1b(Bd\x07\x00\r\x9be\tf\n",
    ),
    [{ text: "ablinkcde\tf\n", style: {} }],
  );
  assert.deepEqual(parseAnsi("cut \x1b[31"), [{ text: "cut ", style: {} }]);
  assert.deepEqual(parseAnsi("\x1b[38;5;999mx")[0].style, {});
  assert.deepEqual(parseAnsi("\x1b[38;2;1mx")[0].style, {});
});
