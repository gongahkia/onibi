import {
  geminiGenerateContentRequest,
  parseGeminiClaims,
  requireEnv,
  type RawClaimsJson
} from "./shared.js";

const geminiApiRoot = "https://generativelanguage.googleapis.com/v1beta";

export async function extractFromGemini(prompt: string, model: string): Promise<RawClaimsJson> {
  const apiKey = requireEnv("GOOGLE_API_KEY", "Gemini");
  const response = await fetch(`${geminiApiRoot}/${geminiModelPath(model)}:generateContent`, {
    body: JSON.stringify(geminiGenerateContentRequest(prompt)),
    headers: {
      "Content-Type": "application/json",
      "x-goog-api-key": apiKey
    },
    method: "POST"
  });
  if (!response.ok) {
    const body = await response.text();
    throw new Error(
      `Gemini generateContent request failed: ${response.status}${body ? ` ${body}` : ""}`
    );
  }
  return parseGeminiClaims(await response.json());
}

function geminiModelPath(model: string): string {
  const path = model.startsWith("models/") ? model : `models/${model}`;
  return path
    .split("/")
    .map((part) => encodeURIComponent(part))
    .join("/");
}
