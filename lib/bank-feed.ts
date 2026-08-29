export type BankFeedProvider = "brankas" | "none";
export type BankConnectionRequest = { householdId: string; redirectUri: string };
export type BankFeedStatus =
  | { status: "ready"; provider: BankFeedProvider; message: string }
  | { status: "unavailable"; provider: BankFeedProvider; message: string };

/**
 * This deliberately does not accept or transmit bank credentials. A production
 * provider must own the consent/login screen and return only a signed callback.
 */
export function bankFeedStatus(): BankFeedStatus {
  const provider = (process.env.BANK_FEED_PROVIDER || "none") as BankFeedProvider;
  if (provider !== "brankas") return { status: "unavailable", provider: "none", message: "No bank-feed provider has been configured." };
  if (!process.env.BRANKAS_API_BASE_URL || !process.env.BRANKAS_API_KEY) {
    return { status: "unavailable", provider, message: "Brankas production credentials and approved account-information access are required." };
  }
  return { status: "ready", provider, message: "Provider credentials are present. Complete the provider-specific consent callback before enabling this connection." };
}

export type NormalizedBankTransaction = {
  providerTransactionId: string;
  postedAt: string;
  amount: string;
  currency: string;
  merchant: string | null;
  description: string;
  pending: boolean;
};

export function normalizeProviderTransaction(input: Record<string, unknown>): NormalizedBankTransaction {
  const amount = String(input.amount ?? input.transaction_amount ?? "0");
  return {
    providerTransactionId: String(input.id ?? input.transaction_id ?? ""),
    postedAt: String(input.date ?? input.created_at ?? new Date().toISOString()),
    amount,
    currency: String(input.currency ?? "SGD").toUpperCase(),
    merchant: typeof input.merchant === "string" ? input.merchant : null,
    description: String(input.description ?? input.name ?? "Bank transaction"),
    pending: Boolean(input.pending)
  };
}
