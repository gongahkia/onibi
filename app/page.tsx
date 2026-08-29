"use client";

import { ChangeEvent, FormEvent, useEffect, useMemo, useState } from "react";
import { SFArrowDown, SFArrowLeft, SFArrowLeftArrowRight, SFArrowRight, SFArrowUp, SFArrowUpRight, SFBanknoteFill, SFCartFill, SFChartLineUptrendXyaxis, SFCheckmark, SFChevronDown, SFCloudFill, SFCreditcardFill, SFForkKnife, SFGearshapeFill, SFHeartFill, SFHouseFill, SFLightbulbFill, SFListBullet, SFPaperclip, SFPlus, SFReceipt, SFSquareAndArrowUp, SFTablecells, SFTarget, SFTramFill } from "sf-symbols-lib/dualtone";
import { demoBankConnections, demoBudgets, demoGoals, demoTransactions } from "@/lib/demo-data";
import { dateLabel, money, type BankConnection, type Budget, type Goal, type SplitMethod, type Transaction, type TransactionKind } from "@/lib/types";

type View = "home" | "ledger" | "plans" | "insights" | "settings";
type IconKey = "home" | "ledger" | "plans" | "insights" | "settings" | "plus" | "cart" | "dining" | "transport" | "utilities" | "salary" | "goals" | "transfer" | "bank" | "download" | "upload" | "check" | "back" | "forward" | "upRight" | "chevronDown" | "cloud" | "attachment" | "receipt" | "table";
type NavItem = { id: View; label: string; icon: IconKey; visible: boolean };
const iconComponents = { home: SFHouseFill, ledger: SFListBullet, plans: SFTarget, insights: SFChartLineUptrendXyaxis, settings: SFGearshapeFill, plus: SFPlus, cart: SFCartFill, dining: SFForkKnife, transport: SFTramFill, utilities: SFLightbulbFill, salary: SFBanknoteFill, goals: SFHeartFill, transfer: SFArrowLeftArrowRight, bank: SFCreditcardFill, download: SFArrowDown, upload: SFSquareAndArrowUp, check: SFCheckmark, back: SFArrowLeft, forward: SFArrowRight, upRight: SFArrowUpRight, chevronDown: SFChevronDown, cloud: SFCloudFill, attachment: SFPaperclip, receipt: SFReceipt, table: SFTablecells };
function AppIcon({ name, size = "md" }: { name: IconKey; size?: "sm" | "md" | "lg" }) { const Icon = iconComponents[name]; return <Icon size={size} aria-hidden="true" />; }
const members = ["Nadia", "Leo"];
const categories = ["Groceries", "Dining", "Transport", "Utilities", "Rent", "Health", "Shopping", "Entertainment", "Salary", "Goals", "Other"];
const defaultNavItems: NavItem[] = [
  { id: "home", label: "Home", icon: "home", visible: true },
  { id: "ledger", label: "Ledger", icon: "ledger", visible: true },
  { id: "plans", label: "Plans", icon: "plans", visible: true },
  { id: "insights", label: "Insights", icon: "insights", visible: true },
  { id: "settings", label: "Settings", icon: "settings", visible: true }
];

function uid(prefix: string) { return `${prefix}-${crypto.randomUUID?.() ?? Date.now().toString(36)}`; }
type DemoState = { transactions: Transaction[]; budgets: Budget[]; goals: Goal[]; banks: BankConnection[]; navItems?: NavItem[]; showNavIcons?: boolean };
function readDemoState(): DemoState {
  const fallback = { transactions: demoTransactions, budgets: demoBudgets, goals: demoGoals, banks: demoBankConnections, navItems: defaultNavItems, showNavIcons: true };
  if (typeof window === "undefined") return fallback;
  try {
    const raw = localStorage.getItem("together-budget-demo");
    if (!raw) return fallback;
    const parsed = JSON.parse(raw) as Partial<DemoState>;
    const navItems = parsed.navItems?.map((item) => ({ ...defaultNavItems.find((entry) => entry.id === item.id), ...item, icon: item.icon || defaultNavItems.find((entry) => entry.id === item.id)?.icon || "home" })) || fallback.navItems;
    return { transactions: parsed.transactions || fallback.transactions, budgets: parsed.budgets || fallback.budgets, goals: parsed.goals || fallback.goals, banks: parsed.banks || fallback.banks, navItems, showNavIcons: parsed.showNavIcons ?? true };
  } catch { return fallback; }
}

