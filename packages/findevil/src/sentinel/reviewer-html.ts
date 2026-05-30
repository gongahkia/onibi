import type { JsonRecord } from "@kelpclaw/workflow-spec";
import type { RepairTraceRow } from "../repair/index.js";
import type { Claim, ClaimLedger, ClaimStatus } from "../types/claim.js";
import type { FirewallEvent } from "../types/firewall.js";
import type { SpoliationCheck } from "../types/spoliation.js";

type EvidenceManifest = JsonRecord | null | undefined;

export function buildReviewerHtml(
  ledger: ClaimLedger,
  repairTrace: readonly RepairTraceRow[],
  firewallEvents: readonly FirewallEvent[],
  spoliationCheck: SpoliationCheck | undefined,
  evidenceManifest: EvidenceManifest
): string {
  const claims = ledger.claims;
  const statusCounts = countStatuses(claims);
  const bootstrap = htmlJson({
    ledger,
    repairTrace,
    firewallEvents,
    spoliationCheck: spoliationCheck ?? null,
    evidenceManifest: evidenceManifest ?? null
  });
  return [
    "<!doctype html>",
    '<html lang="en">',
    "<head>",
    '<meta charset="utf-8">',
    '<meta name="viewport" content="width=device-width, initial-scale=1">',
    '<link rel="icon" href="data:,">',
    "<title>KelpClaw Find Evil Reviewer</title>",
    `<style>${reviewerCss()}</style>`,
    "</head>",
    "<body>",
    '<div class="shell">',
    "<header>",
    '<div><p class="eyebrow">KelpClaw SIFT Sentinel</p>',
    "<h1>Signed Audit Reviewer</h1>",
    `<p class="meta">Ledger ${escapeHtml(ledger.id)}${ledger.runId ? ` - Run ${escapeHtml(ledger.runId)}` : ""} - ${escapeHtml(ledger.generatedAt)}</p></div>`,
    `<div class="scorebar">${renderStatusPills(statusCounts)}</div>`,
    "</header>",
    "<main>",
    '<section class="panel claims-panel">',
    "<h2>Claims</h2>",
    `<div id="claimList" class="claim-list">${renderStaticClaimList(claims)}</div>`,
    "</section>",
    '<section class="panel detail-panel">',
    '<div id="claimDetail" class="claim-detail">',
    claims[0] ? renderStaticClaimDetail(claims[0], repairTrace) : "<p>No claims recorded.</p>",
    "</div>",
    "</section>",
    '<section class="panel firewall-panel">',
    "<h2>Firewall Blocks</h2>",
    `<div id="firewallList" class="firewall-list">${renderStaticFirewallList(firewallEvents)}</div>`,
    '<div id="firewallDetail" class="firewall-detail">',
    firewallEvents[0]
      ? renderStaticFirewallDetail(firewallEvents[0])
      : "<p>No firewall blocks recorded.</p>",
    "</div>",
    "</section>",
    '<section class="panel spoliation-panel">',
    "<h2>Spoliation Check</h2>",
    `<div id="spoliationView">${renderStaticSpoliation(spoliationCheck)}</div>`,
    "</section>",
    "</main>",
    '<footer id="signatureFooter">Manifest signature: loading from manifest.sig - Attestation hash: loading from attestation.json</footer>',
    "</div>",
    `<template id="reviewerBootstrap">${bootstrap}</template>`,
    `<script type="module">${reviewerScript()}</script>`,
    "</body>",
    "</html>"
  ].join("");
}

function renderStaticClaimList(claims: readonly Claim[]): string {
  if (claims.length === 0) {
    return '<p class="empty">No claims recorded.</p>';
  }
  return claims
    .map(
      (claim, index) =>
        `<button class="claim-card ${statusClass(claim.status)}${index === 0 ? " active" : ""}" type="button" data-claim="${escapeHtml(claim.id)}">` +
        `<span class="claim-id">${escapeHtml(claim.id)}</span>` +
        `<span class="claim-text">${escapeHtml(claim.text)}</span>` +
        `<span class="claim-status">${escapeHtml(claim.status)}</span>` +
        "</button>"
    )
    .join("");
}

