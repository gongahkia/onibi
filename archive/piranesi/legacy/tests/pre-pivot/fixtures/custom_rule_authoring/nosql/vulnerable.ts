export async function handler(req, db) {
  const filter = req.body.filter;
  return db.collection.find({ $where: filter });
}
