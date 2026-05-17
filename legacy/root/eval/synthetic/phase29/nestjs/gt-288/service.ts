const bios = new Map<string, string>();

export class ProfileService {
  save(userId: string, bio: string) {
    bios.set(userId, bio.trim());
  }

  load(userId: string) {
    return bios.get(userId) ?? "";
  }
}