export default function BudgetApp() {
  const [initial] = useState(readDemoState);
  const [view, setView] = useState<View>("home");
  const [transactions, setTransactions] = useState<Transaction[]>(initial.transactions);
  const [budgets, setBudgets] = useState<Budget[]>(initial.budgets);
  const [goals, setGoals] = useState<Goal[]>(initial.goals);
  const [banks, setBanks] = useState<BankConnection[]>(initial.banks);
  const [navItems, setNavItems] = useState<NavItem[]>(initial.navItems || defaultNavItems);
  const [showNavIcons, setShowNavIcons] = useState(initial.showNavIcons ?? true);
  const [showAdd, setShowAdd] = useState(false);
  const [showBank, setShowBank] = useState(false);
  const [showImport, setShowImport] = useState(false);
  const [showNavEditor, setShowNavEditor] = useState(false);
  const [dark, setDark] = useState(false);
  const [sensitive, setSensitive] = useState(false);
  // Keep the first server and client renders identical. Browser connectivity is
  // read after hydration, then maintained by online/offline events.
  const [online, setOnline] = useState(true);
  const [notice, setNotice] = useState<string | null>(null);

  useEffect(() => {
    const updateOnline = () => setOnline(navigator.onLine);
    const initialCheck = window.requestAnimationFrame(updateOnline);
    window.addEventListener("online", updateOnline);
    window.addEventListener("offline", updateOnline);
    if ("serviceWorker" in navigator) navigator.serviceWorker.register("/sw.js").catch(() => undefined);
    return () => { window.cancelAnimationFrame(initialCheck); window.removeEventListener("online", updateOnline); window.removeEventListener("offline", updateOnline); };
  }, []);

  useEffect(() => { localStorage.setItem("together-budget-demo", JSON.stringify({ transactions, budgets, goals, banks, navItems, showNavIcons })); }, [transactions, budgets, goals, banks, navItems, showNavIcons]);
  useEffect(() => { if (!notice) return; const id = window.setTimeout(() => setNotice(null), 3200); return () => window.clearTimeout(id); }, [notice]);

  const totals = useMemo(() => transactions.filter((item) => !item.pending).reduce((acc, item) => {
    if (item.kind === "income") acc.income += item.amount;
    if (item.kind === "expense") acc.expense += item.amount;
    if (item.kind === "transfer" && item.transferDirection !== "in") acc.goals += item.amount;
    return acc;
  }, { income: 0, expense: 0, goals: 0 }), [transactions]);
  const sharedBalance = useMemo(() => {
    const shared = transactions.filter((item) => item.kind === "expense" && item.participants.length > 1);
    return shared.reduce((balance, item) => balance + (item.paidBy === "Nadia" ? item.amount : 0) - item.amount / item.participants.length, 0);
  }, [transactions]);

  function addTransaction(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const kind = form.get("kind") as TransactionKind;
    const amount = Number(form.get("amount"));
    if (!amount || amount < 0) { setNotice("Enter an amount greater than zero."); return; }
    const shared = form.get("scope") === "shared";
    const item: Transaction = {
      id: uid("txn"), title: String(form.get("title") || "Untitled entry"), amount, kind,
      category: String(form.get("category")), date: String(form.get("date")), paidBy: String(form.get("paidBy")),
      participants: shared ? members : [String(form.get("paidBy"))], splitMethod: String(form.get("split") || "equal") as SplitMethod,
      notes: String(form.get("notes") || ""), pending: form.get("pending") === "on", recurring: String(form.get("recurring") || ""), currency: "SGD", source: "manual"
    };
    if (kind === "transfer") {
      const destination = String(form.get("transferTo") || "Japan fund");
      const transferGroupId = uid("transfer");
      const outgoing: Transaction = { ...item, title: `Transfer to ${destination}`, category: "Transfer", sheet: "Shared expenses", transferGroupId, transferDirection: "out" };
      const incoming: Transaction = { ...item, id: uid("txn"), title: "Transfer from Shared expenses", category: "Transfer", sheet: destination, participants: [item.paidBy], transferGroupId, transferDirection: "in" };
      setTransactions((items) => [outgoing, incoming, ...items]);
    } else setTransactions((items) => [item, ...items]);
    setShowAdd(false);
    setNotice(kind === "transfer" ? "Linked transfer created in both sheets." : item.pending ? "Pending transaction saved." : "Transaction added.");
  }

  async function importCsv(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    if (!file) return;
    const [header, ...rows] = (await file.text()).trim().split(/\r?\n/);
    const columns = header.split(",").map((column) => column.trim().toLowerCase());
    const required = ["date", "category", "price", "notes"];
    if (!required.every((column) => columns.includes(column))) { setNotice("CSV needs Date, Category, Price, and Notes headers."); return; }
    const imported = rows.flatMap((row) => {
      const cells = row.split(","); const get = (name: string) => cells[columns.indexOf(name)]?.trim() || "";
      const price = Number(get("price"));
      if (!Number.isFinite(price)) return [];
      return [{ id: uid("import"), title: get("notes") || get("merchant") || "Imported transaction", amount: Math.abs(price), kind: price > 0 ? "income" as const : "expense" as const, category: get("category") || "Other", date: get("date").slice(0, 10) || new Date().toISOString().slice(0, 10), paidBy: "Nadia", participants: ["Nadia"], splitMethod: "equal" as const, notes: get("notes"), currency: get("currency") || "SGD", merchant: get("merchant"), source: "manual" as const }];
    });
    setTransactions((items) => [...imported, ...items]); setShowImport(false); setNotice(`${imported.length} transaction${imported.length === 1 ? "" : "s"} imported for review.`);
  }

  function exportCsv() {
    const heading = "Date,Category,Price,Currency,Notes,Merchant,Paid By\n";
    const rows = transactions.map((item) => [item.date, item.category, item.kind === "income" ? item.amount : -item.amount, item.currency, csv(item.notes || item.title), csv(item.merchant || ""), item.paidBy].join(","));
    const blob = new Blob([[heading, ...rows].join("\n")], { type: "text/csv" });
    const link = document.createElement("a"); link.href = URL.createObjectURL(blob); link.download = "together-budget.csv"; link.click(); URL.revokeObjectURL(link.href);
  }

  function connectDemoBank() {
    setBanks((items) => [{ id: uid("bank"), label: "Connected account", institution: "Provider sandbox", status: "connected", lastSynced: "just now", accountMask: "•••• 4821" }, ...items]);
    setShowBank(false); setNotice("Sandbox connection added. Add provider credentials to enable a real bank feed.");
  }

  return <main className={dark ? "app dark" : "app"}>
    {view === "home" && <HomeView totals={totals} budgets={budgets} goals={goals} balance={sharedBalance} sensitive={sensitive} onAdd={() => setShowAdd(true)} onView={(next) => setView(next)} />}
    {view === "ledger" && <LedgerView items={transactions} sensitive={sensitive} onAdd={() => setShowAdd(true)} />}
    {view === "plans" && <PlansView budgets={budgets} goals={goals} sensitive={sensitive} onAddGoal={(goal) => { setGoals((items) => [goal, ...items]); setNotice("Goal created."); }} onAddBudget={(budget) => { setBudgets((items) => [budget, ...items]); setNotice("Budget created."); }} />}
    {view === "insights" && <InsightsView items={transactions} totals={totals} sensitive={sensitive} />}
    {view === "settings" && <SettingsView online={online} dark={dark} sensitive={sensitive} banks={banks} onDark={() => setDark((value) => !value)} onSensitive={() => setSensitive((value) => !value)} onBank={() => setShowBank(true)} onImport={() => setShowImport(true)} onExport={exportCsv} onCustomizeNav={() => setShowNavEditor(true)} />}

    <nav className="tabbar" aria-label="Primary navigation">
      {navItems.filter((item) => item.visible).map((item) => <button key={item.id} onClick={() => setView(item.id)} className={view === item.id ? "active" : ""}>{showNavIcons && <span><AppIcon name={item.icon} /></span>}{item.label}</button>)}
    </nav>
    {showAdd && <TransactionModal onClose={() => setShowAdd(false)} onSubmit={addTransaction} />}
    {showBank && <BankModal onClose={() => setShowBank(false)} onConnect={connectDemoBank} />}
    {showImport && <ImportModal onClose={() => setShowImport(false)} onFile={importCsv} />}
    {showNavEditor && <NavEditor items={navItems} showIcons={showNavIcons} onClose={() => setShowNavEditor(false)} onChange={(items, icons) => { setNavItems(items); setShowNavIcons(icons); if (!items.some((item) => item.id === view && item.visible)) setView(items.find((item) => item.visible)?.id || "home"); }} />}
    {notice && <div className="toast" role="status">{notice}</div>}
  </main>;
}