function renderStaticClaimDetail(claim: Claim, repairTrace: readonly RepairTraceRow[]): string {
  const trace = repairTrace.filter((row) => row.claimId === claim.id);
  return [
    `<div class="detail-head"><span class="status-dot ${statusClass(claim.status)}"></span><div><h2>${escapeHtml(claim.id)}</h2><p>${escapeHtml(claim.text)}</p></div></div>`,
    '<div class="kv">',
    `<span>status</span><strong>${escapeHtml(claim.status)}</strong>`,
    `<span>type</span><strong>${escapeHtml(claim.type)}</strong>`,
    `<span>severity</span><strong>${escapeHtml(claim.severity)}</strong>`,
    `<span>confidence</span><strong>${Math.round(claim.confidence * 100)}%</strong>`,
    "</div>",
    `<h3>Verifier Rule</h3><p>${escapeHtml(ruleText(claim))}</p>`,
    "<h3>Evidence</h3>",
    claim.evidenceRefs.length > 0
      ? claim.evidenceRefs
          .map(
            (ref, index) =>
              `<article class="evidence-ref"><div><strong>${escapeHtml(ref.artifact)}</strong><span>${escapeHtml(ref.locator)}</span></div>` +
              `<p>${escapeHtml(ref.supports)} - ${escapeHtml(ref.hash)}</p>` +
              `<button type="button" data-preview="${index}">Preview linked artifact</button><pre id="preview-${index}"></pre></article>`
          )
          .join("")
      : '<p class="empty">No evidence references recorded.</p>',
    "<h3>Repair Timeline</h3>",
    trace.length > 0
      ? trace
          .map(
            (row) =>
              `<div class="trace-row"><span>${escapeHtml(row.timestamp)}</span><strong>${escapeHtml(row.event)}</strong><p>${escapeHtml(row.prompt ?? row.output ?? row.status ?? "")}</p></div>`
          )
          .join("")
      : '<p class="empty">No repair trace rows for this claim.</p>'
  ].join("");
}

function renderStaticFirewallList(events: readonly FirewallEvent[]): string {
  if (events.length === 0) {
    return '<p class="empty">No firewall blocks recorded.</p>';
  }
  return events
    .map(
      (event, index) =>
        `<button class="firewall-card${index === 0 ? " active" : ""}" type="button" data-firewall="${escapeHtml(event.id)}">` +
        `<strong>${escapeHtml(event.id)}</strong><span>${escapeHtml(event.policyDecision.action)} - ${escapeHtml(event.source.path)}</span>` +
        "</button>"
    )
    .join("");
}

function renderStaticFirewallDetail(event: FirewallEvent): string {
  return [
    `<h3>${escapeHtml(event.id)}</h3>`,
    `<blockquote>${escapeHtml(event.taintedText)}</blockquote>`,
    `<p><strong>Blocked use:</strong> ${escapeHtml(event.blockedUse.text)}</p>`,
    `<p><strong>Policy:</strong> ${escapeHtml(event.policyDecision.reason)}</p>`,
    `<h4>Safe reanalysis prompt</h4><pre>${escapeHtml(event.correctionTask.prompt)}</pre>`
  ].join("");
}

function renderStaticSpoliation(check: SpoliationCheck | undefined): string {
  if (!check) {
    return '<p class="empty">No spoliation check recorded.</p>';
  }
  const changed = new Set(check.changed);
  const after = new Map(check.after.map((row) => [row.path, row]));
  const rows = check.before
    .map((before) => {
      const next = after.get(before.path);
      return (
        "<tr>" +
        `<td>${escapeHtml(before.path)}</td>` +
        `<td>${escapeHtml(shortHash(before.sha256))}</td>` +
        `<td>${escapeHtml(shortHash(next?.sha256 ?? "missing"))}</td>` +
        `<td>${changed.has(before.path) ? "changed" : next ? "same" : "removed"}</td>` +
        "</tr>"
      );
    })
    .join("");
  return [
    `<p class="${check.ok ? "ok" : "bad"}">${check.ok ? "Passed" : "Failed"} - before ${check.before.length} - after ${check.after.length} - added ${check.added.length} - removed ${check.removed.length} - changed ${check.changed.length}</p>`,
    `<table><thead><tr><th>Path</th><th>Before</th><th>After</th><th>Status</th></tr></thead><tbody>${rows}</tbody></table>`
  ].join("");
}

