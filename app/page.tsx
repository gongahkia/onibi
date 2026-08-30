"use client";

import { ChangeEvent, FormEvent, useEffect, useMemo, useRef, useState } from "react";
import JSZip from "jszip";
import { SFArrowDown, SFArrowLeft, SFArrowLeftArrowRight, SFArrowRight, SFArrowUpArrowDown, SFArrowUpRight, SFBanknoteFill, SFCalendar, SFCartFill, SFChartLineUptrendXyaxis, SFChartPie, SFCheckmark, SFChevronDown, SFClock, SFCloudFill, SFCreditcardFill, SFDocumentBadgePlus, SFEllipsis, SFEyeSlash, SFForkKnife, SFGearshapeFill, SFHeartFill, SFHouseFill, SFInfoCircle, SFLightbulbFill, SFListBullet, SFLock, SFMagnifyingglass, SFPaintpalette, SFPaperclip, SFPaperplane, SFPencil, SFPersonBadgePlus, SFPhoto, SFPlus, SFPrinter, SFReceipt, SFRepeat, SFSliderHorizontal3, SFSquareAndArrowUp, SFTablecells, SFTarget, SFTramFill, SFTrash, SFXmark } from "sf-symbols-lib/dualtone";
import { demoBankConnections, demoBudgets, demoGoals, demoTransactions } from "@/lib/demo-data";
import { readAttachment, storeAttachment } from "@/lib/attachments";
import { dateLabel, money, type Attachment, type BankConnection, type Budget, type Goal, type Sheet, type SheetTotalPeriod, type SplitMethod, type Transaction, type TransactionKind } from "@/lib/types";

type View = "home" | "ledger" | "plans" | "insights" | "settings";
type SheetAction = "stats" | "trends" | "exchange" | "select" | "print" | "export" | "import" | "edit";
type StatsRange = "today" | "yearly" | "monthly" | "weekly" | "daily";
type IconKey = "home" | "ledger" | "plans" | "insights" | "settings" | "plus" | "cart" | "dining" | "transport" | "utilities" | "salary" | "goals" | "transfer" | "bank" | "download" | "upload" | "check" | "back" | "forward" | "upRight" | "chevronDown" | "cloud" | "attachment" | "receipt" | "table" | "search" | "close" | "calendar" | "clock" | "photo" | "more" | "personAdd" | "documentAdd" | "chartPie" | "sync" | "printer" | "repeat" | "sliders" | "trash" | "palette" | "paperplane" | "info" | "lock" | "eyeSlash" | "pencil";
type NavItem = { id: View; label: string; icon: IconKey; visible: boolean };
const iconComponents = { home: SFHouseFill, ledger: SFListBullet, plans: SFTarget, insights: SFChartLineUptrendXyaxis, settings: SFGearshapeFill, plus: SFPlus, cart: SFCartFill, dining: SFForkKnife, transport: SFTramFill, utilities: SFLightbulbFill, salary: SFBanknoteFill, goals: SFHeartFill, transfer: SFArrowLeftArrowRight, bank: SFCreditcardFill, download: SFArrowDown, upload: SFSquareAndArrowUp, check: SFCheckmark, back: SFArrowLeft, forward: SFArrowRight, upRight: SFArrowUpRight, chevronDown: SFChevronDown, cloud: SFCloudFill, attachment: SFPaperclip, receipt: SFReceipt, table: SFTablecells, search: SFMagnifyingglass, close: SFXmark, calendar: SFCalendar, clock: SFClock, photo: SFPhoto, more: SFEllipsis, personAdd: SFPersonBadgePlus, documentAdd: SFDocumentBadgePlus, chartPie: SFChartPie, sync: SFArrowUpArrowDown, printer: SFPrinter, repeat: SFRepeat, sliders: SFSliderHorizontal3, trash: SFTrash, palette: SFPaintpalette, paperplane: SFPaperplane, info: SFInfoCircle, lock: SFLock, eyeSlash: SFEyeSlash, pencil: SFPencil };
function AppIcon({ name, size = "md" }: { name: IconKey; size?: "xs" | "sm" | "md" | "lg" | "xl" }) { const Icon = iconComponents[name]; return <Icon size={size} aria-hidden="true" />; }
const members = ["Nadia", "Leo"];
const categories = ["Groceries", "Dining", "Transport", "Utilities", "Rent", "Health", "Shopping", "Entertainment", "Salary", "Goals", "Other"];
const expenseCategories = ["Groceries", "Dining", "Transport", "Utilities", "Rent", "Health", "Shopping", "Entertainment", "Other"];
const incomeCategories = ["Salary", "Freelance", "Interest", "Refund", "Other"];
const defaultSheet: Sheet = { id: "shared-expenses", name: "Shared expenses", currency: "SGD", archived: false, showTotalBalance: true, totalPeriod: "today", input: { showCurrencySelection: true, showMerchant: true, showTime: true, showCategorySuggestions: true } };
type AmountMatch = "exactly" | "atLeast" | "atMost";
type DateMatch = "all" | "today" | "custom";
type LedgerFilters = { amount: string; amountMatch: AmountMatch; category: string; currency: string; date: string; dateMatch: DateMatch; hasAttachment: boolean; kind: "all" | TransactionKind; notes: string; recurring: boolean };
const emptyLedgerFilters: LedgerFilters = { amount: "", amountMatch: "exactly", category: "all", currency: "all", date: "", dateMatch: "all", hasAttachment: false, kind: "all", notes: "", recurring: false };
const defaultNavItems: NavItem[] = [
  { id: "home", label: "Home", icon: "home", visible: true },
  { id: "ledger", label: "Ledger", icon: "ledger", visible: true },
  { id: "plans", label: "Plans", icon: "plans", visible: true },
  { id: "insights", label: "Insights", icon: "insights", visible: true },
  { id: "settings", label: "Settings", icon: "settings", visible: true }
];

function uid(prefix: string) { return `${prefix}-${crypto.randomUUID?.() ?? Date.now().toString(36)}`; }
function todayIso() { return new Date().toISOString().slice(0, 10); }
function sheetDisplayName(sheet: Sheet, position: number, allSheets: Sheet[]) { return allSheets.filter((entry) => entry.name === sheet.name).length > 1 ? `${sheet.name} · ${position + 1}` : sheet.name; }
type DemoState = { transactions: Transaction[]; budgets: Budget[]; goals: Goal[]; banks: BankConnection[]; sheets: Sheet[]; navItems?: NavItem[]; showNavIcons?: boolean };
function readDemoState(): DemoState {
  const fallback: DemoState = { transactions: demoTransactions, budgets: demoBudgets, goals: demoGoals, banks: demoBankConnections, sheets: [defaultSheet], navItems: defaultNavItems, showNavIcons: true };
  if (typeof window === "undefined") return fallback;
  try {
    const raw = localStorage.getItem("together-budget-demo");
    if (!raw) return fallback;
    const parsed = JSON.parse(raw) as Partial<DemoState>;
    const navItems = parsed.navItems?.map((item) => ({ ...defaultNavItems.find((entry) => entry.id === item.id), ...item, icon: item.icon || defaultNavItems.find((entry) => entry.id === item.id)?.icon || "home" })) || fallback.navItems;
    const parsedSheets = parsed.sheets?.length ? parsed.sheets.map((sheet) => ({ ...defaultSheet, ...sheet, input: { ...defaultSheet.input, ...sheet.input } })) : fallback.sheets;
    return { transactions: parsed.transactions || fallback.transactions, budgets: parsed.budgets || fallback.budgets, goals: parsed.goals || fallback.goals, banks: parsed.banks || fallback.banks, sheets: parsedSheets, navItems, showNavIcons: parsed.showNavIcons ?? true };
  } catch { return fallback; }
}

type TransactionDraft = { amount: number; attachments: File[]; category: string; date: string; hasAttachment: boolean; fromSheetId?: string; kind: TransactionKind; merchant: string; notes: string; pending: boolean; recurring: string; sheetId: string; time: string; title: string; toSheetId?: string };
type SheetDraft = Omit<Sheet, "id" | "archived">;
type Confirmation = { description: string; label: string; onConfirm: () => void; title: string };
type ComposerPreset = { category: string; sheetId: string };
type ImportRow = { amount: number; category: string; currency: string; date: string; merchant: string; notes: string; time: string; kind: "expense" | "income" };

