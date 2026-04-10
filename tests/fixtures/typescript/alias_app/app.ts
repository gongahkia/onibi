import express from "express";

export function propertyAssignment(req: any, db: any): void {
  const taintedName = req.body.name;
  const profile: Record<string, unknown> = {};
  profile.name = taintedName;
  db.query(profile.name);
}

export function destructuring(req: any, db: any): void {
  const { email } = req.body;
  db.query(email);
}

export function renamedDestructuring(req: any, db: any): void {
  const payload = req.body;
  const { name: username } = payload;
  db.query(username);
}

export function spreadTracking(req: any, db: any): void {
  const merged = { ...req.body, safe: true };
  db.query(merged.sql);
}

export function commandAlias(req: any, child: any): void {
  const commandBox: Record<string, unknown> = {};
  commandBox.cmd = req.body.command;
  child.exec(commandBox.cmd);
}

const app = express();
app.post("/property", propertyAssignment);
app.post("/destructuring", destructuring);
app.post("/renamed", renamedDestructuring);
app.post("/spread", spreadTracking);
app.post("/command", commandAlias);

export default app;