function renderStatusPills(counts: Readonly<Record<ClaimStatus, number>>): string {
  return (Object.keys(counts) as ClaimStatus[])
    .map(
      (status) =>
        `<span class="pill ${statusClass(status)}">${escapeHtml(status)} ${counts[status]}</span>`
    )
    .join("");
}

function countStatuses(claims: readonly Claim[]): Record<ClaimStatus, number> {
  return {
    confirmed: claims.filter((claim) => claim.status === "confirmed").length,
    inferred: claims.filter((claim) => claim.status === "inferred").length,
    unsupported: claims.filter((claim) => claim.status === "unsupported").length,
    contradicted: claims.filter((claim) => claim.status === "contradicted").length,
    unverifiable: claims.filter((claim) => claim.status === "unverifiable").length
  };
}

function ruleText(claim: Claim): string {
  switch (claim.type) {
    case "program_execution":
      return "Program execution is confirmed only by execution artifacts such as Prefetch, Amcache execution records, ShimCache, Sysmon process-create, or Security 4688. File presence alone is unsupported.";
    case "persistence":
      return "Persistence requires a durable mechanism such as a Run key, scheduled task, service creation event, or equivalent registry and event-log corroboration.";
    case "network_connection":
      return "Network claims require netflow or PCAP evidence. DNS-only evidence is treated as inferred, not confirmed.";
    default:
      return "The verifier requires direct artifact support for the claim type and downgrades claims with missing or contradictory evidence.";
  }
}

function statusClass(status: ClaimStatus): string {
  return `status-${status}`;
}

function shortHash(value: string): string {
  return value.length > 24 ? `${value.slice(0, 19)}...${value.slice(-6)}` : value;
}

function htmlJson(value: unknown): string {
  return escapeHtml(JSON.stringify(value));
}

