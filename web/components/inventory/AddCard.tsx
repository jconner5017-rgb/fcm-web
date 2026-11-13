"use client";

import Link from "next/link";
import { useRef, useState } from "react";
import { send } from "@/lib/client";
import { money } from "@/lib/inventory";

type Step = "photos" | "identifying" | "review" | "saving" | "done";
type Shot = { preview: string; full: Blob; thumb: Blob; ai: Blob };

const SPORTS = ["Baseball", "Basketball", "Football", "Hockey", "Soccer", "Boxing", "Golf", "Mma", "Racing", "Tennis", "Wrestling", "Multisport"];
const TCG = ["Pokemon", "Magic", "YuGiOh", "OnePiece", "Lorcana", "DragonBall", "Digimon", "FleshAndBlood", "FinalFantasy", "StarWarsTcg", "WeissSchwarz",
  "Vanguard", "Naruto", "MetaZoo", "Riftbound", "WorldOfWarcraft", "MyLittlePony", "Sorcery", "Netrunner", "UniVersus", "OtherTcg", "Superheroes", "SciFi",
  "Fantasy", "Anime", "Horror", "Entertainment", "Music", "Historical", "Novelty"];

/** Loads a photo (EXIF rotation applied by the browser) and makes a JPEG no longer than `side` px. */
async function shrink(file: Blob, side: number, quality = 0.85): Promise<Blob> {
  const url = URL.createObjectURL(file);
  try {
    const img = new Image();
    img.decoding = "async";
    img.src = url;
    await img.decode();
    const scale = Math.min(1, side / Math.max(img.naturalWidth, img.naturalHeight));
    const w = Math.round(img.naturalWidth * scale), h = Math.round(img.naturalHeight * scale);
    const c = document.createElement("canvas");
    c.width = w;
    c.height = h;
    c.getContext("2d")!.drawImage(img, 0, 0, w, h);
    return await new Promise<Blob>((ok, fail) => c.toBlob((b) => (b ? ok(b) : fail(new Error("Couldn't read the photo."))), "image/jpeg", quality));
  } finally {
    URL.revokeObjectURL(url);
  }
}

async function base64(b: Blob): Promise<string> {
  const buf = new Uint8Array(await b.arrayBuffer());
  let s = "";
  for (let i = 0; i < buf.length; i += 0x8000) s += String.fromCharCode(...buf.subarray(i, i + 0x8000));
  return btoa(s);
}

const EMPTY = {
  sport: "", player_name: "", team: "", year: "", season_end: "", brand: "", set_name: "", card_number: "", variety: "", serial_number: "",
  condition: "Raw - NM", grading_company: "", card_grade: "", estimated_value: "", sale_price: "", status: "Ready",
};
type Form = typeof EMPTY;
const FLAGS = { is_rookie: false, is_autograph: false, is_memorabilia: false, is_short_print: false, is_chase: false, is_foil: false, is_sketch_card: false };