export default function BudgetApp() {
  const [initial] = useState(readDemoState);
  const [sheets, setSheets] = useState<Sheet[]>(initial.sheets);
  const [transactions, setTransactions] = useState<Transaction[]>(() => initial.transactions.map((item) => {
    const sheetId = item.sheetId || initial.sheets.find((sheet) => sheet.name === item.sheet)?.id || defaultSheet.id;
    const sheet = initial.sheets.find((entry) => entry.id === sheetId) || defaultSheet;
    return { ...item, sheetId, sheet: sheet.name, time: item.time || "09:35" };
  }));
  const [activeSheetId, setActiveSheetId] = useState<string | null>(null);
  const [showAdd, setShowAdd] = useState(false);
  const [showNewSheet, setShowNewSheet] = useState(false);
  const [showFilters, setShowFilters] = useState(false);
  const [showMainMenu, setShowMainMenu] = useState(false);
  const [showSettings, setShowSettings] = useState(false);
  const [showSheetMenu, setShowSheetMenu] = useState(false);
  const [sheetAction, setSheetAction] = useState<SheetAction | null>(null);
  const [selectedTransactionIds, setSelectedTransactionIds] = useState<string[]>([]);
  const [showBatchMenu, setShowBatchMenu] = useState(false);
  const [editingTransactionId, setEditingTransactionId] = useState<string | null>(null);
  const [editingFocus, setEditingFocus] = useState<"merchant" | "category" | undefined>();
  const [actionTransactionId, setActionTransactionId] = useState<string | null>(null);
  const [moveTransactionId, setMoveTransactionId] = useState<string | null>(null);
  const [confirmation, setConfirmation] = useState<Confirmation | null>(null);
  const [preset, setPreset] = useState<ComposerPreset | null>(null);
  const [search, setSearch] = useState("");
  const [sensitive, setSensitive] = useState(false);
  const [filters, setFilters] = useState<LedgerFilters>(emptyLedgerFilters);
  const [notice, setNotice] = useState<string | null>(null);
  const activeSheet = sheets.find((sheet) => sheet.id === activeSheetId) || null;
  const editingTransaction = transactions.find((item) => item.id === editingTransactionId) || null;
  const actionTransaction = transactions.find((item) => item.id === actionTransactionId) || null;
  const movingTransaction = transactions.find((item) => item.id === moveTransactionId) || null;
  const activeSheets = sheets.filter((sheet) => !sheet.archived);

  useEffect(() => { localStorage.setItem("together-budget-demo", JSON.stringify({ ...initial, sheets, transactions })); }, [initial, sheets, transactions]);
  useEffect(() => { if (!notice) return; const id = window.setTimeout(() => setNotice(null), 3200); return () => window.clearTimeout(id); }, [notice]);

  function nameForSheet(sheetId: string) { return sheets.find((sheet) => sheet.id === sheetId)?.name || "Untitled sheet"; }
  function buildBaseTransaction(id: string, draft: TransactionDraft, attachments: Attachment[] = []): Transaction {
    return { id, title: draft.title.trim() || draft.category, amount: draft.amount, kind: draft.kind, category: draft.category, date: draft.date, time: draft.time, paidBy: "Nadia", participants: ["Nadia"], splitMethod: "equal", notes: draft.notes, pending: draft.pending, recurring: draft.recurring, currency: "SGD", hasAttachment: draft.hasAttachment || attachments.length > 0, attachments, merchant: draft.merchant || undefined, source: "manual", sheetId: draft.sheetId, sheet: nameForSheet(draft.sheetId) };
  }
  function transferRecords(base: Transaction, draft: TransactionDraft, transferGroupId: string, ids = { outgoing: uid("txn"), incoming: uid("txn") }) {
    const outgoing: Transaction = { ...base, id: ids.outgoing, title: `Transfer to ${nameForSheet(draft.toSheetId || "")}`, category: "Transfer", sheetId: draft.fromSheetId, sheet: nameForSheet(draft.fromSheetId || ""), transferGroupId, transferDirection: "out" };
    const incoming: Transaction = { ...base, id: ids.incoming, title: `Transfer from ${nameForSheet(draft.fromSheetId || "")}`, category: "Transfer", sheetId: draft.toSheetId, sheet: nameForSheet(draft.toSheetId || ""), transferGroupId, transferDirection: "in" };
    return [outgoing, incoming];
  }

  function createTransaction(draft: TransactionDraft) {
    if (!Number.isFinite(draft.amount) || draft.amount <= 0) { setNotice("Enter an amount greater than zero."); return; }
    const base = buildBaseTransaction(uid("txn"), draft);
    if (draft.kind === "transfer") {
      if (!draft.fromSheetId || !draft.toSheetId || draft.fromSheetId === draft.toSheetId) { setNotice("Choose two different sheets for a transfer."); return; }
      const transferGroupId = uid("transfer");
      const records = transferRecords(base, draft, transferGroupId, { outgoing: base.id, incoming: uid("txn") });
      setTransactions((items) => [...records, ...items]);
      void persistAttachments(records.map((item) => item.id), draft.attachments);
      setNotice("Linked transfer created in both sheets.");
    } else {
      setTransactions((items) => [{ ...base, sheet: nameForSheet(draft.sheetId) }, ...items]);
      void persistAttachments([base.id], draft.attachments);
      setNotice(draft.pending ? "Pending transaction saved." : "Transaction added.");
    }
    setShowAdd(false);
    setPreset(null);
  }

  function updateTransaction(draft: TransactionDraft) {
    if (!editingTransaction || !Number.isFinite(draft.amount) || draft.amount <= 0) { setNotice("Enter an amount greater than zero."); return; }
    const original = editingTransaction;
    const base = buildBaseTransaction(original.id, draft, original.attachments || []);
    if (draft.kind === "transfer") {
      if (!draft.fromSheetId || !draft.toSheetId || draft.fromSheetId === draft.toSheetId) { setNotice("Choose two different sheets for a transfer."); return; }
      const group = original.transferGroupId;
      const counterpart = group ? transactions.find((item) => item.transferGroupId === group && item.id !== original.id) : undefined;
      const outgoingId = original.transferDirection === "out" ? original.id : counterpart?.id || original.id;
      const incomingId = original.transferDirection === "in" ? original.id : counterpart?.id || uid("txn");
      const groupId = group || uid("transfer");
      const records = transferRecords(base, draft, groupId, { outgoing: outgoingId, incoming: incomingId });
      setTransactions((items) => [...records, ...items.filter((item) => group ? item.transferGroupId !== group : item.id !== original.id)]);
      void persistAttachments(records.map((item) => item.id), draft.attachments);
    } else {
      const standalone: Transaction = { ...base, kind: draft.kind, sheetId: draft.sheetId, sheet: nameForSheet(draft.sheetId), transferGroupId: undefined, transferDirection: undefined };
      setTransactions((items) => [standalone, ...items.filter((item) => original.transferGroupId ? item.transferGroupId !== original.transferGroupId : item.id !== original.id)]);
      void persistAttachments([standalone.id], draft.attachments);
    }
    setEditingTransactionId(null);
    setNotice("Transaction updated.");
  }

  function createSheet(draft: SheetDraft) {
    const name = draft.name.trim();
    if (!name) { setNotice("Enter a sheet name."); return false; }
    const createdAt = new Date().toISOString();
    const sheet: Sheet = { ...draft, id: uid("sheet"), name, archived: false, createdAt, updatedAt: createdAt };
    setSheets((items) => [...items, sheet]);
    setActiveSheetId(sheet.id);
    setShowNewSheet(false);
    setNotice("Sheet created.");
    return true;
  }

  function updateSheet(sheetId: string, draft: SheetDraft) {
    const name = draft.name.trim();
    if (!name) { setNotice("Enter a sheet name."); return false; }
    setSheets((items) => items.map((sheet) => sheet.id === sheetId ? { ...sheet, ...draft, name, updatedAt: new Date().toISOString() } : sheet));
    setNotice("Sheet updated.");
    return true;
  }

  async function persistAttachments(transactionIds: string[], files: File[]) {
    if (!files.length) return;
    try {
      const stored = await Promise.all(files.map((file) => storeAttachment(transactionIds[0], file)));
      setTransactions((items) => items.map((item) => transactionIds.includes(item.id) ? { ...item, hasAttachment: true, attachments: [...(item.attachments || []), ...stored] } : item));
      setNotice(`${stored.length} attachment${stored.length === 1 ? "" : "s"} saved.`);
    } catch { setNotice("The transaction was saved, but its attachment could not be stored."); }
  }

  async function shareSheet(sheet: Sheet) {
    const shareData = { title: sheet.name, text: `View the ${sheet.name} budget sheet.`, url: window.location.href };
    try {
      if (navigator.share) { await navigator.share(shareData); return; }
      await navigator.clipboard.writeText(`${shareData.text} ${shareData.url}`);
      setNotice("Sheet link copied.");
    } catch (error) {
      if ((error as DOMException).name !== "AbortError") setNotice("Sharing is unavailable on this device.");
    }
  }

  function selectedWithTransferPairs(ids = selectedTransactionIds) {
    const expanded = new Set(ids);
    transactions.forEach((item) => { if (expanded.has(item.id) && item.transferGroupId) transactions.filter((candidate) => candidate.transferGroupId === item.transferGroupId).forEach((candidate) => expanded.add(candidate.id)); });
    return expanded;
  }

  function applyBatchField(field: "merchant" | "category", value: string) {
    const selected = selectedWithTransferPairs();
    if (!selected.size || !value.trim()) return;
    setTransactions((items) => items.map((item) => selected.has(item.id) ? { ...item, [field]: value.trim() } : item));
    setShowBatchMenu(false);
    setNotice(`${selected.size} transaction${selected.size === 1 ? "" : "s"} updated.`);
  }

  function deleteSelectedTransactions() {
    const selected = selectedWithTransferPairs();
    setTransactions((items) => items.filter((item) => !selected.has(item.id)));
    setSelectedTransactionIds([]);
    setShowBatchMenu(false);
    setNotice(`${selected.size} transaction${selected.size === 1 ? "" : "s"} deleted.`);
  }

  function moveSelectedTransactions(sheetId: string) {
    const selected = selectedWithTransferPairs();
    const transferGroups = new Set(transactions.filter((item) => selected.has(item.id) && item.transferGroupId).map((item) => item.transferGroupId));
    setTransactions((items) => items.map((item) => {
      if (item.transferGroupId && transferGroups.has(item.transferGroupId)) {
        const movingSide = items.find((candidate) => candidate.transferGroupId === item.transferGroupId && candidate.sheetId === activeSheetId) || items.find((candidate) => candidate.transferGroupId === item.transferGroupId && selected.has(candidate.id));
        if (item.id !== movingSide?.id) return item;
        return { ...item, sheetId, sheet: nameForSheet(sheetId), title: item.transferDirection === "out" ? `Transfer to ${nameForSheet(items.find((candidate) => candidate.transferGroupId === item.transferGroupId && candidate.id !== item.id)?.sheetId || "")}` : `Transfer from ${nameForSheet(items.find((candidate) => candidate.transferGroupId === item.transferGroupId && candidate.id !== item.id)?.sheetId || "")}` };
      }
      return selected.has(item.id) ? { ...item, sheetId, sheet: nameForSheet(sheetId) } : item;
    }));
    setShowBatchMenu(false);
    setSelectedTransactionIds([]);
    setNotice("Selected transactions moved.");
  }

  function importTransactions(sheetId: string, rows: ImportRow[]) {
    const imported = rows.map((row) => ({ id: uid("txn"), title: row.category, amount: row.amount, kind: row.kind, category: row.category, date: row.date, time: row.time || "12:00", paidBy: "Nadia", participants: ["Nadia"], splitMethod: "equal" as SplitMethod, notes: row.notes, pending: false, recurring: "", currency: row.currency || "SGD", merchant: row.merchant || undefined, source: "manual" as const, sheetId, sheet: nameForSheet(sheetId) }));
    setTransactions((items) => [...imported, ...items]);
    setNotice(`${imported.length} transaction${imported.length === 1 ? "" : "s"} imported.`);
  }

  function toggleTransactionSelection(transactionId: string) {
    const transaction = transactions.find((item) => item.id === transactionId);
    const linkedIds = transaction?.transferGroupId ? transactions.filter((item) => item.transferGroupId === transaction.transferGroupId).map((item) => item.id) : [transactionId];
    setSelectedTransactionIds((ids) => linkedIds.every((id) => ids.includes(id)) ? ids.filter((id) => !linkedIds.includes(id)) : [...new Set([...ids, ...linkedIds])]);
  }

  function archiveSheet(sheetId: string, archived: boolean) {
    setSheets((items) => items.map((sheet) => sheet.id === sheetId ? { ...sheet, archived } : sheet));
    if (archived && activeSheetId === sheetId) setActiveSheetId(null);
    setNotice(archived ? "Sheet archived." : "Sheet restored.");
  }

  function deleteSheet(sheetId: string) {
    setSheets((items) => items.filter((sheet) => sheet.id !== sheetId));
    setTransactions((items) => items.filter((item) => item.sheetId !== sheetId));
    if (activeSheetId === sheetId) setActiveSheetId(null);
    setNotice("Sheet permanently deleted.");
  }

  function deleteTransaction(transaction: Transaction) {
    setTransactions((items) => items.filter((item) => transaction.transferGroupId ? item.transferGroupId !== transaction.transferGroupId : item.id !== transaction.id));
    setNotice(transaction.transferGroupId ? "Linked transfer deleted." : "Transaction deleted.");
  }

  function duplicateTransaction(transaction: Transaction, useToday: boolean) {
    const group = transaction.transferGroupId ? transactions.filter((item) => item.transferGroupId === transaction.transferGroupId) : [transaction];
    const transferGroupId = transaction.transferGroupId ? uid("transfer") : undefined;
    const copies = group.map((item) => ({ ...item, id: uid("txn"), date: useToday ? todayIso() : item.date, transferGroupId }));
    setTransactions((items) => [...copies, ...items]);
    setNotice(useToday ? "Transaction duplicated to today." : "Transaction duplicated.");
  }

  function moveTransaction(transaction: Transaction, sheetId: string) {
    if (transaction.transferGroupId) {
      const counterpart = transactions.find((item) => item.transferGroupId === transaction.transferGroupId && item.id !== transaction.id);
      if (counterpart?.sheetId === sheetId) { setNotice("A transfer needs two different sheets."); return; }
      setTransactions((items) => items.map((item) => {
        if (item.transferGroupId !== transaction.transferGroupId) return item;
        const nextSheetId = item.id === transaction.id ? sheetId : item.sheetId;
        const otherSheetId = item.id === transaction.id ? counterpart?.sheetId || "" : sheetId;
        return { ...item, sheetId: nextSheetId, sheet: nameForSheet(nextSheetId || ""), title: item.transferDirection === "out" ? `Transfer to ${nameForSheet(otherSheetId)}` : `Transfer from ${nameForSheet(otherSheetId)}` };
      }));
    } else setTransactions((items) => items.map((item) => item.id === transaction.id ? { ...item, sheetId, sheet: nameForSheet(sheetId) } : item));
    setMoveTransactionId(null);
    setNotice("Transaction moved.");
  }

  async function copyTransactionAmount(transaction: Transaction) {
    const incoming = transaction.kind === "income" || transaction.transferDirection === "in";
    const value = `${incoming ? "+" : "-"}${money(transaction.amount, transaction.currency)}`;
    try { await navigator.clipboard?.writeText(value); setNotice(`${value} copied.`); }
    catch {
      const fallback = document.createElement("textarea");
      fallback.value = value;
      document.body.append(fallback);
      fallback.select();
      document.execCommand("copy");
      fallback.remove();
      setNotice(`${value} copied.`);
    }
  }

  return <main className="sheets-app">
    {activeSheet ? sheetAction === "select" ? <SheetSelectionView sheet={activeSheet} items={transactions.filter((item) => item.sheetId === activeSheet.id)} sensitive={sensitive} selectedIds={selectedTransactionIds} showMenu={showBatchMenu} onClose={() => { setSheetAction(null); setSelectedTransactionIds([]); setShowBatchMenu(false); }} onToggle={toggleTransactionSelection} onToggleMenu={() => setShowBatchMenu((value) => !value)} onMove={moveSelectedTransactions} onMerchant={(merchant) => applyBatchField("merchant", merchant)} onCategory={(category) => applyBatchField("category", category)} onDelete={() => setConfirmation({ title: "Delete selected transactions?", description: "Selected transactions and both sides of linked transfers will be permanently deleted.", label: "Delete", onConfirm: deleteSelectedTransactions })} sheets={activeSheets} /> : sheetAction ? <SheetActionView action={sheetAction} sheet={activeSheet} sheets={activeSheets} items={transactions} sensitive={sensitive} onClose={() => setSheetAction(null)} onOpenAction={setSheetAction} onUpdateSheet={updateSheet} onDeleteSheet={() => setConfirmation({ title: "Delete sheet?", description: `“${activeSheet.name}” and all of its transactions will be permanently deleted.`, label: "Move to Trash", onConfirm: () => { deleteSheet(activeSheet.id); setSheetAction(null); } })} onImport={importTransactions} /> : <SheetLedger sheet={activeSheet} items={transactions} search={search} sensitive={sensitive} filters={filters} onSearch={setSearch} onBack={() => { setActiveSheetId(null); setSearch(""); setFilters(emptyLedgerFilters); }} onAdd={() => setShowAdd(true)} onOpenFilters={() => { setShowSheetMenu(false); setShowFilters(true); }} onToggleMenu={() => setShowSheetMenu((open) => !open)} showMenu={showSheetMenu} onShare={() => { void shareSheet(activeSheet); }} onOpenAction={(action) => { setShowSheetMenu(false); setSheetAction(action); }} onSelectTransaction={activeSheet.archived ? undefined : (transactionId) => { setEditingFocus(undefined); setEditingTransactionId(transactionId); }} onLongPressTransaction={activeSheet.archived ? undefined : setActionTransactionId} /> : <SheetsHome sheets={sheets} items={transactions} search={search} sensitive={sensitive} onSearch={setSearch} onOpenSheet={setActiveSheetId} onAdd={() => activeSheets.length ? setShowAdd(true) : setShowNewSheet(true)} onToggleMenu={() => setShowMainMenu((open) => !open)} showMenu={showMainMenu} onOpenSettings={() => { setShowMainMenu(false); setShowSettings(true); }} onNewSheet={() => { setShowMainMenu(false); setShowNewSheet(true); }} onArchive={archiveSheet} onDelete={(sheet) => setConfirmation({ title: "Delete sheet?", description: `“${sheet.name}” and all of its transactions will be permanently deleted.`, label: "Delete sheet", onConfirm: () => deleteSheet(sheet.id) })} />}
    {showAdd && <SheetTransactionComposer sheets={activeSheets} defaultSheetId={preset?.sheetId || activeSheetId || activeSheets[0]?.id || ""} preset={preset || undefined} onClose={() => { setShowAdd(false); setPreset(null); }} onSave={createTransaction} />}
    {showNewSheet && <NewSheetComposer onClose={() => setShowNewSheet(false)} onSave={createSheet} />}
    {editingTransaction && <SheetTransactionComposer sheets={activeSheets} defaultSheetId={editingTransaction.sheetId || ""} transaction={editingTransaction} transferPartner={editingTransaction.transferGroupId ? transactions.find((item) => item.transferGroupId === editingTransaction.transferGroupId && item.id !== editingTransaction.id) : undefined} focusField={editingFocus} onClose={() => { setEditingFocus(undefined); setEditingTransactionId(null); }} onSave={updateTransaction} />}
    {showFilters && <LedgerFilterSheet items={transactions} filters={filters} onClose={() => setShowFilters(false)} onApply={(next) => { setFilters(next); setShowFilters(false); }} />}
    {showSettings && <SettingsSheet sensitive={sensitive} onClose={() => setShowSettings(false)} onToggleSensitive={() => setSensitive((value) => !value)} />}
    {actionTransaction && <TransactionActionSheet transaction={actionTransaction} onClose={() => setActionTransactionId(null)} onNewExpense={() => { setPreset({ category: actionTransaction.category, sheetId: actionTransaction.sheetId || "" }); setActionTransactionId(null); setShowAdd(true); }} onEdit={(focus) => { setEditingFocus(focus); setActionTransactionId(null); setEditingTransactionId(actionTransaction.id); }} onMove={() => { setActionTransactionId(null); setMoveTransactionId(actionTransaction.id); }} onDuplicate={(today) => { duplicateTransaction(actionTransaction, today); setActionTransactionId(null); }} onCopy={() => { void copyTransactionAmount(actionTransaction); setActionTransactionId(null); }} onDelete={() => { setActionTransactionId(null); setConfirmation({ title: "Delete transaction?", description: actionTransaction.transferGroupId ? "Both sides of this linked transfer will be permanently deleted." : "This transaction will be permanently deleted.", label: "Delete transaction", onConfirm: () => deleteTransaction(actionTransaction) }); }} />}
    {movingTransaction && <MoveTransactionSheet transaction={movingTransaction} sheets={activeSheets} onClose={() => setMoveTransactionId(null)} onMove={(sheetId) => moveTransaction(movingTransaction, sheetId)} />}
    {confirmation && <ConfirmationDialog {...confirmation} onClose={() => setConfirmation(null)} onConfirm={() => { confirmation.onConfirm(); setConfirmation(null); }} />}
    {notice && <div className="toast sheets-toast" role="status">{notice}</div>}
  </main>;
}

function SheetsHome({ sheets, items, search, sensitive, onSearch, onOpenSheet, onAdd, onToggleMenu, showMenu, onOpenSettings, onNewSheet, onArchive, onDelete }: { sheets: Sheet[]; items: Transaction[]; search: string; sensitive: boolean; onSearch: (value: string) => void; onOpenSheet: (sheetId: string) => void; onAdd: () => void; onToggleMenu: () => void; showMenu: boolean; onOpenSettings: () => void; onNewSheet: () => void; onArchive: (sheetId: string, archived: boolean) => void; onDelete: (sheet: Sheet) => void }) {
  const query = search.trim();
  const matches = useMemo(() => query ? searchTransactions(items, query) : [], [items, query]);
  const currentSheets = sheets.filter((sheet) => !sheet.archived);
  const archivedSheets = sheets.filter((sheet) => sheet.archived);

  return <section className="sheets-screen">
    <header className="sheets-heading"><h1>Sheets</h1><div className="menu-anchor"><button type="button" className="heading-more" onClick={onToggleMenu} aria-expanded={showMenu} aria-label="Sheets menu"><AppIcon name="more" size="md" /></button>{showMenu && <MainOverflowMenu onNewSheet={onNewSheet} onOpenSettings={onOpenSettings} />}</div></header>
    {query ? <SearchResults items={matches} query={query} sensitive={sensitive} /> : <><div className="sheets-stack">{currentSheets.map((sheet, index) => <SheetCard key={sheet.id} sheet={sheet} position={index} allSheets={sheets} items={items} sensitive={sensitive} onOpen={() => onOpenSheet(sheet.id)} onArchive={() => onArchive(sheet.id, true)} onDelete={() => onDelete(sheet)} />)}</div>{!currentSheets.length && <p className="sheet-empty">Create a sheet to start recording transactions.</p>}{archivedSheets.length > 0 && <section className="archived-sheets"><h2>Archived <AppIcon name="chevronDown" size="sm" /></h2><div className="sheets-stack">{archivedSheets.map((sheet, index) => <SheetCard key={sheet.id} sheet={sheet} position={index} allSheets={sheets} items={items} sensitive={sensitive} onOpen={() => onOpenSheet(sheet.id)} onArchive={() => onArchive(sheet.id, false)} onDelete={() => onDelete(sheet)} />)}</div></section>}</>}
    <SearchField value={search} onChange={onSearch} onAdd={onAdd} />
  </section>;
}

function SheetCard({ sheet, position, allSheets, items, sensitive, onOpen, onArchive, onDelete }: { sheet: Sheet; position: number; allSheets: Sheet[]; items: Transaction[]; sensitive: boolean; onOpen: () => void; onArchive: () => void; onDelete: () => void }) {
  const [open, setOpen] = useState(false);
  const startX = useRef<number | null>(null);
  const suppressClick = useRef(false);
  const sheetItems = items.filter((item) => item.sheetId === sheet.id);
  const balance = sheetBalance(sheetItems);
  const latest = sheetItems.reduce<Transaction | undefined>((recent, item) => !recent || transactionSortKey(item) > transactionSortKey(recent) ? item : recent, undefined);
  function pointerDown(event: React.PointerEvent<HTMLDivElement>) { startX.current = event.clientX; }
  function pointerUp(event: React.PointerEvent<HTMLDivElement>) { if (startX.current !== null && Math.abs(event.clientX - startX.current) > 36) { setOpen((value) => !value); suppressClick.current = true; window.setTimeout(() => { suppressClick.current = false; }, 0); } startX.current = null; }
  return <div className={open ? "sheet-swipe open" : "sheet-swipe"} onPointerDown={pointerDown} onPointerUp={pointerUp} onPointerCancel={() => { startX.current = null; }}><button type="button" className={sheet.archived ? "sheet-card archived" : "sheet-card"} onClick={() => { if (!suppressClick.current) onOpen(); }}><span className="sheet-card-main"><strong>{sheetDisplayName(sheet, position, allSheets)}</strong><small>{sensitive ? "••••••" : money(balance, sheet.currency)}</small></span><span className="sheet-card-meta"><small>{latest ? latestActivity(latest) : "No entries yet"}</small><b>{sheetItems.length}</b><AppIcon name="forward" size="sm" /></span></button><div className="sheet-swipe-actions"><button type="button" className="sheet-archive" onClick={() => { onArchive(); setOpen(false); }} aria-label={sheet.archived ? "Unarchive sheet" : "Archive sheet"}><AppIcon name={sheet.archived ? "receipt" : "table"} size="md" /></button><button type="button" className="sheet-delete" onClick={onDelete} aria-label="Delete sheet"><AppIcon name="trash" size="md" /></button></div></div>;
}

