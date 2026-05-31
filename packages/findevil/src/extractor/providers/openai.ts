import {
  openAiResponsesRequest,
  parseOpenAiClaims,
  requireEnv,
  type RawClaimsJson,
  type ResponsesClient
} from "./shared.js";

type OpenAiConstructor = new (options: { readonly apiKey: string }) => ResponsesClient;

const openAiSdkPackage = "openai";

export async function extractFromOpenAI(prompt: string, model: string): Promise<RawClaimsJson> {
  const OpenAI = await loadOpenAiConstructor();
  const client = new OpenAI({
    apiKey: requireEnv("OPENAI_API_KEY", "OpenAI")
  });
  return parseOpenAiClaims(await client.responses.create(openAiResponsesRequest(prompt, model)));
}

async function loadOpenAiConstructor(): Promise<OpenAiConstructor> {
  const module = (await import(openAiSdkPackage)) as {
    readonly default?: unknown;
    readonly OpenAI?: unknown;
  };
  const constructor = module.default ?? module.OpenAI;
  if (typeof constructor !== "function") {
    throw new Error("openai did not expose an OpenAI constructor.");
  }
  return constructor as OpenAiConstructor;
}
