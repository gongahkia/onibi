import type { BankConnection, Budget, Goal, Transaction } from "./types";

export const demoTransactions: Transaction[] = [
  { id: "t1", title: "FairPrice groceries", amount: 86.4, kind: "expense", category: "Groceries", date: "2026-08-29", paidBy: "Nadia", participants: ["Nadia", "Leo"], splitMethod: "equal", merchant: "FairPrice", currency: "SGD", source: "bank" },
  { id: "t2", title: "MRT top-up", amount: 30, kind: "expense", category: "Transport", date: "2026-08-28", paidBy: "Leo", participants: ["Leo"], splitMethod: "equal", currency: "SGD" },
  { id: "t3", title: "Salary", amount: 4800, kind: "income", category: "Salary", date: "2026-08-27", paidBy: "Nadia", participants: ["Nadia"], splitMethod: "equal", currency: "SGD", source: "bank" },
  { id: "t4", title: "Internet", amount: 49.9, kind: "expense", category: "Utilities", date: "2026-08-26", paidBy: "Leo", participants: ["Nadia", "Leo"], splitMethod: "percent", recurring: "Monthly", currency: "SGD" },
  { id: "t5", title: "Dinner at Kura", amount: 62.5, kind: "expense", category: "Dining", date: "2026-08-24", paidBy: "Nadia", participants: ["Nadia", "Leo"], splitMethod: "amount", merchant: "Kura Sushi", currency: "SGD" },
  { id: "t6", title: "Transfer to Japan fund", amount: 200, kind: "transfer", category: "Goals", date: "2026-08-23", paidBy: "Leo", participants: ["Leo"], splitMethod: "equal", currency: "SGD", sheet: "Shared expenses", transferGroupId: "transfer-demo-1", transferDirection: "out" },
  { id: "t7", title: "Transfer from Shared expenses", amount: 200, kind: "transfer", category: "Goals", date: "2026-08-23", paidBy: "Leo", participants: ["Leo"], splitMethod: "equal", currency: "SGD", sheet: "Japan fund", transferGroupId: "transfer-demo-1", transferDirection: "in" }
];

export const demoBudgets: Budget[] = [
  { id: "b1", title: "Household essentials", limit: 700, spent: 428.2, period: "month", category: "Groceries", shared: true },
  { id: "b2", title: "Eating out", limit: 240, spent: 146.5, period: "month", category: "Dining", shared: true },
  { id: "b3", title: "Nadia's fun money", limit: 120, spent: 63, period: "week", shared: false }
];

export const demoGoals: Goal[] = [
  { id: "g1", title: "Japan in spring", target: 6000, saved: 3640, deadline: "2027-03-01", shared: true },
  { id: "g2", title: "Nadia's camera", target: 1800, saved: 750, deadline: "2026-12-15", shared: false }
];

export const demoBankConnections: BankConnection[] = [
  { id: "bank-1", label: "UOB One", institution: "UOB", status: "connected", lastSynced: "just now", accountMask: "•••• 3291" },
  { id: "bank-2", label: "Leo's credit card", institution: "Connect a provider", status: "not_connected" }
];
