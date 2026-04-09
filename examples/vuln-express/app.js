const express = require("express");
const fs = require("fs");
const path = require("path");
const { execSync } = require("child_process");

const app = express();
const docsDir = path.join(__dirname, "files");

app.use(express.json());
app.use(express.urlencoded({ extended: false }));

function query(sql) {
  if (sql.includes("OR 1=1")) {
    return [
      { id: 1, name: "alice" },
      { id: 2, name: "bob" },
    ];
  }
  if (sql.includes("'")) {
    throw new Error(`SQL syntax error near ${sql}`);
  }
  return [{ id: 1, name: "alice" }];
}

function homeHandler(_req, res) {
  res.send("ok");
}

function searchHandler(req, res) {
  const q = req.query.q;
  res.send(`<html><body>Search results for ${q}</body></html>`);
}

function usersHandler(req, res) {
  const name = req.query.name;
  const rows = query(`SELECT * FROM users WHERE name = '${name}'`);
  res.json({ rows });
}

function filesHandler(req, res) {
  const file = req.query.file;
  const fullPath = path.join(docsDir, file);
  const content = fs.readFileSync(fullPath, "utf8");
  res.json({ content });
}

function shellHandler(req, res) {
  const cmd = req.query.cmd;
  const output = execSync(`echo ${cmd}`, { encoding: "utf8" });
  res.json({ output });
}

async function proxyHandler(req, res) {
  const target = req.query.url;
  const response = await fetch(target, { signal: AbortSignal.timeout(2000) });
  const body = await response.text();
  res.json({ body });
}

app.get("/", homeHandler);
app.get("/search", searchHandler);
app.get("/users", usersHandler);
app.get("/files", filesHandler);
app.get("/shell", shellHandler);
app.get("/proxy", proxyHandler);

app.listen(process.env.PORT || 3000);
