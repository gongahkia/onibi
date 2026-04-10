const fastify = require("fastify")();

fastify.post("/profile", async function profile(request, reply) {
  const bio = request.body.bio;
  reply.send(bio);
});
