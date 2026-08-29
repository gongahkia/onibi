import { NextResponse } from "next/server";
import { bankFeedStatus } from "@/lib/bank-feed";

export const dynamic = "force-dynamic";

export async function GET() {
  return NextResponse.json(bankFeedStatus());
}