function SheetLedger({ sheet, items, search, sensitive, filters, onSearch, onBack, onAdd, onOpenFilters, onToggleMenu, showMenu, onShare, onOpenAction, onSelectTransaction, onLongPressTransaction }: { sheet: Sheet; items: Transaction[]; search: string; sensitive: boolean; filters: LedgerFilters; onSearch: (value: string) => void; onBack: () => void; onAdd: () => void; onOpenFilters: () => void; onToggleMenu: () => void; showMenu: boolean; onShare: () => void; onOpenAction: (action: SheetAction) => void; onSelectTransaction?: (transactionId: string) => void; onLongPressTransaction?: (transactionId: string) => void }) {
  const sheetItems = useMemo(() => items.filter((item) => item.sheetId === sheet.id), [items, sheet.id]);
  const totalItems = useMemo(() => sheet.totalPeriod === "today" ? sheetItems.filter((item) => item.date <= todayIso()) : sheetItems, [sheet.totalPeriod, sheetItems]);
  const searchedItems = useMemo(() => search.trim() ? searchTransactions(items, search) : sheetItems, [items, search, sheetItems]);
  const visibleItems = useMemo(() => filterTransactions(searchedItems, filters), [searchedItems, filters]);
  const totals = useMemo(() => sheetTotals(totalItems), [totalItems]);
  const hasFilters = !isEmptyFilters(filters);
  return <section className="sheets-screen ledger-screen">
    <header className="sheet-ledger-heading"><button type="button" className="round-control" onClick={onBack} aria-label="Back to sheets"><AppIcon name="back" size="md" /></button><h1>{search.trim() ? "Search" : sheet.name}</h1><div className="sheet-header-actions"><button type="button" className="round-control" onClick={onShare} aria-label="Share sheet"><AppIcon name="personAdd" size="md" /></button><div className="menu-anchor"><button type="button" className="round-control" onClick={onToggleMenu} aria-expanded={showMenu} aria-label="Sheet menu"><AppIcon name="more" size="md" /></button>{showMenu && <SheetOverflowMenu onSelect={onOpenAction} />}</div></div></header>
    {sheet.archived && <p className="archived-notice">Archived sheet · read-only</p>}
    {!search.trim() && <SheetSummary sheet={sheet} totals={totals} sensitive={sensitive} />}
    {!search.trim() && <div className="ledger-period-row"><span className="ledger-period"><AppIcon name="receipt" size="sm" /> {sheet.totalPeriod === "today" ? "As of Today" : "All Time"}</span><span className="ledger-transfer-count"><AppIcon name="transfer" size="sm" /> {sheetItems.filter((item) => item.kind === "transfer").length}</span></div>}
    <TransactionList items={visibleItems} sensitive={sensitive} showDailyTotals={!search.trim()} showSheet={Boolean(search.trim())} emptyMessage={search.trim() || hasFilters ? "No transactions match this view." : "This sheet has no transactions yet."} onSelect={onSelectTransaction} onLongPress={onLongPressTransaction} />
    <SearchField value={search} onChange={onSearch} onAdd={onAdd} onFilter={onOpenFilters} filtersActive={hasFilters} disabled={sheet.archived} />
  </section>;
}

function SearchField({ value, onChange, onAdd, onFilter, filtersActive = false, disabled = false }: { value: string; onChange: (value: string) => void; onAdd: () => void; onFilter?: () => void; filtersActive?: boolean; disabled?: boolean }) {
  return <div className="sheet-search-dock">{onFilter && <button type="button" className={filtersActive ? "search-filter active" : "search-filter"} onClick={onFilter} aria-label="Filter transactions"><AppIcon name="sliders" size="md" /></button>}<label className="sheet-search"><AppIcon name="search" size="md" /><input value={value} onChange={(event) => onChange(event.target.value)} placeholder="Search" aria-label="Search transactions across sheets" />{value && <button type="button" onClick={() => onChange("")} aria-label="Clear search"><AppIcon name="close" size="sm" /></button>}</label><button type="button" className="search-add" onClick={onAdd} aria-label="Add transaction" disabled={disabled}><AppIcon name="plus" size="lg" /></button></div>;
}

function MainOverflowMenu({ onNewSheet, onOpenSettings }: { onNewSheet: () => void; onOpenSettings: () => void }) {
  return <div className="glass-menu main-overflow" role="menu"><button type="button" role="menuitem" onClick={onNewSheet}><AppIcon name="documentAdd" size="md" />New Sheet</button><button type="button" role="menuitem" onClick={onOpenSettings}><AppIcon name="settings" size="md" />Settings</button></div>;
}

function SheetOverflowMenu({ onSelect }: { onSelect: (action: SheetAction) => void }) {
  const firstGroup: Array<[IconKey, string]> = [["chartPie", "Stats"], ["insights", "Trends"], ["bank", "Exchange Rate"]];
  const secondGroup: Array<[IconKey, string]> = [["check", "Select"], ["printer", "Print"], ["upload", "Export"], ["download", "Import"]];
  const toAction = (label: string): SheetAction => ({ Stats: "stats", Trends: "trends", "Exchange Rate": "exchange", Select: "select", Print: "print", Export: "export", Import: "import" })[label] as SheetAction;
  return <div className="glass-menu sheet-overflow" role="menu"><div className="menu-group">{firstGroup.map(([icon, label]) => <button type="button" role="menuitem" onClick={() => onSelect(toAction(label))} key={label}><AppIcon name={icon} size="md" />{label}</button>)}</div><div className="menu-group">{secondGroup.map(([icon, label]) => <button type="button" role="menuitem" onClick={() => onSelect(toAction(label))} key={label}><AppIcon name={icon} size="md" />{label}</button>)}</div><button type="button" role="menuitem" onClick={() => onSelect("edit")}><AppIcon name="pencil" size="md" />Edit Sheet</button></div>;
}

function SheetSummary({ sheet, totals, sensitive }: { sheet: Sheet; totals: { balance: number; expense: number; income: number }; sensitive: boolean }) {
  const hidden = "••••••";
  return <section className="sheet-summary"><div className="sheet-summary-top"><b>{sheet.currency}</b><span>{sheet.totalPeriod === "today" ? "As of Today" : "All Time"} <AppIcon name="chevronDown" size="sm" /></span></div>{sheet.showTotalBalance && <strong>{sensitive ? hidden : money(totals.balance, sheet.currency)}</strong>}<div className="sheet-summary-columns"><span>Expense <b>{sensitive ? hidden : `−${money(totals.expense, sheet.currency)}`}</b></span><span>Income <b>{sensitive ? hidden : money(totals.income, sheet.currency)}</b></span></div></section>;
}

function SettingsSheet({ sensitive, onClose, onToggleSensitive }: { sensitive: boolean; onClose: () => void; onToggleSensitive: () => void }) {
  return <div className="sheet-modal-backdrop"><section className="settings-sheet" aria-label="Settings"><header className="overlay-header"><button type="button" className="round-control" onClick={onClose} aria-label="Close settings"><AppIcon name="close" size="lg" /></button><h1>Settings</h1><span /></header><SettingsGroup><SettingsRow icon="printer" label="Print Settings" /><SettingsRow icon="bank" label="Preferred Currency" value="SGD" /></SettingsGroup><SettingsGroup><SettingsRow icon="lock" label="Privacy" /><button type="button" className="settings-row toggle-settings" onClick={onToggleSensitive} aria-pressed={sensitive}><span className="settings-icon neutral"><AppIcon name="eyeSlash" size="md" /></span><span><b>Sensitive Mode</b><small>Hide all sensitive data.</small></span><span className={sensitive ? "composer-switch on" : "composer-switch"}><i /></span></button></SettingsGroup><SettingsGroup><SettingsRow icon="settings" label="App Settings" external /></SettingsGroup><SettingsGroup><SettingsRow icon="goals" label="Expenses Pro" /><SettingsRow icon="sync" label="Sync" /><SettingsRow icon="ledger" label="Categories" /><SettingsRow icon="upload" label="Export" /><SettingsRow icon="download" label="Import" /><SettingsRow icon="trash" label="Trash" /></SettingsGroup><SettingsGroup><SettingsRow icon="palette" label="Appearance" /><SettingsRow icon="table" label="App Icon" /><SettingsRow icon="sync" label="Sort Sheets By" /><SettingsRow icon="printer" label="Print Settings" /></SettingsGroup></section></div>;
}

function SettingsGroup({ children }: { children: React.ReactNode }) { return <section className="settings-card">{children}</section>; }

function SettingsRow({ icon, label, value, external = false }: { icon: IconKey; label: string; value?: string; external?: boolean }) { return <button type="button" className="settings-row" aria-disabled="true"><span className={`settings-icon ${icon}`}><AppIcon name={icon} size="md" /></span><b>{label}</b><span className="settings-row-end">{value}{external ? "↗" : <AppIcon name="forward" size="sm" />}</span></button>; }

function LedgerFilterSheet({ items, filters, onClose, onApply }: { items: Transaction[]; filters: LedgerFilters; onClose: () => void; onApply: (filters: LedgerFilters) => void }) {
  const [draft, setDraft] = useState(filters);
  const availableCategories = [...new Set(items.map((item) => item.category))].sort();
  const update = <Key extends keyof LedgerFilters>(key: Key, value: LedgerFilters[Key]) => setDraft((current) => ({ ...current, [key]: value }));
  return <div className="sheet-modal-backdrop"><section className="filter-sheet" aria-label="Filters"><header className="overlay-header"><button type="button" className="round-control" onClick={onClose} aria-label="Close filters"><AppIcon name="close" size="lg" /></button><h1>Filters</h1><button type="button" className="filter-apply" onClick={() => onApply(draft)} aria-label="Apply filters"><AppIcon name="check" size="lg" /></button></header><button type="button" className="filter-reset" onClick={() => onApply(emptyLedgerFilters)}>No filter <AppIcon name="check" size="sm" /></button><section className="filter-card"><FilterToggle icon="photo" label="Image" checked={draft.hasAttachment} onClick={() => update("hasAttachment", !draft.hasAttachment)} /><FilterToggle icon="repeat" label="Repeat" checked={draft.recurring} onClick={() => update("recurring", !draft.recurring)} /></section><section className="filter-card"><FilterSelect icon="sync" label="Type" value={draft.kind} onChange={(value) => update("kind", value as LedgerFilters["kind"])} options={[["all", "All"], ["expense", "Expense"], ["income", "Income"], ["transfer", "Transfer"]]} /><label className="filter-row"><span># <b>Amount</b></span><input value={draft.amount} onChange={(event) => update("amount", event.target.value)} inputMode="decimal" type="number" min="0" placeholder="Amount" /><select value={draft.amountMatch} onChange={(event) => update("amountMatch", event.target.value as AmountMatch)}><option value="exactly">Exactly</option><option value="atLeast">At least</option><option value="atMost">At most</option></select></label><FilterSelect icon="bank" label="Currency" value={draft.currency} onChange={(value) => update("currency", value)} options={[["all", "All"], ["SGD", "SGD"]]} /><label className="filter-row filter-notes"><span><AppIcon name="receipt" size="sm" /><b>Notes</b></span><input value={draft.notes} onChange={(event) => update("notes", event.target.value)} placeholder="Contains text" /></label><FilterSelect icon="ledger" label="Category" value={draft.category} onChange={(value) => update("category", value)} options={[["all", "All"], ...availableCategories.map((category) => [category, category])]} /><FilterSelect icon="calendar" label="Date" value={draft.dateMatch} onChange={(value) => update("dateMatch", value as DateMatch)} options={[["all", "All"], ["today", "Today"], ["custom", "Custom date"]]} />{draft.dateMatch === "custom" && <label className="filter-row"><span><AppIcon name="calendar" size="sm" /><b>On date</b></span><input type="date" value={draft.date} onChange={(event) => update("date", event.target.value)} /></label>}</section></section></div>;
}

function FilterToggle({ icon, label, checked, onClick }: { icon: IconKey; label: string; checked: boolean; onClick: () => void }) { return <button type="button" className={checked ? "filter-row filter-toggle selected" : "filter-row filter-toggle"} onClick={onClick} aria-pressed={checked}><span><AppIcon name={icon} size="sm" /><b>{label}</b></span>{checked && <AppIcon name="check" size="sm" />}</button>; }

function FilterSelect({ icon, label, value, onChange, options }: { icon: IconKey; label: string; value: string; onChange: (value: string) => void; options: Array<string[]> }) { return <label className="filter-row"><span><AppIcon name={icon} size="sm" /><b>{label}</b></span><select value={value} onChange={(event) => onChange(event.target.value)}>{options.map(([optionValue, optionLabel]) => <option value={optionValue} key={optionValue}>{optionLabel}</option>)}</select></label>; }

function SearchResults({ items, query, sensitive }: { items: Transaction[]; query: string; sensitive: boolean }) {
  return <section className="search-results" aria-live="polite"><p className="search-summary">{items.length ? `${items.length} result${items.length === 1 ? "" : "s"} for “${query}”` : `No results for “${query}”`}</p>{items.length > 0 && <TransactionList items={items} sensitive={sensitive} showSheet emptyMessage="" />}</section>;
}

function TransactionList({ items, sensitive = false, showDailyTotals = false, showSheet = false, emptyMessage, onSelect, onLongPress }: { items: Transaction[]; sensitive?: boolean; showDailyTotals?: boolean; showSheet?: boolean; emptyMessage: string; onSelect?: (transactionId: string) => void; onLongPress?: (transactionId: string) => void }) {
  const grouped = useMemo(() => groupTransactions(items), [items]);
  const pressTimer = useRef<number | null>(null);
  const held = useRef(false);
  function clearPress() { if (pressTimer.current !== null) window.clearTimeout(pressTimer.current); pressTimer.current = null; }
  if (!items.length) return <p className="sheet-empty">{emptyMessage}</p>;
  return <div className="sheet-transaction-list">{grouped.map(([date, group]) => <section key={date}><div className="transaction-date-heading"><h2>{transactionDateLabel(date)}</h2>{showDailyTotals && <b className={dayNet(group) >= 0 ? "positive" : ""}>{sensitive ? "••••" : signedMoney(dayNet(group))}</b>}</div>{group.map((item) => <article className={onSelect ? "sheet-transaction interactive" : "sheet-transaction"} key={item.id} role={onSelect ? "button" : undefined} tabIndex={onSelect ? 0 : undefined} onPointerDown={() => { if (!onLongPress) return; held.current = false; pressTimer.current = window.setTimeout(() => { held.current = true; onLongPress(item.id); }, 460); }} onPointerUp={clearPress} onPointerLeave={clearPress} onPointerCancel={clearPress} onContextMenu={(event) => { if (!onLongPress) return; event.preventDefault(); onLongPress(item.id); }} onClick={() => { if (held.current) { held.current = false; return; } onSelect?.(item.id); }} onKeyDown={(event) => { if (event.key === "Enter" || event.key === " ") { event.preventDefault(); onSelect?.(item.id); } }}><span className={`sheet-category-icon ${item.kind}`}><AppIcon name={iconFor(item.category)} size="sm" /></span><div><strong>{item.title}</strong><small>{item.notes || item.category}{showSheet && item.sheet ? ` · ${item.sheet}` : ""}</small></div><div className={item.kind === "income" || item.transferDirection === "in" ? "sheet-amount positive" : "sheet-amount"}>{sensitive ? "••••" : <>{item.kind === "income" || item.transferDirection === "in" ? "+" : "−"}{money(item.amount, item.currency)}</>}<small>{item.time || ""}</small></div></article>)}</section>)}</div>;
}

