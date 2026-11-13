"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import { send } from "@/lib/client";
import { locationCode, money, setLine, thumbUrl, type CardData } from "@/lib/inventory";

type Slabs = { id: string; status: string; price_cents: number; format: string; updated_at: string };
type Live = { id: string; box_name?: string; data: CardData; slabs?: Slabs };
type Res = { edition: string; cards: Live[]; slabs_checked: boolean; slabs_problem?: string };

type Orphan = { sku: string; title: string; listing_id: string; format: string; price: number };
type Refresh = { live: number; matched: number; marked_live: number; price_changes: { sku: string; name: string; old: number | null; new: number }[]; orphans: Orphan[]; not_live: string[] };

const onEbay = (d: CardData) => d.status === "EBA" || d.status === "EBF";
const onSlabs = (d: CardData) => !!d.marketplace_listing_id;

export default function LiveListings({ edition, hasKey, ebayOn }: { edition: string; hasKey: boolean; ebayOn: boolean }) {
  const [res, setRes] = useState<Res | null>(null);
  const [where, setWhere] = useState("");
  const [q, setQ] = useState("");
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<{ text: string; bad?: boolean } | null>(null);
  const [pulled, setPulled] = useState<Refresh | null>(null);

  // Like FireCardManager's Live Listings refresh: pull every live listing from
  // eBay, mark the matching cards live and copy prices changed on eBay.
  async function pullEbay() {
    setBusy(true);
    setMsg({ text: "Checking your eBay listings..." });
    const r = await send<Refresh>("POST", "/v1/live/ebay/refresh");
    setBusy(false);
    if (!r.ok) return setMsg({ text: r.message, bad: true });
    const d = r.data;
    setPulled(d);
    const parts = [`eBay reports ${d.live} live listing${d.live === 1 ? "" : "s"}; ${d.matched} match cards here.`];
    if (d.marked_live) parts.push(`${d.marked_live} card${d.marked_live === 1 ? " was" : "s were"} marked live.`);
    if (d.price_changes.length) parts.push(`${d.price_changes.length} price${d.price_changes.length === 1 ? "" : "s"} updated from eBay.`);
    if (d.orphans.length) parts.push(`${d.orphans.length} aren't in this inventory (listed below).`);
    setMsg({ text: parts.join(" ") });
    load();
  }

  const load = useCallback(async () => {
    const r = await send<Res>("GET", "/v1/live?edition=" + edition);
    if (r.ok) setRes(r.data);
    else setMsg({ text: r.message, bad: true });
  }, [edition]);
  useEffect(() => { load(); }, [load]);

  const words = q.toLowerCase().split(/\s+/).filter(Boolean);
  const cards = (res?.cards ?? []).filter((c) => {
    if (where === "ebay" && !onEbay(c.data)) return false;
    if (where === "slabs" && !onSlabs(c.data)) return false;
    const text = [c.data.player_name, c.data.set_name, c.data.brand, c.data.sku, c.data.card_number, c.data.year].join(" ").toLowerCase();
    return words.every((w) => text.includes(w));
  });
  const ebayCount = (res?.cards ?? []).filter((c) => onEbay(c.data)).length;
  const slabsCount = (res?.cards ?? []).filter((c) => onSlabs(c.data)).length;
  const sel = cards.filter((c) => picked.has(c.id));
  const selSlabs = sel.filter((c) => onSlabs(c.data));
  const selEbay = sel.filter((c) => onEbay(c.data) && c.data.ebay_offer_id);

  async function endEbay(ids: string[]) {
    if (!confirm(`End ${ids.length} eBay listing${ids.length === 1 ? "" : "s"}? The card${ids.length === 1 ? " goes" : "s go"} back to Ready to list.`)) return;
    setBusy(true);
    const r = await send<{ ended: number; problems?: string[] }>("POST", "/v1/live/ebay/end", { card_ids: ids });
    setBusy(false);
    if (!r.ok) return setMsg({ text: r.message, bad: true });
    setMsg({ text: `Ended ${r.data.ended} eBay listing${r.data.ended === 1 ? "" : "s"}.${r.data.problems?.length ? " " + r.data.problems.join(" ") : ""}`, bad: !!r.data.problems?.length });
    setPicked(new Set());
    load();
  }

  async function repriceEbay(c: Live) {
    const v = prompt(`New eBay price for ${c.data.player_name}:`, c.data.sale_price?.toFixed(2) ?? "");
    if (v === null) return;
    const price = Number(v.replace(/[$,\s]/g, ""));
    if (!(price > 0)) return setMsg({ text: "Enter a price like 12.50.", bad: true });
    setBusy(true);
    const r = await send("POST", "/v1/live/ebay/price", { card_id: c.id, price });
    setBusy(false);
    if (!r.ok) return setMsg({ text: r.message, bad: true });
    setMsg({ text: `${c.data.player_name} is now ${money(price)} on eBay and in your inventory.` });
    load();
  }

  async function end(ids: string[]) {
    if (!confirm(`End ${ids.length} Slabs & Singles listing${ids.length === 1 ? "" : "s"}? The card${ids.length === 1 ? " stays" : "s stay"} in your inventory.`)) return;
    setBusy(true);
    const r = await send<{ ended: number; problems?: string[] }>("POST", "/v1/live/slabs/end", { card_ids: ids });
    setBusy(false);
    if (!r.ok) return setMsg({ text: r.message, bad: true });
    setMsg({ text: `Ended ${r.data.ended} listing${r.data.ended === 1 ? "" : "s"} on Slabs & Singles.${r.data.problems?.length ? " " + r.data.problems.join(" ") : ""}`, bad: !!r.data.problems?.length });
    setPicked(new Set());
    load();
  }

  async function reprice(c: Live) {
    const cur = c.slabs ? (c.slabs.price_cents / 100).toFixed(2) : c.data.sale_price?.toFixed(2) ?? "";
    const v = prompt(`New Slabs & Singles price for ${c.data.player_name}:`, cur);
    if (v === null) return;
    const price = Number(v.replace(/[$,\s]/g, ""));
    if (!(price > 0)) return setMsg({ text: "Enter a price like 12.50.", bad: true });
    setBusy(true);
    const r = await send("POST", "/v1/live/slabs/price", { card_id: c.id, price });
    setBusy(false);
    if (!r.ok) return setMsg({ text: r.message, bad: true });
    setMsg({ text: `${c.data.player_name} is now ${money(price)} on Slabs & Singles and in your inventory.` });
    load();
  }

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <div className="flex gap-1 rounded-full border border-line p-1 text-[13px] font-semibold">
          {[["", `All (${(res?.cards ?? []).length})`], ["ebay", `eBay (${ebayCount})`], ["slabs", `Slabs & Singles (${slabsCount})`]].map(([v, label]) => (
            <button key={v} type="button" onClick={() => { setWhere(v); setPicked(new Set()); }}
              className={`rounded-full px-3 py-1 ${where === v ? "bg-fire text-white" : "hover:text-fire"}`}>{label}</button>
          ))}
        </div>
        <input className="field !mt-0 min-w-[180px] flex-1 !py-1.5" type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search player, set, SKU..." />
        <button type="button" className="btn-ghost !py-1.5" disabled={busy} onClick={() => { setMsg(null); load(); }}>Refresh</button>
        {ebayOn && <button type="button" className="btn !py-1.5" disabled={busy} onClick={pullEbay}>Refresh from eBay</button>}
      </div>

      {res && !res.slabs_checked && slabsCount > 0 && (
        <p className="rounded-lg border border-line px-3 py-2 text-[13px] text-muted">
          {res.slabs_problem || (hasKey ? "" : <>Add a Slabs &amp; Singles key made for FireCardManager Web on the <Link href="/account#market-keys" className="font-semibold text-fire">Account page</Link> to see the site&apos;s prices and change or end listings here.</>)}
        </p>
      )}
      {ebayCount > 0 && !ebayOn && (
        <p className="text-[12px] text-muted">Connect eBay on the <Link href="/account#ebay" className="font-semibold text-fire">Account page</Link> to end or reprice eBay listings here.</p>
      )}

      {(selSlabs.length > 0 || selEbay.length > 0 || msg) && (
        <div className="flex flex-wrap items-center gap-2 rounded-lg border border-line bg-wash px-3 py-2 text-[13px]">
          {selSlabs.length > 0 && (
            <>
              <span className="font-semibold">{selSlabs.length} Slabs &amp; Singles listing{selSlabs.length === 1 ? "" : "s"} selected</span>
              <button type="button" className="btn-ghost !py-1 text-bad" disabled={busy || !res?.slabs_checked} onClick={() => end(selSlabs.map((c) => c.id))}>End on Slabs &amp; Singles</button>
            </>
          )}
          {ebayOn && selEbay.length > 0 && (
            <button type="button" className="btn-ghost !py-1 text-bad" disabled={busy} onClick={() => endEbay(selEbay.map((c) => c.id))}>
              End {selEbay.length} on eBay
            </button>
          )}
          {msg && <span role="status" className={`ml-auto ${msg.bad ? "text-bad" : "text-go"}`}>{msg.text}</span>}
        </div>
      )}

      <div className="overflow-x-auto rounded-xl border border-line">
        <table className="w-full text-left text-[13px]">
          <thead className="bg-wash text-[11px] uppercase tracking-wide text-muted">
            <tr>
              <th className="w-8 px-2 py-2">
                <input type="checkbox" aria-label="Select all shown" checked={cards.length > 0 && cards.every((c) => picked.has(c.id))}
                  onChange={(e) => setPicked(e.target.checked ? new Set(cards.map((c) => c.id)) : new Set())} />
              </th>
              <th className="px-2">Card</th><th className="hidden px-2 md:table-cell">SKU</th><th className="hidden px-2 lg:table-cell">Location</th>
              <th className="px-2">Live on</th><th className="px-2 text-right">Price</th><th className="hidden px-2 md:table-cell">Listed</th><th className="px-2"></th>
            </tr>
          </thead>
          <tbody className="divide-y divide-line">
            {!res && <tr><td colSpan={8} className="p-4 text-muted">Loading...</td></tr>}
            {res && cards.length === 0 && <tr><td colSpan={8} className="p-4 text-muted">Nothing live{where || q ? " matches" : " right now"}.</td></tr>}
            {cards.map((c) => {
              const d = c.data;
              const src = thumbUrl(d);
              const siteCents = c.slabs?.price_cents;
              const differs = siteCents != null && d.sale_price != null && Math.round(d.sale_price * 100) !== siteCents && c.slabs?.format !== "auction";
              return (
                <tr key={c.id} className="hover:bg-wash">
                  <td className="px-2 py-2">
                    <input type="checkbox" checked={picked.has(c.id)} aria-label={`Select ${d.player_name}`}
                      onChange={(e) => { const n = new Set(picked); if (e.target.checked) n.add(c.id); else n.delete(c.id); setPicked(n); }} />
                  </td>
                  <td className="px-2">
                    <Link href={`/inventory/cards/${c.id}`} className="flex items-center gap-2">
                      {src ? (
                        // eslint-disable-next-line @next/next/no-img-element
                        <img src={src} alt="" loading="lazy" className="h-12 w-9 shrink-0 rounded object-contain" />
                      ) : <span className="h-12 w-9 shrink-0 rounded bg-wash" />}
                      <span className="min-w-0">
                        <span className="block truncate font-semibold hover:text-fire">{d.player_name}</span>
                        <span className="block truncate text-[12px] text-muted">{setLine(d)}</span>
                      </span>
                    </Link>
                  </td>
                  <td className="hidden whitespace-nowrap px-2 md:table-cell">{d.sku}</td>
                  <td className="hidden whitespace-nowrap px-2 lg:table-cell">{locationCode({ box_name: c.box_name, data: d })}</td>
                  <td className="whitespace-nowrap px-2">
                    {onEbay(d) && <span className="mr-1 rounded-full bg-go px-2 py-0.5 text-[10px] font-bold uppercase text-white">{d.status === "EBA" ? "eBay auction" : "eBay"}</span>}
                    {onSlabs(d) && <span className="rounded-full bg-go px-2 py-0.5 text-[10px] font-bold uppercase text-white">S&amp;S{c.slabs && c.slabs.status !== "active" ? ` · ${c.slabs.status}` : ""}</span>}
                  </td>
                  <td className="num whitespace-nowrap px-2 text-right">
                    <span className="font-semibold">{money(d.sale_price)}</span>
                    {differs && <span className="block text-[11px] text-fire">S&amp;S: {money(siteCents! / 100)}</span>}
                    {c.slabs?.format === "auction" && <span className="block text-[11px] text-muted">bid {money(siteCents! / 100)}</span>}
                  </td>
                  <td className="hidden whitespace-nowrap px-2 text-muted md:table-cell">{d.marketplace_listed_at ? new Date(d.marketplace_listed_at).toLocaleDateString() : ""}</td>
                  <td className="whitespace-nowrap px-2 text-right">
                    {onEbay(d) && d.ebay_listing_id && (
                      <a className="mr-3 font-semibold text-link" href={`https://www.ebay.com/itm/${d.ebay_listing_id}`} target="_blank" rel="noopener noreferrer">eBay ↗</a>
                    )}
                    {ebayOn && onEbay(d) && d.ebay_offer_id && d.status === "EBF" && (
                      <button type="button" className="mr-3 font-semibold text-link" disabled={busy} onClick={() => repriceEbay(c)}>Price</button>
                    )}
                    {ebayOn && onEbay(d) && d.ebay_offer_id && (
                      <button type="button" className="mr-3 font-semibold text-bad" disabled={busy} onClick={() => endEbay([c.id])}>End</button>
                    )}
                    {onSlabs(d) && (
                      <>
                        <a className="mr-3 font-semibold text-link" href={`https://slabsnsingles.com/item/${d.marketplace_listing_id}`} target="_blank" rel="noopener noreferrer">S&amp;S ↗</a>
                        {res?.slabs_checked && c.slabs?.format !== "auction" && (
                          <button type="button" className="mr-3 font-semibold text-link" disabled={busy} onClick={() => reprice(c)}>Price</button>
                        )}
                        {res?.slabs_checked && <button type="button" className="font-semibold text-bad" disabled={busy} onClick={() => end([c.id])}>End</button>}
                      </>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      {pulled && (pulled.orphans.length > 0 || pulled.not_live.length > 0 || pulled.price_changes.length > 0) && (
        <div className="space-y-2 rounded-lg border border-line px-3 py-2 text-[13px]">
          {pulled.price_changes.length > 0 && (
            <div>
              <p className="font-semibold">Prices changed on eBay</p>
              <ul className="text-muted">{pulled.price_changes.map((p) => (
                <li key={p.sku}>{p.sku} {p.name}: {p.old == null ? "no price" : money(p.old)} → {money(p.new)}</li>
              ))}</ul>
            </div>
          )}
          {pulled.orphans.length > 0 && (
            <div>
              <p className="font-semibold">Live on eBay, not in this inventory</p>
              <p className="text-muted">Their SKU doesn&apos;t match a card here (listed on eBay outside FireCardManager, or the card was deleted here).</p>
              <ul>{pulled.orphans.map((o) => (
                <li key={o.listing_id}>
                  <a className="font-semibold text-link" href={`https://www.ebay.com/itm/${o.listing_id}`} target="_blank" rel="noreferrer">{o.title || o.sku}</a>
                  <span className="text-muted"> · {o.sku} · {o.format === "AUCTION" ? "Auction" : "Buy It Now"} · {money(o.price)}</span>
                </li>
              ))}</ul>
            </div>
          )}
          {pulled.not_live.length > 0 && (
            <div>
              <p className="font-semibold">Marked live here, but not live on eBay</p>
              <p className="text-muted">They may have sold, ended or been listed outside the API. Check them on eBay: {pulled.not_live.join(", ")}</p>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
