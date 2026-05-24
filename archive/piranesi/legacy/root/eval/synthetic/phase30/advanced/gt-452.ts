const db = {
  async insertName(_name: string) {
  },
  async names() {
    return ["attacker"]; 
  },
  async query(_sql: string) {
  },
};

export async function saveProfile(req: { body: { name: string } }) {
  await db.insertName(req.body.name);
}

export async function buildReport() {
  const names = await db.names();
  await db.query(`SELECT * FROM activity WHERE user_name = '${names[0]}'`); // sink
}
