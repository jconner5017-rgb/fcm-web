"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import { send } from "@/lib/client";
import { download } from "@/lib/csv";
import { profileSummary, type Profile } from "@/components/EbaySettings";
import { liveText, locationCode, money, setLine, thumbUrl, type CardPage, type CardRow } from "@/lib/inventory";

/** FireCardManager's Bulk Lister on the web: only the cards sent from the
 *  Card Editor, one set of choices for the run, and the same platforms. */

type Platform = "ebay" | "slabs" | "both" | "shopify" | "whatnot";
type EbayState = { on: boolean; connected: boolean; missing: string[]; profiles: Profile[]; templates: { id: string; name: string; is_default: boolean }[] };

const PLATFORMS: [Platform, string][] = [
  ["ebay", "eBay"],
  ["slabs", "Slabs & Singles"],
  ["both", "eBay and Slabs & Singles"],
  ["shopify", "Shopify (CSV export)"],
  ["whatnot", "Whatnot (CSV export)"],
];

// The desktop's return choices (same eBay policy names).
const RETURNS: [string, string][] = [
  ["", "As set on the Account page"],
  ["NoReturns", "No Returns Accepted"],
  ["14D-BuyerPays", "14 Day Returns, Buyer Pays Return Shipping"],
  ["14D-FreeReturns", "14 Day Returns, Free Returns (You Pay Return Shipping)"],
  ["30D-BuyerPays", "30 Day Returns, Buyer Pays Return Shipping"],
  ["30D-FreeReturns", "30 Day Returns, Free Returns (You Pay Return Shipping)"],
  ["60D-BuyerPays", "60 Day Returns, Buyer Pays Return Shipping"],
  ["60D-FreeReturns", "60 Day Returns, Free Returns (You Pay Return Shipping)"],
];

const MEMORY = "fcm-bulk-lister";
const EXPORT_SAVED = "fcm-export-options";

type Choices = {
  platform: Platform;
  template: string; profile: string; returns: string; category: string; fallback: string;
  auction: boolean; duration: number; offers: boolean; offerDollars: boolean; accept: string; decline: string;
  startBid: string; reserve: string; schedule: boolean; date: string; time: string;
  sTemplate: string; sAuction: boolean; sDuration: number; sStart: string; sBin: boolean; sOffers: boolean; sAccept: string; sDecline: string;
};

const num = (s: string) => (s.trim() === "" ? null : Number(s.replace(/[$,%\s]/g, "")));
const bad = (s: string) => s.trim() !== "" && !(Number(s.replace(/[$,%\s]/g, "")) >= 0);

/** Why a card can't go on the chosen platform (the apps' own checks). */
function problems(c: CardRow, ch: Choices): string[] {
  const d = c.data;
  const p: string[] = [];
  if (d.status === "Sold") return ["sold"];
  if (!d.sku) p.push("not synced from your PC yet");
  if (!d.player_name || d.player_name.startsWith("(unidentified")) p.push("not identified");
  if (ch.platform === "shopify" || ch.platform === "whatnot") return p;
  if (!d.front_photo_id) p.push("no photo");
  const ebay = ch.platform === "ebay" || ch.platform === "both";
  const slabs = ch.platform === "slabs" || ch.platform === "both";
  const price = !!(d.sale_price && d.sale_price > 0);
  const start = !!(d.start_bid && d.start_bid > 0);
  if (ebay) {
    if (!ch.auction && !price && !(num(ch.fallback) ?? 0)) p.push("no price");
    if (ch.auction && !start && !(num(ch.startBid) ?? 0)) p.push("no starting bid");
  }
  if (slabs) {
    if (!d.set_name) p.push("no set");
    if (!ch.sAuction && !price && !(ebay && !ch.auction && (num(ch.fallback) ?? 0))) p.push("no price");
    if (ch.sAuction && !start && !(num(ch.sStart) ?? 0)) p.push("no starting bid");
  }
  return [...new Set(p)];
}

