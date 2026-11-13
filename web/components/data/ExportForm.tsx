"use client";

import { useEffect, useState } from "react";
import { send } from "@/lib/client";
import { download } from "@/lib/csv";
import type { TreeSport } from "@/lib/inventory";

type Kind = "cards" | "shopify" | "whatnot";
type Result = { file_name: string; csv: string; exported: number; skipped: number; notes?: string[] };

const SAVED = "fcm-export-options";

/** The desktop's export choices (Export Database, Shopify, Whatnot) for the cards picked here. */
export default function ExportForm({ edition, sports }: { edition: string; sports: TreeSport[] }) {
  const tcg = edition === "tcg";
  const [kind, setKind] = useState<Kind>("cards");
  const [sport, setSport] = useState("");
  const [box, setBox] = useState("");
  const [status, setStatus] = useState("");
  const [q, setQ] = useState("");
  const [shopify, setShopify] = useState({ vendor: "", product_type: "Trading Card", tags: "trading cards", active: false });
  const [whatnot, setWhatnot] = useState({ category: tcg ? "Trading Card Games" : "Sports Cards", sub_category: "", shipping_profile: "", auction: false, offerable: true });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [result, setResult] = useState<Result | null>(null);

  // Remember the Shopify and Whatnot settings in this browser, like the desktop does.
  useEffect(() => {
    try {
      const s = JSON.parse(localStorage.getItem(SAVED) || "{}");
      if (s.shopify) setShopify((v) => ({ ...v, ...s.shopify }));
      if (s.whatnot?.[edition]) setWhatnot((v) => ({ ...v, ...s.whatnot[edition] }));
    } catch {}
  }, [edition]);

  const s = sports.find((x) => x.sport === sport);

  async function run() {
    setError("");
    setResult(null);
    if (kind === "whatnot" && (!whatnot.category.trim() || !whatnot.shipping_profile.trim()))
      return setError("Fill in Whatnot's Category and Shipping profile exactly as they appear in Whatnot's bulk upload template.");
    try {
      const saved = JSON.parse(localStorage.getItem(SAVED) || "{}");
      localStorage.setItem(SAVED, JSON.stringify({ ...saved, shopify, whatnot: { ...(saved.whatnot ?? {}), [edition]: whatnot } }));
    } catch {}
    setBusy(true);
    const r = await send<Result>("POST", "/v1/export", {
      edition, kind, filter: { sport, box, status, q }, shopify, whatnot,
    });
    setBusy(false);
    if (!r.ok) return setError(r.message);
    setResult(r.data);
    if (r.data.exported > 0) download(r.data.file_name, r.data.csv);
  }

  return (
    <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_340px]">
      <section className="card space-y-4">
        <div>
          <h2 className="text-[16px] font-bold">What to export</h2>
          <div className="mt-2 grid gap-2">
            {([
              ["cards", "FireCardManager CSV", "Every field, in the desktop's Export Database columns. Photos are linked, not copied. Opens in Excel or Google Sheets."],
              ["shopify", "Shopify product import", "Products > Import in Shopify. One product per card, quantity 1, SKU = the card's SKU."],
              ["whatnot", "Whatnot bulk upload", "Seller Hub > Inventory > Bulk upload. Uploads become drafts in Whatnot."],
            ] as [Kind, string, string][]).map(([k, label, help]) => (
              <label key={k} className={`flex cursor-pointer gap-3 rounded-lg border px-3 py-2 ${kind === k ? "border-fire" : "border-line"}`}>
                <input type="radio" className="mt-1" checked={kind === k} onChange={() => setKind(k)} />
                <span>
                  <span className="block font-semibold">{label}</span>
                  <span className="block text-[12px] text-muted">{help}</span>
                </span>
              </label>
            ))}
          </div>
        </div>

        <div>
          <h2 className="text-[16px] font-bold">Which cards</h2>
          <div className="mt-2 flex flex-wrap gap-2">
            <select className="field !mt-0 !w-auto" value={sport} onChange={(e) => { setSport(e.target.value); setBox(""); }} aria-label="Category">
              <option value="">All categories</option>
              {sports.map((x) => <option key={x.sport} value={x.sport}>{x.sport} ({x.cards})</option>)}
            </select>
            {s && (
              <select className="field !mt-0 !w-auto" value={box} onChange={(e) => setBox(e.target.value)} aria-label="Box">
                <option value="">All boxes</option>
                {s.boxes.map((b) => <option key={b.id} value={b.id}>{b.label} ({b.cards})</option>)}
              </select>
            )}
            <select className="field !mt-0 !w-auto" value={status} onChange={(e) => setStatus(e.target.value)} aria-label="Status">
              <option value="">Any status</option>
              <option value="Ready">Ready to list</option>
              <option value="NotForSale">Not for sale</option>
              <option value="listed">Live on eBay</option>
            </select>
            <input className="field !mt-0 min-w-[180px] flex-1" type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search player, set, SKU..." />
          </div>
        </div>

        {kind === "shopify" && (
          <div className="grid gap-3 sm:grid-cols-3">
            <label className="block text-[13px] font-semibold">Vendor (your store or brand)
              <input className="field" value={shopify.vendor} onChange={(e) => setShopify({ ...shopify, vendor: e.target.value })} /></label>
            <label className="block text-[13px] font-semibold">Product type
              <input className="field" value={shopify.product_type} onChange={(e) => setShopify({ ...shopify, product_type: e.target.value })} /></label>
            <label className="block text-[13px] font-semibold">Tags (comma separated)
              <input className="field" value={shopify.tags} onChange={(e) => setShopify({ ...shopify, tags: e.target.value })} /></label>
            <label className="flex items-center gap-2 text-[14px] sm:col-span-3">
              <input type="checkbox" checked={shopify.active} onChange={(e) => setShopify({ ...shopify, active: e.target.checked })} />
              Publish as active (unticked: import as drafts to review first)
            </label>
          </div>
        )}

        {kind === "whatnot" && (
          <div className="grid gap-3 sm:grid-cols-3">
            <label className="block text-[13px] font-semibold">Category
              <input className="field" value={whatnot.category} onChange={(e) => setWhatnot({ ...whatnot, category: e.target.value })} /></label>
            <label className="block text-[13px] font-semibold">Sub category <span className="font-normal text-muted">(blank = by card category)</span>
              <input className="field" value={whatnot.sub_category} onChange={(e) => setWhatnot({ ...whatnot, sub_category: e.target.value })} /></label>
            <label className="block text-[13px] font-semibold">Shipping profile
              <input className="field" value={whatnot.shipping_profile} onChange={(e) => setWhatnot({ ...whatnot, shipping_profile: e.target.value })} /></label>
            <div className="flex flex-wrap gap-4 text-[14px] sm:col-span-3">
              <label className="flex items-center gap-2"><input type="radio" checked={!whatnot.auction} onChange={() => setWhatnot({ ...whatnot, auction: false })} /> Buy it Now</label>
              <label className="flex items-center gap-2"><input type="radio" checked={whatnot.auction} onChange={() => setWhatnot({ ...whatnot, auction: true })} /> Auction (price = starting bid)</label>
              {!whatnot.auction && (
                <label className="flex items-center gap-2"><input type="checkbox" checked={whatnot.offerable} onChange={(e) => setWhatnot({ ...whatnot, offerable: e.target.checked })} /> Accept offers</label>
              )}
            </div>
            <p className="text-[12px] text-muted sm:col-span-3">
              Category, sub category and shipping profile must match the Values tab of Whatnot&apos;s bulk upload template. Delete old drafts before uploading an edited file again.
            </p>
          </div>
        )}

        {error && <p role="alert" className="text-bad">{error}</p>}
        <button type="button" className="btn" disabled={busy} onClick={run}>{busy ? "Exporting..." : "Export"}</button>
      </section>

      <aside className="space-y-3">
        <div className="card text-[13px] text-muted">
          <p className="font-semibold text-ink">Photos</p>
          <p className="mt-1">
            Every file links to your card photos hosted by FireCardManager Web, so Shopify and Whatnot fetch them by themselves. Nothing to upload.
          </p>
          <p className="mt-2">
            The FireCardManager CSV has the same columns as the desktop app&apos;s export, plus SKU, prices, status and flags at the end.
          </p>
        </div>
        {result && (
          <div className="card space-y-2 text-[13px]" role="status">
            <p className="font-semibold">
              {result.exported > 0 ? `Exported ${result.exported} card${result.exported === 1 ? "" : "s"} to ${result.file_name}.` : "Nothing to export."}
              {result.skipped > 0 && ` Skipped ${result.skipped}.`}
            </p>
            {result.exported > 0 && (
              <button type="button" className="font-semibold text-link" onClick={() => download(result.file_name, result.csv)}>Download again</button>
            )}
            {result.notes && result.notes.length > 0 && (
              <ul className="max-h-60 list-disc overflow-y-auto pl-5 text-muted">
                {result.notes.map((n, i) => <li key={i}>{n}</li>)}
              </ul>
            )}
          </div>
        )}
      </aside>
    </div>
  );
}
