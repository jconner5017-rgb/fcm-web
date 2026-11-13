"use client";

import Link from "next/link";
import { useRef, useState } from "react";
import { send } from "@/lib/client";
import { csvRecords, download, toCsv } from "@/lib/csv";
import { fillForm, identify, makeShot, SPORTS, TCG, uploadPhotos, whereText, type Created, type Shot } from "@/lib/addcard";

/** Cards are prepared (photos, AI) a chunk at a time, then saved in CSV order. */
const CHUNK = 12;
const PENDING = "(unidentified - pending ID)";

type Row = {
  n: number; // CSV line (header is line 1)
  label: string;
  fields: Record<string, unknown>;
  boxName: string;
  front: string;
  back: string;
  problem?: string;
};
type Outcome = { n: number; label: string; ok: boolean; text: string; id?: string };

const bool = (v: string) => /^(true|yes|y|1|x)$/i.test(v.trim());
const num = (v: string) => {
  const s = v.replace(/[$,\s]/g, "");
  return s === "" || Number.isNaN(Number(s)) ? undefined : Number(s);
};
const baseName = (p: string) => p.split(/[\\/]/).pop()!.trim().toLowerCase();

/** The CSV's columns (FireCardManager's CardCsvRow, plus the web export's extras) as card fields. */
function toRow(r: Record<string, string>, n: number, cats: string[]): Row {
  const f: Record<string, unknown> = {};
  const text = (col: string, key: string) => { if (r[col]) f[key] = r[col]; };
  const sport = cats.find((c) => c.toLowerCase() === (r.sport ?? "").trim().toLowerCase());
  if (sport) f.sport = sport;
  text("playername", "player_name");
  text("team", "team");
  // "2024" or a season like "2024-25".
  const y = /^(\d{4})(?:\s*-\s*(\d{2,4}))?$/.exec(r.year ?? "");
  if (y && Number(y[1]) > 0) {
    f.year = Number(y[1]);
    if (y[2]) f.season_end = y[2].length === 2 ? Number(y[1].slice(0, 2) + y[2]) : Number(y[2]);
  }
  if (num(r.seasonend ?? "")) f.season_end = num(r.seasonend);
  text("brand", "brand");
  text("setname", "set_name");
  text("cardnumber", "card_number");
  text("variety", "variety");
  text("condition", "condition");
  text("serialnumber", "serial_number");
  text("gradingcompany", "grading_company");
  text("cardgrade", "card_grade");
  text("certnumber", "grading_cert_number");
  for (const [col, key] of [["estimatedvalue", "estimated_value"], ["saleprice", "sale_price"], ["paidprice", "paid_price"]]) {
    const v = num(r[col] ?? "");
    if (v !== undefined && v > 0) f[key] = v;
  }
  if (r.status === "Ready" || r.status === "NotForSale") f.status = r.status;
  for (const [col, key] of [["isrookie", "is_rookie"], ["isautograph", "is_autograph"], ["ismemorabilia", "is_memorabilia"], ["isshortprint", "is_short_print"],
    ["iserrorvariant", "is_error_variant"], ["ischase", "is_chase"], ["isfoil", "is_foil"], ["issketchcard", "is_sketch_card"]]) {
    if (r[col]) f[key] = bool(r[col]);
  }
  if (r.lastpricedat) {
    const t = new Date(/[zZ]|[+-]\d\d:?\d\d$/.test(r.lastpricedat) ? r.lastpricedat : r.lastpricedat + "Z");
    if (!Number.isNaN(t.getTime())) f.last_priced_at = t.toISOString();
  }
  const label = r.playername || (r.cardnumber ? `card #${r.cardnumber}` : `row ${n}`);
  const row: Row = { n, label, fields: f, boxName: r.boxname ?? "", front: r.frontimagefilename ?? "", back: r.backimagefilename ?? "" };
  if (!sport) row.problem = `Sport is required and "${r.sport ?? ""}" isn't recognized.`;
  else if (!row.front || !row.back) row.problem = "FrontImageFileName and BackImageFileName are both required.";
  return row;
}