function SheetTransactionComposer({ sheets, defaultSheetId, transaction, transferPartner, preset, focusField, onClose, onSave }: { sheets: Sheet[]; defaultSheetId: string; transaction?: Transaction; transferPartner?: Transaction; preset?: ComposerPreset; focusField?: "merchant" | "category"; onClose: () => void; onSave: (draft: TransactionDraft) => void }) {
  const now = new Date();
  const initialKind = transaction?.kind === "income" ? "income" : transaction?.kind === "transfer" ? "transfer" : "expense";
  const initialSheetId = transaction?.sheetId || preset?.sheetId || defaultSheetId;
  const initialFromSheetId = transaction?.transferDirection === "out" ? transaction.sheetId || defaultSheetId : transferPartner?.sheetId || defaultSheetId;
  const initialToSheetId = transaction?.transferDirection === "in" ? transaction.sheetId || "" : transferPartner?.sheetId || "";
  const [kind, setKind] = useState<"expense" | "income" | "transfer">(initialKind);
  const [amount, setAmount] = useState(transaction ? String(transaction.amount) : "");
  const [title, setTitle] = useState(transaction?.title || "");
  const [merchant, setMerchant] = useState(transaction?.merchant || "");
  const [notes, setNotes] = useState(transaction?.notes || "");
  const [category, setCategory] = useState(transaction?.category || preset?.category || expenseCategories[0]);
  const [sheetId, setSheetId] = useState(initialSheetId);
  const [fromSheetId, setFromSheetId] = useState(initialFromSheetId);
  const [toSheetId, setToSheetId] = useState(initialToSheetId);
  const [date, setDate] = useState(transaction?.date || todayIso());
  const [time, setTime] = useState(transaction?.time || now.toTimeString().slice(0, 5));
  const [pending, setPending] = useState(Boolean(transaction?.pending));
  const [recurring, setRecurring] = useState(transaction?.recurring || "");
  const [attachmentMessage, setAttachmentMessage] = useState("");
  const [hasAttachment, setHasAttachment] = useState(Boolean(transaction?.hasAttachment));
  const [attachmentFiles, setAttachmentFiles] = useState<File[]>([]);
  const selectedSheet = sheets.find((sheet) => sheet.id === (kind === "transfer" ? fromSheetId : sheetId));
  const input = selectedSheet?.input || defaultSheet.input;
  const categoryOptions = kind === "income" ? incomeCategories : expenseCategories;
  const destinationSheets = sheets.filter((entry) => entry.id !== fromSheetId);
  const modeOptions: Array<"expense" | "income" | "transfer"> = transaction && initialKind !== "transfer" ? ["expense", "income"] : ["expense", "income", "transfer"];

  function chooseKind(next: "expense" | "income" | "transfer") { setKind(next); if (next !== kind) setCategory(next === "income" ? incomeCategories[0] : next === "expense" ? expenseCategories[0] : "Transfer"); }
  async function scanReceipt(file?: File) {
    if (!file) return;
    setAttachmentFiles((files) => [...files, file]);
    setHasAttachment(true);
    if (!file.type.startsWith("image/")) { setAttachmentMessage(`${file.name} attached. Choose an image for receipt suggestions.`); return; }
    if (file.size > 4_500_000) { setAttachmentMessage(`${file.name} attached. It is too large for receipt suggestions.`); return; }
    setAttachmentMessage("Reading receipt…");
    const imageDataUrl = await new Promise<string>((resolve, reject) => { const reader = new FileReader(); reader.onload = () => resolve(String(reader.result)); reader.onerror = reject; reader.readAsDataURL(file); }).catch(() => "");
    if (!imageDataUrl) { setAttachmentMessage("The image could not be read; it is still attached."); return; }
    const response = await fetch("/api/receipt", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ imageDataUrl }) }).catch(() => null);
    const body = response ? await response.json().catch(() => ({})) : {};
    if (body.suggestion) { const suggestion = body.suggestion; const details = [suggestion.merchant, suggestion.amount && `${suggestion.currency || "SGD"} ${suggestion.amount}`, suggestion.date, suggestion.category].filter(Boolean); setAttachmentMessage(details.length ? `Suggestion: ${details.join(" · ")}. Review before saving.` : "Receipt attached. Review its details before saving."); } else setAttachmentMessage(body.error || "Receipt attached. Suggestions are unavailable.");
  }
  function submit(event: FormEvent<HTMLFormElement>) { event.preventDefault(); onSave({ amount: Number(amount), attachments: attachmentFiles, category, date, fromSheetId, hasAttachment, kind, merchant, notes, pending, recurring, sheetId, time, title, toSheetId }); }
  function labelForSheet(sheet: Sheet) { return sheetDisplayName(sheet, sheets.indexOf(sheet), sheets); }

  return <div className="sheet-composer-backdrop" role="presentation"><form className="sheet-composer" onSubmit={submit} aria-label={transaction ? "Transaction details" : "New transaction"}><header className="composer-header"><button type="button" className="round-control" onClick={onClose} aria-label={transaction ? "Close transaction details" : "Discard transaction"}><AppIcon name="close" size="lg" /></button><h1>{transaction ? "Details" : "New Item"}</h1><button className="composer-save" type="submit" aria-label={transaction ? "Save transaction details" : "Save transaction"}><AppIcon name="check" size="lg" /></button></header><div className="composer-segment" role="tablist" aria-label="Transaction type">{modeOptions.map((option) => <button key={option} type="button" role="tab" aria-selected={kind === option} className={kind === option ? "selected" : ""} onClick={() => chooseKind(option)}>{option[0].toUpperCase() + option.slice(1)}</button>)}</div><section className="composer-card amount-card"><label><span className="sr-only">Amount</span><input value={amount} onChange={(event) => setAmount(event.target.value)} type="number" inputMode="decimal" min="0.01" step="0.01" placeholder="Amount" autoFocus={!focusField} required /></label>{input.showCurrencySelection && <div className="composer-row currency-row"><span>◉ <b>SGD</b></span><span>Singapore Dollar <AppIcon name="forward" size="sm" /></span></div>}</section><small className="amount-preview">{amount ? money(Number(amount) || 0) : "$0.00"}</small><label className="composer-note"><span className="sr-only">Notes</span><input value={notes} onChange={(event) => setNotes(event.target.value)} placeholder="Notes" /></label>{kind === "transfer" ? <section className="composer-card"><label className="composer-row"><span>↑ <b>From:</b></span><select value={fromSheetId} onChange={(event) => { setFromSheetId(event.target.value); if (event.target.value === toSheetId) setToSheetId(""); }}>{sheets.map((sheet) => <option value={sheet.id} key={sheet.id}>{labelForSheet(sheet)}</option>)}</select></label><label className="composer-row"><span>↓ <b>To:</b></span><select value={toSheetId} onChange={(event) => setToSheetId(event.target.value)} disabled={!destinationSheets.length}><option value="">{destinationSheets.length ? "Choose a sheet" : "No other sheets yet"}</option>{destinationSheets.map((sheet) => <option value={sheet.id} key={sheet.id}>{labelForSheet(sheet)}</option>)}</select></label>{!destinationSheets.length && <p className="composer-hint">Add another sheet before recording a transfer.</p>}</section> : <><section className="composer-card">{input.showMerchant && <label className="composer-row"><span>⌂ <b>Merchant</b></span><input value={merchant} onChange={(event) => setMerchant(event.target.value)} placeholder="No merchant" autoFocus={focusField === "merchant"} /></label>}<label className="composer-row category-heading"><span><AppIcon name={iconFor(category)} size="sm" /><b>Category</b></span><select value={category} onChange={(event) => setCategory(event.target.value)} autoFocus={focusField === "category"}>{categoryOptions.map((option) => <option value={option} key={option}>{option}</option>)}</select></label>{input.showCategorySuggestions && <div className="category-chips">{categoryOptions.map((option) => <button type="button" className={category === option ? "selected" : ""} onClick={() => setCategory(option)} key={option}><span className={`chip-icon ${option === "Salary" ? "income" : ""}`}><AppIcon name={iconFor(option)} size="sm" /></span>{option}</button>)}</div>}<label className="composer-row"><span><AppIcon name="table" size="sm" /><b>Sheet</b></span><select value={sheetId} onChange={(event) => setSheetId(event.target.value)}>{sheets.map((sheet) => <option value={sheet.id} key={sheet.id}>{labelForSheet(sheet)}</option>)}</select></label></section></>}<section className="composer-card"><label className="composer-row"><span><AppIcon name="calendar" size="sm" /><b>Date</b></span><input value={date} onChange={(event) => setDate(event.target.value)} type="date" required /></label>{input.showTime && <label className="composer-row"><span><AppIcon name="clock" size="sm" /><b>Time</b></span><input value={time} onChange={(event) => setTime(event.target.value)} type="time" required /></label>}</section><button type="button" className="composer-card composer-row toggle-row" onClick={() => setPending((value) => !value)} aria-pressed={pending}><span><AppIcon name="clock" size="sm" /><b>Pending</b></span><span className={pending ? "composer-switch on" : "composer-switch"}><i /></span></button><section className="composer-card"><label className="composer-row"><span><AppIcon name="transfer" size="sm" /><b>Repeat</b></span><select value={recurring} onChange={(event) => setRecurring(event.target.value)}><option value="">Never</option><option value="Weekly">Weekly</option><option value="Monthly">Monthly</option><option value="Yearly">Yearly</option></select></label></section><label className="composer-image"><AppIcon name="photo" size="sm" /><span>{attachmentMessage || (attachmentFiles.length ? `${attachmentFiles.length} new attachment${attachmentFiles.length === 1 ? "" : "s"}` : transaction?.attachments?.length ? `${transaction.attachments.length} attachment${transaction.attachments.length === 1 ? "" : "s"}` : "Add Image")}</span><input type="file" accept="image/*,.pdf" multiple onChange={(event) => { Array.from(event.target.files || []).forEach((file) => { void scanReceipt(file); }); }} /></label><label className="sr-only">Transaction title<input value={title} onChange={(event) => setTitle(event.target.value)} /></label></form></div>;
}

function NewSheetComposer({ onClose, onSave }: { onClose: () => void; onSave: (draft: SheetDraft) => boolean }) {
  const [name, setName] = useState("untitled sheet");
  const [showTotalBalance, setShowTotalBalance] = useState(true);
  const [totalPeriod, setTotalPeriod] = useState<SheetTotalPeriod>("today");
  const [input, setInput] = useState(defaultSheet.input);
  function toggle(key: keyof Sheet["input"]) { setInput((current) => ({ ...current, [key]: !current[key] })); }
  function submit(event: FormEvent<HTMLFormElement>) { event.preventDefault(); onSave({ name, currency: "SGD", showTotalBalance, totalPeriod, input }); }
  return <div className="sheet-composer-backdrop" role="presentation"><form className="sheet-composer new-sheet-composer" onSubmit={submit} aria-label="New Sheet"><header className="composer-header"><button type="button" className="round-control" onClick={onClose} aria-label="Discard sheet"><AppIcon name="close" size="lg" /></button><h1>New Sheet</h1><button className="composer-save active" type="submit" aria-label="Save sheet"><AppIcon name="check" size="lg" /></button></header><label className="new-sheet-name"><span className="sr-only">Sheet name</span><input value={name} onChange={(event) => setName(event.target.value)} onFocus={(event) => event.currentTarget.select()} autoFocus /></label><section className="composer-card"><div className="composer-row currency-row"><span>◉ <b>SGD</b></span><span>Singapore Dollar <AppIcon name="forward" size="sm" /></span></div></section><section className="composer-card new-sheet-options"><button type="button" className="composer-row toggle-row" onClick={() => setShowTotalBalance((value) => !value)} aria-pressed={showTotalBalance}><span><b>Total Balance</b></span><span className={showTotalBalance ? "composer-switch on" : "composer-switch"}><i /></span></button><label className="composer-row"><span><b>Total Period</b></span><select value={totalPeriod} onChange={(event) => setTotalPeriod(event.target.value as SheetTotalPeriod)}><option value="today">As of Today</option><option value="all">All Time</option></select></label></section><h2 className="composer-section-title">Input</h2><section className="composer-card new-sheet-options"><PreferenceToggle label="Show Currency Selection" checked={input.showCurrencySelection} onClick={() => toggle("showCurrencySelection")} /><PreferenceToggle label="Show Merchant" checked={input.showMerchant} onClick={() => toggle("showMerchant")} /><PreferenceToggle label="Show Time" checked={input.showTime} onClick={() => toggle("showTime")} /><PreferenceToggle label="Show category suggestions" checked={input.showCategorySuggestions} onClick={() => toggle("showCategorySuggestions")} /></section></form></div>;
}

function PreferenceToggle({ label, checked, onClick }: { label: string; checked: boolean; onClick: () => void }) { return <button type="button" className="composer-row toggle-row" onClick={onClick} aria-pressed={checked}><span><b>{label}</b></span><span className={checked ? "composer-switch on" : "composer-switch"}><i /></span></button>; }

function ActionHeader({ title, onClose, end }: { title: string; onClose: () => void; end?: React.ReactNode }) {
  return <header className="overlay-header action-header"><button type="button" className="round-control" onClick={onClose} aria-label={`Close ${title}`}><AppIcon name="close" size="lg" /></button><h1>{title}</h1>{end || <span />}</header>;
}

function SheetActionView({ action, sheet, sheets, items, sensitive, onClose, onOpenAction, onUpdateSheet, onDeleteSheet, onImport }: { action: Exclude<SheetAction, "select">; sheet: Sheet; sheets: Sheet[]; items: Transaction[]; sensitive: boolean; onClose: () => void; onOpenAction: (action: SheetAction) => void; onUpdateSheet: (sheetId: string, draft: SheetDraft) => boolean; onDeleteSheet: () => void; onImport: (sheetId: string, rows: ImportRow[]) => void }) {
  if (action === "stats") return <SheetStats sheet={sheet} sheets={sheets} items={items} sensitive={sensitive} onClose={onClose} onPrint={() => onOpenAction("print")} />;
  if (action === "trends") return <SheetTrends sheet={sheet} items={items} onClose={onClose} />;
  if (action === "exchange") return <ExchangeRateSheet sheet={sheet} onClose={onClose} />;
  if (action === "print") return <PrintOptions sheet={sheet} items={items.filter((item) => item.sheetId === sheet.id)} onClose={onClose} />;
  if (action === "export") return <ExportSheet sheet={sheet} sheets={sheets} items={items} onClose={onClose} />;
  if (action === "import") return <ImportSheet sheet={sheet} sheets={sheets} onClose={onClose} onImport={onImport} />;
  return <EditSheet sheet={sheet} onClose={onClose} onSave={onUpdateSheet} onDelete={onDeleteSheet} />;
}

function SheetStats({ sheet, sheets, items, sensitive, onClose, onPrint }: { sheet: Sheet; sheets: Sheet[]; items: Transaction[]; sensitive: boolean; onClose: () => void; onPrint: () => void }) {
  const [range, setRange] = useState<StatsRange>("today");
  const [kind, setKind] = useState<"expense" | "income">("expense");
  const [filterSheetId, setFilterSheetId] = useState(sheet.id);
  const [showFilters, setShowFilters] = useState(false);
  const [showRangeMenu, setShowRangeMenu] = useState(false);
  const scoped = useMemo(() => filterStatsRange(items.filter((item) => item.sheetId === filterSheetId), range), [filterSheetId, items, range]);
  const totals = useMemo(() => sheetTotals(scoped), [scoped]);
  const categories = useMemo(() => categoryTotals(scoped, kind), [kind, scoped]);
  const selectedSheet = sheets.find((candidate) => candidate.id === filterSheetId) || sheet;
  return <section className="sheet-modal-backdrop"><section className="action-sheet stats-screen" aria-label="Stats"><ActionHeader title="Stats" onClose={onClose} end={<div className="action-header-buttons"><button type="button" className="round-control" onClick={() => setShowFilters(true)} aria-label="Stats filters"><AppIcon name="sliders" size="md" /></button><button type="button" className="round-control" onClick={() => setShowRangeMenu((value) => !value)} aria-label="Stats options"><AppIcon name="more" size="md" /></button>{showRangeMenu && <div className="stats-range-menu"><small>Range</small>{(["today", "yearly", "monthly", "weekly", "daily"] as StatsRange[]).map((option) => <button type="button" key={option} onClick={() => { setRange(option); setShowRangeMenu(false); }}><span>{range === option ? "✓" : ""}</span>{statsRangeLabel(option)}</button>)}<button type="button" className="print-option" onClick={onPrint}><AppIcon name="printer" size="md" />Print</button></div>}</div>} /><p className="stats-subtitle">{statsRangeLabel(range)}</p><p className="stats-date-chip">{statsRangeDateLabel(range)}</p><SheetSummary sheet={{ ...selectedSheet, totalPeriod: "all" }} totals={totals} sensitive={sensitive} /><CategoryDonut values={categories} /><div className="composer-segment two-way"><button type="button" className={kind === "expense" ? "selected" : ""} onClick={() => setKind("expense")}>Expense</button><button type="button" className={kind === "income" ? "selected" : ""} onClick={() => setKind("income")}>Income</button></div><section className="stats-category-list"><button type="button" className="stats-category-row"><span>Show All</span><AppIcon name="forward" size="sm" /></button>{categories.length ? categories.map(([category, amount]) => <article className="stats-category-row" key={category}><span className={`sheet-category-icon ${kind}`}><AppIcon name={iconFor(category)} size="sm" /></span><div><b>{category}</b><small>{sensitive ? "••••" : `${kind === "expense" ? "−" : "+"}${money(amount, sheet.currency)}`}</small><i><em style={{ width: `${Math.max(4, Math.round(amount / (categories[0]?.[1] || 1) * 100))}%` }} /></i></div><span className="category-percent">{Math.round(amount / categories.reduce((sum, [, value]) => sum + value, 0) * 100)}%</span><AppIcon name="forward" size="sm" /></article>) : <p className="sheet-empty">No {kind} transactions in this range.</p>}</section>{showFilters && <StatsFilterSheet sheet={sheet} sheets={sheets} selectedSheetId={filterSheetId} onClose={() => setShowFilters(false)} onSelect={(id) => { setFilterSheetId(id); setShowFilters(false); }} />}</section></section>;
}