function HomeView({ totals, budgets, goals, balance, sensitive, onAdd, onView }: { totals: { income: number; expense: number; goals: number }; budgets: Budget[]; goals: Goal[]; balance: number; sensitive: boolean; onAdd: () => void; onView: (view: View) => void }) {
  const reveal = (value: string) => sensitive ? "••••••" : value;
  return <section className="screen">
    <div className="greeting"><div><p className="eyebrow">shared household</p><h2>Good morning, Nadia</h2></div><button className="icon-button" onClick={onAdd} aria-label="Add transaction"><AppIcon name="plus" size="lg" /></button></div>
    <article className="balance-card"><div className="balance-heading"><span>August balance</span><span className="pill">SGD</span></div><strong>{reveal(money(totals.income - totals.expense - totals.goals))}</strong><div className="balance-columns"><span>Income <b>{reveal(money(totals.income))}</b></span><span>Spent <b>{reveal(money(totals.expense))}</b></span></div></article>
    <section className="two-up"><article className="mini-card"><span className="icon-dot yellow"><AppIcon name="upRight" size="sm" /></span><p>Settle up</p><strong>{balance >= 0 ? "Leo owes you" : "You owe Leo"}</strong><b>{reveal(money(Math.abs(balance)))}</b><button onClick={() => onView("ledger")}>See balance <AppIcon name="forward" size="sm" /></button></article><article className="mini-card soft"><span className="icon-dot purple"><AppIcon name="goals" size="sm" /></span><p>Next goal</p><strong>{goals[0].title}</strong><b>{Math.round(goals[0].saved / goals[0].target * 100)}% funded</b><button onClick={() => onView("plans")}>View goals <AppIcon name="forward" size="sm" /></button></article></section>
    <section className="section-heading"><div><p className="eyebrow">this month</p><h3>Budgets</h3></div><button onClick={() => onView("plans")}>See all</button></section>
    <div className="budget-list">{budgets.slice(0, 2).map((budget) => <BudgetRow key={budget.id} budget={budget} sensitive={sensitive} />)}</div>
    <section className="section-heading"><div><p className="eyebrow">keep moving</p><h3>Goals</h3></div><button onClick={() => onView("plans")}>Manage</button></section>
    <div className="goal-strip">{goals.map((goal) => <article key={goal.id} className="goal-card"><span><AppIcon name={goal.shared ? "goals" : "plans"} size="xs" /> {goal.shared ? "shared" : "personal"}</span><h4>{goal.title}</h4><div className="progress"><i style={{ width: `${Math.min(goal.saved / goal.target * 100, 100)}%` }} /></div><b>{reveal(`${money(goal.saved)} of ${money(goal.target)}`)}</b><small>by {dateLabel(goal.deadline)}</small></article>)}</div>
  </section>;
}

