import { UserRepository } from "./repository";

export class UserService {
  private readonly repo = new UserRepository();

  lookup(name: string) {
    return this.repo.findByName(name.trim());
  }
}
