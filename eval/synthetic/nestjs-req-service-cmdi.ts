const Nest = {
  Controller: (): ClassDecorator => () => {},
  Post: (): MethodDecorator => () => {},
  Req: (): ParameterDecorator => () => {},
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
  @Nest.Post()
  run(@Nest.Req() req: { body: { command: string } }) {
    return shellService.run(req.body.command);
  }
}
