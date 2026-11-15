"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { send } from "@/lib/client";
import { KIND_LABEL, kindsFor, type CardKind } from "@/components/inventory/CardKindPicker";
import type { TreeBox } from "@/lib/inventory";

/** The desktop's box sizes (Services.Boxes.BoxRules). */
export const SIZES: Record<CardKind, number[]> = {
  raw: [100, 200, 400, 500, 800, 1000, 1300, 1600, 2000, 3200, 5000],
  relic: [50, 100, 150, 200, 300, 400, 500, 800],
  slab: [10, 15, 20, 25, 30, 40, 50, 100],
};
const SUGGESTED: Record<CardKind, number> = { raw: 800, relic: 200, slab: 25 };
const HINT: Record<CardKind, string> = {
  raw: "Ordinary cards, sleeved or in toploaders. An 800-count box holds about 700 penny-sleeved cards.",
  relic: "Thick patch and relic cards, booklets and oversized cards. Toploader boxes hold about 150 to 180.",
  slab: "PSA, BGS, SGC and other graded slabs. Slab boxes usually hold 10 to 50.",
};

export type Sizes = { raw: number | null; relic: number | null; slab: number | null; fill_sold: boolean; chosen: boolean };

const num = (s: string) => Number(s.replace(/[^\d]/g, ""));
const where = (p: number) => `row ${letter(Math.floor((p - 1) / 50))} slot ${String(((p - 1) % 50) + 1).padStart(2, "0")}`;
function letter(i: number) {
  let n = i + 1, s = "";
  while (n > 0) { n--; s = String.fromCharCode(65 + (n % 26)) + s; n = Math.floor(n / 26); }
  return s;
}

/** Box sizes for new boxes of each type, and the "fill sold slots" choice. */
export function BoxSizesForm({ edition, initial }: { edition: string; initial: Sizes }) {
  const router = useRouter();
  const tcg = edition === "tcg";
  const [v, setV] = useState<Record<CardKind, string>>({
    raw: String(initial.raw ?? SUGGESTED.raw), relic: String(initial.relic ?? SUGGESTED.relic), slab: String(initial.slab ?? SUGGESTED.slab),
  });
  const [fill, setFill] = useState(initial.fill_sold);
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState("");
  const [error, setError] = useState("");

  async function save(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setMsg("");
    setBusy(true);
    const r = await send("PUT", "/v1/inventory/box-sizes", {
      edition, raw: num(v.raw), relic: tcg ? null : num(v.relic), slab: num(v.slab), fill_sold: fill,
    });
    setBusy(false);
    if (!r.ok) return setError(r.message);
    setMsg("Saved. New boxes use these sizes.");
    router.refresh();
  }

  return (
    <form onSubmit={save} className="space-y-3 text-[14px]">
      <p className="text-[13px] text-muted">
        Pick the size of the boxes you store each type of card in. When a box is full, the next card of that type starts a new box. Rows stay 50 cards,
        so the size sets how many rows a box has (an 800-count box is rows A to P). Boxes you already have keep their size; change one with its settings below.
      </p>
      {kindsFor(tcg).map((k) => (
        <label key={k} className="grid gap-1 sm:grid-cols-[1fr_140px] sm:items-center">
          <span>
            <span className="font-semibold">{KIND_LABEL[k]}</span>
            <span className="block text-[12px] text-muted">{HINT[k]}</span>
          </span>
          <span>
            <input className="field num !mt-0" inputMode="numeric" list={`sizes-${k}`} value={v[k]} onChange={(e) => setV({ ...v, [k]: e.target.value })} />
            <datalist id={`sizes-${k}`}>{SIZES[k].map((n) => <option key={n} value={n} />)}</datalist>
          </span>
        </label>
      ))}
      <label className="flex items-start gap-2">
        <input type="checkbox" className="mt-1" checked={fill} onChange={(e) => setFill(e.target.checked)} />
        <span>
          <span className="font-semibold">Fill empty slots left by sold cards</span>
          <span className="block text-[12px] text-muted">Off (recommended): new cards always go after the last card in the box, so a stack stays together in order.</span>
        </span>
      </label>
      {error && <p role="alert" className="text-bad">{error}</p>}
      {msg && <p className="text-go">{msg}</p>}
      <button className="btn" disabled={busy}>{busy ? "Saving..." : "Save box sizes"}</button>
    </form>
  );
}

/** One box: its size (up any time, down only to its last card) and Close box / Reopen box. */
export function BoxSettings({ box }: { box: TreeBox }) {
  const router = useRouter();
  const [size, setSize] = useState(String(box.capacity ?? 1300));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const filled = box.filled_to ?? 0;
  const kind = (box.kind ?? "raw") as CardKind;
  const unsorted = box.name.toLowerCase().startsWith("unsorted");

  async function patch(body: Record<string, unknown>) {
    setError("");
    setBusy(true);
    const r = await send("PATCH", `/v1/inventory/boxes/${box.id}`, body);
    setBusy(false);
    if (!r.ok) return setError(r.message);
    router.refresh();
  }

  return (
    <details className="card mb-3 !p-3 text-[14px]">
      <summary className="cursor-pointer font-semibold">
        Box settings <span className="font-normal text-muted">
          · {KIND_LABEL[kind] ?? kind} · {filled ? `filled to ${where(filled)}` : "empty"} · holds {(box.capacity ?? 1300).toLocaleString()}
          {box.closed ? " · closed" : ""}
        </span>
      </summary>
      <div className="mt-3 grid gap-4 sm:grid-cols-2">
        <form onSubmit={(e) => { e.preventDefault(); patch({ capacity: num(size) }); }} className="space-y-1">
          <label className="block font-semibold">Box size (cards)
            <span className="mt-1 flex gap-2">
              <input className="field num !mt-0" inputMode="numeric" list={`box-sizes-${box.id}`} value={size} onChange={(e) => setSize(e.target.value)} />
              <datalist id={`box-sizes-${box.id}`}>{[...new Set([...SIZES[kind], ...SIZES.raw])].filter((n) => n >= filled).sort((a, b) => a - b).map((n) => <option key={n} value={n} />)}</datalist>
              <button className="btn-ghost !py-1.5" disabled={busy}>Save</button>
            </span>
          </label>
          <p className="text-[12px] text-muted">
            {filled ? `Can't go below ${filled.toLocaleString()} (its last card is at ${where(filled)}). To make it smaller, move cards out in FireCardManager first.`
              : "Rows are 50 cards each, so the size sets how many rows it has."}
            {box.older ? " This box was made before box sizes (1,300 = rows A to Z)." : ""}
          </p>
        </form>
        <div className="space-y-1">
          <p className="font-semibold">{box.closed ? "This box is closed" : "Close this box"}</p>
          <p className="text-[12px] text-muted">
            {box.closed ? "No new cards go in. Reopen it to keep filling it after its last card."
              : "When the real box is full early, close it: no new cards go in, and the next card of this type starts a new box."}
          </p>
          <button type="button" className="btn-ghost !py-1.5" disabled={busy || unsorted} onClick={() => patch({ closed: !box.closed })}>
            {box.closed ? "Reopen box" : "Close box"}
          </button>
        </div>
      </div>
      {error && <p role="alert" className="mt-2 text-bad">{error}</p>}
    </details>
  );
}