function escapeHtml(value: string): string {
  return value
    .replace(/&/gu, "&amp;")
    .replace(/</gu, "&lt;")
    .replace(/>/gu, "&gt;")
    .replace(/"/gu, "&quot;")
    .replace(/'/gu, "&#39;");
}

function reviewerCss(): string {
  return `
:root{color-scheme:light;--bg:#f6f7f9;--panel:#fff;--text:#17202a;--muted:#65717f;--line:#d8dee6;--green:#18794e;--amber:#966f00;--orange:#b45309;--red:#b42318;--gray:#667085;--blue:#175cd3}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--text);font:14px/1.45 ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}.shell{min-height:100vh;display:flex;flex-direction:column}
header{display:flex;justify-content:space-between;gap:24px;padding:24px 28px;border-bottom:1px solid var(--line);background:#fff}h1,h2,h3,h4,p{margin:0}h1{font-size:26px}h2{font-size:17px}h3{font-size:14px;margin-top:18px}.eyebrow,.meta,.empty{color:var(--muted)}.eyebrow{font-size:12px;text-transform:uppercase;letter-spacing:.08em}.scorebar{display:flex;align-items:flex-start;justify-content:flex-end;gap:8px;flex-wrap:wrap}.pill{border:1px solid var(--line);border-radius:999px;padding:4px 8px;font-size:12px;background:#fff}
main{display:grid;grid-template-columns:330px minmax(0,1fr);grid-auto-rows:min-content;gap:16px;padding:16px;align-items:start}.panel{background:var(--panel);border:1px solid var(--line);border-radius:8px;padding:16px}.claims-panel{grid-row:span 3;position:sticky;top:16px;max-height:calc(100vh - 32px);overflow:auto}.claim-list,.firewall-list{display:grid;gap:8px;margin-top:12px}.claim-card,.firewall-card{width:100%;text-align:left;border:1px solid var(--line);background:#fff;border-radius:8px;padding:10px;display:grid;gap:4px;cursor:pointer}.claim-card.active,.firewall-card.active{outline:2px solid var(--blue);border-color:transparent}.claim-id{font-weight:700}.claim-text{color:var(--text)}.claim-status,.firewall-card span{color:var(--muted);font-size:12px}.status-confirmed{border-color:#b7dfc7;background:#f0fdf4;color:var(--green)}.status-inferred{border-color:#ead58f;background:#fffbeb;color:var(--amber)}.status-unsupported{border-color:#fed7aa;background:#fff7ed;color:var(--orange)}.status-contradicted{border-color:#fecaca;background:#fef2f2;color:var(--red)}.status-unverifiable{border-color:#d0d5dd;background:#f8fafc;color:var(--gray)}
.detail-head{display:flex;gap:12px;align-items:flex-start}.status-dot{width:14px;height:14px;border-radius:50%;margin-top:5px;border-width:2px;border-style:solid}.kv{display:grid;grid-template-columns:110px 1fr;gap:6px;margin-top:14px}.kv span{color:var(--muted)}.evidence-ref{border:1px solid var(--line);border-radius:8px;padding:10px;margin-top:8px}.evidence-ref div{display:flex;justify-content:space-between;gap:12px}.evidence-ref span{color:var(--muted)}button[data-preview]{margin-top:8px;border:1px solid var(--line);border-radius:6px;background:#f8fafc;padding:6px 8px}pre{white-space:pre-wrap;word-break:break-word;background:#101828;color:#e6edf3;border-radius:8px;padding:10px;max-height:280px;overflow:auto}.trace-row{border-left:3px solid var(--line);padding:8px 0 8px 10px}.trace-row span{color:var(--muted);font-size:12px}blockquote{margin:10px 0;padding:10px;border-left:4px solid var(--red);background:#fff1f2}table{width:100%;border-collapse:collapse;margin-top:10px}th,td{border-bottom:1px solid var(--line);padding:7px;text-align:left;vertical-align:top}th{font-size:12px;color:var(--muted)}.ok{color:var(--green)}.bad{color:var(--red)}footer{margin-top:auto;padding:12px 16px;color:var(--muted);border-top:1px solid var(--line);background:#fff;font-size:12px}
@media(max-width:860px){header{display:block}main{grid-template-columns:1fr}.claims-panel{position:static;max-height:none}}
`;
}

function reviewerScript(): string {
  return `
const boot=JSON.parse(document.getElementById("reviewerBootstrap").textContent||"{}");
const $=(id)=>document.getElementById(id);
const esc=(v)=>String(v??"").replace(/[&<>"']/g,(c)=>({"&":"&amp;","<":"&lt;",">":"&gt;","\\"":"&quot;","'":"&#39;"}[c]));
const statusClass=(s)=>"status-"+String(s||"unverifiable");
let state={ledger:boot.ledger||{claims:[]},repairTrace:boot.repairTrace||[],firewallEvents:boot.firewallEvents||[],spoliationCheck:boot.spoliationCheck,evidenceManifest:boot.evidenceManifest};
let activeClaimId=state.ledger.claims[0]?.id||"";
async function init(){
  state.ledger=await json("claim-ledger.json",state.ledger);
  state.repairTrace=await jsonl("repair-trace.jsonl",state.repairTrace);
  state.firewallEvents=await jsonl("firewall-events.jsonl",state.firewallEvents);
  state.spoliationCheck=await json("spoliation-check.json",state.spoliationCheck);
  state.evidenceManifest=await json("evidence-manifest.json",state.evidenceManifest);
  activeClaimId=state.ledger.claims[0]?.id||activeClaimId;
  render();
  await footer();
}
async function json(file,fallback){try{const r=await fetch(file,{cache:"no-store"});if(!r.ok)throw new Error(String(r.status));return await r.json()}catch{return fallback}}
async function text(file){try{const r=await fetch(file,{cache:"no-store"});if(!r.ok)throw new Error(String(r.status));return await r.text()}catch{return ""}}
async function jsonl(file,fallback){const t=await text(file);if(!t.trim())return fallback;return t.trim().split(/\\r?\\n/).filter(Boolean).map((line)=>JSON.parse(line))}
function render(){renderClaims();showClaim(activeClaimId);renderFirewall();renderSpoliation()}
function renderClaims(){
  const claims=state.ledger.claims||[];
  $("claimList").innerHTML=claims.length?claims.map((c)=>'<button class="claim-card '+statusClass(c.status)+(c.id===activeClaimId?' active':'')+'" type="button" data-claim="'+esc(c.id)+'"><span class="claim-id">'+esc(c.id)+'</span><span class="claim-text">'+esc(c.text)+'</span><span class="claim-status">'+esc(c.status)+'</span></button>').join(""):'<p class="empty">No claims recorded.</p>';
}
function showClaim(id){
  const claim=(state.ledger.claims||[]).find((c)=>c.id===id)||(state.ledger.claims||[])[0];
  if(!claim){$("claimDetail").innerHTML='<p class="empty">No claims recorded.</p>';return}
  activeClaimId=claim.id;renderClaims();
  const refs=claim.evidenceRefs||[],trace=(state.repairTrace||[]).filter((r)=>r.claimId===claim.id);
  $("claimDetail").innerHTML='<div class="detail-head"><span class="status-dot '+statusClass(claim.status)+'"></span><div><h2>'+esc(claim.id)+'</h2><p>'+esc(claim.text)+'</p></div></div>'+
    '<div class="kv"><span>status</span><strong>'+esc(claim.status)+'</strong><span>type</span><strong>'+esc(claim.type)+'</strong><span>severity</span><strong>'+esc(claim.severity)+'</strong><span>confidence</span><strong>'+Math.round((claim.confidence||0)*100)+'%</strong></div>'+
    '<h3>Verifier Rule</h3><p>'+esc(rule(claim))+'</p><h3>Evidence</h3>'+
    (refs.length?refs.map((ref,i)=>'<article class="evidence-ref"><div><strong>'+esc(ref.artifact)+'</strong><span>'+esc(ref.locator)+'</span></div><p>'+esc(ref.supports)+' - '+esc(ref.hash)+'</p><button type="button" data-preview="'+i+'">Preview linked artifact</button><pre id="preview-'+i+'"></pre></article>').join(""):'<p class="empty">No evidence references recorded.</p>')+
    '<h3>Repair Timeline</h3>'+(trace.length?trace.map((r)=>'<div class="trace-row"><span>'+esc(r.timestamp)+' - iteration '+esc(r.iteration)+'</span><strong>'+esc(r.event)+'</strong><p>'+esc(r.prompt||r.output||r.status||"")+'</p></div>').join(""):'<p class="empty">No repair trace rows for this claim.</p>');
}
function rule(c){if(c.type==="program_execution")return"Program execution is confirmed only by execution artifacts such as Prefetch, Amcache execution records, ShimCache, Sysmon process-create, or Security 4688. File presence alone is unsupported.";if(c.type==="persistence")return"Persistence requires a durable mechanism such as a Run key, scheduled task, service creation event, or equivalent registry and event-log corroboration.";if(c.type==="network_connection")return"Network claims require netflow or PCAP evidence. DNS-only evidence is treated as inferred, not confirmed.";return"The verifier requires direct artifact support for the claim type and downgrades claims with missing or contradictory evidence."}
async function previewRef(index){
  const claim=(state.ledger.claims||[]).find((c)=>c.id===activeClaimId),ref=claim?.evidenceRefs?.[index],pre=$("preview-"+index);
  if(!ref||!pre)return;pre.textContent="Loading "+ref.artifact+" ...";
  if(!safePath(ref.artifact)){pre.textContent="Unsafe artifact path in evidenceRef.";return}
  const raw=await text(ref.artifact);
  if(!raw){pre.textContent="Artifact file is not bundled. Recorded locator: "+ref.locator+"\\nRecorded hash: "+ref.hash;return}
  pre.textContent=artifactPreview(ref,raw);
}
function artifactPreview(ref,raw){
  if(ref.artifact.endsWith(".json")){try{return JSON.stringify(pick(JSON.parse(raw),ref.locator),null,2).slice(0,5000)}catch{}}
  if(ref.locator&&ref.locator.startsWith("row:")){return csvRow(raw,ref.locator)}
  return raw.slice(0,5000);
}
function pick(obj,loc){
  const path=/^([a-zA-Z0-9_]+)\\[(\\d+)\\]$/.exec(loc||"");
  if(path&&Array.isArray(obj?.[path[1]]))return obj[path[1]][Number(path[2])];
  const rec=/record:([^\\s]+)/.exec(loc||"");
  if(rec)return findValue(obj,rec[1])||obj;
  const pf=/prefetchFile:(.+)$/.exec(loc||"");
  if(pf&&obj?.prefetchFile===pf[1])return obj;
  return obj;
}
function findValue(obj,needle){if(!obj||typeof obj!=="object")return null;for(const value of Object.values(obj)){const found=findValue(value,needle);if(found)return found}return JSON.stringify(obj).includes(needle)?obj:null}
function csvRow(raw,loc){const n=Number((/row:(\\d+)/.exec(loc)||[])[1]);const lines=raw.split(/\\r?\\n/);return lines.find((line)=>line.startsWith(n+","))||lines[n-1]||""}
function safePath(path){return typeof path==="string"&&path&&!path.startsWith("/")&&!path.includes("..")}
function renderFirewall(){
  const events=state.firewallEvents||[];
  $("firewallList").innerHTML=events.length?events.map((e,i)=>'<button class="firewall-card '+(i===0?'active':'')+'" type="button" data-firewall="'+esc(e.id)+'"><strong>'+esc(e.id)+'</strong><span>'+esc(e.policyDecision?.action)+' - '+esc(e.source?.path)+'</span></button>').join(""):'<p class="empty">No firewall blocks recorded.</p>';
  if(events[0])showFirewall(events[0].id);
}
function showFirewall(id){
  const event=(state.firewallEvents||[]).find((e)=>e.id===id);if(!event)return;
  document.querySelectorAll("[data-firewall]").forEach((n)=>n.classList.toggle("active",n.dataset.firewall===id));
  $("firewallDetail").innerHTML='<h3>'+esc(event.id)+'</h3><blockquote>'+esc(event.taintedText)+'</blockquote><p><strong>Blocked use:</strong> '+esc(event.blockedUse?.text)+'</p><p><strong>Policy:</strong> '+esc(event.policyDecision?.reason)+'</p><h4>Safe reanalysis prompt</h4><pre>'+esc(event.correctionTask?.prompt)+'</pre>';
}
function renderSpoliation(){
  const c=state.spoliationCheck;if(!c){$("spoliationView").innerHTML='<p class="empty">No spoliation check recorded.</p>';return}
  const after=new Map((c.after||[]).map((r)=>[r.path,r])),changed=new Set(c.changed||[]);
  $("spoliationView").innerHTML='<p class="'+(c.ok?'ok':'bad')+'">'+(c.ok?'Passed':'Failed')+' - before '+(c.before||[]).length+' - after '+(c.after||[]).length+' - added '+(c.added||[]).length+' - removed '+(c.removed||[]).length+' - changed '+(c.changed||[]).length+'</p><table><thead><tr><th>Path</th><th>Before</th><th>After</th><th>Status</th></tr></thead><tbody>'+((c.before||[]).map((b)=>{const a=after.get(b.path);return'<tr><td>'+esc(b.path)+'</td><td>'+esc(shortHash(b.sha256))+'</td><td>'+esc(shortHash(a?.sha256||"missing"))+'</td><td>'+(changed.has(b.path)?'changed':a?'same':'removed')+'</td></tr>'}).join(""))+'</tbody></table>';
}
function shortHash(v){v=String(v||"");return v.length>24?v.slice(0,19)+"..."+v.slice(-6):v}
async function footer(){
  const sig=(await text("manifest.sig")).trim(),attText=await text("attestation.json");
  const att=attText?await sha256(attText):"unavailable";
  $("signatureFooter").textContent="Manifest signature: "+(sig?sig.slice(0,32)+"...":"unavailable")+" - Attestation hash: "+att;
}
async function sha256(value){if(!crypto.subtle)return"unavailable";const bytes=new TextEncoder().encode(value);const hash=await crypto.subtle.digest("SHA-256",bytes);return"sha256:"+[...new Uint8Array(hash)].map((b)=>b.toString(16).padStart(2,"0")).join("")}
document.addEventListener("click",(event)=>{const claim=event.target.closest("[data-claim]");if(claim)showClaim(claim.dataset.claim);const preview=event.target.closest("[data-preview]");if(preview)previewRef(Number(preview.dataset.preview));const fw=event.target.closest("[data-firewall]");if(fw)showFirewall(fw.dataset.firewall)});
init();
`;
}
