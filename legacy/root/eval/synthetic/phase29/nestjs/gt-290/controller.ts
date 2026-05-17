import { FileService } from "./service";

const Nest = {
  Controller: (): ClassDecorator => () => {},
  Get: (): MethodDecorator => () => {},
  Query: (_name?: string): ParameterDecorator => () => {},
};

@Nest.Controller()
export class FileController {
  private readonly service = new FileService();

  @Nest.Get()
  show(@Nest.Query("file") file: string) {
    return this.service.loadDocument(file);
  }
}
