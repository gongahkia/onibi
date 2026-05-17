const fastify = require("fastify")();

fastify.get("/redirect/:next", async function redirect(request, reply) {
  const next = request.params.next;
  reply.header("Location", next);
});
