import { ProfileService } from "./service";

const Nest = {
  Controller: (): ClassDecorator => () => {},
  Get: (): MethodDecorator => () => {},
  Post: (): MethodDecorator => () => {},
  Body: (_name?: string): ParameterDecorator => () => {},
  Param: (_name?: string): ParameterDecorator => () => {},
  Res: (): ParameterDecorator => () => {},
};

@Nest.Controller()
export class ProfileController {
  private readonly service = new ProfileService();

  @Nest.Post()
  save(
    @Nest.Param("id") id: string,
    @Nest.Body("bio") bio: string,
  ) {
    this.service.save(id, bio);
  }

  @Nest.Get()
  show(
    @Nest.Param("id") id: string,
    @Nest.Res() res: { send(value: string): string },
  ) {
    const stored = this.service.load(id);
    const markup = `<section>${stored}</section>`;
    return res.send(markup); // SINK
  }
}
