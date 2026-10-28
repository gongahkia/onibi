// A real raw-mode terminal program, not a simulated multiplexer. It reports
// input literally and never evaluates it as a shell command.
"use strict";
const { createHash } = require("node:crypto");
process.stdin.setRawMode(true);
process.stdin.setEncoding("utf8");
const print = (text) => process.stdout.write(text + "\r\n");
print("READY");
print("CWD:" + process.cwd());
for (const arg of process.argv.slice(2)) print("ARG:" + JSON.stringify(arg));
process.stdout.write("Continue? [y/N]");
let line = "";
process.stdin.on("data", (text) => {
  for (const char of text) {
    if (char === "\x03") {
      line = "";
      print("\r\nINTERRUPTED");
    } else if (char === "\r" || char === "\n") {
      if (line.startsWith("DIGEST:"))
        print("\r\nSHA256:" + createHash("sha256").update(line).digest("hex"));
      else print("\r\nINPUT:" + JSON.stringify(line));
      if (line === "prompt") process.stdout.write("Continue? [y/N]");
      if (line === "bell") process.stdout.write("\x07");
      if (line === "color") print("\x1b[31mCOLORED\x1b[0m");
      if (line === "history")
        for (let i = 0; i < 240; i++) print("HISTORY-" + i);
      if (line === "exit") process.exit(0);
      line = "";
    } else line += char;
  }
});
