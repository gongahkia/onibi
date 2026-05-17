import { ReportService } from "./service";

const Nest = {
  Controller: (): ClassDecorator => () => {},
  Get: (): MethodDecorator => () => {},
  Post: (): MethodDecorator => () => {},
  Body: (_name?: string): ParameterDecorator => () => {},
  Param: (_name?: string): ParameterDecorator => () => {},
};

@Nest.Controller()
export class ReportController {
  private readonly service = new ReportService();

  @Nest.Post()
  save(
    @Nest.Param("id") id: string,
    @Nest.Body("sort") sort: string,
  ) {
    this.service.save(id, sort);
  }

  @Nest.Get()
  show(@Nest.Param("id") id: string) {
    return this.service.render(id);
  }
}