function StatsFilterSheet({ sheet, sheets, selectedSheetId, onClose, onSelect }: { sheet: Sheet; sheets: Sheet[]; selectedSheetId: string; onClose: () => void; onSelect: (sheetId: string) => void }) {
  return <div className="nested-sheet-backdrop"><section className="filter-sheet stats-filter-sheet"><ActionHeader title="Filters" onClose={onClose} end={<button type="button" className="filter-apply" onClick={onClose} aria-label="Apply stats filters"><AppIcon name="check" size="lg" /></button>} /><button type="button" className="filter-reset" onClick={() => onSelect(sheet.id)}>No filter <AppIcon name="check" size="sm" /></button><section className="filter-card"><label className="filter-row"><span><AppIcon name="table" size="sm" /><b>Sheets</b></span><select value={selectedSheetId} onChange={(event) => onSelect(event.target.value)}>{sheets.map((candidate) => <option value={candidate.id} key={candidate.id}>{candidate.name}</option>)}</select></label></section></section></div>;
}

function CategoryDonut({ values }: { values: Array<[string, number]> }) {
  const total = values.reduce((sum, [, amount]) => sum + amount, 0);
  let offset = 0;
  const colors = ["#6350f4", "#ff4149", "#98979e", "#168ee8", "#ffd01e"];
  return <svg className="category-donut" viewBox="0 0 180 180" role="img" aria-label="Category distribution">{total ? values.map(([, amount], index) => { const portion = amount / total; const dash = `${Math.max(0, portion * 100 - 1.5)} ${100 - Math.max(0, portion * 100 - 1.5)}`; const circle = <circle key={index} cx="90" cy="90" r="59" fill="none" stroke={colors[index % colors.length]} strokeWidth="28" strokeDasharray={dash} strokeDashoffset={-offset} pathLength="100" />; offset += portion * 100; return circle; }) : <circle cx="90" cy="90" r="59" fill="none" stroke="#e6e4e9" strokeWidth="28" />}</svg>;
}

function SheetTrends({ sheet, items, onClose }: { sheet: Sheet; items: Transaction[]; onClose: () => void }) {
  const [kind, setKind] = useState<"expense" | "income">("expense");
  const relevant = items.filter((item) => item.sheetId === sheet.id && item.kind === kind && !item.pending);
  return <section className="sheet-modal-backdrop"><section className="action-sheet trends-screen" aria-label="Trends"><ActionHeader title="Trends" onClose={onClose} end={<button type="button" className="round-control" aria-label="Trend filters"><AppIcon name="sliders" size="md" /></button>} /><div className="composer-segment two-way"><button type="button" className={kind === "expense" ? "selected" : ""} onClick={() => setKind("expense")}>Expense</button><button type="button" className={kind === "income" ? "selected" : ""} onClick={() => setKind("income")}>Income</button></div>{(["Daily", "Weekly", "Monthly", "Yearly"] as const).map((label) => <TrendChart key={label} title={label} items={relevant} />)}</section></section>;
}

function TrendChart({ title, items }: { title: "Daily" | "Weekly" | "Monthly" | "Yearly"; items: Transaction[] }) {
  const data = useMemo(() => trendSeries(items, title.toLowerCase() as "daily" | "weekly" | "monthly" | "yearly"), [items, title]);
  const max = Math.max(...data.map((entry) => entry.amount), 1);
  const points = data.map((entry, index) => `${index / Math.max(data.length - 1, 1) * 100},${88 - entry.amount / max * 68}`).join(" ");
  const area = `0,100 ${points} 100,100`;
  return <section className="trend-chart"><h2>{title} <small>{trendRangeDescription(title)}</small></h2><svg viewBox="0 0 100 100" preserveAspectRatio="none" role="img" aria-label={`${title} trend`}><defs><linearGradient id={`trend-${title}`} x1="0" x2="0" y1="0" y2="1"><stop stopColor="#6250f5" stopOpacity=".55" /><stop offset="1" stopColor="#6250f5" stopOpacity=".03" /></linearGradient></defs>{data.map((_, index) => <line x1={index / Math.max(data.length - 1, 1) * 100} x2={index / Math.max(data.length - 1, 1) * 100} y1="8" y2="88" stroke="#dedde2" strokeWidth=".25" key={index} />)}<polygon points={area} fill={`url(#trend-${title})`} /><polyline points={points} fill="none" stroke="#6350f4" strokeWidth="1.2" vectorEffect="non-scaling-stroke" />{data.map((entry, index) => <circle key={index} cx={index / Math.max(data.length - 1, 1) * 100} cy={88 - entry.amount / max * 68} r="1.4" fill="#fff" stroke="#6350f4" strokeWidth=".8" vectorEffect="non-scaling-stroke"><title>{`${entry.label}: ${money(entry.amount)}`}</title></circle>)}</svg><div>{data.map((entry, index) => <small key={`${entry.label}-${index}`}>{entry.label}</small>)}</div></section>;
}

function ExchangeRateSheet({ sheet, onClose }: { sheet: Sheet; onClose: () => void }) {
  const [amount, setAmount] = useState("1");
  const [rates, setRates] = useState(fallbackRates);
  const [sourceDate, setSourceDate] = useState(fallbackRateDate);
  const [usingFallback, setUsingFallback] = useState(false);
  useEffect(() => { let cancelled = false; void fetch("https://api.frankfurter.dev/v2/rates?base=SGD").then((response) => response.ok ? response.json() : Promise.reject(new Error("Rate request failed"))).then((data: Array<{ date: string; quote: string; rate: number }>) => { if (cancelled) return; const live = fallbackRates.map((entry) => { const row = data.find((candidate) => candidate.quote === entry.code); return row ? { ...entry, rate: 1 / row.rate } : entry; }); setRates(live); setSourceDate(data[0]?.date || fallbackRateDate); }).catch(() => { if (!cancelled) setUsingFallback(true); }); return () => { cancelled = true; }; }, []);
  const numericAmount = Number(amount) || 0;
  return <section className="sheet-modal-backdrop"><section className="action-sheet exchange-screen" aria-label="Exchange Rate"><ActionHeader title="Exchange Rate" onClose={onClose} /><h2 className="composer-section-title">Amount</h2><label className="exchange-amount"><input type="number" min="0" inputMode="decimal" value={amount} onChange={(event) => setAmount(event.target.value)} /></label><section className="exchange-rates">{rates.map((entry) => <article key={entry.code}><div><b>1 {entry.code}</b><small>{entry.name} - {entry.code}</small></div><div><b>{money(numericAmount * entry.rate, sheet.currency)}</b><small>{entry.rate.toFixed(6)}</small></div></article>)}</section><p className="rate-source">{usingFallback ? `Offline fallback · ${sourceDate}` : `Live reference rate · ${sourceDate}`}</p></section></section>;
}

function SheetSelectionView({ sheet, items, sensitive, selectedIds, showMenu, onClose, onToggle, onToggleMenu, onMove, onMerchant, onCategory, onDelete, sheets }: { sheet: Sheet; items: Transaction[]; sensitive: boolean; selectedIds: string[]; showMenu: boolean; onClose: () => void; onToggle: (id: string) => void; onToggleMenu: () => void; onMove: (sheetId: string) => void; onMerchant: (value: string) => void; onCategory: (value: string) => void; onDelete: () => void; sheets: Sheet[] }) {
  const [editor, setEditor] = useState<"move" | "merchant" | "category" | null>(null);
  const [value, setValue] = useState("");
  const selected = new Set(selectedIds);
  return <section className="sheets-screen selection-screen"><header className="sheet-ledger-heading"><span /><h1>{selected.size} Selected</h1><button type="button" className="composer-save active" onClick={onClose} aria-label="Finish selection"><AppIcon name="check" size="lg" /></button></header><SheetSummary sheet={sheet} totals={sheetTotals(items)} sensitive={sensitive} /><div className="ledger-period-row"><span className="ledger-period"><AppIcon name="receipt" size="sm" />As of Today</span><span className="ledger-transfer-count"><AppIcon name="transfer" size="sm" />{items.filter((item) => item.kind === "transfer").length}</span></div><SelectionTransactionList items={items} selected={selected} sensitive={sensitive} onToggle={onToggle} /><div className="selection-actions"><button type="button" className="search-filter" onClick={onToggleMenu} aria-expanded={showMenu} aria-label="Selected item actions"><AppIcon name="more" size="md" /></button><button type="button" className="selection-delete" onClick={onDelete}>Delete</button></div>{showMenu && <div className="batch-menu"><button type="button" onClick={() => setEditor("move")}><AppIcon name="table" size="md" />Move</button><button type="button" onClick={() => setEditor("merchant")}><AppIcon name="receipt" size="md" />Change Merchant</button><button type="button" onClick={() => setEditor("category")}><AppIcon name="ledger" size="md" />Change Category</button></div>}{editor && <div className="nested-sheet-backdrop"><section className="move-sheet"><ActionHeader title={editor === "move" ? "Move" : editor === "merchant" ? "Change Merchant" : "Change Category"} onClose={() => setEditor(null)} />{editor === "move" ? <div className="move-sheet-list">{sheets.map((candidate) => <button type="button" key={candidate.id} onClick={() => { onMove(candidate.id); setEditor(null); }} disabled={candidate.id === sheet.id}><span><AppIcon name="table" size="sm" />{candidate.name}</span><AppIcon name="forward" size="sm" /></button>)}</div> : <form className="batch-editor" onSubmit={(event) => { event.preventDefault(); if (editor === "merchant") onMerchant(value); else onCategory(value); setEditor(null); }}><input value={value} onChange={(event) => setValue(event.target.value)} placeholder={editor === "merchant" ? "Merchant" : "Category"} autoFocus required />{editor === "category" && <div className="category-chips">{expenseCategories.map((category) => <button type="button" key={category} onClick={() => setValue(category)}>{category}</button>)}</div>}<button type="submit" className="primary">Apply</button></form>}</section></div>}</section>;
}

function SelectionTransactionList({ items, selected, sensitive, onToggle }: { items: Transaction[]; selected: Set<string>; sensitive: boolean; onToggle: (id: string) => void }) {
  const grouped = groupTransactions(items);
  return <div className="sheet-transaction-list selection-list">{grouped.map(([date, group]) => <section key={date}><div className="transaction-date-heading"><h2>{transactionDateLabel(date)}</h2><b>{sensitive ? "••••" : signedMoney(dayNet(group))}</b></div>{group.map((item) => { const incoming = item.kind === "income" || item.transferDirection === "in"; const isSelected = selected.has(item.id); return <button type="button" className={isSelected ? "sheet-transaction selected" : "sheet-transaction"} key={item.id} onClick={() => onToggle(item.id)}><span className={isSelected ? "selection-check selected" : "selection-check"}>{isSelected && <AppIcon name="check" size="sm" />}</span><span className={`sheet-category-icon ${item.kind}`}><AppIcon name={iconFor(item.category)} size="sm" /></span><div><strong>{item.title}</strong><small>{item.notes || item.category}</small></div><b className={incoming ? "sheet-amount positive" : "sheet-amount"}>{sensitive ? "••••" : `${incoming ? "+" : "−"}${money(item.amount, item.currency)}`}</b></button>; })}</section>)}</div>;
}

function PrintOptions({ sheet, items, onClose }: { sheet: Sheet; items: Transaction[]; onClose: () => void }) {
  const [copies, setCopies] = useState(1);
  const [scaling, setScaling] = useState(100);
  const [orientation, setOrientation] = useState<"portrait" | "landscape">("portrait");
  const share = async () => { try { await navigator.share?.({ title: `${sheet.name} ledger`, text: `Print-ready ledger for ${sheet.name}.` }); } catch { /* cancelling share needs no message */ } };
  return <section className="sheet-modal-backdrop"><section className="action-sheet print-options" aria-label="Print options"><ActionHeader title="Options" onClose={onClose} end={<div className="action-header-buttons"><button type="button" className="round-control" onClick={() => { void share(); }} aria-label="Share print preview"><AppIcon name="upload" size="md" /></button><button type="button" className="round-control" onClick={() => window.print()} aria-label="Print"><AppIcon name="printer" size="md" /></button></div>} /><section className="composer-card"><label className="composer-row"><span><b>Printer</b></span><span>No Printer Selected <AppIcon name="forward" size="sm" /></span></label></section><section className="composer-card print-controls"><Stepper label="Copies" value={copies} onChange={setCopies} min={1} max={99} /><label className="composer-row"><span><b>Range</b></span><span>All {Math.max(1, Math.ceil(items.length / 20))} pages <AppIcon name="forward" size="sm" /></span></label><label className="composer-row"><span><b>Paper Size</b></span><span>A4 <AppIcon name="forward" size="sm" /></span></label><div className="composer-row"><span><b>Orientation</b></span><div className="orientation-buttons"><button type="button" className={orientation === "portrait" ? "selected" : ""} onClick={() => setOrientation("portrait")}>Portrait</button><button type="button" className={orientation === "landscape" ? "selected" : ""} onClick={() => setOrientation("landscape")}>Landscape</button></div></div><Stepper label="Scaling" value={scaling} onChange={setScaling} min={50} max={150} suffix="%" /></section><section className="print-previews"><article>✓ Page 1 of {Math.max(1, Math.ceil(items.length / 20))}</article><article>✓ Page 2</article></section><PrintLedgerDocument sheet={sheet} items={items} copies={copies} scaling={scaling} orientation={orientation} /></section></section>;
}

function Stepper({ label, value, onChange, min, max, suffix = "" }: { label: string; value: number; onChange: (value: number) => void; min: number; max: number; suffix?: string }) { return <div className="composer-row"><span><b>{label}</b></span><div className="stepper"><button type="button" onClick={() => onChange(Math.max(min, value - 1))}>−</button><output>{value}{suffix}</output><button type="button" onClick={() => onChange(Math.min(max, value + 1))}>+</button></div></div>; }

function PrintLedgerDocument({ sheet, items, copies, scaling, orientation }: { sheet: Sheet; items: Transaction[]; copies: number; scaling: number; orientation: "portrait" | "landscape" }) { return <section className={`print-document ${orientation}`} style={{ fontSize: `${scaling}%` }}><h1>{sheet.name}</h1><p>{sheet.currency} · {copies} cop{copies === 1 ? "y" : "ies"}</p>{items.map((item) => <p key={item.id}>{item.date} · {item.category} · {item.title} <b>{item.kind === "income" || item.transferDirection === "in" ? "+" : "−"}{money(item.amount, item.currency)}</b></p>)}</section>; }

