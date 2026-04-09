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
  request(url);
}
