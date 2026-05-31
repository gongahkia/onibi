import {
  openAiResponsesRequest,
  parseOpenAiClaims,
  requireEnv,
  trimTrailingSlashes,
  type RawClaimsJson,
  type ResponsesClient
} from "./shared.js";

type OpenAiConstructor = new (options: {
  readonly apiKey: string;
  readonly baseURL: string;
}) => ResponsesClient;

const openAiSdkPackage = "openai";

export async function extractFromAzureOpenAI(
  prompt: string,
  model: string
): Promise<RawClaimsJson> {
  const OpenAI = await loadOpenAiConstructor();
  const endpoint = trimTrailingSlashes(
    requireEnv("AZURE_OPENAI_ENDPOINT", "Azure OpenAI")
  );
  const apiKey = requireEnv("AZURE_OPENAI_API_KEY", "Azure OpenAI");
  const deployment = requireEnv("AZURE_OPENAI_DEPLOYMENT", "Azure OpenAI");
  const requestModel = deployment.length > 0 ? deployment : model;
  const client = new OpenAI({
    apiKey,
    baseURL: azureResponsesBaseUrl(endpoint)
  });
  return parseOpenAiClaims(
    await client.responses.create(openAiResponsesRequest(prompt, requestModel))
  );
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

function azureResponsesBaseUrl(endpoint: string): string {
  if (endpoint.endsWith("/openai/v1")) {
    return `${endpoint}/`;
  }
  if (endpoint.endsWith("/openai")) {
    return `${endpoint}/v1/`;
  }
  return `${endpoint}/openai/v1/`;
}
