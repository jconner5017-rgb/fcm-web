"use client";

import { useRouter } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { send } from "@/lib/client";
import { profileSummary, type Profile } from "@/components/EbaySettings";
import { liveText, locationCode, money, setLine, thumbUrl, type CardPage, type CardRow, type TreeSport } from "@/lib/inventory";

type Filter = { sport: string; box: string; status: string; q: string };

/** Why the marketplace would turn a card down (the app's own checks), shown before listing. */
function problems(c: CardRow, auction: boolean): string[] {
  const d = c.data;
  const p: string[] = [];
  if (!auction && !(d.sale_price && d.sale_price > 0)) p.push("no price");
  if (!d.front_photo_id) p.push("no front photo");
  if (!d.player_name || d.player_name.startsWith("(unidentified")) p.push("not identified");
  if (!d.set_name) p.push("no set");
  if (!d.sku) p.push("not synced from your PC yet");
  return p;
}

type EbayState = { on: boolean; connected: boolean; missing: string[]; profiles: Profile[] };

export default function BulkLister({ edition, sports, hasKey, ebay, initialIds = [] }: {
  edition: string; sports: TreeSport[]; hasKey: boolean; ebay: EbayState; initialIds?: string[];
}) {
  const [market, setMarket] = useState<"slabs" | "ebay">("slabs");
  const ebayReady = ebay.on && ebay.connected && ebay.missing.length === 0;
  const [profile, setProfile] = useState(ebay.profiles[0]?.id ?? "");
  useEffect(() => {
    try { const v = localStorage.getItem("fcm-ship-profile"); if (v && ebay.profiles.some((p) => p.id === v)) setProfile(v); } catch {}
  }, [ebay.profiles]);
  const router = useRouter();
  // Cards sent from the Card Editor arrive picked, with any status shown.
  const [filter, setFilter] = useState<Filter>({ sport: "", box: "", status: initialIds.length ? "" : "Ready", q: "" });
  const [q, setQ] = useState("");
  const [page, setPage] = useState(1);
  const [data, setData] = useState<CardPage | null>(null);
  const [loading, setLoading] = useState(true);
  const [picked, setPicked] = useState<Set<string>>(new Set(initialIds));
  const [allMatching, setAllMatching] = useState(false);
  const [auction, setAuction] = useState(false);
  const [duration, setDuration] = useState(7);
  const [startBid, setStartBid] = useState("");
  const [binAtPrice, setBinAtPrice] = useState(true);
  const [offers, setOffers] = useState(false);
  const [accept, setAccept] = useState("90");
  const [decline, setDecline] = useState("60");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    const p = new URLSearchParams({ edition, sort: "slot", per: "60", page: String(page) });
    for (const [k, v] of Object.entries(filter) as [string, string][]) if (v) p.set(k, v);
    const r = await send<CardPage>("GET", "/v1/inventory/cards?" + p.toString());
    setLoading(false);
    if (r.ok) setData(r.data);
    else setError(r.message);
  }, [edition, filter, page]);
  useEffect(() => { load(); }, [load]);

  const sport = sports.find((s) => s.sport === filter.sport);
  const cards = data?.cards ?? [];
  const pageIds = cards.map((c) => c.id);
  const allOnPage = pageIds.length > 0 && pageIds.every((id) => picked.has(id));
  const count = allMatching ? data?.total ?? 0 : picked.size;
  const pages = data ? Math.max(1, Math.ceil(data.total / 60)) : 1;

  function change(f: Partial<Filter>) {
    setFilter({ ...filter, ...f });
    setPage(1);
    setPicked(new Set());
    setAllMatching(false);
  }
  function toggle(id: string) {
    const next = new Set(picked);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    setPicked(next);
    setAllMatching(false);
  }
  function togglePage() {
    const next = new Set(picked);
    for (const id of pageIds) {
      if (allOnPage) next.delete(id);
      else next.add(id);
    }
    setPicked(next);
    setAllMatching(false);
  }

  async function start() {
    setError("");
    const num = (s: string) => (s.trim() === "" ? null : Number(s.replace(/[$,%\s]/g, "")));
    const options = {
      auction, duration_days: duration, default_start_bid: num(startBid), buy_it_now_at_sale_price: binAtPrice,
      allow_offers: !auction && offers, accept_percent: offers ? num(accept) : null, decline_percent: offers ? num(decline) : null,
      shipping_profile: market === "ebay" ? profile : undefined,
    };
    if (market === "ebay") try { localStorage.setItem("fcm-ship-profile", profile); } catch {}
    const body: Record<string, unknown> = { edition, market, options };
    if (allMatching) body.filter = { sport: filter.sport, box: filter.box, status: filter.status, q: filter.q };
    else body.card_ids = [...picked];
    setBusy(true);
    const r = await send<{ id: string }>("POST", "/v1/lister/jobs", body);
    setBusy(false);
    if (!r.ok) return setError(r.message);
    router.push(`/lister/runs/${r.data.id}`);
  }

  return (
    <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_320px]">
      <section className="min-w-0 space-y-3">
        <div className="flex flex-wrap gap-2">
          <select className="field !mt-0 !w-auto" value={filter.status} onChange={(e) => change({ status: e.target.value })} aria-label="Status">
            <option value="Ready">Ready to list</option>
            <option value="NotForSale">Not for sale</option>
            <option value="listed">Live on eBay</option>
            <option value="">Any status</option>
          </select>
          <select className="field !mt-0 !w-auto" value={filter.sport} onChange={(e) => change({ sport: e.target.value, box: "" })} aria-label="Category">
            <option value="">All categories</option>
            {sports.map((s) => <option key={s.sport} value={s.sport}>{s.sport} ({s.cards})</option>)}
          </select>
          {sport && (
            <select className="field !mt-0 !w-auto" value={filter.box} onChange={(e) => change({ box: e.target.value })} aria-label="Box">
              <option value="">All boxes</option>
              {sport.boxes.map((b) => <option key={b.id} value={b.id}>{b.label} ({b.cards})</option>)}
            </select>
          )}
          <form className="flex min-w-[180px] flex-1 gap-2" onSubmit={(e) => { e.preventDefault(); change({ q }); }}>
            <input className="field !mt-0" type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search player, set, SKU..." />
            <button className="btn-ghost !py-2" type="submit">Search</button>
          </form>
        </div>

        <div className="flex flex-wrap items-center gap-3 text-[13px]">
          <label className="flex items-center gap-2 font-semibold">
            <input type="checkbox" checked={allOnPage} onChange={togglePage} /> Select this page
          </label>
          {data && data.total > cards.length && (
            <button type="button" className="font-semibold text-link" onClick={() => { setAllMatching(true); setPicked(new Set(pageIds)); }}>
              Select all {data.total.toLocaleString()} matching
            </button>
          )}
          {(picked.size > 0 || allMatching) && (
            <button type="button" className="text-muted hover:text-fire" onClick={() => { setPicked(new Set()); setAllMatching(false); }}>Clear</button>
          )}
          <span className="ml-auto num text-muted">{data ? `${data.total.toLocaleString()} cards` : ""}</span>
        </div>

        <ul className="divide-y divide-line rounded-xl border border-line">
          {loading && cards.length === 0 && <li className="p-4 text-muted">Loading...</li>}
          {!loading && cards.length === 0 && <li className="p-4 text-muted">No cards match.</li>}
          {cards.map((c) => {
            const p = problems(c, auction);
            const live = liveText(c.data);
            const src = c.thumb_url || thumbUrl(c.data);
            const on = allMatching || picked.has(c.id);
            return (
              <li key={c.id}>
                <label className={`flex cursor-pointer items-center gap-3 p-2 ${on ? "bg-wash" : ""}`}>
                  <input type="checkbox" checked={on} onChange={() => toggle(c.id)} disabled={allMatching} />
                  {src ? (
                    // eslint-disable-next-line @next/next/no-img-element
                    <img src={src} alt="" loading="lazy" className="h-14 w-10 shrink-0 rounded object-contain" />
                  ) : <div className="h-14 w-10 shrink-0 rounded bg-wash" />}
                  <div className="min-w-0 flex-1">
                    <p className="truncate font-semibold">{c.data.player_name}</p>
                    <p className="truncate text-[12px] text-muted">{setLine(c.data)}</p>
                    <p className="truncate text-[11px] text-muted">
                      {c.data.sku} · {locationCode(c)}
                      {live && <span className="text-go"> · {live}{live.includes("S&S") ? " (will update)" : ""}</span>}
                      {p.length > 0 && <span className="text-bad"> · {p.join(", ")}</span>}
                    </p>
                  </div>
                  <span className="num shrink-0 font-bold">{money(c.data.sale_price) || "—"}</span>
                </label>
              </li>
            );
          })}
        </ul>

        {pages > 1 && (
          <div className="flex items-center justify-center gap-3 text-[14px]">
            <button type="button" className="btn-ghost" disabled={page <= 1} onClick={() => setPage(page - 1)}>Previous</button>
            <span className="num text-muted">Page {page} of {pages}</span>
            <button type="button" className="btn-ghost" disabled={page >= pages} onClick={() => setPage(page + 1)}>Next</button>
          </div>
        )}
      </section>

      <aside className="space-y-3 lg:sticky lg:top-4 lg:self-start">
        <div className="card space-y-4">
          <div>
            <p className="text-[13px] font-semibold">List on</p>
            <div className="mt-2 grid gap-2">
              <label className={`flex cursor-pointer items-center gap-2 rounded-lg border px-3 py-2 text-[14px] ${market === "slabs" ? "border-fire" : "border-line"}`}>
                <input type="radio" checked={market === "slabs"} onChange={() => setMarket("slabs")} /> Slabs &amp; Singles
              </label>
              <label className={`flex items-center gap-2 rounded-lg border px-3 py-2 text-[14px] ${market === "ebay" ? "border-fire" : "border-line"} ${ebay.on ? "cursor-pointer" : "text-muted"}`}>
                <input type="radio" checked={market === "ebay"} disabled={!ebay.on} onChange={() => setMarket("ebay")} /> eBay
                {!ebay.on && <span className="ml-auto text-[11px]">coming soon</span>}
              </label>
            </div>
          </div>

          {market === "ebay" && ebay.profiles.length > 0 && (
            <label className="block text-[13px] font-semibold">Shipping profile
              <select className="field" value={profile} onChange={(e) => setProfile(e.target.value)}>
                {ebay.profiles.map((p) => <option key={p.id} value={p.id}>{profileSummary(p)}</option>)}
              </select>
              <a href="/account#ebay" className="text-[11px] font-normal text-link">Edit shipping profiles</a>
            </label>
          )}

          <div>
            <p className="text-[13px] font-semibold">Format</p>
            <div className="mt-2 flex gap-2 text-[14px]">
              <label className="flex items-center gap-2"><input type="radio" checked={!auction} onChange={() => setAuction(false)} /> Buy It Now</label>
              <label className="flex items-center gap-2"><input type="radio" checked={auction} onChange={() => setAuction(true)} /> Auction</label>
            </div>
          </div>

          {auction ? (
            <div className="space-y-2 text-[13px]">
              <label className="block font-semibold">Length
                <select className="field" value={duration} onChange={(e) => setDuration(Number(e.target.value))}>
                  {[1, 3, 5, 7, 10].map((d) => <option key={d} value={d}>{d} day{d === 1 ? "" : "s"}</option>)}
                </select>
              </label>
              <label className="block font-semibold">Starting bid for cards without one
                <input className="field num" inputMode="decimal" value={startBid} onChange={(e) => setStartBid(e.target.value)} placeholder="$0.99" />
              </label>
              <label className="flex items-center gap-2"><input type="checkbox" checked={binAtPrice} onChange={(e) => setBinAtPrice(e.target.checked)} /> Buy It Now at the card&apos;s price</label>
            </div>
          ) : (
            <div className="space-y-2 text-[13px]">
              <label className="flex items-center gap-2 font-semibold"><input type="checkbox" checked={offers} onChange={(e) => setOffers(e.target.checked)} /> Accept offers</label>
              {offers && (
                <div className="grid grid-cols-2 gap-2">
                  <label className="block">Auto-accept at
                    <input className="field num" inputMode="numeric" value={accept} onChange={(e) => setAccept(e.target.value)} />
                    <span className="text-[11px] text-muted">% of price or more</span>
                  </label>
                  <label className="block">Auto-decline under
                    <input className="field num" inputMode="numeric" value={decline} onChange={(e) => setDecline(e.target.value)} />
                    <span className="text-[11px] text-muted">% of price</span>
                  </label>
                </div>
              )}
            </div>
          )}

          {market === "ebay" && !ebayReady && (
            <p className="rounded-lg border border-fire px-3 py-2 text-[13px]">
              {!ebay.connected ? <>Connect eBay on the <a href="/account#ebay" className="font-semibold text-fire">Account page</a> first.</>
                : <>Pick your eBay {ebay.missing.join(", ")} on the <a href="/account#ebay" className="font-semibold text-fire">Account page</a> first.</>}
            </p>
          )}
          {market === "slabs" && !hasKey && (
            <p className="rounded-lg border border-fire px-3 py-2 text-[13px]">
              Add a Slabs &amp; Singles key made <b>for FireCardManager Web</b> on the <a href="/account#market-keys" className="font-semibold text-fire">Account page</a> first.
            </p>
          )}
          {error && <p role="alert" className="text-[13px] text-bad">{error}</p>}
          <button type="button" className="btn w-full" disabled={busy || count === 0 || (market === "slabs" ? !hasKey : !ebayReady)} onClick={start}>
            {busy ? "Starting..." : `List ${count.toLocaleString()} card${count === 1 ? "" : "s"} on ${market === "ebay" ? "eBay" : "Slabs & Singles"}`}
          </button>
          {market === "slabs" ? (
            <p className="text-[12px] text-muted">
              Uses your package and weight settings from FireCardManager. Cards already on Slabs &amp; Singles are updated. Cards that can&apos;t be listed
              yet are skipped with the reason. Your PC shows them live after its next sync.
            </p>
          ) : (
            <p className="text-[12px] text-muted">
              Listed like FireCardManager does: the Store Branded template with your photos, item specifics and your business policies (Account &gt; eBay).
              Cards already on eBay are updated. Your PC shows them live after its next sync.
            </p>
          )}
        </div>
      </aside>
    </div>
  );
}
