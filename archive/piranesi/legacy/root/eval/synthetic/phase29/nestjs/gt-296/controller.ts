import { ViewService } from "./service";

const Nest = {
  Controller: (): ClassDecorator => () => {},
  Get: (): MethodDecorator => () => {},
  Query: (_name?: string): ParameterDecorator => () => {},
  Res: (): ParameterDecorator => () => {},
};

@Nest.Controller()
export class SearchController {
  private readonly views = new ViewService();

  @Nest.Get()
  show(
    @Nest.Query("term") term: string,
    @Nest.Res() res: { send(value: string): string },
  ) {
    return res.send(this.views.renderTerm(term)); // SINK
  }
}
