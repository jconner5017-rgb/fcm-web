"use client";

import Link from "next/link";
import { useRef, useState } from "react";
import { send } from "@/lib/client";
import { money } from "@/lib/inventory";
import {
  byName, fillForm, identify, makeShot, SPORTS, TCG, toFields, uploadPhotos, whereText,
  type CardForm, type Created, type Extra, type Flags, type Shot,
} from "@/lib/addcard";

const MAX_CARDS = 100;

type Photo = { name: string; shot: Shot };
type Item = {
  front: Photo;
  back: Photo;
  state: "waiting" | "identifying" | "ready" | "failed" | "saving" | "saved" | "save_failed";
  form?: CardForm;
  flags?: Flags;
  extra?: Extra;
  notes?: string;
  error?: string;
  include: boolean;
  cardId?: string;
  where?: string;
};
type Phase = "pick" | "identify" | "review" | "saving" | "done";

/** Runs fn over the list, a few at a time. */
async function pool<T>(list: T[], size: number, fn: (t: T) => Promise<void>) {
  let i = 0;
  await Promise.all(Array.from({ length: Math.min(size, list.length) }, async () => {
    while (i < list.length) await fn(list[i++]);
  }));
}

export default function BatchUpload({ editions, hasKey }: { editions: string[]; hasKey: boolean }) {
  const [edition, setEdition] = useState(editions[0] ?? "sports");
  const [frontFirst, setFrontFirst] = useState(true);
  const [sortNames, setSortNames] = useState(false);
  const [photos, setPhotos] = useState<Photo[]>([]);
  const [swapped, setSwapped] = useState<Set<number>>(new Set());
  const [items, setItems] = useState<Item[]>([]);
  const [phase, setPhase] = useState<Phase>("pick");
  const [preparing, setPreparing] = useState("");
  const [error, setError] = useState("");
  const input = useRef<HTMLInputElement>(null);
  const tcg = edition === "tcg";
  const cats = tcg ? TCG : SPORTS;

  // Photos in the order picked (or by file name), paired two at a time.
  const ordered = sortNames ? [...photos].sort((a, b) => byName({ name: a.name } as File, { name: b.name } as File)) : photos;
  const pairs: { front: Photo; back: Photo }[] = [];
  for (let i = 0; i + 1 < ordered.length; i += 2) {
    let [front, back] = frontFirst ? [ordered[i], ordered[i + 1]] : [ordered[i + 1], ordered[i]];
    if (swapped.has(i / 2)) [front, back] = [back, front];
    pairs.push({ front, back });
  }
  const leftover = ordered.length % 2 === 1 ? ordered[ordered.length - 1] : null;

  async function add(list: FileList | null) {
    if (!list || list.length === 0) return;
    setError("");
    const files = [...list];
    const room = MAX_CARDS * 2 - photos.length;
    if (files.length > room) setError(`Up to ${MAX_CARDS} cards (${MAX_CARDS * 2} photos) at a time. The extra photos were left out.`);
    const take = files.slice(0, Math.max(0, room));
    const out: Photo[] = [];
    for (let i = 0; i < take.length; i++) {
      setPreparing(`Preparing photo ${i + 1} of ${take.length}...`);
      try {
        out.push({ name: take[i].name, shot: await makeShot(take[i]) });
      } catch {
        setError(`${take[i].name} couldn't be read, so it was left out. Check the pairs below.`);
      }
    }
    setPreparing("");
    setPhotos((p) => [...p, ...out]);
    if (input.current) input.current.value = "";
  }

  function removePair(n: number) {
    const drop = new Set([ordered[n * 2], ordered[n * 2 + 1]]);
    setPhotos(photos.filter((p) => !drop.has(p)));
    setSwapped(new Set());
  }

  function swap(n: number) {
    const s = new Set(swapped);
    if (s.has(n)) s.delete(n);
    else s.add(n);
    setSwapped(s);
  }

  function update(n: number, change: Partial<Item>) {
    setItems((list) => list.map((it, i) => (i === n ? { ...it, ...change } : it)));
  }

  async function start(useAI: boolean) {
    setError("");
    const list: Item[] = pairs.map((p) => ({ ...p, state: useAI ? "waiting" : "ready", include: true, ...(useAI ? {} : fillForm({})) }));
    setItems(list);
    if (!useAI) return setPhase("review");
    setPhase("identify");
    let stop = "";
    await pool(list.map((_, i) => i), 3, async (n) => {
      if (stop) return update(n, { state: "failed", error: stop, ...fillForm({}) });
      update(n, { state: "identifying" });
      const r = await identify(edition, list[n].front.shot, list[n].back.shot);
      if (r.ok) return update(n, { state: "ready", notes: r.data.notes, ...fillForm(r.data.fields) });
      // A missing key or the hourly limit stops the rest; one bad photo doesn't.
      if (["no_ai_key", "too_many", "ai_key_rejected"].includes(r.code) || r.status === 402 || r.status === 401) stop = r.message;
      update(n, { state: "failed", error: r.message, ...fillForm({}) });
    });
    setPhase("review");
  }

  async function retry(n: number) {
    const it = items[n];
    update(n, { state: "identifying", error: "" });
    const r = await identify(edition, it.front.shot, it.back.shot);
    if (r.ok) update(n, { state: "ready", notes: r.data.notes, ...fillForm(r.data.fields) });
    else update(n, { state: "failed", error: r.message });
  }

  async function saveAll() {
    setError("");
    const todo = items.map((it, i) => ({ it, i })).filter(({ it }) => it.include && it.state !== "saved");
    // Check every card first, so nothing is half saved because of a typo.
    let problems = 0;
    for (const { it, i } of todo) {
      const { bad } = toFields(it.form!, it.flags!, it.extra ?? {}, tcg);
      if (!it.form!.player_name.trim()) bad.player_name = "Enter a name.";
      if (!it.form!.sport) bad.sport = tcg ? "Pick a category." : "Pick a sport.";
      if (Object.keys(bad).length) {
        problems++;
        update(i, { error: Object.entries(bad).map(([k, v]) => `${k.replace(/_/g, " ")}: ${v}`).join(" · ") });
      } else if (it.error && it.state !== "failed") update(i, { error: "" });
    }
    if (problems) return setError(`Fix the ${problems} card${problems === 1 ? "" : "s"} marked in red, then save again.`);
    setPhase("saving");
    // One at a time, in order, so the cards are filed in the same order as the photos.
    for (const { it, i } of todo) {
      update(i, { state: "saving", error: "" });
      const { fields } = toFields(it.form!, it.flags!, it.extra ?? {}, tcg);
      const c = await send<Created>("POST", "/v1/inventory/cards", { edition, fields });
      if (!c.ok) {
        update(i, { state: "save_failed", error: c.message + (Object.keys(c.fields).length ? " " + Object.values(c.fields).join(" ") : "") });
        if (c.status === 402 || c.status === 401) break;
        continue;
      }
      const note = await uploadPhotos(c.data.id, it.front.shot, it.back.shot);
      update(i, { state: "saved", cardId: c.data.id, where: whereText(c.data) + (note ? `. ${note}` : "") });
    }
    setPhase("done");
  }

  function startOver() {
    setPhotos([]);
    setItems([]);
    setSwapped(new Set());
    setError("");
    setPhase("pick");
  }

  const included = items.filter((it) => it.include && it.state !== "saved").length;
  const saved = items.filter((it) => it.state === "saved").length;
  const identified = items.filter((it) => it.state !== "waiting" && it.state !== "identifying").length;

  return (
    <div className="space-y-5">
      {phase === "pick" && (
        <>
          <div className="card space-y-3">
            <div className="flex flex-wrap items-end gap-4">
              {editions.length > 1 && (
                <label className="text-[13px] font-semibold">
                  App
                  <select className="field !w-auto" value={edition} onChange={(e) => setEdition(e.target.value)}>
                    {editions.map((e) => <option key={e} value={e}>{e === "tcg" ? "TCG" : "Sports"}</option>)}
                  </select>
                </label>
              )}
              <fieldset className="text-[14px]">
                <legend className="text-[13px] font-semibold">Photo order</legend>
                <div className="mt-1 flex flex-wrap gap-4">
                  <label className="flex items-center gap-2"><input type="radio" checked={frontFirst} onChange={() => setFrontFirst(true)} /> Front, then back</label>
                  <label className="flex items-center gap-2"><input type="radio" checked={!frontFirst} onChange={() => setFrontFirst(false)} /> Back, then front</label>
                </div>
              </fieldset>
              <label className="flex items-center gap-2 text-[14px]">
                <input type="checkbox" checked={sortNames} onChange={(e) => setSortNames(e.target.checked)} /> Sort by file name
              </label>
            </div>
            <p className="text-[13px] text-muted">
              Pick the photos in order, two per card: {frontFirst ? "front then back" : "back then front"}, then the next card. On a phone, tap them in
              that order. Photos from a scanner folder are usually already in order: tick <b>Sort by file name</b>.
            </p>
            <div className="flex flex-wrap items-center gap-2">
              <button type="button" className="btn" disabled={!!preparing} onClick={() => input.current?.click()}>
                {photos.length ? "Add more photos" : "Choose photos"}
              </button>
              {photos.length > 0 && <button type="button" className="btn-ghost" onClick={startOver}>Clear</button>}
              {preparing && <span className="text-[13px] text-muted">{preparing}</span>}
            </div>
            <input ref={input} type="file" accept="image/*" multiple hidden onChange={(e) => add(e.target.files)} />
          </div>

          {error && <p role="alert" className="text-bad">{error}</p>}

          {pairs.length > 0 && (
            <>
              <div className="flex flex-wrap items-center gap-3">
                <h2 className="text-[18px] font-bold">{pairs.length} card{pairs.length === 1 ? "" : "s"}</h2>
                <span className="text-[13px] text-muted">Check each pair. Use Swap if a front and back are the wrong way round.</span>
              </div>
              <ul className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
                {pairs.map((p, n) => (
                  <li key={p.front.name + n} className="card !p-2">
                    <div className="grid grid-cols-2 gap-1">
                      {[["Front", p.front], ["Back", p.back]].map(([label, ph]) => (
                        <figure key={label as string}>
                          {/* eslint-disable-next-line @next/next/no-img-element */}
                          <img src={(ph as Photo).shot.preview} alt={`${label} of card ${n + 1}`} className="aspect-[5/7] w-full rounded bg-wash object-contain" />
                          <figcaption className="mt-0.5 text-center text-[11px] font-semibold text-muted">{label as string}</figcaption>
                        </figure>
                      ))}
                    </div>
                    <div className="mt-1 flex items-center justify-between text-[12px]">
                      <span className="font-semibold">#{n + 1}</span>
                      <span className="flex gap-3">
                        <button type="button" className="font-semibold text-link" onClick={() => swap(n)}>Swap</button>
                        <button type="button" className="text-muted hover:text-bad" onClick={() => removePair(n)}>Remove</button>
                      </span>
                    </div>
                  </li>
                ))}
              </ul>
            </>
          )}
          {leftover && (
            <p className="text-[13px] text-bad">
              One photo has no partner ({leftover.name}), so it&apos;s left out. Every card needs a front and a back.
            </p>
          )}

          {pairs.length > 0 && (
            <div className="space-y-2">
              <div className="flex flex-wrap gap-2">
                <button type="button" className="btn" disabled={!hasKey || !!preparing} onClick={() => start(true)}>
                  Identify {pairs.length} card{pairs.length === 1 ? "" : "s"}
                </button>
                <button type="button" className="btn-ghost" disabled={!!preparing} onClick={() => start(false)}>Type them in myself</button>
              </div>
              {!hasKey && (
                <p className="text-[13px] text-muted">
                  To identify cards automatically, add your Claude or Gemini key on the{" "}
                  <Link href="/account#ai-key" className="font-semibold text-fire">Account page</Link> (or link a PC that has one in FireCardManager).
                </p>
              )}
            </div>
          )}
        </>
      )}

      {phase === "identify" && (
        <div className="card">
          <p className="font-semibold">Identifying cards... {identified} of {items.length}</p>
          <div className="mt-2 h-2 overflow-hidden rounded-full bg-wash">
            <div className="h-full bg-fire transition-all" style={{ width: `${(identified / Math.max(1, items.length)) * 100}%` }} />
          </div>
          <p className="mt-2 text-[13px] text-muted">Keep this page open. Each card takes a few seconds with your AI key.</p>
        </div>
      )}

      {(phase === "review" || phase === "saving" || phase === "done") && (
        <>
          <div className="flex flex-wrap items-center gap-3">
            <h2 className="text-[18px] font-bold">
              {phase === "done" ? `Saved ${saved} of ${items.length} card${items.length === 1 ? "" : "s"}` : "Check the details"}
            </h2>
            {phase === "review" && <span className="text-[13px] text-muted">Fix anything that&apos;s wrong. Untick a card to leave it out.</span>}
          </div>
          {phase === "done" && (
            <p className="text-[14px] text-muted">
              Each card was filed into the next free row and slot of its {tcg ? "category" : "sport"}&apos;s box. Your PC picks them up on its next sync.
            </p>
          )}

          <ul className="space-y-3">
            {items.map((it, n) => (
              <BatchRow key={n} n={n} it={it} tcg={tcg} cats={cats} locked={phase !== "review"}
                onChange={(c) => update(n, c)} onRetry={() => retry(n)} />
            ))}
          </ul>

          {error && <p role="alert" className="text-bad">{error}</p>}
          <div className="sticky bottom-0 -mx-4 flex flex-wrap items-center gap-2 border-t border-line bg-paper px-4 py-3">
            {phase === "review" && (
              <button type="button" className="btn" disabled={included === 0} onClick={saveAll}>
                Save {included} card{included === 1 ? "" : "s"}
              </button>
            )}
            {phase === "saving" && <span className="font-semibold">Saving cards... {saved} of {saved + included}</span>}
            {phase === "done" && (
              <>
                {included > 0 && <button type="button" className="btn" onClick={() => setPhase("review")}>Fix the {included} not saved</button>}
                <button type="button" className={included > 0 ? "btn-ghost" : "btn"} onClick={startOver}>Upload more cards</button>
                <Link href="/inventory?sort=added&dir=desc" className="btn-ghost">See them in inventory</Link>
              </>
            )}
            {phase === "review" && <button type="button" className="btn-ghost" onClick={startOver}>Start over</button>}
          </div>
        </>
      )}
    </div>
  );
}

