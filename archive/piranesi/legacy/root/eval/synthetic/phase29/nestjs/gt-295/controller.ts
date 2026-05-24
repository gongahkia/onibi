import { UserService } from "./service";

const Nest = {
  Controller: (): ClassDecorator => () => {},
  Post: (): MethodDecorator => () => {},
  Body: (_name?: string): ParameterDecorator => () => {},
};

@Nest.Controller()
export class UserController {
  private readonly service = new UserService();

  @Nest.Post()
  create(@Nest.Body("name") name: string) {
    return this.service.lookup(name);
  }
}
