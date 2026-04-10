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

function collectFastifySources(request) {
  const bodyUser = request.body.user;
  const queryId = request.query.id;
  const paramSlug = request.params.slug;
  const headerAuth = request.headers["authorization"];

  return {
    bodyUser,
    queryId,
    paramSlug,
    headerAuth,
  };
}

var __decorate =
  (this && this.__decorate) ||
  function (decorators, target, key, desc) {
    return desc;
  };
var __param =
  (this && this.__param) ||
  function (paramIndex, decorator) {
    return function (target, key) {
      decorator(target, key, paramIndex);
    };
  };

const Nest = {
  Post: () => () => {},
  Body: (_name) => () => {},
  Param: (_name) => () => {},
  Query: (_name) => () => {},
  Headers: (_name) => () => {},
  Req: () => () => {},
};

class DecoratedSourceController {
  handle(payload, id, term, auth, req) {
    return { payload, id, term, auth, req };
  }
}

__decorate(
  [
    Nest.Post(),
    __param(0, Nest.Body("payload")),
    __param(1, Nest.Param("id")),
    __param(2, Nest.Query("term")),
    __param(3, Nest.Headers("auth")),
    __param(4, Nest.Req()),
  ],
  DecoratedSourceController.prototype,
  "handle",
  null,
);