async function pool<T>(list: T[], size: number, fn: (t: T) => Promise<void>) {
  let i = 0;
  await Promise.all(Array.from({ length: Math.min(size, list.length) }, async () => {
    while (i < list.length) await fn(list[i++]);
  }));
}

export default function ImportForm({ editions, hasKey }: { editions: string[]; hasKey: boolean }) {
  const [edition, setEdition] = useState(editions[0] ?? "sports");
  const [csvName, setCsvName] = useState("");
  const [rows, setRows] = useState<Row[]>([]);
  const [images, setImages] = useState<Map<string, File>>(new Map());
  const [useAI, setUseAI] = useState(hasKey);
  const [busy, setBusy] = useState(false);
  const [progress, setProgress] = useState("");
  const [outcomes, setOutcomes] = useState<Outcome[]>([]);
  const [error, setError] = useState("");
  const csvInput = useRef<HTMLInputElement>(null);
  const imgInput = useRef<HTMLInputElement>(null);
  const folderInput = useRef<HTMLInputElement>(null);
  const tcg = edition === "tcg";
  const cats = tcg ? [...TCG, "Mixed"] : SPORTS;

  async function pickCsv(file?: File) {
    if (!file) return;
    setError("");
    setOutcomes([]);
    const { headers, records } = csvRecords(await file.text());
    if (!headers.map((h) => h.toLowerCase()).includes("sport")) {
      setRows([]);
      return setError(`"${file.name}" has no Sport column. Download the sample CSV to see the columns.`);
    }
    setCsvName(file.name);
    setRows(records.map((r, i) => toRow(r, i + 2, cats)));
  }

  function pickImages(list: FileList | null) {
    if (!list) return;
    const m = new Map(images);
    for (const f of Array.from(list)) if (f.type.startsWith("image/") || /\.(jpe?g|png|webp|bmp|gif|tiff?)$/i.test(f.name)) m.set(f.name.toLowerCase(), f);
    setImages(m);
  }

  /** The photo for a CSV cell: a file picked here, or a link (FireCardManager Web's hosted photos). */
  function source(ref: string): File | string | null {
    if (/^https?:\/\//i.test(ref)) return ref;
    return images.get(baseName(ref)) ?? null;
  }

  async function load(ref: string): Promise<Shot> {
    const s = source(ref);
    if (!s) throw new Error(`photo "${ref}" wasn't among the pictures you picked`);
    if (typeof s !== "string") return makeShot(s);
    const u = new URL(s);
    const res = await fetch(u.origin === location.origin ? u.pathname : s).catch(() => null);
    if (!res || !res.ok) throw new Error(`couldn't download ${s}`);
    return makeShot(await res.blob());
  }

  const ready: Row[] = rows.filter((r: Row) => !r.problem);
  const missing = ready.filter((r) => !source(r.front) || !source(r.back)).length;

  async function run() {
    setBusy(true);
    setError("");
    const done: Outcome[] = rows.filter((r) => r.problem).map((r) => ({ n: r.n, label: r.label, ok: false, text: "Skipped: " + r.problem }));
    setOutcomes([...done]);
    let stop = "";
    for (let i = 0; i < ready.length && !stop; i += CHUNK) {
      const chunk = ready.slice(i, i + CHUNK);
      const prepared = new Map<number, { front: Shot; back: Shot; fields: Record<string, unknown> } | string>();
      setProgress(`Preparing cards ${i + 1}-${i + chunk.length} of ${ready.length}...`);
      await pool<Row>(chunk, 3, async (r) => {
        try {
          const [front, back] = [await load(r.front), await load(r.back)];
          let fields = r.fields;
          if (useAI && hasKey && !r.fields.player_name) {
            const ai = await identify(edition, front, back);
            if (ai.ok) {
              // The CSV's own values win; AI fills the blanks.
              const filled = fillForm(ai.data.fields);
              const extra: Record<string, unknown> = { ...filled.flags };
              for (const [k, v] of Object.entries(filled.form)) if (v !== "") extra[k] = ["year", "season_end", "estimated_value", "sale_price"].includes(k) ? Number(v) : v;
              for (const k of ["sale_price_percent", "last_priced_at"] as const) if (ai.data.fields[k] != null) extra[k] = ai.data.fields[k];
              fields = { ...extra, ...r.fields, sport: r.fields.sport };
              if (!fields.sale_price) {
                delete fields.sale_price;
                if (fields.status === "Ready") fields.status = "NotForSale";
              }
            } else if (["no_ai_key", "ai_key_rejected", "too_many"].includes(ai.code)) {
              stop = ai.message;
            }
          }
          prepared.set(r.n, { front, back, fields });
        } catch (e) {
          prepared.set(r.n, "Skipped: " + (e instanceof Error ? e.message : "photos couldn't be read") + ".");
        }
      });
      for (const r of chunk) {
        const p = prepared.get(r.n)!;
        if (typeof p === "string") { done.push({ n: r.n, label: r.label, ok: false, text: p }); continue; }
        setProgress(`Saving card ${done.length + 1} of ${rows.length}...`);
        const c = await send<Created>("POST", "/v1/inventory/cards", { edition, import: true, box_name: r.boxName, fields: p.fields });
        if (!c.ok) {
          done.push({ n: r.n, label: r.label, ok: false, text: c.message + (Object.keys(c.fields).length ? " " + Object.entries(c.fields).map(([k, v]) => `${k}: ${v}`).join("; ") : "") });
          if (c.status === 401 || c.status === 402) { stop = c.message; break; }
          continue;
        }
        const note = await uploadPhotos(c.data.id, p.front, p.back);
        const where = whereText(c.data);
        const boxNote = r.boxName && c.data.box_name?.toLowerCase() !== r.boxName.toLowerCase() ? ` (box "${r.boxName}" doesn't exist here, so it was filed by category)` : "";
        done.push({ n: r.n, label: c.data.data.player_name ?? r.label, ok: true, text: `${where}${boxNote}.${note ? " " + note : ""}`, id: c.data.id });
      }
      setOutcomes([...done]);
    }
    if (stop) setError(stop + " The rest weren't imported.");
    setProgress("");
    setBusy(false);
  }

  function sample() {
    const head = ["Id", "Sport", "PlayerName", "Team", "Year", "Brand", "SetName", "CardNumber", "Variety", "Condition", "EstimatedValue",
      "FrontImageFileName", "BackImageFileName", "BoxName", "SlotRow", "SlotNumber", "LastPricedAt"];
    const full = tcg
      ? ["", "SciFi", "Darth Vader", "Star Wars", "1977", "Topps", "Star Wars Series 1", "1", "", "Raw - NM", "45.00", "vader_front.jpg", "vader_back.jpg", "Vintage Sci-Fi", "", "", ""]
      : ["", "Baseball", "Mike Trout", "Angels", "2011", "Topps Update", "Update Series", "US175", "Refractor", "Raw - NM", "450.00", "trout_front.jpg", "trout_back.jpg", "1990s Junk Wax", "", "", ""];
    const min = ["", tcg ? "Superheroes" : "Basketball", "", "", "", "", "", "", "", "", "", "card002_front.jpg", "card002_back.jpg", "", "", "", ""];
    download(tcg ? "FireCardManagerTCG_sample_import.csv" : "FireCardManager_sample_import.csv", toCsv([head, full, min]));
  }

  const imported = outcomes.filter((o) => o.ok).length;

  return (
    <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_340px]">
      <section className="card space-y-4">
        {editions.length > 1 && (
          <label className="block text-[13px] font-semibold">
            App
            <select className="field !w-auto" value={edition} disabled={busy} onChange={(e) => { setEdition(e.target.value); setRows([]); setCsvName(""); }}>
              {editions.map((e) => <option key={e} value={e}>{e === "tcg" ? "TCG" : "Sports"}</option>)}
            </select>
          </label>
        )}
        <div className="space-y-2">
          <p className="text-[13px] font-semibold">1. The CSV file</p>
          <div className="flex flex-wrap items-center gap-2">
            <button type="button" className="btn-ghost" disabled={busy} onClick={() => csvInput.current?.click()}>{csvName ? "Choose another CSV" : "Choose CSV"}</button>
            {csvName && <span className="text-[13px]">{csvName} · {rows.length} row{rows.length === 1 ? "" : "s"}</span>}
            <button type="button" className="ml-auto text-[13px] font-semibold text-link" onClick={sample}>Download sample CSV</button>
          </div>
          <input ref={csvInput} type="file" accept=".csv,text/csv" hidden onChange={(e) => { pickCsv(e.target.files?.[0]); e.target.value = ""; }} />
        </div>

        <div className="space-y-2">
          <p className="text-[13px] font-semibold">2. The front and back photos</p>
          <div className="flex flex-wrap items-center gap-2">
            <button type="button" className="btn-ghost" disabled={busy} onClick={() => folderInput.current?.click()}>Choose folder</button>
            <button type="button" className="btn-ghost" disabled={busy} onClick={() => imgInput.current?.click()}>Choose pictures</button>
            {images.size > 0 && <span className="text-[13px]">{images.size} picture{images.size === 1 ? "" : "s"}</span>}
            {images.size > 0 && <button type="button" className="text-[13px] text-muted hover:text-bad" onClick={() => setImages(new Map())}>Clear</button>}
          </div>
          <input ref={imgInput} type="file" accept="image/*" multiple hidden onChange={(e) => { pickImages(e.target.files); e.target.value = ""; }} />
          <input ref={folderInput} type="file" {...{ webkitdirectory: "" }} multiple hidden onChange={(e) => { pickImages(e.target.files); e.target.value = ""; }} />
          <p className="text-[12px] text-muted">
            Photos are matched by the file names in FrontImageFileName and BackImageFileName. Links to FireCardManager Web photos (from a web export) work without picking anything.
          </p>
        </div>

        {rows.length > 0 && (
          <div className="space-y-2 text-[13px]">
            <p>
              <b>{ready.length}</b> of {rows.length} rows can be imported.
              {missing > 0 && <span className="text-bad"> {missing} still need their photos picked.</span>}
            </p>
            {rows.filter((r) => r.problem).slice(0, 8).map((r) => (
              <p key={r.n} className="text-bad">Line {r.n} ({r.label}): {r.problem}</p>
            ))}
            {hasKey ? (
              <label className="flex items-center gap-2">
                <input type="checkbox" checked={useAI} onChange={(e) => setUseAI(e.target.checked)} />
                Identify cards with no PlayerName using your AI key ({ready.filter((r) => !r.fields.player_name).length} cards)
              </label>
            ) : (
              <p className="text-muted">
                Cards with no PlayerName are saved as &quot;{PENDING}&quot;. Add your AI key on the{" "}
                <Link href="/account#ai-key" className="font-semibold text-fire">Account page</Link> to identify them as they import.
              </p>
            )}
          </div>
        )}

        {error && <p role="alert" className="text-bad">{error}</p>}
        <div className="flex flex-wrap items-center gap-3">
          <button type="button" className="btn" disabled={busy || ready.length === 0} onClick={run}>
            {busy ? "Importing..." : `Import ${ready.length} card${ready.length === 1 ? "" : "s"}`}
          </button>
          {progress && <span className="text-[13px] text-muted">{progress}</span>}
        </div>
      </section>

      <aside className="space-y-3">
        <div className="card text-[13px] text-muted">
          <p className="font-semibold text-ink">How it works</p>
          <p className="mt-1">The same CSV as FireCardManager&apos;s Import Cards from CSV. Only Sport and the two photo columns are required; everything else can be blank.</p>
          <p className="mt-2">A BoxName that matches one of your boxes puts the card at the end of that box. Otherwise it&apos;s filed into its category&apos;s box, like any new card.</p>
        </div>
        {outcomes.length > 0 && (
          <div className="card space-y-2 text-[13px]" role="status">
            <p className="font-semibold">Imported {imported} card{imported === 1 ? "" : "s"}. Skipped {outcomes.length - imported}.</p>
            <ul className="max-h-[420px] space-y-1 overflow-y-auto">
              {outcomes.map((o) => (
                <li key={o.n} className={o.ok ? "" : "text-bad"}>
                  Line {o.n}: {o.ok && o.id ? <Link href={`/inventory/cards/${o.id}`} className="font-semibold text-link">{o.label}</Link> : o.label}, {o.text}
                </li>
              ))}
            </ul>
          </div>
        )}
      </aside>
    </div>
  );
}
