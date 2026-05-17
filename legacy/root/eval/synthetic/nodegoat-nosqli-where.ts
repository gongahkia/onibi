// NodeGoat-style regression: req.query scalar reaches a Mongo $where query.
// This locks coverage for the historical miss documented in docs/examples/nodegoat.md.
export function getByUserIdAndThresholdVulnerable(req, allocationsCollection, parsedUserId) {
  const threshold = req.query.threshold;
  const query = {
    $where: `this.userId == ${parsedUserId} && this.stocks > '${threshold}'`,
  };
  return allocationsCollection.find(query).toArray();
}

// Safe neighbor: no $where usage, static query shape.
export function getByUserIdAndThresholdSafe(req, allocationsCollection, parsedUserId) {
  const thresholdValue = Number.parseInt(String(req.query.threshold ?? "0"), 10);
  const query = {
    userId: parsedUserId,
    exactThreshold: thresholdValue,
  };
  return allocationsCollection.find(query).toArray();
}
