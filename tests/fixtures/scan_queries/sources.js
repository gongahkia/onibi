function collectSources(req) {
  const bodyUser = req.body.user;
  const queryId = req.query.id;
  const paramSlug = req.params.slug;
  const headerAuth = req.headers["authorization"];
  const cookieSid = req.cookies.sid;
  const envToken = process.env.TOKEN;
  const parsedUrl = new URL(req.url);
  const parsedSearch = new URLSearchParams(req.query);

  return {
    bodyUser,
    queryId,
    paramSlug,
    headerAuth,
    cookieSid,
    envToken,
    parsedUrl,
    parsedSearch,
  };
}
