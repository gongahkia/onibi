function triggerSinks(
  userId,
  cmd,
  script,
  markup,
  pathValue,
  url,
  db,
  prisma,
  sql,
  child,
  res,
  fs,
  axios,
  http,
  https,
  needle,
  app,
  router,
  handlers,
) {
  db.query(userId);
  prisma.$queryRaw(userId);
  prisma.$executeRaw(userId);
  sql.raw(userId);

  child.exec(cmd);
  child.execSync(cmd);
  child.spawn(cmd);
  child.spawnSync(cmd);

  eval(script);
  Function(script);

  dangerouslySetInnerHTML(markup);
  res.send(markup);
  res.render(markup);
  res.write(markup);

  fs.readFile(pathValue);
  fs.readFileSync(pathValue);
  fs.writeFile(pathValue, markup);
  fs.writeFileSync(pathValue, markup);

  fetch(url);
  axios.get(url);
  axios.post(url);
  http.get(url);
  https.get(url);
  needle.get(url);
  request(url);
  request.get(url);
  fetch(`https://internal.service.local/api/users/${userId}`);

  app.get("/health", handlers.health);
  app.post("/users", handlers.createUser);
  router.get("/health", handlers.health);
  router.post("/users", handlers.createUser);
}

function triggerFastifySinks(markup, redirectTo, reply) {
  reply.send(markup);
  reply.header("Location", redirectTo);
}
