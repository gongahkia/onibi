declare const require: (name: string) => any;

const fastify = require("fastify")();

fastify.get("/echo", async function echoQuery(request, reply) {
  const name = request.query.name;
  reply.send(name);
});

fastify.get("/redirect/:next", async function redirectParam(request, reply) {
  const next = request.params.next;
  reply.header("Location", next);
  return reply.send("redirecting");
});

fastify.post(
  "/validated",
  {
    schema: {
      body: {
        type: "object",
        required: ["name"],
        properties: {
          name: { type: "string", maxLength: 50 },
        },
      },
    },
  },
  async function validatedBody(request, reply) {
    const name = request.body.name;
    reply.send(name);
  },
);

fastify.get("/headers", async function headerEcho(request, reply) {
  const origin = request.headers["x-forwarded-host"];
  reply.send(origin);
});