export default function AddCard({ editions, hasKey }: { editions: string[]; hasKey: boolean }) {
  const [edition, setEdition] = useState(editions[0] ?? "sports");
  const [step, setStep] = useState<Step>("photos");
  const [front, setFront] = useState<Shot | null>(null);
  const [back, setBack] = useState<Shot | null>(null);
  const [form, setForm] = useState<Form>(EMPTY);
  const [flags, setFlags] = useState(FLAGS);
  const [extra, setExtra] = useState<Record<string, unknown>>({});
  const [notes, setNotes] = useState("");
  const [error, setError] = useState("");
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [progress, setProgress] = useState("");
  const [added, setAdded] = useState<{ id: string; name: string; where: string } | null>(null);
  const [count, setCount] = useState(0);
  const frontInput = useRef<HTMLInputElement>(null);
  const backInput = useRef<HTMLInputElement>(null);
  const tcg = edition === "tcg";

  async function take(side: "front" | "back", file?: File) {
    if (!file) return;
    setError("");
    try {
      const [full, thumb, ai] = await Promise.all([shrink(file, 1600), shrink(file, 400, 0.8), shrink(file, 1100, 0.85)]);
      const shot = { preview: URL.createObjectURL(thumb), full, thumb, ai };
      if (side === "front") {
        setFront(shot);
        if (!back) setTimeout(() => backInput.current?.click(), 300); // straight on to the back
      } else setBack(shot);
    } catch {
      setError("That photo couldn't be read. Try taking it again.");
    }
  }

  function fill(fields: Record<string, unknown>) {
    const f = { ...EMPTY };
    for (const k of Object.keys(EMPTY) as (keyof Form)[]) {
      const v = fields[k];
      if (v !== undefined && v !== null) f[k] = String(v);
    }
    setForm(f);
    const fl = { ...FLAGS };
    for (const k of Object.keys(FLAGS) as (keyof typeof FLAGS)[]) if (typeof fields[k] === "boolean") fl[k] = fields[k] as boolean;
    setFlags(fl);
    setExtra({ sale_price_percent: fields.sale_price_percent, last_priced_at: fields.last_priced_at });
  }

  async function identify() {
    if (!front || !back) return;
    setStep("identifying");
    setError("");
    const r = await send<{ fields: Record<string, unknown>; notes?: string; confidence?: number }>("POST", "/v1/inventory/identify", {
      edition, front: await base64(front.ai), back: await base64(back.ai),
    });
    if (!r.ok) {
      setStep("photos");
      setError(r.message);
      return;
    }
    fill(r.data.fields);
    setNotes(r.data.notes ?? "");
    setStep("review");
  }

  function manual() {
    fill({});
    setNotes("");
    setStep("review");
  }

  async function save(e: React.FormEvent) {
    e.preventDefault();
    const fields: Record<string, unknown> = { ...flags };
    const bad: Record<string, string> = {};
    for (const [k, v] of Object.entries(form)) {
      if (["year", "season_end", "estimated_value", "sale_price"].includes(k)) {
        const s = v.replace(/[$,\s]/g, "");
        if (s === "") continue;
        if (Number.isNaN(Number(s))) bad[k] = "Enter a number.";
        else fields[k] = Number(s);
      } else if (v.trim() !== "") fields[k] = v.trim();
    }
    if (!tcg) for (const k of ["is_chase", "is_foil", "is_sketch_card"]) delete fields[k];
    if (form.sale_price.trim() === "" && form.status === "Ready") fields.status = "NotForSale";
    if (extra.sale_price_percent != null && form.sale_price.trim() !== "") fields.sale_price_percent = extra.sale_price_percent;
    if (extra.last_priced_at) fields.last_priced_at = extra.last_priced_at;
    if (Object.keys(bad).length) return setFieldErrors(bad);

    setStep("saving");
    setError("");
    setFieldErrors({});
    setProgress("Saving the card...");
    const c = await send<{ id: string; box_name?: string; data: { slot_row?: string; slot_number?: number; player_name?: string } }>(
      "POST", "/v1/inventory/cards", { edition, fields },
    );
    if (!c.ok) {
      setStep("review");
      setError(c.message);
      setFieldErrors(c.fields);
      return;
    }
    // Photos go straight to photo storage, then the server attaches them.
    let photoNote = "";
    if (front && back) {
      setProgress("Uploading photos...");
      const blobs: Record<string, Blob> = { front: front.full, front_thumb: front.thumb, back: back.full, back_thumb: back.thumb };
      const u = await send<{ uploads: { side: string; upload_url?: string; photo_id?: string; error?: string }[] }>(
        "POST", `/v1/inventory/cards/${c.data.id}/photo-urls`, { photos: Object.entries(blobs).map(([side, b]) => ({ side, bytes: b.size })) },
      );
      if (u.ok) {
        const ids: string[] = [];
        await Promise.all(u.data.uploads.map(async (s) => {
          if (!s.upload_url || !s.photo_id) return;
          try {
            const res = await fetch(s.upload_url, { method: "PUT", body: blobs[s.side], headers: { "content-type": "image/jpeg" } });
            if (res.ok) ids.push(s.photo_id);
          } catch {}
        }));
        if (ids.length) await send("POST", "/v1/inventory/photos/confirm", { photo_ids: ids });
        if (ids.length < 4) photoNote = " Some photos didn't upload; your PC can add them, or take them again from the card page later.";
      } else photoNote = " The photos didn't upload: " + u.message;
    }
    const d = c.data.data;
    setAdded({
      id: c.data.id, name: d.player_name ?? "Card",
      where: c.data.box_name ? `${c.data.box_name}${d.slot_row ? ` · Row ${d.slot_row}` : ""}${d.slot_number ? ` · Slot ${d.slot_number}` : ""}` + photoNote : "Not in a box." + photoNote,
    });
    setCount(count + 1);
    setStep("done");
  }

  function again() {
    setFront(null);
    setBack(null);
    setForm(EMPTY);
    setFlags(FLAGS);
    setAdded(null);
    setNotes("");
    setError("");
    setStep("photos");
    setTimeout(() => frontInput.current?.click(), 200);
  }

  const set = (k: keyof Form) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) => setForm({ ...form, [k]: e.target.value });
  const cats = tcg ? TCG : SPORTS;

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-end gap-3">
        {editions.length > 1 && (
          <label className="text-[13px] font-semibold">
            App
            <select className="field !w-auto" value={edition} onChange={(e) => setEdition(e.target.value)} disabled={step !== "photos" && step !== "done"}>
              {editions.map((e) => <option key={e} value={e}>{e === "tcg" ? "TCG" : "Sports"}</option>)}
            </select>
          </label>
        )}
      </div>
      <p className="text-[13px] text-muted">
        Each card is filed for you, the same way FireCardManager does it: into its {tcg ? "category" : "sport"}&apos;s box, right after the last card
        (a new row after 50, a new box when one is full). To move a card, use FireCardManager on your PC.
      </p>

      {step === "done" && added && (
        <div className="card space-y-3 !border-go">
          <p className="text-[18px] font-bold">Added {added.name}</p>
          <p className="text-[14px] text-muted">{added.where} Your PC picks it up on its next sync.</p>
          <div className="flex flex-wrap gap-2">
            <button type="button" className="btn" onClick={again}>Add another card</button>
            <Link href={`/inventory/cards/${added.id}`} className="btn-ghost">Open card</Link>
          </div>
          {count > 1 && <p className="text-[12px] text-muted">{count} cards added this session.</p>}
        </div>
      )}

      {(step === "photos" || step === "identifying") && (
        <>
          <div className="grid grid-cols-2 gap-3">
            {(["front", "back"] as const).map((side) => {
              const shot = side === "front" ? front : back;
              return (
                <button key={side} type="button" onClick={() => (side === "front" ? frontInput : backInput).current?.click()}
                  className="flex aspect-[5/7] w-full flex-col items-center justify-center overflow-hidden rounded-xl border-2 border-dashed border-line bg-wash text-muted hover:border-fire">
                  {shot ? (
                    // eslint-disable-next-line @next/next/no-img-element
                    <img src={shot.preview} alt={`${side} photo`} className="h-full w-full object-contain" />
                  ) : (
                    <>
                      <span className="text-[32px]" aria-hidden>📷</span>
                      <span className="mt-1 font-semibold">{side === "front" ? "Front" : "Back"}</span>
                      <span className="text-[12px]">Tap to take a photo</span>
                    </>
                  )}
                </button>
              );
            })}
          </div>
          <input ref={frontInput} type="file" accept="image/*" capture="environment" hidden onChange={(e) => take("front", e.target.files?.[0])} />
          <input ref={backInput} type="file" accept="image/*" capture="environment" hidden onChange={(e) => take("back", e.target.files?.[0])} />
          <p className="text-[12px] text-muted">Tip: lay the card on a plain, dark surface and fill the frame. Avoid glare on slabs.</p>
          {error && <p role="alert" className="text-bad">{error}</p>}
          <div className="flex flex-wrap gap-2">
            <button type="button" className="btn" disabled={!front || !back || step === "identifying" || !hasKey} onClick={identify}>
              {step === "identifying" ? "Identifying..." : "Identify card"}
            </button>
            <button type="button" className="btn-ghost" disabled={step === "identifying"} onClick={manual}>Type it in myself</button>
          </div>
          {!hasKey && (
            <p className="text-[13px] text-muted">
              To identify cards automatically, add your Claude or Gemini key on the <Link href="/account#ai-key" className="font-semibold text-fire">Account page</Link>{" "}
              (or link a PC that has one in FireCardManager).
            </p>
          )}
        </>
      )}

      {(step === "review" || step === "saving") && (
        <form onSubmit={save} className="card space-y-4">
          <div className="flex items-start gap-3">
            {front && (
              // eslint-disable-next-line @next/next/no-img-element
              <img src={front.preview} alt="" className="h-28 w-20 shrink-0 rounded object-contain" />
            )}
            <div className="min-w-0">
              <h2 className="text-[18px] font-bold">Check the details</h2>
              {notes ? <p className="mt-1 text-[13px] text-muted">AI notes: {notes}</p> : <p className="mt-1 text-[13px] text-muted">Fix anything that&apos;s wrong, then save.</p>}
            </div>
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <label className="block text-[13px] font-semibold sm:col-span-2">
              Player / name
              <input className="field" value={form.player_name} onChange={set("player_name")} required />
              {fieldErrors.player_name && <span className="text-bad">{fieldErrors.player_name}</span>}
            </label>
            <label className="block text-[13px] font-semibold">
              {tcg ? "Category" : "Sport"} <span className="font-normal text-muted">(picks the box)</span>
              <select className="field" value={form.sport} onChange={set("sport")} required>
                <option value="">Choose...</option>
                {cats.map((c) => <option key={c} value={c}>{c}</option>)}
              </select>
            </label>
            <label className="block text-[13px] font-semibold">{tcg ? "Franchise / series" : "Team"}<input className="field" value={form.team} onChange={set("team")} /></label>
            <div className="grid grid-cols-2 gap-3">
              <label className="block text-[13px] font-semibold">
                Year<input className="field num" inputMode="numeric" value={form.year} onChange={set("year")} required />
                {fieldErrors.year && <span className="text-bad">{fieldErrors.year}</span>}
              </label>
              <label className="block text-[13px] font-semibold">Season end<input className="field num" inputMode="numeric" value={form.season_end} onChange={set("season_end")} placeholder="optional" /></label>
            </div>
            <label className="block text-[13px] font-semibold">Brand<input className="field" value={form.brand} onChange={set("brand")} required /></label>
            <label className="block text-[13px] font-semibold">Set<input className="field" value={form.set_name} onChange={set("set_name")} required /></label>
            <label className="block text-[13px] font-semibold">Card number<input className="field" value={form.card_number} onChange={set("card_number")} required /></label>
            <label className="block text-[13px] font-semibold">Parallel / variety<input className="field" value={form.variety} onChange={set("variety")} /></label>
            <label className="block text-[13px] font-semibold">Serial number<input className="field" value={form.serial_number} onChange={set("serial_number")} placeholder="e.g. 12/99" /></label>
            <label className="block text-[13px] font-semibold">Condition<input className="field" value={form.condition} onChange={set("condition")} required /></label>
            {form.condition.toLowerCase().startsWith("graded") && (
              <div className="grid grid-cols-2 gap-3">
                <label className="block text-[13px] font-semibold">Company<input className="field" value={form.grading_company} onChange={set("grading_company")} /></label>
                <label className="block text-[13px] font-semibold">Grade<input className="field" value={form.card_grade} onChange={set("card_grade")} /></label>
              </div>
            )}
            <label className="block text-[13px] font-semibold">
              Estimated value<input className="field num" inputMode="decimal" value={form.estimated_value} onChange={set("estimated_value")} />
            </label>
            <label className="block text-[13px] font-semibold">
              Price<input className="field num" inputMode="decimal" value={form.sale_price} onChange={set("sale_price")} />
              {form.estimated_value && !form.sale_price && <span className="font-normal text-muted">Estimate {money(Number(form.estimated_value))}</span>}
            </label>
            <label className="block text-[13px] font-semibold">
              Status
              <select className="field" value={form.status} onChange={set("status")}>
                <option value="Ready">Ready to list</option>
                <option value="NotForSale">Not for sale</option>
              </select>
            </label>
          </div>
          <div className="flex flex-wrap gap-x-5 gap-y-2 text-[14px]">
            {([["is_rookie", tcg ? "Promo" : "Rookie (RC)"], ["is_autograph", "Autograph"], ["is_memorabilia", "Memorabilia"], ["is_short_print", "Short print"],
              ...(tcg ? [["is_chase", "Chase"], ["is_foil", "Foil"], ["is_sketch_card", "Sketch"]] : [])] as [keyof typeof FLAGS, string][]).map(([k, label]) => (
              <label key={k} className="flex items-center gap-2">
                <input type="checkbox" checked={flags[k]} onChange={(e) => setFlags({ ...flags, [k]: e.target.checked })} /> {label}
              </label>
            ))}
          </div>
          {Object.entries(fieldErrors).filter(([k]) => !["player_name", "year"].includes(k)).map(([k, v]) => (
            <p key={k} className="text-[13px] text-bad">{k.replace(/_/g, " ")}: {v}</p>
          ))}
          {error && <p role="alert" className="text-bad">{error}</p>}
          <div className="flex flex-wrap gap-2">
            <button type="submit" className="btn" disabled={step === "saving"}>{step === "saving" ? progress : "Save card"}</button>
            <button type="button" className="btn-ghost" disabled={step === "saving"} onClick={() => setStep("photos")}>Back to photos</button>
          </div>
        </form>
      )}
    </div>
  );
}
