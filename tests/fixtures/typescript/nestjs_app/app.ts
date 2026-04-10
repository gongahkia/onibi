const Nest = {
  Controller: (): ClassDecorator => () => {},
  Get: (): MethodDecorator => () => {},
  Post: (): MethodDecorator => () => {},
  Body: (_name?: string): ParameterDecorator => () => {},
  Param: (_name?: string): ParameterDecorator => () => {},
  Query: (_name?: string): ParameterDecorator => () => {},
  Headers: (_name?: string): ParameterDecorator => () => {},
  Req: (): ParameterDecorator => () => {},
  Res: (): ParameterDecorator => () => {},
};

const db = {
  query: (sql: string) => sql,
};

function exec(command: string): string {
  return command;
}

class ShellService {
  run(command: string): string {
    return exec(command);
  }
}

@Nest.Controller()
class DemoController {
  private readonly userRepository = {
    query: (sql: string) => db.query(sql),
  };

  private readonly shellService = new ShellService();

  @Nest.Post()
  create(@Nest.Body("name") name: string) {
    const sql = `SELECT * FROM users WHERE name = '${name}'`;
    return this.userRepository.query(sql);
  }

  @Nest.Get()
  show(@Nest.Param("id") id: string) {
    const sql = `SELECT * FROM users WHERE id = '${id}'`;
    return this.userRepository.query(sql);
  }

  @Nest.Get()
  echo(
    @Nest.Query("term") term: string,
    @Nest.Res() res: { send(value: string): string },
  ) {
    return res.send(term);
  }

  @Nest.Get()
  headerEcho(
    @Nest.Headers("auth") auth: string,
    @Nest.Res() res: { send(value: string): string },
  ) {
    return res.send(auth);
  }

  @Nest.Get()
  runCommand(@Nest.Query("cmd") cmd: string) {
    return this.shellService.run(cmd);
  }

  @Nest.Post()
  runRequest(@Nest.Req() req: { body: { command: string } }) {
    return this.shellService.run(req.body.command);
  }
}

export { DemoController };
