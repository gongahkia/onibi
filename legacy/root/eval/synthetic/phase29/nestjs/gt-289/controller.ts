import { CommandService } from "./service";

const Nest = {
  Controller: (): ClassDecorator => () => {},
  Get: (): MethodDecorator => () => {},
  Headers: (_name?: string): ParameterDecorator => () => {},
};

@Nest.Controller()
export class CommandController {
  private readonly service = new CommandService();

  @Nest.Get()
  run(@Nest.Headers("x-command") command: string) {
    return this.service.runProbe(command);
  }
}
