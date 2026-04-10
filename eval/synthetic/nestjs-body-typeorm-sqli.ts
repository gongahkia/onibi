const Nest = {
  Controller: (): ClassDecorator => () => {},
  Post: (): MethodDecorator => () => {},
  Body: (_name?: string): ParameterDecorator => () => {},
};

const repository = {
  query: (sql: string) => sql,
};

@Nest.Controller()
class UserController {
  @Nest.Post()
  create(@Nest.Body("name") name: string) {
    const sql = `SELECT * FROM users WHERE name = '${name}'`;
    return repository.query(sql);
  }
}
