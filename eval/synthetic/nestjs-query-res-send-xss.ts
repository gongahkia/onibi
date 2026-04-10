const Nest = {
  Controller: (): ClassDecorator => () => {},
  Get: (): MethodDecorator => () => {},
  Query: (_name?: string): ParameterDecorator => () => {},
  Res: (): ParameterDecorator => () => {},
};

@Nest.Controller()
class SearchController {
  @Nest.Get()
  echo(
    @Nest.Query("term") term: string,
    @Nest.Res() res: { send(value: string): string },
  ) {
    return res.send(term);
  }
}
