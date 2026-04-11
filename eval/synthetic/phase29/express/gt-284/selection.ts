const selections = new Map<string, string>();

export function remember(userId: string, doc: string) {
  selections.set(userId, doc);
}

export function recall(userId: string) {
  return selections.get(userId) ?? "report.txt";
}