function LedgerView({ items, sensitive, onAdd }: { items: Transaction[]; sensitive: boolean; onAdd: () => void }) {
  return <section className="screen"><div className="screen-title"><div><h2>Ledger</h2></div><button className="icon-button" onClick={onAdd} aria-label="Add transaction"><AppIcon name="plus" size="lg" /></button></div><div className="filter-row"><button className="selected">This month</button><button>All entries</button><button>Filter</button></div><div className="transaction-list">{items.map((item) => { const incoming = item.kind === "income" || item.transferDirection === "in"; return <article className="transaction" key={item.id}><span className={`category-icon ${item.kind}`}><AppIcon name={iconFor(item.category)} size="sm" /></span><div><strong>{item.title}</strong><small>{dateLabel(item.date)} · {item.category}{item.sheet ? ` · ${item.sheet}` : ""}{item.source === "bank" ? " · bank feed" : ""}{item.recurring ? ` · ${item.recurring}` : ""}</small></div><div className={incoming ? "amount positive" : "amount"}>{sensitive ? "••••" : `${incoming ? "+" : "−"}${money(item.amount, item.currency)}`}<small>{item.paidBy}</small></div></article>; })}</div></section>;
}

function PlansView({ budgets, goals, sensitive, onAddGoal, onAddBudget }: { budgets: Budget[]; goals: Goal[]; sensitive: boolean; onAddGoal: (goal: Goal) => void; onAddBudget: (budget: Budget) => void }) {
  const [adding, setAdding] = useState<"goal" | "budget" | null>(null);
  return <section className="screen"><div className="screen-title"><div><h2>Plans</h2></div><button className="icon-button" onClick={() => setAdding("goal")} aria-label="Add savings goal"><AppIcon name="plus" size="lg" /></button></div><section className="section-heading"><h3>Budgets</h3><button onClick={() => setAdding("budget")}>New budget</button></section><div className="budget-list">{budgets.map((budget) => <BudgetRow key={budget.id} budget={budget} sensitive={sensitive} />)}</div><section className="section-heading"><h3>Savings goals</h3><button onClick={() => setAdding("goal")}>New goal</button></section><div className="stack">{goals.map((goal) => <article key={goal.id} className="goal-wide"><div><span className="icon-dot purple"><AppIcon name="goals" size="sm" /></span><div><strong>{goal.title}</strong><small>{goal.shared ? "Shared with Leo" : "Personal"} · due {dateLabel(goal.deadline)}</small></div></div><b>{sensitive ? "••••" : money(goal.saved)} <small>/ {sensitive ? "••••" : money(goal.target)}</small></b><div className="progress"><i style={{ width: `${Math.min(goal.saved / goal.target * 100, 100)}%` }} /></div></article>)}</div>{adding && <PlanForm type={adding} onCancel={() => setAdding(null)} onGoal={(goal) => { onAddGoal(goal); setAdding(null); }} onBudget={(budget) => { onAddBudget(budget); setAdding(null); }} />}</section>;
}

