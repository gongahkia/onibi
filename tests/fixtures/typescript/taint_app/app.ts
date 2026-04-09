import express from "express";

function escape(value: string): string {
  return value;
}

export function userHandler(req: any, res: any, db: any): void {
  const userId = req.body.user;
  const unsafeQuery = "SELECT * FROM users WHERE id = '" + userId + "'";
  db.query(unsafeQuery);
  const safeMarkup = escape(userId);
  res.send(safeMarkup);
}

export function commandHandler(req: any, child: any): void {
  const cmd = req.query.cmd;
  child.exec(cmd);
}

const app = express();
app.post("/users", userHandler);
app.get("/cmd", commandHandler);

export default app;

