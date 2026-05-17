import { UserRepository } from "./repository";

export class UserService {
  private readonly repo = new UserRepository();

  lookup(name: string) {
    const normalized = name.trim();
    return this.repo.findByName(normalized);
  }
}
