import express from "express";
import { exec } from "child_process";

const app = express();

enum ExportFormat {
  CSV = "csv",
  JSON = "json",
}

function isExportFormat(value: string): value is ExportFormat {
  return Object.values(ExportFormat).includes(value as ExportFormat);
}

app.get("/export", (req, res) => {
  const format = req.query.format as string;
  if (!format || !isExportFormat(format)) {
    res.status(400).send("invalid format");
    return;
  }
  exec(`report-tool --format ${format}`, (err, stdout) => { // safe: enum validation limits input to known constants
    if (err) {
      res.status(500).send("export failed");
      return;
    }
    res.send(stdout);
  });
});