function ExportSheet({ sheet, sheets, items, onClose }: { sheet: Sheet; sheets: Sheet[]; items: Transaction[]; onClose: () => void }) {
  const [sheetId, setSheetId] = useState(sheet.id);
  const [includeImages, setIncludeImages] = useState(true);
  const [allTime, setAllTime] = useState(true);
  const [exporting, setExporting] = useState(false);
  async function exportData() { setExporting(true); try { const target = sheets.find((candidate) => candidate.id === sheetId) || sheet; const source = items.filter((item) => item.sheetId === target.id && (allTime || item.date <= todayIso())); await exportTransactionsCsv(target, source, includeImages); } finally { setExporting(false); } }
  return <section className="sheet-modal-backdrop"><section className="action-sheet export-sheet" aria-label="Export"><ActionHeader title="Export" onClose={onClose} /><section className="composer-card"><label className="composer-row"><span><b>Sheets</b></span><select value={sheetId} onChange={(event) => setSheetId(event.target.value)}>{sheets.map((candidate) => <option value={candidate.id} key={candidate.id}>{candidate.name}</option>)}</select></label></section><section className="composer-card"><PreferenceToggle label="Export all images" checked={includeImages} onClick={() => setIncludeImages((value) => !value)} /></section><section className="composer-card"><PreferenceToggle label="All Time" checked={allTime} onClick={() => setAllTime((value) => !value)} /></section><button type="button" className="export-button" disabled={exporting} onClick={() => { void exportData(); }}>{exporting ? "Preparing export…" : "Export"}</button></section></section>;
}

function ImportSheet({ sheet, sheets, onClose, onImport }: { sheet: Sheet; sheets: Sheet[]; onClose: () => void; onImport: (sheetId: string, rows: ImportRow[]) => void }) {
  const [sheetId, setSheetId] = useState(sheet.id);
  const [rows, setRows] = useState<ImportRow[]>([]);
  const [error, setError] = useState("");
  const expenses = rows.filter((row) => row.kind === "expense").length;
  const income = rows.filter((row) => row.kind === "income").length;
  return <section className="sheet-modal-backdrop"><section className="action-sheet import-sheet" aria-label="Import"><ActionHeader title="Import" onClose={onClose} /><section className="composer-card"><label className="composer-row"><span><b>Sheet</b></span><select value={sheetId} onChange={(event) => setSheetId(event.target.value)}>{sheets.map((candidate) => <option value={candidate.id} key={candidate.id}>{candidate.name}</option>)}</select></label></section><label className="file-import-button">Select CSV file<input type="file" accept=".csv,text/csv" onChange={(event) => { const file = event.target.files?.[0]; if (!file) return; void file.text().then((text) => { const result = parseImportCsv(text); setRows(result.rows); setError(result.error); }); }} /></label><h2 className="composer-section-title">Help</h2><button type="button" className="composer-card composer-row" onClick={() => downloadBlob(new Blob([sampleCsv], { type: "text/csv;charset=utf-8" }), "expenses-csv-template.csv")}><span><AppIcon name="info" size="sm" /><b>CSV Data</b></span><AppIcon name="upRight" size="sm" /></button>{error && <p className="import-error">{error}</p>}<section className="composer-card import-counts"><div className="composer-row"><span><b>Expense</b></span><span>{expenses}</span></div><div className="composer-row"><span><b>Income</b></span><span>{income}</span></div></section><button type="button" className="export-button" disabled={!rows.length} onClick={() => { onImport(sheetId, rows); onClose(); }}>Import</button></section></section>;
}

function EditSheet({ sheet, onClose, onSave, onDelete }: { sheet: Sheet; onClose: () => void; onSave: (sheetId: string, draft: SheetDraft) => boolean; onDelete: () => void }) {
  const [name, setName] = useState(sheet.name);
  const [showTotalBalance, setShowTotalBalance] = useState(sheet.showTotalBalance);
  const [totalPeriod, setTotalPeriod] = useState<SheetTotalPeriod>(sheet.totalPeriod);
  const [input, setInput] = useState(sheet.input);
  function toggle(key: keyof Sheet["input"]) { setInput((current) => ({ ...current, [key]: !current[key] })); }
  function submit(event: FormEvent<HTMLFormElement>) { event.preventDefault(); if (onSave(sheet.id, { name, currency: sheet.currency, showTotalBalance, totalPeriod, input })) onClose(); }
  return <section className="sheet-composer-backdrop"><form className="sheet-composer edit-sheet" onSubmit={submit} aria-label="Edit Sheet"><header className="composer-header"><button type="button" className="round-control" onClick={onClose} aria-label="Close sheet details"><AppIcon name="close" size="lg" /></button><h1>Details</h1><button type="submit" className="composer-save active" aria-label="Save sheet details"><AppIcon name="check" size="lg" /></button></header><label className="new-sheet-name"><span className="sr-only">Sheet name</span><input value={name} onChange={(event) => setName(event.target.value)} /></label><section className="composer-card"><div className="composer-row currency-row"><span>◉ <b>{sheet.currency}</b></span><span>Singapore Dollar <AppIcon name="forward" size="sm" /></span></div></section><section className="composer-card new-sheet-options"><button type="button" className="composer-row toggle-row" onClick={() => setShowTotalBalance((value) => !value)}><span><b>Total Balance</b></span><span className={showTotalBalance ? "composer-switch on" : "composer-switch"}><i /></span></button><label className="composer-row"><span><b>Total Period</b></span><select value={totalPeriod} onChange={(event) => setTotalPeriod(event.target.value as SheetTotalPeriod)}><option value="today">As of Today</option><option value="all">All Time</option></select></label></section><h2 className="composer-section-title">Input</h2><section className="composer-card new-sheet-options"><PreferenceToggle label="Show Currency Selection" checked={input.showCurrencySelection} onClick={() => toggle("showCurrencySelection")} /><PreferenceToggle label="Show Merchant" checked={input.showMerchant} onClick={() => toggle("showMerchant")} /><PreferenceToggle label="Show Time" checked={input.showTime} onClick={() => toggle("showTime")} /><PreferenceToggle label="Show category suggestions" checked={input.showCategorySuggestions} onClick={() => toggle("showCategorySuggestions")} /></section><button type="button" className="trash-sheet-button" onClick={onDelete}>Move to Trash</button><p className="sheet-metadata">Created: {formatSheetTimestamp(sheet.createdAt)}<br />Edited: {formatSheetTimestamp(sheet.updatedAt || sheet.createdAt)}</p></form></section>;
}

function TransactionActionSheet({ transaction, onClose, onNewExpense, onEdit, onMove, onDuplicate, onCopy, onDelete }: { transaction: Transaction; onClose: () => void; onNewExpense: () => void; onEdit: (field: "merchant" | "category" | undefined) => void; onMove: () => void; onDuplicate: (today: boolean) => void; onCopy: () => void; onDelete: () => void }) {
  const [duplicateOpen, setDuplicateOpen] = useState(false);
  const incoming = transaction.kind === "income" || transaction.transferDirection === "in";
  return <div className="action-backdrop" onClick={onClose}><section className="transaction-actions" aria-label="Transaction actions" onClick={(event) => event.stopPropagation()}><article className="action-transaction"><span className={`sheet-category-icon ${transaction.kind}`}><AppIcon name={iconFor(transaction.category)} size="sm" /></span><div><strong>{transaction.category}</strong><small>{transaction.merchant || transaction.notes || "No merchant"}</small></div><b>{incoming ? "+" : "−"}{money(transaction.amount, transaction.currency)}</b></article><div className="action-menu"><button type="button" onClick={onNewExpense}><AppIcon name="plus" size="md" />New expense with “{transaction.category}”</button><button type="button" onClick={() => onEdit("merchant")}><AppIcon name="receipt" size="md" />Change Merchant</button><button type="button" onClick={() => onEdit("category")}><AppIcon name="ledger" size="md" />Change Category</button><button type="button" onClick={onMove}><AppIcon name="table" size="md" />Move</button><button type="button" onClick={() => setDuplicateOpen((value) => !value)}><AppIcon name="documentAdd" size="md" />Duplicate <AppIcon name={duplicateOpen ? "chevronDown" : "forward"} size="sm" /></button>{duplicateOpen && <div className="duplicate-actions"><button type="button" onClick={() => onDuplicate(false)}>Duplicate</button><button type="button" onClick={() => onDuplicate(true)}>Duplicate to Today</button></div>}<button type="button" onClick={onCopy}><AppIcon name="receipt" size="md" />Copy <small>{incoming ? "+" : "−"}{money(transaction.amount, transaction.currency)}</small></button><button type="button" className="destructive" onClick={onDelete}><AppIcon name="trash" size="md" />Delete</button></div></section></div>;
}

function MoveTransactionSheet({ transaction, sheets, onClose, onMove }: { transaction: Transaction; sheets: Sheet[]; onClose: () => void; onMove: (sheetId: string) => void }) {
  return <div className="sheet-modal-backdrop"><section className="move-sheet" aria-label="Move transaction"><header className="overlay-header"><button type="button" className="round-control" onClick={onClose} aria-label="Close move"><AppIcon name="close" size="lg" /></button><h1>Move</h1><span /></header><p>Choose the destination sheet.</p><div className="move-sheet-list">{sheets.map((sheet, index) => <button type="button" key={sheet.id} onClick={() => onMove(sheet.id)} disabled={sheet.id === transaction.sheetId}><span><AppIcon name="table" size="sm" />{sheetDisplayName(sheet, index, sheets)}</span><AppIcon name="forward" size="sm" /></button>)}</div></section></div>;
}

function ConfirmationDialog({ title, description, label, onClose, onConfirm }: Confirmation & { onClose: () => void; onConfirm: () => void }) { return <div className="action-backdrop" onClick={onClose}><section className="confirmation-dialog" role="dialog" aria-modal="true" aria-labelledby="confirmation-title" onClick={(event) => event.stopPropagation()}><h2 id="confirmation-title">{title}</h2><p>{description}</p><div><button type="button" onClick={onClose}>Cancel</button><button type="button" className="destructive" onClick={onConfirm}>{label}</button></div></section></div>; }

function HomeView({ totals, budgets, goals, balance, sensitive, onAdd, onView }: { totals: { income: number; expense: number; goals: number }; budgets: Budget[]; goals: Goal[]; balance: number; sensitive: boolean; onAdd: () => void; onView: (view: View) => void }) {
  const reveal = (value: string) => sensitive ? "••••••" : value;
  return <section className="screen">
    <div className="greeting"><div><h2>Good morning, Nadia</h2></div><button className="icon-button" onClick={onAdd} aria-label="Add transaction"><AppIcon name="plus" size="lg" /></button></div>
    <article className="balance-card"><div className="balance-heading"><span>August balance</span><span className="pill">SGD</span></div><strong>{reveal(money(totals.income - totals.expense - totals.goals))}</strong><div className="balance-columns"><span>Income <b>{reveal(money(totals.income))}</b></span><span>Spent <b>{reveal(money(totals.expense))}</b></span></div></article>
    <section className="two-up"><article className="mini-card"><span className="icon-dot yellow"><AppIcon name="upRight" size="sm" /></span><p>Settle up</p><strong>{balance >= 0 ? "Leo owes you" : "You owe Leo"}</strong><b>{reveal(money(Math.abs(balance)))}</b><button onClick={() => onView("ledger")}>See balance <AppIcon name="forward" size="sm" /></button></article><article className="mini-card soft"><span className="icon-dot purple"><AppIcon name="goals" size="sm" /></span><p>Next goal</p><strong>{goals[0].title}</strong><b>{Math.round(goals[0].saved / goals[0].target * 100)}% funded</b><button onClick={() => onView("plans")}>View goals <AppIcon name="forward" size="sm" /></button></article></section>
    <section className="section-heading"><h3>Budgets</h3><button onClick={() => onView("plans")}>See all</button></section>
    <div className="budget-list">{budgets.slice(0, 2).map((budget) => <BudgetRow key={budget.id} budget={budget} sensitive={sensitive} />)}</div>
    <section className="section-heading"><h3>Goals</h3><button onClick={() => onView("plans")}>Manage</button></section>
    <div className="goal-strip">{goals.map((goal) => <article key={goal.id} className="goal-card"><span><AppIcon name={goal.shared ? "goals" : "plans"} size="xs" /> {goal.shared ? "shared" : "personal"}</span><h4>{goal.title}</h4><div className="progress"><i style={{ width: `${Math.min(goal.saved / goal.target * 100, 100)}%` }} /></div><b>{reveal(`${money(goal.saved)} of ${money(goal.target)}`)}</b><small>by {dateLabel(goal.deadline)}</small></article>)}</div>
  </section>;
}

function LedgerView({ items, sensitive, onAdd }: { items: Transaction[]; sensitive: boolean; onAdd: () => void }) {
  const [range, setRange] = useState<"month" | "all">("month");
  const [showFilters, setShowFilters] = useState(false);
  const [kind, setKind] = useState<"all" | TransactionKind>("all");
  const [category, setCategory] = useState("all");
  const reference = latestTransactionDate(items);
  const availableCategories = [...new Set(items.map((item) => item.category))].sort();
  const visibleItems = items.filter((item) => (range === "all" || isInMonth(item.date, reference)) && (kind === "all" || item.kind === kind) && (category === "all" || item.category === category));
  const hasActiveFilter = kind !== "all" || category !== "all";
  function clearFilters() { setKind("all"); setCategory("all"); }
  return <section className="screen"><div className="screen-title"><div><h2>Ledger</h2></div><button className="icon-button" onClick={onAdd} aria-label="Add transaction"><AppIcon name="plus" size="lg" /></button></div><div className="filter-row"><button className={range === "month" ? "selected" : ""} aria-pressed={range === "month"} onClick={() => setRange("month")}>This month</button><button className={range === "all" ? "selected" : ""} aria-pressed={range === "all"} onClick={() => setRange("all")}>All entries</button><button className={showFilters || hasActiveFilter ? "selected" : ""} aria-expanded={showFilters} onClick={() => setShowFilters((open) => !open)}>Filter{hasActiveFilter ? " · active" : ""}</button></div>{showFilters && <section className="filter-panel" aria-label="Ledger filters"><label>Type<select value={kind} onChange={(event) => setKind(event.target.value as "all" | TransactionKind)}><option value="all">All types</option><option value="expense">Expenses</option><option value="income">Income</option><option value="transfer">Transfers</option></select></label><label>Category<select value={category} onChange={(event) => setCategory(event.target.value)}><option value="all">All categories</option>{availableCategories.map((entry) => <option key={entry} value={entry}>{entry}</option>)}</select></label>{hasActiveFilter && <button type="button" onClick={clearFilters}>Clear filters</button>}</section>}<div className="transaction-list">{visibleItems.length ? visibleItems.map((item) => { const incoming = item.kind === "income" || item.transferDirection === "in"; return <article className="transaction" key={item.id}><span className={`category-icon ${item.kind}`}><AppIcon name={iconFor(item.category)} size="sm" /></span><div><strong>{item.title}</strong><small>{dateLabel(item.date)} · {item.category}{item.sheet ? ` · ${item.sheet}` : ""}{item.source === "bank" ? " · bank feed" : ""}{item.recurring ? ` · ${item.recurring}` : ""}</small></div><div className={incoming ? "amount positive" : "amount"}>{sensitive ? "••••" : `${incoming ? "+" : "−"}${money(item.amount, item.currency)}`}<small>{item.paidBy}</small></div></article>; }) : <p className="empty-state">No transactions match these filters.</p>}</div></section>;
}

function PlansView({ budgets, goals, sensitive, onAddGoal, onAddBudget }: { budgets: Budget[]; goals: Goal[]; sensitive: boolean; onAddGoal: (goal: Goal) => void; onAddBudget: (budget: Budget) => void }) {
  const [adding, setAdding] = useState<"goal" | "budget" | null>(null);
  return <section className="screen"><div className="screen-title"><div><h2>Plans</h2></div><button className="icon-button" onClick={() => setAdding("goal")} aria-label="Add savings goal"><AppIcon name="plus" size="lg" /></button></div><section className="section-heading"><h3>Budgets</h3><button onClick={() => setAdding("budget")}>New budget</button></section><div className="budget-list">{budgets.map((budget) => <BudgetRow key={budget.id} budget={budget} sensitive={sensitive} />)}</div><section className="section-heading"><h3>Savings goals</h3><button onClick={() => setAdding("goal")}>New goal</button></section><div className="stack">{goals.map((goal) => <article key={goal.id} className="goal-wide"><div><span className="icon-dot purple"><AppIcon name="goals" size="sm" /></span><div><strong>{goal.title}</strong><small>{goal.shared ? "Shared with Leo" : "Personal"} · due {dateLabel(goal.deadline)}</small></div></div><b>{sensitive ? "••••" : money(goal.saved)} <small>/ {sensitive ? "••••" : money(goal.target)}</small></b><div className="progress"><i style={{ width: `${Math.min(goal.saved / goal.target * 100, 100)}%` }} /></div></article>)}</div>{adding && <PlanForm type={adding} onCancel={() => setAdding(null)} onGoal={(goal) => { onAddGoal(goal); setAdding(null); }} onBudget={(budget) => { onAddBudget(budget); setAdding(null); }} />}</section>;
}

