import { ProxyService } from "./service";

const Nest = {
  Controller: (): ClassDecorator => () => {},
  Get: (): MethodDecorator => () => {},
  Query: (_name?: string): ParameterDecorator => () => {},
};

@Nest.Controller()
export class ProxyController {
  private readonly service = new ProxyService();

  @Nest.Get()
  show(@Nest.Query("url") url: string) {
    return this.service.proxy(url);
  }
}
