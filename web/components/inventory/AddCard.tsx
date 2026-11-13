"use client";

import Link from "next/link";
import { useRef, useState } from "react";
import { send } from "@/lib/client";
import { money } from "@/lib/inventory";
import { identify as identifyCard, makeShot, SPORTS, TCG, uploadPhotos, whereText, type Created, type Shot,
  EMPTY_FORM, NO_FLAGS, fillForm, toFields, type CardForm, type Extra } from "@/lib/addcard";

type Step = "photos" | "identifying" | "review" | "saving" | "done";
const EMPTY = EMPTY_FORM;
type Form = CardForm;
const FLAGS = NO_FLAGS;

export default function AddCard({ editions, hasKey }: { editions: string[]; hasKey: boolean }) {
  const [edition, setEdition] = useState(editions[0] ?? "sports");
  const [step, setStep] = useState<Step>("photos");
  const [front, setFront] = useState<Shot | null>(null);
  const [back, setBack] = useState<Shot | null>(null);
  const [form, setForm] = useState<Form>(EMPTY);
  const [flags, setFlags] = useState(FLAGS);
  const [extra, setExtra] = useState<Extra>({});
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
      const shot = await makeShot(file);
      if (side === "front") {
        setFront(shot);
        if (!back) setTimeout(() => backInput.current?.click(), 300); // straight on to the back
      } else setBack(shot);
    } catch {
      setError("That photo couldn't be read. Try taking it again.");
    }
  }

  function fill(fields: Record<string, unknown>) {
    const f = fillForm(fields);
    setForm(f.form);
    setFlags(f.flags);
    setExtra(f.extra);
  }

  async function identify() {
    if (!front || !back) return;
    setStep("identifying");
    setError("");
    const r = await identifyCard(edition, front, back);
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
    const { fields, bad } = toFields(form, flags, extra, tcg);
    if (Object.keys(bad).length) return setFieldErrors(bad);

    setStep("saving");
    setError("");
    setFieldErrors({});
    setProgress("Saving the card...");
    const c = await send<Created>("POST", "/v1/inventory/cards", { edition, fields });
    if (!c.ok) {
      setStep("review");
      setError(c.message);
      setFieldErrors(c.fields);
      return;
    }
    let photoNote = "";
    if (front && back) {
      setProgress("Uploading photos...");
      photoNote = await uploadPhotos(c.data.id, front, back);
    }
    setAdded({ id: c.data.id, name: c.data.data.player_name ?? "Card", where: whereText(c.data) + "." + (photoNote ? " " + photoNote : "") });
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
