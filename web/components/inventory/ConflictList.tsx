"use client";

import Link from "next/link";
import { useState } from "react";
import { send, when } from "@/lib/client";
import { STATUS_LABELS } from "@/lib/inventory";

export type Conflict = {
  id: number;
  entity: "card" | "sold" | "box";
  entity_id: string;
  name: string;
  field: string;
  kept: unknown;
  lost: unknown;
  current: unknown;
  lost_device?: string;
  lost_at?: string;
  created_at: string;
  gone: boolean;
};

const FIELD_NAMES: Record<string, string> = {
  player_name: "Player", estimated_value: "Estimated value", sale_price: "Price", paid_price: "What you paid", status: "Status",
  box_id: "Box", slot_row: "Row", slot_number: "Slot", set_name: "Set", card_number: "Card number", variety: "Parallel / variety",
  condition: "Condition", card_grade: "Grade", grading_company: "Grading company", name: "Box name", start_bid: "Start bid",
};

function fieldName(f: string): string {
  return FIELD_NAMES[f] ?? f.replace(/_/g, " ").replace(/^./, (c) => c.toUpperCase());
}

function show(field: string, v: unknown): string {
  if (v === null || v === undefined || v === "") return "(empty)";
  if (typeof v === "boolean") return v ? "Yes" : "No";
  if (typeof v === "number" && /value|price|bid|paid/.test(field)) return `$${v.toFixed(2)}`;
  if (field === "status" && typeof v === "string") return STATUS_LABELS[v] ?? v;
  if (field === "box_id") return "another box";
  return String(v);
}

export default function ConflictList({ initial }: { initial: Conflict[] }) {
  const [list, setList] = useState(initial);
  const [busy, setBusy] = useState(0);
  const [error, setError] = useState("");

  async function resolve(c: Conflict, use: "kept" | "lost") {
    setBusy(c.id);
    setError("");
    const r = await send("POST", `/v1/inventory/conflicts/${c.id}`, { use });
    setBusy(0);
    if (!r.ok && r.status !== 404) return setError(r.message);
    setList(list.filter((x) => x.id !== c.id));
  }

  if (list.length === 0) return <p className="card mt-6">No conflicts. Everything is in step.</p>;
  return (
    <>
      {error && <p role="alert" className="mt-4 text-bad">{error}</p>}
      <ul className="mt-6 space-y-3">
        {list.map((c) => (
          <li key={c.id} className="card">
            <div className="flex flex-wrap items-baseline justify-between gap-2">
              <p className="font-bold">
                {c.entity === "card" && !c.gone ? (
                  <Link href={`/inventory/cards/${c.entity_id}`} className="hover:text-fire">{c.name || "A card"}</Link>
                ) : (
                  c.name || (c.entity === "box" ? "A box" : "A card")
                )}{" "}
                <span className="font-normal text-muted">· {fieldName(c.field)}</span>
              </p>
              <span className="text-[12px] text-muted">{when(c.created_at)}</span>
            </div>
            <div className="mt-3 grid gap-2 text-[14px] sm:grid-cols-2">
              <div className="rounded-lg border border-go px-3 py-2">
                <p className="text-[12px] text-muted">Kept (newer)</p>
                <p className="font-semibold">{show(c.field, c.kept)}</p>
              </div>
              <div className="rounded-lg border border-line px-3 py-2">
                <p className="text-[12px] text-muted">Replaced{c.lost_device ? ` · from ${c.lost_device}` : " · from the website"}</p>
                <p className="font-semibold">{show(c.field, c.lost)}</p>
              </div>
            </div>
            {c.gone ? (
              <div className="mt-3 flex items-center justify-between gap-2 text-[13px] text-muted">
                This {c.entity === "box" ? "box" : "card"} has since been deleted or sold.
                <button type="button" className="btn-ghost !py-1" disabled={busy === c.id} onClick={() => resolve(c, "kept")}>Dismiss</button>
              </div>
            ) : (
              <div className="mt-3 flex flex-wrap gap-2">
                <button type="button" className="btn !py-1.5" disabled={busy === c.id} onClick={() => resolve(c, "kept")}>
                  Keep {show(c.field, c.kept)}
                </button>
                <button type="button" className="btn-ghost !py-1.5" disabled={busy === c.id} onClick={() => resolve(c, "lost")}>
                  Use {show(c.field, c.lost)} instead
                </button>
              </div>
            )}
          </li>
        ))}
      </ul>
    </>
  );
}
