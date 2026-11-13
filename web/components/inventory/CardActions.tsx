"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { send } from "@/lib/client";
import { liveText, money, type CardDetail, type CardData } from "@/lib/inventory";

type Mode = "" | "edit" | "sold";

const SPORTS_FLAGS: [keyof CardData, string][] = [
  ["is_rookie", "Rookie (RC)"], ["is_autograph", "Autograph"], ["is_memorabilia", "Memorabilia / relic"],
  ["is_short_print", "Short print"], ["is_error_variant", "Error variant"], ["is_blank_back", "Blank back"],
];
const TCG_FLAGS: [keyof CardData, string][] = [["is_chase", "Chase"], ["is_foil", "Foil"], ["is_sketch_card", "Sketch card"]];

function Field({ label, error, children, wide }: { label: string; error?: string; children: React.ReactNode; wide?: boolean }) {
  return (
    <label className={`block text-[13px] font-semibold ${wide ? "sm:col-span-2" : ""}`}>
      {label}
      {children}
      {error && <span className="mt-1 block font-normal text-bad">{error}</span>}
    </label>
  );
}

function todayLocal(): string {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

export default function CardActions({ card }: { card: CardDetail }) {
  const router = useRouter();
  const d = card.data;
  const sold = d.status === "Sold";
  const live = d.status === "EBA" || d.status === "EBF";
  const tcg = card.edition === "tcg";
  const [mode, setMode] = useState<Mode>("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [notice, setNotice] = useState("");

  // ---- edit form state: every editable field as text/bool ----
  const initial = {
    player_name: d.player_name ?? "", team: d.team ?? "", year: d.year ? String(d.year) : "", season_end: d.season_end ? String(d.season_end) : "",
    brand: d.brand ?? "", set_name: d.set_name ?? "", card_number: d.card_number ?? "", variety: d.variety ?? "", condition: d.condition ?? "",
    grading_company: d.grading_company ?? "", card_grade: d.card_grade ?? "", grading_cert_number: d.grading_cert_number ?? "", serial_number: d.serial_number ?? "",
    estimated_value: d.estimated_value != null ? String(d.estimated_value) : "", sale_price: d.sale_price != null ? String(d.sale_price) : "",
    paid_price: d.paid_price != null ? String(d.paid_price) : "", status: d.status ?? "Ready",
  };
  const [f, setF] = useState(initial);
  const flagList = [...SPORTS_FLAGS, ...(tcg ? TCG_FLAGS : [])];
  const initialFlags = Object.fromEntries([...flagList.map(([k]) => [k, !!d[k]]), ["allow_offers", !!d.allow_offers]]) as Record<string, boolean>;
  const [flags, setFlags] = useState(initialFlags);
  const set = (k: keyof typeof initial) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) => setF({ ...f, [k]: e.target.value });

  // ---- sale form ----
  const [sale, setSale] = useState({
    price: d.sale_price != null ? String(d.sale_price) : d.estimated_value != null ? String(d.estimated_value) : "",
    shipping: "0", date: todayLocal(), where: "in_person", buyer: "", notes: "",
  });

  function open(m: Mode) {
    setMode(m);
    setError("");
    setFieldErrors({});
    setNotice("");
    if (m === "edit") {
      setF(initial);
      setFlags(initialFlags);
    }
  }

  async function saveEdit(e: React.FormEvent) {
    e.preventDefault();
    const text = ["player_name", "team", "brand", "set_name", "card_number", "variety", "condition", "grading_company", "card_grade",
      "grading_cert_number", "serial_number"] as const;
    const nums = ["year", "season_end", "estimated_value", "sale_price", "paid_price"] as const;
    const fields: Record<string, unknown> = {};
    const bad: Record<string, string> = {};
    for (const k of text) if (f[k] !== initial[k]) fields[k] = f[k].trim() === "" ? null : f[k];
    for (const k of nums) {
      if (f[k] === initial[k]) continue;
      const s = f[k].replace(/[$,\s]/g, "");
      if (s === "") fields[k] = null;
      else if (Number.isNaN(Number(s))) bad[k] = "Enter a number.";
      else fields[k] = Number(s);
    }
    if (f.status !== initial.status) fields.status = f.status;
    for (const [k, v] of Object.entries(flags)) if (v !== initialFlags[k]) fields[k] = v;
    if (Object.keys(bad).length) return setFieldErrors(bad);
    if (Object.keys(fields).length === 0) return setMode("");

    setBusy(true);
    setError("");
    const r = await send<{ lost_fields?: string[] }>("PATCH", `/v1/inventory/cards/${card.id}`, { base_version: card.version, fields });
    setBusy(false);
    if (!r.ok) {
      setError(r.message);
      setFieldErrors(r.fields);
      return;
    }
    setMode("");
    setNotice(
      r.data.lost_fields?.length
        ? "Saved. Some fields had also changed on a PC; your edit won and the other value is on the Conflicts page."
        : "Saved. Your PC picks this up on its next sync (within 2 minutes while FireCardManager is open).",
    );
    router.refresh();
  }

  async function saveSale(e: React.FormEvent) {
    e.preventDefault();
    const price = Number(sale.price.replace(/[$,\s]/g, ""));
    const shipping = Number(sale.shipping.replace(/[$,\s]/g, "") || "0");
    if (sale.price.trim() === "" || Number.isNaN(price)) return setFieldErrors({ sale_price: "Enter the amount it sold for." });
    if (Number.isNaN(shipping)) return setFieldErrors({ shipping_charged: "Enter a number, or 0." });
    // The sale date at noon local time, so it stays on that day in any time zone shift.
    const [y, m, dd] = sale.date.split("-").map(Number);
    const date = new Date(y, m - 1, dd, 12).toISOString();
    setBusy(true);
    setError("");
    const r = await send("POST", `/v1/inventory/cards/${card.id}/sold`, {
      base_version: card.version, sale_price: price, shipping_charged: shipping, sale_date: date, where: sale.where, buyer: sale.buyer, notes: sale.notes,
    });
    setBusy(false);
    if (!r.ok) {
      setError(r.message);
      setFieldErrors(r.fields);
      return;
    }
    setMode("");
    setNotice("Marked sold and added to your Sold Log. Your PC moves it out of inventory on its next sync.");
    router.refresh();
  }

  const endsEbay = live && sale.where !== "ebay";
  const endsSlabs = !!d.marketplace_listing_id && sale.where !== "slabs";

  if (sold) {
    return (
      <p className="card !p-3 text-[14px]">
        {notice || "Sold."} See your <a href="/sold" className="font-semibold text-fire">Sold Log</a>.
      </p>
    );
  }

  return (
    <div>
      {notice && <p role="status" className="mb-3 rounded-lg border border-go px-3 py-2 text-[14px]">{notice}</p>}
      {mode === "" && (
        <div className="flex flex-wrap gap-2">
          <button type="button" className="btn" onClick={() => open("edit")}>Edit card</button>
          <button type="button" className="btn-ghost" onClick={() => open("sold")}>Mark sold</button>
          <a href={`/editor?id=${card.id}&edition=${card.edition}`} className="btn-ghost hidden lg:inline-flex">Open in Card Editor</a>
        </div>
      )}

      {mode === "edit" && (
        <form onSubmit={saveEdit} className="card space-y-5">
          <div className="flex items-center justify-between gap-3">
            <h2 className="text-[18px] font-bold">Edit card</h2>
            <button type="button" className="text-[14px] font-semibold text-muted hover:text-fire" onClick={() => setMode("")}>Cancel</button>
          </div>
          {live && (
            <p className="text-[13px] text-muted">
              {liveText(d)}: changing the price here doesn&apos;t change the eBay listing. Use Revise in FireCardManager for that.
            </p>
          )}

          <fieldset className="grid gap-3 sm:grid-cols-2">
            <legend className="mb-2 text-[13px] font-semibold uppercase tracking-wide text-muted">Selling</legend>
            <Field label="Status" error={fieldErrors.status}>
              <select
                className="field"
                value={f.status}
                onChange={(e) => {
                  if (e.target.value !== "Sold") return set("status")(e);
                  // Sold opens the sale form (where it sold, price, date), like FireCardManager's Mark as Sold.
                  const typed = f.sale_price.replace(/[$,\s]/g, "");
                  if (typed) setSale((s) => ({ ...s, price: typed }));
                  setMode("sold");
                  setError("");
                  setFieldErrors({});
                }}
              >
                {live && <option value={f.status}>{liveText(d)}</option>}
                {!live && <option value="Ready">Ready to list</option>}
                {!live && <option value="NotForSale">Not for sale</option>}
                <option value="Sold">Sold...</option>
              </select>
            </Field>
            <Field label="Price" error={fieldErrors.sale_price}>
              <input className="field num" inputMode="decimal" value={f.sale_price} onChange={set("sale_price")} placeholder="$0.00" />
            </Field>
            <Field label="Estimated value" error={fieldErrors.estimated_value}>
              <input className="field num" inputMode="decimal" value={f.estimated_value} onChange={set("estimated_value")} />
            </Field>
            <Field label="What you paid" error={fieldErrors.paid_price}>
              <input className="field num" inputMode="decimal" value={f.paid_price} onChange={set("paid_price")} />
            </Field>
            <label className="flex items-center gap-2 text-[14px] sm:col-span-2">
              <input type="checkbox" checked={flags.allow_offers} onChange={(e) => setFlags({ ...flags, allow_offers: e.target.checked })} />
              Allow offers
            </label>
          </fieldset>

          <p className="text-[13px] text-muted">Box, row and slot are changed only in FireCardManager on your PC, so a card is never misplaced.</p>

          <fieldset className="grid gap-3 sm:grid-cols-2">
            <legend className="mb-2 text-[13px] font-semibold uppercase tracking-wide text-muted">Card</legend>
            <Field label={tcg ? "Character / subject name" : "Player"} error={fieldErrors.player_name} wide>
              <input className="field" value={f.player_name} onChange={set("player_name")} required />
            </Field>
            <Field label={tcg ? "Franchise" : "Team"} error={fieldErrors.team}><input className="field" value={f.team} onChange={set("team")} /></Field>
            <div className="grid grid-cols-2 gap-3">
              <Field label="Year" error={fieldErrors.year}><input className="field num" inputMode="numeric" value={f.year} onChange={set("year")} /></Field>
              <Field label="Season end" error={fieldErrors.season_end}>
                <input className="field num" inputMode="numeric" value={f.season_end} onChange={set("season_end")} placeholder="optional" />
              </Field>
            </div>
            <Field label="Brand" error={fieldErrors.brand}><input className="field" value={f.brand} onChange={set("brand")} required /></Field>
            <Field label="Set" error={fieldErrors.set_name}><input className="field" value={f.set_name} onChange={set("set_name")} required /></Field>
            <Field label="Card number" error={fieldErrors.card_number}><input className="field" value={f.card_number} onChange={set("card_number")} required /></Field>
            <Field label="Parallel / variety" error={fieldErrors.variety}><input className="field" value={f.variety} onChange={set("variety")} /></Field>
            <Field label="Condition" error={fieldErrors.condition}><input className="field" value={f.condition} onChange={set("condition")} required /></Field>
            <Field label="Serial number" error={fieldErrors.serial_number}>
              <input className="field" value={f.serial_number} onChange={set("serial_number")} placeholder="e.g. 12/99" />
            </Field>
            <Field label="Grading company" error={fieldErrors.grading_company}>
              <input className="field" value={f.grading_company} onChange={set("grading_company")} placeholder="PSA, BGS, SGC..." />
            </Field>
            <Field label="Grade" error={fieldErrors.card_grade}><input className="field" value={f.card_grade} onChange={set("card_grade")} /></Field>
            {tcg && (
              <Field label="Cert number" error={fieldErrors.grading_cert_number}>
                <input className="field" value={f.grading_cert_number} onChange={set("grading_cert_number")} />
              </Field>
            )}
            <div className="flex flex-wrap gap-x-5 gap-y-2 sm:col-span-2">
              {flagList.map(([k, label]) => (
                <label key={k} className="flex items-center gap-2 text-[14px]">
                  <input type="checkbox" checked={flags[k]} onChange={(e) => setFlags({ ...flags, [k]: e.target.checked })} />
                  {label}
                </label>
              ))}
            </div>
          </fieldset>

          {error && <p role="alert" className="text-bad">{error}</p>}
          <div className="flex gap-2">
            <button type="submit" className="btn" disabled={busy}>{busy ? "Saving..." : "Save changes"}</button>
            <button type="button" className="btn-ghost" onClick={() => setMode("")}>Cancel</button>
          </div>
        </form>
      )}

      {mode === "sold" && (
        <form onSubmit={saveSale} className="card space-y-4">
          <div className="flex items-center justify-between gap-3">
            <h2 className="text-[18px] font-bold">Mark sold</h2>
            <button type="button" className="text-[14px] font-semibold text-muted hover:text-fire" onClick={() => setMode("")}>Cancel</button>
          </div>
          <fieldset>
            <legend className="text-[13px] font-semibold">Where did it sell?</legend>
            <div className="mt-2 grid gap-2 sm:grid-cols-2">
              {[["in_person", "In person (card show, flea market)"], ["ebay", "eBay"], ["slabs", "Slabs & Singles"], ["other", "Somewhere else"]].map(([v, label]) => (
                <label key={v} className={`flex cursor-pointer items-center gap-2 rounded-lg border px-3 py-2 text-[14px] ${sale.where === v ? "border-fire" : "border-line"}`}>
                  <input type="radio" name="where" value={v} checked={sale.where === v} onChange={() => setSale({ ...sale, where: v })} />
                  {label}
                </label>
              ))}
            </div>
            {fieldErrors.where && <p className="mt-1 text-[13px] text-bad">{fieldErrors.where}</p>}
          </fieldset>
          {(endsEbay || endsSlabs) && (
            <p className="rounded-lg border border-fire px-3 py-2 text-[13px]">
              {endsEbay && <>It&apos;s still live on eBay. FireCardManager ends that listing on your PC&apos;s next sync. </>}
              {endsSlabs && <>It&apos;s listed on Slabs &amp; Singles. FireCardManager takes it down on your PC&apos;s next sync. </>}
              Make sure FireCardManager is open on your PC soon.
            </p>
          )}
          <div className="grid gap-3 sm:grid-cols-3">
            <Field label="Sold for" error={fieldErrors.sale_price}>
              <input className="field num" inputMode="decimal" value={sale.price} onChange={(e) => setSale({ ...sale, price: e.target.value })} required />
            </Field>
            <Field label="Shipping charged" error={fieldErrors.shipping_charged}>
              <input className="field num" inputMode="decimal" value={sale.shipping} onChange={(e) => setSale({ ...sale, shipping: e.target.value })} />
            </Field>
            <Field label="Date" error={fieldErrors.sale_date}>
              <input className="field" type="date" value={sale.date} max={todayLocal()} onChange={(e) => setSale({ ...sale, date: e.target.value })} required />
            </Field>
            <Field label="Buyer (optional)" error={fieldErrors.buyer}>
              <input className="field" value={sale.buyer} onChange={(e) => setSale({ ...sale, buyer: e.target.value })} />
            </Field>
            <Field label="Notes (optional)" error={fieldErrors.notes} wide>
              <input className="field" value={sale.notes} onChange={(e) => setSale({ ...sale, notes: e.target.value })} />
            </Field>
          </div>
          {d.estimated_value != null && <p className="text-[13px] text-muted">Estimated value {money(d.estimated_value)}.</p>}
          {error && <p role="alert" className="text-bad">{error}</p>}
          <div className="flex gap-2">
            <button type="submit" className="btn" disabled={busy}>{busy ? "Saving..." : "Mark sold"}</button>
            <button type="button" className="btn-ghost" onClick={() => setMode("")}>Cancel</button>
          </div>
        </form>
      )}
    </div>
  );
}
