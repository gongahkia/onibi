const Nest = {
  Controller: (): ClassDecorator => () => {},
  Get: (): MethodDecorator => () => {},
  Param: (_name?: string): ParameterDecorator => () => {},
};

const repository = {
  query: (sql: string) => sql,
};

@Nest.Controller()
class UserController {
  @Nest.Get()
  show(@Nest.Param("id") id: string) {
    const sql = `SELECT * FROM users WHERE id = '${id}'`;
    return repository.query(sql);
  }
}
