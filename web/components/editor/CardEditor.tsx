"use client";

import { catName } from "@/lib/cats";
import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";
import { send } from "@/lib/client";
import { CONDITIONS, makeShot, SPORTS, TCG, uploadSide } from "@/lib/addcard";
import { liveText, locationCode, money, seasonText, statusText, type CardDetail, type CardPage, type CardRow, type TreeSport } from "@/lib/inventory";
import ZoomPhoto from "./ZoomPhoto";

const PER = 100;
const PENDING = "(unidentified - pending ID)";

type Filter = { sport: string; box: string; row: string; q: string };

const TEXT = ["player_name", "team", "brand", "set_name", "card_number", "variety", "serial_number", "condition", "grading_company", "card_grade", "grading_cert_number"] as const;
type Form = Record<(typeof TEXT)[number] | "sport" | "year" | "estimated_value" | "sale_price" | "paid_price" | "status", string> & { percent: number };
const FLAGS_SPORTS: [string, string][] = [["is_rookie", "Rookie (RC)"], ["is_autograph", "Autograph"], ["is_memorabilia", "Memorabilia"],
  ["is_short_print", "Short print"], ["is_error_variant", "Error variant"], ["is_blank_back", "Blank back"]];
const FLAGS_TCG: [string, string][] = [["is_chase", "Chase"], ["is_foil", "Foil"], ["is_sketch_card", "Sketch card"]];

function toForm(c: CardDetail): { form: Form; flags: Record<string, boolean> } {
  const d = c.data as Record<string, unknown>;
  const s = (k: string) => (d[k] == null ? "" : String(d[k]));
  const form = Object.fromEntries(TEXT.map((k) => [k, s(k)])) as unknown as Form;
  form.sport = s("sport");
  form.year = seasonText(c.data) || "";
  form.estimated_value = d.estimated_value != null ? Number(d.estimated_value).toFixed(2) : "";
  form.sale_price = d.sale_price != null ? Number(d.sale_price).toFixed(2) : "";
  form.paid_price = d.paid_price != null ? Number(d.paid_price).toFixed(2) : "";
  form.status = s("status") || "Ready";
  form.percent = typeof d.sale_price_percent === "number" ? (d.sale_price_percent as number) : 25;
  if (form.player_name === PENDING) form.player_name = "";
  const flags = Object.fromEntries([...FLAGS_SPORTS, ...FLAGS_TCG].map(([k]) => [k, !!d[k]]));
  return { form, flags };
}

/** The form as changed fields for PATCH, or problems. */
function changes(c: CardDetail, form: Form, flags: Record<string, boolean>, tcg: boolean): { fields: Record<string, unknown>; bad: Record<string, string> } {
  const before = toForm(c);
  const fields: Record<string, unknown> = {};
  const bad: Record<string, string> = {};
  for (const k of TEXT) if (form[k].trim() !== before.form[k].trim()) fields[k] = form[k].trim() || null;
  if (form.player_name.trim() === "" && before.form.player_name === "") delete fields.player_name;
  if (fields.player_name === null) fields.player_name = PENDING;
  if (form.sport !== before.form.sport && form.sport) fields.sport = form.sport;
  if (form.year.trim() !== before.form.year) {
    const m = /^(\d{4})(?:\s*-\s*(\d{2,4}))?$/.exec(form.year.trim());
    if (!m) bad.year = "Enter a year like 2024 or a season like 2024-25.";
    else {
      fields.year = Number(m[1]);
      fields.season_end = m[2] ? (m[2].length === 2 ? Number(m[1].slice(0, 2) + m[2]) : Number(m[2])) : null;
    }
  }
  for (const k of ["estimated_value", "sale_price", "paid_price"] as const) {
    if (form[k].trim() === before.form[k]) continue;
    const v = form[k].replace(/[$,\s]/g, "");
    if (v === "") fields[k] = k === "estimated_value" ? 0 : null;
    else if (Number.isNaN(Number(v))) bad[k] = "Enter an amount.";
    else fields[k] = Number(v);
  }
  if (form.percent !== before.form.percent) fields.sale_price_percent = form.percent;
  if (form.status !== before.form.status) fields.status = form.status;
  for (const [k] of [...FLAGS_SPORTS, ...(tcg ? FLAGS_TCG : [])]) if (flags[k] !== before.flags[k]) fields[k] = flags[k];
  if (form.condition !== "Graded" && before.form.condition === "Graded") { fields.grading_company = null; fields.card_grade = null; }
  return { fields, bad };
}

