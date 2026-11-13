"use client";

import { useCallback, useEffect, useState } from "react";
import { send, shortDate } from "@/lib/client";
import { download, toCsv } from "@/lib/csv";
import { money, setLine, type SoldData, type SoldPage } from "@/lib/inventory";

type Row = SoldPage["sold"][number];
type Filter = { days: string; where: string; shipped: boolean; archived: boolean; q: string };
const SHIP_BY_DAYS = 30; // SoldCard.ShipByDays

function soldOn(d: SoldData): string {
  if (d.marketplace_order_id) return "Slabs & Singles";
  return d.sold_outside_ebay ? "In person / other" : "eBay";
}
function orderNo(d: SoldData): string {
  return (d.marketplace_order_id ?? "").split("|")[0] || d.ebay_order_id || "";
}
const shipped = (d: SoldData) => !!d.tracking_number || !!d.shipped_date_utc;
function shipBy(d: SoldData): Date | null {
  if (!d.sale_date || d.sold_outside_ebay) return null;
  return new Date(new Date(d.sale_date).getTime() + SHIP_BY_DAYS * 86400000);
}
function status(d: SoldData): { text: string; tone: string } {
  if (d.archived) return { text: "Archived", tone: "text-muted" };
  if (shipped(d)) return { text: "Shipped", tone: "text-go" };
  if (d.sold_outside_ebay) return { text: "Done", tone: "text-muted" };
  const by = shipBy(d);
  if (by && by.getTime() < Date.now()) return { text: "Overdue", tone: "text-bad" };
  return { text: "To ship", tone: "text-fire" };
}
const net = (d: SoldData) => (d.sale_price ?? 0) + (d.shipping_charged ?? 0);

