const Nest = {
  Controller: (): ClassDecorator => () => {},
  Get: (): MethodDecorator => () => {},
  Headers: (_name?: string): ParameterDecorator => () => {},
  Res: (): ParameterDecorator => () => {},
};

@Nest.Controller()
class HeaderController {
  @Nest.Get()
  echo(
    @Nest.Headers("auth") auth: string,
    @Nest.Res() res: { send(value: string): string },
  ) {
    return res.send(auth);
  }
}
