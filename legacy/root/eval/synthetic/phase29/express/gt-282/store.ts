const reportPrefs = new Map<string, string>();

export function saveSort(userId: string, sort: string) {
  reportPrefs.set(userId, sort);
}

export function loadSort(userId: string) {
  return reportPrefs.get(userId) ?? "created_at";
}
