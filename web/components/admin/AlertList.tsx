"use client";

import Link from "next/link";
import { useState } from "react";
import { send, when } from "@/lib/client";

export type Alert = {
  key: string; kind: string; level: "high" | "medium" | "low"; title: string; detail: string;
  account_id?: string; account_email?: string; ip?: string; count: number; at: string;
};

const LEVEL: Record<string, string> = { high: "bg-bad", medium: "bg-fire", low: "bg-muted" };

/** Suspicious activity; "Looked at" hides one until more of the same happens. */
export default function AlertList({ initial }: { initial: Alert[] }) {
  const [rows, setRows] = useState(initial);
  const [msg, setMsg] = useState("");
  async function dismiss(a: Alert) {
    const r = await send("POST", "/v1/admin/alerts/dismiss", { key: a.key });
    if (!r.ok) return setMsg(r.message);
    setRows(rows.filter((x) => x.key !== a.key));
  }
  if (rows.length === 0) return <p className="card mt-3 text-muted">Nothing suspicious right now.</p>;
  return (
    <div className="mt-3 space-y-2">
      {msg && <p className="text-bad">{msg}</p>}
      {rows.map((a) => (
        <div key={a.key} className="card flex flex-wrap items-start gap-3">
          <span className={`mt-0.5 rounded-full px-2 py-0.5 text-[11px] font-bold uppercase text-white ${LEVEL[a.level]}`}>{a.level}</span>
          <div className="min-w-0 flex-1">
            <p className="font-semibold">{a.title}</p>
            <p className="break-words text-[13px] text-muted">
              {a.account_id && <Link href={`/admin/members/${a.account_id}`} className="font-semibold text-fire">{a.account_email}</Link>}
              {a.ip && <Link href={`/admin/members?q=${encodeURIComponent(a.ip)}`} className="font-semibold text-fire">{a.ip}</Link>}
              {(a.account_id || a.ip) && a.detail ? " · " : ""}{a.detail}
            </p>
            <p className="text-[12px] text-muted">{when(a.at)}</p>
          </div>
          <button type="button" className="btn-ghost !py-1 text-[13px]" onClick={() => dismiss(a)}>Looked at</button>
        </div>
      ))}
    </div>
  );
}
