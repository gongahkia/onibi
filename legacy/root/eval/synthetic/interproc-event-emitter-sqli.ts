type Handler = (payload: string) => void;

class Emitter {
  on(_event: string, _handler: Handler) {}

  emit(_event: string, _payload: string) {}
}

const emitter = new Emitter();
const db = {
  query(sql: string) {
    return sql;
  },
};

emitter.on("data", (payload) => {
  db.query(payload);
});

export function eventEmitterSqli(req: { body: { sql: string } }) {
  emitter.emit("data", req.body.sql);
}