function BatchRow({ n, it, tcg, cats, locked, onChange, onRetry }: {
  n: number; it: Item; tcg: boolean; cats: string[]; locked: boolean; onChange: (c: Partial<Item>) => void; onRetry: () => void;
}) {
  const f = it.form;
  const set = (k: keyof CardForm) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) => onChange({ form: { ...f!, [k]: e.target.value }, error: "" });
  const busy = it.state === "identifying" || it.state === "waiting";
  const done = it.state === "saved";
  const dim = !it.include && !done;

  return (
    <li className={`card !p-3 ${it.error ? "!border-bad" : done ? "!border-go" : ""} ${dim ? "opacity-50" : ""}`}>
      <div className="flex gap-3">
        <div className="flex shrink-0 flex-col gap-1">
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img src={it.front.shot.preview} alt={`Front of card ${n + 1}`} className="h-24 w-[68px] rounded bg-wash object-contain" />
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img src={it.back.shot.preview} alt={`Back of card ${n + 1}`} className="h-24 w-[68px] rounded bg-wash object-contain" />
        </div>
        <div className="min-w-0 flex-1 space-y-2">
          <div className="flex items-center gap-2 text-[13px]">
            {!done && (
              <input type="checkbox" checked={it.include} disabled={locked} onChange={(e) => onChange({ include: e.target.checked })} aria-label={`Include card ${n + 1}`} />
            )}
            <span className="font-bold">#{n + 1}</span>
            {busy && <span className="text-muted">Identifying...</span>}
            {it.state === "saving" && <span className="text-muted">Saving...</span>}
            {done && (
              <span className="text-go">
                Saved · {it.where}{" "}
                <Link href={`/inventory/cards/${it.cardId}`} className="font-semibold text-link">Open</Link>
              </span>
            )}
            {it.state === "failed" && !locked && (
              <button type="button" className="ml-auto font-semibold text-link" onClick={onRetry}>Try identifying again</button>
            )}
          </div>
          {it.error && <p className="text-[13px] text-bad">{it.error}</p>}
          {it.notes && !done && <p className="text-[12px] text-muted">AI notes: {it.notes}</p>}

          {f && !busy && !done && (
            <fieldset disabled={locked} className="grid grid-cols-2 gap-2 text-[12px] sm:grid-cols-4">
              <label className="col-span-2 block font-semibold">Player / name<input className="field !mt-0.5 !py-1.5" value={f.player_name} onChange={set("player_name")} /></label>
              <label className="block font-semibold">
                {tcg ? "Category" : "Sport"}
                <select className="field !mt-0.5 !py-1.5" value={f.sport} onChange={set("sport")}>
                  <option value="">Choose...</option>
                  {cats.map((c) => <option key={c} value={c}>{c}</option>)}
                </select>
              </label>
              <label className="block font-semibold">Year<input className="field num !mt-0.5 !py-1.5" inputMode="numeric" value={f.year} onChange={set("year")} /></label>
              <label className="block font-semibold">Brand<input className="field !mt-0.5 !py-1.5" value={f.brand} onChange={set("brand")} /></label>
              <label className="block font-semibold">Set<input className="field !mt-0.5 !py-1.5" value={f.set_name} onChange={set("set_name")} /></label>
              <label className="block font-semibold">Number<input className="field !mt-0.5 !py-1.5" value={f.card_number} onChange={set("card_number")} /></label>
              <label className="block font-semibold">Parallel<input className="field !mt-0.5 !py-1.5" value={f.variety} onChange={set("variety")} /></label>
              <label className="block font-semibold">Condition<input className="field !mt-0.5 !py-1.5" value={f.condition} onChange={set("condition")} /></label>
              <label className="block font-semibold">
                Price<input className="field num !mt-0.5 !py-1.5" inputMode="decimal" value={f.sale_price} onChange={set("sale_price")} />
                {f.estimated_value && <span className="font-normal text-muted">Est. {money(Number(f.estimated_value))}</span>}
              </label>
              <label className="block font-semibold">
                Status
                <select className="field !mt-0.5 !py-1.5" value={f.status} onChange={set("status")}>
                  <option value="Ready">Ready to list</option>
                  <option value="NotForSale">Not for sale</option>
                </select>
              </label>
              <div className="col-span-2 flex flex-wrap items-end gap-x-3 gap-y-1 text-[12px] sm:col-span-4">
                {([["is_rookie", tcg ? "Promo" : "RC"], ["is_autograph", "Auto"], ["is_memorabilia", "Relic"], ["is_short_print", "SP"],
                  ...(tcg ? [["is_chase", "Chase"], ["is_foil", "Foil"]] : [])] as [keyof Flags, string][]).map(([k, label]) => (
                  <label key={k} className="flex items-center gap-1 font-normal">
                    <input type="checkbox" checked={!!it.flags?.[k]} onChange={(e) => onChange({ flags: { ...it.flags!, [k]: e.target.checked } })} /> {label}
                  </label>
                ))}
              </div>
            </fieldset>
          )}
        </div>
      </div>
    </li>
  );
}
