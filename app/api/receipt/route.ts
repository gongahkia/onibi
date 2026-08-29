import { NextResponse } from "next/server";

type ReceiptSuggestion = { amount: number | null; currency: string | null; merchant: string | null; date: string | null; category: string | null; confidence: "low" | "medium" | "high" };

const empty: ReceiptSuggestion = { amount: null, currency: null, merchant: null, date: null, category: null, confidence: "low" };

function responseText(payload: { output_text?: string; output?: Array<{ content?: Array<{ text?: string }> }> }) {
  return payload.output_text || payload.output?.flatMap((item) => item.content || []).map((content) => content.text || "").join("") || "";
}

export async function POST(request: Request) {
  const { imageDataUrl } = await request.json().catch(() => ({}));
  if (typeof imageDataUrl !== "string" || !imageDataUrl.startsWith("data:image/") || imageDataUrl.length > 6_000_000) {
    return NextResponse.json({ error: "Provide an image under 4.5 MB." }, { status: 400 });
  }
  if (!process.env.OPENAI_API_KEY) {
    return NextResponse.json({ suggestion: empty, configured: false, message: "Receipt AI is not configured; your image can still be attached." });
  }
  const openai = await fetch("https://api.openai.com/v1/responses", {
    method: "POST",
    headers: { Authorization: `Bearer ${process.env.OPENAI_API_KEY}`, "Content-Type": "application/json" },
    body: JSON.stringify({
      model: process.env.OPENAI_MODEL || "gpt-4.1-mini",
      store: false,
      input: [{ role: "user", content: [
        { type: "input_text", text: "Read this receipt. Return only a JSON object with amount (number or null), currency (ISO 4217 or null), merchant (string or null), date (YYYY-MM-DD or null), category (one of Groceries, Dining, Transport, Utilities, Rent, Health, Shopping, Entertainment, Other or null), and confidence (low, medium, high). Do not invent values." },
        { type: "input_image", image_url: imageDataUrl, detail: "low" }
      ] }]
    })
  });
  if (!openai.ok) return NextResponse.json({ error: "Receipt analysis is unavailable. You can save the attachment without it." }, { status: 502 });
  const text = responseText(await openai.json());
  try {
    const suggestion = JSON.parse(text) as ReceiptSuggestion;
    return NextResponse.json({ suggestion, configured: true });
  } catch {
    return NextResponse.json({ suggestion: empty, configured: true, message: "No reliable receipt details were found." });
  }
}