export default function SoldLog({ edition }: { edition: string }) {
  const [f, setF] = useState<Filter>({ days: "", where: "", shipped: false, archived: false, q: "" });
  const [q, setQ] = useState("");
  const [page, setPage] = useState(1);
  const [data, setData] = useState<SoldPage | null>(null);
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [open, setOpen] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<{ text: string; bad?: boolean } | null>(null);

  const query = useCallback((extra: Record<string, string> = {}) => {
    const p = new URLSearchParams({ edition, view: "1", ...extra });
    if (f.days) p.set("days", f.days);
    if (f.where) p.set("where", f.where);
    if (f.shipped) p.set("shipped", "1");
    if (f.archived) p.set("archived", "1");
    if (f.q) p.set("q", f.q);
    return "/v1/inventory/sold?" + p.toString();
  }, [edition, f]);

  const load = useCallback(async () => {
    const r = await send<SoldPage>("GET", query({ page: String(page) }));
    if (r.ok) setData(r.data);
    else setMsg({ text: r.message, bad: true });
  }, [query, page]);
  useEffect(() => { load(); }, [load]);

  function change(c: Partial<Filter>) {
    setF({ ...f, ...c });
    setPage(1);
    setPicked(new Set());
  }

  const rows: Row[] = data?.sold ?? [];
  const pages = data ? Math.max(1, Math.ceil(data.total / data.per_page)) : 1;
  const sel = rows.filter((r) => picked.has(r.id));

  async function markShipped() {
    if (sel.length === 0) return;
    const tracking = prompt(`Tracking number for ${sel.length} sale${sel.length === 1 ? "" : "s"} (leave blank to mark shipped with no tracking number):`, "");
    if (tracking === null) return;
    const carrier = prompt("Carrier (USPS, UPS, FedEx...), optional:", "USPS") ?? "";
    setBusy(true);
    const r = await send<{ marked: number; slabs_orders: number; problems?: string[] }>("POST", "/v1/inventory/sold/shipped", { ids: sel.map((x) => x.id), tracking, carrier });
    setBusy(false);
    if (!r.ok) return setMsg({ text: r.message, bad: true });
    const told = r.data.slabs_orders - (r.data.problems?.length ?? 0);
    setMsg({
      text: `Marked ${r.data.marked} shipped.${told > 0 ? ` Slabs & Singles was told about ${told} order${told === 1 ? "" : "s"}.` : ""}${r.data.problems?.length ? " " + r.data.problems.join(" ") : ""}`,
      bad: !!r.data.problems?.length,
    });
    setPicked(new Set());
    load();
  }

  async function setArchived(on: boolean) {
    setBusy(true);
    let n = 0;
    for (const x of sel) if ((await send("PATCH", "/v1/inventory/sold/" + x.id, { fields: { archived: on } })).ok) n++;
    setBusy(false);
    setMsg({ text: `${on ? "Archived" : "Restored"} ${n} sale${n === 1 ? "" : "s"}.` });
    setPicked(new Set());
    load();
  }

  async function remove() {
    if (!confirm(`Delete ${sel.length} sold-log record${sel.length === 1 ? "" : "s"}? This only removes the log entries; it doesn't put the cards back in inventory. Your PC removes them too on its next sync.`)) return;
    setBusy(true);
    let n = 0;
    for (const x of sel) if ((await send("DELETE", "/v1/inventory/sold/" + x.id)).ok) n++;
    setBusy(false);
    setMsg({ text: `Deleted ${n} record${n === 1 ? "" : "s"}.` });
    setPicked(new Set());
    load();
  }

  async function exportCsv() {
    setBusy(true);
    const r = await send<SoldPage>("GET", query({ all: "1" }));
    setBusy(false);
    if (!r.ok) return setMsg({ text: r.message, bad: true });
    const out = [["Sale Date", "Sold On", "Card", "Buyer", "Sale Price", "Shipping", "Net", "Order #", "Tracking #", "Carrier", "Ship By", "Status", "Notes"]];
    for (const { data: d } of r.data.sold) {
      const by = shipBy(d);
      out.push([d.sale_date ? d.sale_date.slice(0, 10) : "", soldOn(d), `${d.player_name ?? ""} - ${setLine(d)}`, d.buyer ?? "",
        (d.sale_price ?? 0).toFixed(2), (d.shipping_charged ?? 0).toFixed(2), net(d).toFixed(2), orderNo(d), d.tracking_number ?? "", d.carrier ?? "",
        by && !shipped(d) ? by.toISOString().slice(0, 10) : "", status(d).text, d.notes ?? ""]);
    }
    download(`${edition === "tcg" ? "FireCardManagerTCG" : "FireCardManager"}_SoldLog_${new Date().toISOString().slice(0, 10).replace(/-/g, "")}.csv`, toCsv(out));
  }

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <select className="field !mt-0 !w-auto !py-1.5" value={f.days} onChange={(e) => change({ days: e.target.value })} aria-label="When">
          <option value="">All</option><option value="7">Last 7 days</option><option value="30">Last 30 days</option>
        </select>
        <select className="field !mt-0 !w-auto !py-1.5" value={f.where} onChange={(e) => change({ where: e.target.value })} aria-label="Sold on">
          <option value="">All platforms</option><option value="ebay">eBay</option><option value="slabs">Slabs &amp; Singles</option><option value="other">In person / other</option>
        </select>
        <label className="flex items-center gap-1.5 text-[13px]"><input type="checkbox" checked={f.shipped} onChange={(e) => change({ shipped: e.target.checked })} /> Show shipped</label>
        <label className="flex items-center gap-1.5 text-[13px]"><input type="checkbox" checked={f.archived} onChange={(e) => change({ archived: e.target.checked })} /> Show archived</label>
        <form className="flex min-w-[180px] flex-1 gap-2" onSubmit={(e) => { e.preventDefault(); change({ q }); }}>
          <input className="field !mt-0 !py-1.5" type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search player, set, buyer, tracking..." />
        </form>
        <button type="button" className="btn-ghost hidden !py-1.5 lg:inline-flex" disabled={busy} onClick={exportCsv}>Export CSV</button>
      </div>

      {(picked.size > 0 || msg) && (
        <div className="flex flex-wrap items-center gap-2 rounded-lg border border-line bg-wash px-3 py-2 text-[13px]">
          {picked.size > 0 && (
            <>
              <span className="font-semibold">{picked.size} selected</span>
              <button type="button" className="btn !py-1" disabled={busy} onClick={markShipped}>📦 Mark shipped</button>
              {f.archived && sel.some((x) => x.data.archived)
                ? <button type="button" className="btn-ghost !py-1" disabled={busy} onClick={() => setArchived(false)}>↩ Restore</button>
                : <button type="button" className="btn-ghost !py-1" disabled={busy} onClick={() => setArchived(true)}>Archive</button>}
              <button type="button" className="btn-ghost !py-1 text-bad" disabled={busy} onClick={remove}>Delete</button>
            </>
          )}
          {msg && <span role="status" className={`ml-auto ${msg.bad ? "text-bad" : "text-go"}`}>{msg.text}</span>}
        </div>
      )}

      <p className="num text-[13px] text-muted">
        {data ? `${data.total.toLocaleString()} sale${data.total === 1 ? "" : "s"} · ${money(data.revenue)} in sales` : "Loading..."}
      </p>

      <div className="overflow-x-auto rounded-xl border border-line">
        <table className="w-full text-left text-[13px]">
          <thead className="bg-wash text-[11px] uppercase tracking-wide text-muted">
            <tr>
              <th className="w-8 px-2 py-2">
                <input type="checkbox" aria-label="Select all shown" checked={rows.length > 0 && rows.every((r) => picked.has(r.id))}
                  onChange={(e) => setPicked(e.target.checked ? new Set(rows.map((r) => r.id)) : new Set())} />
              </th>
              <th className="px-2">Date</th><th className="px-2">Card</th>
              <th className="hidden px-2 md:table-cell">Sold on</th><th className="hidden px-2 lg:table-cell">Buyer</th>
              <th className="px-2 text-right">Price</th><th className="hidden px-2 text-right lg:table-cell">Ship</th><th className="hidden px-2 text-right md:table-cell">Net</th>
              <th className="hidden px-2 xl:table-cell">Order #</th><th className="hidden px-2 lg:table-cell">Tracking</th>
              <th className="hidden px-2 xl:table-cell">Ship by</th><th className="px-2">Status</th><th className="hidden px-2 xl:table-cell">Notes</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-line">
            {data && rows.length === 0 && <tr><td colSpan={13} className="p-4 text-muted">No sales match.</td></tr>}
            {rows.map((r) => {
              const d = r.data;
              const st = status(d);
              const by = shipBy(d);
              return [
                <tr key={r.id} className={`cursor-pointer ${open === r.id ? "bg-wash" : "hover:bg-wash"}`} onClick={() => setOpen(open === r.id ? null : r.id)}>
                  <td className="px-2 py-2" onClick={(e) => e.stopPropagation()}>
                    <input type="checkbox" checked={picked.has(r.id)} aria-label={`Select ${d.player_name}`}
                      onChange={(e) => { const n = new Set(picked); if (e.target.checked) n.add(r.id); else n.delete(r.id); setPicked(n); }} />
                  </td>
                  <td className="whitespace-nowrap px-2">{d.sale_date ? shortDate(d.sale_date) : ""}</td>
                  <td className="max-w-[320px] px-2">
                    <p className="truncate font-semibold">{d.player_name}</p>
                    <p className="truncate text-[12px] text-muted">{setLine(d)}</p>
                  </td>
                  <td className="hidden whitespace-nowrap px-2 md:table-cell">{soldOn(d)}</td>
                  <td className="hidden max-w-[140px] truncate px-2 lg:table-cell">{d.buyer}</td>
                  <td className="num px-2 text-right font-semibold">{money(d.sale_price)}</td>
                  <td className="num hidden px-2 text-right lg:table-cell">{money(d.shipping_charged)}</td>
                  <td className="num hidden px-2 text-right md:table-cell">{money(net(d))}</td>
                  <td className="hidden max-w-[120px] truncate px-2 xl:table-cell">{orderNo(d)}</td>
                  <td className="hidden whitespace-nowrap px-2 lg:table-cell">{d.tracking_number ? `${d.carrier ? d.carrier + " " : ""}${d.tracking_number}` : ""}</td>
                  <td className="hidden whitespace-nowrap px-2 xl:table-cell">{by && !shipped(d) ? shortDate(by.toISOString()) : ""}</td>
                  <td className={`whitespace-nowrap px-2 font-semibold ${st.tone}`}>{st.text}</td>
                  <td className="hidden max-w-[200px] truncate px-2 text-muted xl:table-cell">{d.notes}</td>
                </tr>,
                open === r.id && (
                  <tr key={r.id + "-edit"}><td colSpan={13} className="bg-wash px-3 py-3">
                    <SaleEditor row={r} onSaved={(nd) => { setData((p) => p && { ...p, sold: p.sold.map((x) => (x.id === r.id ? { ...x, data: nd } : x)) }); setMsg({ text: "Sale saved." }); }} />
                  </td></tr>
                ),
              ];
            })}
          </tbody>
        </table>
      </div>

      {pages > 1 && (
        <div className="flex items-center justify-center gap-3 text-[14px]">
          <button type="button" className="btn-ghost" disabled={page <= 1} onClick={() => setPage(page - 1)}>Previous</button>
          <span className="num text-muted">Page {page} of {pages}</span>
          <button type="button" className="btn-ghost" disabled={page >= pages} onClick={() => setPage(page + 1)}>Next</button>
        </div>
      )}
    </div>
  );
}

