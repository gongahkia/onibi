import {
  anthropicMessageRequest,
  parseAnthropicClaims,
  requireEnv,
  type RawClaimsJson
} from "./shared.js";

type AnthropicConstructor = new (options: { readonly apiKey: string }) => {
  readonly messages: {
    create(input: Record<string, unknown>): Promise<unknown>;
  };
};

const anthropicSdkPackage = "@anthropic-ai/sdk";

export async function extractFromAnthropic(
  prompt: string,
  model: string
): Promise<RawClaimsJson> {
  const Anthropic = await loadAnthropicConstructor();
  const client = new Anthropic({
    apiKey: requireEnv("ANTHROPIC_API_KEY", "Anthropic")
  });
  return parseAnthropicClaims(await client.messages.create(anthropicMessageRequest(prompt, model)));
}

async function loadAnthropicConstructor(): Promise<AnthropicConstructor> {
  const module = (await import(anthropicSdkPackage)) as {
    readonly default?: unknown;
    readonly Anthropic?: unknown;
  };
  const constructor = module.default ?? module.Anthropic;
  if (typeof constructor !== "function") {
    throw new Error("@anthropic-ai/sdk did not expose an Anthropic constructor.");
  }
  return constructor as AnthropicConstructor;
}
