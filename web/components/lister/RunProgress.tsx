"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { send, when } from "@/lib/client";

type Item = { position: number; card_id: string; name: string; status: string; message?: string };
export type Run = { id: string; market: string; status: string; created_at: string; finished_at?: string; counts: Record<string, number>; items?: Item[] };

const LOOK: Record<string, [string, string]> = {
  pending: ["Waiting", "text-muted"],
  listed: ["Listed", "text-go"],
  updated: ["Updated", "text-go"],
  skipped: ["Skipped", "text-fire"],
  failed: ["Failed", "text-bad"],
};

/** A listing run's progress, refreshed every 2 seconds until it's done. */
export default function RunProgress({ initial }: { initial: Run }) {
  const [run, setRun] = useState(initial);
  const [error, setError] = useState("");
  const running = run.status === "running";

  useEffect(() => {
    if (!running) return;
    const t = setInterval(async () => {
      const r = await send<Run>("GET", `/v1/lister/jobs/${run.id}`);
      if (r.ok) setRun(r.data);
    }, 2000);
    return () => clearInterval(t);
  }, [running, run.id]);

  async function stop() {
    const r = await send<Run>("POST", `/v1/lister/jobs/${run.id}/stop`);
    if (r.ok) setRun(r.data);
    else setError(r.message);
  }

  const items = run.items ?? [];
  const done = items.filter((i) => i.status !== "pending").length;
  const ok = (run.counts.listed ?? 0) + (run.counts.updated ?? 0);
  const problems = items.filter((i) => i.status === "skipped" || i.status === "failed");

  return (
    <div className="space-y-4">
      <div className="card">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <p className="text-[18px] font-bold">
            {running ? `Listing ${done} of ${items.length}...` : run.status === "stopped" ? "Stopped" : "Finished"}
          </p>
          {running && <button type="button" className="btn-ghost !py-1.5 text-bad" onClick={stop}>Stop</button>}
        </div>
        <div className="mt-3 h-2 overflow-hidden rounded-full bg-paper" aria-hidden>
          <div className="h-full rounded-full bg-go transition-all" style={{ width: `${items.length ? (done / items.length) * 100 : 0}%` }} />
        </div>
        <p className="num mt-2 text-[14px]">
          <b className="text-go">{ok}</b> on Slabs &amp; Singles · <b className={problems.length ? "text-fire" : ""}>{problems.length}</b> not listed
          {run.finished_at && <span className="text-muted"> · finished {when(run.finished_at)}</span>}
        </p>
        {!running && ok > 0 && <p className="mt-1 text-[13px] text-muted">Your PC shows these as live on Slabs &amp; Singles after its next sync.</p>}
        {error && <p role="alert" className="mt-2 text-bad">{error}</p>}
      </div>

      {problems.length > 0 && !running && (
        <p className="text-[13px] text-muted">Fix the cards below (open one to edit it), then list them again.</p>
      )}
      <ul className="divide-y divide-line rounded-xl border border-line">
        {items.map((i) => {
          const [label, color] = LOOK[i.status] ?? [i.status, ""];
          return (
            <li key={i.position} className="flex flex-wrap items-baseline gap-x-3 gap-y-0.5 px-3 py-2 text-[14px]">
              <span className={`w-16 shrink-0 text-[12px] font-bold uppercase ${color}`}>{label}</span>
              <Link href={`/inventory/cards/${i.card_id}`} className="min-w-0 flex-1 truncate font-semibold hover:text-fire">{i.name || "Card"}</Link>
              {i.message && i.status !== "listed" && i.status !== "updated" && <span className="w-full pl-[76px] text-[12px] text-muted">{i.message}</span>}
            </li>
          );
        })}
      </ul>
    </div>
  );
}
