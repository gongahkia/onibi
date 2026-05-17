const Nest = {
  Controller: (): ClassDecorator => () => {},
  Get: (): MethodDecorator => () => {},
  Query: (_name?: string): ParameterDecorator => () => {},
};

function exec(command: string): string {
  return command;
}

class ShellService {
  run(command: string) {
    return exec(command);
  }
}

const shellService = new ShellService();

@Nest.Controller()
class CommandController {
  @Nest.Get()
  run(@Nest.Query("cmd") cmd: string) {
    return shellService.run(cmd);
  }
}