function InsightsView({ items, totals, sensitive }: { items: Transaction[]; totals: { income: number; expense: number; goals: number }; sensitive: boolean }) {
  const grouped = Object.entries(items.filter((item) => item.kind === "expense").reduce<Record<string, number>>((acc, item) => ({ ...acc, [item.category]: (acc[item.category] || 0) + item.amount }), {})).sort((a, b) => b[1] - a[1]);
  return <section className="screen"><div className="screen-title"><div><p className="eyebrow">august 2026</p><h2>Insights</h2></div><button className="period">This month <AppIcon name="chevronDown" size="xs" /></button></div><article className="insight-card"><div><span>Total spent</span><strong>{sensitive ? "••••••" : money(totals.expense)}</strong><small><AppIcon name="upRight" size="xs" /> 8% from July</small></div><div className="donut"><b>{sensitive ? "••" : "64%"}</b><small>planned</small></div></article><section className="section-heading"><h3>Spending trend</h3><button>Weekly</button></section><div className="chart">{[44, 26, 63, 38, 72, 48, 82, 58, 35, 65, 54, 91].map((height, index) => <i key={index} style={{ height: `${height}%` }} />)}</div><section className="section-heading"><h3>By category</h3><button>Details</button></section><div className="category-totals">{grouped.map(([category, amount]) => <div key={category}><span><AppIcon name={iconFor(category)} size="sm" /> {category}</span><b>{sensitive ? "••••" : money(amount)}</b></div>)}</div></section>;
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
function iconFor(category: string): IconKey { return ({ Groceries: "cart", Dining: "dining", Transport: "transport", Utilities: "utilities", Rent: "home", Health: "goals", Shopping: "cart", Entertainment: "insights", Salary: "salary", Goals: "goals", Transfer: "transfer" } as Record<string, IconKey>)[category] || "plans"; }
function csv(value: string) { return `"${value.replaceAll('"', '""')}"`; }