export default function BulkLister({ edition, hasKey, ebay, initialIds = [] }: {
  edition: string; hasKey: boolean; ebay: EbayState; initialIds?: string[];
}) {
  const router = useRouter();
  const tcg = edition === "tcg";
  const defTemplate = ebay.templates.find((t) => t.is_default)?.id ?? ebay.templates[0]?.id ?? "";
  const [ch, setCh] = useState<Choices>({
    platform: ebay.on ? "ebay" : "slabs",
    template: defTemplate, profile: ebay.profiles[0]?.id ?? "", returns: "", category: "", fallback: "",
    auction: false, duration: 7, offers: false, offerDollars: false, accept: "", decline: "",
    startBid: "", reserve: "", schedule: false, date: "", time: "9:00 AM",
    sTemplate: "", sAuction: false, sDuration: 7, sStart: "", sBin: false, sOffers: false, sAccept: "90", sDecline: "70",
  });
  const set = (v: Partial<Choices>) => setCh((c) => ({ ...c, ...v }));
  const [ready, setReady] = useState(false);

  // Remember the run's choices in this browser, like the desktop does.
  useEffect(() => {
    try {
      const m = JSON.parse(localStorage.getItem(MEMORY) || "{}") as Partial<Choices>;
      const ok: Partial<Choices> = { ...m, schedule: false, date: "" };
      if (m.platform && m.platform !== "slabs" && m.platform !== "shopify" && m.platform !== "whatnot" && !ebay.on) ok.platform = "slabs";
      if (m.profile && !ebay.profiles.some((p) => p.id === m.profile)) delete ok.profile;
      if (m.template && !ebay.templates.some((t) => t.id === m.template)) delete ok.template;
      if (m.sTemplate && !ebay.templates.some((t) => t.id === m.sTemplate)) delete ok.sTemplate;
      setCh((c) => ({ ...c, ...ok }));
    } catch {}
    setReady(true);
  }, [ebay]);
  useEffect(() => {
    if (!ready) return;
    try { localStorage.setItem(MEMORY, JSON.stringify(ch)); } catch {}
  }, [ch, ready]);

  // The cards sent from the Card Editor.
  const [cards, setCards] = useState<CardRow[] | null>(initialIds.length ? null : []);
  const [error, setError] = useState("");
  const idKey = initialIds.join(",");
  useEffect(() => {
    const ids = idKey ? idKey.split(",") : [];
    if (!ids.length) return;
    let stop = false;
    (async () => {
      const out: CardRow[] = [];
      for (let page = 1; page <= 5; page++) {
        const p = new URLSearchParams({ edition, sort: "slot", per: "120", page: String(page), ids: idKey });
        const r = await send<CardPage>("GET", "/v1/inventory/cards?" + p.toString());
        if (!r.ok) { if (!stop) setError(r.message); break; }
        out.push(...r.data.cards);
        if (out.length >= r.data.total || r.data.cards.length === 0) break;
      }
      if (stop) return;
      const order = new Map(ids.map((id, i) => [id, i]));
      out.sort((a, b) => (order.get(a.id) ?? 0) - (order.get(b.id) ?? 0));
      setCards(out);
    })();
    return () => { stop = true; };
  }, [edition, idKey]);

  const probs = useMemo(() => new Map((cards ?? []).map((c) => [c.id, problems(c, ch)])), [cards, ch]);
  const listable = (cards ?? []).filter((c) => probs.get(c.id)!.length === 0).map((c) => c.id);
  const [picked, setPicked] = useState<Set<string> | null>(null); // null: every card that can be listed
  const chosen = (picked ? [...picked] : listable).filter((id) => probs.get(id)?.length === 0);
  function toggle(id: string) {
    const next = new Set(picked ?? listable);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    setPicked(next);
  }

  // Shopify / Whatnot, the same settings as Import/Export.
  const [shopify, setShopify] = useState({ vendor: "", product_type: "Trading Card", tags: "trading cards", active: false });
  const [whatnot, setWhatnot] = useState({ category: tcg ? "Trading Card Games" : "Sports Cards", sub_category: "", shipping_profile: "", auction: false, offerable: true });
  useEffect(() => {
    try {
      const s = JSON.parse(localStorage.getItem(EXPORT_SAVED) || "{}");
      if (s.shopify) setShopify((v) => ({ ...v, ...s.shopify }));
      if (s.whatnot?.[edition]) setWhatnot((v) => ({ ...v, ...s.whatnot[edition] }));
    } catch {}
  }, [edition]);

  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState("");
  const usesEbay = ch.platform === "ebay" || ch.platform === "both";
  const usesSlabs = ch.platform === "slabs" || ch.platform === "both";
  const csv = ch.platform === "shopify" || ch.platform === "whatnot";
  const blocker = usesEbay && !ebay.on ? "Listing on eBay from the website isn't switched on yet."
    : usesEbay && !ebay.connected ? "connect" : usesEbay && ebay.missing.length ? "missing"
    : usesSlabs && !hasKey ? "slabs" : "";

  function startAt(): string | null | false {
    if (!ch.schedule) return null;
    const m = ch.time.trim().match(/^(\d{1,2})(?::(\d{2}))?\s*(am|pm)?$/i);
    if (!ch.date || !m) return false;
    let h = Number(m[1]) % 12;
    if (m[3]?.toLowerCase() === "pm") h += 12;
    if (!m[3] && Number(m[1]) >= 12) h = Number(m[1]);
    const d = new Date(`${ch.date}T00:00:00`);
    d.setHours(h, Number(m[2] ?? 0), 0, 0);
    if (isNaN(d.getTime()) || d.getTime() < Date.now() + 5 * 60_000) return false;
    return d.toISOString();
  }

  async function run() {
    setError("");
    setNote("");
    if (chosen.length === 0) return setError("Check at least one card that can be listed.");
    if (csv) return exportCsv();
    for (const v of [ch.fallback, ch.startBid, ch.reserve, ch.accept, ch.decline, ch.sStart, ch.sAccept, ch.sDecline])
      if (bad(v)) return setError(`"${v}" isn't a number.`);
    const start = startAt();
    if (usesEbay && start === false) return setError("Pick a start date and time at least a few minutes from now (like 9:00 AM).");
    if (usesEbay && ch.category.trim() && !/^\d+$/.test(ch.category.trim())) return setError("The eBay category override is a number (blank picks it by sport).");
    const ebayOpts = {
      auction: ch.auction, duration_days: ch.duration, default_start_bid: num(ch.startBid), reserve_price: num(ch.reserve),
      buy_it_now_at_sale_price: false,
      allow_offers: !ch.auction && ch.offers, offer_dollars: ch.offerDollars,
      accept_percent: ch.offers && !ch.offerDollars ? num(ch.accept) : null, decline_percent: ch.offers && !ch.offerDollars ? num(ch.decline) : null,
      accept_amount: ch.offers && ch.offerDollars ? num(ch.accept) : null, decline_amount: ch.offers && ch.offerDollars ? num(ch.decline) : null,
      shipping_profile: ch.profile, template: ch.template, returns: ch.returns || undefined,
      category_id: ch.category.trim() || undefined, fallback_price: num(ch.fallback), start_at: start || undefined,
    };
    const slabsOpts = {
      auction: ch.sAuction, duration_days: ch.sDuration, default_start_bid: num(ch.sStart), buy_it_now_at_sale_price: ch.sBin,
      allow_offers: !ch.sAuction && ch.sOffers, accept_percent: ch.sOffers ? num(ch.sAccept) : null, decline_percent: ch.sOffers ? num(ch.sDecline) : null,
      slabs_template: ch.sTemplate || undefined,
    };
    const options = ch.platform === "ebay" ? ebayOpts : ch.platform === "slabs" ? slabsOpts : { ...ebayOpts, slabs: slabsOpts };
    setBusy(true);
    const r = await send<{ id: string }>("POST", "/v1/lister/jobs", { edition, market: ch.platform, card_ids: chosen, options });
    setBusy(false);
    if (!r.ok) return setError(r.message);
    router.push(`/lister/runs/${r.data.id}`);
  }

  async function exportCsv() {
    const kind = ch.platform as "shopify" | "whatnot";
    if (kind === "whatnot" && (!whatnot.category.trim() || !whatnot.shipping_profile.trim()))
      return setError("Fill in Whatnot's Category and Shipping profile exactly as they appear in Whatnot's bulk upload template.");
    try {
      const saved = JSON.parse(localStorage.getItem(EXPORT_SAVED) || "{}");
      localStorage.setItem(EXPORT_SAVED, JSON.stringify({ ...saved, shopify, whatnot: { ...(saved.whatnot ?? {}), [edition]: whatnot } }));
    } catch {}
    setBusy(true);
    const r = await send<{ file_name: string; csv: string; exported: number; skipped: number }>("POST", "/v1/export", {
      edition, kind, card_ids: chosen, shopify, whatnot,
    });
    setBusy(false);
    if (!r.ok) return setError(r.message);
    if (r.data.exported > 0) download(r.data.file_name, r.data.csv);
    setNote(`${r.data.exported} card${r.data.exported === 1 ? "" : "s"} exported${r.data.skipped ? `, ${r.data.skipped} skipped` : ""}. Upload the file on ${kind === "shopify" ? "Shopify" : "Whatnot"}.`);
  }

  const labels: Record<Platform, string> = { ebay: "List on eBay", slabs: "List on Slabs & Singles", both: "List on eBay and Slabs & Singles", shopify: "Export Shopify CSV", whatnot: "Export Whatnot CSV" };
  const runLabel = labels[ch.platform];
  const modeText = usesEbay ? "Listing to eBay - every checked card below becomes a real, public listing."
    : usesSlabs ? "Listing to Slabs & Singles - checked cards become live listings on slabsnsingles.com."
    : `Makes a file to upload yourself - nothing is listed until you import it on ${ch.platform === "shopify" ? "Shopify" : "Whatnot"}.`;

  if (cards && cards.length === 0) {
    return (
      <div className="card mx-auto max-w-[560px] py-10 text-center">
        <p className="text-[18px] font-bold">No cards sent yet</p>
        <p className="mt-2 text-[14px] text-muted">
          In the Card Editor, check the cards for one listing run - all raw, all graded or all oversize - and click <b>Send to Bulk Lister</b>.
        </p>
        <Link href={`/editor?edition=${edition}`} className="btn mt-5 inline-block">Open Card Editor</Link>
        {error && <p role="alert" className="mt-3 text-[13px] text-bad">{error}</p>}
      </div>
    );
  }

  const box = "rounded-lg border border-line p-3 space-y-3";
  const lbl = "block text-[12px] font-semibold";
  return (
    <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_400px]">
      <section className="min-w-0 space-y-3">
        <div className="flex flex-wrap items-end gap-3">
          <div className="min-w-0 flex-1">
            <p className="text-[15px] font-bold">Cards to list</p>
            <p className="text-[12px] text-muted">Sent from the Card Editor. Cards missing a photo, identity or price are greyed out and can&apos;t be listed.</p>
          </div>
          <button type="button" className="btn-ghost !py-1.5" onClick={() => setPicked(null)}>Select All</button>
          <button type="button" className="btn-ghost !py-1.5" onClick={() => setPicked(new Set())}>Select None</button>
        </div>
        <p className="num text-[13px] text-muted">
          {cards ? `${chosen.length} of ${cards.length} checked` : "Loading..."}
          {" · "}<Link href={`/editor?edition=${edition}`} className="font-semibold text-link">Back to the Card Editor</Link>
        </p>
        <ul className="divide-y divide-line rounded-xl border border-line">
          {!cards && <li className="p-4 text-muted">Loading...</li>}
          {cards?.map((c) => {
            const p = probs.get(c.id) ?? [];
            const can = p.length === 0;
            const on = can && (picked ? picked.has(c.id) : true);
            const live = liveText(c.data);
            const src = c.thumb_url || thumbUrl(c.data);
            return (
              <li key={c.id}>
                <label className={`flex items-center gap-3 p-2 ${can ? "cursor-pointer" : "opacity-50"} ${on ? "bg-wash" : ""}`}>
                  <input type="checkbox" checked={on} disabled={!can} onChange={() => toggle(c.id)} />
                  {src ? (
                    // eslint-disable-next-line @next/next/no-img-element
                    <img src={src} alt="" loading="lazy" className="h-20 w-[57px] shrink-0 rounded object-contain lg:h-24 lg:w-[68px]" />
                  ) : <div className="h-20 w-[57px] shrink-0 rounded bg-wash lg:h-24 lg:w-[68px]" />}
                  <div className="min-w-0 flex-1">
                    <p className="truncate font-semibold">{!can && <span className="text-bad">⚠ </span>}{c.data.player_name}</p>
                    <p className="truncate text-[12px] text-muted">{setLine(c.data)}</p>
                    <p className="truncate text-[11px] text-muted">
                      {c.data.sku} · {locationCode(c)}
                      {live && <span className="text-go"> · {live} (will update)</span>}
                      {!can && <span className="text-bad"> · {p.join(", ")}</span>}
                    </p>
                  </div>
                  <span className="num shrink-0 font-bold">{money(c.data.sale_price) || "—"}</span>
                </label>
              </li>
            );
          })}
        </ul>
      </section>

      <aside className="space-y-3 lg:sticky lg:top-4 lg:self-start">
        <div className="card space-y-4">
          <p className={`text-[12px] font-bold ${usesEbay ? "text-fire" : ""}`}>{modeText}</p>
          <label className="block">
            <span className="text-[13px] font-extrabold uppercase tracking-wide text-fire">Platform</span>
            <span className="block text-[11px] text-muted">Where these cards go - the options below change to match.</span>
            <select className="field font-semibold" value={ch.platform} onChange={(e) => set({ platform: e.target.value as Platform })}>
              {PLATFORMS.map(([v, l]) => <option key={v} value={v} disabled={(v === "ebay" || v === "both") && !ebay.on}>{l}{(v === "ebay" || v === "both") && !ebay.on ? " (coming soon)" : ""}</option>)}
            </select>
          </label>

          <div className="max-h-[62vh] space-y-3 overflow-y-auto pr-1 text-[13px]">
            {usesEbay && (
              <>
                <p className="text-[12px] font-semibold text-muted">eBay - applies to every checked card:</p>
                <div className={box}>
                  <p className="text-[12px] font-bold">Listing details</p>
                  <label className={lbl}>Template
                    <select className="field" value={ch.template} onChange={(e) => set({ template: e.target.value })}>
                      {ebay.templates.length === 0 && <option value="">Store Branded (FireCardManager&apos;s default)</option>}
                      {ebay.templates.map((t) => <option key={t.id} value={t.id}>{t.is_default ? "★ " : ""}{t.name}</option>)}
                    </select>
                  </label>
                </div>
                <div className={box}>
                  <p className="text-[12px] font-bold">Shipping &amp; returns</p>
                  <label className={lbl}>Shipping profile
                    <select className="field" value={ch.profile} onChange={(e) => set({ profile: e.target.value })}>
                      {ebay.profiles.map((p) => <option key={p.id} value={p.id}>{profileSummary(p)}</option>)}
                    </select>
                    <a href="/account#ebay" className="text-[11px] font-normal text-link">Edit profiles</a>
                  </label>
                  <p className="text-[11px] text-fire">⚠ One shipping profile per run - don&apos;t mix raw, graded and oversize cards. Send only one type from the Card Editor, then pick the matching profile here.</p>
                  <label className={lbl}>Returns
                    <select className="field" value={ch.returns} onChange={(e) => set({ returns: e.target.value })}>
                      {RETURNS.map(([v, l]) => <option key={v} value={v}>{l}</option>)}
                    </select>
                  </label>
                </div>
                <div className={box}>
                  <p className="text-[12px] font-bold">Pricing defaults</p>
                  <div className="grid grid-cols-2 gap-2">
                    <label className={lbl}>Category ID override
                      <input className="field num" inputMode="numeric" value={ch.category} onChange={(e) => set({ category: e.target.value })} placeholder="auto by sport" />
                    </label>
                    <label className={lbl}>Default Sale Price
                      <input className="field num" inputMode="decimal" value={ch.fallback} onChange={(e) => set({ fallback: e.target.value })} placeholder="optional" />
                      <span className="text-[11px] font-normal text-muted">for cards with none set</span>
                    </label>
                  </div>
                </div>
                <div className={box}>
                  <p className="text-[12px] font-bold">Listing format</p>
                  <div className="flex gap-4">
                    <label className="flex items-center gap-2"><input type="radio" checked={!ch.auction} onChange={() => set({ auction: false })} /> Fixed Price</label>
                    <label className="flex items-center gap-2"><input type="radio" checked={ch.auction} onChange={() => set({ auction: true })} /> Auction</label>
                  </div>
                  <label className={lbl}>Duration
                    <select className="field" value={ch.auction ? ch.duration : 0} disabled={!ch.auction} onChange={(e) => set({ duration: Number(e.target.value) })}>
                      {ch.auction ? [1, 3, 5, 7, 10].map((d) => <option key={d} value={d}>{d} day{d === 1 ? "" : "s"}</option>) : <option value={0}>Good &apos;Til Cancelled</option>}
                    </select>
                  </label>
                  {!ch.auction ? (
                    <div className="space-y-2">
                      <label className="flex items-center gap-2 font-semibold"><input type="checkbox" checked={ch.offers} onChange={(e) => set({ offers: e.target.checked })} /> Allow buyers to send offers</label>
                      {ch.offers && (
                        <>
                          <div className="flex gap-4 text-[12px]">
                            <span className="font-semibold">Threshold type:</span>
                            <label className="flex items-center gap-1"><input type="radio" checked={!ch.offerDollars} onChange={() => set({ offerDollars: false })} /> % of price</label>
                            <label className="flex items-center gap-1"><input type="radio" checked={ch.offerDollars} onChange={() => set({ offerDollars: true })} /> $ amount</label>
                          </div>
                          <div className="grid grid-cols-2 gap-2">
                            <label className={lbl}>Minimum offer ({ch.offerDollars ? "$" : "%"}, auto-decline below)
                              <input className="field num" inputMode="decimal" value={ch.decline} onChange={(e) => set({ decline: e.target.value })} />
                            </label>
                            <label className={lbl}>Auto accept at ({ch.offerDollars ? "$" : "%"})
                              <input className="field num" inputMode="decimal" value={ch.accept} onChange={(e) => set({ accept: e.target.value })} />
                            </label>
                          </div>
                        </>
                      )}
                    </div>
                  ) : (
                    <div className="grid grid-cols-2 gap-2">
                      <label className={lbl}>Starting bid ($)
                        <input className="field num" inputMode="decimal" value={ch.startBid} onChange={(e) => set({ startBid: e.target.value })} placeholder="card's Start Bid" />
                      </label>
                      <label className={lbl}>Reserve price ($)
                        <input className="field num" inputMode="decimal" value={ch.reserve} onChange={(e) => set({ reserve: e.target.value })} placeholder="optional" />
                      </label>
                      <p className="col-span-2 text-[11px] text-muted">Used for cards that don&apos;t have their own Start Bid or Reserve.</p>
                    </div>
                  )}
                </div>
                <div className={box}>
                  <p className="text-[12px] font-bold">Listing Schedule</p>
                  <div className="flex flex-wrap gap-4">
                    <label className="flex items-center gap-2"><input type="radio" checked={!ch.schedule} onChange={() => set({ schedule: false })} /> Make live immediately</label>
                    <label className="flex items-center gap-2"><input type="radio" checked={ch.schedule} onChange={() => set({ schedule: true })} /> Schedule start time</label>
                  </div>
                  {ch.schedule && (
                    <div className="grid grid-cols-2 gap-2">
                      <label className={lbl}>Start date<input type="date" className="field" value={ch.date} onChange={(e) => set({ date: e.target.value })} /></label>
                      <label className={lbl}>Start time (local)<input className="field" value={ch.time} onChange={(e) => set({ time: e.target.value })} /></label>
                    </div>
                  )}
                </div>
              </>
            )}

            {usesSlabs && (
              <div className={box}>
                <p className="text-[12px] font-bold">Slabs &amp; Singles</p>
                <label className={lbl}>Listing template
                  <select className="field" value={ch.sTemplate} onChange={(e) => set({ sTemplate: e.target.value })}>
                    <option value="">(Standard Slabs &amp; Singles description)</option>
                    {ebay.templates.map((t) => <option key={t.id} value={t.id}>{t.is_default ? "★ " : ""}{t.name}</option>)}
                  </select>
                  <span className="text-[11px] font-normal text-muted">Title and description, shown as plain text on the site.</span>
                </label>
                <div className="flex gap-4">
                  <label className="flex items-center gap-2"><input type="radio" checked={!ch.sAuction} onChange={() => set({ sAuction: false })} /> Fixed price</label>
                  <label className="flex items-center gap-2"><input type="radio" checked={ch.sAuction} onChange={() => set({ sAuction: true })} /> Auction</label>
                </div>
                {ch.sAuction ? (
                  <div className="space-y-2">
                    <div className="grid grid-cols-2 gap-2">
                      <label className={lbl}>Length
                        <select className="field" value={ch.sDuration} onChange={(e) => set({ sDuration: Number(e.target.value) })}>
                          {[5, 7, 10].map((d) => <option key={d} value={d}>{d} days</option>)}
                        </select>
                      </label>
                      <label className={lbl}>Starting bid when a card has none ($)
                        <input className="field num" inputMode="decimal" value={ch.sStart} onChange={(e) => set({ sStart: e.target.value })} />
                      </label>
                    </div>
                    <label className="flex items-center gap-2"><input type="checkbox" checked={ch.sBin} onChange={(e) => set({ sBin: e.target.checked })} /> Buy It Now at the card&apos;s Sale Price</label>
                  </div>
                ) : (
                  <div className="space-y-2">
                    <label className="flex items-center gap-2 font-semibold"><input type="checkbox" checked={ch.sOffers} onChange={(e) => set({ sOffers: e.target.checked })} /> Allow offers</label>
                    {ch.sOffers && (
                      <div className="grid grid-cols-2 gap-2">
                        <label className={lbl}>Auto-accept at (%)<input className="field num" inputMode="numeric" value={ch.sAccept} onChange={(e) => set({ sAccept: e.target.value })} /></label>
                        <label className={lbl}>Auto-decline below (%)<input className="field num" inputMode="numeric" value={ch.sDecline} onChange={(e) => set({ sDecline: e.target.value })} /></label>
                      </div>
                    )}
                  </div>
                )}
                <p className="text-[11px] text-muted">Shipping is included in the price and picked per order, using your package and weight settings from FireCardManager. Cards list under the same SKU as eBay, so a sale on either one takes the card off the other.</p>
              </div>
            )}

            {ch.platform === "shopify" && (
              <div className={box}>
                <p className="text-[12px] font-bold">Shopify product import</p>
                <label className={lbl}>Vendor (your store or brand)<input className="field" value={shopify.vendor} onChange={(e) => setShopify({ ...shopify, vendor: e.target.value })} /></label>
                <label className={lbl}>Product type<input className="field" value={shopify.product_type} onChange={(e) => setShopify({ ...shopify, product_type: e.target.value })} /></label>
                <label className={lbl}>Tags (comma separated)<input className="field" value={shopify.tags} onChange={(e) => setShopify({ ...shopify, tags: e.target.value })} /></label>
                <label className="flex items-center gap-2"><input type="checkbox" checked={shopify.active} onChange={(e) => setShopify({ ...shopify, active: e.target.checked })} /> Publish as active (unticked: import as drafts)</label>
                <p className="text-[11px] text-muted">In Shopify: Products &gt; Import &gt; choose this file. Photos are linked from FireCardManager Web.</p>
              </div>
            )}
            {ch.platform === "whatnot" && (
              <div className={box}>
                <p className="text-[12px] font-bold">Whatnot bulk upload</p>
                <label className={lbl}>Category<input className="field" value={whatnot.category} onChange={(e) => setWhatnot({ ...whatnot, category: e.target.value })} /></label>
                <label className={lbl}>Sub category (blank = by card category)<input className="field" value={whatnot.sub_category} onChange={(e) => setWhatnot({ ...whatnot, sub_category: e.target.value })} /></label>
                <label className={lbl}>Shipping profile<input className="field" value={whatnot.shipping_profile} onChange={(e) => setWhatnot({ ...whatnot, shipping_profile: e.target.value })} /></label>
                <div className="flex flex-wrap gap-4">
                  <label className="flex items-center gap-2"><input type="radio" checked={!whatnot.auction} onChange={() => setWhatnot({ ...whatnot, auction: false })} /> Buy it Now</label>
                  <label className="flex items-center gap-2"><input type="radio" checked={whatnot.auction} onChange={() => setWhatnot({ ...whatnot, auction: true })} /> Auction</label>
                  <label className="flex items-center gap-2"><input type="checkbox" checked={whatnot.offerable} onChange={(e) => setWhatnot({ ...whatnot, offerable: e.target.checked })} /> Accept offers</label>
                </div>
                <p className="text-[11px] text-muted">Category, sub category and shipping profile must match the Values tab of Whatnot&apos;s bulk upload template. Uploads become drafts in Whatnot.</p>
              </div>
            )}
          </div>

          {blocker === "connect" && <p className="rounded-lg border border-fire px-3 py-2 text-[13px]">Connect eBay on the <a href="/account#ebay" className="font-semibold text-fire">Account page</a> first.</p>}
          {blocker === "missing" && <p className="rounded-lg border border-fire px-3 py-2 text-[13px]">Pick your eBay {ebay.missing.join(", ")} on the <a href="/account#ebay" className="font-semibold text-fire">Account page</a> first.</p>}
          {blocker === "slabs" && <p className="rounded-lg border border-fire px-3 py-2 text-[13px]">Add a Slabs &amp; Singles key made <b>for FireCardManager Web</b> on the <a href="/account#market-keys" className="font-semibold text-fire">Account page</a> first.</p>}
          {blocker && !["connect", "missing", "slabs"].includes(blocker) && <p className="text-[13px] text-bad">{blocker}</p>}
          {error && <p role="alert" className="text-[13px] text-bad">{error}</p>}
          {note && <p className="text-[13px] text-go">{note}</p>}
          <button type="button" className="btn w-full" disabled={busy || !cards || chosen.length === 0 || (!csv && !!blocker)} onClick={run}>
            {busy ? "Working..." : `${runLabel} (${chosen.length})`}
          </button>
          {usesEbay && !csv && <p className="text-[11px] text-muted">Cards already live are updated. Your PC shows them live after its next sync.</p>}
        </div>
      </aside>
    </div>
  );
}
