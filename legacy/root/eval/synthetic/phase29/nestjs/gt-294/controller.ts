import { DocumentService } from "./service";

const Nest = {
  Controller: (): ClassDecorator => () => {},
  Get: (): MethodDecorator => () => {},
  Post: (): MethodDecorator => () => {},
  Body: (_name?: string): ParameterDecorator => () => {},
  Param: (_name?: string): ParameterDecorator => () => {},
};

@Nest.Controller()
export class DocumentController {
  private readonly service = new DocumentService();

  @Nest.Post()
  save(
    @Nest.Param("id") id: string,
    @Nest.Body("doc") doc: string,
  ) {
    this.service.save(id, doc);
  }

  @Nest.Get()
  show(@Nest.Param("id") id: string) {
    return this.service.show(id);
  }
}