/** One sale's details, opened under its row. */
function SaleEditor({ row, onSaved }: { row: Row; onSaved: (d: SoldData) => void }) {
  const d = row.data;
  const init = {
    buyer: d.buyer ?? "", notes: d.notes ?? "", sale_price: d.sale_price != null ? d.sale_price.toFixed(2) : "",
    shipping_charged: d.shipping_charged != null ? d.shipping_charged.toFixed(2) : "", tracking_number: d.tracking_number ?? "", carrier: d.carrier ?? "",
  };
  const [v, setV] = useState(init);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const set = (k: keyof typeof init) => (e: React.ChangeEvent<HTMLInputElement>) => setV({ ...v, [k]: e.target.value });

  async function save() {
    const fields: Record<string, unknown> = {};
    for (const k of Object.keys(init) as (keyof typeof init)[]) {
      if (v[k] === init[k]) continue;
      if (k === "sale_price" || k === "shipping_charged") {
        const n = Number(v[k].replace(/[$,\s]/g, "") || "0");
        if (Number.isNaN(n)) return setErr("Enter amounts as numbers.");
        fields[k] = n;
      } else fields[k] = v[k].trim() || null;
    }
    if (Object.keys(fields).length === 0) return;
    setBusy(true);
    const r = await send<{ data: SoldData }>("PATCH", "/v1/inventory/sold/" + row.id, { fields });
    setBusy(false);
    if (!r.ok) return setErr(r.message + (Object.keys(r.fields).length ? " " + Object.values(r.fields).join(" ") : ""));
    setErr("");
    onSaved(r.data.data);
  }

  return (
    <div className="grid gap-3 text-[13px] sm:grid-cols-2 lg:grid-cols-4">
      <label className="block font-semibold">Buyer<input className="field" value={v.buyer} onChange={set("buyer")} /></label>
      <label className="block font-semibold">Sale price<input className="field num" inputMode="decimal" value={v.sale_price} onChange={set("sale_price")} /></label>
      <label className="block font-semibold">Shipping charged<input className="field num" inputMode="decimal" value={v.shipping_charged} onChange={set("shipping_charged")} /></label>
      <label className="block font-semibold">Carrier<input className="field" value={v.carrier} onChange={set("carrier")} placeholder="USPS" /></label>
      <label className="block font-semibold">Tracking number<input className="field" value={v.tracking_number} onChange={set("tracking_number")} /></label>
      <label className="block font-semibold sm:col-span-2 lg:col-span-3">Notes<input className="field" value={v.notes} onChange={set("notes")} /></label>
      <div className="flex flex-wrap items-center gap-3 sm:col-span-2 lg:col-span-4">
        <button type="button" className="btn !py-1.5" disabled={busy} onClick={save}>{busy ? "Saving..." : "Save sale"}</button>
        {d.ebay_listing_id && <a className="font-semibold text-link" href={`https://www.ebay.com/itm/${d.ebay_listing_id}`} target="_blank" rel="noopener noreferrer">View on eBay ↗</a>}
        <span className="text-muted">Sold {d.sale_date ? shortDate(d.sale_date) : ""} on {soldOn(d)}{orderNo(d) ? ` · order ${orderNo(d)}` : ""}</span>
        {err && <span className="text-bad">{err}</span>}
      </div>
    </div>
  );
}
