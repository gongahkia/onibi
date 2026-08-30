export type TransactionKind = "expense" | "income" | "transfer" | "settlement";
export type SplitMethod = "equal" | "amount" | "percent" | "shares";
export type Period = "week" | "month" | "year" | "custom";
export type SheetTotalPeriod = "today" | "all";

export type Sheet = {
  id: string;
  name: string;
  currency: string;
  archived: boolean;
  showTotalBalance: boolean;
  totalPeriod: SheetTotalPeriod;
  input: {
    showCurrencySelection: boolean;
    showMerchant: boolean;
    showTime: boolean;
    showCategorySuggestions: boolean;
  };
};

export type Transaction = {
  id: string;
  title: string;
  amount: number;
  kind: TransactionKind;
  category: string;
  date: string;
  time?: string;
  paidBy: string;
  participants: string[];
  splitMethod: SplitMethod;
  notes?: string;
  pending?: boolean;
  recurring?: string;
  currency: string;
  hasAttachment?: boolean;
  merchant?: string;
  source?: "manual" | "bank";
  sheetId?: string;
  sheet?: string;
  transferGroupId?: string;
  transferDirection?: "in" | "out";
};

export type Budget = { id: string; title: string; limit: number; spent: number; period: Period; category?: string; shared: boolean };
export type Goal = { id: string; title: string; target: number; saved: number; deadline: string; shared: boolean };
export type BankConnection = { id: string; label: string; institution: string; status: "connected" | "needs_attention" | "not_connected"; lastSynced?: string; accountMask?: string };

export const money = (value: number, currency = "SGD") => new Intl.NumberFormat("en-SG", { style: "currency", currency, maximumFractionDigits: 2 }).format(value);

export const dateLabel = (iso: string) => new Intl.DateTimeFormat("en-SG", { month: "short", day: "numeric" }).format(new Date(`${iso}T12:00:00`));