function InsightsView({ items, sensitive }: { items: Transaction[]; sensitive: boolean }) {
  const [range, setRange] = useState<"month" | "previousMonth" | "all">("month");
  const [showRangeMenu, setShowRangeMenu] = useState(false);
  const [trendRange, setTrendRange] = useState<"daily" | "weekly" | "monthly">("weekly");
  const [showTrendMenu, setShowTrendMenu] = useState(false);
  const [showDetails, setShowDetails] = useState(false);
  const reference = latestTransactionDate(items);
  const selectedItems = filterInsightRange(items, range, reference);
  const expenses = selectedItems.filter((item) => item.kind === "expense");
  const totalSpent = expenses.reduce((sum, item) => sum + item.amount, 0);
  const grouped = Object.entries(expenses.reduce<Record<string, number>>((acc, item) => ({ ...acc, [item.category]: (acc[item.category] || 0) + item.amount }), {})).sort((a, b) => b[1] - a[1]);
  const trend = trendValues(expenses, trendRange, range === "previousMonth" ? previousMonth(reference) : reference);
  const displayGroups = showDetails ? grouped : grouped.slice(0, 3);
  const rangeLabels = { month: "This month", previousMonth: "Last month", all: "All time" } as const;
  const trendLabels = { daily: "Daily", weekly: "Weekly", monthly: "Monthly" } as const;
  const heading = range === "all" ? "All time" : monthLabel(range === "previousMonth" ? previousMonth(reference) : reference);
  return <section className="screen"><div className="screen-title"><div><p className="eyebrow">{heading}</p><h2>Insights</h2></div><div className="control"><button className="period" aria-expanded={showRangeMenu} onClick={() => setShowRangeMenu((open) => !open)}>{rangeLabels[range]} <AppIcon name="chevronDown" size="xs" /></button>{showRangeMenu && <div className="control-menu" role="menu">{(Object.keys(rangeLabels) as Array<keyof typeof rangeLabels>).map((option) => <button key={option} className={range === option ? "selected" : ""} onClick={() => { setRange(option); setShowRangeMenu(false); }}>{rangeLabels[option]}</button>)}</div>}</div></div><article className="insight-card"><div><span>Total spent</span><strong>{sensitive ? "••••••" : money(totalSpent)}</strong><small>{expenses.length ? <><AppIcon name="upRight" size="xs" /> {expenses.length} expense{expenses.length === 1 ? "" : "s"}</> : "No spending in this period"}</small></div><div className="donut"><b>{sensitive ? "••" : `${Math.round(totalSpent ? (grouped[0]?.[1] || 0) / totalSpent * 100 : 0)}%`}</b><small>{grouped[0]?.[0] || "planned"}</small></div></article><section className="section-heading"><h3>Spending trend</h3><div className="control"><button aria-expanded={showTrendMenu} onClick={() => setShowTrendMenu((open) => !open)}>{trendLabels[trendRange]} <AppIcon name="chevronDown" size="xs" /></button>{showTrendMenu && <div className="control-menu" role="menu">{(Object.keys(trendLabels) as Array<keyof typeof trendLabels>).map((option) => <button key={option} className={trendRange === option ? "selected" : ""} onClick={() => { setTrendRange(option); setShowTrendMenu(false); }}>{trendLabels[option]}</button>)}</div>}</div></section><div className="chart" aria-label={`${trendLabels[trendRange]} spending chart`}>{trend.map((value, index) => <i key={index} style={{ height: `${value.height}%` }} title={`${value.label}: ${money(value.amount)}`} />)}</div><section className="section-heading"><h3>By category</h3><button onClick={() => setShowDetails((visible) => !visible)}>{showDetails ? "Hide details" : "Details"}</button></section><div className="category-totals">{displayGroups.length ? displayGroups.map(([category, amount]) => <div key={category}><span><AppIcon name={iconFor(category)} size="sm" /> {category}{showDetails && <small>{Math.round(amount / totalSpent * 100)}% of spending</small>}</span><b>{sensitive ? "••••" : money(amount)}</b></div>) : <p className="empty-state">No spending recorded for this period.</p>}</div></section>;
}

function SettingsView({ online, dark, sensitive, banks, onDark, onSensitive, onBank, onImport, onExport, onCustomizeNav }: { online: boolean; dark: boolean; sensitive: boolean; banks: BankConnection[]; onDark: () => void; onSensitive: () => void; onBank: () => void; onImport: () => void; onExport: () => void; onCustomizeNav: () => void }) {
  return <section className="screen"><div className="screen-title"><div><h2>Settings</h2></div></div><section className="settings-group"><h3>Connected accounts</h3>{banks.map((bank) => <div className="setting-row" key={bank.id}><span className="bank-logo"><AppIcon name="bank" size="sm" /></span><div><strong>{bank.label}</strong><small>{bank.status === "connected" ? `${bank.institution} ${bank.accountMask || ""} · ${bank.lastSynced}` : "No bank provider connected"}</small></div><button onClick={bank.status === "connected" ? undefined : onBank}>{bank.status === "connected" ? "Manage" : "Connect"}</button></div>)}<button className="wide-action" onClick={onBank}><AppIcon name="plus" size="sm" /> Connect a bank or card</button></section><section className="settings-group"><h3>Data</h3><div className="setting-row"><span className="bank-logo"><AppIcon name="cloud" size="sm" /></span><div><strong>Sync status</strong><small>{online ? "Online — changes are synced when connected" : "Offline — changes will queue on this device"}</small></div></div><button className="setting-row" onClick={onImport}><span className="bank-logo"><AppIcon name="download" size="sm" /></span><div><strong>Import CSV</strong><small>Review a bank or Expenses export before adding it</small></div><AppIcon name="forward" size="sm" /></button><button className="setting-row" onClick={onExport}><span className="bank-logo"><AppIcon name="upload" size="sm" /></span><div><strong>Export your data</strong><small>Download a standard CSV backup</small></div><AppIcon name="forward" size="sm" /></button><button className="setting-row"><span className="bank-logo"><AppIcon name="table" size="sm" /></span><div><strong>Google Sheets backup</strong><small>Connect during deployment to export one-way</small></div><AppIcon name="forward" size="sm" /></button></section><section className="settings-group"><h3>Privacy & appearance</h3><Toggle label="Sensitive mode" detail="Mask money values until you turn it off" checked={sensitive} onChange={onSensitive} /><Toggle label="Dark mode" detail="Use a darker, comfortable colour scheme" checked={dark} onChange={onDark} /><button className="setting-row" onClick={onCustomizeNav}><span className="bank-logo"><AppIcon name="ledger" size="sm" /></span><div><strong>Customize navigation</strong><small>Choose tabs, their order, names, and icons</small></div><AppIcon name="forward" size="sm" /></button><div className="setting-row"><span className="bank-logo"><AppIcon name="receipt" size="sm" /></span><div><strong>Receipt AI</strong><small>Optional OpenAI suggestions; attachment-only always works</small></div><button>Configure</button></div></section></section>;
}

function BudgetRow({ budget, sensitive }: { budget: Budget; sensitive: boolean }) {
  const ratio = Math.min(budget.spent / budget.limit, 1);
  return <article className="budget-row"><div className="budget-copy"><span className="icon-dot yellow"><AppIcon name={iconFor(budget.category || "Other")} size="sm" /></span><div><strong>{budget.title}</strong><small>{budget.shared ? "Shared" : "Personal"} · {budget.period}</small></div><b>{sensitive ? "••••" : `${money(budget.spent)} / ${money(budget.limit)}`}</b></div><div className="progress"><i className={ratio > .8 ? "warning" : ""} style={{ width: `${ratio * 100}%` }} /></div></article>;
}

function TransactionModal({ onClose, onSubmit }: { onClose: () => void; onSubmit: (event: FormEvent<HTMLFormElement>) => void }) {
  const today = new Date().toISOString().slice(0, 10);
  const [kind, setKind] = useState<TransactionKind>("expense");
  const [receiptMessage, setReceiptMessage] = useState<string | null>(null);
  async function scanReceipt(file?: File) {
    if (!file || !file.type.startsWith("image/")) { setReceiptMessage("Attached. Choose an image to request receipt suggestions."); return; }
    if (file.size > 4_500_000) { setReceiptMessage("The image is too large for receipt analysis; it can still be attached."); return; }
    setReceiptMessage("Reading receipt…");
    const imageDataUrl = await new Promise<string>((resolve, reject) => { const reader = new FileReader(); reader.onload = () => resolve(String(reader.result)); reader.onerror = reject; reader.readAsDataURL(file); });
    const response = await fetch("/api/receipt", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ imageDataUrl }) }).catch(() => null);
    const body = response ? await response.json().catch(() => ({})) : {};
    if (body.suggestion) {
      const s = body.suggestion; const fields = [s.merchant, s.amount && `${s.currency || "SGD"} ${s.amount}`, s.date, s.category].filter(Boolean);
      setReceiptMessage(fields.length ? `Suggestion — ${fields.join(" · ")}. Review and enter it before saving.` : (body.message || "No reliable details found. The image remains an attachment."));
    } else setReceiptMessage(body.error || "Receipt analysis is unavailable; save the attachment normally.");
  }
  return <div className="modal-backdrop" role="presentation"><form className="modal" onSubmit={onSubmit}><div className="modal-head"><button type="button" onClick={onClose}>Cancel</button><h2>New entry</h2><button className="save" type="submit">Save</button></div><div className="segment">{(["expense", "income", "transfer"] as TransactionKind[]).map((option) => <label key={option} className={kind === option ? "selected" : ""}><input type="radio" name="kind" value={option} checked={kind === option} onChange={() => setKind(option)} />{option[0].toUpperCase() + option.slice(1)}</label>)}</div><label className="amount-input"><span>SGD</span><input name="amount" type="number" inputMode="decimal" min="0.01" step="0.01" placeholder="0.00" required /></label>{kind === "transfer" ? <><label>Transfer from<input name="title" value="Shared expenses" readOnly /></label><label>Transfer to<select name="transferTo" defaultValue="Japan fund"><option>Japan fund</option><option>Nadia personal</option><option>Leo personal</option></select></label></> : <><label>What was it?<input name="title" placeholder="e.g. groceries" required /></label><div className="field-grid"><label>Category<select name="category" defaultValue="Groceries">{categories.map((category) => <option key={category}>{category}</option>)}</select></label><label>Date<input name="date" type="date" defaultValue={today} required /></label></div></>} {kind === "transfer" && <label>Date<input name="date" type="date" defaultValue={today} required /></label>}<div className="field-grid"><label>Paid by<select name="paidBy" defaultValue="Nadia">{members.map((member) => <option key={member}>{member}</option>)}</select></label>{kind !== "transfer" && <label>Split<select name="split" defaultValue="equal"><option value="equal">Equally</option><option value="amount">Exact amounts</option><option value="percent">Percentage</option><option value="shares">Shares</option></select></label>}</div>{kind !== "transfer" && <fieldset className="scope"><legend>Visibility</legend><label><input type="radio" name="scope" value="shared" defaultChecked /> Shared with Leo</label><label><input type="radio" name="scope" value="personal" /> Personal</label></fieldset>}<label>Repeats<select name="recurring" defaultValue=""><option value="">Does not repeat</option><option>Weekly</option><option>Monthly</option><option>Yearly</option></select></label><label>Notes<input name="notes" placeholder="Optional details" /></label><label className="check"><input name="pending" type="checkbox" /> This is pending and should not count yet</label><div className="attachment-row"><span><AppIcon name="attachment" size="md" /></span><div><strong>Receipt or attachment</strong><small>{receiptMessage || "Works without AI; review AI suggestions before saving."}</small></div><input type="file" accept="image/*,.pdf" aria-label="Attach receipt" onChange={(event) => { void scanReceipt(event.target.files?.[0]); }} /></div></form></div>;
}

function PlanForm({ type, onCancel, onGoal, onBudget }: { type: "goal" | "budget"; onCancel: () => void; onGoal: (goal: Goal) => void; onBudget: (budget: Budget) => void }) {
  function submit(event: FormEvent<HTMLFormElement>) { event.preventDefault(); const data = new FormData(event.currentTarget); const shared = data.get("scope") === "shared"; if (type === "goal") onGoal({ id: uid("goal"), title: String(data.get("title")), target: Number(data.get("target")), saved: Number(data.get("saved") || 0), deadline: String(data.get("deadline")), shared }); else onBudget({ id: uid("budget"), title: String(data.get("title")), limit: Number(data.get("target")), spent: 0, period: data.get("period") as Budget["period"], category: String(data.get("category")), shared }); }
  return <div className="modal-backdrop"><form className="modal compact" onSubmit={submit}><div className="modal-head"><button type="button" onClick={onCancel}>Cancel</button><h2>New {type}</h2><button className="save">Save</button></div><label>Name<input name="title" required placeholder={type === "goal" ? "e.g. Home deposit" : "e.g. Groceries"} /></label><label>{type === "goal" ? "Target" : "Limit"}<input name="target" required type="number" min="1" step="0.01" inputMode="decimal" /></label>{type === "goal" ? <><label>Already saved<input name="saved" type="number" min="0" step="0.01" inputMode="decimal" defaultValue="0" /></label><label>Target date<input name="deadline" type="date" required /></label></> : <><div className="field-grid"><label>Period<select name="period" defaultValue="month"><option value="week">Weekly</option><option value="month">Monthly</option><option value="year">Yearly</option><option value="custom">Custom</option></select></label><label>Category<select name="category">{categories.map((category) => <option key={category}>{category}</option>)}</select></label></div></>}<fieldset className="scope"><legend>Ownership</legend><label><input name="scope" value="shared" defaultChecked type="radio" /> Shared</label><label><input name="scope" value="personal" type="radio" /> Personal</label></fieldset></form></div>;
}

function BankModal({ onClose, onConnect }: { onClose: () => void; onConnect: () => void }) {
  return <div className="modal-backdrop"><section className="modal compact bank-modal"><div className="modal-head"><button onClick={onClose}>Cancel</button><h2>Connect account</h2><span /></div><div className="provider-mark"><AppIcon name="bank" size="xl" /></div><h3>Use your bank&apos;s secure connection</h3><p>We never ask for, see, or store your card number, PIN, or online-banking password. A supported Open Banking provider handles authentication and returns read-only transaction data.</p><div className="provider-note"><strong>Production availability</strong><span>Singapore coverage depends on the provider and your bank. Brankas credentials are required before a live connection can be enabled.</span></div><button className="primary" onClick={onConnect}>Try provider sandbox</button><button className="secondary" onClick={onClose}>Import a bank CSV instead</button></section></div>;
}

function ImportModal({ onClose, onFile }: { onClose: () => void; onFile: (event: ChangeEvent<HTMLInputElement>) => void }) {
  return <div className="modal-backdrop"><section className="modal compact"><div className="modal-head"><button onClick={onClose}>Cancel</button><h2>Import CSV</h2><span /></div><div className="upload-card"><span><AppIcon name="download" size="xl" /></span><h3>Bring your transactions</h3><p>Accepts Expenses or bank CSVs with <b>Date, Category, Price, Notes</b>. Imported rows remain local in this demo until Supabase is configured.</p><label className="primary file-button">Choose CSV<input type="file" accept=".csv,text/csv" onChange={onFile} /></label></div></section></div>;
}

function NavEditor({ items, showIcons, onClose, onChange }: { items: NavItem[]; showIcons: boolean; onClose: () => void; onChange: (items: NavItem[], showIcons: boolean) => void }) {
  function update(id: View, changes: Partial<NavItem>) { onChange(items.map((item) => item.id === id ? { ...item, ...changes } : item), showIcons); }
  function move(index: number, direction: -1 | 1) {
    const destination = index + direction;
    if (destination < 0 || destination >= items.length) return;
    const next = [...items]; [next[index], next[destination]] = [next[destination], next[index]]; onChange(next, showIcons);
  }
  return <div className="modal-backdrop"><section className="modal compact nav-editor"><div className="modal-head"><button onClick={onClose}>Done</button><h2>Customize navigation</h2><span /></div><p>Arrange tabs from left to right. At least one screen must remain visible.</p><Toggle label="Show icons" detail="Hide icons and use text-only tabs" checked={showIcons} onChange={() => onChange(items, !showIcons)} /><div className="nav-editor-list">{items.map((item, index) => <article key={item.id} className={item.visible ? "nav-editor-row" : "nav-editor-row muted-row"}><button className={item.visible ? "visibility on" : "visibility"} aria-label={`${item.visible ? "Hide" : "Show"} ${item.label}`} onClick={() => { if (item.visible && items.filter((entry) => entry.visible).length === 1) return; update(item.id, { visible: !item.visible }); }}><AppIcon name={item.visible ? "check" : "plus"} size="sm" /></button><select aria-label={`${item.label} icon`} className="icon-select" value={item.icon} onChange={(event) => update(item.id, { icon: event.target.value as IconKey })} disabled={!showIcons}>{(["home", "ledger", "plans", "insights", "settings", "bank", "goals", "transfer"] as IconKey[]).map((icon) => <option key={icon} value={icon}>{icon}</option>)}</select><input aria-label={`${item.label} label`} className="nav-label-input" value={item.label} maxLength={14} onChange={(event) => update(item.id, { label: event.target.value || defaultNavItems.find((entry) => entry.id === item.id)?.label || "Tab" })} /><div className="reorder"><button aria-label={`Move ${item.label} left`} disabled={index === 0} onClick={() => move(index, -1)}><AppIcon name="back" size="sm" /></button><button aria-label={`Move ${item.label} right`} disabled={index === items.length - 1} onClick={() => move(index, 1)}><AppIcon name="forward" size="sm" /></button></div></article>)}</div><button className="secondary" onClick={() => onChange(defaultNavItems, true)}>Reset navigation</button></section></div>;
}

