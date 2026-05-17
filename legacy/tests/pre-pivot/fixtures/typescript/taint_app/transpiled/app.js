const express = require("express");

function escape(value) {
  return value;
}

function userHandler(req, res, db) {
  const userId = req.body.user;
  const unsafeQuery = "SELECT * FROM users WHERE id = '" + userId + "'";
  db.query(unsafeQuery);
  const safeMarkup = escape(userId);
  res.send(safeMarkup);
}

function commandHandler(req, child) {
  const cmd = req.query.cmd;
  child.exec(cmd);
}

const app = express();
app.post("/users", userHandler);
app.get("/cmd", commandHandler);

module.exports = { app, userHandler, commandHandler };
//# sourceMappingURL=app.js.map