function researchQuery(d: CardRow["data"]): string {
  return [seasonText(d), d.brand, d.set_name, d.card_number ? `#${d.card_number}` : "", d.player_name === PENDING ? "" : d.player_name].filter(Boolean).join(" ");
}

function comcUrl(d: CardRow["data"]): string {
  const parts = [d.card_number ? `23${d.card_number}` : "", d.brand, d.set_name, d.player_name === PENDING ? "" : d.player_name].filter(Boolean).join(" ");
  return "https://www.comc.com/Cards,=~" + encodeURIComponent(parts).replace(/%20/g, "+");
}

export default function CardEditor({ edition, sports, start }: { edition: string; sports: TreeSport[]; start: Filter & { id?: string } }) {
  const tcg = edition === "tcg";
  const cats = tcg ? [...TCG, "Mixed"] : SPORTS;
  const [filter, setFilter] = useState<Filter>({ sport: start.sport, box: start.box, row: start.row, q: start.q });
  const [q, setQ] = useState(start.q);
  const [page, setPage] = useState(1);
  const [list, setList] = useState<CardPage | null>(null);
  const [selId, setSelId] = useState<string | undefined>(start.id);
  const [card, setCard] = useState<CardDetail | null>(null);
  const [form, setForm] = useState<Form | null>(null);
  const [flags, setFlags] = useState<Record<string, boolean>>({});
  const [priceTyped, setPriceTyped] = useState(false);
  const [checked, setChecked] = useState<Set<string>>(new Set());
  const [bulkCat, setBulkCat] = useState("");
  const [busy, setBusy] = useState("");
  const [msg, setMsg] = useState<{ text: string; bad?: boolean } | null>(null);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const frontFile = useRef<HTMLInputElement>(null);
  const backFile = useRef<HTMLInputElement>(null);

  const loadList = useCallback(async () => {
    const p = new URLSearchParams({ edition, sort: "slot", per: String(PER), page: String(page) });
    for (const [k, v] of Object.entries(filter) as [string, string][]) if (v) p.set(k, v);
    const r = await send<CardPage>("GET", "/v1/inventory/cards?" + p.toString());
    if (r.ok) {
      setList(r.data);
      setSelId((cur) => (cur && r.data.cards.some((c: CardRow) => c.id === cur) ? cur : cur ?? r.data.cards[0]?.id));
    } else setMsg({ text: r.message, bad: true });
  }, [edition, filter, page]);
  useEffect(() => { loadList(); }, [loadList]);

  const loadCard = useCallback(async (id: string) => {
    const r = await send<CardDetail>("GET", "/v1/inventory/cards/" + id);
    if (!r.ok) { setCard(null); setForm(null); return setMsg({ text: r.message, bad: true }); }
    const f = toForm(r.data);
    setCard(r.data);
    setForm(f.form);
    setFlags(f.flags);
    setPriceTyped(false);
    setFieldErrors({});
  }, []);
  useEffect(() => { if (selId) loadCard(selId); }, [selId, loadCard]);

  const dirty = !!(card && form && Object.keys(changes(card, form, flags, tcg).fields).length);

  /** Saves the open card; returns false when something needs fixing. */
  async function save(quiet = false): Promise<boolean> {
    if (!card || !form) return true;
    const { fields, bad } = changes(card, form, flags, tcg);
    if (Object.keys(bad).length) { setFieldErrors(bad); setMsg({ text: "Fix the fields marked in red.", bad: true }); return false; }
    if (Object.keys(fields).length === 0) return true;
    setBusy("Saving...");
    const r = await send<{ card: CardDetail; lost_fields?: string[] }>("PATCH", "/v1/inventory/cards/" + card.id, { base_version: card.version, fields });
    setBusy("");
    if (!r.ok) {
      setFieldErrors(r.fields);
      setMsg({ text: r.message, bad: true });
      return false;
    }
    const f = toForm(r.data.card);
    setCard(r.data.card);
    setForm(f.form);
    setFlags(f.flags);
    setFieldErrors({});
    setList((l) => (l ? { ...l, cards: l.cards.map((c: CardRow) => (c.id === r.data.card.id ? { ...c, data: r.data.card.data } : c)) } : l));
    if (!quiet || r.data.lost_fields?.length)
      setMsg({ text: r.data.lost_fields?.length ? `Saved. It was also changed on your PC (${r.data.lost_fields.join(", ")}); see Conflicts.` : `Saved ${r.data.card.data.sku ?? "the card"} at ${locationCode(r.data.card)}.` });
    return true;
  }

  // Like the desktop: moving to another card saves the open one first.
  async function pick(id: string) {
    if (id === selId) return;
    if (dirty && !(await save(true))) return;
    setMsg(null);
    setSelId(id);
  }

  const cards = list?.cards ?? [];
  const idx = cards.findIndex((c) => c.id === selId);

  useEffect(() => {
    function key(e: KeyboardEvent) {
      const t = e.target as HTMLElement;
      if (t.closest("input, select, textarea")) return;
      if (e.key === "ArrowDown" && idx < cards.length - 1) { e.preventDefault(); pick(cards[idx + 1].id); }
      if (e.key === "ArrowUp" && idx > 0) { e.preventDefault(); pick(cards[idx - 1].id); }
    }
    window.addEventListener("keydown", key);
    return () => window.removeEventListener("keydown", key);
  });

  function change(f: Partial<Filter>) {
    setFilter({ ...filter, ...f });
    setPage(1);
    setChecked(new Set());
  }

  function setField(k: keyof Form, v: string) {
    if (!form) return;
    const next = { ...form, [k]: v };
    if (k === "sale_price") setPriceTyped(true);
    // The estimate or markup moves the price unless one was typed.
    if (k === "estimated_value" && !priceTyped) {
      const est = Number(v.replace(/[$,\s]/g, ""));
      if (est > 0) next.sale_price = (est * (1 + form.percent / 100)).toFixed(2);
    }
    setForm(next);
  }

  function setPercent(p: number) {
    if (!form) return;
    const est = Number(form.estimated_value.replace(/[$,\s]/g, ""));
    setForm({ ...form, percent: p, sale_price: est > 0 ? (est * (1 + p / 100)).toFixed(2) : form.sale_price });
    setPriceTyped(false);
  }

  async function photo(side: "front" | "back", file?: File) {
    if (!file || !card) return;
    setBusy("Uploading photo...");
    const note = await uploadSide(card.id, side, await makeShot(file));
    setBusy("");
    if (note) return setMsg({ text: note, bad: true });
    await loadCard(card.id);
    setMsg({ text: `New ${side} photo saved. Your PC gets it on its next sync.` });
  }

  async function turn(side: "front" | "back", cw: boolean) {
    if (!card) return;
    setBusy("Turning photo...");
    const r = await send<CardDetail>("POST", `/v1/inventory/cards/${card.id}/rotate`, { side, clockwise: cw });
    setBusy("");
    if (!r.ok) return setMsg({ text: r.message, bad: true });
    await loadCard(card.id);
  }

  async function remove() {
    if (!card) return;
    if (!confirm(`Delete ${card.data.player_name ?? "this card"} (${card.data.sku ?? ""}) from your inventory? It's removed from your PC too on its next sync. This can't be undone.`)) return;
    setBusy("Deleting...");
    const r = await send("DELETE", "/v1/inventory/cards/" + card.id);
    setBusy("");
    if (!r.ok) return setMsg({ text: r.message, bad: true });
    const next = cards[idx + 1] ?? cards[idx - 1];
    setCard(null);
    setForm(null);
    setSelId(next?.id);
    setMsg({ text: "Card deleted." });
    loadList();
  }

  async function applyCategory() {
    if (!bulkCat || checked.size === 0) return;
    setBusy("Changing category...");
    let n = 0;
    for (const id of checked) {
      const c = await send<CardDetail>("GET", "/v1/inventory/cards/" + id);
      if (!c.ok) continue;
      const r = await send("PATCH", "/v1/inventory/cards/" + id, { base_version: c.data.version, fields: { sport: bulkCat } });
      if (r.ok) n++;
    }
    setBusy("");
    setMsg({ text: `Category set to ${bulkCat} on ${n} card${n === 1 ? "" : "s"}. Moving cards between boxes is done in FireCardManager.` });
    setChecked(new Set());
    loadList();
    if (selId) loadCard(selId);
  }

  const s = sports.find((x) => x.sport === filter.sport);
  const b = s?.boxes.find((x) => x.id === filter.box);
  const d = card?.data;
  const live = d ? liveText(d) : "";
  const onEbay = d?.status === "EBA" || d?.status === "EBF";
  const graded = form?.condition.toLowerCase() === "graded";
  const err = (k: string) => fieldErrors[k] && <span className="block text-[11px] font-normal text-bad">{fieldErrors[k]}</span>;
  const pages = list ? Math.max(1, Math.ceil(list.total / PER)) : 1;

  return (
    <div className="flex h-[calc(100vh-150px)] min-h-[640px] flex-col gap-3">
      {/* Scope and search, like the desktop's top bar */}
      <div className="flex flex-wrap items-center gap-2">
        <select className="field !mt-0 !w-auto !py-1.5" value={filter.sport} onChange={(e) => change({ sport: e.target.value, box: "", row: "" })} aria-label="Category">
          <option value="">All categories</option>
          {sports.map((x) => <option key={x.sport} value={x.sport}>{catName(x.sport)} ({x.cards})</option>)}
        </select>
        {s && (
          <select className="field !mt-0 !w-auto !py-1.5" value={filter.box} onChange={(e) => change({ box: e.target.value, row: "" })} aria-label="Box">
            <option value="">All boxes</option>
            {s.boxes.map((x) => <option key={x.id} value={x.id}>{x.label} ({x.cards})</option>)}
          </select>
        )}
        {b && (
          <select className="field !mt-0 !w-auto !py-1.5" value={filter.row} onChange={(e) => change({ row: e.target.value })} aria-label="Row">
            <option value="">All rows</option>
            {b.rows.map((r) => <option key={r.row} value={r.row}>Row {r.row} ({r.cards})</option>)}
          </select>
        )}
        <form className="flex min-w-[200px] flex-1 gap-2" onSubmit={(e) => { e.preventDefault(); change({ q }); }}>
          <input className="field !mt-0 !py-1.5" type="search" value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search player, set, SKU, location..." />
        </form>
        <div className="ml-auto flex flex-wrap items-center gap-2">
          {checked.size > 0 && (
            <>
              <span className="text-[13px] font-semibold text-go">{checked.size} checked</span>
              <select className="field !mt-0 !w-auto !py-1.5" value={bulkCat} onChange={(e) => setBulkCat(e.target.value)} aria-label="Category for checked cards">
                <option value="">Category...</option>
                {cats.map((c) => <option key={c} value={c}>{catName(c)}</option>)}
              </select>
              <button type="button" className="btn-ghost !py-1.5" disabled={!bulkCat || !!busy} onClick={applyCategory}>Apply</button>
            </>
          )}
          {card && (
            <Link href={`/lister?edition=${edition}&ids=${(checked.size ? [...checked] : [card.id]).join(",")}`} className="btn-ghost !py-1.5">
              Send {checked.size > 1 ? `${checked.size} cards` : ""} to Bulk Lister
            </Link>
          )}
          {card && <button type="button" className="btn-ghost !py-1.5 text-bad" disabled={!!busy} onClick={remove}>Delete</button>}
        </div>
      </div>

      <div className="grid min-h-0 flex-1 grid-cols-[minmax(0,1fr)_340px] gap-3">
        <div className="flex min-h-0 flex-col gap-3">
          {/* Photos */}
          <div className="grid h-[46%] min-h-[260px] grid-cols-2 gap-3">
            {(["front", "back"] as const).map((side) => (
              <div key={side} className="card flex min-h-0 flex-col !p-2">
                <div className="flex items-center justify-between px-1 pb-1 text-[12px]">
                  <span className="font-bold capitalize">{side}</span>
                  <span className="text-muted">Scroll to zoom · drag · double-click to reset</span>
                </div>
                <div className="min-h-0 flex-1">
                  <ZoomPhoto src={side === "front" ? card?.front_url : card?.back_url} alt={`${side} of ${d?.player_name ?? "card"}`} />
                </div>
                <div className="flex gap-2 pt-2">
                  <button type="button" className="btn-ghost !px-3 !py-1 text-[12px]" disabled={!card || !!busy}
                    onClick={() => (side === "front" ? frontFile : backFile).current?.click()}>Upload image</button>
                  <button type="button" className="btn-ghost !px-3 !py-1 text-[12px]" disabled={!card || !!busy} title="Turn left" onClick={() => turn(side, false)}>⟲</button>
                  <button type="button" className="btn-ghost !px-3 !py-1 text-[12px]" disabled={!card || !!busy} title="Turn right" onClick={() => turn(side, true)}>⟳</button>
                </div>
                <input ref={side === "front" ? frontFile : backFile} type="file" accept="image/*" hidden
                  onChange={(e) => { photo(side, e.target.files?.[0]); e.target.value = ""; }} />
              </div>
            ))}
          </div>

          {/* The card list */}
          <div className="card flex min-h-0 flex-1 flex-col !p-0">
            <div className="min-h-0 flex-1 overflow-auto">
              <table className="w-full text-left text-[12.5px]">
                <thead className="sticky top-0 bg-paper text-[11px] uppercase text-muted">
                  <tr className="border-b border-line">
                    <th className="w-8 px-2 py-2">
                      <input type="checkbox" aria-label="Check all on this page" checked={cards.length > 0 && cards.every((c) => checked.has(c.id))}
                        onChange={(e) => setChecked(e.target.checked ? new Set(cards.map((c) => c.id)) : new Set())} />
                    </th>
                    <th className="px-2">Location</th><th className="px-2">SKU</th><th className="px-2">Category</th><th className="px-2">Year</th>
                    <th className="px-2">Product</th><th className="px-2">Set</th><th className="px-2">Name</th><th className="px-2">Card #</th>
                    <th className="px-2 text-right">Price</th><th className="px-2">Status</th>
                  </tr>
                </thead>
                <tbody>
                  {list === null && <tr><td colSpan={11} className="p-4 text-muted">Loading...</td></tr>}
                  {list && cards.length === 0 && <tr><td colSpan={11} className="p-4 text-muted">No cards match.</td></tr>}
                  {cards.map((c) => (
                    <tr key={c.id} onClick={() => pick(c.id)}
                      className={`cursor-pointer border-b border-line ${c.id === selId ? "bg-fire/15" : "hover:bg-wash"}`}>
                      <td className="px-2 py-1.5" onClick={(e) => e.stopPropagation()}>
                        <input type="checkbox" checked={checked.has(c.id)} aria-label={`Check ${c.data.player_name}`}
                          onChange={(e) => { const n = new Set(checked); if (e.target.checked) n.add(c.id); else n.delete(c.id); setChecked(n); }} />
                      </td>
                      <td className="whitespace-nowrap px-2 font-semibold">{locationCode(c)}</td>
                      <td className="whitespace-nowrap px-2">{c.data.sku}</td>
                      <td className="px-2">{catName(c.data.sport)}</td>
                      <td className="px-2">{seasonText(c.data)}</td>
                      <td className="max-w-[140px] truncate px-2">{c.data.brand}</td>
                      <td className="max-w-[220px] truncate px-2">{c.data.set_name}</td>
                      <td className="max-w-[200px] truncate px-2 font-semibold">{c.data.player_name}</td>
                      <td className="px-2">{c.data.card_number}</td>
                      <td className="num px-2 text-right">{money(c.data.sale_price)}</td>
                      <td className="whitespace-nowrap px-2">{statusText(c.data)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="flex items-center gap-3 border-t border-line px-3 py-2 text-[12px] text-muted">
              <span className="num">{list ? `${list.total.toLocaleString()} cards` : ""}{idx >= 0 ? ` · card ${(page - 1) * PER + idx + 1}` : ""} · ↑ ↓ to move between cards</span>
              {pages > 1 && (
                <span className="ml-auto flex items-center gap-2">
                  <button type="button" className="font-semibold text-link disabled:opacity-40" disabled={page <= 1} onClick={() => setPage(page - 1)}>Previous</button>
                  <span>Page {page} of {pages}</span>
                  <button type="button" className="font-semibold text-link disabled:opacity-40" disabled={page >= pages} onClick={() => setPage(page + 1)}>Next</button>
                </span>
              )}
            </div>
          </div>
        </div>

        {/* Details, like the desktop's right pane */}
        <aside className="card flex min-h-0 flex-col !p-0">
          {!card || !form || !d ? (
            <p className="p-4 text-muted">{list && cards.length === 0 ? "No card selected." : "Loading..."}</p>
          ) : (
            <>
              <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-4 text-[13px]">
                <div className="grid grid-cols-2 gap-2">
                  <div><span className="block text-[11px] text-muted">Location</span><b>{locationCode(card)}</b></div>
                  <div><span className="block text-[11px] text-muted">SKU</span><b>{d.sku ?? "—"}</b></div>
                  {live && <div className="col-span-2 font-semibold text-go">{live}</div>}
                </div>
                <div>
                  <p className="text-[11px] font-bold uppercase text-muted">Check live prices</p>
                  <div className="mt-1 grid grid-cols-2 gap-2">
                    <a className="btn !py-1.5 text-center text-[12px]" target="_blank" rel="noopener noreferrer"
                      href={`https://www.ebay.com/sch/i.html?_nkw=${encodeURIComponent(researchQuery(d))}&LH_Sold=1&LH_Complete=1`}>eBay sold ↗</a>
                    <a className="btn !py-1.5 text-center text-[12px]" target="_blank" rel="noopener noreferrer" href={comcUrl(d)}>COMC ↗</a>
                  </div>
                </div>

                <fieldset className="space-y-2" disabled={!!busy}>
                  <p className="text-[11px] font-bold uppercase text-muted">Identity</p>
                  <label className="block font-semibold">Category
                    <select className="field" value={form.sport} onChange={(e) => setField("sport", e.target.value)}>
                      {!cats.includes(form.sport) && <option value={form.sport}>{catName(form.sport) || "—"}</option>}
                      {cats.map((c) => <option key={c} value={c}>{catName(c)}</option>)}
                    </select></label>
                  <label className="block font-semibold">{tcg ? "Character / subject name" : "Player name"}
                    <input className="field" value={form.player_name} placeholder={d.player_name === PENDING ? "Not identified yet" : ""} onChange={(e) => setField("player_name", e.target.value)} />{err("player_name")}</label>
                  <label className="block font-semibold">{tcg ? "Franchise / show / movie / series" : "Team"}<input className="field" value={form.team} onChange={(e) => setField("team", e.target.value)} /></label>
                  <label className="block font-semibold">Year<input className="field num" value={form.year} onChange={(e) => setField("year", e.target.value)} placeholder="2024 or 2024-25" />{err("year")}</label>
                  <label className="block font-semibold">Product name <span className="font-normal text-muted">(e.g. Panini Prizm, Topps)</span>
                    <input className="field" value={form.brand} onChange={(e) => setField("brand", e.target.value)} />{err("brand")}</label>
                  <label className="block font-semibold">Set name<input className="field" value={form.set_name} onChange={(e) => setField("set_name", e.target.value)} />{err("set_name")}</label>
                  <label className="block font-semibold">Card #<input className="field" value={form.card_number} onChange={(e) => setField("card_number", e.target.value)} />{err("card_number")}</label>
                  <label className="block font-semibold">Variety / parallel<input className="field" value={form.variety} onChange={(e) => setField("variety", e.target.value)} /></label>
                  <label className="block font-semibold">Serial # <span className="font-normal text-muted">(e.g. 23/99)</span>
                    <input className="field" value={form.serial_number} onChange={(e) => setField("serial_number", e.target.value)} /></label>
                  <label className="block font-semibold">Condition
                    <input className="field" list="fcm-conditions" value={form.condition} onChange={(e) => setField("condition", e.target.value)} />{err("condition")}</label>
                  <datalist id="fcm-conditions">{CONDITIONS.map((c) => <option key={c} value={c} />)}</datalist>
                  {graded && (
                    <div className="grid grid-cols-2 gap-2">
                      <label className="block font-semibold">Grading company<input className="field" value={form.grading_company} onChange={(e) => setField("grading_company", e.target.value)} placeholder="PSA" /></label>
                      <label className="block font-semibold">Grade<input className="field" value={form.card_grade} onChange={(e) => setField("card_grade", e.target.value)} placeholder="10" /></label>
                      <label className="col-span-2 block font-semibold">Cert number<input className="field" value={form.grading_cert_number} onChange={(e) => setField("grading_cert_number", e.target.value)} /></label>
                    </div>
                  )}
                  <div className="flex flex-wrap gap-x-4 gap-y-1 pt-1">
                    {[...FLAGS_SPORTS, ...(tcg ? FLAGS_TCG : [])].map(([k, label]) => (
                      <label key={k} className="flex items-center gap-1.5 font-normal">
                        <input type="checkbox" checked={!!flags[k]} onChange={(e) => setFlags({ ...flags, [k]: e.target.checked })} /> {label}
                      </label>
                    ))}
                  </div>
                </fieldset>

                <fieldset className="space-y-2" disabled={!!busy}>
                  <p className="text-[11px] font-bold uppercase text-muted">Pricing</p>
                  <label className="block font-semibold">Status
                    <select className="field" value={form.status} disabled={onEbay} onChange={(e) => setField("status", e.target.value)}>
                      {onEbay && <option value={form.status}>{form.status === "EBA" ? "eBay auction" : "eBay Buy It Now"}</option>}
                      <option value="Ready">Ready to list</option>
                      <option value="NotForSale">Not for sale</option>
                    </select>
                    {onEbay && <span className="block text-[11px] font-normal text-muted">Live on eBay: end the listing to change it.</span>}
                    {err("status")}</label>
                  <div className="grid grid-cols-2 gap-2">
                    <label className="block font-semibold">AI estimate<input className="field num" inputMode="decimal" value={form.estimated_value} onChange={(e) => setField("estimated_value", e.target.value)} />{err("estimated_value")}</label>
                    <label className="block font-semibold">Paid price<input className="field num" inputMode="decimal" value={form.paid_price} onChange={(e) => setField("paid_price", e.target.value)} placeholder="optional" />{err("paid_price")}</label>
                  </div>
                  <label className="block font-semibold">
                    <span className="flex justify-between">Markup above AI estimate <span className="num">{form.percent}%</span></span>
                    <input type="range" min={10} max={300} step={5} value={form.percent} onChange={(e) => setPercent(Number(e.target.value))} className="mt-2 w-full accent-[var(--color-fire)]" />
                  </label>
                  <label className="block font-semibold">Sale price <span className="font-normal text-muted">(overrides the markup)</span>
                    <input className="field num !border-fire text-[16px] font-bold" inputMode="decimal" value={form.sale_price} onChange={(e) => setField("sale_price", e.target.value)} />{err("sale_price")}</label>
                  {d.last_priced_at && <p className="text-[11px] text-muted">Last priced {new Date(d.last_priced_at).toLocaleDateString()}</p>}
                </fieldset>
                <p className="text-[12px]">
                  <Link href={`/inventory/cards/${card.id}`} className="font-semibold text-link">Mark sold...</Link>
                  <span className="text-muted"> · Moving a card to another box or row is done in FireCardManager.</span>
                </p>
              </div>
              <div className="flex flex-wrap items-center gap-2 border-t border-line p-3">
                {msg && <p role="status" className={`w-full text-[12px] ${msg.bad ? "text-bad" : "text-go"}`}>{msg.text}</p>}
                {busy && <span className="text-[12px] text-muted">{busy}</span>}
                <button type="button" className="btn-ghost ml-auto !py-1.5" disabled={!dirty || !!busy}
                  onClick={() => { const f = toForm(card); setForm(f.form); setFlags(f.flags); setFieldErrors({}); setMsg({ text: "Reverted to the last saved values." }); }}>Revert</button>
                <button type="button" className="btn !py-1.5" disabled={!dirty || !!busy} onClick={() => save()}>Save changes</button>
              </div>
            </>
          )}
        </aside>
      </div>
    </div>
  );
}
