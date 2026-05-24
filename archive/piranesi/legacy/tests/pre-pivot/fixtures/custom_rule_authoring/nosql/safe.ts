export async function handler(req, db) {
  const filter = req.body.filter;
  const safeFilter = mongoSanitize(filter);
  return db.collection.find({ $where: safeFilter });
}