function Toggle({ label, detail, checked, onChange }: { label: string; detail: string; checked: boolean; onChange: () => void }) { return <button className="setting-row toggle" onClick={onChange}><div><strong>{label}</strong><small>{detail}</small></div><span className={checked ? "switch on" : "switch"}><i /></span></button>; }
function transactionDate(value: string) { return new Date(`${value.slice(0, 10)}T12:00:00`); }
function transactionSortKey(item: Transaction) { return `${item.date}T${item.time || "00:00"}`; }
function sheetBalance(items: Transaction[]) { return items.filter((item) => !item.pending).reduce((balance, item) => balance + (item.kind === "income" || item.transferDirection === "in" ? item.amount : -item.amount), 0); }
function sheetTotals(items: Transaction[]) { return items.filter((item) => !item.pending).reduce((totals, item) => { if (item.kind === "income") totals.income += item.amount; else if (item.kind === "expense") totals.expense += item.amount; return totals; }, { income: 0, expense: 0, balance: sheetBalance(items) }); }
function dayNet(items: Transaction[]) { return sheetBalance(items); }
function signedMoney(value: number) { return `${value >= 0 ? "+" : "−"}${money(Math.abs(value))}`; }
function isEmptyFilters(filters: LedgerFilters) { return filters.amount === "" && filters.category === "all" && filters.currency === "all" && filters.dateMatch === "all" && !filters.hasAttachment && filters.kind === "all" && filters.notes === "" && !filters.recurring; }
function filterTransactions(items: Transaction[], filters: LedgerFilters) {
  const amount = Number(filters.amount);
  const today = new Date().toISOString().slice(0, 10);
  return items.filter((item) => {
    if (filters.hasAttachment && !item.hasAttachment) return false;
    if (filters.recurring && !item.recurring) return false;
    if (filters.kind !== "all" && item.kind !== filters.kind) return false;
    if (filters.currency !== "all" && item.currency !== filters.currency) return false;
    if (filters.category !== "all" && item.category !== filters.category) return false;
    if (filters.notes && !`${item.notes || ""} ${item.title} ${item.merchant || ""}`.toLocaleLowerCase().includes(filters.notes.toLocaleLowerCase())) return false;
    if (filters.amount !== "" && Number.isFinite(amount)) { if (filters.amountMatch === "exactly" && item.amount !== amount) return false; if (filters.amountMatch === "atLeast" && item.amount < amount) return false; if (filters.amountMatch === "atMost" && item.amount > amount) return false; }
    if (filters.dateMatch === "today" && item.date !== today) return false;
    if (filters.dateMatch === "custom" && item.date !== filters.date) return false;
    return true;
  });
}
function latestActivity(item: Transaction) { return `${dateLabel(item.date)}${item.time ? ` · ${formatTime(item.time)}` : ""}`; }
function formatTime(value: string) { const [hours, minutes] = value.split(":").map(Number); const suffix = hours >= 12 ? "PM" : "AM"; return `${hours % 12 || 12}:${String(minutes).padStart(2, "0")} ${suffix}`; }
function searchTransactions(items: Transaction[], query: string) { const needle = query.trim().toLocaleLowerCase(); return items.filter((item) => [item.title, item.merchant, item.notes, item.category, item.sheet].filter(Boolean).some((value) => value?.toLocaleLowerCase().includes(needle))).sort((left, right) => transactionSortKey(right).localeCompare(transactionSortKey(left))); }
function groupTransactions(items: Transaction[]) { const sorted = [...items].sort((left, right) => transactionSortKey(right).localeCompare(transactionSortKey(left))); return Object.entries(sorted.reduce<Record<string, Transaction[]>>((groups, item) => { (groups[item.date] ||= []).push(item); return groups; }, {})); }
function transactionDateLabel(value: string) { return transactionDate(value).toLocaleDateString("en-SG", { day: "numeric", month: "short" }); }
function latestTransactionDate(items: Transaction[]) { const latest = items.reduce((value, item) => item.date > value ? item.date : value, ""); return latest ? transactionDate(latest) : new Date(); }
function previousMonth(date: Date) { return new Date(date.getFullYear(), date.getMonth() - 1, 1); }
function isInMonth(value: string, reference: Date) { const date = transactionDate(value); return date.getFullYear() === reference.getFullYear() && date.getMonth() === reference.getMonth(); }
function filterInsightRange(items: Transaction[], range: "month" | "previousMonth" | "all", reference: Date) { if (range === "all") return items; return items.filter((item) => isInMonth(item.date, range === "previousMonth" ? previousMonth(reference) : reference)); }
function monthLabel(date: Date) { return date.toLocaleDateString("en-SG", { month: "long", year: "numeric" }); }
function trendValues(items: Transaction[], range: "daily" | "weekly" | "monthly", reference: Date) {
  const bucketDates = range === "daily" ? Array.from({ length: 7 }, (_, index) => new Date(reference.getFullYear(), reference.getMonth(), reference.getDate() - 6 + index)) : range === "weekly" ? Array.from({ length: 4 }, (_, index) => new Date(reference.getFullYear(), reference.getMonth(), reference.getDate() - 21 + index * 7)) : Array.from({ length: 6 }, (_, index) => new Date(reference.getFullYear(), reference.getMonth() - 5 + index, 1));
  const values = bucketDates.map((start, index) => {
    const end = range === "daily" ? new Date(start.getFullYear(), start.getMonth(), start.getDate() + 1) : range === "weekly" ? new Date(start.getFullYear(), start.getMonth(), start.getDate() + 7) : new Date(start.getFullYear(), start.getMonth() + 1, 1);
    const amount = items.reduce((sum, item) => { const date = transactionDate(item.date); return date >= start && date < end ? sum + item.amount : sum; }, 0);
    const label = range === "daily" ? start.toLocaleDateString("en-SG", { weekday: "short" }) : range === "weekly" ? `Week ${index + 1}` : start.toLocaleDateString("en-SG", { month: "short" });
    return { amount, label };
  });
  const maximum = Math.max(...values.map((value) => value.amount), 1);
  return values.map((value) => ({ ...value, height: value.amount ? Math.max(8, value.amount / maximum * 100) : 4 }));
}
const fallbackRateDate = "2026-08-28";
const fallbackRates = [
  { code: "AED", name: "United Arab Emirates Dirham", rate: 0.347114 }, { code: "AFN", name: "Afghan Afghani", rate: 0.019548 }, { code: "ALL", name: "Albanian Lek", rate: 0.015998 }, { code: "AMD", name: "Armenian Dram", rate: 0.003499 }, { code: "ANG", name: "Netherlands Antillean Guilder", rate: 0.71226 }, { code: "AOA", name: "Angolan Kwanza", rate: 0.001373 }, { code: "ARS", name: "Argentine Peso", rate: 0.000843 }, { code: "AUD", name: "Australian Dollar", rate: 0.913307 }, { code: "AWG", name: "Aruban Florin", rate: 0.71226 }
];
const sampleCsv = "Date,Time,Type,Category,Amount,Currency,Merchant,Notes\n2026-08-30,12:00,expense,Food & Drink,8.00,SGD,luckin,coffee\n2026-08-30,12:00,income,Salary,2000.00,SGD,,August salary\n";

function statsRangeLabel(range: StatsRange) { return ({ today: "As of Today", yearly: "Yearly", monthly: "Monthly", weekly: "Weekly", daily: "Daily" })[range]; }
function statsRangeDateLabel(range: StatsRange) {
  const now = new Date();
  if (range === "today") return `- ${now.toLocaleDateString("en-GB")}`;
  if (range === "yearly") return now.getFullYear().toString();
  if (range === "monthly") return now.toLocaleDateString("en-SG", { month: "long", year: "numeric" });
  if (range === "weekly") { const start = new Date(now); start.setDate(now.getDate() - 6); return `${start.toLocaleDateString("en-SG", { day: "numeric", month: "short" })} – ${now.toLocaleDateString("en-SG", { day: "numeric", month: "short" })}`; }
  return now.toLocaleDateString("en-SG", { day: "numeric", month: "long", year: "numeric" });
}
function filterStatsRange(items: Transaction[], range: StatsRange) {
  const now = new Date();
  const start = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  if (range === "today") return items.filter((item) => item.date <= todayIso());
  if (range === "yearly") return items.filter((item) => transactionDate(item.date).getFullYear() === now.getFullYear());
  if (range === "monthly") return items.filter((item) => isInMonth(item.date, now));
  if (range === "weekly") { start.setDate(start.getDate() - 6); return items.filter((item) => transactionDate(item.date) >= start && transactionDate(item.date) <= now); }
  return items.filter((item) => item.date === todayIso());
}
function categoryTotals(items: Transaction[], kind: "expense" | "income") { return Object.entries(items.filter((item) => item.kind === kind && !item.pending).reduce<Record<string, number>>((totals, item) => ({ ...totals, [item.category]: (totals[item.category] || 0) + item.amount }), {})).sort(([, left], [, right]) => right - left); }
function trendRangeDescription(title: "Daily" | "Weekly" | "Monthly" | "Yearly") {
  const now = new Date();
  if (title === "Daily") { const start = new Date(now); start.setDate(now.getDate() - 14); return `${start.toLocaleDateString("en-SG", { day: "numeric", month: "short" })} – ${now.toLocaleDateString("en-SG", { day: "numeric", month: "short" })}`; }
  if (title === "Weekly") return "10 weeks ago – This Week";
  if (title === "Monthly") return `${new Date(now.getFullYear() - 1, now.getMonth()).toLocaleDateString("en-SG", { month: "long", year: "numeric" })} – ${now.toLocaleDateString("en-SG", { month: "long", year: "numeric" })}`;
  return `${now.getFullYear() - 10} – ${now.getFullYear()}`;
}
function trendSeries(items: Transaction[], range: "daily" | "weekly" | "monthly" | "yearly") {
  const now = new Date();
  const count = range === "daily" ? 15 : range === "weekly" ? 10 : range === "monthly" ? 13 : 11;
  const starts = Array.from({ length: count }, (_, index) => {
    if (range === "daily") return new Date(now.getFullYear(), now.getMonth(), now.getDate() - (count - 1 - index));
    if (range === "weekly") return new Date(now.getFullYear(), now.getMonth(), now.getDate() - (count - 1 - index) * 7);
    if (range === "monthly") return new Date(now.getFullYear(), now.getMonth() - (count - 1 - index), 1);
    return new Date(now.getFullYear() - (count - 1 - index), 0, 1);
  });
  return starts.map((start) => {
    const end = range === "daily" ? new Date(start.getFullYear(), start.getMonth(), start.getDate() + 1) : range === "weekly" ? new Date(start.getFullYear(), start.getMonth(), start.getDate() + 7) : range === "monthly" ? new Date(start.getFullYear(), start.getMonth() + 1, 1) : new Date(start.getFullYear() + 1, 0, 1);
    const amount = items.reduce((sum, item) => { const date = transactionDate(item.date); return date >= start && date < end ? sum + item.amount : sum; }, 0);
    const label = range === "daily" ? start.getDate().toString() : range === "weekly" ? `-${Math.round((now.getTime() - start.getTime()) / 604800000)}` : range === "monthly" ? start.toLocaleDateString("en-SG", { month: "short" }) : start.getFullYear().toString();
    return { amount, label };
  });
}
function formatSheetTimestamp(value?: string) { return value ? new Date(value).toLocaleString("en-SG", { day: "numeric", month: "long", year: "numeric", hour: "numeric", minute: "2-digit" }) : "Not recorded"; }
function csvContent(items: Transaction[]) {
  const header = ["Date", "Time", "Type", "Category", "Amount", "Currency", "Merchant", "Notes", "Pending", "Repeat", "Attachment Files"];
  return [header, ...items.map((item) => [item.date, item.time || "", item.kind, item.category, item.amount.toString(), item.currency, item.merchant || "", item.notes || "", item.pending ? "true" : "false", item.recurring || "", (item.attachments || []).map((attachment) => attachment.filename).join("|")])].map((row) => row.map(csv).join(",")).join("\n");
}
async function exportTransactionsCsv(sheet: Sheet, items: Transaction[], includeImages: boolean) {
  const content = csvContent(items);
  const stem = safeDownloadName(sheet.name);
  if (!includeImages) { downloadBlob(new Blob([content], { type: "text/csv;charset=utf-8" }), `${stem}.csv`); return; }
  const zip = new JSZip();
  zip.file(`${stem}.csv`, content);
  const known = new Set<string>();
  for (const attachment of items.flatMap((item) => item.attachments || [])) {
    if (known.has(attachment.storagePath)) continue;
    known.add(attachment.storagePath);
    const file = await readAttachment(attachment);
    if (file) zip.file(`attachments/${safeDownloadName(attachment.filename)}`, file);
  }
  const archive = await zip.generateAsync({ type: "blob" });
  downloadBlob(archive, `${stem}-export.zip`);
}
function downloadBlob(blob: Blob, filename: string) { const url = URL.createObjectURL(blob); const anchor = document.createElement("a"); anchor.href = url; anchor.download = filename; document.body.append(anchor); anchor.click(); anchor.remove(); window.setTimeout(() => URL.revokeObjectURL(url), 0); }
function safeDownloadName(value: string) { return value.trim().replace(/[^a-zA-Z0-9._-]+/g, "-") || "sheet"; }
function parseCsv(text: string) {
  const rows: string[][] = []; let row: string[] = []; let cell = ""; let quoted = false;
  for (let index = 0; index < text.length; index += 1) { const character = text[index]; if (character === '"') { if (quoted && text[index + 1] === '"') { cell += '"'; index += 1; } else quoted = !quoted; } else if (character === "," && !quoted) { row.push(cell); cell = ""; } else if ((character === "\n" || character === "\r") && !quoted) { if (character === "\r" && text[index + 1] === "\n") index += 1; row.push(cell); if (row.some((value) => value.trim())) rows.push(row); row = []; cell = ""; } else cell += character; }
  row.push(cell); if (row.some((value) => value.trim())) rows.push(row); return rows;
}
function parseImportCsv(text: string): { rows: ImportRow[]; error: string } {
  const [header, ...records] = parseCsv(text); if (!header) return { rows: [], error: "Choose a CSV file with a header row." };
  const columns = Object.fromEntries(header.map((value, index) => [value.trim().toLocaleLowerCase(), index]));
  const required = ["date", "type", "category", "amount"]; if (required.some((key) => columns[key] === undefined)) return { rows: [], error: "CSV needs Date, Type, Category, and Amount columns." };
  const rows = records.flatMap((record) => { const kind = record[columns.type]?.trim().toLocaleLowerCase(); const amount = Number(record[columns.amount]); const date = record[columns.date]?.trim(); if ((kind !== "expense" && kind !== "income") || !Number.isFinite(amount) || amount <= 0 || !/^\d{4}-\d{2}-\d{2}$/.test(date)) return []; return [{ kind, amount, date, category: record[columns.category]?.trim() || "Other", currency: record[columns.currency] ? record[columns.currency].trim().toUpperCase() : "SGD", merchant: columns.merchant === undefined ? "" : record[columns.merchant]?.trim() || "", notes: columns.notes === undefined ? "" : record[columns.notes]?.trim() || "", time: columns.time === undefined ? "" : record[columns.time]?.trim() || "" } satisfies ImportRow]; });
  return rows.length ? { rows, error: "" } : { rows: [], error: "No valid expense or income rows were found." };
}
function iconFor(category: string): IconKey { return ({ Groceries: "cart", Dining: "dining", Transport: "transport", Utilities: "utilities", Rent: "home", Health: "goals", Shopping: "cart", Entertainment: "insights", Salary: "salary", Goals: "goals", Transfer: "transfer" } as Record<string, IconKey>)[category] || "plans"; }
function csv(value: string) { return `"${value.replaceAll('"', '""')}"`; }
