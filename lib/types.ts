export type TransactionKind = "expense" | "income" | "transfer" | "settlement";
export type SplitMethod = "equal" | "amount" | "percent" | "shares";
export type Period = "week" | "month" | "year" | "custom";
export type SheetTotalPeriod = "asOfToday" | "year" | "month" | "week" | "day";
export type CategoryKind = "expense" | "income";
export type AppearancePreference = "automatic" | "dark" | "light";
export type SheetSort = "edited" | "created" | "nameAsc" | "nameDesc";
export type PrintFont = "inter" | "nunito" | "lora";
export type SyncRecordType = "sheet" | "category" | "transaction";
export type SyncTombstone = { recordType: SyncRecordType; recordId: string; deletedAt: string };

export type Sheet = {
  id: string;
  name: string;
  currency: string;
  archived: boolean;
  deletedAt?: string;
  createdAt?: string;
  updatedAt?: string;
  showTotalBalance: boolean;
  totalPeriod: SheetTotalPeriod;
  input: {
    showCurrencySelection: boolean;
    showMerchant: boolean;
    showTime: boolean;
    showCategorySuggestions: boolean;
  };
};

export type Category = {
  id: string;
  name: string;
  kind: CategoryKind;
  icon: string;
  color: string;
  sortOrder: number;
  deletedAt?: string;
  updatedAt: string;
};

export type AppPreferences = {
  appearance: AppearancePreference;
  preferredCurrency: string;
  printFont: PrintFont;
  printFontSize: number;
  sheetSort: SheetSort;
  syncEnabled: boolean;
  lastSyncedAt?: string;
  lastGoogleBackupAt?: string;
  syncTombstones?: SyncTombstone[];
  /** Local marker that makes an upgrade run one safe full reconciliation. */
  syncReconciliationVersion?: number;
  updatedAt: string;
};

export type Attachment = {
  filename: string;
  mimeType: string;
  size: number;
  storage: "supabase" | "indexeddb";
  storagePath: string;
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
  attachments?: Attachment[];
  merchant?: string;
  source?: "manual" | "bank";
  sheetId?: string;
  sheet?: string;
  transferGroupId?: string;
  transferDirection?: "in" | "out";
  /** OCR text is retained after the source image has been discarded. */
  ocrText?: string;
  updatedAt?: string;
  deletedAt?: string;
};

export type Budget = { id: string; title: string; limit: number; spent: number; period: Period; category?: string; shared: boolean };
export type Goal = { id: string; title: string; target: number; saved: number; deadline: string; shared: boolean };
export type BankConnection = { id: string; label: string; institution: string; status: "connected" | "needs_attention" | "not_connected"; lastSynced?: string; accountMask?: string };

export const money = (value: number, currency = "SGD") => new Intl.NumberFormat("en-SG", { style: "currency", currency, maximumFractionDigits: 2 }).format(value);

export const dateLabel = (iso: string) => new Intl.DateTimeFormat("en-SG", { month: "short", day: "numeric" }).format(new Date(`${iso}T12:00:00`));
