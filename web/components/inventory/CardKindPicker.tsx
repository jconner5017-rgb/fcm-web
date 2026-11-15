"use client";

/** What kind of cards these are - it picks the type of box they're filed in
 *  (the same question FireCardManager asks before every scan). */
export type CardKind = "raw" | "relic" | "slab";

export const KIND_LABEL: Record<CardKind, string> = {
  raw: "Raw cards (toploaders too)",
  relic: "Memorabilia, relic or oversized",
  slab: "Slabs (graded cards)",
};

export function kindsFor(tcg: boolean): CardKind[] {
  return tcg ? ["raw", "slab"] : ["raw", "relic", "slab"];
}

export default function CardKindPicker({ value, onChange, tcg, many }: { value: CardKind; onChange: (k: CardKind) => void; tcg: boolean; many?: boolean }) {
  return (
    <fieldset className="text-[14px]">
      <legend className="text-[13px] font-semibold">{many ? "What kind of cards are these?" : "Card type"} <span className="font-normal text-muted">(picks the box)</span></legend>
      <div className="mt-1 flex flex-wrap gap-x-4 gap-y-1">
        {kindsFor(tcg).map((k) => (
          <label key={k} className="flex items-center gap-2">
            <input type="radio" checked={value === k} onChange={() => onChange(k)} /> {KIND_LABEL[k]}
          </label>
        ))}
      </div>
      {many && (
        <p className="mt-2 rounded-lg border border-bad px-3 py-2 text-[13px] font-semibold">
          Don&apos;t mix types in one batch: every card is filed as the type you pick, so a slab mixed in with raw cards would go in a raw-card box.
          Do {tcg ? "raw cards and slabs" : "raw cards, memorabilia/relic cards and slabs"} as separate batches.
        </p>
      )}
    </fieldset>
  );
}
